"""브라우저 세션·CSRF·로그아웃 테스트(#25). API 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import json
import uuid
from collections.abc import Iterator

import pytest
from fastapi import Response
from fastapi.testclient import TestClient
from helpers import bearer, csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, func, select, text, update

from agent_office.auth import sessions as session_service
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app
from agent_office.security import token_hash


def test_session_cookie_attributes(monkeypatch: pytest.MonkeyPatch) -> None:
    response = Response()
    session_service.set_session_cookie(response, "synthetic-token")
    header = response.headers["set-cookie"].lower()

    assert "httponly" in header and "secure" in header
    assert "samesite=lax" in header and "path=/" in header

    monkeypatch.setenv(session_service.COOKIE_SECURE_ENV, "false")
    local = Response()
    session_service.set_session_cookie(local, "synthetic-token")
    assert "secure" not in local.headers["set-cookie"].lower()


def test_csrf_token_is_bound_to_session() -> None:
    first = session_service.csrf_token_for("session-a")

    assert first == session_service.csrf_token_for("session-a")
    assert first != session_service.csrf_token_for("session-b")
    assert "session-a" not in first


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def test_current_session_returns_user_and_csrf_token(client: TestClient, engine: Engine) -> None:
    user_id, session = sign_in(engine)

    body = client.get("/api/v1/auth/session", headers=session_cookie(session)).json()

    assert body["user"]["id"] == str(user_id)
    assert body["csrf_token"] == session_service.csrf_token_for(session)
    assert session not in json.dumps(body)


def test_csrf_token_must_match_this_session(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    _, other = sign_in(engine)
    url = "/api/v1/devices/registration-codes"

    assert client.post(url, headers=session_cookie(session)).status_code == 403
    assert client.post(url, headers={**session_cookie(session), **csrf(other)}).status_code == 403
    assert (
        client.post(url, headers={**session_cookie(session), "X-CSRF-Token": "forged"}).status_code
        == 403
    )
    assert client.post(url, headers={**session_cookie(session), **csrf(session)}).status_code == 201


def test_logout_revokes_session_and_clears_cookie(client: TestClient, engine: Engine) -> None:
    user_id, session = sign_in(engine)

    response = client.post(
        "/api/v1/auth/logout", headers={**session_cookie(session), **csrf(session)}
    )

    assert response.status_code == 204
    cleared = response.headers["set-cookie"].lower()
    assert cleared.startswith(f"{session_service.SESSION_COOKIE}=") and "max-age=0" in cleared
    assert client.get("/api/v1/auth/session", headers=session_cookie(session)).status_code == 401
    with engine.connect() as connection:
        actions = (
            connection.execute(
                select(schema.audit_logs.c.action).where(
                    schema.audit_logs.c.actor_user_id == user_id
                )
            )
            .scalars()
            .all()
        )
    assert "auth.logout" in actions


def test_logout_all_ends_browser_sessions_but_not_devices(
    client: TestClient, engine: Engine
) -> None:
    user_id, first = sign_in(engine)
    with engine.begin() as connection:
        second = session_service.issue_session(connection, user_id)
    device = register(client, new_code(client, first))

    assert (
        client.post(
            "/api/v1/auth/logout-all", headers={**session_cookie(second), **csrf(second)}
        ).status_code
        == 204
    )
    for token in (first, second):
        assert client.get("/api/v1/auth/session", headers=session_cookie(token)).status_code == 401
    heartbeat = client.post(
        "/api/v1/devices/heartbeat", json={}, headers=bearer(device["device_token"])
    )
    assert heartbeat.status_code == 204


def test_list_and_revoke_own_sessions_only(client: TestClient, engine: Engine) -> None:
    user_id, current = sign_in(engine)
    with engine.begin() as connection:
        other_session = session_service.issue_session(connection, user_id)
    _, stranger = sign_in(engine)

    listed = client.get("/api/v1/auth/sessions", headers=session_cookie(current)).json()
    assert len(listed) == 2
    assert [s["current"] for s in listed].count(True) == 1
    target = next(s["id"] for s in listed if not s["current"])

    path = f"/api/v1/auth/sessions/{target}/revoke"
    assert (
        client.post(path, headers={**session_cookie(stranger), **csrf(stranger)}).status_code == 404
    )
    assert (
        client.post(path, headers={**session_cookie(current), **csrf(current)}).status_code == 204
    )
    assert (
        client.get("/api/v1/auth/session", headers=session_cookie(other_session)).status_code == 401
    )
    assert client.get("/api/v1/auth/session", headers=session_cookie(current)).status_code == 200


def test_absolute_and_idle_expiry(client: TestClient, engine: Engine) -> None:
    _, idle = sign_in(engine)
    _, expired = sign_in(engine)
    sessions = schema.login_sessions
    with engine.begin() as connection:
        connection.execute(
            update(sessions)
            .where(sessions.c.token_hash == token_hash(idle))
            .values(last_seen_at=text("now() - interval '8 days'"))
        )
        connection.execute(
            update(sessions)
            .where(sessions.c.token_hash == token_hash(expired))
            .values(
                created_at=text("now() - interval '31 days'"),
                last_seen_at=text("now() - interval '1 minute'"),
                expires_at=text("now() - interval '1 day'"),
            )
        )

    for token in (idle, expired):
        assert client.get("/api/v1/auth/session", headers=session_cookie(token)).status_code == 401


def test_last_seen_is_touched_at_most_every_few_minutes(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    sessions = schema.login_sessions
    where = sessions.c.token_hash == token_hash(session)

    def last_seen() -> object:
        with engine.connect() as connection:
            return connection.execute(select(sessions.c.last_seen_at).where(where)).scalar_one()

    before = last_seen()
    client.get("/api/v1/auth/session", headers=session_cookie(session))
    assert last_seen() == before

    with engine.begin() as connection:
        connection.execute(
            update(sessions).where(where).values(last_seen_at=text("now() - interval '10 minutes'"))
        )
    stale = last_seen()
    client.get("/api/v1/auth/session", headers=session_cookie(session))
    assert last_seen() != stale


def test_issue_session_replaces_previous_and_caps_active_sessions(engine: Engine) -> None:
    user_id, first = sign_in(engine)
    sessions = schema.login_sessions
    with engine.begin() as connection:
        replaced = session_service.issue_session(connection, user_id, replaces_token=first)
        for _ in range(session_service.MAX_ACTIVE_SESSIONS + 5):
            session_service.issue_session(connection, user_id)
        first_revoked = connection.execute(
            select(sessions.c.revoked_at).where(sessions.c.token_hash == token_hash(first))
        ).scalar_one()
        active = connection.execute(
            select(func.count())
            .select_from(sessions)
            .where(sessions.c.user_id == user_id, sessions.c.revoked_at.is_(None))
        ).scalar_one()
        stored = connection.execute(select(sessions.c.token_hash)).scalars().all()

    assert first_revoked is not None
    assert active == session_service.MAX_ACTIVE_SESSIONS
    assert replaced.encode() not in b"".join(stored)


def test_disabled_user_session_is_rejected(client: TestClient, engine: Engine) -> None:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users).where(schema.users.c.id == user_id).values(status="disabled")
        )

    assert client.get("/api/v1/auth/session", headers=session_cookie(session)).status_code == 401
    assert uuid.UUID(str(user_id))

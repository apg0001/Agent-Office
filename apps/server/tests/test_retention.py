"""보존 기간 정리·계정 삭제 테스트(#39). PostgreSQL이 필요하다."""

from __future__ import annotations

import uuid
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from helpers import csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, func, select, text, update
from test_office_live import send_event

from agent_office import retention
from agent_office.api import events as events_api
from agent_office.auth import passwords
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def count(engine: Engine, table: str, where: str = "true") -> int:
    with engine.connect() as connection:
        return int(
            connection.execute(text(f"SELECT count(*) FROM {table} WHERE {where}")).scalar_one()
        )


def purge(engine: Engine) -> dict[str, int]:
    with engine.begin() as connection:
        counts = retention.purge(connection)
    assert counts is not None
    return counts


def age(engine: Engine, statement: str) -> None:
    with engine.begin() as connection:
        connection.execute(text(statement))


def test_old_events_and_ended_sessions_are_removed_but_open_state_stays(
    client: TestClient, engine: Engine
) -> None:
    _, session = sign_in(engine)
    device = register(client, new_code(client, session))
    open_sid, ended_sid = uuid.uuid4(), uuid.uuid4()
    send_event(client, device, open_sid, uuid.uuid4(), "session.started", minute=0)
    ended_agent = uuid.uuid4()
    send_event(client, device, ended_sid, ended_agent, "session.started", minute=0)
    send_event(client, device, ended_sid, ended_agent, "session.ended", minute=1)

    age(engine, "UPDATE activity_events SET received_at = now() - interval '31 days'")
    age(
        engine,
        "UPDATE agent_sessions SET ended_at = now() - interval '31 days' WHERE lifecycle = 'ENDED'",
    )
    counts = purge(engine)

    assert counts["activity_events"] == 3 and counts["ended_sessions"] == 1
    assert count(engine, "activity_events") == 0
    # 열린 세션과 그 현재 상태는 이벤트가 지워져도 남는다.
    assert count(engine, "agent_sessions") == 1
    assert count(engine, "agent_sessions", "lifecycle = 'OPEN'") == 1
    assert count(engine, "agent_current_states") == 1


def test_recent_data_is_kept(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    device = register(client, new_code(client, session))
    send_event(client, device, uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)

    counts = purge(engine)

    assert counts["activity_events"] == 0 and count(engine, "activity_events") == 1


def test_expired_tokens_and_old_audit_logs_are_removed(engine: Engine) -> None:
    user_id, _ = sign_in(engine)
    with engine.begin() as connection:
        for table in (schema.email_verifications, schema.password_resets):
            connection.execute(
                table.insert().values(
                    user_id=user_id,
                    token_hash=uuid.uuid4().bytes * 2,
                    created_at=text("now() - interval '3 days'"),
                    expires_at=text("now() - interval '2 days'"),
                )
            )
        connection.execute(
            schema.audit_logs.insert().values(
                action="old", outcome="success", occurred_at=text("now() - interval '400 days'")
            )
        )
    counts = purge(engine)

    assert counts["email_verifications"] == counts["password_resets"] == 1
    assert counts["audit_logs"] == 1
    assert count(engine, "audit_logs", "action = 'old'") == 0


def test_concurrent_purge_is_skipped(engine: Engine) -> None:
    with engine.begin() as first, engine.begin() as second:
        assert retention.purge(first) is not None
        assert retention.purge(second) is None


def test_audit_retention_has_a_floor(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(retention.AUDIT_RETENTION_ENV, "1")
    assert retention.audit_retention().days == 30
    monkeypatch.setenv(retention.AUDIT_RETENTION_ENV, "nope")
    assert retention.audit_retention().days == 365


PASSWORD = "delete me please"


def account_user(engine: Engine, role: str = "USER") -> tuple[uuid.UUID, dict[str, str], str]:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users)
            .where(schema.users.c.id == user_id)
            .values(password_hash=passwords.hash_password(PASSWORD), instance_role=role)
        )
        email = connection.execute(
            select(schema.users.c.email).where(schema.users.c.id == user_id)
        ).scalar_one()
    return user_id, {**session_cookie(session), **csrf(session)}, str(email)


def delete_account(client: TestClient, headers: dict[str, str], body: dict[str, str]) -> int:
    return client.request("DELETE", "/api/v1/auth/account", json=body, headers=headers).status_code


def test_account_deletion_removes_data_and_survives_restore(
    client: TestClient, engine: Engine
) -> None:
    user_id, headers, email = account_user(engine)
    device = register(client, new_code(client, headers["Cookie"].split("=", 1)[1]))
    send_event(client, device, uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)
    solo = client.post("/api/v1/teams", json={"name": "혼자"}, headers=headers).json()["id"]

    assert (
        delete_account(client, headers, {"confirm_email": "x@example.com", "password": PASSWORD})
        == 400
    )
    assert (
        delete_account(client, headers, {"confirm_email": email, "password": "wrong!!!!!"}) == 401
    )
    assert delete_account(client, headers, {"confirm_email": email, "password": PASSWORD}) == 204

    assert count(engine, "users", f"id = '{user_id}'") == 0
    assert count(engine, "devices") == 0 and count(engine, "activity_events") == 0
    assert count(engine, "teams", f"id = '{solo}'") == 0
    assert count(engine, "deleted_users", f"user_id = '{user_id}'") == 1
    assert client.get("/api/v1/auth/session", headers=headers).status_code == 401

    # 백업 복원으로 계정이 되살아나도 보존 작업이 다시 지운다.
    with engine.begin() as connection:
        connection.execute(
            schema.users.insert().values(id=user_id, email=email, display_name="restored")
        )
    assert purge(engine)["deleted_users_reapplied"] == 1
    assert count(engine, "users", f"id = '{user_id}'") == 0


def test_account_deletion_guards(client: TestClient, engine: Engine) -> None:
    _, admin_headers, admin_email = account_user(engine, "SUPER_ADMIN")
    body = {"confirm_email": admin_email, "password": PASSWORD}
    response = client.request("DELETE", "/api/v1/auth/account", json=body, headers=admin_headers)
    assert response.status_code == 409 and response.json()["detail"] == "last_super_admin"

    _, owner_headers, owner_email = account_user(engine)
    member_id, member_headers, member_email = account_user(engine)
    team = client.post("/api/v1/teams", json={"name": "공동"}, headers=owner_headers).json()["id"]
    invite = client.post(
        f"/api/v1/teams/{team}/invitations", json={"email": member_email}, headers=owner_headers
    ).json()["invite_url"]
    token = invite.split("#token=")[1]
    client.post("/api/v1/invitations/accept", json={"token": token}, headers=member_headers)

    owner_body = {"confirm_email": owner_email, "password": PASSWORD}
    blocked = client.request(
        "DELETE", "/api/v1/auth/account", json=owner_body, headers=owner_headers
    )
    assert blocked.status_code == 409 and blocked.json()["detail"] == "owner_must_transfer"
    # 구성원은 지울 수 있고 팀은 남는다.
    member_body = {"confirm_email": member_email, "password": PASSWORD}
    assert delete_account(client, member_headers, member_body) == 204
    with engine.connect() as connection:
        members = connection.execute(
            select(func.count())
            .select_from(schema.team_members)
            .where(schema.team_members.c.team_id == team)
        ).scalar_one()
    assert members == 1 and member_id

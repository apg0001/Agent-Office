"""세션 철회가 REST와 WebSocket에 즉시 반영되는지(#64)."""

from __future__ import annotations

from collections.abc import Callable

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import Engine
from starlette.websockets import WebSocketDisconnect

from agent_office.api import office
from agent_office.auth import accounts
from agent_office.auth.sessions import SESSION_COOKIE
from security.secworld import ORIGIN, Actor, FakeMailer, login_session_id, make_user, new_session

PASSWORD = "correct horse battery"


def _assert_ws_closes(
    client: TestClient,
    session: str,
    query: str,
    revoke: Callable[[], None],
    code: int = office.CLOSE_UNAUTHORIZED,
) -> None:
    headers = {"cookie": f"{SESSION_COOKIE}={session}", "host": "testserver"}
    with client.websocket_connect(f"/api/v1/live{query}", headers=headers) as ws:
        assert ws.receive_json()["type"] == "snapshot"
        revoke()
        with pytest.raises(WebSocketDisconnect) as closed:
            for _ in range(400):
                ws.receive_json()
    assert closed.value.code == code


def _team(client: TestClient, actor: Actor) -> str:
    response = client.post("/api/v1/teams", json={"name": "t"}, headers=actor.headers)
    return str(response.json()["id"])


@pytest.mark.parametrize("scope", ["personal", "team", "combined"])
def test_sec_session_001_logout_closes_rest_and_ws(
    client: TestClient, engine: Engine, scope: str
) -> None:
    """SEC-SESSION-001: 로그아웃하면 그 세션의 REST는 바로 401, 열린 WebSocket은 4401."""
    actor = make_user(engine)
    query = {"personal": "", "team": f"?team={_team(client, actor)}", "combined": "?view=combined"}

    def revoke() -> None:
        assert client.post("/api/v1/auth/logout", headers=actor.headers).status_code == 204

    _assert_ws_closes(client, actor.session, query[scope], revoke)
    assert client.get("/api/v1/auth/session", headers=actor.cookie).status_code == 401
    assert client.get("/api/v1/office", headers=actor.cookie).status_code == 401


def test_sec_session_002_logout_all_and_targeted_revoke(client: TestClient, engine: Engine) -> None:
    """SEC-SESSION-002: 다른 세션에서 개별 폐기·전체 로그아웃하면 대상 세션의 REST·WS가 끊긴다."""
    actor = make_user(engine)
    second = new_session(engine, actor.id)
    second_id = login_session_id(engine, second)

    def revoke_second() -> None:
        response = client.post(f"/api/v1/auth/sessions/{second_id}/revoke", headers=actor.headers)
        assert response.status_code == 204

    _assert_ws_closes(client, second, "?view=combined", revoke_second)
    assert (
        client.get(
            "/api/v1/auth/session", headers={"Cookie": f"{SESSION_COOKIE}={second}"}
        ).status_code
        == 401
    )
    assert client.get("/api/v1/auth/session", headers=actor.cookie).status_code == 200

    third = new_session(engine, actor.id)

    def logout_all() -> None:
        assert client.post("/api/v1/auth/logout-all", headers=actor.headers).status_code == 204

    _assert_ws_closes(client, third, "", logout_all)
    for token in (actor.session, third):
        cookie = {"Cookie": f"{SESSION_COOKIE}={token}"}
        assert client.get("/api/v1/auth/session", headers=cookie).status_code == 401


def test_sec_session_003_password_reset_revokes_sessions_and_ws(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    """SEC-SESSION-003: 비밀번호 재설정은 모든 브라우저 세션을 폐기하고 열린 WebSocket을 닫는다."""
    with engine.begin() as connection:
        accounts.create_user(
            connection,
            email="reset@example.invalid",
            display_name="r",
            password=PASSWORD,
            status="active",
            email_verified=True,
        )
    login = client.post(
        "/api/v1/auth/login",
        json={"email": "reset@example.invalid", "password": PASSWORD},
        headers={"origin": ORIGIN},
    )
    session = login.cookies.get(SESSION_COOKIE)
    assert session

    def reset() -> None:
        requested = client.post(
            "/api/v1/auth/password-reset/request",
            json={"email": "reset@example.invalid"},
            headers={"origin": ORIGIN},
        )
        assert requested.status_code == 202
        confirmed = client.post(
            "/api/v1/auth/password-reset/confirm",
            json={"token": outbox.token(), "password": "another long passphrase"},
            headers={"origin": ORIGIN},
        )
        assert confirmed.status_code == 204

    _assert_ws_closes(client, session, "", reset)
    cookie = {"Cookie": f"{SESSION_COOKIE}={session}"}
    assert client.get("/api/v1/auth/session", headers=cookie).status_code == 401


def test_sec_session_004_admin_disable_closes_sessions_and_ws(
    client: TestClient, engine: Engine
) -> None:
    """SEC-SESSION-004: 관리자가 비활성화하면 그 사용자의 REST·WS가 즉시 끊긴다."""
    admin = make_user(engine, role="ADMIN")
    victim = make_user(engine)

    def disable() -> None:
        response = client.post(f"/api/v1/admin/users/{victim.id}/disable", headers=admin.headers)
        assert response.status_code == 200

    _assert_ws_closes(client, victim.session, f"?team={_team(client, victim)}", disable)
    assert client.get("/api/v1/auth/session", headers=victim.cookie).status_code == 401


def test_sec_session_005_role_change_takes_effect_on_next_request(
    client: TestClient, engine: Engine
) -> None:
    """SEC-SESSION-005: 강등된 관리자는 기존 세션으로도 다음 요청부터 관리 경로 403."""
    super_admin = make_user(engine, role="SUPER_ADMIN")
    admin = make_user(engine, role="ADMIN")
    assert client.get("/api/v1/admin/users", headers=admin.cookie).status_code == 200
    demoted = client.put(
        f"/api/v1/admin/users/{admin.id}/role",
        json={"instance_role": "USER"},
        headers=super_admin.headers,
    )
    assert demoted.status_code == 200
    assert client.get("/api/v1/admin/users", headers=admin.cookie).status_code == 403

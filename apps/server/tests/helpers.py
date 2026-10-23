"""서버 통합 테스트 공통 헬퍼. 값은 모두 합성이다."""

from __future__ import annotations

import secrets
import uuid

from fastapi.testclient import TestClient
from sqlalchemy import Engine

from agent_office.auth.sessions import CSRF_HEADER, SESSION_COOKIE, csrf_token_for, issue_session
from agent_office.db import schema


def sign_in(engine: Engine, status: str = "active") -> tuple[uuid.UUID, str]:
    """#23 로그인 전까지 사용자는 테스트가 만들고, 세션은 실제 발급 함수로 만든다."""
    with engine.begin() as connection:
        user_id: uuid.UUID = connection.execute(
            schema.users.insert()
            .values(
                email=f"{secrets.token_hex(4)}@example.invalid",
                display_name="synthetic",
                status=status,
            )
            .returning(schema.users.c.id)
        ).scalar_one()
        token = issue_session(connection, user_id)
    return user_id, token


def csrf(session: str) -> dict[str, str]:
    return {CSRF_HEADER: csrf_token_for(session)}


def session_cookie(token: str) -> dict[str, str]:
    return {"Cookie": f"{SESSION_COOKIE}={token}"}


def bearer(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def new_code(client: TestClient, session: str) -> str:
    response = client.post(
        "/api/v1/devices/registration-codes", headers={**csrf(session), **session_cookie(session)}
    )
    assert response.status_code == 201, response.text
    return str(response.json()["code"])


def register(client: TestClient, code: str, name: str = "pc-1") -> dict[str, str]:
    response = client.post(
        "/api/v1/devices/register",
        json={"registration_code": code, "name": name, "platform": "linux"},
    )
    assert response.status_code == 201, response.text
    return dict(response.json())

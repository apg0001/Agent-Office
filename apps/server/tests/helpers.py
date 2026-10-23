"""서버 통합 테스트 공통 헬퍼. 값은 모두 합성이다."""

from __future__ import annotations

import hashlib
import secrets
import uuid

from fastapi.testclient import TestClient
from sqlalchemy import Engine, text

from agent_office.api.deps import CSRF_HEADER, SESSION_COOKIE
from agent_office.db import schema

CSRF = {CSRF_HEADER: "1"}


def sign_in(engine: Engine, status: str = "active") -> tuple[uuid.UUID, str]:
    """#25 전까지 로그인 세션은 테스트가 직접 만든다(토큰 해시만 저장)."""
    token = secrets.token_urlsafe(32)
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
        connection.execute(
            schema.login_sessions.insert().values(
                user_id=user_id,
                token_hash=hashlib.sha256(token.encode()).digest(),
                expires_at=text("now() + interval '1 hour'"),
            )
        )
    return user_id, token


def session_cookie(token: str) -> dict[str, str]:
    return {"Cookie": f"{SESSION_COOKIE}={token}"}


def bearer(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def new_code(client: TestClient, session: str) -> str:
    response = client.post(
        "/api/v1/devices/registration-codes", headers={**CSRF, **session_cookie(session)}
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

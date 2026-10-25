"""장치 등록·인증·회전·폐기 API 통합 테스트(#26). PostgreSQL이 필요하다."""

from __future__ import annotations

import hashlib
import json
import uuid
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, select, text, update

from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def test_registration_flow_binds_device_to_code_owner(client: TestClient, engine: Engine) -> None:
    user_id, session = sign_in(engine)
    code = new_code(client, session)
    assert len(code) == 9 and code[4] == "-"

    # 입력 형식이 달라도(소문자·공백) 같은 코드로 인정한다.
    device = register(client, code.lower().replace("-", " "))
    assert device["device_token"].startswith("aod_")

    heartbeat = client.post(
        "/api/v1/devices/heartbeat",
        json={"collector_version": "0.1.0", "queue_depth": 3},
        headers=bearer(device["device_token"]),
    )
    assert heartbeat.status_code == 204

    listed = client.get("/api/v1/devices", headers=session_cookie(session)).json()
    assert [d["id"] for d in listed] == [device["device_id"]]
    assert listed[0]["collector_version"] == "0.1.0"
    assert listed[0]["last_seen_at"] is not None
    assert listed[0]["connection"] == "ONLINE"

    with engine.connect() as connection:
        owner = connection.execute(
            select(schema.devices.c.owner_user_id).where(
                schema.devices.c.id == uuid.UUID(device["device_id"])
            )
        ).scalar_one()
        stored = connection.execute(select(schema.device_credentials.c.token_hash)).scalar_one()
        audit_dump = json.dumps(
            [dict(r._mapping) for r in connection.execute(select(schema.audit_logs))],
            default=str,
        )
    assert owner == user_id
    assert stored == hashlib.sha256(device["device_token"].encode()).digest()
    # 감사 로그에는 토큰·등록 코드 원문이 없다.
    assert device["device_token"] not in audit_dump and code not in audit_dump
    assert "device.registered" in audit_dump


def test_registration_code_is_single_use_and_expires(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    code = new_code(client, session)
    register(client, code)

    reused = client.post(
        "/api/v1/devices/register", json={"registration_code": code, "name": "pc-2"}
    )
    expired_code = new_code(client, session)
    with engine.begin() as connection:
        connection.execute(
            update(schema.device_registration_codes)
            .where(schema.device_registration_codes.c.consumed_at.is_(None))
            .values(
                created_at=text("now() - interval '20 minutes'"),
                expires_at=text("now() - interval '10 minutes'"),
            )
        )
    expired = client.post(
        "/api/v1/devices/register", json={"registration_code": expired_code, "name": "pc-3"}
    )
    malformed = client.post(
        "/api/v1/devices/register", json={"registration_code": "not-a-code", "name": "pc-4"}
    )
    unknown = client.post(
        "/api/v1/devices/register", json={"registration_code": "AAAA-AAAA", "name": "pc-5"}
    )

    # 사용됨·만료·형식 오류·없음을 구분하지 않는다.
    for response in (reused, expired, malformed, unknown):
        assert response.status_code == 400
        assert response.json() == {"detail": "invalid_registration_code"}


def test_user_endpoints_require_session_and_csrf_header(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)

    no_session = client.post("/api/v1/devices/registration-codes", headers=csrf(session))
    no_csrf = client.post("/api/v1/devices/registration-codes", headers=session_cookie(session))
    forged = client.get("/api/v1/devices", headers=session_cookie("forged"))

    assert no_session.status_code == 401
    assert no_csrf.status_code == 403
    assert forged.status_code == 401


def test_client_supplied_identity_fields_are_rejected(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    code = new_code(client, session)

    response = client.post(
        "/api/v1/devices/register",
        json={"registration_code": code, "name": "pc", "owner_user_id": str(uuid.uuid4())},
    )

    assert response.status_code == 422


def test_name_conflict_does_not_consume_code(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    register(client, new_code(client, session), name="Laptop")
    code = new_code(client, session)

    conflict = client.post(
        "/api/v1/devices/register", json={"registration_code": code, "name": "laptop"}
    )

    assert conflict.status_code == 409
    assert register(client, code, name="desktop")["device_token"].startswith("aod_")


def test_open_registration_codes_are_limited(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    for _ in range(5):
        new_code(client, session)

    response = client.post(
        "/api/v1/devices/registration-codes", headers={**csrf(session), **session_cookie(session)}
    )

    assert response.status_code == 429


def test_token_rotation_keeps_old_token_only_for_grace_period(
    client: TestClient, engine: Engine
) -> None:
    _, session = sign_in(engine)
    old = register(client, new_code(client, session))["device_token"]

    rotated = client.post("/api/v1/devices/token/rotate", headers=bearer(old))
    assert rotated.status_code == 200
    new = rotated.json()["device_token"]

    assert new != old
    assert client.post("/api/v1/devices/heartbeat", json={}, headers=bearer(new)).status_code == 204
    assert client.post("/api/v1/devices/heartbeat", json={}, headers=bearer(old)).status_code == 204
    assert client.post("/api/v1/devices/token/rotate", headers=bearer(old)).status_code == 409

    with engine.begin() as connection:
        credentials = schema.device_credentials
        connection.execute(
            update(credentials)
            .where(credentials.c.expires_at.is_not(None))
            .values(expires_at=text("now() - interval '1 second'"))
        )
    assert client.post("/api/v1/devices/heartbeat", json={}, headers=bearer(old)).status_code == 401
    assert client.post("/api/v1/devices/heartbeat", json={}, headers=bearer(new)).status_code == 204


def test_self_revoke_and_owner_revoke(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    _, other_session = sign_in(engine)
    first = register(client, new_code(client, session), name="pc-1")
    second = register(client, new_code(client, session), name="pc-2")

    assert (
        client.post("/api/v1/devices/revoke", headers=bearer(first["device_token"])).status_code
        == 204
    )
    assert (
        client.post(
            "/api/v1/devices/heartbeat", json={}, headers=bearer(first["device_token"])
        ).status_code
        == 401
    )

    # 다른 사용자는 내 장치를 보거나 폐기할 수 없고 존재 여부도 알 수 없다.
    path = f"/api/v1/devices/{second['device_id']}/revoke"
    assert client.get("/api/v1/devices", headers=session_cookie(other_session)).json() == []
    assert (
        client.post(
            path, headers={**csrf(other_session), **session_cookie(other_session)}
        ).status_code
        == 404
    )
    assert client.post(path, headers=session_cookie(session)).status_code == 403
    assert (
        client.post(path, headers={**csrf(session), **session_cookie(session)}).status_code == 204
    )
    assert (
        client.post(
            "/api/v1/devices/heartbeat", json={}, headers=bearer(second["device_token"])
        ).status_code
        == 401
    )

    listed = client.get("/api/v1/devices", headers=session_cookie(session)).json()
    assert all(d["revoked_at"] is not None for d in listed)
    assert all(d["connection"] == "DISCONNECTED" for d in listed)


def test_disabled_owner_loses_device_access(client: TestClient, engine: Engine) -> None:
    user_id, session = sign_in(engine)
    token = register(client, new_code(client, session))["device_token"]
    pending_code = new_code(client, session)

    with engine.begin() as connection:
        connection.execute(
            update(schema.users).where(schema.users.c.id == user_id).values(status="disabled")
        )

    assert (
        client.post("/api/v1/devices/heartbeat", json={}, headers=bearer(token)).status_code == 401
    )
    assert (
        client.post(
            "/api/v1/devices/register", json={"registration_code": pending_code, "name": "pc-9"}
        ).status_code
        == 400
    )


def test_malformed_device_tokens_are_rejected(client: TestClient) -> None:
    for header in (
        "",
        "Bearer",
        "Basic abc",
        "Bearer not-a-device-token",
        "Bearer aod_" + "x" * 300,
    ):
        response = client.post(
            "/api/v1/devices/heartbeat",
            json={},
            headers={"Authorization": header} if header else {},
        )
        assert response.status_code == 401

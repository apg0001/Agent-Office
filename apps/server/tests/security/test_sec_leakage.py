"""비밀·원문 유출 검사(#64).

설치·관리 설정·가입·로그인·복구·초대·장치 등록·토큰 회전·이벤트 수집·알림 시험·검증 오류까지
여러 흐름을 합성 sentinel 비밀로 실행한 뒤, sentinel이 응답 본문·헤더, 모든 DB 테이블(감사 로그·
인스턴스 설정·암호문 포함), 로그, 알림 메일 어디에도 없는지 확인한다. 원문이 반드시 한 번 나가야
하는 응답(장치 토큰 발급·회전, 초대 링크, 등록 코드)만 허용 목록에 둔다.
"""

from __future__ import annotations

import logging
import secrets
import urllib.error
import uuid
from typing import Any

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import bearer
from sqlalchemy import Engine, select

from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.notifications import manager, providers
from agent_office.setup import bootstrap
from security.secworld import ORIGIN, FakeMailer, event


def _sentinel(label: str) -> str:
    """실행마다 새로 만드는 합성 값. 저장소에 비밀 모양 문자열을 남기지 않는다(Gitleaks)."""
    return f"sentinel-{label}-{secrets.token_hex(6)}"


PASSWORD = _sentinel("admin")
USER_PASSWORD = _sentinel("user")
NEW_PASSWORD = _sentinel("new")
WRONG_PASSWORD = _sentinel("wrong")
SMTP_PASSWORD = _sentinel("smtp")
OAUTH_SECRET = _sentinel("oauth")
WEBHOOK_SECRET = _sentinel("webhook")
WEBHOOK = "https://discord.com/api/webhooks/123/" + WEBHOOK_SECRET
TELEGRAM = "123456:" + _sentinel("telegram") + "-0123456789"
RAW = _sentinel("raw-prompt")
BAD_BOOTSTRAP = _sentinel("bootstrap")


def _scan_database(engine: Engine) -> str:
    chunks = []
    with engine.connect() as connection:
        for table in schema.metadata.sorted_tables:
            for row in connection.execute(select(table)):
                for value in row:
                    if isinstance(value, bytes | memoryview):
                        chunks.append(bytes(value).decode("latin-1"))
                    else:
                        chunks.append(repr(value))
    return "\n".join(chunks)


def test_sec_leak_001_sentinels_never_leak(
    client: TestClient,
    engine: Engine,
    outbox: FakeMailer,
    caplog: pytest.LogCaptureFixture,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """SEC-LEAK-001: 모든 흐름의 sentinel 비밀이 응답·DB 전체·로그·메일 어디에도 없다."""
    caplog.set_level(logging.DEBUG)
    manager.reset()
    responses: list[tuple[str, httpx.Response]] = []

    def record(response: httpx.Response) -> None:
        response.read()
        responses.append((f"{response.request.method} {response.request.url.path}", response))

    client.event_hooks = {"response": [record]}
    origin = {"origin": ORIGIN}

    # 1. 최초 관리자(#66): 틀린 토큰, 맞는 토큰.
    with engine.begin() as connection:
        bootstrap_token = bootstrap.issue_token(connection)
    admin_body = {
        "email": "admin@example.invalid",
        "password": PASSWORD,
        "display_name": "admin",
    }
    rejected = client.post(
        "/api/v1/setup/admin", json={**admin_body, "bootstrap_token": BAD_BOOTSTRAP}, headers=origin
    )
    assert rejected.status_code == 403
    created = client.post(
        "/api/v1/setup/admin",
        json={**admin_body, "bootstrap_token": bootstrap_token},
        headers=origin,
    )
    assert created.status_code == 201
    admin_session = created.cookies.get(SESSION_COOKIE)
    assert admin_session
    admin = {
        "Cookie": f"{SESSION_COOKIE}={admin_session}",
        "X-CSRF-Token": created.json()["csrf_token"],
    }

    # 2. 관리 설정의 비밀(#67, #70~#74). 검증 오류(422)도 값을 되돌려 보내지 않아야 한다.
    base = "/api/v1/admin/settings"
    smtp = {
        "host": "smtp.example.invalid",
        "port": 587,
        "sender": "office@example.invalid",
        "password": SMTP_PASSWORD,
    }
    assert (
        client.put(f"{base}/smtp", json={**smtp, "port": SMTP_PASSWORD}, headers=admin).status_code
        == 422
    )
    assert (
        client.put(f"{base}/smtp", json={**smtp, SMTP_PASSWORD: 1}, headers=admin).status_code
        == 422
    )
    assert client.put(f"{base}/smtp", json=smtp, headers=admin).status_code == 200
    oauth = {"enabled": True, "client_id": "client-id", "client_secret": OAUTH_SECRET}
    assert client.put(f"{base}/oauth/google", json=oauth, headers=admin).status_code == 200
    assert (
        client.put(
            f"{base}/site",
            json={"service_name": "x", "public_url": "https://office.example"},
            headers=admin,
        ).status_code
        == 200
    )
    policy = client.put(
        f"{base}/signup-policy", json={"policy": "PUBLIC_VERIFIED_EMAIL"}, headers=admin
    )
    assert policy.status_code == 200
    channels = "/api/v1/admin/notifications"
    bad_channel = {
        "provider": "discord",
        "display_name": "d",
        "secret": "https://evil.example/" + WEBHOOK_SECRET,
    }
    assert client.post(channels, json=bad_channel, headers=admin).status_code == 400
    assert (
        client.post(
            channels, json={**bad_channel, "provider": WEBHOOK_SECRET}, headers=admin
        ).status_code
        == 422
    )
    discord = client.post(
        channels,
        json={"provider": "discord", "display_name": "d", "secret": WEBHOOK},
        headers=admin,
    )
    assert discord.status_code == 201
    telegram = client.post(
        channels,
        json={
            "provider": "telegram",
            "display_name": "t",
            "secret": TELEGRAM,
            "config": {"chat_id": "12345", "token": TELEGRAM},
        },
        headers=admin,
    )
    assert telegram.status_code == 201
    # 외부 전송 실패: 오류 문구에 비밀 주소가 들어가도 응답·로그·DB에는 원인 코드만 남는다.

    def fail(request: Any, timeout: float) -> Any:
        raise urllib.error.URLError(f"cannot reach {request.full_url}")

    monkeypatch.setattr(providers.urllib.request, "urlopen", fail)
    for channel in (discord, telegram):
        tested = client.post(f"{channels}/{channel.json()['id']}/test", headers=admin)
        assert tested.status_code == 200 and tested.json()["ok"] is False
    patched = client.patch(
        f"{channels}/{discord.json()['id']}", json={"secret": WEBHOOK + "x"}, headers=admin
    )
    assert patched.status_code == 200
    client.get(base, headers=admin)
    client.get(channels, headers=admin)
    client.get("/api/v1/admin/metrics", headers=admin)

    # 3. 가입·확인·로그인 실패/성공·복구(#23).
    user_email = "user@example.invalid"
    signup = {"email": user_email, "password": USER_PASSWORD, "display_name": "user"}
    assert (
        client.post(
            "/api/v1/auth/signup", json={**signup, "prompt": RAW}, headers=origin
        ).status_code
        == 422
    )
    assert (
        client.post("/api/v1/auth/signup", json={**signup, RAW: RAW}, headers=origin).status_code
        == 422
    )
    assert client.post("/api/v1/auth/signup", json=signup, headers=origin).status_code == 202
    assert (
        client.post(
            "/api/v1/auth/verify-email", json={"token": outbox.token()}, headers=origin
        ).status_code
        == 204
    )
    wrong = client.post(
        "/api/v1/auth/login", json={"email": user_email, "password": WRONG_PASSWORD}, headers=origin
    )
    assert wrong.status_code == 401
    login = client.post(
        "/api/v1/auth/login", json={"email": user_email, "password": USER_PASSWORD}, headers=origin
    )
    assert login.status_code == 200
    first_user_session = login.cookies.get(SESSION_COOKIE)
    assert first_user_session
    reset = client.post(
        "/api/v1/auth/password-reset/request", json={"email": user_email}, headers=origin
    )
    assert reset.status_code == 202
    reset_token = outbox.token()
    confirm = client.post(
        "/api/v1/auth/password-reset/confirm",
        json={"token": reset_token, "password": NEW_PASSWORD},
        headers=origin,
    )
    assert confirm.status_code == 204
    login = client.post(
        "/api/v1/auth/login", json={"email": user_email, "password": NEW_PASSWORD}, headers=origin
    )
    user_session = login.cookies.get(SESSION_COOKIE)
    assert user_session
    user = {
        "Cookie": f"{SESSION_COOKIE}={user_session}",
        "X-CSRF-Token": login.json()["csrf_token"],
    }

    # 4. 팀 초대(#27): 링크는 초대한 사람에게 한 번만.
    team = client.post("/api/v1/teams", json={"name": "team"}, headers=admin).json()["id"]
    invite = client.post(
        f"/api/v1/teams/{team}/invitations", json={"email": user_email}, headers=admin
    )
    invite_token = invite.json()["invite_url"].split("#token=")[1]
    client.get(f"/api/v1/teams/{team}/invitations", headers=admin)
    assert (
        client.post(
            "/api/v1/invitations/accept", json={"token": invite_token}, headers=user
        ).status_code
        == 200
    )

    # 5. 장치 등록·회전(#26), 원문 필드를 실은 이벤트(#35)는 422이고 저장되지 않는다.
    code = client.post("/api/v1/devices/registration-codes", headers=user).json()["code"]
    device = client.post(
        "/api/v1/devices/register",
        json={"registration_code": code, "name": "pc", "platform": "linux"},
    ).json()
    old_token = device["device_token"]
    rotated = client.post("/api/v1/devices/token/rotate", headers=bearer(old_token))
    device_token = rotated.json()["device_token"]
    good = event(device["device_id"], uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)
    for key in ("prompt", "command", "cwd", "file_path", "tool_output", RAW):
        attempt = {**good, "event_id": str(uuid.uuid4()), key: RAW}
        response = client.post(
            "/api/v1/events/batch",
            json={"schema_version": "1.0", "events": [attempt]},
            headers=bearer(device_token),
        )
        assert response.status_code == 422, key
    for field in ("activity", "event_type", "provider", "source", "occurred_at", "session_id"):
        response = client.post(
            "/api/v1/events/batch",
            json={"schema_version": "1.0", "events": [{**good, field: RAW}]},
            headers=bearer(device_token),
        )
        assert response.status_code == 422, field
    accepted = client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": [good]},
        headers=bearer(device_token),
    )
    assert accepted.json()["accepted_event_ids"] == [good["event_id"]]
    for path in (
        "/api/v1/office",
        "/api/v1/office/combined",
        "/api/v1/devices",
        "/api/v1/auth/sessions",
    ):
        client.get(path, headers=user)
    client.get("/api/v1/admin/audit", params={"limit": 200}, headers=admin)
    client.get("/api/v1/admin/users", headers=admin)

    # --- 검사 ------------------------------------------------------------------------
    always_secret = [
        PASSWORD,
        USER_PASSWORD,
        NEW_PASSWORD,
        WRONG_PASSWORD,
        SMTP_PASSWORD,
        OAUTH_SECRET,
        WEBHOOK_SECRET,
        TELEGRAM,
        RAW,
        BAD_BOOTSTRAP,
        bootstrap_token,
        admin_session,
        user_session,
        first_user_session,
        reset_token,
    ]
    issued_once = {
        old_token: {"POST /api/v1/devices/register"},
        device_token: {"POST /api/v1/devices/token/rotate"},
        invite_token: {f"POST /api/v1/teams/{team}/invitations"},
        code: {"POST /api/v1/devices/registration-codes"},
    }
    assert len(responses) > 50
    for key, response in responses:
        headers = "\n".join(
            f"{k}: {v}" for k, v in response.headers.items() if k.lower() != "set-cookie"
        )
        for secret in always_secret:
            assert secret not in response.text, (key, "body")
            assert secret not in headers, (key, "headers")
        for secret, allowed in issued_once.items():
            if key not in allowed:
                assert secret not in response.text, (key, "issued token")
        assert response.status_code < 500, (key, response.text)

    database = _scan_database(engine)
    log_text = caplog.text
    mail_text = "\n".join(m.body + m.subject for m in outbox.outbox)
    for secret in [*always_secret, *issued_once]:
        assert secret not in database, "database"
        assert secret not in log_text, "logs"
    for secret in (
        PASSWORD,
        USER_PASSWORD,
        NEW_PASSWORD,
        SMTP_PASSWORD,
        OAUTH_SECRET,
        WEBHOOK_SECRET,
        TELEGRAM,
        RAW,
    ):
        assert secret not in mail_text, "mail"

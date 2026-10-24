"""알림 관리자·공급자·채널 API 테스트(#70~#74). API·라우팅 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import base64
import os
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, select, update

from agent_office import secret_store
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app
from agent_office.notifications import manager, model, providers

DISCORD = "https://discord.com/api/webhooks/123/secret-webhook-token-abcdefghijklmnop"
BASE = "/api/v1/admin/notifications"


# --- 문구 -----------------------------------------------------------------------------


def test_render_is_plain_and_scrubbed() -> None:
    text = model.render(model.build("backup.failed", "pg_dump_failed"), "<@everyone> Office`*")
    assert text.startswith("[긴급] everyone Office")
    assert "원인 코드: pg_dump_failed" in text and "조치:" in text
    assert "@" not in text.split("\n")[0]
    assert model.build("backup.failed", "Free text!").code is None


@pytest.mark.parametrize(
    "secret",
    [
        "https://hooks.example/abc",
        "/home/alice/project/secret.txt",
        "C:\\Users\\alice\\key.pem",
        "x" * 40,
        "alice@example.com",
    ],
)
def test_scrub_removes_sensitive_looking_text(secret: str) -> None:
    assert secret not in model.scrub(f"before {secret} after")


# --- 공급자 설정 검사 -----------------------------------------------------------------------


@pytest.mark.parametrize(
    "url",
    [
        "http://discord.com/api/webhooks/1/x",
        "https://evil.example/api/webhooks/1/x",
        "https://discord.com.evil.example/api/webhooks/1/x",
        "https://127.0.0.1/api/webhooks/1/x",
        "https://user:pw@discord.com/api/webhooks/1/x",
        "https://discord.com/other/1",
    ],
)
def test_discord_rejects_non_webhook_urls(url: str) -> None:
    with pytest.raises(providers.ConfigError):
        providers.Discord().validate({}, url)


def test_provider_validation_accepts_good_values() -> None:
    assert providers.Discord().validate({}, DISCORD) == {}
    token = "123456789:" + "A" * 35
    assert providers.Telegram().validate({"chat_id": " -100123 "}, token) == {"chat_id": "-100123"}
    with pytest.raises(providers.ConfigError):
        providers.Telegram().validate({"chat_id": "1"}, "not-a-token")
    teams = "https://prod-01.westus.logic.azure.com:443/workflows/abc/triggers/manual"
    assert providers.TeamsWorkflows().validate({}, teams) == {}
    email = providers.EmailSmtp().validate({"recipients": ["B@x.co", "a@x.co", "a@x.co"]}, None)
    assert email == {"recipients": ["a@x.co", "b@x.co"]}
    with pytest.raises(providers.ConfigError):
        providers.EmailSmtp().validate({"recipients": [f"u{i}@x.co" for i in range(11)]}, None)


# --- 라우팅·억제·재시도 ----------------------------------------------------------------------


@pytest.fixture
def sent(monkeypatch: pytest.MonkeyPatch) -> Iterator[list[tuple[str, dict[str, Any]]]]:
    calls: list[tuple[str, dict[str, Any]]] = []
    monkeypatch.setattr(providers, "_post_json", lambda url, body: calls.append((url, body)))
    monkeypatch.setenv(
        secret_store.SECRET_KEY_ENV, base64.urlsafe_b64encode(os.urandom(32)).decode()
    )
    manager.reset()
    yield calls
    manager.reset()


def add_channel(engine: Engine, severities: list[str], enabled: bool = True) -> uuid.UUID:
    channel_id = uuid.uuid4()
    with engine.begin() as connection:
        connection.execute(
            schema.notification_channels.insert().values(
                id=channel_id,
                provider="discord",
                display_name="ops",
                enabled=enabled,
                secret_ciphertext=secret_store.encrypt(manager.secret_name(channel_id), DISCORD),
                subscribed_severities=severities,
            )
        )
    return channel_id


def notify(engine: Engine, kind: str, code: str | None = None) -> list[manager.DeliveryResult]:
    with engine.begin() as connection:
        return manager.notify(connection, kind, code, sleep=lambda _: None)


def test_routes_by_severity_and_enabled(engine: Engine, sent: list[Any]) -> None:
    critical = add_channel(engine, ["critical"])
    add_channel(engine, ["info"])
    add_channel(engine, ["critical"], enabled=False)

    results = notify(engine, "backup.failed", "pg_dump_failed")

    assert [r.channel_id for r in results] == [critical] and results[0].ok
    url, body = sent[0]
    assert url == DISCORD and body["allowed_mentions"] == {"parse": []}
    assert "pg_dump_failed" in body["content"] and DISCORD not in body["content"]


def test_dedup_and_recovery(engine: Engine, sent: list[Any]) -> None:
    add_channel(engine, ["critical", "info"])

    assert notify(engine, "backup.recovered") == []  # 열린 장애가 없으면 복구 알림 없음
    assert len(notify(engine, "backup.failed")) == 1
    assert notify(engine, "backup.failed") == []  # 1시간 안 반복 억제
    assert len(notify(engine, "backup.recovered")) == 1
    assert notify(engine, "backup.recovered") == []
    assert len(notify(engine, "backup.failed")) == 1  # 복구 뒤 새 장애는 다시 알림
    assert len(sent) == 3


def test_retry_and_failure_codes(
    engine: Engine, sent: list[Any], monkeypatch: pytest.MonkeyPatch
) -> None:
    add_channel(engine, ["critical", "high"])
    attempts: list[int] = []

    def flaky(url: str, body: dict[str, Any]) -> None:
        attempts.append(1)
        if len(attempts) < 3:
            raise providers.DeliveryError("http_503", True)

    monkeypatch.setattr(providers, "_post_json", flaky)
    assert notify(engine, "backup.failed")[0].ok and len(attempts) == 3

    def forbidden(url: str, body: dict[str, Any]) -> None:
        attempts.append(1)
        raise providers.DeliveryError("http_403", False)

    attempts.clear()
    manager.reset()
    monkeypatch.setattr(providers, "_post_json", forbidden)
    result = notify(engine, "restore_test.failed")
    assert result[0].error == "http_403" and len(attempts) == 1
    with engine.connect() as connection:
        logged = connection.execute(
            select(schema.audit_logs.c.details).where(
                schema.audit_logs.c.action == "notification.sent",
                schema.audit_logs.c.outcome == "failure",
            )
        ).scalar_one()
    assert logged == {"reason": "http_403"}


# --- API --------------------------------------------------------------------------------


@pytest.fixture
def client(engine: Engine, sent: list[Any]) -> Iterator[TestClient]:
    get_engine.cache_clear()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def admin_headers(engine: Engine, role: str = "SUPER_ADMIN") -> dict[str, str]:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users).where(schema.users.c.id == user_id).values(instance_role=role)
        )
    return {**session_cookie(session), **csrf(session)}


def test_channel_api_keeps_secrets_hidden(
    client: TestClient, engine: Engine, sent: list[Any]
) -> None:
    admin = admin_headers(engine)
    body = {
        "provider": "discord",
        "display_name": "운영",
        "secret": DISCORD,
        "severities": ["critical"],
    }
    created = client.post(BASE, json=body, headers=admin)
    assert created.status_code == 201
    channel = created.json()
    assert channel["secret_set"] is True and DISCORD not in created.text
    assert DISCORD not in client.get(BASE, headers=admin).text
    with engine.connect() as connection:
        blob = connection.execute(
            select(schema.notification_channels.c.secret_ciphertext)
        ).scalar_one()
    assert DISCORD.encode() not in bytes(blob)

    patched = client.patch(
        f"{BASE}/{channel['id']}", json={"severities": ["critical", "warning"]}, headers=admin
    )
    assert patched.json()["severities"] == ["critical", "warning"] and patched.json()["secret_set"]

    assert client.post(f"{BASE}/{channel['id']}/test", headers=admin).json() == {
        "ok": True,
        "error": None,
    }
    assert sent and "알림 채널 시험" in sent[-1][1]["content"]
    assert client.delete(f"{BASE}/{channel['id']}", headers=admin).status_code == 204
    assert client.get(BASE, headers=admin).json() == []


def test_channel_api_validation_and_permissions(
    client: TestClient, engine: Engine, monkeypatch: pytest.MonkeyPatch
) -> None:
    admin = admin_headers(engine)
    user = admin_headers(engine, "USER")
    bad = {"provider": "discord", "display_name": "x", "secret": "https://evil.example/hook"}
    assert client.post(BASE, json=bad, headers=admin).json()["detail"] == "webhook_url_not_allowed"
    assert client.get(BASE, headers=user).status_code == 403
    email = {"provider": "email_smtp", "display_name": "메일", "config": {"recipients": ["a@x.co"]}}
    assert client.post(BASE, json=email, headers=admin).status_code == 201
    monkeypatch.delenv(secret_store.SECRET_KEY_ENV)
    good = {"provider": "discord", "display_name": "x", "secret": DISCORD}
    assert client.post(BASE, json=good, headers=admin).status_code == 503

"""관리자 인스턴스 설정·비밀 암호화 테스트(#67). API 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import base64
import json
import os
import smtplib
from collections.abc import Iterator
from typing import Any

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, select, update

from agent_office import secret_store
from agent_office.api import admin_settings
from agent_office.auth import mailer
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app

KEY = base64.urlsafe_b64encode(os.urandom(32)).decode()
SECRET = "smtp-sentinel-password-7c1e"
BASE = "/api/v1/admin/settings"


# --- 암호화 -------------------------------------------------------------------------


def test_encrypt_round_trip_and_binding(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(secret_store.SECRET_KEY_ENV, KEY)
    blob = secret_store.encrypt("mail.smtp.password", SECRET)

    assert SECRET.encode() not in blob
    assert secret_store.decrypt("mail.smtp.password", blob) == SECRET
    assert secret_store.encrypt("mail.smtp.password", SECRET) != blob  # nonce
    # 다른 칸으로 옮긴 암호문·변조된 암호문·다른 키는 읽을 수 없다.
    with pytest.raises(secret_store.SecretUnreadable):
        secret_store.decrypt("oauth.github.client_secret", blob)
    with pytest.raises(secret_store.SecretUnreadable):
        secret_store.decrypt("mail.smtp.password", blob[:-1] + bytes([blob[-1] ^ 1]))
    monkeypatch.setenv(
        secret_store.SECRET_KEY_ENV, base64.urlsafe_b64encode(os.urandom(32)).decode()
    )
    with pytest.raises(secret_store.SecretUnreadable):
        secret_store.decrypt("mail.smtp.password", blob)


@pytest.mark.parametrize("value", ["", "short", "!!!not base64!!!"])
def test_missing_or_bad_key_disables_storage(monkeypatch: pytest.MonkeyPatch, value: str) -> None:
    monkeypatch.setenv(secret_store.SECRET_KEY_ENV, value)
    assert not secret_store.available()
    with pytest.raises(secret_store.SecretKeyMissing):
        secret_store.encrypt("x", "y")


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("https://Office.Example.com/", "https://office.example.com"),
        ("http://192.168.0.10:8080", "http://192.168.0.10:8080"),
        ("ftp://office.example.com", None),
        ("https://office.example.com/path", None),
        ("https://user:pw@office.example.com", None),
        ("https://office.example.com/?q=1", None),
        ("office.example.com", None),
    ],
)
def test_public_url_normalization(raw: str, expected: str | None) -> None:
    assert admin_settings.normalize_public_url(raw) == expected


# --- API ---------------------------------------------------------------------------


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setenv(secret_store.SECRET_KEY_ENV, KEY)
    for name in [n for n in os.environ if n.startswith(mailer.SMTP_ENV_PREFIX)]:
        monkeypatch.delenv(name)
    get_engine.cache_clear()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


@pytest.fixture
def admin(engine: Engine) -> dict[str, str]:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users)
            .where(schema.users.c.id == user_id)
            .values(instance_role="SUPER_ADMIN", email="admin@example.com")
        )
    return {**session_cookie(session), **csrf(session)}


def put(client: TestClient, headers: dict[str, str], path: str, body: Any) -> httpx.Response:
    return client.put(f"{BASE}{path}", json=body, headers=headers)


SMTP = {
    "host": "smtp.example.com",
    "port": 587,
    "username": "mailer",
    "sender": "Office@Example.com",
    "starttls": True,
    "password": SECRET,
}


def test_only_admins_can_read_or_write(client: TestClient, engine: Engine) -> None:
    _, session = sign_in(engine)
    user = {**session_cookie(session), **csrf(session)}

    assert client.get(BASE, headers=user).status_code == 403
    assert put(client, user, "/signup-policy", {"policy": "CLOSED"}).status_code == 403
    assert client.get(BASE).status_code == 401


def test_writes_require_csrf(client: TestClient, admin: dict[str, str]) -> None:
    no_csrf = {"Cookie": admin["Cookie"]}
    assert put(client, no_csrf, "/signup-policy", {"policy": "CLOSED"}).status_code == 403


def test_site_and_signup_policy(client: TestClient, admin: dict[str, str]) -> None:
    view = client.get(BASE, headers=admin).json()
    assert view["site"] == {"service_name": "Agent Office", "public_url": None}
    assert view["signup_policy"] == "CLOSED" and view["secret_storage_available"] is True

    site = put(
        client, admin, "/site", {"service_name": " 우리 팀 ", "public_url": "https://A.example/"}
    )
    assert site.json()["site"] == {"service_name": "우리 팀", "public_url": "https://a.example"}
    bad = put(client, admin, "/site", {"service_name": "x", "public_url": "javascript:alert(1)"})
    assert bad.status_code == 400

    policy = put(client, admin, "/signup-policy", {"policy": "PUBLIC_VERIFIED_EMAIL"})
    assert policy.json()["signup_policy"] == "PUBLIC_VERIFIED_EMAIL"
    assert put(client, admin, "/signup-policy", {"policy": "OPEN"}).status_code == 422


def test_smtp_password_is_encrypted_and_never_returned(
    client: TestClient, admin: dict[str, str], engine: Engine
) -> None:
    saved = put(client, admin, "/smtp", SMTP)
    assert saved.status_code == 200
    body = saved.json()
    assert body["smtp"]["password_set"] is True and body["smtp"]["sender"] == "office@example.com"
    assert body["smtp_source"] == "settings"
    assert SECRET not in json.dumps(body)
    assert SECRET not in client.get(BASE, headers=admin).text

    with engine.connect() as connection:
        settings = json.dumps(
            [list(r) for r in connection.execute(select(schema.instance_settings)).all()],
            default=str,
        )
        blob = connection.execute(select(schema.instance_secrets.c.ciphertext)).scalar_one()
        assert mailer.smtp_settings(connection) == mailer.SmtpSettings(
            "smtp.example.com", 587, "mailer", SECRET, "office@example.com", True
        )
    assert SECRET not in settings and SECRET.encode() not in bytes(blob)

    # 암호를 보내지 않으면 유지, 빈 문자열이면 삭제.
    keep = put(client, admin, "/smtp", {**SMTP, "password": None})
    assert keep.json()["smtp"]["password_set"] is True
    cleared = put(client, admin, "/smtp", {**SMTP, "password": ""})
    assert cleared.json()["smtp"]["password_set"] is False

    assert client.delete(f"{BASE}/smtp", headers=admin).status_code == 204
    assert client.get(BASE, headers=admin).json()["smtp"] is None


def test_secret_requires_key(
    client: TestClient, admin: dict[str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delenv(secret_store.SECRET_KEY_ENV)
    response = put(client, admin, "/smtp", SMTP)
    assert response.status_code == 503 and response.json()["detail"] == "secret_key_missing"
    # 암호 없는 SMTP(내부 릴레이)는 키 없이도 저장할 수 있다.
    assert put(client, admin, "/smtp", {**SMTP, "password": None}).status_code == 200


def test_smtp_validation(client: TestClient, admin: dict[str, str]) -> None:
    assert put(client, admin, "/smtp", {**SMTP, "sender": "nope"}).status_code == 400
    assert put(client, admin, "/smtp", {**SMTP, "host": "evil.com/x"}).status_code == 400
    assert put(client, admin, "/smtp", {**SMTP, "port": 0}).status_code == 422
    # 한글 실명 같은 비ASCII 사용자 이름은 SMTP 로그인을 깨므로 저장 단계에서 거부한다(#198).
    korean = put(client, admin, "/smtp", {**SMTP, "username": "박기찬"})
    assert korean.status_code == 400
    assert "smtp_username_not_ascii" in korean.text


class FakeSMTP:
    sent: list[Any] = []
    fail_login = False

    def __init__(self, host: str, port: int, timeout: float) -> None:
        self.host = host

    def __enter__(self) -> FakeSMTP:
        return self

    def __exit__(self, *_: object) -> None:
        return None

    def starttls(self, context: object) -> None:
        return None

    def login(self, username: str, password: str) -> None:
        # 실제 smtplib처럼 AUTH 문자열을 ASCII로 인코딩한다.
        (username + password).encode("ascii")
        if FakeSMTP.fail_login:
            raise smtplib.SMTPAuthenticationError(535, b"bad")

    def send_message(self, message: Any) -> None:
        FakeSMTP.sent.append(message)


def test_smtp_test_sends_to_admin_and_reports_error_codes(
    client: TestClient, admin: dict[str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(smtplib, "SMTP", FakeSMTP)
    FakeSMTP.sent = []
    put(client, admin, "/smtp", SMTP)

    ok = client.post(f"{BASE}/smtp/test", headers=admin)
    assert ok.json() == {"ok": True, "error": None}
    assert FakeSMTP.sent[-1]["To"] == "admin@example.com"

    FakeSMTP.fail_login = True
    try:
        failed = client.post(f"{BASE}/smtp/test", headers=admin)
    finally:
        FakeSMTP.fail_login = False
    assert failed.json() == {"ok": False, "error": "auth_failed"}
    assert SECRET not in failed.text

    # 비ASCII 암호는 500이 아니라 원인 코드로 돌려준다(#198).
    put(client, admin, "/smtp", {**SMTP, "password": "앱비밀번호"})
    encoded = client.post(f"{BASE}/smtp/test", headers=admin)
    assert encoded.status_code == 200
    assert encoded.json() == {"ok": False, "error": "non_ascii_credentials"}


def test_oauth_settings(client: TestClient, admin: dict[str, str]) -> None:
    incomplete = put(client, admin, "/oauth/github", {"enabled": True, "client_id": "abc"})
    assert incomplete.status_code == 400

    body = {"enabled": True, "client_id": "abc", "client_secret": SECRET}
    saved = put(client, admin, "/oauth/github", body)
    assert saved.json()["oauth"]["github"] == {
        "enabled": True,
        "client_id": "abc",
        "client_secret_set": True,
    }
    assert SECRET not in saved.text
    assert put(client, admin, "/oauth/twitter", body).status_code == 422


def test_backup_actions_require_key_and_record_status(
    client: TestClient, admin: dict[str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
    from agent_office import backup

    monkeypatch.delenv(backup.BACKUP_KEY_ENV, raising=False)
    missing = client.post("/api/v1/admin/backup/run", headers=admin)
    assert missing.status_code == 503 and missing.json()["detail"] == "backup_key_missing"

    monkeypatch.setenv(backup.BACKUP_KEY_ENV, base64.urlsafe_b64encode(os.urandom(32)).decode())

    def fail() -> backup.BackupInfo:
        raise backup.BackupError("pg_dump_failed")

    monkeypatch.setattr(backup, "create_backup", fail)
    result = client.post("/api/v1/admin/backup/run", headers=admin).json()
    assert result == {"ok": False, "error": "pg_dump_failed", "name": None, "size_bytes": None}
    status = client.get("/api/v1/admin/metrics", headers=admin).json()["backup"]
    assert status["configured"] is True and status["last_backup_ok"] is False
    assert status["last_error"] == "pg_dump_failed"

    monkeypatch.setenv(backup.BACKUP_DIR_ENV, "/nonexistent-backups")
    test = client.post("/api/v1/admin/backup/restore-test", headers=admin).json()
    assert test["ok"] is False and test["error"] == "no_backup"
    assert client.post("/api/v1/admin/backup/delete", headers=admin).status_code == 422
    assert (
        client.post("/api/v1/admin/backup/run", headers={"Cookie": admin["Cookie"]}).status_code
        == 403
    )

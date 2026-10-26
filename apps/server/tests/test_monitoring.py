"""준비 상태·운영 지표·외부 Heartbeat 테스트(#59). API 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import http.server
import json
import threading
import uuid
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from helpers import csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, text, update
from test_office_live import send_event

from agent_office import monitoring
from agent_office.api import events as events_api
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


def test_ready_reports_database_and_migrations(client: TestClient, engine: Engine) -> None:
    response = client.get("/api/v1/system/ready")
    assert response.status_code == 200
    assert response.json() == {"ready": True, "checks": {"database": "ok", "migrations": "ok"}}

    with engine.begin() as connection:
        connection.execute(text("UPDATE alembic_version SET version_num = '0001'"))
    outdated = client.get("/api/v1/system/ready")
    assert outdated.status_code == 503
    assert outdated.json()["checks"]["migrations"] == "outdated"


def test_expected_revision_is_the_package_head() -> None:
    assert monitoring.expected_revision() == "0008"


def admin_headers(engine: Engine, role: str = "SUPER_ADMIN") -> dict[str, str]:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users).where(schema.users.c.id == user_id).values(instance_role=role)
        )
    return {**session_cookie(session), **csrf(session)}


def test_metrics_for_admins_only_without_identifiers(
    client: TestClient, engine: Engine, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv(monitoring.HEARTBEAT_URL_ENV, "https://hc.example/ping/secret-uuid")
    _, session = sign_in(engine)
    device = register(client, new_code(client, session), name="private-pc-name")
    send_event(client, device, uuid.uuid4(), uuid.uuid4(), "session.started", minute=29)

    assert (
        client.get("/api/v1/admin/metrics", headers=admin_headers(engine, "USER")).status_code
        == 403
    )
    response = client.get("/api/v1/admin/metrics", headers=admin_headers(engine))
    assert response.status_code == 200
    body = response.json()
    assert body["collection"]["devices"] == 1 and body["collection"]["events_last_hour"] == 1
    assert body["collection"]["open_sessions"] == 1
    assert body["collection"]["ingest_lag_p95_seconds"] >= 0
    assert body["database"]["migration"] == "0008" and body["database"]["size_bytes"] > 0
    assert body["host"]["disk"]["total_bytes"] > 0
    assert body["external_heartbeat"]["configured"] is True
    dumped = json.dumps(body)
    for secret in ("private-pc-name", "secret-uuid", device["device_id"], device["device_token"]):
        assert secret not in dumped


def test_heartbeat_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(monitoring.HEARTBEAT_URL_ENV, "ftp://nope")
    assert monitoring.heartbeat_url() is None
    monkeypatch.setenv(monitoring.HEARTBEAT_URL_ENV, " https://hc.example/x ")
    assert monitoring.heartbeat_url() == "https://hc.example/x"
    monkeypatch.setenv(monitoring.HEARTBEAT_INTERVAL_ENV, "5")
    assert monitoring.heartbeat_interval() == 30
    monkeypatch.setenv(monitoring.HEARTBEAT_INTERVAL_ENV, "bad")
    assert monitoring.heartbeat_interval() == 60


def test_ping_reports_success_and_failure() -> None:
    hits: list[str] = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            hits.append(self.path)
            self.send_response(200 if self.path == "/ok" else 500)
            self.end_headers()

        def log_message(self, *_: object) -> None:
            return None

    server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        base = f"http://127.0.0.1:{server.server_address[1]}"
        assert monitoring._ping(f"{base}/ok") is True
        assert monitoring._ping(f"{base}/fail") is False
        assert monitoring._ping("http://127.0.0.1:9/closed") is False
    finally:
        server.shutdown()
    assert hits == ["/ok", "/fail"]


def test_security_checks_and_deployment(
    client: TestClient, engine: Engine, monkeypatch: pytest.MonkeyPatch
) -> None:
    import base64
    import os

    for name in (
        "AGENT_OFFICE_SECRET_KEY",
        "AGENT_OFFICE_BACKUP_KEY",
        "AGENT_OFFICE_BACKUP_OFFSITE_DIR",
    ):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("AGENT_OFFICE_COOKIE_SECURE", "false")
    monkeypatch.setenv("AGENT_OFFICE_ALLOWED_ORIGINS", "*")
    monkeypatch.setenv("AGENT_OFFICE_PUBLIC_URL", "http://office.example")
    monkeypatch.setenv("AGENT_OFFICE_VERSION", "1.2.3")
    headers = admin_headers(engine)

    body = client.get("/api/v1/admin/metrics", headers=headers).json()
    codes = {c["code"] for c in body["security"]}
    assert {
        "cookie_not_secure",
        "allowed_origins_wildcard",
        "public_url_http",
        "secret_key_missing",
        "backup_disabled",
        "setup_incomplete",
    } <= codes
    assert body["deployment"]["version"] == "1.2.3"
    assert body["deployment"]["migration_expected"] == "0008"
    assert "office.example" not in json.dumps(body["security"])

    key = base64.urlsafe_b64encode(os.urandom(32)).decode()
    monkeypatch.setenv("AGENT_OFFICE_COOKIE_SECURE", "true")
    monkeypatch.setenv("AGENT_OFFICE_ALLOWED_ORIGINS", "")
    monkeypatch.setenv("AGENT_OFFICE_PUBLIC_URL", "https://office.example")
    monkeypatch.setenv("AGENT_OFFICE_SECRET_KEY", key)
    monkeypatch.setenv("AGENT_OFFICE_BACKUP_KEY", key)
    monkeypatch.setenv("AGENT_OFFICE_BACKUP_OFFSITE_DIR", "/backups-offsite")
    codes = {
        c["code"] for c in client.get("/api/v1/admin/metrics", headers=headers).json()["security"]
    }
    assert codes == {"setup_incomplete"}

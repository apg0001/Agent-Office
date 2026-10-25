"""관리자 사용자 관리·감사 로그·가입 남용 방지 테스트(#28). PostgreSQL이 필요하다."""

from __future__ import annotations

import re
import uuid
from collections.abc import Iterator
from dataclasses import dataclass, field

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, func, select, text, update

from agent_office.api import accounts as accounts_api
from agent_office.auth import accounts, mailer
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app

ORIGIN = {"origin": "http://testserver"}


@dataclass
class Outbox:
    configured: bool = True
    sent: list[mailer.OutgoingMail] = field(default_factory=list)

    def send(self, mail: mailer.OutgoingMail) -> bool:
        self.sent.append(mail)
        return True


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setenv(mailer.PUBLIC_URL_ENV, "https://office.example")
    get_engine.cache_clear()
    for limiter in accounts_api.LIMITERS:
        limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()
    mailer.set_mailer(None)


def person(engine: Engine, role: str = "USER") -> tuple[uuid.UUID, dict[str, str]]:
    user_id, session = sign_in(engine)
    with engine.begin() as connection:
        connection.execute(
            update(schema.users)
            .where(schema.users.c.id == user_id)
            .values(instance_role=role, email_verified_at=func.now())
        )
    return user_id, {**session_cookie(session), **csrf(session)}


def policy(engine: Engine, value: str) -> None:
    with engine.begin() as connection:
        connection.execute(
            text(
                "INSERT INTO instance_settings (key, value) VALUES (:k, to_jsonb(CAST(:v AS text)))"
                " ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value"
            ),
            {"k": accounts.SIGNUP_POLICY_KEY, "v": value},
        )


def signup(client: TestClient, email: str, **extra: str) -> httpx.Response:
    body = {"email": email, "password": "long enough pass", "display_name": "N", **extra}
    return client.post("/api/v1/auth/signup", json=body, headers=ORIGIN)


def test_admin_approval_flow(client: TestClient, engine: Engine) -> None:
    outbox = Outbox()
    mailer.set_mailer(outbox)
    policy(engine, "ADMIN_APPROVAL")
    _, admin = person(engine, "ADMIN")
    assert signup(client, "new@example.com").status_code == 202

    # 이메일 확인 전에는 승인 대기 목록에 들어가지 않고 승인도 안 된다.
    page = client.get("/api/v1/admin/users?status=pending", headers=admin).json()
    new = next(u for u in page["users"] if u["email"] == "new@example.com")
    assert page["pending_approval"] == 0
    assert client.post(f"/api/v1/admin/users/{new['id']}/approve", headers=admin).status_code == 409

    token = re.search(r"#token=(\S+)", outbox.sent[-1].body)
    assert token
    client.post("/api/v1/auth/verify-email", json={"token": token.group(1)}, headers=ORIGIN)
    assert client.get("/api/v1/admin/users", headers=admin).json()["pending_approval"] == 1

    approved = client.post(f"/api/v1/admin/users/{new['id']}/approve", headers=admin)
    assert approved.status_code == 200 and approved.json()["status"] == "active"
    login = client.post(
        "/api/v1/auth/login",
        json={"email": "new@example.com", "password": "long enough pass"},
        headers=ORIGIN,
    )
    assert login.status_code == 200


def test_disable_revokes_sessions_and_enable_restores(client: TestClient, engine: Engine) -> None:
    _, admin = person(engine, "ADMIN")
    target_id, target = person(engine)

    disabled = client.post(f"/api/v1/admin/users/{target_id}/disable", headers=admin)
    assert disabled.json()["status"] == "disabled"
    assert client.get("/api/v1/auth/session", headers=target).status_code == 401
    enabled = client.post(f"/api/v1/admin/users/{target_id}/enable", headers=admin)
    assert enabled.json()["status"] == "active"


def test_role_rules_and_last_super_admin(client: TestClient, engine: Engine) -> None:
    root_id, root = person(engine, "SUPER_ADMIN")
    admin_id, admin = person(engine, "ADMIN")
    user_id, user = person(engine)

    assert client.get("/api/v1/admin/users", headers=user).status_code == 403
    # ADMIN은 역할을 바꾸지 못한다.
    body = {"instance_role": "SUPER_ADMIN"}
    assert (
        client.put(f"/api/v1/admin/users/{admin_id}/role", json=body, headers=admin).status_code
        == 403
    )
    # 자기 계정 비활성화 금지, 마지막 SUPER_ADMIN 강등 금지.
    assert client.post(f"/api/v1/admin/users/{root_id}/disable", headers=root).status_code == 409
    demote = client.put(
        f"/api/v1/admin/users/{root_id}/role", json={"instance_role": "USER"}, headers=root
    )
    assert demote.status_code == 409 and demote.json()["detail"] == "last_super_admin"
    promoted = client.put(f"/api/v1/admin/users/{user_id}/role", json=body, headers=root)
    assert promoted.json()["instance_role"] == "SUPER_ADMIN"
    # 이제 다른 SUPER_ADMIN이 있으니 강등할 수 있다.
    again = client.put(
        f"/api/v1/admin/users/{root_id}/role", json={"instance_role": "ADMIN"}, headers=root
    )
    assert again.status_code == 200


def test_audit_log_paging_and_filter(client: TestClient, engine: Engine) -> None:
    _, admin = person(engine, "ADMIN")
    targets = [person(engine)[0] for _ in range(3)]
    for target in targets:
        client.post(f"/api/v1/admin/users/{target}/disable", headers=admin)

    first = client.get("/api/v1/admin/audit?limit=2&action=user.", headers=admin).json()
    assert [e["action"] for e in first["entries"]] == ["user.disabled", "user.disabled"]
    assert first["entries"][0]["actor_display_name"] == "synthetic"
    second = client.get(
        f"/api/v1/admin/audit?limit=2&action=user.&before={first['next_before']}", headers=admin
    ).json()
    assert len(second["entries"]) == 1 and second["next_before"] is None
    assert client.get("/api/v1/admin/audit?action=DROP;", headers=admin).status_code == 422


def test_signup_abuse_protections(client: TestClient, engine: Engine) -> None:
    mailer.set_mailer(Outbox())
    policy(engine, "PUBLIC_VERIFIED_EMAIL")
    _, admin = person(engine, "SUPER_ADMIN")

    # 숨은 입력란을 채운 봇: 같은 응답, 계정은 만들지 않음.
    assert signup(client, "bot@example.com", website="http://spam").status_code == 202
    with engine.connect() as connection:
        bots = connection.execute(
            select(func.count())
            .select_from(schema.users)
            .where(schema.users.c.email == "bot@example.com")
        ).scalar_one()
    assert bots == 0

    blocked = client.put(
        "/api/v1/admin/settings/blocked-domains",
        json={"domains": ["Mailinator.com", "@trash.example"]},
        headers=admin,
    )
    assert blocked.json()["blocked_email_domains"] == ["mailinator.com", "trash.example"]
    bad = client.put(
        "/api/v1/admin/settings/blocked-domains", json={"domains": ["not a domain"]}, headers=admin
    )
    assert bad.status_code == 400
    for email in ("a@mailinator.com", "b@sub.trash.example"):
        response = signup(client, email)
        assert response.status_code == 400 and response.json()["detail"] == "email_domain_blocked"
    assert signup(client, "ok@example.com").status_code == 202


def test_global_signup_limit(client: TestClient, engine: Engine) -> None:
    mailer.set_mailer(Outbox())
    policy(engine, "PUBLIC_VERIFIED_EMAIL")
    accounts_api.signup_limiter.limit = 1000
    try:
        codes = [signup(client, f"u{i}@example.com").status_code for i in range(31)]
    finally:
        accounts_api.signup_limiter.limit = 5
    assert codes[:30] == [202] * 30 and codes[30] == 429

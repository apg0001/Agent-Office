"""이메일 가입·검증·로그인·비밀번호 복구 테스트(#23). API 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import json
import re
from collections.abc import Iterator
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie
from sqlalchemy import Engine, Row, select, text, update

from agent_office.api import accounts as accounts_api
from agent_office.auth import accounts, mailer, passwords
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app

PASSWORD = "correct horse battery"
JSON_ORIGIN = {"origin": "http://testserver"}


# --- 순수 규칙 ---------------------------------------------------------------------


def test_password_policy_and_hashing() -> None:
    assert passwords.password_problem("short") == "password_too_short"
    assert passwords.password_problem("x" * 129) == "password_too_long"
    assert passwords.password_problem(PASSWORD) is None

    encoded = passwords.hash_password(PASSWORD)
    assert encoded.startswith("$argon2id$") and PASSWORD not in encoded
    assert passwords.verify_password(encoded, PASSWORD)
    assert not passwords.verify_password(encoded, PASSWORD + "!")
    assert not passwords.verify_password(None, PASSWORD)
    assert not passwords.verify_password("not-a-hash", PASSWORD)
    assert not passwords.needs_rehash(encoded)


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("  Alice@Example.COM ", "alice@example.com"),
        ("no-at-sign", None),
        ("a@localhost", None),
        ("a@@b.com", None),
        ("a b@c.com", None),
        ("x" * 65 + "@example.com", None),
    ],
)
def test_normalize_email(raw: str, expected: str | None) -> None:
    assert accounts.normalize_email(raw) == expected


# --- API ---------------------------------------------------------------------------


@dataclass
class FakeMailer:
    configured: bool = True
    outbox: list[mailer.OutgoingMail] = field(default_factory=list)

    def send(self, mail: mailer.OutgoingMail) -> bool:
        self.outbox.append(mail)
        return True

    def token(self, index: int = -1) -> str:
        found = re.search(r"#token=([A-Za-z0-9_-]+)", self.outbox[index].body)
        assert found
        return found.group(1)


@pytest.fixture
def outbox(monkeypatch: pytest.MonkeyPatch) -> Iterator[FakeMailer]:
    fake = FakeMailer()
    monkeypatch.setenv(mailer.PUBLIC_URL_ENV, "https://office.example")
    mailer.set_mailer(fake)
    yield fake
    mailer.set_mailer(None)


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    for limiter in accounts_api.LIMITERS:
        limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def set_policy(engine: Engine, policy: str) -> None:
    with engine.begin() as connection:
        connection.execute(
            text(
                "INSERT INTO instance_settings (key, value) VALUES (:k, to_jsonb(CAST(:v AS text)))"
                " ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value"
            ),
            {"k": accounts.SIGNUP_POLICY_KEY, "v": policy},
        )


def post(client: TestClient, path: str, body: dict[str, str]) -> httpx.Response:
    return client.post(f"/api/v1/auth/{path}", json=body, headers=JSON_ORIGIN)


def signup(client: TestClient, email: str = "alice@example.com") -> httpx.Response:
    return post(client, "signup", {"email": email, "password": PASSWORD, "display_name": "Alice"})


def user_row(engine: Engine, email: str = "alice@example.com") -> Row[Any]:
    with engine.connect() as connection:
        return connection.execute(select(schema.users).where(schema.users.c.email == email)).one()


def test_signup_verify_login_flow(client: TestClient, engine: Engine, outbox: FakeMailer) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")

    response = signup(client, "  Alice@Example.com ")
    assert response.status_code == 202
    row = user_row(engine)
    assert row.status == "pending" and row.email_verified_at is None
    assert PASSWORD not in row.password_hash
    assert outbox.outbox[-1].to == "alice@example.com"
    assert "https://office.example/verify-email#token=" in outbox.outbox[-1].body

    login = {"email": "alice@example.com", "password": PASSWORD}
    assert post(client, "login", login).json()["detail"] == "email_not_verified"

    token = outbox.token()
    assert post(client, "verify-email", {"token": token}).status_code == 204
    # 일회용: 두 번째 사용은 실패한다.
    assert post(client, "verify-email", {"token": token}).status_code == 400

    response = post(client, "login", login)
    assert response.status_code == 200
    body = response.json()
    cookie = response.cookies.get(SESSION_COOKIE)
    assert cookie and body["csrf_token"] and body["user"]["display_name"] == "Alice"
    assert PASSWORD not in json.dumps(body)
    me = client.get("/api/v1/auth/session", headers=session_cookie(cookie))
    assert me.status_code == 200 and me.json()["csrf_token"] == body["csrf_token"]


def test_login_failures_do_not_reveal_accounts(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    signup(client)
    post(client, "verify-email", {"token": outbox.token()})

    wrong = post(client, "login", {"email": "alice@example.com", "password": "wrong password!"})
    missing = post(client, "login", {"email": "nobody@example.com", "password": PASSWORD})
    assert wrong.status_code == missing.status_code == 401
    assert wrong.json() == missing.json() == {"detail": "invalid_credentials"}

    with engine.begin() as connection:
        connection.execute(
            update(schema.users)
            .where(schema.users.c.email == "alice@example.com")
            .values(status="disabled")
        )
    disabled = post(client, "login", {"email": "alice@example.com", "password": PASSWORD})
    assert disabled.json() == {"detail": "invalid_credentials"}


def test_duplicate_signup_looks_the_same(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    first = signup(client)
    second = signup(client)

    assert first.status_code == second.status_code == 202
    assert first.json() == second.json()
    # 두 번째 메일은 토큰 없는 안내다.
    assert "#token=" not in outbox.outbox[-1].body
    with engine.connect() as connection:
        count = connection.execute(text("SELECT count(*) FROM users")).scalar_one()
    assert count == 1


@pytest.mark.parametrize(
    ("policy", "detail"), [("CLOSED", "signup_closed"), ("INVITE_ONLY", "invite_required")]
)
def test_signup_respects_policy(
    client: TestClient, engine: Engine, outbox: FakeMailer, policy: str, detail: str
) -> None:
    set_policy(engine, policy)
    response = signup(client)
    assert response.status_code == 403 and response.json()["detail"] == detail


def test_signup_closed_by_default_and_without_mail(
    client: TestClient, engine: Engine, monkeypatch: pytest.MonkeyPatch
) -> None:
    assert signup(client).json()["detail"] == "signup_closed"

    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    monkeypatch.delenv(mailer.PUBLIC_URL_ENV, raising=False)
    mailer.set_mailer(mailer.SmtpMailer(None, None))
    try:
        response = signup(client)
        assert response.status_code == 503
        assert response.json()["detail"] == "mail_not_configured"
        with engine.connect() as connection:
            assert connection.execute(text("SELECT count(*) FROM users")).scalar_one() == 0
        reset = post(client, "password-reset/request", {"email": "alice@example.com"})
        assert reset.status_code == 503
    finally:
        mailer.set_mailer(None)


def test_admin_approval_keeps_verified_user_pending(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    set_policy(engine, "ADMIN_APPROVAL")
    signup(client)
    post(client, "verify-email", {"token": outbox.token()})

    row = user_row(engine)
    assert row.status == "pending" and row.email_verified_at is not None
    login = post(client, "login", {"email": "alice@example.com", "password": PASSWORD})
    assert login.json()["detail"] == "approval_pending"


def test_password_reset_flow_revokes_sessions(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    signup(client)
    post(client, "verify-email", {"token": outbox.token()})
    old = post(client, "login", {"email": "alice@example.com", "password": PASSWORD})
    old_cookie = old.cookies.get(SESSION_COOKIE)

    unknown = post(client, "password-reset/request", {"email": "nobody@example.com"})
    known = post(client, "password-reset/request", {"email": "alice@example.com"})
    assert unknown.status_code == known.status_code == 202
    assert unknown.json() == known.json()
    assert outbox.outbox[-1].to == "alice@example.com"
    token = outbox.token()

    assert (
        post(client, "password-reset/confirm", {"token": token, "password": "short"}).json()[
            "detail"
        ]
        == "password_too_short"
    )
    new_password = "a brand new passphrase"
    confirm = post(client, "password-reset/confirm", {"token": token, "password": new_password})
    assert confirm.status_code == 204
    reused = post(client, "password-reset/confirm", {"token": token, "password": new_password})
    assert reused.status_code == 400

    assert client.get("/api/v1/auth/session", headers=session_cookie(old_cookie)).status_code == 401
    assert (
        post(client, "login", {"email": "alice@example.com", "password": PASSWORD}).status_code
        == 401
    )
    assert (
        post(client, "login", {"email": "alice@example.com", "password": new_password}).status_code
        == 200
    )


def test_expired_token_is_rejected(client: TestClient, engine: Engine, outbox: FakeMailer) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    signup(client)
    with engine.begin() as connection:
        connection.execute(
            text(
                "UPDATE email_verifications SET created_at = now() - interval '2 days',"
                " expires_at = now() - interval '1 day'"
            )
        )
    assert post(client, "verify-email", {"token": outbox.token()}).status_code == 400


def test_cross_site_and_form_posts_are_rejected(client: TestClient, engine: Engine) -> None:
    body = {"email": "alice@example.com", "password": PASSWORD}
    evil = client.post("/api/v1/auth/login", json=body, headers={"origin": "https://evil.example"})
    form = client.post(
        "/api/v1/auth/login",
        content=json.dumps(body),
        headers={"content-type": "text/plain", "origin": "http://testserver"},
    )
    assert evil.status_code == form.status_code == 403
    assert evil.json()["detail"] == "cross_site_rejected"


def test_login_is_rate_limited_per_email(client: TestClient, engine: Engine) -> None:
    body = {"email": "alice@example.com", "password": "wrong password!"}
    codes = [post(client, "login", body).status_code for _ in range(11)]
    assert codes[:10] == [401] * 10
    assert codes[10] == 429


def test_login_replaces_existing_session(
    client: TestClient, engine: Engine, outbox: FakeMailer
) -> None:
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    signup(client)
    post(client, "verify-email", {"token": outbox.token()})
    body = {"email": "alice@example.com", "password": PASSWORD}
    first = post(client, "login", body).cookies.get(SESSION_COOKIE)

    second = client.post(
        "/api/v1/auth/login", json=body, headers={**JSON_ORIGIN, **session_cookie(first)}
    )

    assert second.status_code == 200
    assert client.get("/api/v1/auth/session", headers=session_cookie(first)).status_code == 401
    new = second.cookies.get(SESSION_COOKIE)
    assert (
        client.post("/api/v1/auth/logout", headers={**session_cookie(new), **csrf(new)}).status_code
        == 204
    )

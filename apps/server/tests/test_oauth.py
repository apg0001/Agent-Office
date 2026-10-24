"""Google/GitHub OAuth 로그인·가입·연결 테스트(#24). 제공자는 가짜 HTTP 함수로 대신한다.

값은 모두 합성이며 실제 Google·GitHub에는 접속하지 않는다.
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
import urllib.parse
from collections.abc import Iterator
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, func, select, text, update

from agent_office import instance, retention, secret_store
from agent_office.api import accounts as accounts_api
from agent_office.api import oauth as oauth_api
from agent_office.auth import accounts, mailer, oauth
from agent_office.auth.sessions import COOKIE_SECURE_ENV, SESSION_COOKIE, issue_session
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app

PUBLIC = "https://office.example"
CLIENT_SECRET = "synthetic-client-secret-value"
ACCESS_TOKEN = "synthetic-access-token-value"


# --- 순수 규칙 ---------------------------------------------------------------------


def test_pkce_challenge_matches_rfc7636_example() -> None:
    # RFC 7636 부록 B의 예시 값.
    verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
    assert oauth.code_challenge(verifier) == "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
    fresh = oauth.new_code_verifier()
    assert 43 <= len(fresh) <= 128


def test_authorize_url_has_state_pkce_and_minimal_scope() -> None:
    config = oauth.ClientConfig(client_id="cid", client_secret=CLIENT_SECRET)
    for provider, scope in (("google", "openid email"), ("github", "user:email")):
        url = oauth.authorize_url(
            provider, config, callback="https://o.example/cb", state="st", verifier="v" * 43
        )
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
        assert query["state"] == ["st"] and query["scope"] == [scope]
        assert query["code_challenge_method"] == ["S256"]
        assert query["code_challenge"] == [oauth.code_challenge("v" * 43)]
        assert CLIENT_SECRET not in url and "v" * 43 not in url


# --- 가짜 제공자 -------------------------------------------------------------------


@dataclass
class FakeProvider:
    google_sub: str = "google-sub-1"
    github_id: int = 4242
    email: str | None = "oauth.user@example.com"
    email_verified: bool = True
    token_status: int = 200
    calls: list[tuple[str, str, dict[str, str], bytes | None]] = field(default_factory=list)

    def __call__(
        self, method: str, url: str, headers: dict[str, str], body: bytes | None
    ) -> tuple[int, Any]:
        self.calls.append((method, url, headers, body))
        if url in (oauth.PROVIDERS["google"].token_url, oauth.PROVIDERS["github"].token_url):
            if self.token_status != 200:
                return self.token_status, {"error": "bad_verification_code"}
            return 200, {"access_token": ACCESS_TOKEN, "token_type": "bearer"}
        assert headers["Authorization"] == f"Bearer {ACCESS_TOKEN}"
        if url == oauth.GOOGLE_USERINFO_URL:
            return 200, {
                "sub": self.google_sub,
                "email": self.email,
                "email_verified": self.email_verified,
                "name": "Profile Name Not Stored",
            }
        if url == oauth.GITHUB_USER_URL:
            return 200, {"id": self.github_id, "login": "octo-synthetic", "name": "Not Stored"}
        if url == oauth.GITHUB_EMAILS_URL:
            return 200, [
                {"email": "secondary@example.com", "primary": False, "verified": True},
                {"email": self.email, "primary": True, "verified": self.email_verified},
            ]
        raise AssertionError(url)

    def token_form(self) -> dict[str, list[str]]:
        body = next(b for m, _u, _h, b in self.calls if m == "POST")
        assert body is not None
        return urllib.parse.parse_qs(body.decode())


@pytest.fixture
def provider(monkeypatch: pytest.MonkeyPatch) -> Iterator[FakeProvider]:
    fake = FakeProvider()
    oauth.set_http(fake)
    yield fake
    oauth.set_http(None)


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setenv(
        secret_store.SECRET_KEY_ENV, base64.urlsafe_b64encode(os.urandom(32)).decode()
    )
    monkeypatch.setenv(mailer.PUBLIC_URL_ENV, PUBLIC)
    # TestClient는 http라 Secure 쿠키를 돌려보내지 않는다.
    monkeypatch.setenv(COOKIE_SECURE_ENV, "false")
    get_engine.cache_clear()
    for limiter in (*accounts_api.LIMITERS, *oauth_api.LIMITERS):
        limiter.reset()
    with TestClient(app, base_url="https://testserver") as test_client:
        yield test_client
    get_engine.cache_clear()


def configure(engine: Engine, provider: str = "google", *, enabled: bool = True) -> None:
    with engine.begin() as connection:
        secret_store.put(connection, instance.oauth_secret(provider), CLIENT_SECRET, None)
        instance.put_value(
            connection,
            instance.oauth_key(provider),
            {"enabled": enabled, "client_id": f"{provider}-client-id"},
        )


def set_policy(engine: Engine, policy: str) -> None:
    with engine.begin() as connection:
        instance.put_value(connection, accounts.SIGNUP_POLICY_KEY, policy)


def start(
    client: TestClient, provider: str = "google", **params: str
) -> tuple[httpx.Response, dict[str, list[str]]]:
    response = client.get(
        f"/api/v1/auth/oauth/{provider}/start", params=params, follow_redirects=False
    )
    query = urllib.parse.parse_qs(urllib.parse.urlsplit(response.headers["location"]).query)
    return response, query


def finish(client: TestClient, provider: str, state: str, code: str = "auth-code") -> str:
    response = client.get(
        f"/api/v1/auth/oauth/{provider}/callback",
        params={"code": code, "state": state},
        follow_redirects=False,
    )
    assert response.status_code == 302
    return str(response.headers["location"])


def round_trip(client: TestClient, provider: str = "google", **params: str) -> str:
    response, query = start(client, provider, **params)
    assert response.status_code == 302, response.text
    return finish(client, provider, query["state"][0])


def user_by_email(engine: Engine, email: str) -> Any:
    with engine.connect() as connection:
        return connection.execute(
            select(schema.users).where(schema.users.c.email == email)
        ).one_or_none()


# --- 제공자 목록·시작 --------------------------------------------------------------


def test_providers_lists_only_fully_configured(client: TestClient, engine: Engine) -> None:
    assert client.get("/api/v1/auth/oauth/providers").json() == {"providers": []}
    configure(engine, "github", enabled=False)
    configure(engine, "google")
    body = client.get("/api/v1/auth/oauth/providers").text
    assert json.loads(body) == {"providers": ["google"]}
    assert CLIENT_SECRET not in body and "client-id" not in body


def test_disabled_provider_redirects_with_code(client: TestClient, engine: Engine) -> None:
    response, _ = start(client, "github")
    assert response.headers["location"] == f"{PUBLIC}/login#oauth=provider_disabled"


def test_start_stores_only_hashes_and_sets_httponly_cookie(
    client: TestClient, engine: Engine
) -> None:
    configure(engine)
    response, query = start(client)
    location = response.headers["location"]
    assert location.startswith("https://accounts.google.com/o/oauth2/v2/auth?")
    assert query["redirect_uri"] == [f"{PUBLIC}/api/v1/auth/oauth/google/callback"]
    assert query["client_id"] == ["google-client-id"]
    assert CLIENT_SECRET not in location
    cookie = response.headers["set-cookie"]
    assert cookie.startswith(f"{oauth_api.OAUTH_COOKIE}=")
    assert "HttpOnly" in cookie and "Path=/api/v1/auth/oauth" in cookie
    assert "samesite=lax" in cookie.lower()
    verifier = client.cookies.get(oauth_api.OAUTH_COOKIE)
    assert verifier and query["code_challenge"] == [oauth.code_challenge(verifier)]
    state = query["state"][0]
    with engine.connect() as connection:
        row = connection.execute(select(schema.oauth_states)).one()
        dump = str(
            connection.execute(text("SELECT row_to_json(o)::text FROM oauth_states o")).scalar()
        )
    assert row.state_hash == hashlib.sha256(state.encode()).digest()
    assert state not in dump and verifier not in dump


# --- 신규 가입·정책 -----------------------------------------------------------------


def test_public_policy_signup_then_login(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    assert round_trip(client) == f"{PUBLIC}/#oauth=signed_up"
    # 토큰 교환에 PKCE verifier와 같은 redirect_uri를 보냈다.
    form = provider.token_form()
    assert form["code_verifier"] == [
        client.cookies.get(oauth_api.OAUTH_COOKIE) or form["code_verifier"][0]
    ]
    assert form["redirect_uri"] == [f"{PUBLIC}/api/v1/auth/oauth/google/callback"]
    row = user_by_email(engine, "oauth.user@example.com")
    assert row.status == "active" and row.email_verified_at is not None
    assert row.password_hash is None and row.display_name == "oauth.user"
    session = client.cookies.get(SESSION_COOKIE)
    assert session
    assert client.get("/api/v1/auth/session").status_code == 200
    with engine.connect() as connection:
        identity = connection.execute(select(schema.auth_identities)).one()
    assert (identity.provider, identity.provider_subject) == ("google", "google-sub-1")

    client.cookies.clear()
    assert round_trip(client) == f"{PUBLIC}/#oauth=signed_in"


def test_github_uses_primary_verified_email(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine, "github")
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    response, query = start(client, "github")
    assert response.headers["location"].startswith("https://github.com/login/oauth/authorize?")
    assert query["code_challenge_method"] == ["S256"]
    assert finish(client, "github", query["state"][0]) == f"{PUBLIC}/#oauth=signed_up"
    assert user_by_email(engine, "oauth.user@example.com") is not None
    assert user_by_email(engine, "secondary@example.com") is None
    with engine.connect() as connection:
        subject = connection.execute(select(schema.auth_identities.c.provider_subject)).scalar()
    assert subject == "4242"


def test_unverified_email_cannot_sign_up(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    provider.email_verified = False
    assert round_trip(client) == f"{PUBLIC}/login#oauth=email_unverified"
    assert user_by_email(engine, "oauth.user@example.com") is None


@pytest.mark.parametrize(
    ("policy", "expected"),
    [("CLOSED", "signup_closed"), ("INVITE_ONLY", "invite_required")],
)
def test_closed_and_invite_only_reject_new_accounts(
    client: TestClient, engine: Engine, provider: FakeProvider, policy: str, expected: str
) -> None:
    configure(engine)
    set_policy(engine, policy)
    assert round_trip(client) == f"{PUBLIC}/login#oauth={expected}"
    assert user_by_email(engine, "oauth.user@example.com") is None


def test_admin_approval_creates_pending_account(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "ADMIN_APPROVAL")
    assert round_trip(client) == f"{PUBLIC}/login#oauth=approval_pending"
    assert user_by_email(engine, "oauth.user@example.com").status == "pending"
    assert client.cookies.get(SESSION_COOKIE) is None
    # 승인 전에는 다시 와도 로그인되지 않는다.
    assert round_trip(client) == f"{PUBLIC}/login#oauth=approval_pending"


def test_blocked_domain_rejected(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    with engine.begin() as connection:
        instance.put_value(connection, accounts.BLOCKED_DOMAINS_KEY, ["example.com"])
    assert round_trip(client) == f"{PUBLIC}/login#oauth=email_domain_blocked"


def _invite(engine: Engine, email: str) -> str:
    token = "synthetic-invite-token-" + base64.urlsafe_b64encode(os.urandom(9)).decode()
    owner, _ = sign_in(engine)
    with engine.begin() as connection:
        team_id = connection.execute(
            schema.teams.insert().values(name="t").returning(schema.teams.c.id)
        ).scalar_one()
        connection.execute(text("SET CONSTRAINTS ALL DEFERRED"))
        connection.execute(
            schema.team_members.insert().values(team_id=team_id, user_id=owner, role="OWNER")
        )
        connection.execute(
            schema.team_invitations.insert().values(
                team_id=team_id,
                email=email,
                role="MEMBER",
                token_hash=hashlib.sha256(token.encode()).digest(),
                invited_by_user_id=owner,
                expires_at=func.now() + text("interval '1 day'"),
            )
        )
    return token


def test_invite_only_accepts_matching_invitation(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "INVITE_ONLY")
    response, _ = start(client, invite="not-a-real-invite")
    assert response.headers["location"] == f"{PUBLIC}/login#oauth=invalid_invite"

    other = _invite(engine, "someone.else@example.com")
    assert round_trip(client, invite=other) == f"{PUBLIC}/login#oauth=invite_required"
    token = _invite(engine, "oauth.user@example.com")
    assert round_trip(client, invite=token) == f"{PUBLIC}/#oauth=signed_up"
    assert user_by_email(engine, "oauth.user@example.com").status == "active"


# --- 기존 계정·상태 -----------------------------------------------------------------


def test_never_auto_links_existing_email_account(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    with engine.begin() as connection:
        victim = accounts.create_user(
            connection,
            email="oauth.user@example.com",
            display_name="victim",
            password="correct horse battery",
            status="active",
            email_verified=True,
        )
    assert round_trip(client) == f"{PUBLIC}/login#oauth=account_exists_link_required"
    assert client.cookies.get(SESSION_COOKIE) is None
    with engine.connect() as connection:
        assert (
            connection.execute(select(func.count()).select_from(schema.auth_identities)).scalar()
            == 0
        )
        assert (
            connection.execute(
                select(schema.users.c.id).where(schema.users.c.email == "oauth.user@example.com")
            ).scalar()
            == victim
        )


def test_disabled_user_cannot_log_in(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    assert round_trip(client) == f"{PUBLIC}/#oauth=signed_up"
    with engine.begin() as connection:
        connection.execute(update(schema.users).values(status="disabled"))
    client.cookies.clear()
    assert round_trip(client) == f"{PUBLIC}/login#oauth=login_failed"
    assert client.cookies.get(SESSION_COOKIE) is None


# --- state·CSRF --------------------------------------------------------------------


def test_state_is_bound_to_the_starting_browser(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    _, query = start(client)
    state = query["state"][0]
    # 공격자가 자기 state·코드를 피해자 브라우저(쿠키 없음)로 보내도 실패한다.
    client.cookies.clear()
    assert finish(client, "google", state) == f"{PUBLIC}/login#oauth=state_invalid"
    assert provider.calls == []
    # state는 한 번 쓰면 사라진다.
    _, query = start(client)
    state = query["state"][0]
    assert finish(client, "google", state).endswith("#oauth=signed_up")
    assert finish(client, "google", state) == f"{PUBLIC}/login#oauth=state_invalid"


def test_wrong_cookie_unknown_state_and_expiry(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    configure(engine, "github")
    _, query = start(client)
    client.cookies.set(oauth_api.OAUTH_COOKIE, "x" * 43, path=oauth_api.OAUTH_COOKIE_PATH)
    assert finish(client, "google", query["state"][0]).endswith("#oauth=state_invalid")

    assert finish(client, "google", "made-up-state").endswith("#oauth=state_invalid")

    # 다른 제공자의 콜백으로는 쓸 수 없다.
    _, query = start(client)
    assert finish(client, "github", query["state"][0]).endswith("#oauth=state_invalid")

    _, query = start(client)
    with engine.begin() as connection:
        connection.execute(
            update(schema.oauth_states).values(
                created_at=func.now() - text("interval '1 hour'"),
                expires_at=func.now() - text("interval '1 minute'"),
            )
        )
    assert finish(client, "google", query["state"][0]).endswith("#oauth=state_invalid")
    assert provider.calls == []


def test_denied_and_exchange_failure(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    _, query = start(client)
    response = client.get(
        "/api/v1/auth/oauth/google/callback",
        params={"state": query["state"][0], "error": "access_denied"},
        follow_redirects=False,
    )
    assert response.headers["location"] == f"{PUBLIC}/login#oauth=denied"
    provider.token_status = 400
    assert round_trip(client) == f"{PUBLIC}/login#oauth=exchange_failed"


# --- 연결·해제 ---------------------------------------------------------------------


def _log_in(client: TestClient, engine: Engine, *, password: str | None) -> tuple[Any, str]:
    with engine.begin() as connection:
        user_id = accounts.create_user(
            connection,
            email="owner@example.com",
            display_name="owner",
            password=password,
            status="active",
            email_verified=True,
        )
        token = issue_session(connection, user_id)
    client.cookies.set(SESSION_COOKIE, token)
    return user_id, token


def test_link_requires_login_and_links_identity(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    response, _ = start(client, intent="link")
    assert response.headers["location"] == f"{PUBLIC}/login#oauth=login_required"

    user_id, token = _log_in(client, engine, password="correct horse battery")
    # 제공자 이메일이 계정 이메일과 달라도 로그인한 본인이 연결하므로 허용한다.
    assert round_trip(client, intent="link") == f"{PUBLIC}/account#oauth=linked"
    assert round_trip(client, intent="link") == f"{PUBLIC}/account#oauth=already_linked"
    body = client.get("/api/v1/auth/identities", headers=session_cookie(token)).json()
    assert body["password_set"] is True
    assert [i["provider"] for i in body["identities"]] == ["google"]

    # 연결한 신원으로 로그인하면 같은 계정이다.
    client.cookies.clear()
    assert round_trip(client) == f"{PUBLIC}/#oauth=signed_in"
    session = client.cookies.get(SESSION_COOKIE)
    me = client.get("/api/v1/auth/session", headers=session_cookie(session or "")).json()
    assert me["user"]["id"] == str(user_id)


def test_link_refuses_identity_owned_by_someone_else(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    assert round_trip(client).endswith("#oauth=signed_up")
    client.cookies.clear()
    _log_in(client, engine, password="correct horse battery")
    assert round_trip(client, intent="link") == f"{PUBLIC}/account#oauth=identity_in_use"
    # 같은 제공자의 다른 신원은 사용자당 하나만.
    provider.google_sub = "google-sub-2"
    assert round_trip(client, intent="link") == f"{PUBLIC}/account#oauth=linked"
    provider.google_sub = "google-sub-3"
    assert round_trip(client, intent="link") == f"{PUBLIC}/account#oauth=provider_already_linked"


def test_link_rejects_session_swap(
    client: TestClient, engine: Engine, provider: FakeProvider
) -> None:
    configure(engine)
    _log_in(client, engine, password="correct horse battery")
    _, query = start(client, intent="link")
    _, other = sign_in(engine)
    client.cookies.set(SESSION_COOKIE, other)
    assert finish(client, "google", query["state"][0]) == f"{PUBLIC}/login#oauth=login_required"


def test_unlink_rules(client: TestClient, engine: Engine, provider: FakeProvider) -> None:
    configure(engine)
    configure(engine, "github")
    user_id, token = _log_in(client, engine, password=None)
    assert round_trip(client, intent="link").endswith("#oauth=linked")
    assert round_trip(client, "github", intent="link").endswith("#oauth=linked")
    headers = session_cookie(token)
    assert client.delete("/api/v1/auth/identities/google", headers=headers).status_code == 403
    response = client.delete("/api/v1/auth/identities/google", headers={**headers, **csrf(token)})
    assert response.status_code == 204
    response = client.delete("/api/v1/auth/identities/google", headers={**headers, **csrf(token)})
    assert response.status_code == 404
    # 비밀번호가 없으면 마지막 로그인 수단은 지울 수 없다.
    response = client.delete("/api/v1/auth/identities/github", headers={**headers, **csrf(token)})
    assert response.status_code == 409 and response.json()["detail"] == "last_login_method"
    with engine.begin() as connection:
        connection.execute(
            update(schema.users)
            .where(schema.users.c.id == user_id)
            .values(password_hash="$argon2id$synthetic")
        )
    response = client.delete("/api/v1/auth/identities/github", headers={**headers, **csrf(token)})
    assert response.status_code == 204


# --- 프라이버시 --------------------------------------------------------------------


def test_no_tokens_secrets_or_emails_leak(
    client: TestClient,
    engine: Engine,
    provider: FakeProvider,
    caplog: pytest.LogCaptureFixture,
) -> None:
    configure(engine)
    set_policy(engine, "PUBLIC_VERIFIED_EMAIL")
    caplog.set_level("DEBUG")
    locations = [round_trip(client)]
    provider.token_status = 401
    client.cookies.clear()
    locations.append(round_trip(client))
    with engine.connect() as connection:
        audit = str(connection.execute(text("SELECT json_agg(a)::text FROM audit_logs a")).scalar())
        states = connection.execute(select(func.count()).select_from(schema.oauth_states)).scalar()
    # 테스트 클라이언트(httpx)의 요청 로그는 제외하고 서버 코드가 남긴 기록만 본다.
    server_logs = "\n".join(
        record.getMessage() for record in caplog.records if record.name.startswith("agent_office")
    )
    assert "token exchange rejected" in server_logs
    for secret in (ACCESS_TOKEN, CLIENT_SECRET, "oauth.user@example.com", "auth-code", "Profile"):
        assert secret not in server_logs
        assert secret not in audit
        assert all(secret not in location for location in locations)
    assert all("#oauth=" in location and "?" not in location for location in locations)
    assert states == 0


def test_retention_purges_expired_states(client: TestClient, engine: Engine) -> None:

    configure(engine)
    start(client)
    start(client)
    with engine.begin() as connection:
        connection.execute(
            update(schema.oauth_states)
            .where(
                schema.oauth_states.c.id
                == select(schema.oauth_states.c.id).limit(1).scalar_subquery()
            )
            .values(
                created_at=func.now() - text("interval '1 hour'"),
                expires_at=func.now() - text("interval '1 minute'"),
            )
        )
        counts = retention.purge(connection)
        left = connection.execute(select(func.count()).select_from(schema.oauth_states)).scalar()
    assert counts is not None and counts["oauth_states"] == 1 and left == 1

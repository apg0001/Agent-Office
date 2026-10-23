"""최초 관리자 bootstrap 테스트(#66). PostgreSQL이 필요하다."""

from __future__ import annotations

import threading
from collections.abc import Iterator

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, select, text

from agent_office.api import setup as setup_api
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app
from agent_office.setup import bootstrap

ORIGIN = {"origin": "http://testserver"}
PASSWORD = "an admin passphrase"


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    setup_api.bootstrap_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def issue(engine: Engine) -> str:
    with engine.begin() as connection:
        return bootstrap.issue_token(connection)


def create_admin(client: TestClient, token: str, email: str = "root@example.com") -> httpx.Response:
    return client.post(
        "/api/v1/setup/admin",
        json={
            "bootstrap_token": token,
            "email": email,
            "password": PASSWORD,
            "display_name": "Root",
        },
        headers=ORIGIN,
    )


def test_status_reports_progress(client: TestClient, engine: Engine) -> None:
    def status() -> dict[str, bool]:
        return dict(client.get("/api/v1/setup/status").json())

    assert status() == {"setup_completed": False, "admin_exists": False, "bootstrap_pending": False}
    issue(engine)
    assert status()["bootstrap_pending"] is True


def test_token_creates_one_super_admin_and_is_single_use(
    client: TestClient, engine: Engine
) -> None:
    token = issue(engine)
    assert token.startswith("aob_")
    with engine.connect() as connection:
        stored = str(
            connection.execute(
                select(schema.instance_settings.c.value).where(
                    schema.instance_settings.c.key == bootstrap.BOOTSTRAP_KEY
                )
            ).scalar_one()
        )
    assert token not in stored

    response = create_admin(client, token)
    assert response.status_code == 201
    assert response.json()["user"]["instance_role"] == "SUPER_ADMIN"
    cookie = response.cookies.get(SESSION_COOKIE)
    assert client.get("/api/v1/auth/session", headers=session_cookie(cookie)).status_code == 200

    reused = create_admin(client, token, "other@example.com")
    assert reused.status_code == 403 and reused.json()["detail"] == "bootstrap_rejected"
    # 관리자가 생긴 뒤에는 새 토큰도 발급할 수 없다.
    with pytest.raises(bootstrap.BootstrapUnavailable):
        issue(engine)


@pytest.mark.parametrize("token", ["", "aob_wrong", "x" * 256])
def test_wrong_tokens_are_rejected(client: TestClient, engine: Engine, token: str) -> None:
    issue(engine)
    assert create_admin(client, token).status_code == 403
    with engine.connect() as connection:
        assert connection.execute(text("SELECT count(*) FROM users")).scalar_one() == 0


def test_reissue_invalidates_previous_and_expiry_applies(
    client: TestClient, engine: Engine
) -> None:
    old = issue(engine)
    new = issue(engine)
    assert create_admin(client, old).status_code == 403

    with engine.begin() as connection:
        connection.execute(
            text(
                "UPDATE instance_settings SET value = jsonb_set(value, '{expires_at}',"
                " to_jsonb((now() - interval '1 minute')::text)) WHERE key = :k"
            ),
            {"k": bootstrap.BOOTSTRAP_KEY},
        )
    assert create_admin(client, new).status_code == 403


def test_concurrent_requests_create_only_one_admin(client: TestClient, engine: Engine) -> None:
    token = issue(engine)
    results: list[int] = []

    def attempt(index: int) -> None:
        results.append(create_admin(client, token, f"admin{index}@example.com").status_code)

    threads = [threading.Thread(target=attempt, args=(i,)) for i in range(4)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert sorted(results) == [201, 403, 403, 403]
    with engine.connect() as connection:
        admins = connection.execute(
            text("SELECT count(*) FROM users WHERE instance_role = 'SUPER_ADMIN'")
        ).scalar_one()
    assert admins == 1


def test_existing_email_keeps_token_usable(client: TestClient, engine: Engine) -> None:
    with engine.begin() as connection:
        connection.execute(
            schema.users.insert().values(email="root@example.com", display_name="Taken")
        )
    token = issue(engine)

    assert create_admin(client, token).status_code == 409
    assert create_admin(client, token, "admin@example.com").status_code == 201


def test_complete_locks_setup(client: TestClient, engine: Engine) -> None:
    _, user_session = sign_in(engine)
    forbidden = client.post(
        "/api/v1/setup/complete", headers={**session_cookie(user_session), **csrf(user_session)}
    )
    assert forbidden.status_code == 403

    admin = create_admin(client, issue(engine)).cookies.get(SESSION_COOKIE)
    done = client.post("/api/v1/setup/complete", headers={**session_cookie(admin), **csrf(admin)})
    assert done.status_code == 204
    assert client.get("/api/v1/setup/status").json()["setup_completed"] is True


def test_cross_site_and_bad_input(client: TestClient, engine: Engine) -> None:
    token = issue(engine)
    evil = client.post(
        "/api/v1/setup/admin",
        json={
            "bootstrap_token": token,
            "email": "a@b.co",
            "password": PASSWORD,
            "display_name": "x",
        },
        headers={"origin": "https://evil.example"},
    )
    assert evil.status_code == 403
    short = client.post(
        "/api/v1/setup/admin",
        json={
            "bootstrap_token": token,
            "email": "a@b.co",
            "password": "short",
            "display_name": "x",
        },
        headers=ORIGIN,
    )
    assert short.json()["detail"] == "password_too_short"
    # 입력 오류는 토큰을 소비하지 않는다.
    assert create_admin(client, token).status_code == 201


def test_cli_prints_token_once(engine: Engine, capsys: pytest.CaptureFixture[str]) -> None:
    get_engine.cache_clear()
    assert bootstrap.main() == 0
    token = capsys.readouterr().out.strip().splitlines()[-1]
    assert token.startswith("aob_")
    with engine.begin() as connection:
        assert bootstrap.consume_token(connection, token)
    get_engine.cache_clear()

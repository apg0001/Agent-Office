"""보안 시나리오 테스트(#64) 공통 fixture. 값은 모두 합성이다."""

from __future__ import annotations

import base64
import os
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import Engine

from agent_office import secret_store
from agent_office.api import accounts as accounts_api
from agent_office.api import events as events_api
from agent_office.api import oauth as oauth_api
from agent_office.api import office
from agent_office.api import setup as setup_api
from agent_office.auth import mailer
from agent_office.db.engine import get_engine
from agent_office.main import app
from security.secworld import FakeMailer


def _reset_limiters() -> None:
    for limiter in (*accounts_api.LIMITERS, *oauth_api.LIMITERS, setup_api.bootstrap_limiter):
        limiter.reset()
    events_api.rate_limiter.reset()


@pytest.fixture
def outbox(monkeypatch: pytest.MonkeyPatch) -> Iterator[FakeMailer]:
    fake = FakeMailer()
    monkeypatch.setenv(mailer.PUBLIC_URL_ENV, "https://office.example")
    mailer.set_mailer(fake)
    yield fake
    mailer.set_mailer(None)


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setattr(office, "POLL_INTERVAL_SECONDS", 0.05)
    monkeypatch.setenv(
        secret_store.SECRET_KEY_ENV, base64.urlsafe_b64encode(os.urandom(32)).decode()
    )
    for name in [n for n in os.environ if n.startswith(mailer.SMTP_ENV_PREFIX)]:
        monkeypatch.delenv(name)
    monkeypatch.delenv("AGENT_OFFICE_ALLOWED_ORIGINS", raising=False)
    get_engine.cache_clear()
    _reset_limiters()
    with TestClient(app, raise_server_exceptions=False) as test_client:
        yield test_client
    get_engine.cache_clear()
    _reset_limiters()

"""서버 테스트 공통 fixture. PostgreSQL 테스트는 테스트마다 임시 DB를 만든다."""

from __future__ import annotations

import os
import secrets
from collections.abc import Iterator
from importlib.resources import files

import pytest
from alembic import command
from alembic.config import Config
from sqlalchemy import Engine, create_engine, text
from sqlalchemy.engine import make_url

from agent_office.db.config import DATABASE_URL_ENV

ADMIN_URL_ENV = "AGENT_OFFICE_TEST_DATABASE_URL"


def admin_url() -> str:
    url = os.environ.get(ADMIN_URL_ENV)
    if url:
        return url
    if os.environ.get("AGENT_OFFICE_REQUIRE_DB_TESTS") == "1":
        pytest.fail(f"{ADMIN_URL_ENV} is required in this environment")
    pytest.skip(f"{ADMIN_URL_ENV} is not set; PostgreSQL schema tests not run")


def alembic_config() -> Config:
    config = Config()
    config.set_main_option("script_location", str(files("agent_office.db") / "migrations"))
    return config


@pytest.fixture
def database(monkeypatch: pytest.MonkeyPatch) -> Iterator[str]:
    server_url = make_url(admin_url())
    name = f"ao_test_{secrets.token_hex(6)}"
    admin = create_engine(server_url, isolation_level="AUTOCOMMIT")
    with admin.connect() as connection:
        connection.execute(text(f'CREATE DATABASE "{name}"'))
    url = server_url.set(database=name).render_as_string(hide_password=False)
    monkeypatch.setenv(DATABASE_URL_ENV, url)
    try:
        yield url
    finally:
        with admin.connect() as connection:
            connection.execute(text(f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)'))
        admin.dispose()


@pytest.fixture
def engine(database: str) -> Iterator[Engine]:
    command.upgrade(alembic_config(), "head")
    created = create_engine(database)
    try:
        yield created
    finally:
        created.dispose()

"""PostgreSQL 스키마·마이그레이션 테스트(#22).

`AGENT_OFFICE_TEST_DATABASE_URL`에 관리 권한이 있는 PostgreSQL URL을 주면 실행된다.
테스트마다 임시 데이터베이스를 만들고 끝나면 삭제한다. CI는
`AGENT_OFFICE_REQUIRE_DB_TESTS=1`로 DB가 없을 때 건너뛰지 않고 실패시킨다.
"""

from __future__ import annotations

import hashlib
import os
import secrets
import uuid
from collections.abc import Iterator
from importlib.resources import files
from typing import Any

import pytest
from alembic import command
from alembic.autogenerate import compare_metadata
from alembic.config import Config
from alembic.migration import MigrationContext
from sqlalchemy import Engine, String, create_engine, inspect, text
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.engine import Connection, make_url
from sqlalchemy.exc import IntegrityError

from agent_office.db import schema
from agent_office.db.config import DATABASE_URL_ENV

ADMIN_URL_ENV = "AGENT_OFFICE_TEST_DATABASE_URL"
FORBIDDEN_COLUMN_WORDS = {
    "prompt",
    "command",
    "content",
    "body",
    "path",
    "output",
    "input",
    "transcript",
    "message",
}


def test_schema_has_no_free_text_or_raw_content_columns() -> None:
    for table in schema.metadata.sorted_tables:
        for column in table.columns:
            words = set(column.name.split("_"))
            assert not words & FORBIDDEN_COLUMN_WORDS, f"{table.name}.{column.name}"
            if isinstance(column.type, String):
                assert column.type.length is not None, f"{table.name}.{column.name}"
                assert column.type.length <= 500, f"{table.name}.{column.name}"


def test_event_columns_match_event_v1_whitelist() -> None:
    event_v1 = {
        "schema_version",
        "event_id",
        "device_id",
        "session_id",
        "agent_id",
        "provider",
        "source",
        "event_type",
        "activity",
        "confidence",
        "occurred_at",
        "sequence",
    }
    server_only = {"received_at", "payload_digest"}

    assert set(schema.activity_events.columns.keys()) == event_v1 | server_only


def _admin_url() -> str:
    url = os.environ.get(ADMIN_URL_ENV)
    if url:
        return url
    if os.environ.get("AGENT_OFFICE_REQUIRE_DB_TESTS") == "1":
        pytest.fail(f"{ADMIN_URL_ENV} is required in this environment")
    pytest.skip(f"{ADMIN_URL_ENV} is not set; PostgreSQL schema tests not run")


def _alembic_config() -> Config:
    config = Config()
    config.set_main_option("script_location", str(files("agent_office.db") / "migrations"))
    return config


@pytest.fixture
def database(monkeypatch: pytest.MonkeyPatch) -> Iterator[str]:
    admin_url = make_url(_admin_url())
    name = f"ao_test_{secrets.token_hex(6)}"
    admin = create_engine(admin_url, isolation_level="AUTOCOMMIT")
    with admin.connect() as connection:
        connection.execute(text(f'CREATE DATABASE "{name}"'))
    url = admin_url.set(database=name).render_as_string(hide_password=False)
    monkeypatch.setenv(DATABASE_URL_ENV, url)
    try:
        yield url
    finally:
        with admin.connect() as connection:
            connection.execute(text(f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)'))
        admin.dispose()


@pytest.fixture
def engine(database: str) -> Iterator[Engine]:
    command.upgrade(_alembic_config(), "head")
    created = create_engine(database)
    try:
        yield created
    finally:
        created.dispose()


def test_migration_head_matches_metadata(engine: Engine) -> None:
    with engine.connect() as connection:
        context = MigrationContext.configure(connection, opts={"compare_type": True})
        differences = compare_metadata(context, schema.metadata)

    assert differences == []


def test_downgrade_removes_everything_and_upgrade_is_repeatable(engine: Engine) -> None:
    config = _alembic_config()

    command.downgrade(config, "base")
    with engine.connect() as connection:
        tables = set(inspect(connection).get_table_names())
        functions = connection.execute(
            text("SELECT count(*) FROM pg_proc WHERE proname LIKE 'ao\\_%'")
        ).scalar_one()
    command.upgrade(config, "head")
    with engine.connect() as connection:
        restored = set(inspect(connection).get_table_names())

    assert tables == {"alembic_version"}
    assert functions == 0
    assert restored == set(schema.metadata.tables) | {"alembic_version"}


# --- 무결성 규칙 ---------------------------------------------------------------


def _insert(connection: Connection, table: Any, **values: object) -> Any:
    return connection.execute(table.insert().values(**values).returning(*table.primary_key))


def _user(connection: Connection, role: str = "USER", status: str = "active") -> uuid.UUID:
    email = f"{secrets.token_hex(4)}@example.invalid"
    row = _insert(
        connection,
        schema.users,
        email=email,
        display_name="synthetic",
        instance_role=role,
        status=status,
    ).one()
    return uuid.UUID(str(row.id))


def test_email_must_be_normalized_and_unique(engine: Engine) -> None:
    with engine.begin() as connection:
        _insert(connection, schema.users, email="a@example.invalid", display_name="a")
    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(connection, schema.users, email="a@example.invalid", display_name="b")
    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(connection, schema.users, email="B@example.invalid", display_name="b")


def test_last_active_super_admin_cannot_be_removed(engine: Engine) -> None:
    users = schema.users
    with engine.begin() as connection:
        first = _user(connection, "SUPER_ADMIN")

    for change in (
        users.update().where(users.c.id == first).values(instance_role="ADMIN"),
        users.update().where(users.c.id == first).values(status="disabled"),
        users.delete().where(users.c.id == first),
    ):
        with pytest.raises(IntegrityError), engine.begin() as connection:
            connection.execute(change)

    with engine.begin() as connection:
        second = _user(connection, "SUPER_ADMIN")
        connection.execute(users.update().where(users.c.id == first).values(instance_role="USER"))
    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(users.delete().where(users.c.id == second))


def _team_with_owner(connection: Connection, owner: uuid.UUID) -> uuid.UUID:
    team = _insert(connection, schema.teams, name="synthetic team").one().id
    _insert(connection, schema.team_members, team_id=team, user_id=owner, role="OWNER")
    return uuid.UUID(str(team))


def test_team_requires_exactly_one_owner(engine: Engine) -> None:
    members = schema.team_members
    with engine.begin() as connection:
        owner, member = _user(connection), _user(connection)

    # OWNER 없이 만든 팀은 커밋 시점에 거부된다.
    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(connection, schema.teams, name="ownerless")

    with engine.begin() as connection:
        team = _team_with_owner(connection, owner)
        _insert(connection, members, team_id=team, user_id=member, role="MEMBER")

    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(members.update().where(members.c.user_id == member).values(role="OWNER"))

    # 같은 트랜잭션 안에서 강등 후 승격하면 소유권 이전이 된다.
    with engine.begin() as connection:
        connection.execute(members.update().where(members.c.user_id == owner).values(role="MEMBER"))
        connection.execute(members.update().where(members.c.user_id == member).values(role="OWNER"))

    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(members.delete().where(members.c.user_id == member))

    # OWNER인 사용자를 삭제하면 소속이 연쇄 삭제되어 커밋 시점에 거부된다.
    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(schema.users.delete().where(schema.users.c.id == member))

    # 팀 자체를 지우면 소속도 함께 지워지고 OWNER 검사는 건너뛴다.
    with engine.begin() as connection:
        connection.execute(schema.teams.delete().where(schema.teams.c.id == team))
        assert connection.execute(members.select()).all() == []


def _device(connection: Connection, owner: uuid.UUID, name: str) -> uuid.UUID:
    return uuid.UUID(
        str(_insert(connection, schema.devices, owner_user_id=owner, name=name).one().id)
    )


def _session(connection: Connection, device: uuid.UUID) -> uuid.UUID:
    row = _insert(
        connection,
        schema.agent_sessions,
        device_id=device,
        provider="claude",
        collector_session_id=uuid.uuid4(),
    ).one()
    return uuid.UUID(str(row.id))


def _event(device: uuid.UUID, session: uuid.UUID, **overrides: object) -> dict[str, object]:
    values: dict[str, object] = {
        "event_id": uuid.uuid4(),
        "device_id": device,
        "session_id": session,
        "schema_version": "1.0",
        "provider": "claude",
        "source": "hook",
        "event_type": "tool.started",
        "activity": "reading",
        "confidence": "observed",
        "occurred_at": text("now()"),
        "sequence": 1,
        "payload_digest": hashlib.sha256(b"synthetic").digest(),
    }
    values.update(overrides)
    return values


def test_events_are_idempotent_and_bound_to_their_device(engine: Engine) -> None:
    events = schema.activity_events
    with engine.begin() as connection:
        owner = _user(connection)
        device, other_device = (
            _device(connection, owner, "pc-1"),
            _device(connection, owner, "pc-2"),
        )
        session = _session(connection, device)
    event = _event(device, session)

    # 같은 event_id 재전송은 ON CONFLICT로 무시되고 한 건만 남는다.
    with engine.begin() as connection:
        for _ in range(3):
            connection.execute(
                insert(events).values(**event).on_conflict_do_nothing(index_elements=["event_id"])
            )
        assert len(connection.execute(events.select()).all()) == 1

    # 다른 장치의 자격증명으로 남의 세션에 이벤트를 넣을 수 없다.
    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(events.insert().values(**_event(other_device, session)))
    # 추정 소스는 observed로 저장할 수 없다.
    with pytest.raises(IntegrityError), engine.begin() as connection:
        connection.execute(events.insert().values(**_event(device, session, source="inferred")))


def test_agent_hierarchy_stays_within_one_session(engine: Engine) -> None:
    agents = schema.agents
    with engine.begin() as connection:
        owner = _user(connection)
        device = _device(connection, owner, "pc")
        session, other_session = _session(connection, device), _session(connection, device)
        lead = (
            _insert(
                connection, agents, session_id=session, collector_agent_id=uuid.uuid4(), role="lead"
            )
            .one()
            .id
        )
        _insert(
            connection,
            agents,
            session_id=session,
            parent_agent_id=lead,
            collector_agent_id=uuid.uuid4(),
            role="subagent",
        )

    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(
            connection,
            agents,
            session_id=other_session,
            parent_agent_id=lead,
            collector_agent_id=uuid.uuid4(),
            role="subagent",
        )
    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(
            connection,
            agents,
            session_id=session,
            parent_agent_id=lead,
            collector_agent_id=uuid.uuid4(),
            role="lead",
        )


def test_session_occupies_at_most_one_desk_per_layout(engine: Engine) -> None:
    slots = schema.office_slots
    with engine.begin() as connection:
        owner = _user(connection)
        session = _session(connection, _device(connection, owner, "pc"))
        layout = (
            _insert(connection, schema.office_layouts, scope="user", owner_user_id=owner).one().id
        )
        _insert(
            connection,
            slots,
            layout_id=layout,
            slot_index=0,
            grid_x=0,
            grid_y=0,
            session_id=session,
            assigned_at=text("now()"),
        )

    with pytest.raises(IntegrityError), engine.begin() as connection:
        _insert(
            connection,
            slots,
            layout_id=layout,
            slot_index=1,
            grid_x=1,
            grid_y=0,
            session_id=session,
            assigned_at=text("now()"),
        )

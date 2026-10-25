"""세션별 책상 배정·재사용·확장 테스트(#48). PostgreSQL이 필요하다."""

from __future__ import annotations

import datetime as dt
import uuid
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, text, update

from agent_office.api import events as events_api
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app
from agent_office.state.desks import slot_cell


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()
    events_api.rate_limiter.reset()


def owner(client: TestClient, engine: Engine) -> tuple[str, dict[str, str]]:
    _, session = sign_in(engine)
    return session, register(client, new_code(client, session))


def send(
    client: TestClient, device: dict[str, str], session_id: uuid.UUID, event_type: str
) -> None:
    event = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": device["device_id"],
        "session_id": str(session_id),
        "agent_id": str(session_id),
        "parent_agent_id": None,
        "provider": "claude",
        "source": "hook",
        "event_type": event_type,
        "activity": "unknown",
        "confidence": "observed",
        "occurred_at": dt.datetime.now(dt.UTC).isoformat(),
        "sequence": 1 if event_type == "session.started" else 2,
    }
    response = client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": [event]},
        headers=bearer(device["device_token"]),
    )
    assert response.status_code == 200 and response.json()["rejected_events"] == []


def desks(client: TestClient, session: str) -> dict[str, int | None]:
    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()
    return {s["id"]: s["desk_slot"] for s in snapshot["sessions"]}


def test_slot_cells_match_web_seat_layout() -> None:
    assert slot_cell(0) == (2, 2)
    assert slot_cell(6) == (14, 2)
    assert slot_cell(7) == (2, 5)


def test_desk_is_stable_for_the_session(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    first, second = uuid.uuid4(), uuid.uuid4()
    send(client, device, first, "session.started")
    send(client, device, second, "session.started")
    before = desks(client, session)

    send(client, device, first, "turn.started")

    assert sorted(before.values()) == [0, 1]
    assert desks(client, session) == before


def test_released_desk_is_reused_only_after_exit_grace(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    leaving, staying = uuid.uuid4(), uuid.uuid4()
    send(client, device, leaving, "session.started")
    send(client, device, staying, "session.started")
    send(client, device, leaving, "session.ended")

    # 퇴근 연출 중에는 비우지 않는다.
    newcomer = uuid.uuid4()
    send(client, device, newcomer, "session.started")
    assert sorted(v for v in desks(client, session).values() if v is not None) == [0, 1, 2]

    with engine.begin() as connection:
        connection.execute(
            update(schema.agent_sessions)
            .where(schema.agent_sessions.c.lifecycle == "ENDED")
            .values(
                started_at=text("now() - interval '10 minutes'"),
                ended_at=text("now() - interval '1 minute'"),
            )
        )
    latest = uuid.uuid4()
    send(client, device, latest, "session.started")

    with engine.connect() as connection:
        slots = {row.slot_index for row in connection.execute(schema.office_slots.select()).all()}
    assert slots == {0, 1, 2}


def test_thirty_sessions_get_unique_desks(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    for _ in range(30):
        send(client, device, uuid.uuid4(), "session.started")

    assigned = list(desks(client, session).values())

    assert sorted(assigned) == list(range(30))


def test_each_user_has_a_separate_office(client: TestClient, engine: Engine) -> None:
    first_session, first = owner(client, engine)
    second_session, second = owner(client, engine)
    send(client, first, uuid.uuid4(), "session.started")
    send(client, second, uuid.uuid4(), "session.started")

    assert list(desks(client, first_session).values()) == [0]
    assert list(desks(client, second_session).values()) == [0]

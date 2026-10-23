"""상태 엔진 테스트(#36). 전이 규칙은 순수 함수로, 반영은 수집 API로 검증한다."""

from __future__ import annotations

import datetime as dt
import uuid
from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register, sign_in
from sqlalchemy import Engine, select

from agent_office.api import events as events_api
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app
from agent_office.state.engine import (
    IDLE,
    AgentActivity,
    Lifecycle,
    connection_state,
    effective_lifecycle,
    is_newer,
    next_activity,
    next_lifecycle,
)

T0 = dt.datetime(2026, 10, 23, 12, 0, tzinfo=dt.UTC)


def at(minutes: float) -> dt.datetime:
    return T0 + dt.timedelta(minutes=minutes)


# --- 순수 규칙 ---------------------------------------------------------------------


def test_connection_state_uses_heartbeat_age() -> None:
    assert connection_state(None, T0) == "DISCONNECTED"
    assert connection_state(T0 - dt.timedelta(seconds=60), T0) == "ONLINE"
    assert connection_state(T0 - dt.timedelta(seconds=121), T0) == "DISCONNECTED"


def test_quiet_session_is_never_ended_only_unconfirmed() -> None:
    long_ago = T0 - dt.timedelta(hours=5)

    assert effective_lifecycle("OPEN", long_ago, "ONLINE", T0) == "OPEN"
    assert effective_lifecycle("OPEN", long_ago, "DISCONNECTED", T0) == "UNKNOWN"
    assert effective_lifecycle("OPEN", T0 - dt.timedelta(minutes=5), "DISCONNECTED", T0) == "OPEN"
    assert effective_lifecycle("ENDED", long_ago, "DISCONNECTED", T0) == "ENDED"


def test_lifecycle_transitions_and_resume() -> None:
    discovered = Lifecycle("DISCOVERED", None, None)

    opened = next_lifecycle(discovered, "session.started", at(0))
    assert opened == Lifecycle("OPEN", at(0), None)
    assert next_lifecycle(discovered, "tool.started", at(1)).state == "OPEN"

    ended = next_lifecycle(opened, "session.ended", at(10))
    assert ended == Lifecycle("ENDED", at(0), at(10))
    # 종료보다 오래된(늦게 도착한) 이벤트로는 다시 열리지 않는다.
    assert next_lifecycle(ended, "tool.started", at(9)) == ended
    # 종료 뒤 더 늦은 활동은 재개다(#20: resume은 같은 세션 ID).
    assert next_lifecycle(ended, "session.started", at(20)) == Lifecycle("OPEN", at(0), None)


@pytest.mark.parametrize(
    ("event_type", "activity", "confidence", "expected"),
    [
        ("turn.started", "thinking", "observed", AgentActivity("THINKING", "thinking")),
        ("tool.started", "reading", "observed", AgentActivity("WORKING", "reading")),
        ("tool.completed", "reading", "observed", AgentActivity("THINKING", "thinking")),
        (
            "approval.requested",
            "waiting_user",
            "observed",
            AgentActivity("WAITING_USER", "waiting_user"),
        ),
        ("approval.requested", "waiting_user", "inferred", AgentActivity("WORKING", "coding")),
        ("turn.completed", "idle", "observed", IDLE),
        ("session.ended", "idle", "observed", IDLE),
        ("plan.updated", "planning", "observed", AgentActivity("WORKING", "coding")),
        ("unknown", "unknown", "observed", AgentActivity("WORKING", "coding")),
    ],
)
def test_activity_transitions(
    event_type: str, activity: str, confidence: str, expected: AgentActivity
) -> None:
    working = AgentActivity("WORKING", "coding")

    assert next_activity(working, event_type, activity, confidence) == expected


def test_newer_compares_time_then_sequence() -> None:
    assert is_newer(at(1), 0, None, None)
    assert is_newer(at(2), 0, at(1), 99)
    assert not is_newer(at(1), 5, at(2), 0)
    assert is_newer(at(1), 6, at(1), 5)
    assert not is_newer(at(1), 5, at(1), 5)


# --- 수집 API로 반영 -----------------------------------------------------------------


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()
    events_api.rate_limiter.reset()


@pytest.fixture
def device(client: TestClient, engine: Engine) -> dict[str, str]:
    _, session = sign_in(engine)
    return register(client, new_code(client, session))


class Stream:
    """한 세션의 합성 이벤트를 만든다. 시각은 지금 기준 분 단위 오프셋이다."""

    def __init__(self, device: dict[str, str]) -> None:
        self.device = device
        self.session = uuid.uuid4()
        self.base = dt.datetime.now(dt.UTC) - dt.timedelta(hours=1)
        self.sequence = 0

    def event(
        self,
        event_type: str,
        minute: float,
        agent: uuid.UUID,
        *,
        parent: uuid.UUID | None = None,
        activity: str = "unknown",
        confidence: str = "observed",
    ) -> dict[str, object]:
        self.sequence += 1
        return {
            "schema_version": "1.0",
            "event_id": str(uuid.uuid4()),
            "device_id": self.device["device_id"],
            "session_id": str(self.session),
            "agent_id": str(agent),
            "parent_agent_id": str(parent) if parent else None,
            "provider": "claude",
            "source": "hook",
            "event_type": event_type,
            "activity": activity,
            "confidence": confidence,
            "occurred_at": (self.base + dt.timedelta(minutes=minute)).isoformat(),
            "sequence": self.sequence,
        }

    def send(self, client: TestClient, *events: dict[str, object]) -> None:
        response = client.post(
            "/api/v1/events/batch",
            json={"schema_version": "1.0", "events": list(events)},
            headers=bearer(self.device["device_token"]),
        )
        assert response.status_code == 200, response.text
        assert response.json()["rejected_events"] == []


def lifecycle(engine: Engine) -> str:
    with engine.connect() as connection:
        return str(connection.execute(select(schema.agent_sessions.c.lifecycle)).scalar_one())


def agent_state(engine: Engine, collector_agent_id: uuid.UUID) -> tuple[str, str, int]:
    states, agents = schema.agent_current_states, schema.agents
    with engine.connect() as connection:
        row = connection.execute(
            select(states.c.turn_state, states.c.activity, states.c.state_version)
            .select_from(states.join(agents, agents.c.id == states.c.agent_id))
            .where(agents.c.collector_agent_id == collector_agent_id)
        ).one()
    return row.turn_state, row.activity, row.state_version


def test_ingestion_drives_lifecycle_and_activity(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    stream, lead = Stream(device), uuid.uuid4()

    stream.send(client, stream.event("session.started", 0, lead))
    assert lifecycle(engine) == "OPEN"
    assert agent_state(engine, lead)[:2] == ("IDLE", "idle")

    stream.send(
        client,
        stream.event("turn.started", 1, lead),
        stream.event("tool.started", 2, lead, activity="reading"),
    )
    assert agent_state(engine, lead)[:2] == ("WORKING", "reading")

    stream.send(client, stream.event("approval.requested", 3, lead, activity="waiting_user"))
    assert agent_state(engine, lead)[:2] == ("WAITING_USER", "waiting_user")

    stream.send(client, stream.event("turn.completed", 4, lead))
    # 턴 완료는 유휴 전환일 뿐 세션 종료가 아니다.
    assert agent_state(engine, lead)[:2] == ("IDLE", "idle")
    assert lifecycle(engine) == "OPEN"

    stream.send(client, stream.event("session.ended", 5, lead))
    assert lifecycle(engine) == "ENDED"


def test_late_events_do_not_rewind_current_state(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    stream, lead = Stream(device), uuid.uuid4()
    started = stream.event("session.started", 0, lead)
    tool = stream.event("tool.started", 2, lead, activity="coding")
    ended = stream.event("session.ended", 10, lead)
    stream.send(client, started, ended)
    _, _, version = agent_state(engine, lead)

    # 오프라인 큐에서 뒤늦게 온 과거 이벤트: 저장은 되지만 상태를 되돌리지 않는다.
    stream.send(client, tool)

    assert lifecycle(engine) == "ENDED"
    assert agent_state(engine, lead) == ("IDLE", "idle", version)


def test_subagent_has_its_own_state_and_lifetime(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    stream, lead, child = Stream(device), uuid.uuid4(), uuid.uuid4()
    stream.send(
        client,
        stream.event("session.started", 0, lead),
        stream.event("turn.started", 1, lead),
        stream.event("tool.started", 2, lead, activity="delegating"),
        stream.event("agent.started", 3, child, parent=lead, activity="reading"),
        stream.event("turn.completed", 4, lead),
    )
    # 하위 에이전트가 일하는 동안 대장의 턴이 끝날 수 있다(#20 실측).
    assert agent_state(engine, lead)[:2] == ("IDLE", "idle")
    assert agent_state(engine, child)[:2] == ("WORKING", "reading")

    stream.send(client, stream.event("agent.completed", 6, child, parent=lead))
    assert agent_state(engine, child)[:2] == ("IDLE", "idle")
    with engine.connect() as connection:
        lifetime = connection.execute(
            select(schema.agents.c.started_at, schema.agents.c.ended_at).where(
                schema.agents.c.collector_agent_id == child
            )
        ).one()
    assert lifetime.started_at is not None and lifetime.ended_at is not None


def test_resumed_session_reopens_with_later_activity(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    stream, lead = Stream(device), uuid.uuid4()
    stream.send(
        client, stream.event("session.started", 0, lead), stream.event("session.ended", 5, lead)
    )

    stream.send(client, stream.event("session.started", 20, lead))

    assert lifecycle(engine) == "OPEN"
    with engine.connect() as connection:
        ended_at = connection.execute(select(schema.agent_sessions.c.ended_at)).scalar_one()
    assert ended_at is None


def test_current_state_records_inferred_confidence(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    stream, lead = Stream(device), uuid.uuid4()
    stream.send(
        client,
        stream.event("session.started", 0, lead),
        stream.event("tool.started", 1, lead, activity="reading", confidence="inferred"),
    )
    states, agents = schema.agent_current_states, schema.agents
    with engine.connect() as connection:
        confidence = connection.execute(
            select(states.c.confidence)
            .select_from(states.join(agents, agents.c.id == states.c.agent_id))
            .where(agents.c.collector_agent_id == lead)
        ).scalar_one()
    assert confidence == "inferred"

"""수집된 이벤트를 현재 상태 테이블에 반영한다. 이벤트 저장과 같은 트랜잭션에서 호출한다."""

from __future__ import annotations

import datetime as dt
import uuid

from sqlalchemy import Connection, func, select, update
from sqlalchemy.dialects.postgresql import insert

from agent_office.db import schema
from agent_office.state.desks import ensure_desk
from agent_office.state.engine import (
    AgentActivity,
    Lifecycle,
    is_newer,
    next_activity,
    next_lifecycle,
)


def apply_event(
    connection: Connection,
    *,
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    event_type: str,
    activity: str,
    confidence: str,
    occurred_at: dt.datetime,
    sequence: int,
) -> None:
    lifecycle = _apply_lifecycle(connection, session_id, event_type, occurred_at)
    if lifecycle != "ENDED":
        ensure_desk(connection, session_id)
    _apply_agent(connection, agent_id, event_type, activity, confidence, occurred_at, sequence)


def apply_plan(
    connection: Connection,
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    statuses: list[str],
    occurred_at: dt.datetime,
) -> bool:
    """세션 계획을 이 상태 목록으로 바꾼다(#38). 이미 더 늦은 계획이 있으면 무시한다."""
    steps = schema.plan_steps
    latest = connection.execute(
        select(func.max(steps.c.updated_at)).where(steps.c.session_id == session_id)
    ).scalar_one()
    if latest is not None and occurred_at <= latest:
        return False
    connection.execute(steps.delete().where(steps.c.session_id == session_id))
    connection.execute(
        steps.insert(),
        [
            {
                "session_id": session_id,
                "agent_id": agent_id,
                "step_index": index,
                "status": status,
                "updated_at": occurred_at,
            }
            for index, status in enumerate(statuses)
        ],
    )
    # 상세 패널이 다시 가져오도록 이 에이전트의 변경 번호를 올린다.
    states = schema.agent_current_states
    connection.execute(
        update(states)
        .where(states.c.agent_id == agent_id)
        .values(change_seq=_next_change(), updated_at=func.clock_timestamp())
    )
    return True


def _apply_lifecycle(
    connection: Connection, session_id: uuid.UUID, event_type: str, occurred_at: dt.datetime
) -> str:
    sessions = schema.agent_sessions
    row = connection.execute(
        select(sessions.c.lifecycle, sessions.c.started_at, sessions.c.ended_at)
        .where(sessions.c.id == session_id)
        .with_for_update()
    ).one()
    current = Lifecycle(row.lifecycle, row.started_at, row.ended_at)
    changed = next_lifecycle(current, event_type, occurred_at)
    if changed != current:
        connection.execute(
            update(sessions)
            .where(sessions.c.id == session_id)
            .values(
                lifecycle=changed.state,
                started_at=changed.started_at,
                ended_at=changed.ended_at,
                change_seq=_next_change(),
                changed_at=func.clock_timestamp(),
            )
        )
    return changed.state


def _apply_agent(
    connection: Connection,
    agent_id: uuid.UUID,
    event_type: str,
    activity: str,
    confidence: str,
    occurred_at: dt.datetime,
    sequence: int,
) -> None:
    states, agents = schema.agent_current_states, schema.agents
    connection.execute(
        insert(states).values(agent_id=agent_id).on_conflict_do_nothing(index_elements=["agent_id"])
    )
    row = connection.execute(
        select(
            states.c.turn_state,
            states.c.activity,
            states.c.confidence,
            states.c.state_version,
            states.c.last_event_occurred_at,
            states.c.last_event_sequence,
        )
        .where(states.c.agent_id == agent_id)
        .with_for_update()
    ).one()

    lifetime: dict[str, object] = {}
    if event_type == "agent.started":
        lifetime["started_at"] = func.coalesce(agents.c.started_at, occurred_at)
    elif event_type == "agent.completed":
        lifetime["ended_at"] = func.greatest(
            func.coalesce(agents.c.ended_at, occurred_at), occurred_at
        )
    if lifetime:
        connection.execute(update(agents).where(agents.c.id == agent_id).values(**lifetime))

    newer = is_newer(occurred_at, sequence, row.last_event_occurred_at, row.last_event_sequence)
    current = AgentActivity(row.turn_state, row.activity)
    # 과거 이벤트는 저장만 하고 현재 상태를 되돌리지 않는다.
    changed = next_activity(current, event_type, activity, confidence) if newer else current
    values: dict[str, object] = {}
    if newer:
        values.update(last_event_occurred_at=occurred_at, last_event_sequence=sequence)
    confidence_changed = newer and changed != current and row.confidence != confidence
    if changed != current:
        values.update(
            turn_state=changed.turn_state,
            activity=changed.activity,
            confidence=confidence,
            state_version=row.state_version + 1,
        )
    if changed != current or lifetime or confidence_changed:
        # 화면에 보이는 값이 바뀌었을 때만 실시간 전달 대상이 된다.
        values.update(change_seq=_next_change(), updated_at=func.clock_timestamp())
    if values:
        connection.execute(update(states).where(states.c.agent_id == agent_id).values(**values))


def _next_change() -> object:
    return func.nextval("state_change_seq")

"""인증된 이벤트 배치 수집(#35).

계약: `packages/event-schema`의 이벤트 v1·배치 요청/응답 v1(`docs/api/event-ingestion-v1.md`).
장치와 소유자는 장치 토큰으로 서버가 판정한다. 응답은 DB 커밋이 끝난 뒤에만 보내므로
`accepted_event_ids`·`duplicate_event_ids`는 영속화가 끝난 이벤트만 담는다(영속 ACK).
"""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import threading
import time
import uuid
from collections import deque
from typing import Annotated, Literal

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator
from sqlalchemy import Connection, func, select, update
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.exc import SQLAlchemyError

from agent_office.api.deps import CurrentDevice, DbConnection, DeviceDep
from agent_office.db import schema

MAX_BATCH_EVENTS = 500
RETENTION_WINDOW = dt.timedelta(days=30)
MAX_FUTURE_SKEW = dt.timedelta(days=1)
RATE_LIMIT_REQUESTS = 120
RATE_LIMIT_WINDOW_SECONDS = 60.0
MAX_SAFE_INTEGER = 9_007_199_254_740_991

router = APIRouter(prefix="/api/v1/events", tags=["events"])

Provider = Literal["codex", "claude", "unknown"]
Source = Literal["hook", "jsonl", "app_server", "inferred"]
EventType = Literal[
    "session.discovered",
    "session.started",
    "session.ended",
    "turn.started",
    "turn.completed",
    "turn.interrupted",
    "tool.started",
    "tool.completed",
    "tool.failed",
    "approval.requested",
    "approval.resolved",
    "agent.started",
    "agent.completed",
    "plan.updated",
    "unknown",
]
Activity = Literal[
    "reading",
    "browsing",
    "coding",
    "testing",
    "planning",
    "delegating",
    "reporting",
    "thinking",
    "waiting_user",
    "idle",
    "unknown",
]
Confidence = Literal["observed", "inferred"]
RejectionCode = Literal[
    "unauthorized_device",
    "unauthorized_scope",
    "unsupported_schema_version",
    "retention_window_exceeded",
    "event_id_conflict",
    "parent_agent_unknown",
    "parent_agent_conflict",
    "occurred_at_out_of_range",
    "internal_error",
]
# RFC 3339 시간대(Z 또는 ±HH:MM)가 반드시 붙은 문자열만 받는다.
OccurredAt = Annotated[
    str, StringConstraints(max_length=40, pattern=r"^\d{4}-\d{2}-\d{2}T.+(?:Z|[+-]\d{2}:\d{2})$")
]


class EventV1(BaseModel):
    """이벤트 v1. 필드가 닫혀 있어 원문·식별자 추가 필드를 받지 않는다."""

    model_config = ConfigDict(extra="forbid", frozen=True)

    schema_version: Literal["1.0"]
    event_id: uuid.UUID
    device_id: uuid.UUID
    session_id: uuid.UUID
    agent_id: uuid.UUID
    parent_agent_id: uuid.UUID | None
    provider: Provider
    source: Source
    event_type: EventType
    activity: Activity
    confidence: Confidence
    occurred_at: OccurredAt
    sequence: Annotated[int, Field(strict=True, ge=0, le=MAX_SAFE_INTEGER)]

    @model_validator(mode="after")
    def _inferred_source_is_inferred(self) -> EventV1:
        if self.source == "inferred" and self.confidence != "inferred":
            raise ValueError("inferred source requires inferred confidence")
        if self.parent_agent_id == self.agent_id:
            raise ValueError("agent cannot be its own parent")
        return self

    def occurred_datetime(self) -> dt.datetime:
        return dt.datetime.fromisoformat(self.occurred_at.replace("Z", "+00:00"))

    def digest(self) -> bytes:
        """정규 직렬화의 SHA-256. 같은 시각의 다른 표기는 같은 값이 된다."""
        canonical = self.model_dump(mode="json")
        canonical["occurred_at"] = self.occurred_datetime().astimezone(dt.UTC).isoformat()
        return hashlib.sha256(
            json.dumps(canonical, sort_keys=True, separators=(",", ":")).encode()
        ).digest()


class BatchRequestV1(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"]
    events: Annotated[list[EventV1], Field(min_length=1, max_length=MAX_BATCH_EVENTS)]

    @model_validator(mode="after")
    def _unique_items(self) -> BatchRequestV1:
        # JSON Schema `uniqueItems`와 같게 완전히 같은 이벤트 객체의 반복을 거부한다.
        if len(set(self.events)) != len(self.events):
            raise ValueError("events must be unique")
        return self


class RejectedEventV1(BaseModel):
    model_config = ConfigDict(extra="forbid")

    event_id: uuid.UUID
    code: RejectionCode
    retryable: bool


class BatchResponseV1(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"] = "1.0"
    accepted_event_ids: list[uuid.UUID] = []
    duplicate_event_ids: list[uuid.UUID] = []
    rejected_events: list[RejectedEventV1] = []


class DeviceRateLimiter:
    """장치별 고정 창 제한. 단일 서버 프로세스 기준이며 다중 인스턴스는 #63에서 재검토한다."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._hits: dict[uuid.UUID, deque[float]] = {}

    def check(self, device_id: uuid.UUID) -> float | None:
        """허용되면 None, 아니면 재시도까지 남은 초."""
        now = time.monotonic()
        with self._lock:
            hits = self._hits.setdefault(device_id, deque())
            while hits and now - hits[0] >= RATE_LIMIT_WINDOW_SECONDS:
                hits.popleft()
            if len(hits) >= RATE_LIMIT_REQUESTS:
                return RATE_LIMIT_WINDOW_SECONDS - (now - hits[0])
            hits.append(now)
            return None

    def reset(self) -> None:
        with self._lock:
            self._hits.clear()


rate_limiter = DeviceRateLimiter()


def limit_rate(device: DeviceDep) -> CurrentDevice:
    retry_after = rate_limiter.check(device.id)
    if retry_after is not None:
        raise HTTPException(
            status.HTTP_429_TOO_MANY_REQUESTS,
            detail="rate_limited",
            headers={"Retry-After": str(max(1, int(retry_after) + 1))},
        )
    return device


class _Deferred(Exception):
    """부모 에이전트가 아직 없다. 같은 배치의 다른 이벤트를 처리한 뒤 다시 시도한다."""


class _Rejected(Exception):
    def __init__(self, code: RejectionCode, retryable: bool) -> None:
        self.code: RejectionCode = code
        self.retryable = retryable


def _session_id(connection: Connection, device_id: uuid.UUID, event: EventV1) -> uuid.UUID:
    sessions = schema.agent_sessions
    connection.execute(
        insert(sessions)
        .values(
            device_id=device_id,
            provider=event.provider,
            collector_session_id=event.session_id,
        )
        .on_conflict_do_nothing(index_elements=["device_id", "provider", "collector_session_id"])
    )
    found: uuid.UUID = connection.execute(
        select(sessions.c.id).where(
            sessions.c.device_id == device_id,
            sessions.c.provider == event.provider,
            sessions.c.collector_session_id == event.session_id,
        )
    ).scalar_one()
    return found


def _agent_id(connection: Connection, session_id: uuid.UUID, event: EventV1) -> uuid.UUID:
    agents = schema.agents
    parent = agents.alias("parent")
    existing = connection.execute(
        select(agents.c.id, parent.c.collector_agent_id.label("parent_collector_id"))
        .select_from(agents.outerjoin(parent, parent.c.id == agents.c.parent_agent_id))
        .where(agents.c.session_id == session_id, agents.c.collector_agent_id == event.agent_id)
    ).one_or_none()
    if existing is not None:
        # 부모는 처음 관측된 값으로 고정한다. 바뀌면 순환·허위 계층이 생길 수 있다.
        if existing.parent_collector_id != event.parent_agent_id:
            raise _Rejected("parent_agent_conflict", retryable=False)
        agent_id: uuid.UUID = existing.id
        return agent_id

    parent_id = None
    if event.parent_agent_id is not None:
        parent_id = connection.execute(
            select(agents.c.id).where(
                agents.c.session_id == session_id,
                agents.c.collector_agent_id == event.parent_agent_id,
            )
        ).scalar_one_or_none()
        if parent_id is None:
            raise _Deferred()
    connection.execute(
        insert(agents)
        .values(
            session_id=session_id,
            collector_agent_id=event.agent_id,
            parent_agent_id=parent_id,
            role="lead" if parent_id is None else "subagent",
        )
        .on_conflict_do_nothing(index_elements=["session_id", "collector_agent_id"])
    )
    created: uuid.UUID = connection.execute(
        select(agents.c.id).where(
            agents.c.session_id == session_id, agents.c.collector_agent_id == event.agent_id
        )
    ).scalar_one()
    return created


def _store(
    connection: Connection, device: CurrentDevice, event: EventV1, now: dt.datetime
) -> Literal["accepted", "duplicate"]:
    if event.device_id != device.id:
        raise _Rejected("unauthorized_device", retryable=False)
    occurred = event.occurred_datetime()
    if occurred < now - RETENTION_WINDOW:
        raise _Rejected("retention_window_exceeded", retryable=False)
    if occurred > now + MAX_FUTURE_SKEW:
        raise _Rejected("occurred_at_out_of_range", retryable=False)

    events = schema.activity_events
    digest = event.digest()
    stored = connection.execute(
        select(events.c.payload_digest).where(events.c.event_id == event.event_id)
    ).scalar_one_or_none()
    if stored is not None:
        if stored == digest:
            return "duplicate"
        raise _Rejected("event_id_conflict", retryable=False)

    session_id = _session_id(connection, device.id, event)
    agent_id = _agent_id(connection, session_id, event)
    inserted = connection.execute(
        insert(events)
        .values(
            event_id=event.event_id,
            device_id=device.id,
            session_id=session_id,
            agent_id=agent_id,
            schema_version=event.schema_version,
            provider=event.provider,
            source=event.source,
            event_type=event.event_type,
            activity=event.activity,
            confidence=event.confidence,
            occurred_at=occurred,
            sequence=event.sequence,
            payload_digest=digest,
        )
        .on_conflict_do_nothing(index_elements=["event_id"])
        .returning(events.c.event_id)
    ).scalar_one_or_none()
    if inserted is None:
        # 동시 요청이 먼저 저장했다. 내용이 같으면 중복, 다르면 충돌이다.
        raced = connection.execute(
            select(events.c.payload_digest).where(events.c.event_id == event.event_id)
        ).scalar_one()
        if raced != digest:
            raise _Rejected("event_id_conflict", retryable=False)
        return "duplicate"

    sessions = schema.agent_sessions
    connection.execute(
        update(sessions)
        .where(sessions.c.id == session_id)
        .values(
            last_event_at=func.greatest(func.coalesce(sessions.c.last_event_at, occurred), occurred)
        )
    )
    return "accepted"


# 본문 크기(1MiB)는 `agent_office.http_limits.BodySizeLimitMiddleware`가 본문을 읽기 전에 제한한다.
@router.post("/batch", response_model=BatchResponseV1)
def ingest_batch(
    request: BatchRequestV1,
    device: Annotated[CurrentDevice, Depends(limit_rate)],
    connection: DbConnection,
) -> BatchResponseV1:
    response = BatchResponseV1()
    now = connection.execute(select(func.now())).scalar_one()
    pending = list(request.events)
    while pending:
        deferred: list[EventV1] = []
        for event in pending:
            try:
                with connection.begin_nested():
                    outcome = _store(connection, device, event, now)
            except _Deferred:
                deferred.append(event)
                continue
            except _Rejected as rejection:
                response.rejected_events.append(
                    RejectedEventV1(
                        event_id=event.event_id, code=rejection.code, retryable=rejection.retryable
                    )
                )
                continue
            except SQLAlchemyError:
                response.rejected_events.append(
                    RejectedEventV1(event_id=event.event_id, code="internal_error", retryable=True)
                )
                continue
            target = (
                response.accepted_event_ids
                if outcome == "accepted"
                else response.duplicate_event_ids
            )
            if event.event_id not in target:
                target.append(event.event_id)
        if len(deferred) == len(pending):
            # 더 진행할 수 없다. 부모가 다음 배치에 올 수 있으므로 재시도 가능으로 돌려준다.
            response.rejected_events.extend(
                RejectedEventV1(event_id=e.event_id, code="parent_agent_unknown", retryable=True)
                for e in deferred
            )
            break
        pending = deferred

    connection.execute(
        update(schema.devices)
        .where(schema.devices.c.id == device.id)
        .values(last_seen_at=func.now())
    )
    return response

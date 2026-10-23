"""이벤트 배치 수집 API 통합·계약 테스트(#35). DB 테스트는 PostgreSQL이 필요하다."""

from __future__ import annotations

import datetime as dt
import json
import uuid
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import jsonschema
import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register, sign_in
from pydantic import ValidationError
from sqlalchemy import Engine, func, select

from agent_office.api import events as events_api
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app

ROOT = Path(__file__).resolve().parents[3]
SCHEMA_DIR = ROOT / "packages" / "event-schema" / "schema"
FIXTURES = ROOT / "packages" / "event-schema" / "fixtures"
BATCH = "/api/v1/events/batch"


# --- 계약(DB 불필요) -------------------------------------------------------------


def _load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def test_valid_contract_fixtures_parse() -> None:
    events_api.EventV1.model_validate(_load(FIXTURES / "valid" / "event-observed.json"))
    events_api.EventV1.model_validate(_load(FIXTURES / "valid" / "event-child-inferred.json"))
    events_api.BatchRequestV1.model_validate(_load(FIXTURES / "valid" / "batch-request.json"))
    events_api.BatchResponseV1.model_validate(_load(FIXTURES / "valid" / "batch-response.json"))


@pytest.mark.parametrize(
    "name",
    [
        "event-additional-content.json",
        "event-bad-id.json",
        "event-inferred-observed.json",
        "event-naive-time.json",
        "event-sequence-overflow.json",
        "event-unsupported-enum.json",
    ],
)
def test_invalid_event_fixtures_are_rejected(name: str) -> None:
    with pytest.raises(ValidationError):
        events_api.EventV1.model_validate(_load(FIXTURES / "invalid" / name))


@pytest.mark.parametrize("name", ["batch-request-duplicate.json", "batch-request-empty.json"])
def test_invalid_batch_fixtures_are_rejected(name: str) -> None:
    with pytest.raises(ValidationError):
        events_api.BatchRequestV1.model_validate(_load(FIXTURES / "invalid" / name))


def test_rejection_codes_match_response_schema() -> None:
    schema_doc = _load(SCHEMA_DIR / "collector-event-batch-response-v1.schema.json")
    codes = schema_doc["$defs"]["rejectedEvent"]["properties"]["code"]["enum"]

    assert list(events_api.RejectionCode.__args__) == codes


def test_digest_ignores_time_notation_but_not_content() -> None:
    base = _load(FIXTURES / "valid" / "event-observed.json")
    same_instant = {**base, "occurred_at": base["occurred_at"].replace("Z", "+00:00")}
    changed = {**base, "activity": "testing"}

    digest = events_api.EventV1.model_validate(base).digest()

    assert events_api.EventV1.model_validate(same_instant).digest() == digest
    assert events_api.EventV1.model_validate(changed).digest() != digest


# --- API(PostgreSQL) -------------------------------------------------------------


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


def event(
    device_id: str,
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    parent: uuid.UUID | None = None,
    **overrides: object,
) -> dict[str, object]:
    values: dict[str, object] = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": device_id,
        "session_id": str(session_id),
        "agent_id": str(agent_id),
        "parent_agent_id": str(parent) if parent else None,
        "provider": "claude",
        "source": "hook",
        "event_type": "tool.started",
        "activity": "reading",
        "confidence": "observed",
        "occurred_at": dt.datetime.now(dt.UTC).isoformat(),
        "sequence": 1,
    }
    values.update(overrides)
    return values


def send(client: TestClient, token: str, events: list[dict[str, object]]) -> Any:
    response = client.post(
        BATCH, json={"schema_version": "1.0", "events": events}, headers=bearer(token)
    )
    assert response.status_code == 200, response.text
    body = response.json()
    jsonschema.validate(body, _load(SCHEMA_DIR / "collector-event-batch-response-v1.schema.json"))
    return body


def count(engine: Engine, table: Any) -> int:
    with engine.connect() as connection:
        return int(connection.execute(select(func.count()).select_from(table)).scalar_one())


def test_batch_is_stored_once_and_resends_are_duplicates(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    session, lead, child = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    # 자식 이벤트가 부모보다 먼저 와도 같은 배치 안에서 해결된다.
    batch = [
        event(device["device_id"], session, child, parent=lead, event_type="agent.started"),
        event(device["device_id"], session, lead, event_type="session.started"),
    ]

    first = send(client, device["device_token"], batch)
    assert sorted(first["accepted_event_ids"]) == sorted(e["event_id"] for e in batch)
    assert first["rejected_events"] == []

    # 마스터 사양 SLO: 100회 재전송에도 DB 중복 0건.
    for _ in range(100):
        again = send(client, device["device_token"], batch)
        assert again["accepted_event_ids"] == []
        assert sorted(again["duplicate_event_ids"]) == sorted(e["event_id"] for e in batch)

    assert count(engine, schema.activity_events) == 2
    assert count(engine, schema.agent_sessions) == 1
    with engine.connect() as connection:
        agents = connection.execute(
            select(schema.agents.c.collector_agent_id, schema.agents.c.role)
        ).all()
    assert {(row.collector_agent_id, row.role) for row in agents} == {
        (lead, "lead"),
        (child, "subagent"),
    }


def test_conflicting_content_for_same_event_id_is_rejected(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    original = event(device["device_id"], uuid.uuid4(), uuid.uuid4())
    send(client, device["device_token"], [original])

    changed = {**original, "activity": "coding"}
    body = send(client, device["device_token"], [changed])

    assert body["rejected_events"] == [
        {"event_id": original["event_id"], "code": "event_id_conflict", "retryable": False}
    ]
    with engine.connect() as connection:
        stored = connection.execute(select(schema.activity_events.c.activity)).scalar_one()
    assert stored == "reading"


def test_events_cannot_claim_another_device(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    other = event(str(uuid.uuid4()), uuid.uuid4(), uuid.uuid4())

    body = send(client, device["device_token"], [other])

    assert body["rejected_events"][0]["code"] == "unauthorized_device"
    assert count(engine, schema.activity_events) == 0


def test_same_collector_session_id_on_two_devices_stays_separate(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    _, other_session = sign_in(engine)
    other = register(client, new_code(client, other_session), name="pc-other")
    shared_session, shared_agent = uuid.uuid4(), uuid.uuid4()

    send(client, device["device_token"], [event(device["device_id"], shared_session, shared_agent)])
    send(client, other["device_token"], [event(other["device_id"], shared_session, shared_agent)])

    assert count(engine, schema.agent_sessions) == 2
    assert count(engine, schema.agents) == 2


def test_missing_parent_is_retryable_and_resolves_later(
    client: TestClient, engine: Engine, device: dict[str, str]
) -> None:
    session, lead, child = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    orphan = event(device["device_id"], session, child, parent=lead)

    first = send(client, device["device_token"], [orphan])
    assert first["rejected_events"] == [
        {"event_id": orphan["event_id"], "code": "parent_agent_unknown", "retryable": True}
    ]

    second = send(
        client, device["device_token"], [event(device["device_id"], session, lead), orphan]
    )
    assert len(second["accepted_event_ids"]) == 2


def test_parent_is_immutable(client: TestClient, engine: Engine, device: dict[str, str]) -> None:
    session, lead, other_lead, child = (uuid.uuid4() for _ in range(4))
    send(
        client,
        device["device_token"],
        [
            event(device["device_id"], session, lead),
            event(device["device_id"], session, other_lead),
            event(device["device_id"], session, child, parent=lead),
        ],
    )

    moved = event(device["device_id"], session, child, parent=other_lead)
    promoted = event(device["device_id"], session, child)
    body = send(client, device["device_token"], [moved, promoted])

    assert {r["code"] for r in body["rejected_events"]} == {"parent_agent_conflict"}
    assert len(body["rejected_events"]) == 2


def test_occurred_at_window(client: TestClient, engine: Engine, device: dict[str, str]) -> None:
    now = dt.datetime.now(dt.UTC)
    old = event(
        device["device_id"],
        uuid.uuid4(),
        uuid.uuid4(),
        occurred_at=(now - dt.timedelta(days=31)).isoformat(),
    )
    future = event(
        device["device_id"],
        uuid.uuid4(),
        uuid.uuid4(),
        occurred_at=(now + dt.timedelta(days=2)).isoformat(),
    )
    skewed = event(
        device["device_id"],
        uuid.uuid4(),
        uuid.uuid4(),
        occurred_at=(now + dt.timedelta(minutes=5)).isoformat(),
    )

    body = send(client, device["device_token"], [old, future, skewed])

    codes = {r["event_id"]: (r["code"], r["retryable"]) for r in body["rejected_events"]}
    assert codes == {
        old["event_id"]: ("retention_window_exceeded", False),
        future["event_id"]: ("occurred_at_out_of_range", False),
    }
    assert body["accepted_event_ids"] == [skewed["event_id"]]


@pytest.mark.parametrize(
    "mutate",
    [
        lambda e: {**e, "prompt": "SENSITIVE"},
        lambda e: {**e, "occurred_at": "2026-10-23T00:00:00"},
        lambda e: {**e, "provider": "other"},
        lambda e: {**e, "source": "inferred"},
        lambda e: {**e, "sequence": "1"},
    ],
)
def test_invalid_payload_rejects_whole_batch(
    client: TestClient, engine: Engine, device: dict[str, str], mutate: Any
) -> None:
    good = event(device["device_id"], uuid.uuid4(), uuid.uuid4())
    bad = mutate(event(device["device_id"], uuid.uuid4(), uuid.uuid4()))

    response = client.post(
        BATCH,
        json={"schema_version": "1.0", "events": [good, bad]},
        headers=bearer(device["device_token"]),
    )

    assert response.status_code == 422
    assert "SENSITIVE" not in response.text
    assert count(engine, schema.activity_events) == 0


def test_batch_size_limits(client: TestClient, device: dict[str, str]) -> None:
    empty = client.post(
        BATCH, json={"schema_version": "1.0", "events": []}, headers=bearer(device["device_token"])
    )
    too_many = client.post(
        BATCH,
        json={
            "schema_version": "1.0",
            "events": [event(device["device_id"], uuid.uuid4(), uuid.uuid4()) for _ in range(501)],
        },
        headers=bearer(device["device_token"]),
    )

    assert empty.status_code == 422
    assert too_many.status_code == 422


def test_body_size_is_limited_before_reading(client: TestClient, device: dict[str, str]) -> None:
    oversized = client.post(
        BATCH,
        content=b"{" + b" " * 1_048_576 + b"}",
        headers={**bearer(device["device_token"]), "Content-Type": "application/json"},
    )
    chunked = client.post(
        BATCH,
        content=iter([b'{"schema_version": "1.0", "events": []}']),
        headers={**bearer(device["device_token"]), "Content-Type": "application/json"},
    )

    assert oversized.status_code == 413
    assert chunked.status_code == 411


def test_authentication_and_rate_limit(
    client: TestClient, device: dict[str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
    payload = {
        "schema_version": "1.0",
        "events": [event(device["device_id"], uuid.uuid4(), uuid.uuid4())],
    }
    assert client.post(BATCH, json=payload).status_code == 401
    assert client.post(BATCH, json=payload, headers=bearer("aod_forged")).status_code == 401

    monkeypatch.setattr(events_api, "RATE_LIMIT_REQUESTS", 2)
    assert (
        client.post(BATCH, json=payload, headers=bearer(device["device_token"])).status_code == 200
    )
    assert (
        client.post(BATCH, json=payload, headers=bearer(device["device_token"])).status_code == 200
    )
    limited = client.post(BATCH, json=payload, headers=bearer(device["device_token"]))
    assert limited.status_code == 429
    assert int(limited.headers["Retry-After"]) >= 1

    assert (
        client.post("/api/v1/devices/revoke", headers=bearer(device["device_token"])).status_code
        == 204
    )
    events_api.rate_limiter.reset()
    assert (
        client.post(BATCH, json=payload, headers=bearer(device["device_token"])).status_code == 401
    )

"""계획 단계 수집·타임라인 페이지 테스트(#38). PostgreSQL이 필요하다."""

from __future__ import annotations

import datetime as dt
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine

from agent_office.api import events as events_api
from agent_office.db.engine import get_engine
from agent_office.main import app

BASE = dt.datetime.now(dt.UTC) - dt.timedelta(hours=1)


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


class Stream:
    def __init__(self, client: TestClient, engine: Engine) -> None:
        self.client = client
        _, self.session = sign_in(engine)
        self.device = register(client, new_code(client, self.session))
        self.sid, self.lead = uuid.uuid4(), uuid.uuid4()
        self.sequence = 0

    def send(self, event_type: str, minute: int, **extra: Any) -> dict[str, Any]:
        self.sequence += 1
        event = {
            "schema_version": "1.0",
            "event_id": str(uuid.uuid4()),
            "device_id": self.device["device_id"],
            "session_id": str(self.sid),
            "agent_id": str(self.lead),
            "parent_agent_id": None,
            "provider": "claude",
            "source": "hook",
            "event_type": event_type,
            "activity": extra.pop("activity", "unknown"),
            "confidence": "observed",
            "occurred_at": (BASE + dt.timedelta(minutes=minute)).isoformat(),
            "sequence": self.sequence,
            **extra,
        }
        response = self.client.post(
            "/api/v1/events/batch",
            json={"schema_version": "1.0", "events": [event]},
            headers=bearer(self.device["device_token"]),
        )
        assert response.status_code == 200, response.text
        return dict(response.json())

    def agent_id(self) -> str:
        snapshot = self.client.get("/api/v1/office", headers=session_cookie(self.session)).json()
        return str(snapshot["agents"][0]["id"])

    def get(self, path: str) -> Any:
        return self.client.get(path, headers=session_cookie(self.session))


def test_plan_updates_replace_steps_and_ignore_late_ones(
    client: TestClient, engine: Engine
) -> None:
    stream = Stream(client, engine)
    stream.send("session.started", 0)
    result = stream.send(
        "plan.updated", 2, activity="planning", plan_steps=["completed", "in_progress", "pending"]
    )
    assert result["rejected_events"] == []
    agent = stream.agent_id()
    detail = stream.get(f"/api/v1/office/agents/{agent}").json()
    assert detail["plan_steps"] == [
        {"step_index": 0, "status": "completed"},
        {"step_index": 1, "status": "in_progress"},
        {"step_index": 2, "status": "pending"},
    ]

    # 오프라인 큐에서 늦게 온 과거 계획은 현재 계획을 되돌리지 않는다.
    stream.send("plan.updated", 1, plan_steps=["pending"])
    assert len(stream.get(f"/api/v1/office/agents/{agent}").json()["plan_steps"]) == 3
    stream.send("plan.updated", 3, plan_steps=["completed", "completed"])
    steps = stream.get(f"/api/v1/office/agents/{agent}").json()["plan_steps"]
    assert [s["status"] for s in steps] == ["completed", "completed"]


def test_plan_steps_only_on_plan_events(client: TestClient, engine: Engine) -> None:
    stream = Stream(client, engine)
    event = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": stream.device["device_id"],
        "session_id": str(stream.sid),
        "agent_id": str(stream.lead),
        "parent_agent_id": None,
        "provider": "claude",
        "source": "hook",
        "event_type": "tool.started",
        "activity": "reading",
        "confidence": "observed",
        "occurred_at": BASE.isoformat(),
        "sequence": 1,
        "plan_steps": ["pending"],
    }
    response = client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": [event]},
        headers=bearer(stream.device["device_token"]),
    )
    assert response.status_code == 422


def test_timeline_pages_with_cursor(client: TestClient, engine: Engine) -> None:
    stream = Stream(client, engine)
    stream.send("session.started", 0)
    for minute in range(1, 6):
        stream.send("tool.started", minute, activity="reading")
    agent = stream.agent_id()
    path = f"/api/v1/office/agents/{agent}/timeline"

    first = stream.get(f"{path}?limit=4").json()
    assert len(first["entries"]) == 4 and first["next"]
    second = stream.get(f"{path}?limit=4&before={first['next']}").json()
    assert [e["event_type"] for e in second["entries"]] == ["tool.started", "session.started"]
    assert second["next"] is None
    times = [e["occurred_at"] for e in first["entries"] + second["entries"]]
    assert times == sorted(times, reverse=True)
    assert set(first["entries"][0]) == {
        "event_type",
        "activity",
        "confidence",
        "source",
        "occurred_at",
    }

    assert stream.get(f"{path}?before=not-a-cursor").status_code == 400
    assert stream.get(f"{path}?limit=500").status_code == 422
    other = Stream(client, engine)
    assert other.get(path).status_code == 404


def test_team_timeline_requires_shared_session(client: TestClient, engine: Engine) -> None:
    stream = Stream(client, engine)
    stream.send("session.started", 0)
    agent = stream.agent_id()
    team = client.post(
        "/api/v1/teams",
        json={"name": "t", "share_mode": "selected"},
        headers={**session_cookie(stream.session), "X-CSRF-Token": _csrf(stream.session)},
    ).json()["id"]
    # 공유하지 않은 세션은 팀 타임라인에 없다.
    assert stream.get(f"/api/v1/teams/{team}/office/agents/{agent}/timeline").status_code == 404


def _csrf(session: str) -> str:
    from agent_office.auth.sessions import csrf_token_for

    return csrf_token_for(session)

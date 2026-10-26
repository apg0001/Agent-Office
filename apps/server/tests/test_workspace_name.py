"""세션 작업 폴더 이름(#181) 수집·표시 범위 테스트. PostgreSQL이 필요하다."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register, session_cookie
from sqlalchemy import Engine
from test_office_live import live_headers, next_delta, owner
from test_team_office import Person, share, team_with

from agent_office.api import events as events_api
from agent_office.api import office
from agent_office.db.engine import get_engine
from agent_office.main import app

LIVE = "/api/v1/live"


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setattr(office, "POLL_INTERVAL_SECONDS", 0.1)
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()
    events_api.rate_limiter.reset()


def event(
    device: dict[str, str],
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    event_type: str,
    *,
    minute: int,
    workspace_name: str | None = None,
) -> dict[str, Any]:
    occurred = dt.datetime.now(dt.UTC) - dt.timedelta(minutes=30) + dt.timedelta(minutes=minute)
    body: dict[str, Any] = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": device["device_id"],
        "session_id": str(session_id),
        "agent_id": str(agent_id),
        "parent_agent_id": None,
        "provider": "claude",
        "source": "hook",
        "event_type": event_type,
        "activity": "unknown",
        "confidence": "observed",
        "occurred_at": occurred.isoformat(),
        "sequence": minute,
    }
    if workspace_name is not None:
        body["workspace_name"] = workspace_name
    return body


def post(client: TestClient, device: dict[str, str], *events: dict[str, Any]) -> Any:
    return client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": list(events)},
        headers=bearer(device["device_token"]),
    )


def accepted(client: TestClient, device: dict[str, str], *events: dict[str, Any]) -> None:
    response = post(client, device, *events)
    assert response.status_code == 200 and response.json()["rejected_events"] == []


def test_latest_workspace_name_is_shown_in_snapshot_live_and_detail(
    client: TestClient, engine: Engine
) -> None:
    session, device = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    accepted(client, device, event(device, sid, lead, "session.started", minute=0))
    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()
    assert snapshot["sessions"][0]["workspace_name"] is None

    accepted(
        client,
        device,
        event(device, sid, lead, "turn.started", minute=2, workspace_name="Agent-Office"),
    )
    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()
    assert snapshot["sessions"][0]["workspace_name"] == "Agent-Office"

    # 늦게 도착한 과거 이벤트는 이름을 되돌리지 않고, 이름 없는 이벤트는 지우지 않는다.
    accepted(
        client, device, event(device, sid, lead, "tool.started", minute=1, workspace_name="old")
    )
    accepted(client, device, event(device, sid, lead, "tool.started", minute=3))
    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()
    assert snapshot["sessions"][0]["workspace_name"] == "Agent-Office"

    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        assert ws.receive_json()["type"] == "snapshot"
        accepted(
            client,
            device,
            event(device, sid, lead, "turn.started", minute=4, workspace_name="블로그 repo"),
        )
        delta = next_delta(ws)
        assert delta["sessions"][0]["workspace_name"] == "블로그 repo"

    agent = snapshot["agents"][0]["id"]
    detail = client.get(f"/api/v1/office/agents/{agent}", headers=session_cookie(session)).json()
    assert detail["session"]["workspace_name"] == "블로그 repo"


@pytest.mark.parametrize(
    "value",
    [
        "/home/user/Agent-Office",
        "Agent/Office",
        "C:\\Users\\user",
        "a\x00b",
        "a\nb",
        "a\n",
        "a\x7f",
        "a\u202e",
        "a\u200b",
        ".",
        "..",
        "",
        "x" * 65,
    ],
)
def test_paths_and_control_characters_are_rejected(
    client: TestClient, engine: Engine, value: str
) -> None:
    session, device = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    response = post(
        client, device, event(device, sid, lead, "session.started", minute=0, workspace_name=value)
    )
    assert response.status_code == 422
    # 오류 응답에 입력 값을 되돌려 보내지 않는다.
    if len(value) > 2:
        assert value not in response.text
    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()
    assert snapshot["sessions"] == []


def test_digest_covers_workspace_name(client: TestClient, engine: Engine) -> None:
    _, device = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    first = event(device, sid, lead, "session.started", minute=0, workspace_name="blog")
    accepted(client, device, first)
    again = post(client, device, first).json()
    assert again["duplicate_event_ids"] == [first["event_id"]]
    changed = post(client, device, {**first, "workspace_name": "other"}).json()
    assert changed["rejected_events"][0]["code"] == "event_id_conflict"
    without = {k: v for k, v in first.items() if k != "workspace_name"}
    assert post(client, device, without).json()["rejected_events"][0]["code"] == (
        "event_id_conflict"
    )


def test_digest_without_workspace_name_is_unchanged() -> None:
    body = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": str(uuid.uuid4()),
        "session_id": str(uuid.uuid4()),
        "agent_id": str(uuid.uuid4()),
        "parent_agent_id": None,
        "provider": "claude",
        "source": "hook",
        "event_type": "session.started",
        "activity": "unknown",
        "confidence": "observed",
        "occurred_at": "2026-10-26T00:00:00Z",
        "sequence": 0,
    }
    digest = events_api.EventV1.model_validate(body).digest()
    canonical = events_api.EventV1.model_validate(body).model_dump(mode="json")
    canonical.pop("plan_steps")
    canonical.pop("workspace_name")
    canonical["occurred_at"] = "2026-10-26T00:00:00+00:00"
    expected = hashlib.sha256(
        json.dumps(canonical, sort_keys=True, separators=(",", ":")).encode()
    ).digest()
    assert digest == expected


def test_workspace_name_is_visible_only_to_device_owner(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob)
    device = register(bob.client, new_code(bob.client, bob.session), name="bob-pc")
    sid, lead = uuid.uuid4(), uuid.uuid4()
    accepted(
        client,
        device,
        event(device, sid, lead, "session.started", minute=0, workspace_name="secret-folder"),
    )
    own = bob.call("GET", "/office").json()
    assert own["sessions"][0]["workspace_name"] == "secret-folder"
    share(bob, own["sessions"][0]["id"], team)

    team_view = alice.call("GET", f"/teams/{team}/office").json()
    assert team_view["sessions"][0]["workspace_name"] is None
    assert "secret-folder" not in json.dumps(team_view)
    agent = team_view["agents"][0]["id"]
    detail = alice.call("GET", f"/teams/{team}/office/agents/{agent}").json()
    assert detail["session"]["workspace_name"] is None
    combined = alice.call("GET", "/office/combined").json()
    assert "secret-folder" not in json.dumps(combined)
    combined_detail = alice.call("GET", f"/office/combined/agents/{agent}").json()
    assert "secret-folder" not in json.dumps(combined_detail)

    # 소유자 본인은 팀 화면에서는 null(팀원과 같은 화면), 통합 사무실에서는 보인다.
    assert bob.call("GET", f"/teams/{team}/office").json()["sessions"][0]["workspace_name"] is None
    mine = bob.call("GET", "/office/combined").json()
    assert mine["sessions"][0]["workspace_name"] == "secret-folder"

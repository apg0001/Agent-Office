"""개인 사무실 스냅샷·WebSocket 실시간 테스트(#37). PostgreSQL이 필요하다."""

from __future__ import annotations

import datetime as dt
import json
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine
from starlette.websockets import WebSocketDisconnect

from agent_office.api import events as events_api
from agent_office.api import office
from agent_office.auth.sessions import SESSION_COOKIE
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


def owner(client: TestClient, engine: Engine) -> tuple[str, dict[str, str]]:
    _, session = sign_in(engine)
    return session, register(client, new_code(client, session))


def send_event(
    client: TestClient,
    device: dict[str, str],
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    event_type: str,
    *,
    minute: int,
    activity: str = "unknown",
) -> None:
    occurred = dt.datetime.now(dt.UTC) - dt.timedelta(minutes=30) + dt.timedelta(minutes=minute)
    event = {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": device["device_id"],
        "session_id": str(session_id),
        "agent_id": str(agent_id),
        "parent_agent_id": None,
        "provider": "claude",
        "source": "hook",
        "event_type": event_type,
        "activity": activity,
        "confidence": "observed",
        "occurred_at": occurred.isoformat(),
        "sequence": minute,
    }
    response = client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": [event]},
        headers=bearer(device["device_token"]),
    )
    assert response.status_code == 200 and response.json()["rejected_events"] == []


def live_headers(session: str) -> dict[str, str]:
    return {"cookie": f"{SESSION_COOKIE}={session}", "host": "testserver"}


def next_delta(ws: Any) -> dict[str, Any]:
    for _ in range(50):
        message = ws.receive_json()
        if message["type"] == "delta":
            return dict(message)
    raise AssertionError("no delta received")


def test_snapshot_contains_only_own_state_without_raw_content(
    client: TestClient, engine: Engine
) -> None:
    session, device = owner(client, engine)
    _, stranger = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    send_event(client, device, sid, lead, "session.started", minute=0)
    send_event(client, device, sid, lead, "tool.started", minute=1, activity="reading")
    send_event(client, stranger, uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)

    snapshot = client.get("/api/v1/office", headers=session_cookie(session)).json()

    assert snapshot["type"] == "snapshot"
    assert [d["id"] for d in snapshot["devices"]] == [device["device_id"]]
    assert snapshot["devices"][0]["connection"] == "ONLINE"
    assert len(snapshot["sessions"]) == 1 and snapshot["sessions"][0]["lifecycle"] == "OPEN"
    agent = snapshot["agents"][0]
    assert (agent["turn_state"], agent["activity"], agent["role"]) == ("WORKING", "reading", "lead")
    # 서버 ID만 노출한다. Collector가 보낸 세션·에이전트 ID와 토큰은 나오지 않는다.
    dumped = json.dumps(snapshot)
    assert str(sid) not in dumped and str(lead) not in dumped
    assert device["device_token"] not in dumped


def test_live_sends_snapshot_then_deltas(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    send_event(client, device, sid, lead, "session.started", minute=0)

    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        snapshot = ws.receive_json()
        assert snapshot["type"] == "snapshot"
        first_seq = snapshot["agents"][0]["change_seq"]

        send_event(client, device, sid, lead, "tool.started", minute=1, activity="coding")
        delta = next_delta(ws)
        agent = delta["agents"][0]
        assert (agent["turn_state"], agent["activity"]) == ("WORKING", "coding")
        assert agent["change_seq"] > first_seq

        send_event(client, device, sid, lead, "session.ended", minute=2)
        delta = next_delta(ws)
        assert delta["sessions"][0]["lifecycle"] == "ENDED"


def test_reconnect_restores_latest_state(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    send_event(client, device, sid, lead, "session.started", minute=0)
    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        ws.receive_json()

    # 연결이 끊긴 사이의 변경은 재접속 스냅샷으로 복원한다.
    send_event(client, device, sid, lead, "turn.started", minute=1)
    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        snapshot = ws.receive_json()
    assert snapshot["agents"][0]["turn_state"] == "THINKING"


@pytest.mark.parametrize(
    "headers",
    [
        {"host": "testserver"},
        {"cookie": f"{SESSION_COOKIE}=forged", "host": "testserver"},
    ],
)
def test_live_requires_valid_session(client: TestClient, headers: dict[str, str]) -> None:
    with (
        pytest.raises(WebSocketDisconnect) as closed,
        client.websocket_connect(LIVE, headers=headers),
    ):
        pass
    assert closed.value.code == office.CLOSE_UNAUTHORIZED


def test_live_rejects_cross_site_origin(client: TestClient, engine: Engine) -> None:
    session, _ = owner(client, engine)
    headers = {**live_headers(session), "origin": "https://evil.example"}

    with (
        pytest.raises(WebSocketDisconnect) as closed,
        client.websocket_connect(LIVE, headers=headers),
    ):
        pass
    assert closed.value.code == office.CLOSE_FORBIDDEN_ORIGIN

    same_site = {**live_headers(session), "origin": "http://testserver"}
    with client.websocket_connect(LIVE, headers=same_site) as ws:
        assert ws.receive_json()["type"] == "snapshot"


def test_logout_closes_live_connection(client: TestClient, engine: Engine) -> None:
    session, _ = owner(client, engine)

    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        ws.receive_json()
        response = client.post(
            "/api/v1/auth/logout", headers={**session_cookie(session), **csrf(session)}
        )
        assert response.status_code == 204
        with pytest.raises(WebSocketDisconnect) as closed:
            for _ in range(50):
                ws.receive_json()
    assert closed.value.code == office.CLOSE_UNAUTHORIZED


def test_other_users_changes_are_not_delivered(client: TestClient, engine: Engine) -> None:
    session, device = owner(client, engine)
    _, stranger = owner(client, engine)
    sid, lead = uuid.uuid4(), uuid.uuid4()
    send_event(client, device, sid, lead, "session.started", minute=0)

    with client.websocket_connect(LIVE, headers=live_headers(session)) as ws:
        ws.receive_json()
        send_event(client, stranger, uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)
        send_event(client, device, sid, lead, "turn.started", minute=1)
        delta = next_delta(ws)

    assert all(s["device_id"] == device["device_id"] for s in delta["sessions"])
    assert len(delta["agents"]) == 1

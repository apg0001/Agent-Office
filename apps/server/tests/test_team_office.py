"""팀 사무실 스냅샷·실시간과 공유 철회 즉시 차단 테스트(#54, #55). PostgreSQL이 필요하다."""

from __future__ import annotations

import json
import re
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import csrf, new_code, register, session_cookie, sign_in
from sqlalchemy import Engine, select, update
from starlette.websockets import WebSocketDisconnect
from test_office_live import live_headers, send_event

from agent_office.api import events as events_api
from agent_office.api import office
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app


@pytest.fixture
def client(engine: Engine, monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setattr(office, "POLL_INTERVAL_SECONDS", 0.1)
    get_engine.cache_clear()
    events_api.rate_limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


class Person:
    def __init__(self, client: TestClient, engine: Engine, name: str) -> None:
        self.client = client
        self.id, self.session = sign_in(engine)
        with engine.begin() as connection:
            connection.execute(
                update(schema.users).where(schema.users.c.id == self.id).values(display_name=name)
            )
        self.headers = {**session_cookie(self.session), **csrf(self.session)}

    def call(self, method: str, path: str, body: Any = None) -> Any:
        return self.client.request(method, f"/api/v1{path}", json=body, headers=self.headers)


def team_with(owner: Person, *members: Person) -> str:
    team = owner.call("POST", "/teams", {"name": "팀"}).json()["id"]
    for member in members:
        invited = owner.call("POST", f"/teams/{team}/invitations", {"email": email_of(member)})
        token = re.search(r"#token=(.+)$", invited.json()["invite_url"])
        assert token
        assert (
            member.call("POST", "/invitations/accept", {"token": token.group(1)}).status_code == 200
        )
    return str(team)


def email_of(person: Person) -> str:
    with get_engine().connect() as connection:
        return str(
            connection.execute(
                select(schema.users.c.email).where(schema.users.c.id == person.id)
            ).scalar_one()
        )


def working_session(person: Person, device_name: str = "secret-laptop") -> str:
    """장치를 등록하고 세션 하나를 연 뒤, 그 세션의 서버 ID를 돌려준다."""
    device = register(person.client, new_code(person.client, person.session), name=device_name)
    send_event(person.client, device, uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)
    snapshot = person.call("GET", "/office").json()
    return str(snapshot["sessions"][-1]["id"])


def share(owner: Person, session_id: str, team: str) -> str:
    project = owner.call("POST", "/projects", {"display_name": "공유 프로젝트"}).json()["id"]
    path = f"/office/sessions/{session_id}/project"
    assert owner.call("PUT", path, {"project_id": project}).status_code == 204
    assert owner.call("PUT", f"/projects/{project}/shares/{team}").status_code == 200
    return str(project)


def test_team_snapshot_shows_only_shared_sessions(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob)
    shared_session = working_session(bob)
    working_session(bob, "private-pc")  # 프로젝트에 넣지 않은 세션

    assert alice.call("GET", f"/teams/{team}/office").json()["sessions"] == []
    share(bob, shared_session, team)

    snapshot = alice.call("GET", f"/teams/{team}/office").json()
    assert snapshot["scope"] == {"kind": "team", "team_id": team}
    assert [s["id"] for s in snapshot["sessions"]] == [shared_session]
    assert snapshot["sessions"][0]["desk_slot"] == 0
    # 팀원에게는 PC 이름 대신 소유자 표시 이름만 보인다.
    assert [d["name"] for d in snapshot["devices"]] == ["Bob"]
    assert "secret-laptop" not in json.dumps(snapshot) and "private-pc" not in json.dumps(snapshot)
    assert len(snapshot["agents"]) == 1

    agent = snapshot["agents"][0]["id"]
    detail = alice.call("GET", f"/teams/{team}/office/agents/{agent}").json()
    assert detail["device"]["name"] == "Bob" and detail["timeline"]


def test_outsiders_and_foreign_sessions_are_hidden(client: TestClient, engine: Engine) -> None:
    alice, bob, eve = (Person(client, engine, n) for n in ("Alice", "Bob", "Eve"))
    team = team_with(alice, bob)
    session = working_session(bob)
    share(bob, session, team)
    agent = alice.call("GET", f"/teams/{team}/office").json()["agents"][0]["id"]

    assert eve.call("GET", f"/teams/{team}/office").status_code == 404
    assert eve.call("GET", f"/teams/{team}/office/agents/{agent}").status_code == 404
    assert alice.call("GET", f"/teams/{uuid.uuid4()}/office").status_code == 404
    # 남의 세션·남의 프로젝트에는 지정할 수 없다.
    eve_project = eve.call("POST", "/projects", {"display_name": "x"}).json()["id"]
    path = f"/office/sessions/{session}/project"
    assert eve.call("PUT", path, {"project_id": eve_project}).status_code == 404
    assert bob.call("PUT", path, {"project_id": eve_project}).status_code == 404


def test_unshare_and_removal_hide_sessions_immediately(client: TestClient, engine: Engine) -> None:
    alice, bob, carol = (Person(client, engine, n) for n in ("Alice", "Bob", "Carol"))
    team = team_with(alice, bob, carol)
    bob_project = share(bob, working_session(bob), team)
    share(carol, working_session(carol), team)
    assert len(alice.call("GET", f"/teams/{team}/office").json()["sessions"]) == 2

    bob.call("DELETE", f"/projects/{bob_project}/shares/{team}")
    names = [d["name"] for d in alice.call("GET", f"/teams/{team}/office").json()["devices"]]
    assert names == ["Carol"]

    # 추방되면 그 사람의 공유도 거둬진다.
    assert alice.call("DELETE", f"/teams/{team}/members/{carol.id}").status_code == 204
    assert alice.call("GET", f"/teams/{team}/office").json()["sessions"] == []
    assert carol.call("GET", "/projects").json()[0]["shared_team_ids"] == []


def test_team_update_diff() -> None:
    base = {"devices": [], "sessions": [{"id": "s1", "change_seq": 1}], "agents": []}
    same = json.loads(json.dumps(base))
    changed = {**base, "sessions": [{"id": "s1", "change_seq": 2}]}
    removed = {**base, "sessions": []}

    assert office.team_update(base, same) is None
    delta = office.team_update(base, changed)
    assert delta and delta["type"] == "delta" and delta["sessions"] == changed["sessions"]
    assert office.team_update(base, removed) is removed


def test_team_live_revokes_on_unshare_and_removal(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob)
    project = share(alice, working_session(alice), team)
    url = f"{office.LIVE_PATH}?team={team}"

    with client.websocket_connect(url, headers=live_headers(bob.session)) as ws:
        first = ws.receive_json()
        assert first["type"] == "snapshot" and len(first["sessions"]) == 1

        alice.call("DELETE", f"/projects/{project}/shares/{team}")
        message = next_non_ping(ws)
        assert message["type"] == "snapshot" and message["sessions"] == []

        assert alice.call("DELETE", f"/teams/{team}/members/{bob.id}").status_code == 204
        with pytest.raises(WebSocketDisconnect) as closed:
            for _ in range(50):
                ws.receive_json()
    assert closed.value.code == office.CLOSE_SCOPE_UNAVAILABLE


def test_team_live_rejects_non_members(client: TestClient, engine: Engine) -> None:
    alice, eve = Person(client, engine, "Alice"), Person(client, engine, "Eve")
    team = team_with(alice)
    for target in (team, "not-a-uuid"):
        with (
            pytest.raises(WebSocketDisconnect) as closed,
            client.websocket_connect(
                f"{office.LIVE_PATH}?team={target}", headers=live_headers(eve.session)
            ),
        ):
            pass
        assert closed.value.code == office.CLOSE_SCOPE_UNAVAILABLE


def next_non_ping(ws: Any) -> dict[str, Any]:
    for _ in range(50):
        message = dict(ws.receive_json())
        if message["type"] != "ping":
            return message
    raise AssertionError("no message")

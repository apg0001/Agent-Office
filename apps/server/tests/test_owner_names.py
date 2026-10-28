"""팀·통합 사무실의 사람 이름표(#206). PostgreSQL이 필요하다."""

from __future__ import annotations

import json

from fastapi.testclient import TestClient
from sqlalchemy import Engine
from test_office_live import live_headers
from test_team_office import Person, client, email_of, share, team_with, working_session

from agent_office.api import office

__all__ = ["client"]


def test_team_payload_has_owner_name_without_pc_name_or_email(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob)
    bob_session = working_session(bob, "bob-secret-pc")
    share(bob, bob_session, team)
    share(alice, working_session(alice, "alice-pc"), team)

    snapshot = alice.call("GET", f"/teams/{team}/office").json()
    # 팀 화면은 내 장치도 표시 이름(팀원과 같은 화면), 사람 이름표도 표시 이름.
    assert sorted((d["name"], d["owner_name"]) for d in snapshot["devices"]) == [
        ("Alice", "Alice"),
        ("Bob", "Bob"),
    ]
    dumped = json.dumps(snapshot)
    assert "bob-secret-pc" not in dumped and "alice-pc" not in dumped
    assert email_of(bob) not in dumped and email_of(alice) not in dumped

    bob_agent = next(a["id"] for a in snapshot["agents"] if a["session_id"] == bob_session)
    detail = alice.call("GET", f"/teams/{team}/office/agents/{bob_agent}").json()
    assert detail["device"]["owner_name"] == "Bob" and detail["device"]["name"] == "Bob"
    assert email_of(bob) not in json.dumps(detail) and "bob-secret-pc" not in json.dumps(detail)


def test_combined_payload_names_me_and_teammates(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob)
    own = working_session(alice, "alice-pc")
    share(bob, working_session(bob, "bob-laptop"), team)

    combined = alice.call("GET", "/office/combined").json()
    # 내 장치는 PC 이름 + 내 표시 이름, 남의 장치는 표시 이름만.
    assert sorted((d["name"], d["owner_name"]) for d in combined["devices"]) == [
        ("Bob", "Bob"),
        ("alice-pc", "Alice"),
    ]
    dumped = json.dumps(combined)
    assert "bob-laptop" not in dumped and email_of(bob) not in dumped

    own_agent = next(a["id"] for a in combined["agents"] if a["session_id"] == own)
    detail = alice.call("GET", f"/office/combined/agents/{own_agent}").json()
    assert detail["device"]["name"] == "alice-pc" and detail["device"]["owner_name"] == "Alice"

    # 개인 사무실은 내 PC만 있으므로 사람 이름을 담지 않는다.
    personal = alice.call("GET", "/office").json()
    assert all("owner_name" not in d for d in personal["devices"])
    assert "owner_name" not in alice.call("GET", f"/office/agents/{own_agent}").json()["device"]


def test_renamed_device_reaches_personal_live(client: TestClient, engine: Engine) -> None:
    alice = Person(client, engine, "Alice")
    working_session(alice, "old-name")
    device = alice.call("GET", "/office").json()["devices"][0]["id"]
    with client.websocket_connect(office.LIVE_PATH, headers=live_headers(alice.session)) as ws:
        assert ws.receive_json()["type"] == "snapshot"
        assert alice.call("PATCH", f"/devices/{device}", {"name": "new-name"}).status_code == 200
        for _ in range(50):
            message = ws.receive_json()
            if message["type"] == "delta" and message["devices"]:
                assert message["devices"][0]["name"] == "new-name"
                break
        else:
            raise AssertionError("rename delta not received")

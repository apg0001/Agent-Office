"""팀별 'PC 이름 공개' 옵션(`team_members.share_device_name`, 마이그레이션 0011) 테스트.

PostgreSQL이 필요하다. 값은 모두 합성이다.
"""

from __future__ import annotations

import json
import re
import uuid
from typing import Any

from alembic import command
from conftest import alembic_config
from fastapi.testclient import TestClient
from sqlalchemy import Engine, create_engine, inspect, select, text
from test_office_live import live_headers
from test_share_modes import client, device_of, open_session, set_mode, team_office
from test_team_office import Person, email_of, next_non_ping, team_with

from agent_office.api import office
from agent_office.db import schema

__all__ = ["client"]


def set_name_sharing(person: Person, team: str, value: bool) -> Any:
    return person.call("PUT", f"/teams/{team}/share-mode", {"share_device_name": value})


def device_names(snapshot: dict[str, Any]) -> list[tuple[str, str]]:
    return sorted((d["name"], d["owner_name"]) for d in snapshot["devices"])


def join(owner: Person, team: str, member: Person) -> None:
    invited = owner.call("POST", f"/teams/{team}/invitations", {"email": email_of(member)})
    token = re.search(r"#token=(.+)$", invited.json()["invite_url"])
    assert token
    accepted = member.call("POST", "/invitations/accept", {"token": token.group(1)})
    assert accepted.status_code == 200, accepted.text


def member_columns(engine: Engine) -> set[str]:
    with engine.connect() as connection:
        return {c["name"] for c in inspect(connection).get_columns("team_members")}


# --- 기본값·API ----------------------------------------------------------------------


def test_default_is_off_for_new_teams_and_joined_members(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    created = alice.call("POST", "/teams", {"name": "기본"}).json()
    assert created["share_device_name"] is False
    team = created["id"]
    join(alice, team, bob)
    assert bob.call("GET", "/teams").json()[0]["share_device_name"] is False
    detail = bob.call("GET", f"/teams/{team}").json()
    assert detail["share_device_name"] is False
    # 다른 구성원의 값은 어디에도 나오지 않는다.
    assert all(
        set(m) == {"user_id", "display_name", "role", "joined_at"} for m in detail["members"]
    )
    # 팀 만들기 본문으로는 켤 수 없다(알 수 없는 필드는 거부).
    bad = alice.call("POST", "/teams", {"name": "x", "share_device_name": True})
    assert bad.status_code == 422


def test_only_i_can_change_my_option_and_fields_are_optional(
    client: TestClient, engine: Engine
) -> None:
    alice, bob, eve = (Person(client, engine, n) for n in ("Alice", "Bob", "Eve"))
    team = team_with(alice, bob, mode="all")
    path = f"/teams/{team}/share-mode"

    # Alice(OWNER)가 켜도 Alice 자신의 값만 바뀐다. 경로에 남을 지정할 방법이 없다.
    changed = set_name_sharing(alice, team, True)
    assert changed.status_code == 200 and changed.json()["share_device_name"] is True
    assert changed.json()["share_mode"] == "all"  # 보내지 않은 필드는 그대로
    assert bob.call("GET", f"/teams/{team}").json()["share_device_name"] is False
    # 구성원이 아니면 없는 팀과 같다.
    assert set_name_sharing(eve, team, True).status_code == 404
    members = schema.team_members
    with engine.connect() as connection:
        rows = connection.execute(
            select(members.c.user_id, members.c.share_device_name).where(
                members.c.team_id == uuid.UUID(team)
            )
        ).all()
    assert {r.user_id: r.share_device_name for r in rows} == {alice.id: True, bob.id: False}

    # 이전 클라이언트처럼 share_mode만 보내도 되고, PC 이름 공개 값은 바뀌지 않는다.
    only_mode = set_mode(alice, team, "selected").json()
    assert (only_mode["share_mode"], only_mode["share_device_name"]) == ("selected", True)
    body = {"share_mode": "all", "share_device_name": False}
    both = alice.call("PUT", path, body).json()
    assert (both["share_mode"], both["share_device_name"]) == ("all", False)

    # 빈 본문·잘못된 값·CSRF 없음은 거부하고 값은 그대로다.
    assert alice.call("PUT", path, {}).status_code == 422
    assert alice.call("PUT", path, {"share_device_name": "maybe"}).status_code == 422
    no_csrf = {k: v for k, v in alice.headers.items() if k.lower() == "cookie"}
    response = client.put(
        f"/api/v1/teams/{team}/share-mode", json={"share_device_name": True}, headers=no_csrf
    )
    assert response.status_code == 403
    assert alice.call("GET", "/teams").json()[0]["share_device_name"] is False


def test_change_is_audited_only_when_value_changes_and_without_pc_name(
    client: TestClient, engine: Engine
) -> None:
    bob = Person(client, engine, "Bob")
    team = team_with(bob, mode="all")
    open_session(bob, device_of(bob, "bob-secret-pc"))
    for value in (True, True, False, False):
        assert set_name_sharing(bob, team, value).status_code == 200
    with engine.connect() as connection:
        logs = connection.execute(
            select(schema.audit_logs)
            .where(schema.audit_logs.c.actor_user_id == bob.id)
            .order_by(schema.audit_logs.c.id)
        ).all()
    changes = [log for log in logs if log.action == "team.device_name_sharing_changed"]
    assert [log.details["reason"] for log in changes] == ["on", "off"]
    assert all(str(log.target_id) == team and log.target_type == "team" for log in changes)
    assert "bob-secret-pc" not in json.dumps([dict(log.details or {}) for log in logs])


# --- 팀 화면 -------------------------------------------------------------------------


def test_team_office_shows_pc_name_only_when_owner_opted_in(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    _, bob_session = open_session(bob, device_of(bob, "bob-secret-pc"), workspace_name="folder")
    open_session(alice, device_of(alice, "alice-pc"), workspace_name="alice-folder")

    # 기본(끔): 남의 장치는 표시 이름, 내 장치만 PC 이름.
    before = team_office(alice, team)
    assert device_names(before) == [("Bob", "Bob"), ("alice-pc", "Alice")]
    assert "bob-secret-pc" not in json.dumps(before)
    agent = next(a["id"] for a in before["agents"] if a["session_id"] == bob_session)
    path = f"/teams/{team}/office/agents/{agent}"
    detail = alice.call("GET", path).json()
    assert (detail["device"]["name"], detail["device"]["owner_name"]) == ("Bob", "Bob")
    assert "bob-secret-pc" not in json.dumps(detail)

    # 켬: 팀원에게 PC 이름이 보이고 사람 이름표는 그대로 표시 이름.
    assert set_name_sharing(bob, team, True).status_code == 200
    shown = team_office(alice, team)
    assert device_names(shown) == [("alice-pc", "Alice"), ("bob-secret-pc", "Bob")]
    detail = alice.call("GET", path).json()
    assert (detail["device"]["name"], detail["device"]["owner_name"]) == ("bob-secret-pc", "Bob")
    # Bob이 켰다고 Alice의 PC 이름이 Bob에게 보이지는 않는다.
    seen_by_bob = team_office(bob, team)
    assert device_names(seen_by_bob) == [("Alice", "Alice"), ("bob-secret-pc", "Bob")]
    assert "alice-pc" not in json.dumps(seen_by_bob)

    # 팀 화면의 작업 폴더 이름은 옵션과 무관하게 본인에게도 null이다.
    for viewer in (alice, bob):
        snapshot = team_office(viewer, team)
        assert all(s["workspace_name"] is None for s in snapshot["sessions"])
    own_detail = bob.call("GET", path).json()
    assert own_detail["device"]["name"] == "bob-secret-pc"
    assert own_detail["session"]["workspace_name"] is None

    # 끄면 다시 숨겨진다.
    assert set_name_sharing(bob, team, False).status_code == 200
    hidden = team_office(alice, team)
    assert device_names(hidden) == [("Bob", "Bob"), ("alice-pc", "Alice")]
    assert "bob-secret-pc" not in json.dumps(hidden)
    detail = alice.call("GET", path).json()
    assert detail["device"]["name"] == "Bob" and "bob-secret-pc" not in json.dumps(detail)


def test_option_is_per_team(client: TestClient, engine: Engine) -> None:
    alice, bob, carol = (Person(client, engine, n) for n in ("Alice", "Bob", "Carol"))
    open_team = team_with(alice, bob, mode="all")
    other_team = team_with(carol, bob, mode="all")
    _, session = open_session(bob, device_of(bob, "bob-secret-pc"))
    assert set_name_sharing(bob, open_team, True).status_code == 200

    assert device_names(team_office(alice, open_team)) == [("bob-secret-pc", "Bob")]
    # 같은 세션이 보이는 다른 팀(옵션 끔)에는 PC 이름이 나가지 않는다.
    other = team_office(carol, other_team)
    assert [s["id"] for s in other["sessions"]] == [session]
    assert device_names(other) == [("Bob", "Bob")]
    assert "bob-secret-pc" not in json.dumps(other)
    agent = other["agents"][0]["id"]
    detail = carol.call("GET", f"/teams/{other_team}/office/agents/{agent}").json()
    assert detail["device"]["name"] == "Bob" and "bob-secret-pc" not in json.dumps(detail)
    combined = carol.call("GET", "/office/combined").json()
    assert device_names(combined) == [("Bob", "Bob")]
    assert "bob-secret-pc" not in json.dumps(combined)
    combined_detail = carol.call("GET", f"/office/combined/agents/{agent}").json()
    assert combined_detail["device"]["name"] == "Bob"
    assert "bob-secret-pc" not in json.dumps(combined_detail)


def test_team_live_sends_name_change_as_device_delta(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    open_session(bob, device_of(bob, "bob-secret-pc"))
    open_session(alice, device_of(alice, "alice-pc"), workspace_name="alice-folder")

    url = f"{office.LIVE_PATH}?team={team}"
    with client.websocket_connect(url, headers=live_headers(alice.session)) as ws:
        first = ws.receive_json()
        # 실시간도 보는 사람 기준: 내 장치는 PC 이름, 작업 폴더 이름은 null.
        assert device_names(first) == [("Bob", "Bob"), ("alice-pc", "Alice")]
        assert all(s["workspace_name"] is None for s in first["sessions"])

        set_name_sharing(bob, team, True)
        message = next_non_ping(ws)
        assert message["type"] == "delta" and message["sessions"] == []
        assert device_names(message) == [("bob-secret-pc", "Bob")]

        set_name_sharing(bob, team, False)
        message = next_non_ping(ws)
        assert message["type"] == "delta"
        assert device_names(message) == [("Bob", "Bob")]


def test_team_update_sends_changed_device_name_as_delta() -> None:
    device = {"id": "d1", "name": "Bob", "owner_name": "Bob", "connection": "ONLINE"}
    base: dict[str, Any] = {"devices": [device], "sessions": [], "agents": []}
    shown = {**base, "devices": [{**device, "name": "bob-pc"}]}
    delta = office.team_update(base, shown)
    assert delta == {"type": "delta", "devices": shown["devices"], "sessions": [], "agents": []}
    back = office.team_update(shown, base)
    assert back is not None and back["devices"] == [device]


# --- 통합 화면 -----------------------------------------------------------------------


def test_combined_office_follows_the_teams_that_show_the_session(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    first, second = team_with(alice, bob, mode="all"), team_with(alice, bob, mode="all")
    _, own = open_session(alice, device_of(alice, "alice-pc"), workspace_name="alice-folder")
    _, bob_session = open_session(bob, device_of(bob, "bob-secret-pc"))
    hidden_names = [("Bob", "Bob"), ("alice-pc", "Alice")]

    combined = alice.call("GET", "/office/combined").json()
    assert device_names(combined) == hidden_names
    assert "bob-secret-pc" not in json.dumps(combined)
    agent = next(a["id"] for a in combined["agents"] if a["session_id"] == bob_session)
    path = f"/office/combined/agents/{agent}"
    assert alice.call("GET", path).json()["device"]["name"] == "Bob"

    # 함께 속한 팀 가운데 하나에서만 켜도 통합 화면에는 PC 이름이 보인다(어느 팀이 먼저든).
    assert set_name_sharing(bob, second, True).status_code == 200
    combined = alice.call("GET", "/office/combined").json()
    assert device_names(combined) == [("alice-pc", "Alice"), ("bob-secret-pc", "Bob")]
    detail = alice.call("GET", path).json()
    assert (detail["device"]["name"], detail["device"]["owner_name"]) == ("bob-secret-pc", "Bob")
    assert detail["session"]["workspace_name"] is None
    # 옵션을 켜지 않은 팀의 화면은 그대로다.
    assert device_names(team_office(alice, first)) == hidden_names
    # 통합 화면의 내 세션 작업 폴더 이름은 계속 내게 보인다.
    mine = next(s for s in combined["sessions"] if s["id"] == own)
    assert mine["workspace_name"] == "alice-folder"

    # 옵션을 켠 팀에서 그 세션이 보이지 않게 되면(공유 안 함) 그 팀의 옵션은 쓰이지 않는다.
    assert set_mode(bob, second, "none").status_code == 200
    combined = alice.call("GET", "/office/combined").json()
    assert bob_session in [s["id"] for s in combined["sessions"]]
    assert device_names(combined) == hidden_names
    assert "bob-secret-pc" not in json.dumps(combined)
    assert alice.call("GET", path).json()["device"]["name"] == "Bob"

    # 다시 보이게 한 뒤 그 팀을 나가면 숨겨진다(다른 팀으로는 세션이 계속 보인다).
    assert set_mode(bob, second, "all").status_code == 200
    combined = alice.call("GET", "/office/combined").json()
    assert ("bob-secret-pc", "Bob") in device_names(combined)
    assert bob.call("DELETE", f"/teams/{second}/members/{bob.id}").status_code == 204
    combined = alice.call("GET", "/office/combined").json()
    assert bob_session in [s["id"] for s in combined["sessions"]]
    assert device_names(combined) == hidden_names
    assert "bob-secret-pc" not in json.dumps(combined)
    assert alice.call("GET", path).json()["device"]["name"] == "Bob"


def test_combined_live_hides_name_again_when_option_is_turned_off(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    open_session(bob, device_of(bob, "bob-secret-pc"))
    set_name_sharing(bob, team, True)

    url = f"{office.LIVE_PATH}?view=combined"
    with client.websocket_connect(url, headers=live_headers(alice.session)) as ws:
        assert device_names(ws.receive_json()) == [("bob-secret-pc", "Bob")]
        set_name_sharing(bob, team, False)
        message = next_non_ping(ws)
        assert message["type"] == "delta"
        assert device_names(message) == [("Bob", "Bob")]


def test_personal_office_is_unchanged(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    open_session(bob, device_of(bob, "bob-secret-pc"), workspace_name="folder")
    set_name_sharing(bob, team, True)

    personal = bob.call("GET", "/office").json()
    assert [d["name"] for d in personal["devices"]] == ["bob-secret-pc"]
    assert all("owner_name" not in d for d in personal["devices"])
    assert personal["sessions"][0]["workspace_name"] == "folder"
    # 팀원의 개인 사무실에는 여전히 내 것이 없다.
    assert alice.call("GET", "/office").json()["devices"] == []
    agent = personal["agents"][0]["id"]
    assert alice.call("GET", f"/office/agents/{agent}").status_code == 404


# --- 마이그레이션 --------------------------------------------------------------------


def test_migration_0011_adds_a_false_default_and_downgrades(database: str) -> None:
    config = alembic_config()
    command.upgrade(config, "0010")
    engine = create_engine(database)
    try:
        with engine.begin() as connection:
            user = connection.execute(
                text(
                    "INSERT INTO users (email, display_name, status)"
                    " VALUES ('old@example.invalid', 'Old', 'active') RETURNING id"
                )
            ).scalar_one()
            team = connection.execute(
                text("INSERT INTO teams (name) VALUES ('old') RETURNING id")
            ).scalar_one()
            connection.execute(
                text("INSERT INTO team_members (team_id, user_id, role) VALUES (:t, :u, 'OWNER')"),
                {"t": team, "u": user},
            )
        assert "share_device_name" not in member_columns(engine)

        command.upgrade(config, "0011")
        assert "share_device_name" in member_columns(engine)
        with engine.begin() as connection:
            # 기존 구성원은 끔으로 채워지고, 다른 값은 그대로다.
            rows = connection.execute(
                text("SELECT share_mode, share_device_name FROM team_members")
            ).all()
            assert [tuple(r) for r in rows] == [("all", False)]
            connection.execute(text("UPDATE team_members SET share_device_name = true"))

        command.downgrade(config, "0010")
        assert "share_device_name" not in member_columns(engine)
        with engine.connect() as connection:
            rows = connection.execute(text("SELECT role, share_mode FROM team_members")).all()
            assert [tuple(r) for r in rows] == [("OWNER", "all")]

        command.upgrade(config, "head")
        with engine.connect() as connection:
            flags = connection.execute(text("SELECT share_device_name FROM team_members"))
            assert flags.scalars().all() == [False]
    finally:
        engine.dispose()

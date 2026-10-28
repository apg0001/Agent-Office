"""팀원별 공유 범위(모두·선택·안 함)와 폴더 이름 프로젝트 자동 생성 테스트(#197).

PostgreSQL이 필요하다. 값은 모두 합성이다.
"""

from __future__ import annotations

import json
import re
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
from alembic import command
from conftest import alembic_config
from fastapi.testclient import TestClient
from helpers import new_code, register
from sqlalchemy import Engine, create_engine, select, text
from sqlalchemy.exc import IntegrityError
from test_office_live import live_headers
from test_team_office import Person, email_of, next_non_ping, team_with
from test_workspace_name import accepted, event

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
    events_api.rate_limiter.reset()


def device_of(person: Person, name: str) -> dict[str, str]:
    return register(person.client, new_code(person.client, person.session), name=name)


def open_session(
    person: Person, device: dict[str, str], workspace_name: str | None = None
) -> tuple[uuid.UUID, str]:
    """세션 하나를 열고 (Collector 세션 ID, 서버 세션 ID)를 돌려준다."""
    sid = uuid.uuid4()
    accepted(
        person.client,
        device,
        event(
            device, sid, uuid.uuid4(), "session.started", minute=0, workspace_name=workspace_name
        ),
    )
    with get_engine().connect() as connection:
        server_id = connection.execute(
            select(schema.agent_sessions.c.id).where(
                schema.agent_sessions.c.collector_session_id == sid
            )
        ).scalar_one()
    return sid, str(server_id)


def team_office(person: Person, team: str) -> dict[str, Any]:
    response = person.call("GET", f"/teams/{team}/office")
    assert response.status_code == 200, response.text
    return dict(response.json())


def session_ids(snapshot: dict[str, Any]) -> list[str]:
    return sorted(s["id"] for s in snapshot["sessions"])


def set_mode(person: Person, team: str, mode: str) -> Any:
    return person.call("PUT", f"/teams/{team}/share-mode", {"share_mode": mode})


# --- 기본값·마이그레이션 ---------------------------------------------------------------


def test_new_memberships_default_to_all_and_respect_the_choice(
    client: TestClient, engine: Engine
) -> None:
    alice, bob, carol = (Person(client, engine, n) for n in ("Alice", "Bob", "Carol"))
    created = alice.call("POST", "/teams", {"name": "기본"}).json()
    assert created["share_mode"] == "all"
    team = created["id"]
    for person, body in ((bob, {}), (carol, {"share_mode": "selected"})):
        invited = alice.call("POST", f"/teams/{team}/invitations", {"email": email_of(person)})
        token = re.search(r"#token=(.+)$", invited.json()["invite_url"])
        assert token
        joined = person.call("POST", "/invitations/accept", {"token": token.group(1), **body})
        assert joined.status_code == 200
    assert bob.call("GET", "/teams").json()[0]["share_mode"] == "all"
    assert carol.call("GET", "/teams").json()[0]["share_mode"] == "selected"
    # 상세에는 내 범위만 나오고 다른 구성원의 범위는 나오지 않는다.
    detail = carol.call("GET", f"/teams/{team}").json()
    assert detail["share_mode"] == "selected"
    assert all(
        set(m) == {"user_id", "display_name", "role", "joined_at"} for m in detail["members"]
    )

    chosen = Person(client, engine, "Dave").call(
        "POST", "/teams", {"name": "선택", "share_mode": "none"}
    )
    assert chosen.json()["share_mode"] == "none"
    bad = alice.call("POST", "/teams", {"name": "x", "share_mode": "everything"})
    assert bad.status_code == 422


def test_migration_0009_keeps_existing_members_on_selected(database: str) -> None:
    config = alembic_config()
    command.upgrade(config, "0008")
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
            project = connection.execute(
                text(
                    "INSERT INTO projects (owner_user_id, display_name) VALUES (:u, 'p')"
                    " RETURNING id"
                ),
                {"u": user},
            ).scalar_one()
            device = connection.execute(
                text("INSERT INTO devices (owner_user_id, name) VALUES (:u, 'pc') RETURNING id"),
                {"u": user},
            ).scalar_one()
            for assigned in (project, None):
                connection.execute(
                    text(
                        "INSERT INTO agent_sessions"
                        " (device_id, provider, collector_session_id, project_id)"
                        " VALUES (:d, 'claude', :c, :p)"
                    ),
                    {"d": device, "c": uuid.uuid4(), "p": assigned},
                )
        command.upgrade(config, "head")
        with engine.begin() as connection:
            assert connection.execute(
                text("SELECT share_mode FROM team_members")
            ).scalars().all() == ["selected"]
            assert connection.execute(text("SELECT origin FROM projects")).scalar_one() == "manual"
            manual = connection.execute(
                text("SELECT project_id IS NOT NULL, project_manual FROM agent_sessions")
            ).all()
            assert sorted(tuple(r) for r in manual) == [(False, False), (True, True)]
            # 마이그레이션 뒤 새 구성원 행의 기본값은 'all'이다.
            other = connection.execute(
                text(
                    "INSERT INTO users (email, display_name, status)"
                    " VALUES ('new@example.invalid', 'New', 'active') RETURNING id"
                )
            ).scalar_one()
            connection.execute(
                text("INSERT INTO team_members (team_id, user_id, role) VALUES (:t, :u, 'MEMBER')"),
                {"t": team, "u": other},
            )
            assert (
                connection.execute(
                    text("SELECT share_mode FROM team_members WHERE user_id = :u"), {"u": other}
                ).scalar_one()
                == "all"
            )
            with pytest.raises(IntegrityError), connection.begin_nested():
                connection.execute(text("UPDATE team_members SET share_mode = 'everything'"))
    finally:
        engine.dispose()


# --- 범위별 가시성 -------------------------------------------------------------------


def test_all_mode_shows_every_session_without_device_names(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    device = device_of(bob, "bob-secret-pc")
    _, unnamed = open_session(bob, device)
    _, foldered = open_session(bob, device, workspace_name="private-folder")

    snapshot = team_office(alice, team)
    assert session_ids(snapshot) == sorted([unnamed, foldered])
    assert [d["name"] for d in snapshot["devices"]] == ["Bob"]
    assert all(s["workspace_name"] is None for s in snapshot["sessions"])
    assert "bob-secret-pc" not in json.dumps(snapshot)
    by_id = {s["id"]: s for s in snapshot["sessions"]}
    # 프로젝트가 없으면 이름도 없다(화면은 "(이름 없음)"). 폴더 이름은 workspace_name이 아니라
    # 자동 생성된 프로젝트의 표시 이름으로만 보인다(모두 공유에 동의한 범위).
    assert (by_id[unnamed]["project_id"], by_id[unnamed]["project_name"]) == (None, None)
    assert by_id[foldered]["project_name"] == "private-folder"

    # 모두 공유면 그 사람의 프로젝트 이름(폴더 이름으로 만든 것 포함)이 팀에 보인다.
    projects = alice.call("GET", f"/teams/{team}/projects").json()
    assert [(p["display_name"], p["owner_display_name"]) for p in projects] == [
        ("private-folder", "Bob")
    ]
    agent = next(a["id"] for a in snapshot["agents"] if a["session_id"] == unnamed)
    detail = alice.call("GET", f"/teams/{team}/office/agents/{agent}").json()
    assert detail["device"]["name"] == "Bob" and detail["session"]["workspace_name"] is None


def test_selected_mode_shows_only_shared_projects(client: TestClient, engine: Engine) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="selected")
    device = device_of(bob, "bob-pc")
    _, blog = open_session(bob, device, workspace_name="blog")
    open_session(bob, device, workspace_name="secret-work")
    open_session(bob, device)
    assert team_office(alice, team)["sessions"] == []
    assert alice.call("GET", f"/teams/{team}/projects").json() == []

    blog_project = next(
        p for p in bob.call("GET", "/projects").json() if p["display_name"] == "blog"
    )
    assert blog_project["origin"] == "workspace"
    assert bob.call("PUT", f"/projects/{blog_project['id']}/shares/{team}").status_code == 200
    snapshot = team_office(alice, team)
    assert session_ids(snapshot) == [blog]
    assert snapshot["sessions"][0]["project_name"] == "blog"
    assert snapshot["sessions"][0]["workspace_name"] is None
    assert "secret-work" not in json.dumps(snapshot)
    names = [p["display_name"] for p in alice.call("GET", f"/teams/{team}/projects").json()]
    assert names == ["blog"]


def test_none_mode_hides_everything_even_shared_projects(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="selected")
    _, session = open_session(bob, device_of(bob, "bob-pc"), workspace_name="blog")
    project = bob.call("GET", "/projects").json()[0]["id"]
    bob.call("PUT", f"/projects/{project}/shares/{team}")
    assert session_ids(team_office(alice, team)) == [session]
    agent = team_office(alice, team)["agents"][0]["id"]

    assert set_mode(bob, team, "none").json()["share_mode"] == "none"
    assert team_office(alice, team)["sessions"] == []
    assert alice.call("GET", f"/teams/{team}/projects").json() == []
    assert alice.call("GET", f"/teams/{team}/office/agents/{agent}").status_code == 404
    timeline = alice.call("GET", f"/teams/{team}/office/agents/{agent}/timeline")
    assert timeline.status_code == 404
    assert alice.call("GET", "/office/combined").json()["sessions"] == []
    # 'selected'로 돌아오면 전에 체크한 공유가 다시 쓰인다.
    set_mode(bob, team, "selected")
    assert session_ids(team_office(alice, team)) == [session]


def test_no_cross_team_leak(client: TestClient, engine: Engine) -> None:
    alice, bob, carol, eve = (Person(client, engine, n) for n in ("Alice", "Bob", "Carol", "Eve"))
    open_team = team_with(alice, bob, mode="all")
    closed_team = team_with(carol, mode="all")
    invited = carol.call("POST", f"/teams/{closed_team}/invitations", {"email": email_of(bob)})
    token = re.search(r"#token=(.+)$", invited.json()["invite_url"])
    assert token
    body = {"token": token.group(1), "share_mode": "none"}
    assert bob.call("POST", "/invitations/accept", body).status_code == 200
    _, session = open_session(bob, device_of(bob, "bob-pc"), workspace_name="blog")

    assert session_ids(team_office(alice, open_team)) == [session]
    # 같은 사람이라도 'none'인 팀에는 보이지 않는다.
    assert team_office(carol, closed_team)["sessions"] == []
    assert carol.call("GET", f"/teams/{closed_team}/projects").json() == []
    assert carol.call("GET", "/office/combined").json()["sessions"] == []
    agent = team_office(alice, open_team)["agents"][0]["id"]
    assert carol.call("GET", f"/teams/{closed_team}/office/agents/{agent}").status_code == 404
    assert carol.call("GET", f"/office/combined/agents/{agent}").status_code == 404
    # 구성원이 아닌 팀은 범위 변경도 조회도 없는 팀과 같다.
    assert eve.call("GET", f"/teams/{open_team}/office").status_code == 404
    assert set_mode(eve, open_team, "all").status_code == 404
    assert carol.call("GET", f"/teams/{open_team}/projects").status_code == 404

    # 팀을 나가면 'all'이어도 바로 사라진다.
    assert bob.call("DELETE", f"/teams/{open_team}/members/{bob.id}").status_code == 204
    assert team_office(alice, open_team)["sessions"] == []


def test_share_mode_change_requires_csrf_and_valid_value(
    client: TestClient, engine: Engine
) -> None:
    alice = Person(client, engine, "Alice")
    team = team_with(alice, mode="all")
    no_csrf = {k: v for k, v in alice.headers.items() if k.lower() == "cookie"}
    response = client.put(
        f"/api/v1/teams/{team}/share-mode", json={"share_mode": "none"}, headers=no_csrf
    )
    assert response.status_code == 403
    assert set_mode(alice, team, "public").status_code == 422
    assert alice.call("GET", "/teams").json()[0]["share_mode"] == "all"


def test_mode_switch_applies_immediately_to_live_and_is_audited(
    client: TestClient, engine: Engine
) -> None:
    alice, bob = Person(client, engine, "Alice"), Person(client, engine, "Bob")
    team = team_with(alice, bob, mode="all")
    _, session = open_session(bob, device_of(bob, "bob-pc"))

    url = f"{office.LIVE_PATH}?team={team}"
    with client.websocket_connect(url, headers=live_headers(alice.session)) as ws:
        first = ws.receive_json()
        assert [s["id"] for s in first["sessions"]] == [session]

        assert set_mode(bob, team, "selected").status_code == 200
        message = next_non_ping(ws)
        assert message["type"] == "snapshot" and message["sessions"] == []

        assert set_mode(bob, team, "all").status_code == 200
        message = next_non_ping(ws)
        assert [s["id"] for s in message["sessions"]] == [session]

    with client.websocket_connect(
        f"{office.LIVE_PATH}?view=combined", headers=live_headers(alice.session)
    ) as ws:
        assert [s["id"] for s in ws.receive_json()["sessions"]] == [session]
        set_mode(bob, team, "none")
        message = next_non_ping(ws)
        assert message["type"] == "snapshot" and message["sessions"] == []

    # 같은 값으로 바꾸면 감사 로그를 남기지 않는다.
    set_mode(bob, team, "none")
    with engine.connect() as connection:
        logs = connection.execute(
            select(schema.audit_logs.c.action, schema.audit_logs.c.details)
            .where(
                schema.audit_logs.c.action == "team.share_mode_changed",
                schema.audit_logs.c.actor_user_id == bob.id,
            )
            .order_by(schema.audit_logs.c.id)
        ).all()
    assert [log.details["reason"] for log in logs] == [
        "all_to_selected",
        "selected_to_all",
        "all_to_none",
    ]


# --- 폴더 이름 프로젝트 자동 생성 --------------------------------------------------------


def projects_of(person: Person) -> list[dict[str, Any]]:
    return list(person.call("GET", "/projects").json())


def project_of_session(session_id: str) -> Any:
    with get_engine().connect() as connection:
        return connection.execute(
            select(schema.agent_sessions.c.project_id).where(
                schema.agent_sessions.c.id == uuid.UUID(session_id)
            )
        ).scalar_one()


def test_workspace_projects_are_created_once_per_owner_and_name(
    client: TestClient, engine: Engine
) -> None:
    bob, carol = Person(client, engine, "Bob"), Person(client, engine, "Carol")
    device, other = device_of(bob, "bob-pc"), device_of(bob, "bob-laptop")
    sid, first = open_session(bob, device, workspace_name="blog")
    _, second = open_session(bob, other, workspace_name="blog")
    _, unnamed = open_session(bob, device)
    for minute in (1, 2):
        accepted(
            client,
            device,
            event(device, sid, uuid.uuid4(), "turn.started", minute=minute, workspace_name="blog"),
        )
    _, carol_session = open_session(carol, device_of(carol, "carol-pc"), workspace_name="blog")

    mine = projects_of(bob)
    assert [(p["display_name"], p["origin"]) for p in mine] == [("blog", "workspace")]
    assert project_of_session(first) == project_of_session(second) == uuid.UUID(mine[0]["id"])
    assert project_of_session(unnamed) is None  # 폴더 이름이 없으면 프로젝트도 없다.
    # 소유자별로 따로 만든다.
    assert project_of_session(carol_session) == uuid.UUID(projects_of(carol)[0]["id"])
    assert projects_of(carol)[0]["id"] != mine[0]["id"]

    # 폴더 이름이 바뀌면 그 이름의 프로젝트로 옮긴다. 같은 이름의 수동 프로젝트는 다시 쓴다.
    manual = bob.call("POST", "/projects", {"display_name": "notes"}).json()
    accepted(
        client,
        device,
        event(device, sid, uuid.uuid4(), "turn.started", minute=5, workspace_name="notes"),
    )
    assert project_of_session(first) == uuid.UUID(manual["id"])
    assert sorted(p["display_name"] for p in projects_of(bob)) == ["blog", "notes"]


def test_manual_assignment_wins_over_workspace_project(client: TestClient, engine: Engine) -> None:
    bob = Person(client, engine, "Bob")
    device = device_of(bob, "bob-pc")
    sid, session = open_session(bob, device, workspace_name="blog")
    manual = bob.call("POST", "/projects", {"display_name": "직접"}).json()["id"]
    path = f"/office/sessions/{session}/project"
    assert bob.call("PUT", path, {"project_id": manual}).status_code == 204

    accepted(
        client,
        device,
        event(device, sid, uuid.uuid4(), "turn.started", minute=3, workspace_name="other"),
    )
    assert project_of_session(session) == uuid.UUID(manual)
    assert "other" not in [p["display_name"] for p in projects_of(bob)]

    # 직접 "없음"으로 둔 것도 존중한다.
    assert bob.call("PUT", path, {"project_id": None}).status_code == 204
    accepted(
        client,
        device,
        event(device, sid, uuid.uuid4(), "turn.started", minute=4, workspace_name="blog"),
    )
    assert project_of_session(session) is None

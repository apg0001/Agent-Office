"""팀 역할·초대·소유권 이전·프로젝트 공유 테스트(#27). PostgreSQL이 필요하다."""

from __future__ import annotations

import re
import uuid
from collections.abc import Iterator
from typing import Any

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import csrf, session_cookie, sign_in
from sqlalchemy import Engine, select, text, update

from agent_office.api import accounts as accounts_api
from agent_office.auth import accounts, mailer
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.main import app


class Person:
    def __init__(self, engine: Engine, role: str = "USER") -> None:
        self.id, self.session = sign_in(engine)
        with engine.begin() as connection:
            if role != "USER":
                connection.execute(
                    update(schema.users)
                    .where(schema.users.c.id == self.id)
                    .values(instance_role=role)
                )
            self.email = str(
                connection.execute(
                    select(schema.users.c.email).where(schema.users.c.id == self.id)
                ).scalar_one()
            )
        self.headers = {**session_cookie(self.session), **csrf(self.session)}


@pytest.fixture
def client(engine: Engine) -> Iterator[TestClient]:
    get_engine.cache_clear()
    mailer.set_mailer(None)
    for limiter in accounts_api.LIMITERS:
        limiter.reset()
    with TestClient(app) as test_client:
        yield test_client
    get_engine.cache_clear()


def req(
    client: TestClient, person: Person, method: str, path: str, body: Any = None
) -> httpx.Response:
    return client.request(method, f"/api/v1{path}", json=body, headers=person.headers)


def make_team(client: TestClient, owner: Person, name: str = "플랫폼") -> str:
    response = req(client, owner, "POST", "/teams", {"name": name})
    assert response.status_code == 201
    return str(response.json()["id"])


def code(client: TestClient, person: Person, method: str, path: str, body: Any = None) -> int:
    return req(client, person, method, path, body).status_code


def token_of(invited: httpx.Response) -> str:
    found = re.search(r"#token=(.+)$", invited.json()["invite_url"])
    assert found
    return found.group(1)


def invite(
    client: TestClient, team: str, inviter: Person, email: str, role: str = "MEMBER"
) -> httpx.Response:
    body = {"email": email, "role": role}
    return req(client, inviter, "POST", f"/teams/{team}/invitations", body)


def accept(client: TestClient, person: Person, token: str) -> int:
    return code(client, person, "POST", "/invitations/accept", {"token": token})


def join(
    client: TestClient, team: str, inviter: Person, person: Person, role: str = "MEMBER"
) -> None:
    invited = invite(client, team, inviter, person.email, role)
    assert invited.status_code == 201, invited.text
    assert accept(client, person, token_of(invited)) == 200


def test_owner_creates_team_and_members_see_it(client: TestClient, engine: Engine) -> None:
    owner, member, outsider = Person(engine), Person(engine), Person(engine)
    team = make_team(client, owner)
    join(client, team, owner, member)

    detail = req(client, member, "GET", f"/teams/{team}").json()
    assert detail["role"] == "MEMBER"
    assert sorted(m["role"] for m in detail["members"]) == ["MEMBER", "OWNER"]
    assert all("email" not in m for m in detail["members"])
    assert [t["id"] for t in req(client, member, "GET", "/teams").json()] == [team]
    # 남의 팀은 존재 여부도 드러나지 않는다.
    assert code(client, outsider, "GET", f"/teams/{team}") == 404
    assert code(client, outsider, "GET", f"/teams/{uuid.uuid4()}") == 404


def test_instance_admin_has_no_team_power(client: TestClient, engine: Engine) -> None:
    owner, admin = Person(engine), Person(engine, "SUPER_ADMIN")
    team = make_team(client, owner)

    assert code(client, admin, "GET", f"/teams/{team}") == 404
    assert code(client, admin, "DELETE", f"/teams/{team}") == 404


def test_role_permissions(client: TestClient, engine: Engine) -> None:
    owner, manager, member, other = Person(engine), Person(engine), Person(engine), Person(engine)
    team = make_team(client, owner)
    join(client, team, owner, manager, "MANAGER")
    join(client, team, manager, member)
    members = f"/teams/{team}/members"

    # MANAGER는 MEMBER만 초대하고, 역할 변경·팀 삭제는 못 한다.
    assert invite(client, team, manager, other.email, "MANAGER").status_code == 403
    assert code(client, manager, "PATCH", f"{members}/{member.id}", {"role": "MANAGER"}) == 403
    assert code(client, manager, "DELETE", f"/teams/{team}") == 403
    # MEMBER는 초대·수정 불가.
    assert invite(client, team, member, other.email).status_code == 403
    assert code(client, member, "PATCH", f"/teams/{team}", {"name": "x"}) == 403
    # MANAGER는 MEMBER를 내보낼 수 있지만 다른 MANAGER·OWNER는 못 한다.
    assert code(client, manager, "DELETE", f"{members}/{owner.id}") == 403
    assert code(client, manager, "DELETE", f"{members}/{member.id}") == 204
    # OWNER는 역할을 바꾸되 OWNER 지정은 이전으로만.
    assert code(client, owner, "PATCH", f"{members}/{manager.id}", {"role": "MEMBER"}) == 200
    assert code(client, owner, "PATCH", f"{members}/{manager.id}", {"role": "OWNER"}) == 422


def test_owner_must_transfer_before_leaving(client: TestClient, engine: Engine) -> None:
    owner, member = Person(engine), Person(engine)
    team = make_team(client, owner)
    join(client, team, owner, member)

    left = req(client, owner, "DELETE", f"/teams/{team}/members/{owner.id}")
    assert left.status_code == 409 and left.json()["detail"] == "owner_must_transfer"

    moved = req(client, owner, "POST", f"/teams/{team}/transfer", {"user_id": str(member.id)})
    assert moved.status_code == 200
    roles = {m["user_id"]: m["role"] for m in moved.json()["members"]}
    assert roles == {str(owner.id): "MANAGER", str(member.id): "OWNER"}
    assert code(client, owner, "DELETE", f"/teams/{team}/members/{owner.id}") == 204
    assert code(client, owner, "GET", f"/teams/{team}") == 404


def test_invitation_rules(client: TestClient, engine: Engine) -> None:
    owner, invitee, stranger = Person(engine), Person(engine), Person(engine)
    team = make_team(client, owner)
    first = invite(client, team, owner, invitee.email)
    second = invite(client, team, owner, invitee.email)
    assert first.json()["mailed"] is False

    # 재초대하면 이전 링크는 무효, 열린 초대는 하나.
    assert len(req(client, owner, "GET", f"/teams/{team}/invitations").json()) == 1
    assert accept(client, invitee, token_of(first)) == 400
    # 링크를 넘겨받은 다른 계정은 수락할 수 없다.
    assert accept(client, stranger, token_of(second)) == 400
    assert accept(client, invitee, token_of(second)) == 200
    # 한 번만 쓸 수 있고, 이미 구성원이면 다시 초대할 수 없다.
    assert accept(client, invitee, token_of(second)) == 400
    assert invite(client, team, owner, invitee.email).status_code == 409

    revoked = invite(client, team, owner, stranger.email)
    path = f"/teams/{team}/invitations/{revoked.json()['id']}"
    assert code(client, owner, "DELETE", path) == 204
    assert accept(client, stranger, token_of(revoked)) == 400

    expired = invite(client, team, owner, stranger.email)
    with engine.begin() as connection:
        connection.execute(
            text(
                "UPDATE team_invitations SET created_at = now() - interval '9 days',"
                " expires_at = now() - interval '1 day'"
                " WHERE accepted_at IS NULL AND revoked_at IS NULL"
            )
        )
    assert accept(client, stranger, token_of(expired)) == 400


def test_user_can_join_many_teams(client: TestClient, engine: Engine) -> None:
    alice, bob, carol = Person(engine), Person(engine), Person(engine)
    first = make_team(client, alice, "A")
    second = make_team(client, bob, "B")
    join(client, first, alice, carol)
    join(client, second, bob, carol, "MANAGER")

    teams = {t["name"]: t["role"] for t in req(client, carol, "GET", "/teams").json()}
    assert teams == {"A": "MEMBER", "B": "MANAGER"}


def test_project_sharing_requires_owner_and_membership(client: TestClient, engine: Engine) -> None:
    owner, teammate, outsider = Person(engine), Person(engine), Person(engine)
    team = make_team(client, owner)
    other_team = make_team(client, outsider, "다른 팀")
    join(client, team, owner, teammate)
    project = req(client, owner, "POST", "/projects", {"display_name": "결제 서비스"}).json()["id"]
    share = f"/projects/{project}/shares"

    # 남의 프로젝트는 공유할 수 없고, 내가 속하지 않은 팀에도 공유할 수 없다.
    assert code(client, teammate, "PUT", f"{share}/{team}") == 404
    assert code(client, owner, "PUT", f"{share}/{other_team}") == 404

    assert req(client, owner, "PUT", f"{share}/{team}").json()["shared_team_ids"] == [team]
    visible = req(client, teammate, "GET", f"/teams/{team}/projects").json()
    assert [p["display_name"] for p in visible] == ["결제 서비스"]
    assert code(client, outsider, "GET", f"/teams/{team}/projects") == 404

    assert req(client, owner, "DELETE", f"{share}/{team}").json()["shared_team_ids"] == []
    assert req(client, teammate, "GET", f"/teams/{team}/projects").json() == []


def test_invite_only_signup_needs_matching_invitation(
    client: TestClient, engine: Engine, monkeypatch: pytest.MonkeyPatch
) -> None:
    class Outbox:
        configured = True

        def send(self, mail: mailer.OutgoingMail) -> bool:
            return True

    mailer.set_mailer(Outbox())
    monkeypatch.setenv(mailer.PUBLIC_URL_ENV, "https://office.example")
    try:
        with engine.begin() as connection:
            connection.execute(
                text("INSERT INTO instance_settings (key, value) VALUES (:k, '\"INVITE_ONLY\"')"),
                {"k": accounts.SIGNUP_POLICY_KEY},
            )
        owner = Person(engine)
        token = token_of(invite(client, make_team(client, owner), owner, "new@example.com"))

        def signup(email: str, invite_token: str | None) -> int:
            body = {"email": email, "password": "long enough pass", "display_name": "N"}
            if invite_token is not None:
                body["invite_token"] = invite_token
            return client.post(
                "/api/v1/auth/signup", json=body, headers={"origin": "http://testserver"}
            ).status_code

        assert signup("new@example.com", None) == 403
        assert signup("other@example.com", token) == 403
        assert signup("new@example.com", token) == 202
    finally:
        mailer.set_mailer(None)


def test_requires_login_and_csrf(client: TestClient, engine: Engine) -> None:
    owner = Person(engine)
    assert client.get("/api/v1/teams").status_code == 401
    cookie_only = session_cookie(owner.session)
    assert client.post("/api/v1/teams", json={"name": "x"}, headers=cookie_only).status_code == 403

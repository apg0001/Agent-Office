"""역할 상승 시나리오(#64): USER→ADMIN, ADMIN→SUPER_ADMIN, 팀 MEMBER→MANAGER/OWNER.

인스턴스 역할과 팀 역할은 독립이다. 상위 역할의 동작은 하위 역할로 어떤 경로에서도 할 수 없다.
"""

from __future__ import annotations

from typing import Any

from fastapi.testclient import TestClient
from sqlalchemy import Engine, select

from agent_office.db import schema
from agent_office.main import app
from security.secworld import Actor, call, fill, make_user, routes_of


def _role_of(engine: Engine, user: Actor) -> str:
    with engine.connect() as connection:
        return str(
            connection.execute(
                select(schema.users.c.instance_role).where(schema.users.c.id == user.id)
            ).scalar_one()
        )


def _status_of(engine: Engine, user: Actor) -> str:
    with engine.connect() as connection:
        return str(
            connection.execute(
                select(schema.users.c.status).where(schema.users.c.id == user.id)
            ).scalar_one()
        )


def test_sec_role_001_user_cannot_reach_any_admin_route(client: TestClient, engine: Engine) -> None:
    """SEC-ROLE-001: 일반 사용자는 모든 관리 경로(설정·사용자·감사·알림·지표·백업)에서 403."""
    user = make_user(engine)
    routes = routes_of(app, "admin", "super_admin")
    assert len(routes) >= 20
    for route in routes:
        response = call(client, route, fill(route.path, {"user_id": str(user.id)}), user.headers)
        assert response.status_code == 403, (route.key, response.text)
        assert response.json() == {"detail": "forbidden"}
    assert _role_of(engine, user) == "USER"


def test_sec_role_002_admin_cannot_do_super_admin_actions(
    client: TestClient, engine: Engine
) -> None:
    """SEC-ROLE-002: ADMIN은 자기 승격·남의 승격·SUPER_ADMIN 강등·설치 완료 잠금을 할 수 없다."""
    admin = make_user(engine, role="ADMIN")
    super_admin = make_user(engine, role="SUPER_ADMIN")
    other_super = make_user(engine, role="SUPER_ADMIN")
    user = make_user(engine)
    attempts: list[tuple[str, str, dict[str, Any] | None]] = [
        ("PUT", f"/api/v1/admin/users/{admin.id}/role", {"instance_role": "SUPER_ADMIN"}),
        ("PUT", f"/api/v1/admin/users/{user.id}/role", {"instance_role": "ADMIN"}),
        ("PUT", f"/api/v1/admin/users/{super_admin.id}/role", {"instance_role": "USER"}),
        ("POST", "/api/v1/setup/complete", {}),
    ]
    for method, path, body in attempts:
        response = client.request(method, path, json=body, headers=admin.headers)
        assert response.status_code == 403, (path, response.text)
    assert _role_of(engine, admin) == "ADMIN"
    assert _role_of(engine, user) == "USER"
    assert _role_of(engine, super_admin) == "SUPER_ADMIN"
    assert other_super  # 마지막 SUPER_ADMIN 트리거와 무관하게 거부되는지 보기 위해 둘을 둔다.


def test_sec_role_003_admin_cannot_change_super_admin_status(
    client: TestClient, engine: Engine
) -> None:
    """SEC-ROLE-003(수정된 취약점): ADMIN이 SUPER_ADMIN을 비활성화·재활성화·승인할 수 없다.

    이전에는 SUPER_ADMIN이 둘 이상이면 ADMIN이 그중 하나를 비활성화(세션 폐기)할 수 있었다.
    """
    admin = make_user(engine, role="ADMIN")
    target = make_user(engine, role="SUPER_ADMIN")
    keeper = make_user(engine, role="SUPER_ADMIN")
    for action in ("disable", "enable", "approve"):
        response = client.post(f"/api/v1/admin/users/{target.id}/{action}", headers=admin.headers)
        assert response.status_code == 403, (action, response.text)
    assert _status_of(engine, target) == "active"
    assert client.get("/api/v1/auth/session", headers=target.cookie).status_code == 200

    # SUPER_ADMIN은 다른 SUPER_ADMIN을 비활성화할 수 있고, ADMIN은 그 계정을 되살릴 수 없다.
    disabled = client.post(f"/api/v1/admin/users/{target.id}/disable", headers=keeper.headers)
    assert disabled.status_code == 200
    revived = client.post(f"/api/v1/admin/users/{target.id}/enable", headers=admin.headers)
    assert revived.status_code == 403
    assert _status_of(engine, target) == "disabled"
    # 일반 사용자 관리는 ADMIN도 그대로 할 수 있다.
    user = make_user(engine)
    assert (
        client.post(f"/api/v1/admin/users/{user.id}/disable", headers=admin.headers).status_code
        == 200
    )


def _team(client: TestClient, owner: Actor, *members: tuple[Actor, str]) -> str:
    team_id = str(
        client.post("/api/v1/teams", json={"name": "role-team"}, headers=owner.headers).json()["id"]
    )
    for actor, role in members:
        invite = client.post(
            f"/api/v1/teams/{team_id}/invitations",
            json={"email": actor.email, "role": role},
            headers=owner.headers,
        )
        token = invite.json()["invite_url"].split("#token=")[1]
        accepted = client.post(
            "/api/v1/invitations/accept", json={"token": token}, headers=actor.headers
        )
        assert accepted.status_code == 200
    return team_id


def _roles(client: TestClient, team_id: str, viewer: Actor) -> dict[str, str]:
    team = client.get(f"/api/v1/teams/{team_id}", headers=viewer.cookie).json()
    return {m["user_id"]: m["role"] for m in team["members"]}


def test_sec_role_004_team_member_and_manager_cannot_escalate(
    client: TestClient, engine: Engine
) -> None:
    """SEC-ROLE-004: MEMBER는 관리 동작 전부, MANAGER는 OWNER 동작(삭제·역할 변경·이전·MANAGER
    초대·MANAGER/OWNER 내보내기)을 할 수 없다. SUPER_ADMIN 인스턴스 역할도 팀 역할을 올리지
    않는다."""
    owner = make_user(engine)
    manager = make_user(engine)
    member = make_user(engine, role="SUPER_ADMIN")  # 인스턴스 최고 관리자이지만 팀에서는 MEMBER
    peer = make_user(engine)
    team = _team(client, owner, (manager, "MANAGER"), (member, "MEMBER"), (peer, "MEMBER"))
    before = _roles(client, team, owner)
    invitation = client.post(
        f"/api/v1/teams/{team}/invitations",
        json={"email": "later@example.invalid"},
        headers=owner.headers,
    ).json()["id"]

    member_attempts: list[tuple[str, str, Any]] = [
        ("PATCH", f"/api/v1/teams/{team}", {"name": "pwned"}),
        ("DELETE", f"/api/v1/teams/{team}", None),
        ("PATCH", f"/api/v1/teams/{team}/members/{member.id}", {"role": "MANAGER"}),
        ("PATCH", f"/api/v1/teams/{team}/members/{peer.id}", {"role": "MANAGER"}),
        ("DELETE", f"/api/v1/teams/{team}/members/{peer.id}", None),
        ("DELETE", f"/api/v1/teams/{team}/members/{owner.id}", None),
        ("POST", f"/api/v1/teams/{team}/transfer", {"user_id": str(member.id)}),
        ("POST", f"/api/v1/teams/{team}/invitations", {"email": "x@example.invalid"}),
        ("GET", f"/api/v1/teams/{team}/invitations", None),
        ("DELETE", f"/api/v1/teams/{team}/invitations/{invitation}", None),
    ]
    manager_attempts: list[tuple[str, str, Any]] = [
        ("DELETE", f"/api/v1/teams/{team}", None),
        ("PATCH", f"/api/v1/teams/{team}/members/{manager.id}", {"role": "MANAGER"}),
        ("PATCH", f"/api/v1/teams/{team}/members/{peer.id}", {"role": "MANAGER"}),
        ("PATCH", f"/api/v1/teams/{team}/members/{owner.id}", {"role": "MEMBER"}),
        ("DELETE", f"/api/v1/teams/{team}/members/{owner.id}", None),
        ("POST", f"/api/v1/teams/{team}/transfer", {"user_id": str(manager.id)}),
        (
            "POST",
            f"/api/v1/teams/{team}/invitations",
            {"email": "y@example.invalid", "role": "MANAGER"},
        ),
    ]
    for actor, attempts in ((member, member_attempts), (manager, manager_attempts)):
        for method, path, body in attempts:
            kwargs: dict[str, Any] = {"headers": actor.headers}
            if method != "GET":
                kwargs["json"] = body or {}
            response = client.request(method, path, **kwargs)
            assert response.status_code == 403, (method, path, response.text)
    assert _roles(client, team, owner) == before
    # 초대 수락 본문으로 역할을 고를 수 없다(초대에 적힌 역할만).
    smuggle = client.post(
        "/api/v1/invitations/accept", json={"token": "x", "role": "OWNER"}, headers=peer.headers
    )
    assert smuggle.status_code == 422

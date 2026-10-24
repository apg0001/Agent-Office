"""멀티테넌시·IDOR(#64): 공격자 B는 어떤 경로로도 피해자 A의 자원을 읽거나 바꾸지 못한다.

경로는 `app.routes`에서 전수로 뽑고, 경로 변수에는 A의 실제 ID를 넣는다. 본문은 검증을 통과하는
값을 넣어 권한 검사까지 실제로 도달하게 한다(422로 끝나면 실패로 본다).
"""

from __future__ import annotations

from typing import Any

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import Engine
from starlette.websockets import WebSocketDisconnect

from agent_office.api import office
from agent_office.main import app
from security.secworld import (
    STATE_CHANGING,
    Actor,
    World,
    assert_world_intact,
    build_world,
    call,
    fill,
    make_user,
    routes_of,
)


def _values(world: World, route_key: str) -> dict[str, str]:
    values = {
        "device_id": world.device["device_id"],
        "agent_id": str(world.lead),
        "team_id": str(world.team_id),
        "member_id": str(world.owner.id),
        "invitation_id": str(world.invitation_id),
        "project_id": str(world.project_id),
        "user_id": str(world.owner.id),
        "session_id": str(world.agent_session_id),
    }
    if route_key.startswith("POST /api/v1/auth/sessions/"):
        values["session_id"] = str(world.other_login_session)
    return values


def _bodies(world: World, attacker: Actor) -> dict[str, Any]:
    return {
        "PATCH /api/v1/teams/{team_id}": {"name": "pwned"},
        "PATCH /api/v1/teams/{team_id}/members/{member_id}": {"role": "MEMBER"},
        "POST /api/v1/teams/{team_id}/transfer": {"user_id": str(attacker.id)},
        "POST /api/v1/teams/{team_id}/invitations": {"email": attacker.email, "role": "MANAGER"},
        "PUT /api/v1/office/sessions/{session_id}/project": {"project_id": None},
        "PUT /api/v1/admin/users/{user_id}/role": {"instance_role": "USER"},
        "PATCH /api/v1/admin/notifications/{channel_id}": {"enabled": False},
        "PUT /api/v1/admin/settings/oauth/{provider}": {"enabled": False, "client_id": ""},
    }


def _attack_all(
    client: TestClient, world: World, attacker: Actor, kinds: tuple[str, ...]
) -> list[str]:
    """경로 변수가 있는 모든 경로를 A의 ID로 호출한다. 시도한 경로 키 목록을 돌려준다."""
    bodies = _bodies(world, attacker)
    tried = []
    for route in routes_of(app, *kinds):
        if not route.params:
            continue
        path = fill(route.path, _values(world, route.key))
        response = call(client, route, path, attacker.headers, bodies.get(route.key))
        assert response.status_code in (403, 404), (route.key, response.status_code, response.text)
        for value in world.ids():
            assert value not in response.text, (route.key, value)
        tried.append(route.key)
    return tried


def test_sec_tenant_001_user_cannot_touch_other_users_resources(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TENANT-001: 일반 사용자 B가 A의 장치·세션·에이전트·타임라인·프로젝트·팀·초대·로그인
    세션·관리 경로를 모두 시도해도 403/404만 받고 A의 상태는 그대로다."""
    world = build_world(client, engine)
    attacker = make_user(engine, display_name="attacker")
    tried = _attack_all(client, world, attacker, ("user", "admin", "super_admin"))
    assert len(tried) >= 25
    assert_world_intact(client, engine, world)


@pytest.mark.parametrize("role", ["ADMIN", "SUPER_ADMIN"])
def test_sec_tenant_002_instance_admin_gets_no_personal_or_team_power(
    client: TestClient, engine: Engine, role: str
) -> None:
    """SEC-TENANT-002: 인스턴스 관리자도 남의 개인 자원·팀 자원에는 권한이 없다(404)."""
    world = build_world(client, engine)
    attacker = make_user(engine, role=role, display_name="instance-admin")
    _attack_all(client, world, attacker, ("user",))
    assert_world_intact(client, engine, world)


def test_sec_tenant_003_list_endpoints_never_include_other_users_data(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TENANT-003: 경로 변수 없는 모든 조회 경로(내 장치·사무실·통합·팀·프로젝트·세션 등)에
    A의 데이터가 나오지 않는다. 같은 팀 구성원은 공유 세션을 본다(양성 대조군)."""
    world = build_world(client, engine)
    for role in ("USER", "SUPER_ADMIN"):
        attacker = make_user(engine, role=role)
        for route in routes_of(app, "user"):
            if route.method != "GET" or route.params:
                continue
            response = client.get(route.path, headers=attacker.cookie)
            assert response.status_code == 200, (route.key, response.text)
            for value in world.ids():
                assert value not in response.text, (role, route.key, value)
    shared = client.get(f"/api/v1/teams/{world.team_id}/office", headers=world.member.cookie)
    assert str(world.agent_session_id) in shared.text
    # 팀원에게도 장치 이름(PC 이름)과 장치 토큰은 보이지 않는다.
    assert world.device["device_token"] not in shared.text
    assert "victim-pc" not in shared.text


def test_sec_tenant_004_cross_scope_combinations(client: TestClient, engine: Engine) -> None:
    """SEC-TENANT-004: 자기 자원과 남의 자원을 섞은 요청(내 팀에 남의 프로젝트 공유, 남의 팀에
    내 프로젝트 공유, 내 세션을 남의 프로젝트에 지정, 내 팀 범위로 남의 에이전트 조회)도 거부."""
    world = build_world(client, engine)
    attacker = make_user(engine)
    team = client.post("/api/v1/teams", json={"name": "attacker-team"}, headers=attacker.headers)
    attacker_team = team.json()["id"]
    project = client.post(
        "/api/v1/projects", json={"display_name": "attacker-project"}, headers=attacker.headers
    ).json()["id"]
    h = attacker.headers
    attempts = [
        ("PUT", f"/api/v1/projects/{world.project_id}/shares/{attacker_team}", None),
        ("DELETE", f"/api/v1/projects/{world.project_id}/shares/{world.team_id}", None),
        ("PUT", f"/api/v1/projects/{project}/shares/{world.team_id}", None),
        ("GET", f"/api/v1/teams/{attacker_team}/office/agents/{world.lead}", None),
        ("GET", f"/api/v1/teams/{attacker_team}/office/agents/{world.lead}/timeline", None),
        ("GET", f"/api/v1/office/combined/agents/{world.sub}", None),
        (
            "PUT",
            f"/api/v1/office/sessions/{world.agent_session_id}/project",
            {"project_id": project},
        ),
    ]
    for method, path, body in attempts:
        kwargs: dict[str, Any] = {"headers": h}
        if method in STATE_CHANGING:
            kwargs["json"] = body or {}
        response = client.request(method, path, **kwargs)
        assert response.status_code == 404, (method, path, response.text)
    # 초대 토큰을 가로챈 다른 계정은 수락할 수 없다(초대 이메일과 계정 이메일이 달라야 함).
    stolen = client.post(
        "/api/v1/invitations/accept", json={"token": world.invite_token}, headers=h
    )
    assert stolen.status_code == 400
    assert_world_intact(client, engine, world)


def _closed(client: TestClient, path: str, headers: dict[str, str]) -> int:
    with (
        pytest.raises(WebSocketDisconnect) as closed,
        client.websocket_connect(path, headers=headers),
    ):
        pass
    return closed.value.code


def test_sec_tenant_005_websocket_scopes(client: TestClient, engine: Engine) -> None:
    """SEC-TENANT-005: WebSocket 개인·통합 스냅샷에 남의 데이터가 없고, 구성원이 아닌 팀 범위는
    4404로 닫힌다(인스턴스 관리자 포함). 팀원은 공유 세션을 받는다(양성 대조군)."""
    world = build_world(client, engine)
    for role in ("USER", "SUPER_ADMIN"):
        attacker = make_user(engine, role=role)
        for query in ("", "?view=combined"):
            with client.websocket_connect(
                f"/api/v1/live{query}", headers=attacker.ws_headers
            ) as ws:
                text = str(ws.receive_json())
            for value in world.ids():
                assert value not in text, (role, query, value)
        for team in (str(world.team_id), "not-a-uuid", "../" + str(world.team_id)):
            code = _closed(client, f"/api/v1/live?team={team}", attacker.ws_headers)
            assert code == office.CLOSE_SCOPE_UNAVAILABLE
    with client.websocket_connect(
        f"/api/v1/live?team={world.team_id}", headers=world.member.ws_headers
    ) as ws:
        snapshot = ws.receive_json()
    assert str(world.agent_session_id) in str(snapshot)


def test_sec_tenant_006_removed_member_loses_access_immediately(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TENANT-006: 팀에서 내보내진 구성원은 REST 404, 열린 팀 WebSocket은 4404로 닫힌다."""
    world = build_world(client, engine)
    member = world.member
    with client.websocket_connect(
        f"/api/v1/live?team={world.team_id}", headers=member.ws_headers
    ) as ws:
        ws.receive_json()
        removed = client.delete(
            f"/api/v1/teams/{world.team_id}/members/{member.id}", headers=world.owner.headers
        )
        assert removed.status_code == 204
        with pytest.raises(WebSocketDisconnect) as closed:
            for _ in range(200):
                ws.receive_json()
    assert closed.value.code == office.CLOSE_SCOPE_UNAVAILABLE
    for path in (
        f"/api/v1/teams/{world.team_id}",
        f"/api/v1/teams/{world.team_id}/office",
        f"/api/v1/teams/{world.team_id}/office/agents/{world.lead}",
        f"/api/v1/teams/{world.team_id}/office/agents/{world.lead}/timeline",
        f"/api/v1/office/combined/agents/{world.lead}",
    ):
        assert client.get(path, headers=member.cookie).status_code == 404, path


def test_sec_tenant_007_device_cannot_write_into_other_devices_scope(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TENANT-007: 장치 B가 A의 device_id·같은 Collector 세션 ID로 이벤트를 보내도 A의 상태에
    섞이지 않는다(device_id 불일치는 unauthorized_device, 세션은 장치별로 분리)."""
    from helpers import new_code, register

    from security.secworld import event, send

    world = build_world(client, engine)
    attacker = make_user(engine)
    device = register(client, new_code(client, attacker.session), name="attacker-pc")
    forged = event(
        world.device["device_id"], world.collector_session, world.lead, "tool.started", minute=5
    )
    response = send(client, device, [forged])
    assert [r["code"] for r in response.json()["rejected_events"]] == ["unauthorized_device"]
    same_ids = event(
        device["device_id"], world.collector_session, world.lead, "session.ended", minute=6
    )
    assert send(client, device, [same_ids]).json()["rejected_events"] == []
    snapshot = client.get("/api/v1/office", headers=world.owner.cookie).json()
    lifecycles = {s["id"]: s["lifecycle"] for s in snapshot["sessions"]}
    assert lifecycles[str(world.agent_session_id)] != "ENDED"
    assert_world_intact(client, engine, world)

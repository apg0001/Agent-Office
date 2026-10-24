"""보안 시나리오 테스트(#64) 공통 도구: 경로 목록·분류, 피해자/공격자 세계 구성.

모든 값은 합성이며 실제 비밀·사용자 데이터가 아니다.
"""

from __future__ import annotations

import datetime as dt
import re
import secrets
import uuid
from dataclasses import dataclass, field
from typing import Any

from fastapi import FastAPI
from fastapi.routing import APIRoute, APIWebSocketRoute
from fastapi.testclient import TestClient
from helpers import bearer, csrf, new_code, register, session_cookie
from sqlalchemy import Engine, select, update

from agent_office.auth import mailer
from agent_office.auth.sessions import SESSION_COOKIE, issue_session
from agent_office.db import schema
from agent_office.security import token_hash

ORIGIN = "http://testserver"
SAME_ORIGIN = {"origin": ORIGIN}


# --- 경로 목록 ---------------------------------------------------------------------


@dataclass(frozen=True)
class Route:
    method: str  # HTTP 메서드 또는 "WS"
    path: str
    route: Any

    @property
    def key(self) -> str:
        return f"{self.method} {self.path}"

    @property
    def params(self) -> list[str]:
        return re.findall(r"{(\w+)}", self.path)


def iter_routes(app: FastAPI) -> list[Route]:
    """앱의 모든 HTTP·WebSocket 경로. include_router로 붙인 라우터 안까지 펼친다."""
    found: list[Route] = []

    def walk(routes: list[Any]) -> None:
        for route in routes:
            inner = getattr(route, "original_router", None)
            if inner is not None:
                walk(list(inner.routes))
            elif isinstance(route, APIWebSocketRoute):
                found.append(Route("WS", route.path, route))
            else:
                for method in sorted(getattr(route, "methods", None) or ()):
                    if method != "HEAD":
                        found.append(Route(method, route.path, route))

    walk(list(app.routes))
    return found


# 인증 없이 열린 경로(비밀 없음). 새 경로가 생기면 분류 테스트가 실패해 검토를 강제한다.
PUBLIC = {
    "GET /api/openapi.json",
    "GET /api/docs",
    "GET /docs/oauth2-redirect",
    "GET /redoc",
    "GET /api/v1/system/health",
    "GET /api/v1/system/ready",
    "GET /api/v1/setup/status",
    "GET /api/v1/auth/oauth/providers",
    # 로그인 전 리디렉션. state·PKCE·HttpOnly 결속 쿠키로 보호(#24).
    "GET /api/v1/auth/oauth/{provider}/start",
    "GET /api/v1/auth/oauth/{provider}/callback",
}
# 로그인 전 쓰기. CSRF 토큰 대신 Origin·JSON 본문 검사.
PRE_LOGIN = {
    "POST /api/v1/auth/signup",
    "POST /api/v1/auth/verify-email",
    "POST /api/v1/auth/login",
    "POST /api/v1/auth/password-reset/request",
    "POST /api/v1/auth/password-reset/confirm",
    "POST /api/v1/setup/admin",
}
# 일회용 등록 코드로만 인증(쿠키를 쓰지 않음).
REGISTRATION = {"POST /api/v1/devices/register"}
# 장치 토큰(Bearer)만.
DEVICE = {
    "POST /api/v1/devices/heartbeat",
    "POST /api/v1/devices/token/rotate",
    "POST /api/v1/devices/revoke",
    "POST /api/v1/events/batch",
}
WEBSOCKET = {"WS /api/v1/live"}
SUPER_ADMIN_ONLY = {
    "POST /api/v1/setup/complete",
    "PUT /api/v1/admin/users/{user_id}/role",
}
STATE_CHANGING = {"POST", "PUT", "PATCH", "DELETE"}


def classify(route: Route) -> str:
    if route.key in PUBLIC:
        return "public"
    if route.key in PRE_LOGIN:
        return "pre_login"
    if route.key in REGISTRATION:
        return "registration"
    if route.key in DEVICE:
        return "device"
    if route.key in WEBSOCKET:
        return "websocket"
    if route.key in SUPER_ADMIN_ONLY:
        return "super_admin"
    if route.path.startswith("/api/v1/admin"):
        return "admin"
    return "user"


def routes_of(app: FastAPI, *kinds: str) -> list[Route]:
    return [r for r in iter_routes(app) if classify(r) in kinds]


def is_api_route(route: Route) -> bool:
    return isinstance(route.route, APIRoute)


DEFAULT_PARAMS: dict[str, str] = {"provider": "google", "action": "run"}


def fill(path: str, values: dict[str, str] | None = None) -> str:
    values = {**DEFAULT_PARAMS, **(values or {})}

    def sub(match: re.Match[str]) -> str:
        return values.get(match.group(1)) or str(uuid.uuid4())

    return re.sub(r"{(\w+)}", sub, path)


def call(
    client: TestClient,
    route: Route,
    path: str,
    headers: dict[str, str],
    body: Any = None,
) -> Any:
    kwargs: dict[str, Any] = {"headers": headers}
    if route.method in STATE_CHANGING:
        kwargs["json"] = {} if body is None else body
    return client.request(route.method, path, **kwargs)


# --- 사용자·세계 ------------------------------------------------------------------


@dataclass
class FakeMailer:
    configured: bool = True
    outbox: list[mailer.OutgoingMail] = field(default_factory=list)

    def send(self, mail: mailer.OutgoingMail) -> bool:
        self.outbox.append(mail)
        return True

    def token(self, index: int = -1) -> str:
        found = re.search(r"#token=([A-Za-z0-9_-]+)", self.outbox[index].body)
        assert found
        return found.group(1)


@dataclass
class Actor:
    id: uuid.UUID
    email: str
    session: str

    @property
    def cookie(self) -> dict[str, str]:
        return session_cookie(self.session)

    @property
    def headers(self) -> dict[str, str]:
        """쿠키 + 자기 세션 CSRF 토큰."""
        return {**session_cookie(self.session), **csrf(self.session)}

    @property
    def ws_headers(self) -> dict[str, str]:
        return {"cookie": f"{SESSION_COOKIE}={self.session}", "host": "testserver"}


def make_user(
    engine: Engine,
    *,
    role: str = "USER",
    status: str = "active",
    display_name: str = "synthetic",
    email: str | None = None,
) -> Actor:
    email = email or f"{secrets.token_hex(5)}@example.invalid"
    with engine.begin() as connection:
        user_id: uuid.UUID = connection.execute(
            schema.users.insert()
            .values(
                email=email,
                display_name=display_name,
                status=status,
                instance_role=role,
                email_verified_at=dt.datetime.now(dt.UTC),
            )
            .returning(schema.users.c.id)
        ).scalar_one()
        token = issue_session(connection, user_id)
    return Actor(user_id, email, token)


def new_session(engine: Engine, user_id: uuid.UUID) -> str:
    with engine.begin() as connection:
        return issue_session(connection, user_id)


def login_session_id(engine: Engine, token: str) -> uuid.UUID:
    sessions = schema.login_sessions
    with engine.connect() as connection:
        value: uuid.UUID = connection.execute(
            select(sessions.c.id).where(sessions.c.token_hash == token_hash(token))
        ).scalar_one()
    return value


def event(
    device_id: str,
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    event_type: str,
    *,
    minute: int,
    parent: uuid.UUID | None = None,
    **extra: Any,
) -> dict[str, Any]:
    occurred = dt.datetime.now(dt.UTC) - dt.timedelta(minutes=30) + dt.timedelta(minutes=minute)
    return {
        "schema_version": "1.0",
        "event_id": str(uuid.uuid4()),
        "device_id": device_id,
        "session_id": str(session_id),
        "agent_id": str(agent_id),
        "parent_agent_id": str(parent) if parent else None,
        "provider": "claude",
        "source": "hook",
        "event_type": event_type,
        "activity": "coding",
        "confidence": "observed",
        "occurred_at": occurred.isoformat(),
        "sequence": minute,
        **extra,
    }


def send(client: TestClient, device: dict[str, str], events: list[dict[str, Any]]) -> Any:
    return client.post(
        "/api/v1/events/batch",
        json={"schema_version": "1.0", "events": events},
        headers=bearer(device["device_token"]),
    )


@dataclass
class World:
    """피해자 A의 자원 전체. 공격자는 이 ID들로 접근을 시도한다."""

    owner: Actor
    member: Actor  # A의 팀 T의 MEMBER(양성 대조군)
    device: dict[str, str]
    collector_session: uuid.UUID
    agent_session_id: uuid.UUID
    lead: uuid.UUID  # 서버 에이전트 ID
    sub: uuid.UUID
    project_id: uuid.UUID
    team_id: uuid.UUID
    invitation_id: uuid.UUID
    invite_token: str
    other_login_session: uuid.UUID
    secrets: list[str]

    def ids(self) -> list[str]:
        """공격자 응답에 나오면 안 되는 값."""
        return [
            str(self.owner.id),
            self.owner.email,
            self.device["device_id"],
            str(self.agent_session_id),
            str(self.lead),
            str(self.sub),
            str(self.project_id),
            str(self.team_id),
            str(self.invitation_id),
            str(self.other_login_session),
            *self.secrets,
        ]


VICTIM_DEVICE = "victim-pc-5e1f"
VICTIM_PROJECT = "victim-project-77aa"
VICTIM_TEAM = "victim-team-90bd"


def build_world(client: TestClient, engine: Engine) -> World:
    owner = make_user(engine, display_name="victim-owner")
    member = make_user(engine, display_name="victim-member")
    device = register(client, new_code(client, owner.session), name=VICTIM_DEVICE)
    collector_session, lead_c, sub_c = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    response = send(
        client,
        device,
        [
            event(device["device_id"], collector_session, lead_c, "session.started", minute=0),
            event(
                device["device_id"],
                collector_session,
                sub_c,
                "agent.started",
                minute=1,
                parent=lead_c,
            ),
            event(device["device_id"], collector_session, lead_c, "tool.started", minute=2),
        ],
    )
    assert response.status_code == 200 and response.json()["rejected_events"] == [], response.text
    sessions, agents = schema.agent_sessions, schema.agents
    with engine.connect() as connection:
        agent_session_id = connection.execute(
            select(sessions.c.id).where(sessions.c.collector_session_id == collector_session)
        ).scalar_one()
        lead = connection.execute(
            select(agents.c.id).where(agents.c.collector_agent_id == lead_c)
        ).scalar_one()
        sub = connection.execute(
            select(agents.c.id).where(agents.c.collector_agent_id == sub_c)
        ).scalar_one()

    project = client.post(
        "/api/v1/projects", json={"display_name": VICTIM_PROJECT}, headers=owner.headers
    )
    assert project.status_code == 201
    project_id = uuid.UUID(project.json()["id"])
    team = client.post("/api/v1/teams", json={"name": VICTIM_TEAM}, headers=owner.headers)
    assert team.status_code == 201
    team_id = uuid.UUID(team.json()["id"])
    # 구성원은 초대 수락으로 합류시킨다(실제 흐름).
    invited = client.post(
        f"/api/v1/teams/{team_id}/invitations",
        json={"email": member.email, "role": "MEMBER"},
        headers=owner.headers,
    )
    token = invited.json()["invite_url"].split("#token=")[1]
    assert (
        client.post(
            "/api/v1/invitations/accept", json={"token": token}, headers=member.headers
        ).status_code
        == 200
    )
    pending = client.post(
        f"/api/v1/teams/{team_id}/invitations",
        json={"email": "pending-invitee@example.invalid"},
        headers=owner.headers,
    )
    assert pending.status_code == 201
    shared = client.put(f"/api/v1/projects/{project_id}/shares/{team_id}", headers=owner.headers)
    assert shared.status_code == 200
    assigned = client.put(
        f"/api/v1/office/sessions/{agent_session_id}/project",
        json={"project_id": str(project_id)},
        headers=owner.headers,
    )
    assert assigned.status_code == 204
    other = new_session(engine, owner.id)
    return World(
        owner=owner,
        member=member,
        device=device,
        collector_session=collector_session,
        agent_session_id=agent_session_id,
        lead=lead,
        sub=sub,
        project_id=project_id,
        team_id=team_id,
        invitation_id=uuid.UUID(pending.json()["id"]),
        invite_token=pending.json()["invite_url"].split("#token=")[1],
        other_login_session=login_session_id(engine, other),
        secrets=[device["device_token"], VICTIM_DEVICE, VICTIM_PROJECT, VICTIM_TEAM],
    )


def set_role(engine: Engine, user_id: uuid.UUID, role: str) -> None:
    with engine.begin() as connection:
        connection.execute(
            update(schema.users).where(schema.users.c.id == user_id).values(instance_role=role)
        )


def assert_world_intact(client: TestClient, engine: Engine, world: World) -> None:
    """공격 뒤에도 A의 상태가 그대로인지(쓰기 시도가 반영되지 않았는지) 확인한다."""
    owner = world.owner
    assert client.get("/api/v1/auth/session", headers=owner.cookie).status_code == 200
    heartbeat = client.post(
        "/api/v1/devices/heartbeat", json={}, headers=bearer(world.device["device_token"])
    )
    assert heartbeat.status_code == 204
    team = client.get(f"/api/v1/teams/{world.team_id}", headers=owner.cookie)
    assert team.status_code == 200 and team.json()["name"] == VICTIM_TEAM
    roles = {m["user_id"]: m["role"] for m in team.json()["members"]}
    assert roles == {str(owner.id): "OWNER", str(world.member.id): "MEMBER"}
    invitations = client.get(f"/api/v1/teams/{world.team_id}/invitations", headers=owner.cookie)
    assert [i["id"] for i in invitations.json()] == [str(world.invitation_id)]
    projects = client.get("/api/v1/projects", headers=owner.cookie).json()
    assert projects == [
        {
            "id": str(world.project_id),
            "display_name": VICTIM_PROJECT,
            "shared_team_ids": [str(world.team_id)],
        }
    ]
    sessions = client.get("/api/v1/auth/sessions", headers=owner.cookie).json()
    assert str(world.other_login_session) in {s["id"] for s in sessions}
    with engine.connect() as connection:
        project_id = connection.execute(
            select(schema.agent_sessions.c.project_id).where(
                schema.agent_sessions.c.id == world.agent_session_id
            )
        ).scalar_one()
    assert project_id == world.project_id

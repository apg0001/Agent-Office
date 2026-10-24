"""입력 남용(#64): 큰 본문, 깊은 중첩 JSON, 제어·유니코드 문자, 경로 조작 ID, SQL 모양 문자열,
이벤트 enum 퍼즈, 커서 퍼즈. 어떤 입력도 500이나 입력 되돌려 보내기(echo)로 끝나지 않는다.
"""

from __future__ import annotations

import base64
import random
import uuid
from typing import Any

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, new_code, register
from sqlalchemy import Engine, func, select

from agent_office.api import events as events_api
from agent_office.db import schema
from agent_office.http_limits import DEFAULT_MAX_BODY_BYTES, PATH_MAX_BODY_BYTES
from security.secworld import ORIGIN, make_user, send
from security.secworld import event as make_event

ECHO = "sentinel-echo-c0ffee"


def test_sec_input_001_body_size_limits(client: TestClient, engine: Engine) -> None:
    """SEC-INPUT-001: 본문 상한 초과 413, 길이 없는 chunked 411, 잘못된 길이 400(본문 읽기 전)."""
    actor = make_user(engine)
    big = b'{"name":"' + b"a" * DEFAULT_MAX_BODY_BYTES + b'"}'
    for path, headers in (
        ("/api/v1/auth/login", {"origin": ORIGIN}),
        ("/api/v1/teams", actor.headers),
        ("/api/v1/devices/register", {}),
    ):
        response = client.post(
            path, content=big, headers={**headers, "content-type": "application/json"}
        )
        assert response.status_code == 413, path
    batch = PATH_MAX_BODY_BYTES["/api/v1/events/batch"]
    response = client.post(
        "/api/v1/events/batch",
        content=b"[" + b" " * batch + b"]",
        headers={"content-type": "application/json", "authorization": "Bearer aod_x"},
    )
    assert response.status_code == 413

    def chunks() -> Any:
        yield b"{}"

    chunked = client.post("/api/v1/auth/login", content=chunks(), headers={"origin": ORIGIN})
    assert chunked.status_code == 411
    invalid = client.post(
        "/api/v1/auth/login",
        content=b"{}",
        headers={"origin": ORIGIN, "content-length": "abc", "content-type": "application/json"},
    )
    assert invalid.status_code == 400


@pytest.mark.parametrize("depth", [100, 5_000, 30_000])
def test_sec_input_002_deeply_nested_json(client: TestClient, engine: Engine, depth: int) -> None:
    """SEC-INPUT-002: 깊은 중첩 JSON은 4xx(파싱·검증 오류)이고 서버 오류가 아니다. 임의 dict를
    받는 알림 채널 config에도 같은 중첩을 넣는다."""
    admin = make_user(engine, role="SUPER_ADMIN")
    nested = "[" * depth + "]" * depth
    targets: list[tuple[str, dict[str, str], str]] = [
        ("/api/v1/auth/login", {"origin": ORIGIN}, nested),
        ("/api/v1/teams", admin.headers, '{"name":' + nested + "}"),
        (
            "/api/v1/admin/notifications",
            admin.headers,
            '{"provider":"email_smtp","display_name":"x","config":{"recipients":' + nested + "}}",
        ),
        ("/api/v1/events/batch", bearer("aod_x"), nested),
    ]
    for path, headers, body in targets:
        if len(body) > DEFAULT_MAX_BODY_BYTES and path != "/api/v1/events/batch":
            continue
        response = client.post(
            path, content=body, headers={**headers, "content-type": "application/json"}
        )
        assert 400 <= response.status_code < 500, (path, response.status_code)


NASTY_NAMES = [
    "nul\x00byte",
    "\x00",
    "ctrl\x01\x07\x1b[31m",
    "rtl‮evil",
    "zero​width",
    "emoji \U0001f600",
    "<script>alert(1)</script>",
    "'; DROP TABLE users; --",
    '" OR 1=1 --',
    "../../etc/passwd",
    "%00%2e%2e",
    "퟿",
]


def test_sec_input_003_unicode_control_and_sql_like_names(
    client: TestClient, engine: Engine
) -> None:
    """SEC-INPUT-003(수정된 결함): 이름 칸의 NUL 문자는 이전에 DB 오류로 500이었다. 이제 400
    `invalid_input`이고, 그 밖의 제어·유니코드·SQL·HTML 모양 문자열은 그대로 문자로 저장된다."""
    actor = make_user(engine)
    for name in NASTY_NAMES:
        cases = [
            ("/api/v1/teams", {"name": name}),
            ("/api/v1/projects", {"display_name": name}),
        ]
        for path, body in cases:
            response = client.post(path, json=body, headers=actor.headers)
            if "\x00" in name:
                assert response.status_code == 400, (path, name, response.text)
                assert response.json() == {"detail": "invalid_input"}
            else:
                assert response.status_code == 201, (path, name, response.text)
                assert response.headers["content-type"].startswith("application/json")
        code = new_code(client, actor.session)
        registered = client.post(
            "/api/v1/devices/register",
            json={"registration_code": code, "name": name + str(uuid.uuid4())[:4]},
        )
        assert registered.status_code in (201, 400), (name, registered.text)
        assert registered.status_code == (400 if "\x00" in name else 201)
        for path, body in (
            ("/api/v1/auth/login", {"email": name + "@example.invalid", "password": name}),
            ("/api/v1/auth/password-reset/request", {"email": name}),
            ("/api/v1/auth/verify-email", {"token": name}),
            ("/api/v1/auth/signup", {"email": name, "password": name, "display_name": name}),
        ):
            response = client.post(path, json=body, headers={"origin": ORIGIN})
            # 503(메일 미설정)은 의도된 응답이다. 처리되지 않은 오류(500)만 실패로 본다.
            assert response.status_code != 500, (path, name, response.text)
    # SQL 모양 문자열은 데이터로 저장되었고 표는 그대로다.
    with engine.connect() as connection:
        users = connection.execute(select(func.count()).select_from(schema.users)).scalar_one()
        stored = set(connection.execute(select(schema.teams.c.name)).scalars())
    assert users >= 1 and "'; DROP TABLE users; --" in stored


@pytest.mark.parametrize(
    "path",
    [
        "/api/v1/devices/..%2f..%2fadmin%2fusers/revoke",
        "/api/v1/devices/%2e%2e/revoke",
        "/api/v1/office/agents/..%2F..%2Fadmin%2Faudit",
        "/api/v1/office/agents/1%20OR%201=1",
        "/api/v1/teams/%00",
        "/api/v1/teams/00000000-0000-0000-0000-000000000000%00",
        "/api/v1/admin/backup/..%2frestore-test",
        "/api/v1/auth/oauth/..%2fgithub/start",
    ],
)
def test_sec_input_004_path_traversal_like_ids(
    client: TestClient, engine: Engine, path: str
) -> None:
    """SEC-INPUT-004: 경로 조작·SQL 모양 ID는 404/405/422로 끝나고 다른 경로로 새지 않는다."""
    actor = make_user(engine, role="SUPER_ADMIN")
    for method in ("GET", "POST"):
        response = client.request(method, path, headers=actor.headers)
        assert response.status_code in (404, 405, 422), (method, path, response.status_code)


def test_sec_input_005_event_enum_fuzz(client: TestClient, engine: Engine) -> None:
    """SEC-INPUT-005: 이벤트 필드 퍼즈(무작위 문자열·타입·경계 값). 항상 422 또는 정상 판정이고,
    422 응답은 입력 값을 되돌려 보내지 않으며 거부된 이벤트는 저장되지 않는다."""
    actor = make_user(engine)
    device = register(client, new_code(client, actor.session))
    base = make_event(device["device_id"], uuid.uuid4(), uuid.uuid4(), "session.started", minute=0)
    rng = random.Random(64)
    garbage: list[Any] = [
        ECHO,
        ECHO.upper(),
        "",
        " ",
        None,
        0,
        -1,
        1.5,
        True,
        [],
        {},
        {"$ne": None},
        "x" * 5000,
        "SESSION.STARTED",
        "session.started\x00",
        "unknown ",
        2**63,
        -(2**63),
    ]
    fields = [
        "schema_version",
        "event_id",
        "device_id",
        "session_id",
        "agent_id",
        "parent_agent_id",
        "provider",
        "source",
        "event_type",
        "activity",
        "confidence",
        "occurred_at",
        "sequence",
        "plan_steps",
    ]
    for index in range(150):
        if index % 50 == 0:
            events_api.rate_limiter.reset()  # 장치별 분당 한도(120)는 이 검사의 대상이 아니다.
        candidate = dict(base, event_id=str(uuid.uuid4()))
        for field in rng.sample(fields, rng.randint(1, 3)):
            candidate[field] = rng.choice(garbage)
        response = send(client, device, [candidate])
        assert response.status_code in (200, 422), (candidate, response.text)
        assert ECHO not in response.text and ECHO.upper() not in response.text
        if response.status_code == 200:
            body = response.json()
            assert len(body["accepted_event_ids"]) + len(body["rejected_events"]) == 1
    for occurred in ("2026-01-01T00:00:00", "9999-12-31T23:59:59Z", "0001-01-01T00:00:00Z"):
        response = send(
            client, device, [dict(base, event_id=str(uuid.uuid4()), occurred_at=occurred)]
        )
        assert response.status_code in (200, 422), occurred
        if response.status_code == 200:
            assert response.json()["accepted_event_ids"] == []
    with engine.connect() as connection:
        stored = connection.execute(
            select(schema.activity_events.c.activity, schema.activity_events.c.event_type)
        ).all()
    allowed_activity = {
        "reading",
        "browsing",
        "coding",
        "testing",
        "planning",
        "delegating",
        "reporting",
        "thinking",
        "waiting_user",
        "idle",
        "unknown",
    }
    assert all(row.activity in allowed_activity for row in stored)


def test_sec_input_006_timeline_cursor_and_query_fuzz(client: TestClient, engine: Engine) -> None:
    """SEC-INPUT-006: 타임라인 커서·감사 로그 필터·페이지 값 퍼즈는 4xx 또는 빈 결과(500 없음)."""
    actor = make_user(engine, role="SUPER_ADMIN")
    device = register(client, new_code(client, actor.session))
    session, agent = uuid.uuid4(), uuid.uuid4()
    assert (
        send(
            client,
            device,
            [make_event(device["device_id"], session, agent, "session.started", minute=0)],
        ).status_code
        == 200
    )
    agent_id = client.get("/api/v1/office", headers=actor.cookie).json()["agents"][0]["id"]
    path = f"/api/v1/office/agents/{agent_id}/timeline"

    def b64(raw: bytes) -> str:
        return base64.urlsafe_b64encode(raw).decode().rstrip("=")

    cursors = [
        "!!!",
        "",
        b64(b"\xff\xfe"),
        b64(b"no-separator"),
        b64(b"2026-01-01T00:00:00+00:00|" + b"9" * 40),
        b64(b"2026-01-01T00:00:00+00:00|-" + b"9" * 40),
        b64(b"9999-12-31T23:59:59-23:59|1"),
        b64(b"0001-01-01T00:00:00+14:00|1"),
        b64(b"2026-01-01|1|2"),
        b64(b"' OR 1=1 --|1"),
        "A" * 200,
        "A" * 201,
    ]
    for cursor in cursors:
        response = client.get(path, params={"before": cursor}, headers=actor.cookie)
        assert response.status_code in (200, 400, 422), (
            cursor,
            response.status_code,
            response.text,
        )
    for limit in ("0", "-1", "201", "1e9", "x"):
        assert client.get(path, params={"limit": limit}, headers=actor.cookie).status_code == 422
    for action in ("auth.%", "a\x00", "'; --", "A", "x" * 65):
        response = client.get(
            "/api/v1/admin/audit", params={"action": action}, headers=actor.cookie
        )
        assert response.status_code == 422, action
    for before in ("0", "-1", str(2**70), "x"):
        response = client.get(
            "/api/v1/admin/audit", params={"before": before}, headers=actor.cookie
        )
        assert response.status_code in (200, 400, 422), (before, response.status_code)


def test_sec_input_007_ssrf_webhook_destinations(client: TestClient, engine: Engine) -> None:
    """SEC-INPUT-007: 알림 Webhook은 제공자 HTTPS 호스트만. 내부 주소·자격 증명·다른 스킴·호스트
    혼동 주소는 400."""
    admin = make_user(engine, role="SUPER_ADMIN")
    urls = {
        "discord": [
            "http://discord.com/api/webhooks/1/x",
            "https://169.254.169.254/api/webhooks/1/x",
            "https://127.0.0.1/api/webhooks/1/x",
            "https://discord.com.evil.example/api/webhooks/1/x",
            "https://evil.example/#@discord.com/api/webhooks/1/x",
            "https://user:pw@discord.com/api/webhooks/1/x",
            "https://discord.com\\@evil.example/api/webhooks/1/x",
            "file:///etc/passwd",
            "https://discord.com/other/1/x",
        ],
        "teams_workflows": [
            "https://logic.azure.com.evil.example/x",
            "https://localhost/x",
            "gopher://logic.azure.com/x",
        ],
    }
    for provider, values in urls.items():
        for url in values:
            response = client.post(
                "/api/v1/admin/notifications",
                json={"provider": provider, "display_name": "x", "secret": url},
                headers=admin.headers,
            )
            assert response.status_code == 400, (provider, url, response.text)
            assert url not in response.text

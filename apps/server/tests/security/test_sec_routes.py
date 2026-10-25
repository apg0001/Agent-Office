"""경로 전수 보안 검사(#64): 분류·미인증·CSRF·장치 토큰과 브라우저 세션 분리·대량 할당.

모든 경로를 `app.routes`에서 직접 펼쳐 검사하므로, 새 경로가 생기면 분류 표를 갱신해야 한다.
"""

from __future__ import annotations

import datetime as dt

import pytest
from fastapi.testclient import TestClient
from helpers import bearer, csrf, new_code, register, session_cookie
from pydantic import BaseModel
from sqlalchemy import Engine, func, update
from starlette.websockets import WebSocketDisconnect

from agent_office.api import office
from agent_office.api.deps import require_csrf
from agent_office.db import schema
from agent_office.main import app
from security.secworld import (
    ORIGIN,
    PRE_LOGIN,
    PUBLIC,
    STATE_CHANGING,
    call,
    classify,
    fill,
    is_api_route,
    iter_routes,
    make_user,
    new_session,
    routes_of,
)

COOKIE_ROUTES = routes_of(app, "user", "admin", "super_admin")
STATE_CHANGING_COOKIE_ROUTES = [r for r in COOKIE_ROUTES if r.method in STATE_CHANGING]


def test_sec_route_001_inventory_matches_openapi_and_is_classified() -> None:
    """SEC-ROUTE-001: 펼친 경로 목록이 OpenAPI 연산과 일치하고 모두 분류된다."""
    routes = iter_routes(app)
    keys = [r.key for r in routes]
    assert len(keys) == len(set(keys))
    documented = {
        f"{method.upper()} {path}"
        for path, operations in app.openapi()["paths"].items()
        for method in operations
    }
    api_keys = {r.key for r in routes if is_api_route(r) and r.method != "WS"}
    assert api_keys == documented
    # 분류 표의 항목이 실제로 존재한다(이름이 바뀌면 표도 고쳐야 한다).
    assert set(keys) >= (PUBLIC | PRE_LOGIN)
    assert "WS /api/v1/live" in keys
    assert len(COOKIE_ROUTES) >= 50
    for route in routes:
        assert classify(route)


def test_sec_route_002_cookie_routes_require_session(client: TestClient) -> None:
    """SEC-ROUTE-002: 쿠키 인증 경로는 세션 없이·위조 세션으로 401."""
    for route in COOKIE_ROUTES:
        path = fill(route.path)
        for headers in ({}, session_cookie("forged-session"), session_cookie("x" * 300)):
            response = call(client, route, path, headers)
            assert response.status_code == 401, (route.key, response.text)
            assert response.json() == {"detail": "unauthorized"}


def test_sec_csrf_001_state_changing_routes_reject_missing_or_foreign_token(
    client: TestClient, engine: Engine
) -> None:
    """SEC-CSRF-001: 쓰기 경로는 CSRF 토큰 없음·다른 사용자 토큰·같은 사용자의 다른 세션 토큰·
    임의 값을 403으로 거부한다(SUPER_ADMIN이라 역할 검사는 통과한다)."""
    actor = make_user(engine, role="SUPER_ADMIN")
    other = make_user(engine)
    sibling = new_session(engine, actor.id)
    attempts = [
        actor.cookie,
        {**actor.cookie, **csrf(other.session)},
        {**actor.cookie, **csrf(sibling)},
        {**actor.cookie, "X-CSRF-Token": "forged"},
        {**actor.cookie, "X-CSRF-Token": ""},
    ]
    for route in STATE_CHANGING_COOKIE_ROUTES:
        path = fill(route.path)
        for headers in attempts:
            response = call(client, route, path, headers)
            assert response.status_code == 403, (route.key, response.text)
            assert response.json() == {"detail": "csrf_required"}
    # 거부된 요청은 아무것도 바꾸지 않았다: 세션이 살아 있다.
    assert client.get("/api/v1/auth/session", headers=actor.cookie).status_code == 200


def _depends_on(dependant, target) -> bool:
    return dependant.call is target or any(_depends_on(d, target) for d in dependant.dependencies)


@pytest.mark.parametrize("route", STATE_CHANGING_COOKIE_ROUTES, ids=lambda r: r.key)
def test_sec_csrf_002_state_changing_routes_declare_csrf_dependency(route) -> None:
    """SEC-CSRF-002: 정적 확인. 모든 쿠키 쓰기 경로가 `require_csrf` 의존성을 가진다."""
    assert _depends_on(route.route.dependant, require_csrf), route.key


@pytest.mark.parametrize("key", sorted(PRE_LOGIN))
@pytest.mark.parametrize(
    "headers",
    [
        {"origin": "https://evil.example", "content-type": "application/json"},
        {"origin": "null", "content-type": "application/json"},
        {"origin": ORIGIN, "content-type": "text/plain"},
        {"origin": ORIGIN, "content-type": "application/x-www-form-urlencoded"},
        {"content-type": "multipart/form-data; boundary=x"},
    ],
    ids=["foreign-origin", "null-origin", "text-plain", "form", "multipart"],
)
def test_sec_csrf_003_pre_login_routes_enforce_origin_and_json(
    client: TestClient, key: str, headers: dict[str, str]
) -> None:
    """SEC-CSRF-003: 로그인 전 쓰기는 다른 사이트 Origin·폼 본문을 403으로 거부한다."""
    method, path = key.split(" ")
    response = client.request(method, path, content=b"{}", headers=headers)
    assert response.status_code == 403, (key, response.text)
    assert response.json() == {"detail": "cross_site_rejected"}


def test_sec_csrf_004_allowed_origins_list_is_exact(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    """SEC-CSRF-004: 허용 Origin은 정확히 일치해야 한다(접두어·하위 도메인 불가)."""
    monkeypatch.setenv("AGENT_OFFICE_ALLOWED_ORIGINS", "https://office.example")
    body = {"email": "nobody@example.invalid", "password": "x"}
    for origin in (
        "https://office.example.evil",
        "https://evil.office.example",
        "http://office.example",
    ):
        response = client.post("/api/v1/auth/login", json=body, headers={"origin": origin})
        assert response.status_code == 403, origin
    allowed = client.post(
        "/api/v1/auth/login", json=body, headers={"origin": "https://office.example"}
    )
    assert allowed.status_code == 401


@pytest.mark.parametrize(
    "query", ["", "?team=00000000-0000-0000-0000-000000000000", "?view=combined"]
)
def test_sec_csrf_005_websocket_rejects_cross_site_origin(
    client: TestClient, engine: Engine, query: str
) -> None:
    """SEC-CSRF-005: 교차 사이트 WebSocket 하이재킹 차단(개인·팀·통합 모두)."""
    actor = make_user(engine)
    headers = {**actor.ws_headers, "origin": "https://evil.example"}
    with (
        pytest.raises(WebSocketDisconnect) as closed,
        client.websocket_connect(f"/api/v1/live{query}", headers=headers),
    ):
        pass
    assert closed.value.code == office.CLOSE_FORBIDDEN_ORIGIN


def test_sec_csrf_006_session_cookie_attributes(client: TestClient, engine: Engine) -> None:
    """SEC-CSRF-006: 세션 쿠키는 HttpOnly·SameSite=Lax·Secure(기본)."""
    from agent_office.auth import accounts

    with engine.begin() as connection:
        accounts.create_user(
            connection,
            email="cookie@example.invalid",
            display_name="c",
            password="correct horse battery",
            status="active",
            email_verified=True,
        )
    response = client.post(
        "/api/v1/auth/login",
        json={"email": "cookie@example.invalid", "password": "correct horse battery"},
        headers={"origin": ORIGIN},
    )
    assert response.status_code == 200
    cookie = response.headers["set-cookie"].lower()
    assert "httponly" in cookie and "samesite=lax" in cookie and "secure" in cookie
    assert "path=/" in cookie


def test_sec_token_001_device_token_cannot_call_browser_routes(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TOKEN-001: 장치 토큰(Bearer)으로는 브라우저 경로를 쓸 수 없다(관리자 소유 장치 포함)."""
    actor = make_user(engine, role="SUPER_ADMIN")
    device = register(client, new_code(client, actor.session))
    for route in COOKIE_ROUTES:
        response = call(client, route, fill(route.path), bearer(device["device_token"]))
        assert response.status_code == 401, (route.key, response.text)
    with (
        pytest.raises(WebSocketDisconnect) as closed,
        client.websocket_connect("/api/v1/live", headers=bearer(device["device_token"])),
    ):
        pass
    assert closed.value.code == office.CLOSE_UNAUTHORIZED


def test_sec_token_002_browser_session_cannot_call_device_routes(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TOKEN-002: 브라우저 세션(쿠키·CSRF, 세션 토큰 Bearer)으로는 장치 경로를 쓸 수 없다."""
    actor = make_user(engine)
    for route in routes_of(app, "device"):
        for headers in (actor.headers, bearer(actor.session), bearer("aod_" + actor.session)):
            response = call(client, route, route.path, headers)
            assert response.status_code == 401, (route.key, response.text)


def test_sec_token_003_revoked_device_is_rejected_everywhere(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TOKEN-003: 소유자가 폐기한 장치 토큰·회전 뒤 유예가 끝난 이전 토큰은 모든 장치 경로에서
    401."""
    actor = make_user(engine)
    device = register(client, new_code(client, actor.session))
    rotated = client.post("/api/v1/devices/token/rotate", headers=bearer(device["device_token"]))
    assert rotated.status_code == 200
    with engine.begin() as connection:
        connection.execute(
            update(schema.device_credentials)
            .where(schema.device_credentials.c.expires_at.is_not(None))
            .values(expires_at=func.now() - dt.timedelta(seconds=1))
        )
    new_token = rotated.json()["device_token"]
    heartbeat = "/api/v1/devices/heartbeat"
    assert client.post(heartbeat, json={}, headers=bearer(new_token)).status_code == 204
    revoked = client.post(f"/api/v1/devices/{device['device_id']}/revoke", headers=actor.headers)
    assert revoked.status_code == 204
    for route in routes_of(app, "device"):
        for token in (device["device_token"], new_token):
            response = call(client, route, route.path, bearer(token))
            assert response.status_code == 401, (route.key, response.text)


def test_sec_token_004_owner_disable_or_delete_kills_device_token(
    client: TestClient, engine: Engine
) -> None:
    """SEC-TOKEN-004: 소유자 비활성화·계정 삭제 뒤에는 장치 토큰도 쓸 수 없다."""
    admin = make_user(engine, role="SUPER_ADMIN")
    victim = make_user(engine)
    device = register(client, new_code(client, victim.session))
    heartbeat = "/api/v1/devices/heartbeat"
    assert (
        client.post(heartbeat, json={}, headers=bearer(device["device_token"])).status_code == 204
    )
    disabled = client.post(f"/api/v1/admin/users/{victim.id}/disable", headers=admin.headers)
    assert disabled.status_code == 200
    assert (
        client.post(heartbeat, json={}, headers=bearer(device["device_token"])).status_code == 401
    )

    leaver = make_user(engine)
    other = register(client, new_code(client, leaver.session))
    deleted = client.request(
        "DELETE",
        "/api/v1/auth/account",
        json={"confirm_email": leaver.email},
        headers=leaver.headers,
    )
    assert deleted.status_code == 204
    assert client.post(heartbeat, json={}, headers=bearer(other["device_token"])).status_code == 401


def test_sec_mass_001_every_request_model_forbids_extra_fields() -> None:
    """SEC-MASS-001: 모든 요청 본문 모델이 미등록 필드를 거부한다(역할·소유자 주입 차단)."""
    checked = 0
    for route in iter_routes(app):
        body_field = getattr(route.route, "body_field", None)
        if body_field is None or route.method == "WS":
            continue
        stack = [body_field.field_info.annotation]
        while stack:
            model = stack.pop()
            if isinstance(model, type) and issubclass(model, BaseModel):
                assert model.model_config.get("extra") == "forbid", (route.key, model)
                checked += 1
                stack.extend(f.annotation for f in model.model_fields.values())
            for arg in getattr(model, "__args__", ()) or ():
                stack.append(arg)
    assert checked >= 20


@pytest.mark.parametrize(
    ("path", "body"),
    [
        (
            "/api/v1/auth/signup",
            {
                "email": "a@example.invalid",
                "password": "correct horse battery",
                "display_name": "a",
                "instance_role": "SUPER_ADMIN",
            },
        ),
        (
            "/api/v1/auth/login",
            {"email": "a@example.invalid", "password": "x", "user_id": "x"},
        ),
    ],
)
def test_sec_mass_002_pre_login_injection_rejected(
    client: TestClient, path: str, body: dict[str, str]
) -> None:
    """SEC-MASS-002: 가입·로그인 본문에 역할·사용자 ID를 넣으면 422(값은 되돌려 보내지 않음)."""
    response = client.post(path, json=body, headers={"origin": ORIGIN})
    assert response.status_code == 422
    assert "SUPER_ADMIN" not in response.text and "instance_role" not in response.text

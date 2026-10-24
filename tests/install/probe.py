"""설치 결과의 외부 동작을 비밀 없이 요약한다(#69). 두 설치 방식의 출력이 같아야 한다.

사용: python3 probe.py <기준 주소> <bootstrap 토큰>
표준 출력에 JSON 한 개. 토큰·쿠키 값·이메일은 출력하지 않는다.
"""

from __future__ import annotations

import http.cookiejar
import json
import sys
import urllib.error
import urllib.request
from typing import Any

SECURITY_HEADERS = ("x-content-type-options", "referrer-policy", "x-frame-options")


def request(
    opener: urllib.request.OpenerDirector,
    base: str,
    path: str,
    body: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
) -> tuple[int, dict[str, str], Any]:
    """POST(본문 있음)는 같은 출처 Origin과 JSON을 붙인다."""
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        base + path,
        data=data,
        method="POST" if body is not None else "GET",
        headers={
            **(
                {"Content-Type": "application/json", "Origin": base}
                if body is not None
                else {}
            ),
            **(headers or {}),
        },
    )
    try:
        with opener.open(req, timeout=15) as response:
            raw = response.read()
            status, response_headers = response.status, dict(response.headers.items())
    except urllib.error.HTTPError as error:
        raw, status, response_headers = (
            error.read(),
            error.code,
            dict(error.headers.items()),
        )
    try:
        parsed = json.loads(raw) if raw else None
    except ValueError:
        parsed = "<html>" if raw.lstrip().startswith(b"<") else "<text>"
    return status, {k.lower(): v for k, v in response_headers.items()}, parsed


def main(base: str, token: str) -> dict[str, Any]:
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    out: dict[str, Any] = {}

    status, headers, body = request(opener, base, "/api/v1/system/ready")
    out["ready"] = (status, body)
    status, headers, _ = request(opener, base, "/")
    out["root"] = status
    out["security_headers"] = {h: headers.get(h) for h in SECURITY_HEADERS}
    out["server_header_hidden"] = "server" not in headers
    out["spa_routes"] = {
        p: request(opener, base, p)[0] for p in ("/setup", "/login", "/admin")
    }
    out["setup_status_before"] = request(opener, base, "/api/v1/setup/status")[2]
    signup = {
        "email": "probe@example.com",
        "password": "long enough pass",
        "display_name": "P",
    }
    out["signup_default"] = request(opener, base, "/api/v1/auth/signup", signup)[:3:2]
    evil = request(
        opener,
        base,
        "/api/v1/auth/login",
        {"email": "a@b.co", "password": "x"},
        {"Origin": "https://evil.example"},
    )
    out["cross_site_login"] = (evil[0], evil[2])
    wrong = {
        "bootstrap_token": "aob_wrong",
        "email": "root@example.com",
        "password": "an admin passphrase",
        "display_name": "Root",
    }
    out["bootstrap_wrong_token"] = request(opener, base, "/api/v1/setup/admin", wrong)[
        :3:2
    ]
    admin = {**wrong, "bootstrap_token": token}
    status, headers, body = request(opener, base, "/api/v1/setup/admin", admin)
    cookie = headers.get("set-cookie", "").lower()
    out["bootstrap_admin"] = (status, (body or {}).get("user", {}).get("instance_role"))
    out["session_cookie_flags"] = sorted(
        flag
        for flag in ("httponly", "secure", "samesite=lax", "path=/")
        if flag in cookie
    )
    csrf = (body or {}).get("csrf_token", "")
    # 쿠키는 Secure라 평문 HTTP 로컬 점검에서는 자동으로 돌아가지 않는다(정상). HTTPS 앞단을 흉내 내
    # 직접 붙인다. 값은 출력하지 않는다.
    raw_cookie = headers.get("set-cookie", "").split(";", 1)[0]
    auth = {"Cookie": raw_cookie}
    status, _, metrics = request(opener, base, "/api/v1/admin/metrics", headers=auth)
    out["admin_metrics"] = status
    out["security_codes"] = sorted(
        c["code"] for c in (metrics or {}).get("security", [])
    )
    out["readiness_in_metrics"] = (metrics or {}).get("readiness")
    no_csrf = request(opener, base, "/api/v1/setup/complete", {}, auth)
    out["write_without_csrf"] = no_csrf[0]
    done = request(
        opener, base, "/api/v1/setup/complete", {}, {**auth, "X-CSRF-Token": csrf}
    )
    out["setup_complete"] = done[0]
    out["setup_status_after"] = request(opener, base, "/api/v1/setup/status")[2]
    return out


if __name__ == "__main__":
    print(
        json.dumps(
            main(sys.argv[1], sys.argv[2]), ensure_ascii=False, sort_keys=True, indent=1
        )
    )

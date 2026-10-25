"""Google/GitHub OAuth 클라이언트: 설정 조회, state·PKCE, 코드 교환, 신원 확인(#24).

제공자에게서 받는 것 중 서버에 남기는 것은 불변 subject(Google `sub`, GitHub 숫자 `id`)와
가입 시 계정 이메일(제공자가 검증한 주소)뿐이다. 액세스 토큰은 신원 확인 직후 버리고
저장·기록하지 않는다. HTTP 호출은 `set_http`로 바꿀 수 있어 테스트는 가짜 제공자를 쓴다.
"""

from __future__ import annotations

import base64
import hashlib
import json
import logging
import secrets
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from sqlalchemy import Connection

from agent_office import instance, secret_store
from agent_office.auth import accounts, mailer

logger = logging.getLogger(__name__)

HTTP_TIMEOUT_SECONDS = 10
MAX_RESPONSE_BYTES = 256 * 1024
USER_AGENT = "agent-office-server"


@dataclass(frozen=True)
class ProviderSpec:
    name: str
    authorize_url: str
    token_url: str
    scope: str


PROVIDERS: dict[str, ProviderSpec] = {
    "google": ProviderSpec(
        name="google",
        authorize_url="https://accounts.google.com/o/oauth2/v2/auth",
        token_url="https://oauth2.googleapis.com/token",
        scope="openid email",
    ),
    "github": ProviderSpec(
        name="github",
        authorize_url="https://github.com/login/oauth/authorize",
        token_url="https://github.com/login/oauth/access_token",
        # 이메일 목록(검증 여부 포함)을 읽는 최소 범위. 저장소 권한은 요청하지 않는다.
        scope="user:email",
    ),
}
GOOGLE_USERINFO_URL = "https://openidconnect.googleapis.com/v1/userinfo"
GITHUB_USER_URL = "https://api.github.com/user"
GITHUB_EMAILS_URL = "https://api.github.com/user/emails"


class OAuthError(Exception):
    """제공자 통신·응답 실패. `code`는 웹에 보낼 결과 코드다(원문 응답은 담지 않는다)."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


@dataclass(frozen=True)
class ClientConfig:
    client_id: str
    client_secret: str


@dataclass(frozen=True)
class Identity:
    subject: str
    # 제공자가 검증했다고 알린 주소만. 검증되지 않았으면 None.
    verified_email: str | None


# (method, url, headers, body) -> (status, parsed JSON). 실패는 OAuthError.
HttpCall = Callable[[str, str, dict[str, str], bytes | None], tuple[int, Any]]


def _urllib_http(
    method: str, url: str, headers: dict[str, str], body: bytes | None
) -> tuple[int, Any]:
    request = urllib.request.Request(url, data=body, method=method, headers=headers)  # noqa: S310
    try:
        with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT_SECONDS) as response:  # noqa: S310
            status = int(response.status)
            raw = response.read(MAX_RESPONSE_BYTES + 1)
    except urllib.error.HTTPError as error:
        status = int(error.code)
        raw = error.read(MAX_RESPONSE_BYTES + 1)
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        logger.warning("oauth provider request failed: %s", type(error).__name__)
        raise OAuthError("provider_unreachable") from error
    if len(raw) > MAX_RESPONSE_BYTES:
        raise OAuthError("provider_error")
    try:
        return status, json.loads(raw) if raw else None
    except ValueError as error:
        raise OAuthError("provider_error") from error


_http: HttpCall = _urllib_http


def set_http(call: HttpCall | None) -> None:
    """테스트용. None이면 실제 urllib 호출로 되돌린다."""
    global _http
    _http = call or _urllib_http


def client_config(connection: Connection, provider: str) -> ClientConfig | None:
    """관리자가 켜고 client ID·secret을 모두 저장한 제공자만. 아니면 None(기능 비활성)."""
    if provider not in PROVIDERS:
        return None
    stored = instance.get_dict(connection, instance.oauth_key(provider))
    client_id = str(stored.get("client_id") or "")
    if stored.get("enabled") is not True or not client_id:
        return None
    try:
        secret = secret_store.get(connection, instance.oauth_secret(provider))
    except (secret_store.SecretKeyMissing, secret_store.SecretUnreadable):
        logger.warning("oauth client secret unreadable for %s", provider)
        return None
    return ClientConfig(client_id=client_id, client_secret=secret) if secret else None


def enabled_providers(connection: Connection) -> list[str]:
    """비밀을 복호화하지 않고 설정 여부만 본다(목록 조회는 로그인 전에도 열려 있다)."""
    if mailer.public_url(connection) is None:
        return []
    result = []
    for provider in PROVIDERS:
        stored = instance.get_dict(connection, instance.oauth_key(provider))
        if (
            stored.get("enabled") is True
            and stored.get("client_id")
            and secret_store.exists(connection, instance.oauth_secret(provider))
        ):
            result.append(provider)
    return result


def redirect_uri(public_url: str, provider: str) -> str:
    return f"{public_url}/api/v1/auth/oauth/{provider}/callback"


def new_state() -> str:
    return secrets.token_urlsafe(32)


def new_code_verifier() -> str:
    """RFC 7636: 43~128자 unreserved 문자. 32바이트 → 43자."""
    return secrets.token_urlsafe(32)


def code_challenge(verifier: str) -> str:
    digest = hashlib.sha256(verifier.encode("ascii")).digest()
    return base64.urlsafe_b64encode(digest).decode().rstrip("=")


def authorize_url(
    provider: str, config: ClientConfig, *, callback: str, state: str, verifier: str
) -> str:
    spec = PROVIDERS[provider]
    params = {
        "client_id": config.client_id,
        "redirect_uri": callback,
        "response_type": "code",
        "scope": spec.scope,
        "state": state,
        "code_challenge": code_challenge(verifier),
        "code_challenge_method": "S256",
    }
    if provider == "google":
        params["prompt"] = "select_account"
    else:
        # GitHub: 가입 화면을 띄우지 않는다(기존 계정으로만 인가).
        params["allow_signup"] = "false"
    return f"{spec.authorize_url}?{urllib.parse.urlencode(params)}"


def _form(values: dict[str, str]) -> bytes:
    return urllib.parse.urlencode(values).encode()


def exchange_code(
    provider: str, config: ClientConfig, *, code: str, callback: str, verifier: str
) -> str:
    """인가 코드를 액세스 토큰으로 바꾼다. 토큰은 호출자가 신원 확인에만 쓰고 버린다."""
    spec = PROVIDERS[provider]
    status, payload = _http(
        "POST",
        spec.token_url,
        {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "User-Agent": USER_AGENT,
        },
        _form(
            {
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": callback,
                "client_id": config.client_id,
                "client_secret": config.client_secret,
                "code_verifier": verifier,
            }
        ),
    )
    token = payload.get("access_token") if isinstance(payload, dict) else None
    if status != 200 or not isinstance(token, str) or not token:
        # 응답 본문(오류 설명 포함)은 기록하지 않는다.
        logger.warning("oauth token exchange rejected by %s: status %s", provider, status)
        raise OAuthError("exchange_failed")
    return token


def _get_json(url: str, access_token: str, accept: str) -> Any:
    status, payload = _http(
        "GET",
        url,
        {"Authorization": f"Bearer {access_token}", "Accept": accept, "User-Agent": USER_AGENT},
        None,
    )
    if status != 200:
        logger.warning("oauth identity request rejected: status %s", status)
        raise OAuthError("identity_failed")
    return payload


def fetch_identity(provider: str, access_token: str) -> Identity:
    if provider == "google":
        info = _get_json(GOOGLE_USERINFO_URL, access_token, "application/json")
        if not isinstance(info, dict):
            raise OAuthError("identity_failed")
        subject = info.get("sub")
        email = info.get("email")
        verified = info.get("email_verified") is True
        if not isinstance(subject, str) or not subject or len(subject) > 255:
            raise OAuthError("identity_failed")
        return Identity(
            subject=subject,
            verified_email=_normalized(email) if verified else None,
        )
    accept = "application/vnd.github+json"
    user = _get_json(GITHUB_USER_URL, access_token, accept)
    raw_id = user.get("id") if isinstance(user, dict) else None
    # bool은 int의 하위 형이라 따로 막는다.
    if not isinstance(raw_id, int) or isinstance(raw_id, bool) or raw_id <= 0:
        raise OAuthError("identity_failed")
    emails = _get_json(GITHUB_EMAILS_URL, access_token, accept)
    verified_email = None
    if isinstance(emails, list):
        for entry in emails:
            if (
                isinstance(entry, dict)
                and entry.get("primary") is True
                and entry.get("verified") is True
            ):
                verified_email = _normalized(entry.get("email"))
                break
    return Identity(subject=str(raw_id), verified_email=verified_email)


def _normalized(value: object) -> str | None:
    return accounts.normalize_email(value) if isinstance(value, str) else None

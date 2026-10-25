"""Google/GitHub OAuth 로그인·가입·계정 연결(#24).

- state: 무작위 값. DB에는 해시만 두고 10분 뒤 만료, 한 번 쓰면 지운다.
- 로그인 CSRF 방지: 시작한 브라우저에만 HttpOnly 쿠키(PKCE verifier)를 주고, 콜백에서 state와
  쿠키가 같은 시작 요청에서 나왔는지 확인한다. 다른 사람의 인가 코드를 심어도 쿠키가 없어 실패한다.
- PKCE(S256)는 Google·GitHub 모두 보낸다.
- 이메일이 같다는 이유로 기존 계정에 자동 연결하지 않는다(계정 탈취 위험). 기존 계정은 먼저
  로그인한 뒤 계정 화면에서 연결한다.
- 결과는 웹 주소의 조각(`#oauth=<코드>`)으로만 알린다. 토큰·이메일은 URL에 넣지 않는다.
"""

from __future__ import annotations

import datetime as dt
import logging
import uuid
from typing import Annotated, Literal

from fastapi import APIRouter, Cookie, HTTPException, Path, Query, Request, Response, status
from fastapi.responses import RedirectResponse
from pydantic import BaseModel, ConfigDict
from sqlalchemy import Connection, delete, func, select
from sqlalchemy.exc import IntegrityError

from agent_office import audit
from agent_office.api.accounts import client_host, start_session
from agent_office.api.auth import SessionUser
from agent_office.api.deps import CsrfDep, DbConnection, UserDep, authenticate_session
from agent_office.auth import accounts, mailer, oauth
from agent_office.auth.sessions import SESSION_COOKIE, cookie_secure
from agent_office.db import schema
from agent_office.ratelimit import KeyedRateLimiter
from agent_office.security import constant_time_equal, token_hash

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/api/v1/auth", tags=["auth"])

OAUTH_COOKIE = "ao_oauth"
OAUTH_COOKIE_PATH = "/api/v1/auth/oauth"
STATE_LIFETIME = dt.timedelta(minutes=10)

start_limiter = KeyedRateLimiter(limit=30, window_seconds=600)
callback_limiter = KeyedRateLimiter(limit=30, window_seconds=600)
LIMITERS = (start_limiter, callback_limiter)

Provider = Literal["google", "github"]
Intent = Literal["login", "link"]


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ProvidersResponse(StrictModel):
    providers: list[str]


class IdentityView(StrictModel):
    provider: str
    linked_at: dt.datetime


class IdentitiesResponse(StrictModel):
    password_set: bool
    identities: list[IdentityView]


@router.get("/oauth/providers", response_model=ProvidersResponse)
def list_providers(connection: DbConnection) -> ProvidersResponse:
    """설정이 끝난 제공자 이름만. client ID·secret은 내보내지 않는다."""
    return ProvidersResponse(providers=oauth.enabled_providers(connection))


def _web_redirect(connection: Connection, path: str, code: str) -> RedirectResponse:
    base = mailer.public_url(connection) or ""
    response = RedirectResponse(f"{base}{path}#oauth={code}", status_code=status.HTTP_302_FOUND)
    response.headers["Cache-Control"] = "no-store"
    response.headers["Referrer-Policy"] = "no-referrer"
    return response


def _clear_cookie(response: Response) -> None:
    response.delete_cookie(
        OAUTH_COOKIE, path=OAUTH_COOKIE_PATH, secure=cookie_secure(), httponly=True, samesite="lax"
    )


def _limited(limiter: KeyedRateLimiter, request: Request) -> bool:
    return limiter.hit(f"oauth:{client_host(request)}") is not None


def _open_invitation_id(connection: Connection, token: str) -> uuid.UUID | None:
    """시작 단계에서는 이메일을 아직 모르므로 토큰만 확인한다. 이메일은 콜백에서 맞춘다."""
    invitations = schema.team_invitations
    if not token or len(token) > 256:
        return None
    value = connection.execute(
        select(invitations.c.id).where(
            invitations.c.token_hash == token_hash(token),
            invitations.c.accepted_at.is_(None),
            invitations.c.revoked_at.is_(None),
            invitations.c.expires_at > func.now(),
        )
    ).scalar_one_or_none()
    return None if value is None else uuid.UUID(str(value))


@router.get("/oauth/{provider}/start", status_code=status.HTTP_302_FOUND)
def start(
    provider: Annotated[Provider, Path()],
    request: Request,
    connection: DbConnection,
    intent: Annotated[Intent, Query()] = "login",
    invite: Annotated[str | None, Query(max_length=256)] = None,
    session_token: Annotated[str | None, Cookie(alias=SESSION_COOKIE)] = None,
) -> RedirectResponse:
    """브라우저를 제공자 인가 화면으로 보낸다. 계정 연결은 로그인한 사용자만 시작할 수 있다."""
    back = "/account" if intent == "link" else "/login"
    if _limited(start_limiter, request):
        return _web_redirect(connection, back, "rate_limited")
    config = oauth.client_config(connection, provider)
    public_url = mailer.public_url(connection)
    if config is None or public_url is None:
        return _web_redirect(connection, back, "provider_disabled")
    user_id = None
    if intent == "link":
        user = authenticate_session(connection, session_token)
        if user is None:
            return _web_redirect(connection, "/login", "login_required")
        user_id = user.id
    invitation_id = _open_invitation_id(connection, invite) if invite else None
    if invite and invitation_id is None:
        return _web_redirect(connection, back, "invalid_invite")

    state = oauth.new_state()
    verifier = oauth.new_code_verifier()
    connection.execute(
        schema.oauth_states.insert().values(
            state_hash=token_hash(state),
            binding_hash=token_hash(verifier),
            provider=provider,
            intent=intent,
            user_id=user_id,
            invitation_id=invitation_id,
            expires_at=func.now() + STATE_LIFETIME,
        )
    )
    callback = oauth.redirect_uri(public_url, provider)
    response = RedirectResponse(
        oauth.authorize_url(provider, config, callback=callback, state=state, verifier=verifier),
        status_code=status.HTTP_302_FOUND,
    )
    response.headers["Cache-Control"] = "no-store"
    response.headers["Referrer-Policy"] = "no-referrer"
    # 제공자에서 돌아오는 최상위 GET 이동에도 실리도록 SameSite=Lax.
    response.set_cookie(
        OAUTH_COOKIE,
        verifier,
        max_age=int(STATE_LIFETIME.total_seconds()),
        path=OAUTH_COOKIE_PATH,
        secure=cookie_secure(),
        httponly=True,
        samesite="lax",
    )
    return response


def _consume_state(
    connection: Connection, provider: str, state: str
) -> tuple[bytes, str, uuid.UUID | None, uuid.UUID | None] | None:
    states = schema.oauth_states
    row = connection.execute(
        delete(states)
        .where(
            states.c.state_hash == token_hash(state),
            states.c.provider == provider,
            states.c.expires_at > func.now(),
        )
        .returning(states.c.binding_hash, states.c.intent, states.c.user_id, states.c.invitation_id)
    ).one_or_none()
    if row is None:
        return None
    return bytes(row.binding_hash), str(row.intent), row.user_id, row.invitation_id


def _invitation_matches(connection: Connection, invitation_id: uuid.UUID, email: str) -> bool:
    invitations = schema.team_invitations
    return (
        connection.execute(
            select(invitations.c.id).where(
                invitations.c.id == invitation_id,
                invitations.c.email == email,
                invitations.c.accepted_at.is_(None),
                invitations.c.revoked_at.is_(None),
                invitations.c.expires_at > func.now(),
            )
        ).first()
        is not None
    )


def _fail(
    connection: Connection, path: str, code: str, provider: str, user_id: uuid.UUID | None = None
) -> RedirectResponse:
    audit.record(
        connection,
        "auth.oauth",
        "failure",
        actor_user_id=user_id,
        details={"reason": f"{provider}:{code}"},
    )
    response = _web_redirect(connection, path, code)
    _clear_cookie(response)
    return response


@router.get("/oauth/{provider}/callback", status_code=status.HTTP_302_FOUND)
def callback(
    provider: Annotated[Provider, Path()],
    request: Request,
    connection: DbConnection,
    code: Annotated[str | None, Query(max_length=2048)] = None,
    state: Annotated[str | None, Query(max_length=256)] = None,
    error: Annotated[str | None, Query(max_length=256)] = None,
    binding: Annotated[str | None, Cookie(alias=OAUTH_COOKIE)] = None,
    session_token: Annotated[str | None, Cookie(alias=SESSION_COOKIE)] = None,
) -> RedirectResponse:
    if _limited(callback_limiter, request):
        return _fail(connection, "/login", "rate_limited", provider)
    stored = _consume_state(connection, provider, state) if state else None
    if stored is None or not binding or len(binding) > 256:
        return _fail(connection, "/login", "state_invalid", provider)
    binding_hash, intent, link_user_id, invitation_id = stored
    back = "/account" if intent == "link" else "/login"
    if not constant_time_equal(token_hash(binding), binding_hash):
        return _fail(connection, back, "state_invalid", provider)
    if error or not code:
        # 사용자가 동의 화면에서 취소했거나 제공자가 거절했다. 제공자 설명 문구는 버린다.
        return _fail(connection, back, "denied", provider)
    config = oauth.client_config(connection, provider)
    public_url = mailer.public_url(connection)
    if config is None or public_url is None:
        return _fail(connection, back, "provider_disabled", provider)
    try:
        access_token = oauth.exchange_code(
            provider,
            config,
            code=code,
            callback=oauth.redirect_uri(public_url, provider),
            verifier=binding,
        )
        # 액세스 토큰은 신원 확인에만 쓰고 저장하지 않는다.
        identity = oauth.fetch_identity(provider, access_token)
    except oauth.OAuthError as failure:
        return _fail(connection, back, failure.code, provider)

    if intent == "link":
        return _link(connection, provider, identity, link_user_id, session_token)
    return _login(connection, provider, identity, invitation_id, session_token)


def _identity_owner(connection: Connection, provider: str, subject: str) -> uuid.UUID | None:
    identities = schema.auth_identities
    value = connection.execute(
        select(identities.c.user_id).where(
            identities.c.provider == provider, identities.c.provider_subject == subject
        )
    ).scalar_one_or_none()
    return None if value is None else uuid.UUID(str(value))


def _link(
    connection: Connection,
    provider: str,
    identity: oauth.Identity,
    link_user_id: uuid.UUID | None,
    session_token: str | None,
) -> RedirectResponse:
    user = authenticate_session(connection, session_token)
    # 시작한 사용자와 지금 로그인한 사용자가 같아야 한다(중간에 계정이 바뀌면 거부).
    if user is None or link_user_id is None or user.id != link_user_id:
        return _fail(connection, "/login", "login_required", provider)
    owner = _identity_owner(connection, provider, identity.subject)
    if owner == user.id:
        return _web_redirect(connection, "/account", "already_linked")
    if owner is not None:
        return _fail(connection, "/account", "identity_in_use", provider, user.id)
    try:
        with connection.begin_nested():
            connection.execute(
                schema.auth_identities.insert().values(
                    user_id=user.id, provider=provider, provider_subject=identity.subject
                )
            )
    except IntegrityError:
        # 같은 제공자의 다른 계정이 이미 연결돼 있다(사용자당 제공자 하나).
        return _fail(connection, "/account", "provider_already_linked", provider, user.id)
    audit.record(
        connection,
        "auth.oauth.linked",
        "success",
        actor_user_id=user.id,
        details={"reason": provider},
    )
    return _web_redirect(connection, "/account", "linked")


def _signed_in(
    connection: Connection, user_id: uuid.UUID, previous: str | None, code: str
) -> RedirectResponse:
    users = schema.users
    row = connection.execute(
        select(users.c.display_name, users.c.instance_role).where(users.c.id == user_id)
    ).one()
    response = _web_redirect(connection, "/", code)
    start_session(
        connection,
        response,
        SessionUser(id=user_id, display_name=row.display_name, instance_role=row.instance_role),
        previous,
    )
    _clear_cookie(response)
    return response


def _login(
    connection: Connection,
    provider: str,
    identity: oauth.Identity,
    invitation_id: uuid.UUID | None,
    previous: str | None,
) -> RedirectResponse:
    users = schema.users
    owner = _identity_owner(connection, provider, identity.subject)
    if owner is not None:
        user_status = connection.execute(
            select(users.c.status).where(users.c.id == owner)
        ).scalar_one()
        if user_status == "disabled":
            return _fail(connection, "/login", "login_failed", provider, owner)
        if user_status != "active":
            return _fail(connection, "/login", "approval_pending", provider, owner)
        audit.record(
            connection,
            "auth.oauth.login",
            "success",
            actor_user_id=owner,
            details={"reason": provider},
        )
        return _signed_in(connection, owner, previous, "signed_in")

    email = identity.verified_email
    if email is None:
        return _fail(connection, "/login", "email_unverified", provider)
    existing = connection.execute(select(users.c.id).where(users.c.email == email)).first()
    if existing is not None:
        # 같은 이메일의 계정이 있어도 자동 연결하지 않는다. 로그인 후 계정 화면에서 연결한다.
        return _fail(connection, "/login", "account_exists_link_required", provider)
    policy = accounts.signup_policy(connection)
    if policy == "CLOSED":
        return _fail(connection, "/login", "signup_closed", provider)
    if policy == "INVITE_ONLY" and (
        invitation_id is None or not _invitation_matches(connection, invitation_id, email)
    ):
        return _fail(connection, "/login", "invite_required", provider)
    if accounts.domain_blocked(connection, email):
        return _fail(connection, "/login", "email_domain_blocked", provider)
    pending = policy == "ADMIN_APPROVAL"
    try:
        with connection.begin_nested():
            user_id = accounts.create_user(
                connection,
                email=email,
                display_name=email.split("@")[0][:80],
                password=None,
                status="pending" if pending else "active",
                email_verified=True,
            )
            connection.execute(
                schema.auth_identities.insert().values(
                    user_id=user_id, provider=provider, provider_subject=identity.subject
                )
            )
    except IntegrityError:
        # 동시에 같은 주소·신원으로 가입한 경우.
        return _fail(connection, "/login", "account_exists_link_required", provider)
    audit.record(
        connection,
        "auth.oauth.signup",
        "success",
        actor_user_id=user_id,
        details={"reason": provider},
    )
    if pending:
        response = _web_redirect(connection, "/login", "approval_pending")
        _clear_cookie(response)
        return response
    return _signed_in(connection, user_id, previous, "signed_up")


@router.get("/identities", response_model=IdentitiesResponse)
def list_identities(user: UserDep, connection: DbConnection) -> IdentitiesResponse:
    identities = schema.auth_identities
    rows = connection.execute(
        select(identities.c.provider, identities.c.created_at)
        .where(identities.c.user_id == user.id)
        .order_by(identities.c.provider)
    ).all()
    password_hash = connection.execute(
        select(schema.users.c.password_hash).where(schema.users.c.id == user.id)
    ).scalar_one()
    return IdentitiesResponse(
        password_set=password_hash is not None,
        identities=[IdentityView(provider=r.provider, linked_at=r.created_at) for r in rows],
    )


@router.delete("/identities/{provider}", status_code=status.HTTP_204_NO_CONTENT)
def unlink_identity(
    provider: Annotated[Provider, Path()], user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> Response:
    """연결 해제. 비밀번호가 없는 계정의 마지막 로그인 수단은 지울 수 없다."""
    users, identities = schema.users, schema.auth_identities
    # 동시 해제로 로그인 수단이 모두 사라지지 않게 사용자 행을 잠근다.
    password_hash = connection.execute(
        select(users.c.password_hash).where(users.c.id == user.id).with_for_update()
    ).scalar_one()
    linked = set(
        connection.execute(
            select(identities.c.provider).where(identities.c.user_id == user.id)
        ).scalars()
    )
    if provider not in linked:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
    if password_hash is None and len(linked) == 1:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="last_login_method")
    connection.execute(
        delete(identities).where(identities.c.user_id == user.id, identities.c.provider == provider)
    )
    audit.record(
        connection,
        "auth.oauth.unlinked",
        "success",
        actor_user_id=user.id,
        details={"reason": provider},
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)

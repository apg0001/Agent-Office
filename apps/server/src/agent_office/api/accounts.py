"""이메일 가입·검증·로그인·비밀번호 복구(#23).

계정 존재 여부가 드러나지 않도록 가입·복구 요청은 같은 응답을 돌려주고, 로그인 실패는 이유를
구분하지 않는다. 로그인 전 요청이라 CSRF 토큰이 없으므로 Origin·JSON 본문으로 교차 사이트
요청을 막는다.
"""

from __future__ import annotations

import logging
import uuid
from typing import Annotated

from fastapi import APIRouter, Cookie, Depends, HTTPException, Request, Response, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import Connection, func, select, update
from sqlalchemy.exc import IntegrityError

from agent_office import audit, retention
from agent_office.api import teams
from agent_office.api.auth import CurrentSessionResponse, SessionUser
from agent_office.api.deps import CsrfDep, DbConnection, UserDep
from agent_office.auth import accounts, mailer, passwords
from agent_office.auth import sessions as session_service
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.origin import origin_allowed
from agent_office.ratelimit import KeyedRateLimiter
from agent_office.security import token_hash

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/api/v1/auth", tags=["auth"])

# 키는 "종류:IP" 또는 "종류:이메일". 리버스 프록시 뒤 실제 IP 반영은 #57에서 한다.
signup_limiter = KeyedRateLimiter(limit=5, window_seconds=3600)
# 여러 IP로 나눠 들어오는 대량 가입을 막는 인스턴스 전체 한도(#28).
signup_global_limiter = KeyedRateLimiter(limit=30, window_seconds=3600)
verify_limiter = KeyedRateLimiter(limit=20, window_seconds=3600)
login_ip_limiter = KeyedRateLimiter(limit=30, window_seconds=600)
login_email_limiter = KeyedRateLimiter(limit=10, window_seconds=900)
reset_ip_limiter = KeyedRateLimiter(limit=10, window_seconds=3600)
reset_email_limiter = KeyedRateLimiter(limit=3, window_seconds=3600)
LIMITERS = (
    signup_limiter,
    signup_global_limiter,
    verify_limiter,
    login_ip_limiter,
    login_email_limiter,
    reset_ip_limiter,
    reset_email_limiter,
)


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class SignupRequest(StrictModel):
    email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    password: str = Field(max_length=1024)
    display_name: str = Field(min_length=1, max_length=80)
    # 초대 전용 정책에서 팀 초대 링크로 가입할 때(#27).
    invite_token: str | None = Field(default=None, max_length=256)
    # 사람에게는 보이지 않는 입력란(#28). 값이 있으면 자동 가입 봇으로 보고 조용히 무시한다.
    website: str | None = Field(default=None, max_length=200)


class TokenRequest(StrictModel):
    token: str = Field(max_length=256)


class LoginRequest(StrictModel):
    email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    password: str = Field(max_length=1024)


class ResetRequest(StrictModel):
    email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)


class ResetConfirm(StrictModel):
    token: str = Field(max_length=256)
    password: str = Field(max_length=1024)


class AcceptedResponse(StrictModel):
    status: str


def require_same_origin(request: Request) -> None:
    """로그인 전 쓰기 요청: 다른 사이트의 폼·스크립트가 보낸 요청을 거부한다."""
    content_type = request.headers.get("content-type", "").split(";")[0].strip().lower()
    if content_type != "application/json" or not origin_allowed(
        request.headers.get("origin"), request.headers.get("host")
    ):
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="cross_site_rejected")


SameOrigin = Annotated[None, Depends(require_same_origin)]


def client_host(request: Request) -> str:
    return request.client.host if request.client else "unknown"


def enforce_limit(limiter: KeyedRateLimiter, key: str) -> None:
    retry = limiter.hit(key)
    if retry is not None:
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS,
            detail="rate_limited",
            headers={"Retry-After": str(max(1, int(retry)))},
        )


def _bad_request(code: str) -> HTTPException:
    return HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=code)


def _require_mail(connection: Connection) -> mailer.Mailer:
    sender = mailer.get_mailer(connection)
    if not sender.configured:
        # 메일을 보낼 수 없으면 검증·복구가 불가능한 계정을 만들지 않는다.
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="mail_not_configured"
        )
    return sender


def _link(connection: Connection, path: str, token: str) -> str:
    # 토큰은 URL 조각(#)에 넣어 서버 접근 로그·Referer에 남지 않게 한다.
    return f"{mailer.public_url(connection)}{path}#token={token}"


@router.post("/signup", status_code=status.HTTP_202_ACCEPTED, response_model=AcceptedResponse)
def signup(
    body: SignupRequest, request: Request, _origin: SameOrigin, connection: DbConnection
) -> AcceptedResponse:
    enforce_limit(signup_limiter, f"signup:{client_host(request)}")
    if body.website:
        return AcceptedResponse(status="verification_sent")
    enforce_limit(signup_global_limiter, "signup:all")
    policy = accounts.signup_policy(connection)
    if policy == "CLOSED":
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="signup_closed")
    email = accounts.normalize_email(body.email)
    if policy == "INVITE_ONLY" and (
        email is None or teams.open_invitation(connection, body.invite_token or "", email) is None
    ):
        # 초대받은 주소와 초대 토큰이 함께 맞아야 한다. 팀 합류는 로그인 후 수락에서 한다.
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="invite_required")
    if email is None:
        raise _bad_request("invalid_email")
    if accounts.domain_blocked(connection, email):
        raise _bad_request("email_domain_blocked")
    problem = passwords.password_problem(body.password)
    if problem:
        raise _bad_request(problem)
    sender = _require_mail(connection)
    display_name = body.display_name.strip() or email.split("@")[0]
    try:
        with connection.begin_nested():
            user_id = accounts.create_user(
                connection, email=email, display_name=display_name, password=body.password
            )
    except IntegrityError:
        # 이미 가입된 주소: 응답은 같게 두고 본인에게만 안내 메일을 보낸다.
        sender.send(
            mailer.OutgoingMail(
                to=email,
                subject="Agent Office 가입 안내",
                body=(
                    "이 주소로 이미 계정이 있습니다. 비밀번호가 기억나지 않으면 로그인 화면에서 "
                    "비밀번호 복구를 사용하세요.\n본인이 요청하지 않았다면 이 메일을 무시하세요."
                ),
            )
        )
        return AcceptedResponse(status="verification_sent")
    token = accounts.issue_one_time_token(
        connection, schema.email_verifications, user_id, accounts.EMAIL_VERIFICATION_LIFETIME
    )
    audit.record(connection, "auth.signup", "success", actor_user_id=user_id)
    sender.send(
        mailer.OutgoingMail(
            to=email,
            subject="Agent Office 이메일 확인",
            body=(
                "아래 링크를 24시간 안에 열어 이메일을 확인하세요.\n"
                f"{_link(connection, '/verify-email', token)}\n"
                "본인이 요청하지 않았다면 이 메일을 무시하세요."
            ),
        )
    )
    return AcceptedResponse(status="verification_sent")


def _mark_verified(connection: Connection, user_id: uuid.UUID, *, activate: bool) -> None:
    users = schema.users
    connection.execute(
        update(users)
        .where(users.c.id == user_id, users.c.email_verified_at.is_(None))
        .values(email_verified_at=func.now(), updated_at=func.now())
    )
    if activate:
        connection.execute(
            update(users)
            .where(users.c.id == user_id, users.c.status == "pending")
            .values(status="active", updated_at=func.now())
        )


@router.post("/verify-email", status_code=status.HTTP_204_NO_CONTENT)
def verify_email(
    body: TokenRequest, request: Request, _origin: SameOrigin, connection: DbConnection
) -> Response:
    enforce_limit(verify_limiter, f"verify:{client_host(request)}")
    user_id = accounts.consume_one_time_token(connection, schema.email_verifications, body.token)
    if user_id is None:
        raise _bad_request("invalid_token")
    # 관리자 승인 정책이면 확인 후에도 승인 전까지 대기 상태로 둔다.
    approved = accounts.signup_policy(connection) != "ADMIN_APPROVAL"
    _mark_verified(connection, user_id, activate=approved)
    audit.record(connection, "auth.email_verified", "success", actor_user_id=user_id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


def _invalid_credentials() -> HTTPException:
    return HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid_credentials")


@router.post("/login", response_model=CurrentSessionResponse)
def login(
    body: LoginRequest,
    request: Request,
    response: Response,
    _origin: SameOrigin,
    connection: DbConnection,
    previous: Annotated[str | None, Cookie(alias=SESSION_COOKIE)] = None,
) -> CurrentSessionResponse:
    enforce_limit(login_ip_limiter, f"login:{client_host(request)}")
    email = accounts.normalize_email(body.email)
    enforce_limit(login_email_limiter, f"login:{email or '-'}")
    users = schema.users
    row = (
        connection.execute(
            select(
                users.c.id,
                users.c.display_name,
                users.c.password_hash,
                users.c.instance_role,
                users.c.status,
                users.c.email_verified_at,
            ).where(users.c.email == email)
        ).one_or_none()
        if email
        else None
    )
    # 없는 계정도 같은 비용으로 검증해 응답 시간으로 존재 여부가 드러나지 않게 한다.
    if not passwords.verify_password(row.password_hash if row else None, body.password):
        if row is not None:
            audit.record(connection, "auth.login", "failure", actor_user_id=row.id)
        raise _invalid_credentials()
    if row is None or row.status == "disabled":
        raise _invalid_credentials()
    if row.email_verified_at is None:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="email_not_verified")
    if row.status != "active":
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="approval_pending")
    if passwords.needs_rehash(row.password_hash):
        connection.execute(
            update(users)
            .where(users.c.id == row.id)
            .values(password_hash=passwords.hash_password(body.password))
        )
    audit.record(connection, "auth.login", "success", actor_user_id=row.id)
    return start_session(
        connection,
        response,
        SessionUser(id=row.id, display_name=row.display_name, instance_role=row.instance_role),
        previous,
    )


def start_session(
    connection: Connection, response: Response, user: SessionUser, previous: str | None
) -> CurrentSessionResponse:
    """새 브라우저 세션을 발급해 쿠키로 내보낸다. 최초 관리자 생성(#66)도 쓴다."""
    token = session_service.issue_session(connection, user.id, replaces_token=previous)
    session_service.set_session_cookie(response, token)
    expires_at = connection.execute(
        select(schema.login_sessions.c.expires_at).where(
            schema.login_sessions.c.token_hash == token_hash(token)
        )
    ).scalar_one()
    return CurrentSessionResponse(
        user=user,
        csrf_token=session_service.csrf_token_for(token),
        expires_at=expires_at,
    )


@router.post(
    "/password-reset/request",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=AcceptedResponse,
)
def request_password_reset(
    body: ResetRequest, request: Request, _origin: SameOrigin, connection: DbConnection
) -> AcceptedResponse:
    enforce_limit(reset_ip_limiter, f"reset:{client_host(request)}")
    sender = _require_mail(connection)
    email = accounts.normalize_email(body.email)
    accepted = AcceptedResponse(status="reset_sent_if_exists")
    if email is None:
        return accepted
    enforce_limit(reset_email_limiter, f"reset:{email}")
    users = schema.users
    user_id = connection.execute(
        select(users.c.id).where(users.c.email == email, users.c.status != "disabled")
    ).scalar_one_or_none()
    if user_id is None:
        return accepted
    token = accounts.issue_one_time_token(
        connection, schema.password_resets, user_id, accounts.PASSWORD_RESET_LIFETIME
    )
    audit.record(connection, "auth.password_reset.requested", "success", actor_user_id=user_id)
    sender.send(
        mailer.OutgoingMail(
            to=email,
            subject="Agent Office 비밀번호 재설정",
            body=(
                "아래 링크를 1시간 안에 열어 새 비밀번호를 정하세요.\n"
                f"{_link(connection, '/reset-password', token)}\n"
                "본인이 요청하지 않았다면 이 메일을 무시하세요. 비밀번호는 바뀌지 않습니다."
            ),
        )
    )
    return accepted


@router.post("/password-reset/confirm", status_code=status.HTTP_204_NO_CONTENT)
def confirm_password_reset(
    body: ResetConfirm, request: Request, _origin: SameOrigin, connection: DbConnection
) -> Response:
    enforce_limit(verify_limiter, f"reset-confirm:{client_host(request)}")
    problem = passwords.password_problem(body.password)
    if problem:
        raise _bad_request(problem)
    user_id = accounts.consume_one_time_token(connection, schema.password_resets, body.token)
    if user_id is None:
        raise _bad_request("invalid_token")
    users = schema.users
    connection.execute(
        update(users)
        .where(users.c.id == user_id)
        .values(password_hash=passwords.hash_password(body.password), updated_at=func.now())
    )
    # 메일을 받았다는 것은 주소 소유 증명이다. 승인 상태는 바꾸지 않는다.
    _mark_verified(connection, user_id, activate=False)
    # 탈취된 세션을 끊기 위해 모든 브라우저 세션을 폐기한다(장치 토큰은 유지).
    session_service.revoke_all(connection, user_id)
    audit.record(connection, "auth.password_reset", "success", actor_user_id=user_id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


class DeleteAccountRequest(StrictModel):
    confirm_email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    password: str | None = Field(default=None, max_length=1024)


@router.delete("/account", status_code=status.HTTP_204_NO_CONTENT)
def delete_account(
    body: DeleteAccountRequest, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> Response:
    """계정 삭제(#39). 이메일을 다시 입력하고, 비밀번호가 있는 계정은 비밀번호도 확인한다.

    지워지는 것: 계정·장치·세션·이벤트·프로젝트·공유·혼자인 팀. 감사 로그에는 사용자 ID만 남고
    (행위자 연결은 끊김), 삭제 기록(`deleted_users`)은 백업 복원 후 재삭제에 쓴다.
    """
    users, members = schema.users, schema.team_members
    row = connection.execute(
        select(users.c.email, users.c.password_hash, users.c.instance_role).where(
            users.c.id == user.id
        )
    ).one()
    if accounts.normalize_email(body.confirm_email) != row.email:
        raise _bad_request("confirm_email_mismatch")
    if row.password_hash is not None and not passwords.verify_password(
        row.password_hash, body.password or ""
    ):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid_credentials")
    if row.instance_role == "SUPER_ADMIN":
        others = connection.execute(
            select(func.count())
            .select_from(users)
            .where(
                users.c.instance_role == "SUPER_ADMIN",
                users.c.status == "active",
                users.c.id != user.id,
            )
        ).scalar_one()
        if others == 0:
            raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="last_super_admin")
    other_members = members.alias("other_members")
    shared_team = connection.execute(
        select(members.c.team_id)
        .join(
            other_members,
            (other_members.c.team_id == members.c.team_id) & (other_members.c.user_id != user.id),
        )
        .where(members.c.user_id == user.id, members.c.role == "OWNER")
        .limit(1)
    ).first()
    if shared_team is not None:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="owner_must_transfer")
    connection.execute(schema.deleted_users.insert().values(user_id=user.id))
    retention.remove_user(connection, user.id)
    audit.record(connection, "account.deleted", "success", target_type="user", target_id=user.id)
    response = Response(status_code=status.HTTP_204_NO_CONTENT)
    session_service.clear_session_cookie(response)
    return response

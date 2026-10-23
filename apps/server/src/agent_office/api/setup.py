"""설치 상태와 최초 관리자 생성(#66). 설치 마법사(#67)의 첫 단계다."""

from __future__ import annotations

from typing import Annotated

from fastapi import APIRouter, Cookie, HTTPException, Request, Response, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.exc import IntegrityError

from agent_office import audit
from agent_office.api.accounts import SameOrigin, client_host, enforce_limit, start_session
from agent_office.api.auth import CurrentSessionResponse, SessionUser
from agent_office.api.deps import CsrfDep, DbConnection, UserDep
from agent_office.auth import accounts, passwords
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.ratelimit import KeyedRateLimiter
from agent_office.setup import bootstrap

router = APIRouter(prefix="/api/v1/setup", tags=["setup"])

bootstrap_limiter = KeyedRateLimiter(limit=10, window_seconds=600)


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class SetupStatus(StrictModel):
    setup_completed: bool
    admin_exists: bool
    bootstrap_pending: bool


class CreateAdminRequest(StrictModel):
    bootstrap_token: str = Field(max_length=256)
    email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    password: str = Field(max_length=1024)
    display_name: str = Field(min_length=1, max_length=80)


@router.get("/status", response_model=SetupStatus)
def setup_status(connection: DbConnection) -> SetupStatus:
    """마법사가 어느 화면을 보여 줄지 정한다. 비밀은 없다."""
    return SetupStatus(
        setup_completed=bootstrap.setup_completed(connection),
        admin_exists=bootstrap.super_admin_exists(connection),
        bootstrap_pending=bootstrap.bootstrap_pending(connection),
    )


def _forbidden() -> HTTPException:
    # 토큰 오류·만료·이미 사용·설치 완료를 구분하지 않는다.
    return HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="bootstrap_rejected")


@router.post("/admin", status_code=status.HTTP_201_CREATED, response_model=CurrentSessionResponse)
def create_first_admin(
    body: CreateAdminRequest,
    request: Request,
    response: Response,
    _origin: SameOrigin,
    connection: DbConnection,
    previous: Annotated[str | None, Cookie(alias=SESSION_COOKIE)] = None,
) -> CurrentSessionResponse:
    enforce_limit(bootstrap_limiter, f"bootstrap:{client_host(request)}")
    email = accounts.normalize_email(body.email)
    if email is None:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="invalid_email")
    problem = passwords.password_problem(body.password)
    if problem:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=problem)
    if not bootstrap.consume_token(connection, body.bootstrap_token):
        raise _forbidden()
    display_name = body.display_name.strip() or email.split("@")[0]
    try:
        with connection.begin_nested():
            user_id = accounts.create_user(
                connection,
                email=email,
                display_name=display_name,
                password=body.password,
                instance_role="SUPER_ADMIN",
                status="active",
                # 콘솔 접근 권한이 있는 설치자가 직접 입력한 주소다. 메일 설정 전이라 확인 메일을
                # 보낼 수 없으므로 확인된 것으로 둔다.
                email_verified=True,
            )
    except IntegrityError as error:
        # 같은 이메일의 기존 계정이 있으면 토큰 폐기를 되돌리고 다시 시도하게 한다.
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT, detail="email_already_registered"
        ) from error
    audit.record(connection, "setup.bootstrap", "success", actor_user_id=user_id, target_id=user_id)
    return start_session(
        connection,
        response,
        SessionUser(id=user_id, display_name=display_name, instance_role="SUPER_ADMIN"),
        previous,
    )


@router.post("/complete", status_code=status.HTTP_204_NO_CONTENT)
def complete(user: UserDep, _csrf: CsrfDep, connection: DbConnection) -> Response:
    """설치 완료 잠금. 마법사 마지막 단계(#67)가 호출한다."""
    if user.instance_role != "SUPER_ADMIN":
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="forbidden")
    bootstrap.complete_setup(connection, user.id)
    audit.record(connection, "setup.completed", "success", actor_user_id=user.id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)

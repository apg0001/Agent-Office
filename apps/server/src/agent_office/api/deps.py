"""인증 의존성. 사용자·장치는 항상 서버가 자격증명으로 판정한다."""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from typing import Annotated

from fastapi import Cookie, Depends, Header, HTTPException, status
from sqlalchemy import Connection, func, or_, select

from agent_office.auth import sessions as session_service
from agent_office.auth.sessions import CSRF_HEADER, SESSION_COOKIE
from agent_office.db import schema
from agent_office.db.engine import get_connection
from agent_office.security import DEVICE_TOKEN_PREFIX, constant_time_equal, token_hash

__all__ = ["CSRF_HEADER", "SESSION_COOKIE"]

# 커밋이 끝난 뒤에 응답을 보낸다. 기본 scope("request")는 응답 후 커밋해서,
# 커밋이 실패해도 클라이언트가 이미 토큰을 받는 일이 생길 수 있다.
DbConnection = Annotated[Connection, Depends(get_connection, scope="function")]


@dataclass(frozen=True)
class CurrentUser:
    id: uuid.UUID
    instance_role: str
    session_id: uuid.UUID
    csrf_token: str


@dataclass(frozen=True)
class CurrentDevice:
    id: uuid.UUID
    owner_user_id: uuid.UUID
    credential_id: uuid.UUID
    credential_expires_at: object | None


def _unauthorized() -> HTTPException:
    return HTTPException(
        status_code=status.HTTP_401_UNAUTHORIZED,
        detail="unauthorized",
        headers={"WWW-Authenticate": "Bearer"},
    )


def current_user(
    connection: DbConnection,
    session_token: Annotated[str | None, Cookie(alias=SESSION_COOKIE)] = None,
) -> CurrentUser:
    """브라우저 세션을 검증한다. 폐기·절대 만료·유휴 만료·비활성 사용자를 모두 거부한다."""
    if not session_token or len(session_token) > 256:
        raise _unauthorized()
    sessions, users = schema.login_sessions, schema.users
    row = connection.execute(
        select(sessions.c.id.label("session_id"), users.c.id, users.c.instance_role)
        .select_from(sessions.join(users, users.c.id == sessions.c.user_id))
        .where(
            sessions.c.token_hash == token_hash(session_token),
            *session_service.active_session_filter(),
            users.c.status == "active",
        )
    ).one_or_none()
    if row is None:
        raise _unauthorized()
    session_service.touch(connection, row.session_id)
    return CurrentUser(
        id=row.id,
        instance_role=row.instance_role,
        session_id=row.session_id,
        csrf_token=session_service.csrf_token_for(session_token),
    )


def require_csrf(
    user: Annotated[CurrentUser, Depends(current_user)],
    token: Annotated[str | None, Header(alias=CSRF_HEADER)] = None,
) -> None:
    """쿠키 인증 쓰기 요청은 세션에서 유도한 CSRF 토큰을 헤더로 보내야 한다."""
    if not token or not constant_time_equal(token.encode(), user.csrf_token.encode()):
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="csrf_required")


def current_device(
    connection: DbConnection,
    authorization: Annotated[str | None, Header()] = None,
) -> CurrentDevice:
    scheme, _, token = (authorization or "").partition(" ")
    if scheme.lower() != "bearer" or not token.startswith(DEVICE_TOKEN_PREFIX) or len(token) > 256:
        raise _unauthorized()
    credentials, devices, users = schema.device_credentials, schema.devices, schema.users
    row = connection.execute(
        select(
            devices.c.id,
            devices.c.owner_user_id,
            credentials.c.id.label("credential_id"),
            credentials.c.expires_at,
        )
        .select_from(
            credentials.join(devices, devices.c.id == credentials.c.device_id).join(
                users, users.c.id == devices.c.owner_user_id
            )
        )
        .where(
            credentials.c.token_hash == token_hash(token),
            credentials.c.revoked_at.is_(None),
            or_(credentials.c.expires_at.is_(None), credentials.c.expires_at > func.now()),
            devices.c.revoked_at.is_(None),
            users.c.status == "active",
        )
    ).one_or_none()
    if row is None:
        raise _unauthorized()
    return CurrentDevice(
        id=row.id,
        owner_user_id=row.owner_user_id,
        credential_id=row.credential_id,
        credential_expires_at=row.expires_at,
    )


UserDep = Annotated[CurrentUser, Depends(current_user)]
DeviceDep = Annotated[CurrentDevice, Depends(current_device)]
CsrfDep = Annotated[None, Depends(require_csrf)]

"""인증 의존성. 사용자·장치는 항상 서버가 자격증명으로 판정한다."""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from typing import Annotated

from fastapi import Cookie, Depends, Header, HTTPException, status
from sqlalchemy import Connection, func, or_, select

from agent_office.db import schema
from agent_office.db.engine import get_connection
from agent_office.security import DEVICE_TOKEN_PREFIX, token_hash

SESSION_COOKIE = "ao_session"
# #25에서 CSRF 토큰을 도입하기 전까지 쿠키 인증 쓰기 요청에 요구하는 헤더.
# 교차 사이트 폼·단순 요청은 사용자 정의 헤더를 보낼 수 없고, 서버는 CORS를 열지 않는다.
CSRF_HEADER = "X-Agent-Office-Request"

# 커밋이 끝난 뒤에 응답을 보낸다. 기본 scope("request")는 응답 후 커밋해서,
# 커밋이 실패해도 클라이언트가 이미 토큰을 받는 일이 생길 수 있다.
DbConnection = Annotated[Connection, Depends(get_connection, scope="function")]


@dataclass(frozen=True)
class CurrentUser:
    id: uuid.UUID
    instance_role: str


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
    """브라우저 세션 검증만 한다. 세션 발급·로그아웃은 #25 범위다."""
    if not session_token or len(session_token) > 256:
        raise _unauthorized()
    sessions, users = schema.login_sessions, schema.users
    row = connection.execute(
        select(users.c.id, users.c.instance_role)
        .select_from(sessions.join(users, users.c.id == sessions.c.user_id))
        .where(
            sessions.c.token_hash == token_hash(session_token),
            sessions.c.revoked_at.is_(None),
            sessions.c.expires_at > func.now(),
            users.c.status == "active",
        )
    ).one_or_none()
    if row is None:
        raise _unauthorized()
    return CurrentUser(id=row.id, instance_role=row.instance_role)


def require_csrf_header(
    marker: Annotated[str | None, Header(alias=CSRF_HEADER)] = None,
) -> None:
    if marker != "1":
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
CsrfDep = Annotated[None, Depends(require_csrf_header)]

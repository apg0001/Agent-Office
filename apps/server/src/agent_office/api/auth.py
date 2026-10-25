"""브라우저 세션 조회·로그아웃·세션 관리(#25).

세션 발급은 로그인 수단(#23 이메일·비밀번호, #24 OAuth)이 `issue_session`으로 한다.
"""

from __future__ import annotations

import datetime as dt
import uuid
from typing import Annotated

from fastapi import APIRouter, HTTPException, Path, Response, status
from pydantic import BaseModel, ConfigDict
from sqlalchemy import select

from agent_office import audit
from agent_office.api.deps import CsrfDep, DbConnection, UserDep
from agent_office.auth import sessions as session_service
from agent_office.db import schema

router = APIRouter(prefix="/api/v1/auth", tags=["auth"])


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class SessionUser(StrictModel):
    id: uuid.UUID
    display_name: str
    instance_role: str


class CurrentSessionResponse(StrictModel):
    user: SessionUser
    csrf_token: str
    expires_at: dt.datetime


class SessionSummary(StrictModel):
    id: uuid.UUID
    created_at: dt.datetime
    last_seen_at: dt.datetime
    expires_at: dt.datetime
    current: bool


@router.get("/session", response_model=CurrentSessionResponse)
def current_session(user: UserDep, connection: DbConnection) -> CurrentSessionResponse:
    """웹이 시작할 때 사용자와 CSRF 토큰을 받는다. CSRF 토큰은 쓰기 요청 헤더로 돌려보낸다."""
    users, sessions = schema.users, schema.login_sessions
    row = connection.execute(
        select(users.c.display_name, sessions.c.expires_at)
        .select_from(users.join(sessions, sessions.c.user_id == users.c.id))
        .where(sessions.c.id == user.session_id)
    ).one()
    return CurrentSessionResponse(
        user=SessionUser(
            id=user.id, display_name=row.display_name, instance_role=user.instance_role
        ),
        csrf_token=user.csrf_token,
        expires_at=row.expires_at,
    )


@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT)
def logout(user: UserDep, _csrf: CsrfDep, connection: DbConnection) -> Response:
    session_service.revoke_session(connection, user.session_id)
    audit.record(connection, "auth.logout", "success", actor_user_id=user.id)
    response = Response(status_code=status.HTTP_204_NO_CONTENT)
    session_service.clear_session_cookie(response)
    return response


@router.post("/logout-all", status_code=status.HTTP_204_NO_CONTENT)
def logout_all(user: UserDep, _csrf: CsrfDep, connection: DbConnection) -> Response:
    """분실·탈취가 의심될 때 모든 브라우저 세션을 끝낸다. 장치 토큰은 건드리지 않는다."""
    session_service.revoke_all(connection, user.id)
    audit.record(connection, "auth.logout_all", "success", actor_user_id=user.id)
    response = Response(status_code=status.HTTP_204_NO_CONTENT)
    session_service.clear_session_cookie(response)
    return response


@router.get("/sessions", response_model=list[SessionSummary])
def list_sessions(user: UserDep, connection: DbConnection) -> list[SessionSummary]:
    sessions = schema.login_sessions
    rows = connection.execute(
        select(
            sessions.c.id,
            sessions.c.created_at,
            sessions.c.last_seen_at,
            sessions.c.expires_at,
        )
        .where(sessions.c.user_id == user.id, *session_service.active_session_filter())
        .order_by(sessions.c.last_seen_at.desc())
    ).all()
    return [SessionSummary(**row._asdict(), current=row.id == user.session_id) for row in rows]


@router.post("/sessions/{session_id}/revoke", status_code=status.HTTP_204_NO_CONTENT)
def revoke_session(
    session_id: Annotated[uuid.UUID, Path()],
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Response:
    sessions = schema.login_sessions
    owned = connection.execute(
        select(sessions.c.id).where(sessions.c.id == session_id, sessions.c.user_id == user.id)
    ).one_or_none()
    if owned is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
    session_service.revoke_session(connection, session_id)
    audit.record(
        connection,
        "auth.session.revoked",
        "success",
        actor_user_id=user.id,
        target_type="login_session",
        target_id=session_id,
    )
    response = Response(status_code=status.HTTP_204_NO_CONTENT)
    if session_id == user.session_id:
        session_service.clear_session_cookie(response)
    return response

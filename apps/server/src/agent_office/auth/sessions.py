"""브라우저 로그인 세션 발급·검증·폐기(#25).

세션 토큰 원문은 쿠키로만 전달하고 DB에는 SHA-256 해시만 저장한다. CSRF 토큰은 세션
토큰에서 유도한다. 세션 쿠키는 HttpOnly라 스크립트·다른 사이트가 읽을 수 없으므로, 유도 값은
세션을 가진 쪽만 알 수 있다.
"""

from __future__ import annotations

import base64
import datetime as dt
import hashlib
import os
import secrets
import uuid

from fastapi import Response
from sqlalchemy import ColumnElement, Connection, func, select, update

from agent_office import audit
from agent_office.db import schema
from agent_office.security import token_hash

SESSION_COOKIE = "ao_session"
CSRF_HEADER = "X-CSRF-Token"
SESSION_ABSOLUTE_LIFETIME = dt.timedelta(days=30)
SESSION_IDLE_TIMEOUT = dt.timedelta(days=7)
# 매 요청마다 쓰지 않도록 마지막 사용 시각은 이 간격 이상 지났을 때만 갱신한다.
SESSION_TOUCH_INTERVAL = dt.timedelta(minutes=5)
MAX_ACTIVE_SESSIONS = 20
COOKIE_SECURE_ENV = "AGENT_OFFICE_COOKIE_SECURE"


def cookie_secure() -> bool:
    # 기본은 Secure. 로컬 HTTP 개발에서만 명시적으로 끈다.
    return os.environ.get(COOKIE_SECURE_ENV, "true").lower() != "false"


def csrf_token_for(session_token: str) -> str:
    digest = hashlib.sha256(b"agent-office-csrf:" + session_token.encode()).digest()
    return base64.urlsafe_b64encode(digest).decode().rstrip("=")


def set_session_cookie(response: Response, session_token: str) -> None:
    response.set_cookie(
        SESSION_COOKIE,
        session_token,
        max_age=int(SESSION_ABSOLUTE_LIFETIME.total_seconds()),
        path="/",
        secure=cookie_secure(),
        httponly=True,
        samesite="lax",
    )


def clear_session_cookie(response: Response) -> None:
    response.delete_cookie(
        SESSION_COOKIE, path="/", secure=cookie_secure(), httponly=True, samesite="lax"
    )


def issue_session(
    connection: Connection,
    user_id: uuid.UUID,
    *,
    replaces_token: str | None = None,
) -> str:
    """로그인 성공 시(#23·#24) 새 세션을 만든다. 이전 쿠키 세션은 폐기한다(고정 공격 방지)."""
    sessions = schema.login_sessions
    if replaces_token:
        connection.execute(
            update(sessions)
            .where(
                sessions.c.token_hash == token_hash(replaces_token), sessions.c.revoked_at.is_(None)
            )
            .values(revoked_at=func.now())
        )
    token = secrets.token_urlsafe(32)
    connection.execute(
        sessions.insert().values(
            user_id=user_id,
            token_hash=token_hash(token),
            expires_at=func.now() + SESSION_ABSOLUTE_LIFETIME,
        )
    )
    # 활성 세션이 한도를 넘으면 가장 오래 쓰지 않은 것부터 폐기한다.
    overflow = (
        select(sessions.c.id)
        .where(sessions.c.user_id == user_id, sessions.c.revoked_at.is_(None))
        .order_by(sessions.c.last_seen_at.desc(), sessions.c.created_at.desc())
        .offset(MAX_ACTIVE_SESSIONS)
    )
    connection.execute(
        update(sessions).where(sessions.c.id.in_(overflow)).values(revoked_at=func.now())
    )
    audit.record(connection, "auth.session.created", "success", actor_user_id=user_id)
    return token


def active_session_filter() -> list[ColumnElement[bool]]:
    sessions = schema.login_sessions
    return [
        sessions.c.revoked_at.is_(None),
        sessions.c.expires_at > func.now(),
        sessions.c.last_seen_at > func.now() - SESSION_IDLE_TIMEOUT,
    ]


def touch(connection: Connection, session_id: uuid.UUID) -> None:
    sessions = schema.login_sessions
    connection.execute(
        update(sessions)
        .where(
            sessions.c.id == session_id,
            sessions.c.last_seen_at < func.now() - SESSION_TOUCH_INTERVAL,
        )
        .values(last_seen_at=func.now())
    )


def revoke_session(connection: Connection, session_id: uuid.UUID) -> None:
    sessions = schema.login_sessions
    connection.execute(
        update(sessions)
        .where(sessions.c.id == session_id, sessions.c.revoked_at.is_(None))
        .values(revoked_at=func.now())
    )


def revoke_all(connection: Connection, user_id: uuid.UUID) -> int:
    sessions = schema.login_sessions
    result = connection.execute(
        update(sessions)
        .where(sessions.c.user_id == user_id, sessions.c.revoked_at.is_(None))
        .values(revoked_at=func.now())
    )
    return int(result.rowcount)

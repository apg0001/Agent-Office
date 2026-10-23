"""보존 기간 정리(#39).

- 활동 이벤트: 서버 수신 시각 기준 30일
- 종료 세션(에이전트·현재 상태·계획 단계 포함): 종료 후 30일. 열린 세션과 그 현재 상태는 남긴다.
- 만료·사용·폐기된 토큰과 초대, 오래된 heartbeat 정리
- 감사 로그: 기본 365일(`AGENT_OFFICE_AUDIT_RETENTION_DAYS`)
- 삭제 기록(`deleted_users`)에 있는 계정이 백업 복원으로 되살아났으면 다시 지운다

서버가 한 시간마다 실행하고(여러 프로세스여도 advisory lock으로 하나만), 운영자는
`agent-office-retention`으로 즉시 실행할 수 있다.
"""

from __future__ import annotations

import asyncio
import datetime as dt
import logging
import os
import sys
import uuid
from typing import Any

from sqlalchemy import Connection, and_, delete, func, or_, select, text, update
from sqlalchemy.exc import DBAPIError
from starlette.concurrency import run_in_threadpool

from agent_office.db import schema

logger = logging.getLogger(__name__)

EVENT_RETENTION = dt.timedelta(days=30)
ENDED_SESSION_RETENTION = dt.timedelta(days=30)
HEARTBEAT_RETENTION = dt.timedelta(days=30)
TOKEN_GRACE = dt.timedelta(days=1)
INVITATION_RETENTION = dt.timedelta(days=30)
LOGIN_SESSION_GRACE = dt.timedelta(days=7)
AUDIT_RETENTION_ENV = "AGENT_OFFICE_AUDIT_RETENTION_DAYS"
DEFAULT_AUDIT_RETENTION_DAYS = 365
RUN_INTERVAL_SECONDS = 3600
_LOCK_KEY = "agent_office.retention"


def audit_retention() -> dt.timedelta:
    try:
        days = int(os.environ.get(AUDIT_RETENTION_ENV, DEFAULT_AUDIT_RETENTION_DAYS))
    except ValueError:
        days = DEFAULT_AUDIT_RETENTION_DAYS
    # 너무 짧게 잡아 보안 조사 근거가 사라지지 않도록 최소 30일.
    return dt.timedelta(days=max(days, 30))


def _count(connection: Connection, statement: Any) -> int:
    return int(connection.execute(statement).rowcount or 0)


def release_desks(connection: Connection, sessions: Any) -> None:
    """지울 세션의 책상을 먼저 비운다. FK의 SET NULL은 session_id만 비워 배정 쌍 검사에 걸린다."""
    slots = schema.office_slots
    connection.execute(
        update(slots)
        .where(slots.c.session_id.in_(sessions))
        .values(session_id=None, assigned_at=None)
    )


def remove_user(connection: Connection, user_id: uuid.UUID) -> None:
    """계정과 그 사람만 있는 팀을 지운다. 소속·장치·세션·이벤트·프로젝트는 FK로 함께 지워진다."""
    members, teams = schema.team_members, schema.teams
    owned = select(members.c.team_id).where(members.c.user_id == user_id, members.c.role == "OWNER")
    connection.execute(delete(teams).where(teams.c.id.in_(owned)))
    sessions, devices = schema.agent_sessions, schema.devices
    release_desks(
        connection,
        select(sessions.c.id)
        .join(devices, devices.c.id == sessions.c.device_id)
        .where(devices.c.owner_user_id == user_id),
    )
    connection.execute(delete(schema.users).where(schema.users.c.id == user_id))


def reapply_deletions(connection: Connection) -> int:
    """백업 복원으로 되살아난 삭제 계정을 다시 지운다. 실패한 계정은 다음 주기에 다시 시도한다."""
    users, deleted = schema.users, schema.deleted_users
    removed = 0
    for user_id in connection.execute(
        select(users.c.id).where(users.c.id.in_(select(deleted.c.user_id)))
    ).scalars():
        try:
            with connection.begin_nested():
                remove_user(connection, uuid.UUID(str(user_id)))
                # 팀 OWNER 검사는 지연 제약이라 저장점 안에서 바로 확인한다.
                connection.execute(text("SET CONSTRAINTS ALL IMMEDIATE"))
                connection.execute(text("SET CONSTRAINTS ALL DEFERRED"))
            removed += 1
        except DBAPIError:
            continue
    return removed


def _purge_ended_sessions(connection: Connection, now: dt.datetime) -> int:
    sessions = schema.agent_sessions
    expired = (sessions.c.lifecycle == "ENDED") & (
        sessions.c.ended_at < now - ENDED_SESSION_RETENTION
    )
    release_desks(connection, select(sessions.c.id).where(expired))
    return _count(connection, delete(sessions).where(expired))


def purge(connection: Connection) -> dict[str, int] | None:
    """한 번 정리하고 표 이름별 삭제 수를 돌려준다. 다른 실행이 진행 중이면 None."""
    locked = connection.execute(
        select(func.pg_try_advisory_xact_lock(func.hashtext(_LOCK_KEY)))
    ).scalar_one()
    if not locked:
        return None
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    s = schema
    counts = {
        "deleted_users_reapplied": reapply_deletions(connection),
        "activity_events": _count(
            connection,
            delete(s.activity_events).where(
                s.activity_events.c.received_at < now - EVENT_RETENTION
            ),
        ),
        "ended_sessions": _purge_ended_sessions(connection, now),
        "device_heartbeats": _count(
            connection,
            delete(s.device_heartbeats).where(
                s.device_heartbeats.c.received_at < now - HEARTBEAT_RETENTION
            ),
        ),
        "login_sessions": _count(
            connection,
            delete(s.login_sessions).where(
                or_(
                    s.login_sessions.c.expires_at < now - LOGIN_SESSION_GRACE,
                    s.login_sessions.c.revoked_at < now - LOGIN_SESSION_GRACE,
                )
            ),
        ),
        "audit_logs": _count(
            connection,
            delete(s.audit_logs).where(s.audit_logs.c.occurred_at < now - audit_retention()),
        ),
    }
    for name, table in (
        ("email_verifications", s.email_verifications),
        ("password_resets", s.password_resets),
        ("device_registration_codes", s.device_registration_codes),
    ):
        counts[name] = _count(
            connection,
            delete(table).where(
                or_(
                    table.c.expires_at < now - TOKEN_GRACE,
                    table.c.consumed_at < now - TOKEN_GRACE,
                )
            ),
        )
    invitations = s.team_invitations
    counts["team_invitations"] = _count(
        connection,
        delete(invitations).where(
            or_(
                invitations.c.expires_at < now - INVITATION_RETENTION,
                and_(
                    invitations.c.accepted_at.is_not(None),
                    invitations.c.accepted_at < now - INVITATION_RETENTION,
                ),
                and_(
                    invitations.c.revoked_at.is_not(None),
                    invitations.c.revoked_at < now - INVITATION_RETENTION,
                ),
            )
        ),
    )
    return counts


FIRST_RUN_DELAY_SECONDS = 300


def _run_once() -> dict[str, int] | None:
    from agent_office.db.engine import get_engine

    with get_engine().begin() as connection:
        return purge(connection)


async def run_periodically() -> None:
    """서버 수명 동안 한 시간마다 정리한다. 실패는 로그만 남기고 다음 주기에 다시 한다."""
    await asyncio.sleep(FIRST_RUN_DELAY_SECONDS)
    while True:
        try:
            counts = await run_in_threadpool(_run_once)
            if counts:
                logger.info("retention purge: %s", {k: v for k, v in counts.items() if v})
        except Exception as error:  # noqa: BLE001 - 정리 실패가 서버를 멈추면 안 된다.
            logger.warning("retention purge failed: %s", type(error).__name__)
        await asyncio.sleep(RUN_INTERVAL_SECONDS)


def main() -> int:
    """`agent-office-retention`: 지금 한 번 정리하고 표별 삭제 수만 출력한다."""
    counts = _run_once()
    if counts is None:
        print("다른 정리 작업이 실행 중입니다.")
        return 1
    for name, count in sorted(counts.items()):
        print(f"{name}: {count}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

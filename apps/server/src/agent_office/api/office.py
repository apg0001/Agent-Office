"""개인 사무실 스냅샷과 실시간 증분(#37).

스냅샷과 증분에는 서버 ID, enum, 시각, 사용자가 붙인 장치 이름만 담는다. 원문은 없다.
팀 범위(공유 프로젝트)는 #54·#55에서 같은 권한 재검사 구조로 추가한다.
"""

from __future__ import annotations

import asyncio
import datetime as dt
import os
import uuid
from typing import Any
from urllib.parse import urlsplit

from fastapi import APIRouter, WebSocket, WebSocketDisconnect
from sqlalchemy import Connection, Row, func, or_, select, true
from starlette.concurrency import run_in_threadpool

from agent_office.api.deps import DbConnection, UserDep, authenticate_session
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.state.engine import connection_state, effective_lifecycle

# 종료된 세션은 퇴근 연출을 위해 이 시간 동안 스냅샷에 남긴다.
ENDED_SESSION_GRACE = dt.timedelta(minutes=10)
POLL_INTERVAL_SECONDS = 1.0
# 늦게 커밋된 트랜잭션도 놓치지 않도록 매 조회마다 이 기간을 다시 본다(중복은 change_seq로 거름).
CHANGE_LOOKBACK = dt.timedelta(seconds=30)
PING_INTERVAL_SECONDS = 25.0
ALLOWED_ORIGINS_ENV = "AGENT_OFFICE_ALLOWED_ORIGINS"
CLOSE_UNAUTHORIZED = 4401
CLOSE_FORBIDDEN_ORIGIN = 4403

router = APIRouter(tags=["office"])


def _iso(value: dt.datetime | None) -> str | None:
    return value.isoformat() if value is not None else None


def _devices(connection: Connection, user_id: uuid.UUID) -> list[Any]:
    devices = schema.devices
    return list(
        connection.execute(
            select(devices.c.id, devices.c.name, devices.c.last_seen_at).where(
                devices.c.owner_user_id == user_id, devices.c.revoked_at.is_(None)
            )
        ).all()
    )


def _session_rows(connection: Connection, device_ids: list[uuid.UUID], extra: Any) -> list[Any]:
    sessions = schema.agent_sessions
    if not device_ids:
        return []
    return list(
        connection.execute(
            select(
                sessions.c.id,
                sessions.c.device_id,
                sessions.c.provider,
                sessions.c.lifecycle,
                sessions.c.started_at,
                sessions.c.ended_at,
                sessions.c.last_event_at,
                sessions.c.change_seq,
            ).where(sessions.c.device_id.in_(device_ids), extra)
        ).all()
    )


def _agent_rows(connection: Connection, session_ids: list[uuid.UUID], extra: Any) -> list[Any]:
    agents, states = schema.agents, schema.agent_current_states
    if not session_ids:
        return []
    return list(
        connection.execute(
            select(
                agents.c.id,
                agents.c.session_id,
                agents.c.parent_agent_id,
                agents.c.role,
                agents.c.started_at,
                agents.c.ended_at,
                states.c.turn_state,
                states.c.activity,
                states.c.state_version,
                states.c.change_seq,
            )
            .select_from(agents.join(states, states.c.agent_id == agents.c.id))
            .where(agents.c.session_id.in_(session_ids), extra)
        ).all()
    )


def _device_json(row: Row[Any], now: dt.datetime) -> dict[str, Any]:
    return {
        "id": str(row.id),
        "name": row.name,
        "connection": connection_state(row.last_seen_at, now),
    }


def _session_json(
    row: Row[Any], connection_of: dict[uuid.UUID, str], now: dt.datetime
) -> dict[str, Any]:
    return {
        "id": str(row.id),
        "device_id": str(row.device_id),
        "provider": row.provider,
        "lifecycle": effective_lifecycle(
            row.lifecycle, row.last_event_at, connection_of.get(row.device_id, "DISCONNECTED"), now
        ),
        "started_at": _iso(row.started_at),
        "ended_at": _iso(row.ended_at),
        "last_event_at": _iso(row.last_event_at),
        "change_seq": row.change_seq,
    }


def _agent_json(row: Row[Any]) -> dict[str, Any]:
    return {
        "id": str(row.id),
        "session_id": str(row.session_id),
        "parent_agent_id": str(row.parent_agent_id) if row.parent_agent_id else None,
        "role": row.role,
        "turn_state": row.turn_state,
        "activity": row.activity,
        "state_version": row.state_version,
        "started_at": _iso(row.started_at),
        "ended_at": _iso(row.ended_at),
        "change_seq": row.change_seq,
    }


def build_snapshot(connection: Connection, user_id: uuid.UUID) -> dict[str, Any]:
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    devices = _devices(connection, user_id)
    connection_of = {d.id: connection_state(d.last_seen_at, now) for d in devices}
    sessions = schema.agent_sessions
    visible = or_(sessions.c.lifecycle != "ENDED", sessions.c.ended_at > now - ENDED_SESSION_GRACE)
    session_rows = _session_rows(connection, [d.id for d in devices], visible)
    agent_rows = _agent_rows(connection, [s.id for s in session_rows], true())
    return {
        "type": "snapshot",
        "server_time": now.isoformat(),
        "devices": [_device_json(d, now) for d in devices],
        "sessions": [_session_json(s, connection_of, now) for s in session_rows],
        "agents": [_agent_json(a) for a in agent_rows],
    }


def load_changes(connection: Connection, user_id: uuid.UUID) -> dict[str, Any]:
    """최근 변경 후보. 호출자가 change_seq로 이미 보낸 행을 거른다."""
    now: dt.datetime = connection.execute(select(func.clock_timestamp())).scalar_one()
    since = now - CHANGE_LOOKBACK
    devices = _devices(connection, user_id)
    connection_of = {d.id: connection_state(d.last_seen_at, now) for d in devices}
    device_ids = [d.id for d in devices]
    sessions = schema.agent_sessions
    changed_sessions = _session_rows(connection, device_ids, sessions.c.changed_at >= since)
    session_ids = (
        list(
            connection.execute(
                select(sessions.c.id).where(sessions.c.device_id.in_(device_ids))
            ).scalars()
        )
        if device_ids
        else []
    )
    changed_agents = _agent_rows(
        connection, session_ids, schema.agent_current_states.c.updated_at >= since
    )
    return {
        "devices": [_device_json(d, now) for d in devices],
        "sessions": [_session_json(s, connection_of, now) for s in changed_sessions],
        "agents": [_agent_json(a) for a in changed_agents],
    }


@router.get("/api/v1/office")
def office_snapshot(user: UserDep, connection: DbConnection) -> dict[str, Any]:
    """브라우저 첫 접속·새로고침용 개인 사무실 스냅샷."""
    return build_snapshot(connection, user.id)


def _origin_allowed(websocket: WebSocket) -> bool:
    """교차 사이트 WebSocket 하이재킹 방지. 브라우저가 보낸 Origin은 같은 호스트여야 한다."""
    origin = websocket.headers.get("origin")
    if origin is None:
        # Origin이 없는 비브라우저 클라이언트는 피해자 쿠키를 쓸 수 없다.
        return True
    allowed = {o.strip() for o in os.environ.get(ALLOWED_ORIGINS_ENV, "").split(",") if o.strip()}
    if origin in allowed:
        return True
    return urlsplit(origin).netloc == websocket.headers.get("host")


def _with_connection(function: Any, *args: Any) -> Any:
    with get_engine().begin() as connection:
        return function(connection, *args)


@router.websocket("/api/v1/live")
async def live(websocket: WebSocket) -> None:
    if not _origin_allowed(websocket):
        await websocket.close(code=CLOSE_FORBIDDEN_ORIGIN)
        return
    token = websocket.cookies.get(SESSION_COOKIE)
    user = await run_in_threadpool(_with_connection, authenticate_session, token)
    if user is None:
        await websocket.close(code=CLOSE_UNAUTHORIZED)
        return
    await websocket.accept()

    snapshot = await run_in_threadpool(_with_connection, build_snapshot, user.id)
    await websocket.send_json(snapshot)
    sent: dict[tuple[str, str], int] = {}
    for kind in ("sessions", "agents"):
        for row in snapshot[kind]:
            sent[(kind, row["id"])] = row["change_seq"]
    connections = {d["id"]: d["connection"] for d in snapshot["devices"]}

    receiver = asyncio.create_task(websocket.receive())
    loop = asyncio.get_running_loop()
    last_ping = loop.time()
    try:
        while True:
            done, _ = await asyncio.wait({receiver}, timeout=POLL_INTERVAL_SECONDS)
            if receiver in done:
                message = receiver.result()
                if message["type"] == "websocket.disconnect":
                    return
                # 클라이언트 메시지는 처리하지 않는다(향후 구독 범위 변경용).
                receiver = asyncio.create_task(websocket.receive())

            # 권한 철회 즉시 반영: 매 주기 세션을 다시 검증한다.
            still = await run_in_threadpool(_with_connection, authenticate_session, token)
            if still is None or still.id != user.id:
                await websocket.close(code=CLOSE_UNAUTHORIZED)
                return

            changes = await run_in_threadpool(_with_connection, load_changes, user.id)
            delta: dict[str, Any] = {"type": "delta", "devices": [], "sessions": [], "agents": []}
            for device in changes["devices"]:
                if connections.get(device["id"]) != device["connection"]:
                    connections[device["id"]] = device["connection"]
                    delta["devices"].append(device)
            for kind in ("sessions", "agents"):
                for row in changes[kind]:
                    key = (kind, row["id"])
                    if row["change_seq"] > sent.get(key, -1):
                        sent[key] = row["change_seq"]
                        delta[kind].append(row)
            if delta["devices"] or delta["sessions"] or delta["agents"]:
                await websocket.send_json(delta)
            elif loop.time() - last_ping >= PING_INTERVAL_SECONDS:
                await websocket.send_json({"type": "ping"})
                last_ping = loop.time()
    except WebSocketDisconnect:
        return
    finally:
        receiver.cancel()

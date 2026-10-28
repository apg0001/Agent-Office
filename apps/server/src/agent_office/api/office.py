"""개인 사무실 스냅샷과 실시간 증분(#37).

스냅샷과 증분에는 서버 ID, enum, 시각, 사용자가 붙인 장치 이름만 담는다. 원문은 없다.
팀 범위는 #54·#55의 재검사 구조에 팀원별 공유 범위(#197)를 더해 거른다.
"""

from __future__ import annotations

import asyncio
import base64
import datetime as dt
import uuid
from typing import Annotated, Any

from fastapi import APIRouter, HTTPException, Query, Response, WebSocket, WebSocketDisconnect
from pydantic import BaseModel, ConfigDict
from sqlalchemy import Connection, Row, func, or_, select, true, union, update
from sqlalchemy import null as sql_null
from starlette.concurrency import run_in_threadpool

from agent_office.api.deps import CsrfDep, DbConnection, UserDep, authenticate_session
from agent_office.auth.sessions import SESSION_COOKIE
from agent_office.db import schema
from agent_office.db.engine import get_engine
from agent_office.origin import origin_allowed
from agent_office.state.engine import connection_state, effective_lifecycle

# 종료된 세션은 퇴근 연출을 위해 이 시간 동안 스냅샷에 남긴다.
ENDED_SESSION_GRACE = dt.timedelta(minutes=10)
# 표시 지연 P95 2초 목표(#63 실측: 30세션 90에이전트에서 수집 약 1초 + 이 주기).
POLL_INTERVAL_SECONDS = 0.5
# 늦게 커밋된 트랜잭션도 놓치지 않도록 매 조회마다 이 기간을 다시 본다(중복은 change_seq로 거름).
CHANGE_LOOKBACK = dt.timedelta(seconds=30)
PING_INTERVAL_SECONDS = 25.0
# 상세 패널 타임라인에 보여 줄 최근 이벤트 수(#51).
TIMELINE_LIMIT = 50
LIVE_PATH = "/api/v1/live"
CLOSE_UNAUTHORIZED = 4401
CLOSE_FORBIDDEN_ORIGIN = 4403
# 팀 구성원이 아니거나(탈퇴·추방) 팀이 없을 때. 존재 여부를 구분하지 않는다.
CLOSE_SCOPE_UNAVAILABLE = 4404

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
    sessions, slots, projects = schema.agent_sessions, schema.office_slots, schema.projects
    if not device_ids:
        return []
    return list(
        connection.execute(
            select(
                slots.c.slot_index.label("desk_slot"),
                sessions.c.id,
                sessions.c.device_id,
                sessions.c.provider,
                sessions.c.lifecycle,
                sessions.c.started_at,
                sessions.c.ended_at,
                sessions.c.last_event_at,
                sessions.c.change_seq,
                sessions.c.project_id,
                projects.c.display_name.label("project_name"),
                sessions.c.workspace_name,
            )
            .select_from(
                sessions.outerjoin(slots, slots.c.session_id == sessions.c.id).outerjoin(
                    projects, projects.c.id == sessions.c.project_id
                )
            )
            .where(sessions.c.device_id.in_(device_ids), extra)
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
                states.c.confidence,
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
    row: Row[Any],
    connection_of: dict[uuid.UUID, str],
    now: dt.datetime,
    *,
    show_workspace: bool = True,
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
        # 개인 사무실 책상 번호(#48). 화면이 번호로 자리 좌표를 정한다.
        "desk_slot": row.desk_slot,
        # 사용자가 지정한 프로젝트(서버 ID). 팀 공유 범위를 정한다(#54).
        "project_id": str(row.project_id) if row.project_id else None,
        # 프로젝트 표시 이름(#197). 보이는 세션의 프로젝트만 담기므로 그 범위에 보이는 프로젝트다.
        "project_name": row.project_name,
        # 작업 폴더의 마지막 이름(#181). 장치 소유자에게만 보인다(팀 화면은 null).
        "workspace_name": row.workspace_name if show_workspace else None,
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
        "confidence": row.confidence,
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


def build_agent_detail(
    connection: Connection, user_id: uuid.UUID, agent_id: uuid.UUID
) -> dict[str, Any] | None:
    """에이전트 상세(#51). 소유하지 않은 에이전트는 존재 여부도 드러내지 않도록 None."""
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    agents, events, steps = schema.agents, schema.activity_events, schema.plan_steps
    session_id = connection.execute(
        select(agents.c.session_id).where(agents.c.id == agent_id)
    ).scalar_one_or_none()
    if session_id is None:
        return None
    devices = _devices(connection, user_id)
    session_rows = _session_rows(
        connection, [d.id for d in devices], schema.agent_sessions.c.id == session_id
    )
    if not session_rows:
        return None
    session_row = session_rows[0]
    device = next(d for d in devices if d.id == session_row.device_id)
    connection_of = {device.id: connection_state(device.last_seen_at, now)}
    members = _agent_rows(connection, [session_id], true())
    agent = next(a for a in members if a.id == agent_id)
    timeline = connection.execute(
        select(
            events.c.event_type,
            events.c.activity,
            events.c.confidence,
            events.c.source,
            events.c.occurred_at,
        )
        .where(events.c.session_id == session_id, events.c.agent_id == agent_id)
        .order_by(events.c.occurred_at.desc(), events.c.sequence.desc())
        .limit(TIMELINE_LIMIT)
    ).all()
    plan = connection.execute(
        select(steps.c.step_index, steps.c.status)
        .where(steps.c.session_id == session_id)
        .order_by(steps.c.step_index)
    ).all()
    return {
        "agent": _agent_json(agent),
        "session": _session_json(session_row, connection_of, now),
        "device": _device_json(device, now),
        "parent": next((_agent_json(a) for a in members if a.id == agent.parent_agent_id), None),
        "children": [_agent_json(a) for a in members if a.parent_agent_id == agent_id],
        # 이벤트 종류·활동·신뢰도·시각만. 도구 입력·명령·경로 같은 원문은 서버에 없다.
        "timeline": [
            {
                "event_type": e.event_type,
                "activity": e.activity,
                "confidence": e.confidence,
                "source": e.source,
                "occurred_at": _iso(e.occurred_at),
            }
            for e in timeline
        ],
        # 계획은 순서와 상태만 저장한다(본문 없음).
        "plan_steps": [{"step_index": p.step_index, "status": p.status} for p in plan],
    }


@router.get("/api/v1/office/agents/{agent_id}")
def agent_detail(agent_id: uuid.UUID, user: UserDep, connection: DbConnection) -> dict[str, Any]:
    """사무실 캐릭터 상세 패널용. 본인 장치의 에이전트만 조회한다."""
    detail = build_agent_detail(connection, user.id, agent_id)
    if detail is None:
        raise HTTPException(status_code=404, detail="not found")
    return detail


def _origin_allowed(websocket: WebSocket) -> bool:
    """교차 사이트 WebSocket 하이재킹 방지."""
    return origin_allowed(websocket.headers.get("origin"), websocket.headers.get("host"))


def _with_connection(function: Any, *args: Any) -> Any:
    with get_engine().begin() as connection:
        return function(connection, *args)


def team_update(previous: dict[str, Any], current: dict[str, Any]) -> dict[str, Any] | None:
    """팀 범위 변경분. 보이던 세션·에이전트가 빠지면(공유 해제·탈퇴) 클라이언트가 지우도록
    새 스냅샷을, 그렇지 않으면 바뀐 행만 담은 증분을 돌려준다. 변경이 없으면 None."""
    for kind in ("sessions", "agents", "devices"):
        if {r["id"] for r in previous[kind]} - {r["id"] for r in current[kind]}:
            return current
    delta: dict[str, Any] = {"type": "delta", "devices": [], "sessions": [], "agents": []}
    for kind in ("devices", "sessions", "agents"):
        before = {r["id"]: r for r in previous[kind]}
        for row in current[kind]:
            if before.get(row["id"]) != row:
                delta[kind].append(row)
    if delta["devices"] or delta["sessions"] or delta["agents"]:
        return delta
    return None


def _team_snapshot_if_member(
    connection: Connection, team_id: uuid.UUID, user_id: uuid.UUID
) -> dict[str, Any] | None:
    if not team_member(connection, team_id, user_id):
        return None
    return build_team_snapshot(connection, team_id)


async def _live_scoped(
    websocket: WebSocket,
    token: str | None,
    user_id: uuid.UUID,
    build: Any,
    *args: Any,
) -> None:
    """팀·통합 사무실 실시간. 매 주기 로그인 세션과 범위 자격을 다시 확인하고 스냅샷을 다시
    계산한다(Pi 규모 기준). 공유 해제·탈퇴·추방은 다음 주기(1초 이내)에 반영된다."""
    snapshot = await run_in_threadpool(_with_connection, build, *args)
    if snapshot is None:
        await websocket.close(code=CLOSE_SCOPE_UNAVAILABLE)
        return
    await websocket.accept()
    await websocket.send_json(snapshot)
    receiver = asyncio.create_task(websocket.receive())
    loop = asyncio.get_running_loop()
    last_ping = loop.time()
    try:
        while True:
            done, _ = await asyncio.wait({receiver}, timeout=POLL_INTERVAL_SECONDS)
            if receiver in done:
                if receiver.result()["type"] == "websocket.disconnect":
                    return
                receiver = asyncio.create_task(websocket.receive())
            still = await run_in_threadpool(_with_connection, authenticate_session, token)
            if still is None or still.id != user_id:
                await websocket.close(code=CLOSE_UNAUTHORIZED)
                return
            current = await run_in_threadpool(_with_connection, build, *args)
            if current is None:
                await websocket.close(code=CLOSE_SCOPE_UNAVAILABLE)
                return
            message = team_update(snapshot, current)
            snapshot = current
            if message is not None:
                await websocket.send_json(message)
            elif loop.time() - last_ping >= PING_INTERVAL_SECONDS:
                await websocket.send_json({"type": "ping"})
                last_ping = loop.time()
    except WebSocketDisconnect:
        return
    finally:
        receiver.cancel()


@router.websocket(LIVE_PATH)
async def live(websocket: WebSocket) -> None:
    if not _origin_allowed(websocket):
        await websocket.close(code=CLOSE_FORBIDDEN_ORIGIN)
        return
    token = websocket.cookies.get(SESSION_COOKIE)
    user = await run_in_threadpool(_with_connection, authenticate_session, token)
    if user is None:
        await websocket.close(code=CLOSE_UNAUTHORIZED)
        return
    team = websocket.query_params.get("team")
    if team is not None:
        try:
            team_id = uuid.UUID(team)
        except ValueError:
            await websocket.close(code=CLOSE_SCOPE_UNAVAILABLE)
            return
        await _live_scoped(websocket, token, user.id, _team_snapshot_if_member, team_id, user.id)
        return
    if websocket.query_params.get("view") == "combined":
        await _live_scoped(websocket, token, user.id, build_combined_snapshot, user.id)
        return
    await websocket.accept()

    snapshot = await run_in_threadpool(_with_connection, build_snapshot, user.id)
    await websocket.send_json(snapshot)
    sent: dict[tuple[str, str], int] = {}
    for kind in ("sessions", "agents"):
        for row in snapshot[kind]:
            sent[(kind, row["id"])] = row["change_seq"]
    # 연결 상태나 이름(#206 이름 변경)이 바뀐 장치만 증분으로 보낸다.
    connections = {d["id"]: (d["connection"], d["name"]) for d in snapshot["devices"]}

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
                seen = (device["connection"], device["name"])
                if connections.get(device["id"]) != seen:
                    connections[device["id"]] = seen
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


# --- 세션 프로젝트 지정·팀 사무실(#54, #55) -------------------------------------------


class ProjectAssignment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    project_id: uuid.UUID | None


@router.put("/api/v1/office/sessions/{session_id}/project", status_code=204)
def assign_project(
    session_id: uuid.UUID,
    body: ProjectAssignment,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Response:
    """내 세션을 내 프로젝트에 넣는다. 그 프로젝트가 팀에 공유되어 있으면 팀 사무실에 나타난다."""
    sessions, devices, projects = schema.agent_sessions, schema.devices, schema.projects
    owned = connection.execute(
        select(sessions.c.id)
        .select_from(sessions.join(devices, devices.c.id == sessions.c.device_id))
        .where(sessions.c.id == session_id, devices.c.owner_user_id == user.id)
    ).first()
    if owned is None:
        raise HTTPException(status_code=404, detail="not_found")
    if body.project_id is not None:
        mine = connection.execute(
            select(projects.c.id).where(
                projects.c.id == body.project_id, projects.c.owner_user_id == user.id
            )
        ).first()
        if mine is None:
            raise HTTPException(status_code=404, detail="not_found")
    connection.execute(
        update(sessions)
        .where(sessions.c.id == session_id)
        .values(
            project_id=body.project_id,
            # 직접 정한 배정(없음 포함)은 폴더 이름 자동 배정보다 우선한다(#197).
            project_manual=True,
            change_seq=func.nextval("state_change_seq"),
            changed_at=func.clock_timestamp(),
        )
    )
    return Response(status_code=204)


def team_member(connection: Connection, team_id: uuid.UUID, user_id: uuid.UUID) -> bool:
    members = schema.team_members
    return (
        connection.execute(
            select(members.c.user_id).where(
                members.c.team_id == team_id, members.c.user_id == user_id
            )
        ).first()
        is not None
    )


def _team_session_ids(connection: Connection, team_id: uuid.UUID, now: dt.datetime) -> Any:
    """팀에 보이는 세션(#54, #197). 세션이 지금도 팀 구성원인 사람의 (폐기되지 않은) 장치에서 왔고,
    그 구성원의 공유 범위가

    - 'all'이면 모든 세션(프로젝트가 없어도),
    - 'selected'면 본인 프로젝트이면서 이 팀에 공유한 프로젝트의 세션만,
    - 'none'이면 아무것도 보이지 않는다.
    """
    sessions, projects, shares = schema.agent_sessions, schema.projects, schema.project_shares
    members, devices = schema.team_members, schema.devices
    shared_project = (
        select(shares.c.project_id)
        .select_from(shares.join(projects, projects.c.id == shares.c.project_id))
        .where(
            shares.c.team_id == team_id,
            shares.c.project_id == sessions.c.project_id,
            projects.c.owner_user_id == devices.c.owner_user_id,
        )
        .exists()
    )
    return (
        select(sessions.c.id)
        .select_from(
            sessions.join(
                devices,
                (devices.c.id == sessions.c.device_id) & devices.c.revoked_at.is_(None),
            ).join(
                members,
                (members.c.team_id == team_id) & (members.c.user_id == devices.c.owner_user_id),
            )
        )
        .where(
            or_(sessions.c.lifecycle != "ENDED", sessions.c.ended_at > now - ENDED_SESSION_GRACE),
            or_(
                members.c.share_mode == "all",
                (members.c.share_mode == "selected") & shared_project,
            ),
        )
    )


def _scoped_snapshot(
    connection: Connection,
    now: dt.datetime,
    visible: Any,
    scope: dict[str, str],
    viewer_id: uuid.UUID | None,
) -> dict[str, Any]:
    """보이는 세션 집합으로 만든 스냅샷. 남의 장치는 PC 이름 대신 소유자 표시 이름을 쓰고,
    책상은 화면용 순번(시작 순서)이다. 세션은 한 번만 나온다(여러 팀에 공유돼도)."""
    sessions, devices, users = schema.agent_sessions, schema.devices, schema.users
    projects = schema.projects
    session_rows = list(
        connection.execute(
            select(
                sql_null().label("desk_slot"),
                sessions.c.id,
                sessions.c.device_id,
                sessions.c.provider,
                sessions.c.lifecycle,
                sessions.c.started_at,
                sessions.c.ended_at,
                sessions.c.last_event_at,
                sessions.c.change_seq,
                sessions.c.project_id,
                projects.c.display_name.label("project_name"),
                sessions.c.workspace_name,
            )
            .select_from(sessions.outerjoin(projects, projects.c.id == sessions.c.project_id))
            .where(sessions.c.id.in_(visible))
            .order_by(sessions.c.started_at, sessions.c.id)
        ).all()
    )
    device_ids = sorted({s.device_id for s in session_rows})
    device_rows: list[Any] = (
        list(
            connection.execute(
                select(
                    devices.c.id,
                    devices.c.name.label("device_name"),
                    devices.c.owner_user_id,
                    users.c.display_name,
                    devices.c.last_seen_at,
                )
                .select_from(devices.join(users, users.c.id == devices.c.owner_user_id))
                .where(devices.c.id.in_(device_ids))
            ).all()
        )
        if device_ids
        else []
    )
    connection_of = {d.id: connection_state(d.last_seen_at, now) for d in device_rows}
    owner_of = {d.id: d.owner_user_id for d in device_rows}
    agent_rows = _agent_rows(connection, [s.id for s in session_rows], true())
    session_json = []
    desk = 0
    for row in session_rows:
        # 작업 폴더 이름(#181)은 장치 소유자 본인에게만 보인다(팀 화면은 viewer_id가 없음).
        item = _session_json(
            row,
            connection_of,
            now,
            show_workspace=viewer_id is not None and owner_of.get(row.device_id) == viewer_id,
        )
        if item["lifecycle"] != "ENDED":
            item["desk_slot"] = desk
            desk += 1
        session_json.append(item)
    return {
        "type": "snapshot",
        "scope": scope,
        "server_time": now.isoformat(),
        "devices": [
            {
                "id": str(d.id),
                "name": d.device_name if d.owner_user_id == viewer_id else d.display_name,
                # 팀·통합 화면의 사람 이름표(#206). 내 장치도 내 표시 이름. 이메일은 담지 않는다.
                "owner_name": d.display_name,
                "connection": connection_state(d.last_seen_at, now),
            }
            for d in device_rows
        ],
        "sessions": session_json,
        "agents": [_agent_json(a) for a in agent_rows],
    }


def build_team_snapshot(connection: Connection, team_id: uuid.UUID) -> dict[str, Any]:
    """팀 사무실 스냅샷. 팀 화면에서는 내 장치도 표시 이름으로 보인다(팀원과 같은 화면)."""
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    return _scoped_snapshot(
        connection,
        now,
        _team_session_ids(connection, team_id, now),
        {"kind": "team", "team_id": str(team_id)},
        None,
    )


def build_combined_snapshot(connection: Connection, user_id: uuid.UUID) -> dict[str, Any]:
    """통합 큰 사무실(#53): 내 세션 + 내가 속한 모든 팀에 보이는 세션을 한 번씩."""
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    sessions, devices, members = schema.agent_sessions, schema.devices, schema.team_members
    own = (
        select(sessions.c.id)
        .select_from(sessions.join(devices, devices.c.id == sessions.c.device_id))
        .where(
            devices.c.owner_user_id == user_id,
            devices.c.revoked_at.is_(None),
            or_(sessions.c.lifecycle != "ENDED", sessions.c.ended_at > now - ENDED_SESSION_GRACE),
        )
    )
    team_ids = list(
        connection.execute(select(members.c.team_id).where(members.c.user_id == user_id)).scalars()
    )
    parts = [own, *(_team_session_ids(connection, t, now) for t in team_ids)]
    visible = union(*parts).subquery()
    return _scoped_snapshot(connection, now, select(visible.c.id), {"kind": "combined"}, user_id)


def _require_team(connection: Connection, team_id: uuid.UUID, user_id: uuid.UUID) -> None:
    if not team_member(connection, team_id, user_id):
        raise HTTPException(status_code=404, detail="not_found")


@router.get("/api/v1/office/combined")
def combined_office(user: UserDep, connection: DbConnection) -> dict[str, Any]:
    """통합 큰 사무실 스냅샷(#53). 이벤트는 한 번만 저장되고 화면 배치만 다르다."""
    return build_combined_snapshot(connection, user.id)


@router.get("/api/v1/teams/{team_id}/office")
def team_office(team_id: uuid.UUID, user: UserDep, connection: DbConnection) -> dict[str, Any]:
    """팀 사무실 스냅샷. 구성원이 아니면 팀 존재도 드러내지 않는다."""
    _require_team(connection, team_id, user.id)
    return build_team_snapshot(connection, team_id)


@router.get("/api/v1/teams/{team_id}/office/agents/{agent_id}")
def team_agent_detail(
    team_id: uuid.UUID, agent_id: uuid.UUID, user: UserDep, connection: DbConnection
) -> dict[str, Any]:
    _require_team(connection, team_id, user.id)
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    agents = schema.agents
    session_id = connection.execute(
        select(agents.c.session_id).where(
            agents.c.id == agent_id,
            agents.c.session_id.in_(_team_session_ids(connection, team_id, now)),
        )
    ).scalar_one_or_none()
    if session_id is None:
        raise HTTPException(status_code=404, detail="not_found")
    owner = connection.execute(
        select(schema.devices.c.owner_user_id)
        .select_from(
            schema.agent_sessions.join(
                schema.devices, schema.devices.c.id == schema.agent_sessions.c.device_id
            )
        )
        .where(schema.agent_sessions.c.id == session_id)
    ).scalar_one()
    detail = build_agent_detail(connection, owner, agent_id)
    if detail is None:
        raise HTTPException(status_code=404, detail="not_found")
    # 작업 폴더 이름(#181)은 팀 화면에 보이지 않는다(팀 스냅샷과 같게 소유자 본인에게도 null).
    detail["session"]["workspace_name"] = None
    # 팀원에게는 PC 이름 대신 소유자 표시 이름을 보여 준다.
    owner_name = connection.execute(
        select(schema.users.c.display_name).where(schema.users.c.id == owner)
    ).scalar_one()
    detail["device"]["name"] = owner_name
    detail["device"]["owner_name"] = owner_name
    return detail


@router.get("/api/v1/office/combined/agents/{agent_id}")
def combined_agent_detail(
    agent_id: uuid.UUID, user: UserDep, connection: DbConnection
) -> dict[str, Any]:
    """통합 사무실의 상세: 내 에이전트면 개인 상세, 아니면 그것이 보이는 팀의 상세."""
    own = build_agent_detail(connection, user.id, agent_id)
    if own is not None:
        # 통합 화면은 사람 이름을 함께 보여 준다(#206). 내 에이전트면 내 표시 이름.
        own["device"]["owner_name"] = connection.execute(
            select(schema.users.c.display_name).where(schema.users.c.id == user.id)
        ).scalar_one()
        return own
    members = schema.team_members
    for team_id in connection.execute(
        select(members.c.team_id).where(members.c.user_id == user.id)
    ).scalars():
        try:
            return team_agent_detail(team_id, agent_id, user, connection)
        except HTTPException:
            continue
    raise HTTPException(status_code=404, detail="not_found")


# --- 타임라인 페이지(#38) ---------------------------------------------------------------

MAX_TIMELINE_PAGE = 200


def _encode_cursor(occurred_at: dt.datetime, sequence: int) -> str:
    raw = f"{occurred_at.isoformat()}|{sequence}".encode()
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def _decode_cursor(cursor: str) -> tuple[dt.datetime, int]:
    try:
        raw = base64.urlsafe_b64decode(cursor + "=" * (-len(cursor) % 4)).decode()
        moment, sequence = raw.rsplit("|", 1)
        return dt.datetime.fromisoformat(moment), int(sequence)
    except (ValueError, UnicodeDecodeError) as error:
        raise HTTPException(status_code=400, detail="invalid_cursor") from error


def timeline_page(
    connection: Connection,
    session_id: uuid.UUID,
    agent_id: uuid.UUID,
    before: str | None,
    limit: int,
) -> dict[str, Any]:
    """최신순 이벤트 페이지. 종류·활동·신뢰도·출처·시각만 담는다. `next`로 이어 읽는다."""
    events = schema.activity_events
    query = select(
        events.c.event_type,
        events.c.activity,
        events.c.confidence,
        events.c.source,
        events.c.occurred_at,
        events.c.sequence,
    ).where(events.c.session_id == session_id, events.c.agent_id == agent_id)
    if before:
        moment, sequence = _decode_cursor(before)
        query = query.where(
            or_(
                events.c.occurred_at < moment,
                (events.c.occurred_at == moment) & (events.c.sequence < sequence),
            )
        )
    rows = connection.execute(
        query.order_by(events.c.occurred_at.desc(), events.c.sequence.desc()).limit(limit + 1)
    ).all()
    page = rows[:limit]
    return {
        "entries": [
            {
                "event_type": r.event_type,
                "activity": r.activity,
                "confidence": r.confidence,
                "source": r.source,
                "occurred_at": _iso(r.occurred_at),
            }
            for r in page
        ],
        "next": _encode_cursor(page[-1].occurred_at, page[-1].sequence)
        if len(rows) > limit
        else None,
    }


def _own_agent_session(
    connection: Connection, user_id: uuid.UUID, agent_id: uuid.UUID
) -> uuid.UUID:
    agents, sessions, devices = schema.agents, schema.agent_sessions, schema.devices
    session_id = connection.execute(
        select(agents.c.session_id)
        .select_from(
            agents.join(sessions, sessions.c.id == agents.c.session_id).join(
                devices, devices.c.id == sessions.c.device_id
            )
        )
        .where(agents.c.id == agent_id, devices.c.owner_user_id == user_id)
    ).scalar_one_or_none()
    if session_id is None:
        raise HTTPException(status_code=404, detail="not_found")
    return uuid.UUID(str(session_id))


TimelineLimit = Annotated[int, Query(ge=1, le=MAX_TIMELINE_PAGE)]
TimelineCursor = Annotated[str | None, Query(max_length=200)]


@router.get("/api/v1/office/agents/{agent_id}/timeline")
def agent_timeline(
    agent_id: uuid.UUID,
    user: UserDep,
    connection: DbConnection,
    before: TimelineCursor = None,
    limit: TimelineLimit = 50,
) -> dict[str, Any]:
    """내 에이전트 타임라인(#38). 30일 보존(#39) 안의 이벤트만 있다."""
    session_id = _own_agent_session(connection, user.id, agent_id)
    return timeline_page(connection, session_id, agent_id, before, limit)


@router.get("/api/v1/teams/{team_id}/office/agents/{agent_id}/timeline")
def team_agent_timeline(
    team_id: uuid.UUID,
    agent_id: uuid.UUID,
    user: UserDep,
    connection: DbConnection,
    before: TimelineCursor = None,
    limit: TimelineLimit = 50,
) -> dict[str, Any]:
    """팀에 공유된 에이전트의 타임라인. 공유가 거둬지면 바로 404."""
    _require_team(connection, team_id, user.id)
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    agents = schema.agents
    session_id = connection.execute(
        select(agents.c.session_id).where(
            agents.c.id == agent_id,
            agents.c.session_id.in_(_team_session_ids(connection, team_id, now)),
        )
    ).scalar_one_or_none()
    if session_id is None:
        raise HTTPException(status_code=404, detail="not_found")
    return timeline_page(connection, uuid.UUID(str(session_id)), agent_id, before, limit)

"""세션별 책상 배정·재사용·확장(#48).

책상은 세션 동안 유지된다. 빈 자리(배정 없음, 종료 후 퇴근 연출 시간이 지난 세션, 또는 화면에서
숨겨질 만큼 오래 조용한 세션 #291)를 번호가 낮은 순서로 재사용하고, 없을 때만 새 슬롯을 만들어
사무실을 넓힌다. 좌표는 화면이 슬롯 번호로
계산하므로 여기서는 번호와 논리 칸만 저장한다.
"""

from __future__ import annotations

import datetime as dt
import uuid

from sqlalchemy import Connection, exists, false, func, or_, select, update
from sqlalchemy.dialects.postgresql import insert

from agent_office import instance
from agent_office.db import schema

# 종료된 세션의 책상은 퇴근 연출이 끝날 때까지 비우지 않는다(마스터 사양 §7: 약 30초).
DESK_RELEASE_GRACE = dt.timedelta(seconds=30)
DESKS_PER_ROW = 7


def slot_cell(slot_index: int) -> tuple[int, int]:
    """웹 `grid.ts`의 자리 배치와 같은 규칙: 한 줄 7자리, 줄 간격 3칸."""
    row, column = divmod(slot_index, DESKS_PER_ROW)
    return 2 + column * 2, 2 + row * 3


def ensure_desk(connection: Connection, session_id: uuid.UUID) -> bool:
    """세션에 책상이 없으면 배정한다. 새로 배정했으면 True."""
    sessions, devices = schema.agent_sessions, schema.devices
    layouts, slots = schema.office_layouts, schema.office_slots
    owner = connection.execute(
        select(devices.c.owner_user_id)
        .select_from(sessions.join(devices, devices.c.id == sessions.c.device_id))
        .where(sessions.c.id == session_id)
    ).scalar_one()

    connection.execute(
        insert(layouts)
        .values(scope="user", owner_user_id=owner)
        .on_conflict_do_nothing(index_elements=["owner_user_id"])
    )
    # 같은 사용자의 동시 배정을 직렬화한다.
    layout_id = connection.execute(
        select(layouts.c.id).where(layouts.c.owner_user_id == owner).with_for_update()
    ).scalar_one()

    if connection.execute(
        select(slots.c.id).where(slots.c.layout_id == layout_id, slots.c.session_id == session_id)
    ).first():
        return False

    occupant = sessions.alias("occupant")
    # 종료 신호 없이 멈춘 세션(터미널을 닫음 등)은 ENDED가 되지 않는다. 활동 없는 세션 숨기기(#245)
    # 기준을 넘겨 화면에서 사라진 세션의 책상은 새 세션에 내준다(#291). 세션 상태는 그대로이고, 다시
    # 활동하면 새 책상을 받는다. 승인 대기 세션은 사람이 봐야 하므로 비우지 않는다.
    minutes = instance.hide_idle_after_minutes(connection)
    agents, states = schema.agents, schema.agent_current_states
    waiting = exists().where(
        agents.c.session_id == occupant.c.id,
        agents.c.ended_at.is_(None),
        states.c.agent_id == agents.c.id,
        states.c.turn_state == "WAITING_USER",
    )
    idle = (
        (occupant.c.lifecycle != "ENDED")
        & (
            func.coalesce(occupant.c.last_event_at, occupant.c.started_at)
            < func.now() - dt.timedelta(minutes=minutes)
        )
        & ~waiting
        if minutes > 0
        else false()
    )
    free = connection.execute(
        select(slots.c.id)
        .select_from(slots.outerjoin(occupant, occupant.c.id == slots.c.session_id))
        .where(
            slots.c.layout_id == layout_id,
            or_(
                slots.c.session_id.is_(None),
                (occupant.c.lifecycle == "ENDED")
                & (occupant.c.ended_at < func.now() - DESK_RELEASE_GRACE),
                idle,
            ),
        )
        .order_by(slots.c.slot_index)
        .limit(1)
        .with_for_update(of=slots)
    ).scalar_one_or_none()

    if free is not None:
        connection.execute(
            update(slots)
            .where(slots.c.id == free)
            .values(session_id=session_id, assigned_at=func.now())
        )
    else:
        next_index = connection.execute(
            select(func.coalesce(func.max(slots.c.slot_index) + 1, 0)).where(
                slots.c.layout_id == layout_id
            )
        ).scalar_one()
        grid_x, grid_y = slot_cell(next_index)
        connection.execute(
            slots.insert().values(
                layout_id=layout_id,
                slot_index=next_index,
                grid_x=grid_x,
                grid_y=grid_y,
                session_id=session_id,
                assigned_at=func.now(),
            )
        )
    connection.execute(
        update(sessions)
        .where(sessions.c.id == session_id)
        .values(change_seq=func.nextval("state_change_seq"), changed_at=func.clock_timestamp())
    )
    return True

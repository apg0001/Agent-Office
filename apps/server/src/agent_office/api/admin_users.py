"""관리자 사용자 관리·감사 로그 조회(#28).

- 관리자 승인 정책(`ADMIN_APPROVAL`)의 대기 사용자 승인, 비활성화·재활성화
- 인스턴스 역할 변경은 SUPER_ADMIN만. 마지막 활성 SUPER_ADMIN 강등·비활성화는 DB 트리거가 막는다.
- 감사 로그는 최신순으로 커서(`before`) 페이지 조회. details에는 허용 목록 키의 짧은 값만 있다.
"""

from __future__ import annotations

import datetime as dt
import uuid
from typing import Annotated, Any, Literal

from fastapi import APIRouter, HTTPException, Path, Query, status
from pydantic import BaseModel, ConfigDict
from sqlalchemy import Connection, func, select, update
from sqlalchemy.exc import IntegrityError

from agent_office import audit
from agent_office.api.admin_settings import AdminDep
from agent_office.api.deps import CsrfDep, CurrentUser, DbConnection
from agent_office.auth import sessions as session_service
from agent_office.db import schema

router = APIRouter(prefix="/api/v1/admin", tags=["admin"])

UserId = Annotated[uuid.UUID, Path()]
InstanceRole = Literal["SUPER_ADMIN", "ADMIN", "USER"]


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AdminUser(StrictModel):
    id: uuid.UUID
    email: str
    display_name: str
    instance_role: InstanceRole
    status: Literal["pending", "active", "disabled"]
    email_verified: bool
    created_at: dt.datetime


class UserPage(StrictModel):
    users: list[AdminUser]
    total: int
    pending_approval: int


class RoleBody(StrictModel):
    instance_role: InstanceRole


class AuditEntry(StrictModel):
    id: int
    occurred_at: dt.datetime
    action: str
    outcome: str
    actor_display_name: str | None
    actor_device: bool
    target_type: str | None
    target_id: uuid.UUID | None
    details: dict[str, Any]


class AuditPage(StrictModel):
    entries: list[AuditEntry]
    next_before: int | None


def _user(connection: Connection, user_id: uuid.UUID) -> Any:
    row = connection.execute(select(schema.users).where(schema.users.c.id == user_id)).one_or_none()
    if row is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
    return row


def _managed_user(connection: Connection, admin: CurrentUser, user_id: uuid.UUID) -> Any:
    """상태를 바꿀 대상. ADMIN은 SUPER_ADMIN 계정을 승인·비활성화·재활성화할 수 없다(#64)."""
    row = _user(connection, user_id)
    if row.instance_role == "SUPER_ADMIN" and admin.instance_role != "SUPER_ADMIN":
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="forbidden")
    return row


def _as_admin_user(row: Any) -> AdminUser:
    return AdminUser(
        id=row.id,
        email=row.email,
        display_name=row.display_name,
        instance_role=row.instance_role,
        status=row.status,
        email_verified=row.email_verified_at is not None,
        created_at=row.created_at,
    )


def _change(
    connection: Connection, admin: CurrentUser, user_id: uuid.UUID, action: str, **values: Any
) -> AdminUser:
    if user_id == admin.id and ("status" in values):
        # 자기 계정을 비활성화하면 관리 화면에서 스스로 잠긴다.
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="cannot_modify_self")
    try:
        with connection.begin_nested():
            connection.execute(
                update(schema.users)
                .where(schema.users.c.id == user_id)
                .values(**values, updated_at=func.now())
            )
    except IntegrityError as error:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT, detail="last_super_admin"
        ) from error
    audit.record(
        connection,
        action,
        "success",
        actor_user_id=admin.id,
        target_type="user",
        target_id=user_id,
        details={"reason": str(values.get("status") or values.get("instance_role"))},
    )
    return _as_admin_user(_user(connection, user_id))


@router.get("/users", response_model=UserPage)
def list_users(
    _admin: AdminDep,
    connection: DbConnection,
    status_filter: Annotated[
        Literal["pending", "active", "disabled"] | None, Query(alias="status")
    ] = None,
    limit: Annotated[int, Query(ge=1, le=200)] = 50,
    offset: Annotated[int, Query(ge=0)] = 0,
) -> UserPage:
    users = schema.users
    query = select(users)
    if status_filter:
        query = query.where(users.c.status == status_filter)
    rows = connection.execute(
        query.order_by(users.c.created_at.desc(), users.c.id).limit(limit).offset(offset)
    ).all()
    total = connection.execute(select(func.count()).select_from(users)).scalar_one()
    pending = connection.execute(
        select(func.count())
        .select_from(users)
        .where(users.c.status == "pending", users.c.email_verified_at.is_not(None))
    ).scalar_one()
    return UserPage(users=[_as_admin_user(r) for r in rows], total=total, pending_approval=pending)


@router.post("/users/{user_id}/approve", response_model=AdminUser)
def approve_user(
    user_id: UserId, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> AdminUser:
    row = _managed_user(connection, admin, user_id)
    if row.status != "pending" or row.email_verified_at is None:
        # 이메일 확인 전에는 승인하지 않는다(주소 소유가 확인된 계정만).
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="not_awaiting_approval")
    return _change(connection, admin, user_id, "user.approved", status="active")


@router.post("/users/{user_id}/disable", response_model=AdminUser)
def disable_user(
    user_id: UserId, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> AdminUser:
    _managed_user(connection, admin, user_id)
    result = _change(connection, admin, user_id, "user.disabled", status="disabled")
    # 로그인 중인 브라우저 세션도 바로 끊는다.
    session_service.revoke_all(connection, user_id)
    return result


@router.post("/users/{user_id}/enable", response_model=AdminUser)
def enable_user(
    user_id: UserId, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> AdminUser:
    row = _managed_user(connection, admin, user_id)
    if row.status != "disabled":
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="not_disabled")
    target = "active" if row.email_verified_at is not None else "pending"
    return _change(connection, admin, user_id, "user.enabled", status=target)


@router.put("/users/{user_id}/role", response_model=AdminUser)
def change_role(
    user_id: UserId, body: RoleBody, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> AdminUser:
    if admin.instance_role != "SUPER_ADMIN":
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="forbidden")
    _user(connection, user_id)
    return _change(
        connection, admin, user_id, "user.role_changed", instance_role=body.instance_role
    )


@router.get("/audit", response_model=AuditPage)
def audit_log(
    _admin: AdminDep,
    connection: DbConnection,
    limit: Annotated[int, Query(ge=1, le=200)] = 50,
    before: Annotated[int | None, Query(ge=1)] = None,
    action: Annotated[str | None, Query(max_length=64, pattern=r"^[a-z][a-z0-9_.]*$")] = None,
) -> AuditPage:
    logs, users = schema.audit_logs, schema.users
    query = select(
        logs.c.id,
        logs.c.occurred_at,
        logs.c.action,
        logs.c.outcome,
        users.c.display_name,
        logs.c.actor_device_id,
        logs.c.target_type,
        logs.c.target_id,
        logs.c.details,
    ).select_from(logs.outerjoin(users, users.c.id == logs.c.actor_user_id))
    if before is not None:
        query = query.where(logs.c.id < before)
    if action:
        # 접두어로 거른다(예: "auth.", "team.").
        query = query.where(logs.c.action.startswith(action))
    rows = connection.execute(query.order_by(logs.c.id.desc()).limit(limit + 1)).all()
    entries = [
        AuditEntry(
            id=r.id,
            occurred_at=r.occurred_at,
            action=r.action,
            outcome=r.outcome,
            actor_display_name=r.display_name,
            actor_device=r.actor_device_id is not None,
            target_type=r.target_type,
            target_id=r.target_id,
            details=dict(r.details or {}),
        )
        for r in rows[:limit]
    ]
    return AuditPage(entries=entries, next_before=entries[-1].id if len(rows) > limit else None)

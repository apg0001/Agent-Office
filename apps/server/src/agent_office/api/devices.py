"""장치 등록·인증·heartbeat·토큰 회전·폐기(#26).

등록 흐름: 로그인한 사용자가 웹에서 단기 일회용 코드를 받고, Collector가 그 코드로
장치를 등록해 장치 토큰을 받는다. 소유자와 장치는 서버가 코드·토큰으로 판정하며
요청 본문의 사용자·장치 식별자를 신뢰하지 않는다. 계정 비밀번호는 Collector에 전달되지 않는다.
"""

from __future__ import annotations

import datetime as dt
import uuid
from typing import Annotated, Literal

from fastapi import APIRouter, HTTPException, Path, Response, status
from pydantic import BaseModel, ConfigDict, Field, StringConstraints
from sqlalchemy import func, select, update
from sqlalchemy.exc import IntegrityError

from agent_office import audit
from agent_office.api.deps import CsrfDep, DbConnection, DeviceDep, UserDep
from agent_office.db import schema
from agent_office.security import (
    new_device_token,
    new_registration_code,
    normalize_registration_code,
    token_hash,
)
from agent_office.state.engine import connection_state

REGISTRATION_CODE_TTL = dt.timedelta(minutes=10)
MAX_OPEN_REGISTRATION_CODES = 5
# 회전 응답을 받지 못한 Collector가 이전 토큰으로 재시도할 수 있는 유예 시간.
ROTATION_GRACE = dt.timedelta(minutes=5)

router = APIRouter(prefix="/api/v1/devices", tags=["devices"])

DeviceName = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)]
CollectorVersion = Annotated[str, StringConstraints(pattern=r"^[0-9A-Za-z.+-]{1,64}$")]
Platform = Literal["windows", "macos", "linux", "unknown"]


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RegistrationCodeResponse(StrictModel):
    code: str
    expires_at: dt.datetime


class RegisterRequest(StrictModel):
    registration_code: Annotated[str, StringConstraints(max_length=32)]
    name: DeviceName
    platform: Platform = "unknown"
    collector_version: CollectorVersion | None = None


class RegisterResponse(StrictModel):
    device_id: uuid.UUID
    device_token: str


class HeartbeatRequest(StrictModel):
    collector_version: CollectorVersion | None = None
    queue_depth: Annotated[int, Field(ge=0, le=10_000_000)] | None = None


class RotateResponse(StrictModel):
    device_token: str
    previous_token_expires_at: dt.datetime


class DeviceSummary(StrictModel):
    id: uuid.UUID
    name: str
    platform: Platform
    collector_version: str | None
    created_at: dt.datetime
    last_seen_at: dt.datetime | None
    revoked_at: dt.datetime | None
    # 마지막 heartbeat로 계산한 Collector 연결 상태(#36). 세션 종료와는 별개다.
    connection: Literal["ONLINE", "DISCONNECTED"]


def _invalid_code() -> HTTPException:
    # 없음·만료·사용됨을 구분하지 않아 코드 존재 여부를 알려주지 않는다.
    return HTTPException(
        status_code=status.HTTP_400_BAD_REQUEST, detail="invalid_registration_code"
    )


@router.post(
    "/registration-codes",
    status_code=status.HTTP_201_CREATED,
    response_model=RegistrationCodeResponse,
)
def create_registration_code(
    user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> RegistrationCodeResponse:
    codes = schema.device_registration_codes
    open_count = connection.execute(
        select(func.count())
        .select_from(codes)
        .where(
            codes.c.user_id == user.id,
            codes.c.consumed_at.is_(None),
            codes.c.expires_at > func.now(),
        )
    ).scalar_one()
    if open_count >= MAX_OPEN_REGISTRATION_CODES:
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS, detail="too_many_open_codes"
        )
    code = new_registration_code()
    expires_at = connection.execute(
        codes.insert()
        .values(
            user_id=user.id,
            code_hash=token_hash(code),
            expires_at=func.now() + REGISTRATION_CODE_TTL,
        )
        .returning(codes.c.expires_at)
    ).scalar_one()
    audit.record(
        connection,
        "device.registration_code.created",
        "success",
        actor_user_id=user.id,
    )
    return RegistrationCodeResponse(code=code, expires_at=expires_at)


@router.post("/register", status_code=status.HTTP_201_CREATED, response_model=RegisterResponse)
def register_device(request: RegisterRequest, connection: DbConnection) -> RegisterResponse:
    code = normalize_registration_code(request.registration_code)
    if code is None:
        raise _invalid_code()
    codes, devices = schema.device_registration_codes, schema.devices
    # 같은 코드로 동시에 등록하는 경쟁을 막기 위해 행을 잠근다.
    row = connection.execute(
        select(codes.c.id, codes.c.user_id)
        .select_from(codes.join(schema.users, schema.users.c.id == codes.c.user_id))
        .where(
            codes.c.code_hash == token_hash(code),
            codes.c.consumed_at.is_(None),
            codes.c.expires_at > func.now(),
            schema.users.c.status == "active",
        )
        .with_for_update(of=codes)
    ).one_or_none()
    if row is None:
        raise _invalid_code()

    try:
        with connection.begin_nested():
            device_id = connection.execute(
                devices.insert()
                .values(
                    owner_user_id=row.user_id,
                    name=request.name,
                    platform=request.platform,
                    collector_version=request.collector_version,
                )
                .returning(devices.c.id)
            ).scalar_one()
    except IntegrityError:
        # 같은 사용자의 활성 장치 이름 중복. 코드는 소비하지 않는다.
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT, detail="device_name_conflict"
        ) from None

    connection.execute(
        update(codes)
        .where(codes.c.id == row.id)
        .values(consumed_at=func.now(), device_id=device_id)
    )
    token = new_device_token()
    connection.execute(
        schema.device_credentials.insert().values(device_id=device_id, token_hash=token_hash(token))
    )
    audit.record(
        connection,
        "device.registered",
        "success",
        actor_user_id=row.user_id,
        actor_device_id=device_id,
        target_type="device",
        target_id=device_id,
        details={"platform": request.platform},
    )
    return RegisterResponse(device_id=device_id, device_token=token)


@router.post("/heartbeat", status_code=status.HTTP_204_NO_CONTENT)
def heartbeat(request: HeartbeatRequest, device: DeviceDep, connection: DbConnection) -> Response:
    connection.execute(
        schema.device_heartbeats.insert().values(
            device_id=device.id,
            collector_version=request.collector_version,
            queue_depth=request.queue_depth,
        )
    )
    values: dict[str, object] = {"last_seen_at": func.now()}
    if request.collector_version is not None:
        values["collector_version"] = request.collector_version
    connection.execute(
        update(schema.devices).where(schema.devices.c.id == device.id).values(**values)
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.post("/token/rotate", response_model=RotateResponse)
def rotate_token(device: DeviceDep, connection: DbConnection) -> RotateResponse:
    credentials = schema.device_credentials
    if device.credential_expires_at is not None:
        # 유예 중인 이전 토큰으로는 다시 회전할 수 없다. 새 토큰을 써야 한다.
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="rotation_in_progress")
    previous_expires_at = connection.execute(
        update(credentials)
        .where(credentials.c.id == device.credential_id)
        .values(expires_at=func.now() + ROTATION_GRACE)
        .returning(credentials.c.expires_at)
    ).scalar_one()
    token = new_device_token()
    connection.execute(
        credentials.insert().values(
            device_id=device.id,
            token_hash=token_hash(token),
            rotated_from_id=device.credential_id,
        )
    )
    audit.record(
        connection,
        "device.token.rotated",
        "success",
        actor_device_id=device.id,
        target_type="device",
        target_id=device.id,
    )
    return RotateResponse(device_token=token, previous_token_expires_at=previous_expires_at)


def _revoke(connection: DbConnection, device_id: uuid.UUID) -> None:
    connection.execute(
        update(schema.devices)
        .where(schema.devices.c.id == device_id, schema.devices.c.revoked_at.is_(None))
        .values(revoked_at=func.now())
    )
    connection.execute(
        update(schema.device_credentials)
        .where(
            schema.device_credentials.c.device_id == device_id,
            schema.device_credentials.c.revoked_at.is_(None),
        )
        .values(revoked_at=func.now())
    )


@router.post("/revoke", status_code=status.HTTP_204_NO_CONTENT)
def revoke_self(device: DeviceDep, connection: DbConnection) -> Response:
    """Collector 로그아웃·제거 시 자기 장치를 폐기한다."""
    _revoke(connection, device.id)
    audit.record(
        connection,
        "device.revoked",
        "success",
        actor_device_id=device.id,
        target_type="device",
        target_id=device.id,
        details={"reason": "self"},
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get("", response_model=list[DeviceSummary])
def list_devices(user: UserDep, connection: DbConnection) -> list[DeviceSummary]:
    devices = schema.devices
    rows = connection.execute(
        select(
            devices.c.id,
            devices.c.name,
            devices.c.platform,
            devices.c.collector_version,
            devices.c.created_at,
            devices.c.last_seen_at,
            devices.c.revoked_at,
        )
        .where(devices.c.owner_user_id == user.id)
        .order_by(devices.c.created_at)
    ).all()
    now = connection.execute(select(func.now())).scalar_one()
    return [
        DeviceSummary.model_validate(
            {
                **row._asdict(),
                "connection": "DISCONNECTED"
                if row.revoked_at is not None
                else connection_state(row.last_seen_at, now),
            }
        )
        for row in rows
    ]


@router.post("/{device_id}/revoke", status_code=status.HTTP_204_NO_CONTENT)
def revoke_owned(
    device_id: Annotated[uuid.UUID, Path()],
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Response:
    devices = schema.devices
    owned = connection.execute(
        select(devices.c.id).where(devices.c.id == device_id, devices.c.owner_user_id == user.id)
    ).one_or_none()
    if owned is None:
        # 다른 사용자의 장치는 존재 여부도 알려주지 않는다.
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
    _revoke(connection, device_id)
    audit.record(
        connection,
        "device.revoked",
        "success",
        actor_user_id=user.id,
        target_type="device",
        target_id=device_id,
        details={"reason": "owner"},
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)

"""알림 채널 관리(#70~#74). 관리자만.

비밀(Webhook URL·봇 토큰)은 쓰기만 하고 설정 여부만 돌려준다.
"""

from __future__ import annotations

import uuid
from typing import Annotated, Any, Literal

from fastapi import APIRouter, HTTPException, Path, Response, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import Connection, func, select, update

from agent_office import audit, secret_store
from agent_office.api.admin_settings import AdminDep
from agent_office.api.deps import CsrfDep, DbConnection
from agent_office.db import schema
from agent_office.notifications import manager
from agent_office.notifications.providers import PROVIDERS, ConfigError

router = APIRouter(prefix="/api/v1/admin/notifications", tags=["admin"])

ProviderName = Literal["discord", "telegram", "teams_workflows", "email_smtp"]
SeverityName = Literal["critical", "high", "warning", "info"]
ChannelId = Annotated[uuid.UUID, Path()]
MAX_CHANNELS = 20


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ChannelBody(StrictModel):
    provider: ProviderName
    display_name: str = Field(min_length=1, max_length=80)
    enabled: bool = True
    config: dict[str, Any] = Field(default_factory=dict)
    # 생성 때는 필요(이메일 제외), 수정 때 None이면 기존 유지.
    secret: str | None = Field(default=None, max_length=1024)
    severities: list[SeverityName] = Field(default=["critical", "high"], min_length=1)


class ChannelPatch(StrictModel):
    display_name: str | None = Field(default=None, min_length=1, max_length=80)
    enabled: bool | None = None
    config: dict[str, Any] | None = None
    secret: str | None = Field(default=None, max_length=1024)
    severities: list[SeverityName] | None = Field(default=None, min_length=1)


class ChannelView(StrictModel):
    id: uuid.UUID
    provider: ProviderName
    display_name: str
    enabled: bool
    config: dict[str, Any]
    secret_set: bool
    severities: list[SeverityName]


class TestResult(StrictModel):
    ok: bool
    error: str | None = None


def _view(row: Any) -> ChannelView:
    return ChannelView(
        id=row.id,
        provider=row.provider,
        display_name=row.display_name,
        enabled=row.enabled,
        config=dict(row.config or {}),
        secret_set=row.secret_ciphertext is not None,
        severities=list(row.subscribed_severities),
    )


def _bad(code: str) -> HTTPException:
    return HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=code)


def _validated(provider: str, config: dict[str, Any], secret: str | None) -> dict[str, Any]:
    try:
        return PROVIDERS[provider].validate(config, secret)
    except ConfigError as error:
        raise _bad(error.code) from error


def _encrypt(channel_id: uuid.UUID, secret: str | None) -> bytes | None:
    if not secret:
        return None
    if not secret_store.available():
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="secret_key_missing"
        )
    return secret_store.encrypt(manager.secret_name(channel_id), secret.strip())


def _row(connection: Connection, channel_id: uuid.UUID) -> Any:
    channels = schema.notification_channels
    row = connection.execute(select(channels).where(channels.c.id == channel_id)).one_or_none()
    if row is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
    return row


@router.get("", response_model=list[ChannelView])
def list_channels(_admin: AdminDep, connection: DbConnection) -> list[ChannelView]:
    channels = schema.notification_channels
    rows = connection.execute(select(channels).order_by(channels.c.created_at)).all()
    return [_view(r) for r in rows]


@router.post("", status_code=status.HTTP_201_CREATED, response_model=ChannelView)
def create_channel(
    body: ChannelBody, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> ChannelView:
    channels = schema.notification_channels
    count = connection.execute(select(func.count()).select_from(channels)).scalar_one()
    if count >= MAX_CHANNELS:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="too_many_channels")
    config = _validated(body.provider, body.config, body.secret)
    channel_id = uuid.uuid4()
    connection.execute(
        channels.insert().values(
            id=channel_id,
            provider=body.provider,
            display_name=body.display_name.strip(),
            enabled=body.enabled,
            config=config,
            secret_ciphertext=_encrypt(channel_id, body.secret),
            subscribed_severities=sorted(set(body.severities)),
        )
    )
    audit.record(
        connection,
        "notification.channel_created",
        "success",
        actor_user_id=admin.id,
        target_type="notification_channel",
        target_id=channel_id,
        details={"reason": body.provider},
    )
    return _view(_row(connection, channel_id))


@router.patch("/{channel_id}", response_model=ChannelView)
def update_channel(
    channel_id: ChannelId,
    body: ChannelPatch,
    admin: AdminDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> ChannelView:
    row = _row(connection, channel_id)
    secret = body.secret
    if body.config is not None or secret is not None:
        # 비밀을 바꾸지 않으면 기존 비밀로 설정을 다시 검사한다.
        current_secret = secret if secret is not None else manager._channel_secret(row)
        config = _validated(row.provider, body.config or dict(row.config or {}), current_secret)
    else:
        config = dict(row.config or {})
    values: dict[str, Any] = {"config": config, "updated_at": func.now()}
    if body.display_name is not None:
        values["display_name"] = body.display_name.strip()
    if body.enabled is not None:
        values["enabled"] = body.enabled
    if body.severities is not None:
        values["subscribed_severities"] = sorted(set(body.severities))
    if secret is not None:
        values["secret_ciphertext"] = _encrypt(channel_id, secret)
    channels = schema.notification_channels
    connection.execute(update(channels).where(channels.c.id == channel_id).values(**values))
    audit.record(
        connection,
        "notification.channel_updated",
        "success",
        actor_user_id=admin.id,
        target_type="notification_channel",
        target_id=channel_id,
    )
    return _view(_row(connection, channel_id))


@router.delete("/{channel_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_channel(
    channel_id: ChannelId, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> Response:
    _row(connection, channel_id)
    channels = schema.notification_channels
    connection.execute(channels.delete().where(channels.c.id == channel_id))
    audit.record(
        connection,
        "notification.channel_deleted",
        "success",
        actor_user_id=admin.id,
        target_type="notification_channel",
        target_id=channel_id,
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.post("/{channel_id}/test", response_model=TestResult)
def test_channel(
    channel_id: ChannelId, _admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> TestResult:
    """시험 알림을 보낸다. 실패는 원인 코드만(응답 본문·주소는 남기지 않음)."""
    _row(connection, channel_id)
    result = manager.send_test(connection, channel_id)
    return TestResult(ok=result.ok, error=result.error)

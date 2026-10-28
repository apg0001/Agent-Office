"""인스턴스 운영 설정(`instance_settings`) 읽기·쓰기. 비밀은 `secret_store`에 둔다(#67)."""

from __future__ import annotations

import uuid
from typing import Any

from sqlalchemy import Connection, func, select
from sqlalchemy.dialects.postgresql import insert

from agent_office.db import schema

SITE_KEY = "site"
SMTP_KEY = "mail.smtp"
SMTP_PASSWORD_SECRET = "mail.smtp.password"
OAUTH_PROVIDERS = ("google", "github")
# 활동 없는 세션 숨기기(#245): 마지막 활동 후 이 분이 지나면 사무실 화면에서 숨긴다. 0이면 끈다.
HIDE_IDLE_KEY = "office.hide_idle_after_minutes"
DEFAULT_HIDE_IDLE_MINUTES = 60
MAX_HIDE_IDLE_MINUTES = 7 * 24 * 60


def oauth_key(provider: str) -> str:
    return f"oauth.{provider}"


def oauth_secret(provider: str) -> str:
    return f"oauth.{provider}.client_secret"


def get_value(connection: Connection, key: str, *, lock: bool = False) -> Any:
    query = select(schema.instance_settings.c.value).where(schema.instance_settings.c.key == key)
    if lock:
        query = query.with_for_update()
    return connection.execute(query).scalar_one_or_none()


def put_value(
    connection: Connection, key: str, value: Any, user_id: uuid.UUID | None = None
) -> None:
    table = schema.instance_settings
    statement = insert(table).values(key=key, value=value, updated_by_user_id=user_id)
    connection.execute(
        statement.on_conflict_do_update(
            index_elements=[table.c.key],
            set_={
                "value": statement.excluded.value,
                "updated_by_user_id": statement.excluded.updated_by_user_id,
                "updated_at": func.now(),
            },
        )
    )


def delete_value(connection: Connection, key: str) -> None:
    table = schema.instance_settings
    connection.execute(table.delete().where(table.c.key == key))


def get_dict(connection: Connection, key: str) -> dict[str, Any]:
    value = get_value(connection, key)
    return value if isinstance(value, dict) else {}


def hide_idle_after_minutes(connection: Connection) -> int:
    """저장 값이 없거나 범위를 벗어나면 기본값. bool은 정수로 보지 않는다."""
    value = get_value(connection, HIDE_IDLE_KEY)
    valid = isinstance(value, int) and not isinstance(value, bool)
    if valid and 0 <= value <= MAX_HIDE_IDLE_MINUTES:
        return int(value)
    return DEFAULT_HIDE_IDLE_MINUTES


def office_settings(connection: Connection) -> dict[str, Any]:
    """사무실 스냅샷에 싣는 화면 설정. 숫자만 담는다."""
    return {"hide_idle_after_minutes": hide_idle_after_minutes(connection)}

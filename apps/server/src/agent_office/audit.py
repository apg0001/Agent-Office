"""감사 로그 기록. details에는 허용 목록 키의 짧은 값만 넣는다."""

from __future__ import annotations

import uuid

from sqlalchemy import Connection

from agent_office.db import schema

ALLOWED_DETAIL_KEYS = {"platform", "collector_version", "reason", "credential_id"}


def record(
    connection: Connection,
    action: str,
    outcome: str,
    *,
    actor_user_id: uuid.UUID | None = None,
    actor_device_id: uuid.UUID | None = None,
    target_type: str | None = None,
    target_id: uuid.UUID | None = None,
    details: dict[str, str] | None = None,
) -> None:
    safe = {
        key: value[:64]
        for key, value in (details or {}).items()
        if key in ALLOWED_DETAIL_KEYS and isinstance(value, str)
    }
    connection.execute(
        schema.audit_logs.insert().values(
            action=action,
            outcome=outcome,
            actor_user_id=actor_user_id,
            actor_device_id=actor_device_id,
            target_type=target_type,
            target_id=target_id,
            details=safe,
        )
    )

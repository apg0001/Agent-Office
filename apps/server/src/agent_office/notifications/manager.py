"""알림 라우팅·중복 억제·복구 알림·재시도·속도 제한(#70).

- 켜진 채널 중 심각도를 구독한 채널로만 보낸다(복수 채널).
- 같은 종류의 장애 알림은 1시간 안에 다시 보내지 않는다.
- `X.failed`를 보낸 뒤에만 `X.recovered`를 보낸다.
  열린 장애는 instance_settings에 남아 재시작에도 유지된다.
- 채널마다 시간당 20건, 실패는 재시도 가능한 경우 최대 3번(1초·4초 간격).
- 결과는 감사 로그에 채널 ID·원인 코드만 남긴다.
"""

from __future__ import annotations

import datetime as dt
import logging
import time
import uuid
from dataclasses import dataclass
from typing import Any

from sqlalchemy import Connection, any_, literal, select

from agent_office import audit, instance, secret_store
from agent_office.db import schema
from agent_office.notifications.model import Notification, build, render
from agent_office.notifications.providers import PROVIDERS, DeliveryError
from agent_office.ratelimit import KeyedRateLimiter

logger = logging.getLogger(__name__)

DEDUP_WINDOW = dt.timedelta(hours=1)
OPEN_INCIDENTS_KEY = "notifications.open_incidents"
RETRY_DELAYS = (1.0, 4.0)
channel_limiter = KeyedRateLimiter(limit=20, window_seconds=3600)
_last_sent: dict[str, dt.datetime] = {}


@dataclass(frozen=True)
class DeliveryResult:
    channel_id: uuid.UUID
    ok: bool
    error: str | None = None


def secret_name(channel_id: uuid.UUID) -> str:
    return f"notification.{channel_id}"


def _channel_secret(row: Any) -> str | None:
    if row.secret_ciphertext is None:
        return None
    return secret_store.decrypt(secret_name(row.id), bytes(row.secret_ciphertext))


def deliver(
    connection: Connection, row: Any, notification: Notification, *, sleep: Any = time.sleep
) -> DeliveryResult:
    provider = PROVIDERS.get(row.provider)
    if provider is None:
        return DeliveryResult(row.id, False, "unknown_provider")
    if channel_limiter.hit(str(row.id)) is not None:
        return DeliveryResult(row.id, False, "rate_limited")
    try:
        secret = _channel_secret(row)
    except (secret_store.SecretKeyMissing, secret_store.SecretUnreadable):
        return DeliveryResult(row.id, False, "secret_unreadable")
    name = instance.get_dict(connection, instance.SITE_KEY).get("service_name") or "Agent Office"
    text = render(notification, str(name))
    attempts = (0.0, *RETRY_DELAYS)
    error: DeliveryError | None = None
    for delay in attempts:
        if delay:
            sleep(delay)
        try:
            provider.send(text, dict(row.config or {}), secret, connection)
            return DeliveryResult(row.id, True)
        except DeliveryError as failure:
            error = failure
            if not failure.retryable:
                break
    assert error is not None
    return DeliveryResult(row.id, False, error.code)


def _record(connection: Connection, result: DeliveryResult, kind: str) -> None:
    audit.record(
        connection,
        "notification.sent",
        "success" if result.ok else "failure",
        target_type="notification_channel",
        target_id=result.channel_id,
        details={"reason": result.error or kind.replace(".", "_")},
    )


def _open_incidents(connection: Connection) -> set[str]:
    value = instance.get_value(connection, OPEN_INCIDENTS_KEY)
    return set(value) if isinstance(value, list) else set()


def notify(
    connection: Connection,
    kind: str,
    code: str | None = None,
    *,
    now: dt.datetime | None = None,
    sleep: Any = time.sleep,
) -> list[DeliveryResult]:
    """종류 코드로 알림을 보낸다. 억제·구독 없음이면 빈 목록."""
    notification = build(kind, code)
    now = now or dt.datetime.now(dt.UTC)
    base, _, phase = kind.rpartition(".")
    incidents = _open_incidents(connection)
    if phase == "recovered":
        if base not in incidents:
            return []
        incidents.discard(base)
        instance.put_value(connection, OPEN_INCIDENTS_KEY, sorted(incidents))
    elif phase == "failed":
        last = _last_sent.get(kind)
        if base in incidents and last is not None and now - last < DEDUP_WINDOW:
            return []
        incidents.add(base)
        instance.put_value(connection, OPEN_INCIDENTS_KEY, sorted(incidents))
    _last_sent[kind] = now
    channels = schema.notification_channels
    rows = connection.execute(
        select(channels).where(
            channels.c.enabled.is_(True),
            literal(notification.severity) == any_(channels.c.subscribed_severities),
        )
    ).all()
    results = [deliver(connection, row, notification, sleep=sleep) for row in rows]
    for result in results:
        _record(connection, result, kind)
        if not result.ok:
            logger.warning("notification delivery failed: %s", result.error)
    return results


def notify_safely(kind: str, code: str | None = None) -> None:
    """백그라운드 작업용: 자체 트랜잭션으로 보내고 어떤 실패도 호출자에게 올리지 않는다."""
    from agent_office.db.engine import get_engine

    try:
        with get_engine().begin() as connection:
            notify(connection, kind, code)
    except Exception as error:  # noqa: BLE001 - 알림 실패가 백업·정리를 멈추면 안 된다.
        logger.warning("notification failed: %s", type(error).__name__)


def send_test(connection: Connection, channel_id: uuid.UUID) -> DeliveryResult:
    row = connection.execute(
        select(schema.notification_channels).where(schema.notification_channels.c.id == channel_id)
    ).one()
    result = deliver(connection, row, build("channel.test"))
    _record(connection, result, "channel.test")
    return result


def reset() -> None:
    """테스트용."""
    _last_sent.clear()
    channel_limiter.reset()

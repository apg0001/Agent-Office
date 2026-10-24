"""준비 상태·운영 지표·외부 Heartbeat(#59).

- 준비 상태: DB 연결과 마이그레이션 버전만 확인한다(익명, 세부 정보 없음).
- 운영 지표: 관리자만. 호스트 자원·DB·수집 상태의 숫자만 담고 사용자·장치 식별자는 없다.
- 외부 Heartbeat: `AGENT_OFFICE_HEARTBEAT_URL`을 설정하면 준비 상태가 정상일 때만 주기적으로
  호출한다. 외부 서비스(healthchecks.io, Uptime Kuma push 등)가 신호가 끊기면 알려 주므로
  호스트 전원 차단·네트워크 단절을 감지할 수 있다. URL은 비밀로 보고 로그에 남기지 않는다.
"""

from __future__ import annotations

import asyncio
import datetime as dt
import logging
import os
import shutil
import time
import urllib.request
from functools import lru_cache
from pathlib import Path
from typing import Any

from alembic.script import ScriptDirectory
from sqlalchemy import Connection, func, select, text
from starlette.concurrency import run_in_threadpool

from agent_office.db import schema
from agent_office.state.engine import CONNECTION_TIMEOUT

logger = logging.getLogger(__name__)

HEARTBEAT_URL_ENV = "AGENT_OFFICE_HEARTBEAT_URL"
HEARTBEAT_INTERVAL_ENV = "AGENT_OFFICE_HEARTBEAT_INTERVAL_SECONDS"
DEFAULT_HEARTBEAT_INTERVAL = 60
DISK_PATH_ENV = "AGENT_OFFICE_DISK_PATH"


@lru_cache(maxsize=1)
def expected_revision() -> str | None:
    """패키지에 들어 있는 마이그레이션의 head. 이 서버 코드가 기대하는 DB 버전이다."""
    script = ScriptDirectory(str(Path(__file__).parent / "db" / "migrations"))
    return script.get_current_head()


def migration_revision(connection: Connection) -> str | None:
    try:
        with connection.begin_nested():
            value = connection.execute(text("SELECT version_num FROM alembic_version")).scalar()
    except Exception:  # noqa: BLE001 - 표가 없으면 아직 마이그레이션 전이다.
        return None
    return None if value is None else str(value)


def readiness(connection: Connection) -> dict[str, Any]:
    """`{"ready": bool, "checks": {"database": .., "migrations": ..}}`. 값은 ok/fail/outdated만."""
    connection.execute(select(1))
    revision = migration_revision(connection)
    migrations = (
        "ok" if revision == expected_revision() else ("fail" if revision is None else "outdated")
    )
    return {"ready": migrations == "ok", "checks": {"database": "ok", "migrations": migrations}}


def _memory() -> dict[str, int] | None:
    try:
        with open("/proc/meminfo", encoding="ascii") as handle:
            values = {
                line.split(":")[0]: int(line.split()[1]) * 1024 for line in handle if ":" in line
            }
    except (OSError, ValueError, IndexError):
        return None
    total, available = values.get("MemTotal"), values.get("MemAvailable")
    if total is None or available is None:
        return None
    return {"total_bytes": total, "available_bytes": available}


def host_metrics() -> dict[str, Any]:
    load: list[float] | None
    try:
        load = [round(v, 2) for v in os.getloadavg()]
    except OSError:
        load = None
    disk = shutil.disk_usage(os.environ.get(DISK_PATH_ENV, "/"))
    return {
        "cpu_count": os.cpu_count(),
        "load_average": load,
        "memory": _memory(),
        "disk": {"total_bytes": disk.total, "free_bytes": disk.free},
    }


def database_metrics(connection: Connection) -> dict[str, Any]:
    started = time.perf_counter()
    connection.execute(select(1))
    latency_ms = round((time.perf_counter() - started) * 1000, 2)
    size = connection.execute(select(func.pg_database_size(func.current_database()))).scalar_one()
    return {
        "latency_ms": latency_ms,
        "size_bytes": int(size),
        "migration": migration_revision(connection),
    }


def collection_metrics(connection: Connection) -> dict[str, Any]:
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    s = schema
    hour_ago = now - dt.timedelta(hours=1)

    def scalar(statement: Any) -> Any:
        return connection.execute(statement).scalar_one()

    lag = scalar(
        select(
            func.percentile_cont(0.95).within_group(
                func.extract(
                    "epoch", s.activity_events.c.received_at - s.activity_events.c.occurred_at
                )
            )
        ).where(s.activity_events.c.received_at >= hour_ago)
    )
    versions = connection.execute(
        select(
            s.device_heartbeats.c.collector_version,
            func.count(func.distinct(s.device_heartbeats.c.device_id)),
        )
        .where(s.device_heartbeats.c.received_at >= now - dt.timedelta(days=1))
        .group_by(s.device_heartbeats.c.collector_version)
    ).all()
    queue = scalar(
        select(func.max(s.device_heartbeats.c.queue_depth)).where(
            s.device_heartbeats.c.received_at >= now - CONNECTION_TIMEOUT
        )
    )
    return {
        "users": scalar(select(func.count()).select_from(s.users)),
        "devices": scalar(
            select(func.count()).select_from(s.devices).where(s.devices.c.revoked_at.is_(None))
        ),
        "devices_online": scalar(
            select(func.count())
            .select_from(s.devices)
            .where(
                s.devices.c.revoked_at.is_(None),
                s.devices.c.last_seen_at >= now - CONNECTION_TIMEOUT,
            )
        ),
        "open_sessions": scalar(
            select(func.count())
            .select_from(s.agent_sessions)
            .where(s.agent_sessions.c.lifecycle != "ENDED")
        ),
        "events_last_hour": scalar(
            select(func.count())
            .select_from(s.activity_events)
            .where(s.activity_events.c.received_at >= hour_ago)
        ),
        # 이벤트가 일어난 뒤 서버에 도착하기까지(오프라인 큐 포함) 95백분위 초.
        "ingest_lag_p95_seconds": None if lag is None else round(float(lag), 1),
        "max_collector_queue_depth": queue,
        "collector_versions": {str(v or "unknown"): int(c) for v, c in versions},
    }


def heartbeat_url() -> str | None:
    value = os.environ.get(HEARTBEAT_URL_ENV, "").strip()
    return value if value.startswith(("https://", "http://")) else None


def heartbeat_interval() -> int:
    try:
        return max(30, int(os.environ.get(HEARTBEAT_INTERVAL_ENV, DEFAULT_HEARTBEAT_INTERVAL)))
    except ValueError:
        return DEFAULT_HEARTBEAT_INTERVAL


last_heartbeat: dict[str, Any] = {"at": None, "ok": None}


def _ping(url: str) -> bool:
    request = urllib.request.Request(url, method="GET", headers={"User-Agent": "agent-office"})
    try:
        with urllib.request.urlopen(request, timeout=10) as response:  # noqa: S310 - 운영자 설정 URL
            status: int = response.status
            return 200 <= status < 300
    except OSError:
        return False


def _ready_now() -> bool:
    from agent_office.db.engine import get_engine

    try:
        with get_engine().begin() as connection:
            ready: bool = readiness(connection)["ready"]
            return ready
    except Exception:  # noqa: BLE001 - DB가 죽었으면 신호를 보내지 않아 외부가 알게 한다.
        return False


async def send_heartbeats() -> None:
    """준비 상태가 정상일 때만 외부로 신호를 보낸다. 실패는 결과만 기록한다(URL은 남기지 않음)."""
    url = heartbeat_url()
    if url is None:
        return
    while True:
        ok = False
        if await run_in_threadpool(_ready_now):
            ok = await run_in_threadpool(_ping, url)
            if not ok:
                logger.warning("external heartbeat failed")
            # 전송 실패·복구를 다른 채널로 알린다(호스트가 꺼진 경우는 외부 서비스가 알린다).
            from agent_office.notifications.manager import notify_safely

            if not ok:
                await run_in_threadpool(notify_safely, "heartbeat.failed")
            elif last_heartbeat["ok"] is False:
                await run_in_threadpool(notify_safely, "heartbeat.recovered")
        last_heartbeat.update(at=dt.datetime.now(dt.UTC).isoformat(), ok=ok)
        await asyncio.sleep(heartbeat_interval())

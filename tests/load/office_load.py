"""사무실 부하·지연 실측(#63). 합성 이벤트만 보낸다(원문 없음).

사용(운영 Compose가 떠 있고 관리자 세션 쿠키 값을 알 때):
  python office_load.py --base http://127.0.0.1:18080 --cookie <ao_session 값> \
      --devices 3 --sessions 30 --subagents 2 --rounds 60 --interval 2

라운드마다 모든 세션의 모든 에이전트가 활동을 바꾸는 이벤트를 한 배치씩 보낸다. WebSocket
증분으로 그 라운드의 모든 에이전트 변경이 보일 때까지의 시간을 "표시 지연"으로 잰다.
결과는 JSON(라운드 지연 p50·p95·최대, 전송 실패, 재연결 회복 시간)으로 표준 출력에 낸다.
"""

from __future__ import annotations

import argparse
import asyncio
import datetime as dt
import json
import statistics
import time
import uuid
from typing import Any

import httpx
import websockets

ACTIVITIES = ["reading", "coding", "testing", "browsing"]


def percentile(values: list[float], q: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, round(q * (len(ordered) - 1))))
    return round(ordered[index], 3)


class Office:
    def __init__(self, base: str, cookie: str) -> None:
        self.base = base
        self.cookie = cookie
        self.http = httpx.AsyncClient(base_url=base, timeout=15)

    def headers(self, csrf: str | None = None) -> dict[str, str]:
        out = {"Cookie": f"ao_session={self.cookie}", "Origin": self.base}
        if csrf:
            out["X-CSRF-Token"] = csrf
        return out

    async def csrf(self) -> str:
        response = await self.http.get("/api/v1/auth/session", headers=self.headers())
        response.raise_for_status()
        return str(response.json()["csrf_token"])

    async def register(self, count: int) -> list[dict[str, str]]:
        csrf = await self.csrf()
        devices = []
        for index in range(count):
            code = await self.http.post(
                "/api/v1/devices/registration-codes", headers=self.headers(csrf)
            )
            code.raise_for_status()
            device = await self.http.post(
                "/api/v1/devices/register",
                json={
                    "registration_code": code.json()["code"],
                    "name": f"load-{index}",
                    "platform": "linux",
                },
            )
            device.raise_for_status()
            devices.append(device.json())
        return devices


class Agent:
    def __init__(self, session: Session, parent: Agent | None) -> None:
        self.id = uuid.uuid4()
        self.session = session
        self.parent = parent


class Session:
    def __init__(self, device: dict[str, str]) -> None:
        self.device = device
        self.id = uuid.uuid4()
        self.sequence = 0
        self.agents: list[Agent] = []

    def event(self, agent: Agent, event_type: str, activity: str) -> dict[str, Any]:
        self.sequence += 1
        return {
            "schema_version": "1.0",
            "event_id": str(uuid.uuid4()),
            "device_id": self.device["device_id"],
            "session_id": str(self.id),
            "agent_id": str(agent.id),
            "parent_agent_id": str(agent.parent.id) if agent.parent else None,
            "provider": "claude",
            "source": "hook",
            "event_type": event_type,
            "activity": activity,
            "confidence": "observed",
            "occurred_at": dt.datetime.now(dt.UTC).isoformat(),
            "sequence": self.sequence,
        }


async def send(
    http: httpx.AsyncClient, session: Session, events: list[dict[str, Any]]
) -> bool:
    # Collector처럼 재시도한다(서버 재시작 동안 최대 약 30초).
    for attempt in range(10):
        try:
            response = await http.post(
                "/api/v1/events/batch",
                json={"schema_version": "1.0", "events": events},
                headers={"Authorization": f"Bearer {session.device['device_token']}"},
            )
            if response.status_code == 200:
                return True
        except httpx.HTTPError:
            pass
        await asyncio.sleep(min(5.0, 0.5 * (attempt + 1)))
    return False


class Watcher:
    """WebSocket 증분을 받아 에이전트별 마지막 활동을 기록한다. 끊기면 다시 연결한다."""

    def __init__(self, office: Office) -> None:
        self.office = office
        self.activity: dict[str, str] = {}
        self.changes = 0
        self.reconnects: list[float] = []
        self.connected = asyncio.Event()
        self.stop = False

    async def run(self) -> None:
        url = self.office.base.replace("http", "ws", 1) + "/api/v1/live"
        lost_at: float | None = None
        while not self.stop:
            try:
                async with websockets.connect(
                    url, additional_headers=self.office.headers(), open_timeout=10
                ) as ws:
                    async for raw in ws:
                        message = json.loads(raw)
                        if message["type"] == "snapshot":
                            self.connected.set()
                            if lost_at is not None:
                                self.reconnects.append(time.monotonic() - lost_at)
                                lost_at = None
                        for agent in message.get("agents", []):
                            self.activity[agent["id"]] = agent["activity"]
                            self.changes += 1
                        if self.stop:
                            return
            except (OSError, websockets.WebSocketException):
                pass
            if lost_at is None:
                lost_at = time.monotonic()
            self.connected.clear()
            await asyncio.sleep(0.5)


async def main(args: argparse.Namespace) -> dict[str, Any]:
    office = Office(args.base, args.cookie)
    devices = await office.register(args.devices)
    sessions = [Session(devices[i % len(devices)]) for i in range(args.sessions)]
    http = httpx.AsyncClient(base_url=args.base, timeout=15)
    for session in sessions:
        lead = Agent(session, None)
        session.agents = [lead, *(Agent(session, lead) for _ in range(args.subagents))]
        events = [session.event(lead, "session.started", "unknown")]
        events += [
            session.event(a, "agent.started", "reading") for a in session.agents[1:]
        ]
        await send(http, session, events)

    watcher = Watcher(office)
    task = asyncio.create_task(watcher.run())
    await asyncio.wait_for(watcher.connected.wait(), 30)
    total_agents = args.sessions * (1 + args.subagents)
    latencies: list[float] = []
    ingest: list[float] = []
    failures = 0
    missing_rounds = 0
    for round_index in range(args.rounds):
        activity = ACTIVITIES[round_index % len(ACTIVITIES)]
        started = time.monotonic()
        # 실제 Collector처럼 장치마다 그 장치의 모든 세션 이벤트를 배치 하나로 보낸다.
        by_device: dict[str, list[dict[str, Any]]] = {}
        first: dict[str, Session] = {}
        for s in sessions:
            key = s.device["device_id"]
            first.setdefault(key, s)
            by_device.setdefault(key, []).extend(
                s.event(a, "tool.started", activity) for a in s.agents
            )
        results = await asyncio.gather(
            *(send(http, first[key], batch) for key, batch in by_device.items())
        )
        failures += results.count(False)
        ingest.append(time.monotonic() - started)
        deadline = started + args.timeout
        while time.monotonic() < deadline:
            done = sum(1 for v in watcher.activity.values() if v == activity)
            if done >= total_agents:
                latencies.append(time.monotonic() - started)
                break
            await asyncio.sleep(0.02)
        else:
            missing_rounds += 1
        await asyncio.sleep(max(0.0, args.interval - (time.monotonic() - started)))
    watcher.stop = True
    task.cancel()
    return {
        "sessions": args.sessions,
        "agents": total_agents,
        "rounds": args.rounds,
        "events_per_round": total_agents,
        "display_latency_seconds": {
            "p50": percentile(latencies, 0.5),
            "p95": percentile(latencies, 0.95),
            "max": round(max(latencies), 3) if latencies else None,
            "mean": round(statistics.fmean(latencies), 3) if latencies else None,
        },
        # 장시간 실행에서 데이터 증가에 따른 저하를 보려고 처음·마지막 10% 라운드를 따로 낸다.
        "display_p95_first_10pct": percentile(
            latencies[: max(1, len(latencies) // 10)], 0.95
        ),
        "display_p95_last_10pct": percentile(
            latencies[-max(1, len(latencies) // 10) :], 0.95
        ),
        "ingest_p95_first_10pct": percentile(ingest[: max(1, len(ingest) // 10)], 0.95),
        "ingest_p95_last_10pct": percentile(ingest[-max(1, len(ingest) // 10) :], 0.95),
        # 라운드의 모든 배치가 서버 응답(저장·상태 반영 완료)을 받기까지.
        "ingest_seconds": {
            "p50": percentile(ingest, 0.5),
            "p95": percentile(ingest, 0.95),
        },
        "rounds_not_fully_visible": missing_rounds,
        "failed_batches": failures,
        "ws_reconnect_seconds": [round(r, 2) for r in watcher.reconnects],
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    parser.add_argument("--cookie", required=True)
    parser.add_argument("--devices", type=int, default=3)
    parser.add_argument("--sessions", type=int, default=30)
    parser.add_argument("--subagents", type=int, default=2)
    parser.add_argument("--rounds", type=int, default=60)
    parser.add_argument("--interval", type=float, default=2.0)
    parser.add_argument("--timeout", type=float, default=10.0)
    print(
        json.dumps(asyncio.run(main(parser.parse_args())), ensure_ascii=False, indent=1)
    )

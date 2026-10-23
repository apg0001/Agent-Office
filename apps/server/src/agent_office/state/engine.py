"""상태 전이 규칙(순수 함수). DB 없이 테스트할 수 있게 저장과 분리한다.

독립 상태축(마스터 사양 §7):
1. Collector 연결: ONLINE / DISCONNECTED — 마지막 heartbeat로 읽을 때 계산
2. 세션 생명주기: DISCOVERED / OPEN / ENDED / UNKNOWN — 저장 + 읽을 때 UNKNOWN 투영
3. 에이전트 활동: WORKING / THINKING / WAITING_USER / IDLE / UNKNOWN — 저장
"""

from __future__ import annotations

import datetime as dt
from dataclasses import dataclass

# Collector heartbeat가 이보다 오래 없으면 연결이 끊긴 것으로 본다.
CONNECTION_TIMEOUT = dt.timedelta(seconds=120)
# 연결이 끊기고 이보다 오래 조용한 OPEN 세션은 열림을 확인할 수 없어 UNKNOWN으로 보인다.
SESSION_UNCONFIRMED_AFTER = dt.timedelta(minutes=30)


def connection_state(last_seen_at: dt.datetime | None, now: dt.datetime) -> str:
    if last_seen_at is None or now - last_seen_at > CONNECTION_TIMEOUT:
        return "DISCONNECTED"
    return "ONLINE"


def effective_lifecycle(
    lifecycle: str,
    last_event_at: dt.datetime | None,
    connection: str,
    now: dt.datetime,
) -> str:
    """저장값은 바꾸지 않는다. 조용하다는 이유만으로 ENDED가 되지 않는다."""
    if (
        lifecycle == "OPEN"
        and connection == "DISCONNECTED"
        and (last_event_at is None or now - last_event_at > SESSION_UNCONFIRMED_AFTER)
    ):
        return "UNKNOWN"
    return lifecycle


@dataclass(frozen=True)
class Lifecycle:
    state: str
    started_at: dt.datetime | None
    ended_at: dt.datetime | None


def next_lifecycle(current: Lifecycle, event_type: str, occurred_at: dt.datetime) -> Lifecycle:
    """세션 생명주기 전이. 종료 시각보다 오래된 이벤트로는 다시 열지 않는다."""
    if (
        current.state == "ENDED"
        and current.ended_at is not None
        and occurred_at <= current.ended_at
    ):
        return current
    started_at = current.started_at
    if event_type == "session.started" and (started_at is None or occurred_at < started_at):
        started_at = occurred_at
    if event_type == "session.ended":
        return Lifecycle("ENDED", started_at, occurred_at)
    if event_type == "session.discovered" and current.state in {"DISCOVERED", "UNKNOWN"}:
        return Lifecycle("DISCOVERED", started_at, None)
    # 세션에서 무엇이든 관측됐다면 열려 있다는 근거다. 종료 뒤 더 늦은 활동은 재개(resume)다.
    return Lifecycle("OPEN", started_at, None)


@dataclass(frozen=True)
class AgentActivity:
    turn_state: str
    activity: str


IDLE = AgentActivity("IDLE", "idle")


def next_activity(
    current: AgentActivity, event_type: str, activity: str, confidence: str
) -> AgentActivity:
    """에이전트 활동 전이. WAITING_USER는 관측된 승인 요청에서만 만든다."""
    if event_type in {"session.started", "turn.completed", "turn.interrupted", "session.ended"}:
        return IDLE
    if event_type == "agent.completed":
        return IDLE
    if event_type == "turn.started":
        return AgentActivity("THINKING", "thinking")
    if event_type == "tool.started":
        return AgentActivity("WORKING", activity)
    if event_type in {"tool.completed", "tool.failed"}:
        # 도구가 끝나도 턴은 계속된다. 다음 판단 중이다.
        return AgentActivity("THINKING", "thinking")
    if event_type == "approval.requested":
        if confidence == "observed":
            return AgentActivity("WAITING_USER", "waiting_user")
        return current
    if event_type == "approval.resolved":
        return AgentActivity(
            "WORKING", current.activity if current.activity != "waiting_user" else "unknown"
        )
    if event_type == "agent.started":
        return AgentActivity("WORKING", "unknown" if activity == "idle" else activity)
    # session.discovered, plan.updated, unknown: 활동을 바꿀 근거가 없다.
    return current


def is_newer(
    occurred_at: dt.datetime,
    sequence: int,
    last_occurred_at: dt.datetime | None,
    last_sequence: int | None,
) -> bool:
    """늦게 도착한 과거 이벤트가 현재 상태를 되돌리지 않게 비교한다."""
    if last_occurred_at is None:
        return True
    if occurred_at != last_occurred_at:
        return occurred_at > last_occurred_at
    return last_sequence is None or sequence > last_sequence

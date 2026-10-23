"""Claude Code Hook·JSONL 이중 수집 중복 제거 규칙의 참조 구현(#20).

두 소스가 모두 관측할 수 있는 활동만 결정적 키를 가진다. 같은 키는 같은 `event_id`가
되므로 Collector 재시작으로 메모리 상태를 잃어도 서버 멱등성으로 다시 걸러진다.
원문 값(프롬프트, 도구 입력·출력, 경로)은 읽지 않고 식별자만 꺼낸다.
"""

from __future__ import annotations

import uuid
from collections.abc import Iterable
from dataclasses import dataclass
from typing import Any

# 키 → event_id 변환용 고정 namespace. 바꾸면 기존 큐와 서버의 멱등성이 깨진다.
EVENT_ID_NAMESPACE = uuid.UUID("6f9c2b1e-4d0a-5c3b-9e7f-2a1d8c4b6e50")
PROVIDER = "claude"
# transcript에서 활동을 읽는 줄 유형. api-request-blob 같은 내부 유형은 이전 메시지를
# 다시 담고 있어 같은 tool_use가 중복으로 나타나므로 읽지 않는다.
TRANSCRIPT_ACTIVITY_LINE_TYPES = {"assistant", "user"}


@dataclass(frozen=True)
class Candidate:
    """정규화 직전 후보. 값은 식별자와 enum뿐이다."""

    event_type: str
    source: str
    session_ref: str
    activity_ref: str | None
    agent_ref: str | None = None

    @property
    def dedup_kind(self) -> str:
        # 완료와 실패는 같은 도구 호출의 끝이므로 한 키로 합친다.
        if self.event_type in {"tool.completed", "tool.failed"}:
            return "tool.finished"
        return self.event_type


def dedup_key(device_ref: str, candidate: Candidate) -> str | None:
    """교차 소스 키. 한 소스에서만 관측되는 활동은 None이다."""
    if candidate.activity_ref is None:
        return None
    scope = f"{device_ref}|{PROVIDER}|{candidate.session_ref}"
    return f"{scope}|{candidate.dedup_kind}|{candidate.activity_ref}"


def event_id_for(device_ref: str, candidate: Candidate) -> uuid.UUID:
    key = dedup_key(device_ref, candidate)
    # 키가 없는 Hook 전용 이벤트는 큐에 처음 기록할 때 무작위 UUID를 받는다.
    return uuid.uuid5(EVENT_ID_NAMESPACE, key) if key else uuid.uuid4()


def _string(value: object) -> str | None:
    return value if isinstance(value, str) and value else None


def from_hook(payload: dict[str, Any]) -> list[Candidate]:
    session = _string(payload.get("session_id"))
    if session is None:
        return []
    event = payload.get("hook_event_name")
    agent = _string(payload.get("agent_id"))
    tool_use = _string(payload.get("tool_use_id"))
    prompt = _string(payload.get("prompt_id"))
    mapping: dict[object, tuple[str, str | None]] = {
        "SessionStart": ("session.started", None),
        "SessionEnd": ("session.ended", None),
        "UserPromptSubmit": ("turn.started", prompt),
        "Stop": ("turn.completed", None),
        "PreToolUse": ("tool.started", tool_use),
        "PostToolUse": ("tool.completed", tool_use),
        "PostToolUseFailure": ("tool.failed", tool_use),
        # PermissionRequest에는 tool_use_id가 없다(공식 문서·#19 실측).
        "PermissionRequest": ("approval.requested", None),
        "SubagentStart": ("agent.started", agent),
        "SubagentStop": ("agent.completed", agent),
    }
    if event not in mapping:
        return []
    event_type, activity = mapping[event]
    if event_type.startswith("tool.") and activity is None:
        return []
    owner = None if event_type.startswith("agent.") else agent
    return [Candidate(event_type, "hook", session, activity, owner)]


def from_transcript_line(item: dict[str, Any]) -> list[Candidate]:
    if item.get("type") not in TRANSCRIPT_ACTIVITY_LINE_TYPES:
        return []
    session = _string(item.get("sessionId"))
    message = item.get("message")
    if session is None or not isinstance(message, dict):
        return []
    agent = _string(item.get("agentId"))
    content = message.get("content")
    blocks = (
        [b for b in content if isinstance(b, dict)] if isinstance(content, list) else []
    )
    candidates: list[Candidate] = []
    for block in blocks:
        block_type = block.get("type")
        if item["type"] == "assistant" and block_type == "tool_use":
            tool_use = _string(block.get("id"))
            if tool_use:
                candidates.append(
                    Candidate("tool.started", "jsonl", session, tool_use, agent)
                )
        elif item["type"] == "user" and block_type == "tool_result":
            tool_use = _string(block.get("tool_use_id"))
            if tool_use:
                failed = block.get("is_error") is True
                candidates.append(
                    Candidate(
                        "tool.failed" if failed else "tool.completed",
                        "jsonl",
                        session,
                        tool_use,
                        agent,
                    )
                )
    prompt = _string(item.get("promptId"))
    is_prompt_line = (
        item["type"] == "user"
        and agent is None
        and item.get("isMeta") is not True
        and not any(block.get("type") == "tool_result" for block in blocks)
    )
    if prompt and is_prompt_line:
        candidates.append(Candidate("turn.started", "jsonl", session, prompt))
    return candidates


class Deduplicator:
    """키별 최초 관측만 통과시킨다. 운영에서는 #33 SQLite 큐가 이 집합을 영속화한다."""

    def __init__(self, device_ref: str, seen: Iterable[str] = ()) -> None:
        self.device_ref = device_ref
        self.seen: set[str] = set(seen)
        self.suppressed = 0

    def accept(self, candidate: Candidate) -> bool:
        key = dedup_key(self.device_ref, candidate)
        if key is None:
            return True
        if key in self.seen:
            self.suppressed += 1
            return False
        self.seen.add(key)
        return True

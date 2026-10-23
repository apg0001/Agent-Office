"""Claude Code Hook 입력을 원문 없이 구조 통계로 축약하는 PoC 프로브."""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import os
import re
import sys
from collections import Counter
from pathlib import Path
from typing import Any

MAX_INPUT_BYTES = 1_048_576
MAX_TRANSCRIPT_BYTES = 4_194_304
MAX_TRANSCRIPT_LINES = 5_000
MAX_FINGERPRINTS = 64
# 같은 길이의 앞부분 지문끼리 비교해 append-only 여부를 판정한다.
PREFIX_LENGTHS = (256, 1024, 4096)

KNOWN_HOOK_EVENTS = {
    "Notification",
    "PermissionDenied",
    "PermissionRequest",
    "PostCompact",
    "PostToolUse",
    "PostToolUseFailure",
    "PreCompact",
    "PreToolUse",
    "SessionEnd",
    "SessionStart",
    "Stop",
    "StopFailure",
    "SubagentStart",
    "SubagentStop",
    "TaskCompleted",
    "TaskCreated",
    "UserPromptSubmit",
}
# 값은 버리고 존재·타입만 남기는 알려진 입력 필드(공식 Hooks 문서 기준).
# background_tasks·session_crons 안에는 작업 설명·명령·프롬프트 원문이 들어갈 수 있다.
KNOWN_INPUT_FIELDS = {
    "agent_id",
    "agent_transcript_path",
    "agent_type",
    "background_tasks",
    "cwd",
    "duration_ms",
    "effort",
    "error",
    "hook_event_name",
    "is_interrupt",
    "last_assistant_message",
    "mcp_server",
    "message",
    "model",
    "notification_type",
    "permission_mode",
    "permission_suggestions",
    "prompt",
    "prompt_id",
    "reason",
    "scratchpad_dir",
    "session_crons",
    "session_id",
    "source",
    "stop_hook_active",
    "title",
    "tool_input",
    "tool_name",
    "tool_response",
    "tool_use_id",
    "transcript_path",
    "trigger",
}
# 최상위 Hook 키 이름은 Claude Code가 정한 스키마 이름이다. 값이 아니라 이름만,
# 그리고 식별자 형태일 때만 미등록 필드로 보고한다.
FIELD_NAME_PATTERN = re.compile(r"[a-z][a-z0-9_]{0,39}")
# transcript 줄의 `type` 값도 스키마 이름이므로 같은 조건으로만 이름을 보고한다.
TYPE_NAME_PATTERN = re.compile(r"[a-z][a-z0-9_-]{0,39}")
TRANSCRIPT_KEY_PATTERN = re.compile(r"[a-z][A-Za-z0-9_]{0,39}")
SAFE_SOURCE_VALUES = {"startup", "resume", "clear", "compact"}
SAFE_REASON_VALUES = {
    "bypass_permissions_disabled",
    "clear",
    "logout",
    "other",
    "prompt_input_exit",
}
SAFE_TRIGGER_VALUES = {"manual", "auto"}
SAFE_PERMISSION_MODES = {
    "acceptEdits",
    "auto",
    "bypassPermissions",
    "default",
    "dontAsk",
    "manual",
    "plan",
}
SAFE_NOTIFICATION_TYPES = {
    "auth_success",
    "elicitation_dialog",
    "idle_prompt",
    "permission_prompt",
}
SAFE_TOOL_VALUES = {
    "Agent",
    "Bash",
    "Edit",
    "ExitPlanMode",
    "Glob",
    "Grep",
    "NotebookEdit",
    "Read",
    "Task",
    "TodoWrite",
    "WebFetch",
    "WebSearch",
    "Write",
}
SAFE_AGENT_TYPES = {"Explore", "Plan", "general-purpose", "poc-reader"}
SAFE_TRANSCRIPT_TYPES = {
    "assistant",
    "attachment",
    "file-history-snapshot",
    "progress",
    "queue-operation",
    "summary",
    "system",
    "user",
}
SAFE_TRANSCRIPT_KEYS = {
    "agentId",
    "cwd",
    "gitBranch",
    "isMeta",
    "isSidechain",
    "message",
    "parentUuid",
    "promptId",
    "requestId",
    "sessionId",
    "subtype",
    "timestamp",
    "toolUseResult",
    "type",
    "userType",
    "uuid",
    "version",
}
SAFE_CONTENT_BLOCK_TYPES = {"text", "thinking", "tool_result", "tool_use"}
PATH_TEMPLATE_WORDS = {"projects", "subagents"}


def value_type(value: object) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, int | float):
        return "number"
    if isinstance(value, str):
        return "string"
    if isinstance(value, list):
        return "array"
    if isinstance(value, dict):
        return "object"
    return "other"


def fingerprint(value: object, salt: str) -> str | None:
    if not isinstance(value, str) or not value:
        return None
    digest = hmac.new(salt.encode(), value.encode(), hashlib.sha256).hexdigest()
    return digest[:16]


def safe_enum(value: object, allowed: set[str]) -> str | None:
    return value if isinstance(value, str) and value in allowed else None


def safe_tool_name(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    if value in SAFE_TOOL_VALUES:
        return value
    if value.startswith("mcp__"):
        return "mcp"
    return "other"


def claude_home() -> Path:
    configured = os.environ.get("CLAUDE_CONFIG_DIR")
    return Path(configured) if configured else Path.home() / ".claude"


def redacted_path_shape(raw_path: object) -> dict[str, object]:
    if not isinstance(raw_path, str) or not raw_path:
        return {"present": False}

    path = Path(raw_path)
    result: dict[str, object] = {
        "present": True,
        "exists": path.is_file(),
        "suffix": path.suffix.lower(),
    }
    try:
        relative = path.resolve().relative_to(claude_home().resolve())
    except (OSError, ValueError):
        result["under_claude_home"] = False
        return result

    result["under_claude_home"] = True
    template_parts = []
    for index, part in enumerate(relative.parts):
        if index == len(relative.parts) - 1:
            template_parts.append(f"<redacted>{path.suffix.lower()}")
        elif part in PATH_TEMPLATE_WORDS:
            template_parts.append(part)
        else:
            template_parts.append("<redacted>")
    result["template"] = "/".join(template_parts)
    return result


def content_blocks(item: dict[str, Any]) -> list[dict[str, Any]]:
    message = item.get("message")
    if not isinstance(message, dict):
        return []
    content = message.get("content")
    if not isinstance(content, list):
        return []
    return [block for block in content if isinstance(block, dict)]


def bounded(values: set[str]) -> list[str]:
    return sorted(values)[:MAX_FINGERPRINTS]


def bytes_fingerprint(value: bytes, salt: str) -> str:
    return hmac.new(salt.encode(), value, hashlib.sha256).hexdigest()[:16]


def file_shape(path: Path, salt: str) -> dict[str, object]:
    """append-only 여부 판정용: 파일 동일성·크기·앞부분 지문만 남긴다."""
    stat = path.stat()
    with path.open("rb") as stream:
        prefix = stream.read(max(PREFIX_LENGTHS))
    return {
        "size_bytes": stat.st_size,
        "file_identity_fingerprint": fingerprint(f"{stat.st_dev}:{stat.st_ino}", salt),
        "prefix_fingerprints": {
            str(length): bytes_fingerprint(prefix[:length], salt)
            for length in PREFIX_LENGTHS
            if len(prefix) >= length
        },
        "starts_with_bom": prefix.startswith(b"\xef\xbb\xbf"),
        "ends_with_newline": stat.st_size == 0 or _last_byte(path) == b"\n",
    }


def _last_byte(path: Path) -> bytes:
    with path.open("rb") as stream:
        stream.seek(-1, 2)
        return stream.read(1)


def inspect_transcript(raw_path: object, salt: str) -> dict[str, object]:
    path_shape = redacted_path_shape(raw_path)
    if (
        not path_shape.get("exists")
        or not path_shape.get("under_claude_home")
        or not isinstance(raw_path, str)
    ):
        return path_shape

    top_level_keys: set[str] = set()
    unlisted_keys: set[str] = set()
    type_counts: Counter[str] = Counter()
    unlisted_type_counts: Counter[str] = Counter()
    block_counts: Counter[str] = Counter()
    tool_use_line_types: Counter[str] = Counter()
    tool_use_fingerprints: set[str] = set()
    tool_result_fingerprints: set[str] = set()
    agent_fingerprints: set[str] = set()
    session_fingerprints: set[str] = set()
    prompt_fingerprints: set[str] = set()
    uuid_lines = 0
    unique_uuids: set[str] = set()
    seen_message_ids: set[str] = set()
    repeated_message_id_lines = 0
    sidechain_lines = 0
    valid_lines = 0
    invalid_lines = 0
    bytes_read = 0
    truncated = False

    try:
        with Path(raw_path).open("rb") as transcript:
            for line_number, raw_line in enumerate(transcript, start=1):
                if line_number > MAX_TRANSCRIPT_LINES:
                    truncated = True
                    break
                bytes_read += len(raw_line)
                if bytes_read > MAX_TRANSCRIPT_BYTES:
                    truncated = True
                    break
                try:
                    item = json.loads(raw_line)
                except (UnicodeDecodeError, json.JSONDecodeError):
                    invalid_lines += 1
                    continue
                if not isinstance(item, dict):
                    invalid_lines += 1
                    continue
                valid_lines += 1
                top_level_keys.update(SAFE_TRANSCRIPT_KEYS.intersection(item))
                # 키 이름은 Claude Code 스키마 이름이다. 식별자 형태일 때만 이름을 남긴다.
                unlisted_keys.update(
                    key
                    for key in item
                    if key not in SAFE_TRANSCRIPT_KEYS
                    and TRANSCRIPT_KEY_PATTERN.fullmatch(key)
                )
                item_type = item.get("type")
                safe_type = safe_enum(item_type, SAFE_TRANSCRIPT_TYPES)
                type_counts[safe_type or "other"] += 1
                if (
                    safe_type is None
                    and isinstance(item_type, str)
                    and TYPE_NAME_PATTERN.fullmatch(item_type)
                ):
                    unlisted_type_counts[item_type] += 1
                line_type_name = safe_type or (
                    item_type
                    if isinstance(item_type, str)
                    and TYPE_NAME_PATTERN.fullmatch(item_type)
                    else "other"
                )
                if item.get("isSidechain") is True:
                    sidechain_lines += 1
                hashed_uuid = fingerprint(item.get("uuid"), salt)
                if hashed_uuid:
                    uuid_lines += 1
                    unique_uuids.add(hashed_uuid)
                message = item.get("message")
                hashed_message = fingerprint(
                    message.get("id") if isinstance(message, dict) else None, salt
                )
                if hashed_message:
                    if hashed_message in seen_message_ids:
                        repeated_message_id_lines += 1
                    seen_message_ids.add(hashed_message)
                for raw_value, bucket in (
                    (item.get("agentId"), agent_fingerprints),
                    (item.get("sessionId"), session_fingerprints),
                    (item.get("promptId"), prompt_fingerprints),
                ):
                    hashed = fingerprint(raw_value, salt)
                    if hashed:
                        bucket.add(hashed)
                for block in content_blocks(item):
                    block_type = safe_enum(block.get("type"), SAFE_CONTENT_BLOCK_TYPES)
                    block_counts[block_type or "other"] += 1
                    if block_type == "tool_use":
                        tool_use_line_types[line_type_name] += 1
                        hashed = fingerprint(block.get("id"), salt)
                        if hashed:
                            tool_use_fingerprints.add(hashed)
                    elif block_type == "tool_result":
                        hashed = fingerprint(block.get("tool_use_id"), salt)
                        if hashed:
                            tool_result_fingerprints.add(hashed)
        shape = file_shape(Path(raw_path), salt)
    except OSError:
        path_shape["read_error"] = True
        return path_shape

    path_shape.update(shape)
    path_shape.update(
        {
            "uuid_lines": uuid_lines,
            "unique_uuid_count": len(unique_uuids),
            "message_id_count": len(seen_message_ids),
            "repeated_message_id_lines": repeated_message_id_lines,
            "valid_json_lines": valid_lines,
            "invalid_json_lines": invalid_lines,
            "top_level_keys": sorted(top_level_keys),
            "unlisted_top_level_keys": sorted(unlisted_keys)[:MAX_FINGERPRINTS],
            "type_counts": dict(sorted(type_counts.items())),
            "unlisted_type_counts": dict(sorted(unlisted_type_counts.items())),
            "content_block_counts": dict(sorted(block_counts.items())),
            "tool_use_line_types": dict(sorted(tool_use_line_types.items())),
            "sidechain_lines": sidechain_lines,
            "agent_fingerprints": bounded(agent_fingerprints),
            "session_fingerprints": bounded(session_fingerprints),
            "prompt_fingerprints": bounded(prompt_fingerprints),
            "tool_use_fingerprints": bounded(tool_use_fingerprints),
            "tool_result_fingerprints": bounded(tool_result_fingerprints),
            "truncated": truncated,
        }
    )
    return path_shape


def sanitize(payload: dict[str, Any], raw_size: int, salt: str) -> dict[str, object]:
    event_name = (
        safe_enum(payload.get("hook_event_name"), KNOWN_HOOK_EVENTS) or "unknown"
    )
    present_fields = sorted(KNOWN_INPUT_FIELDS.intersection(payload))
    unlisted_fields = sorted(
        key
        for key in payload
        if key not in KNOWN_INPUT_FIELDS and FIELD_NAME_PATTERN.fullmatch(key)
    )
    malformed_field_names = sum(
        1
        for key in payload
        if key not in KNOWN_INPUT_FIELDS and not FIELD_NAME_PATTERN.fullmatch(key)
    )
    record: dict[str, object] = {
        "schema_version": "1.0",
        "probe_status": "captured",
        "hook_event_name": event_name,
        "present_fields": present_fields,
        "field_types": {field: value_type(payload[field]) for field in present_fields},
        "unlisted_fields": unlisted_fields,
        "malformed_field_name_count": malformed_field_names,
        "raw_size_bucket": min((raw_size // 256) * 256, MAX_INPUT_BYTES),
        "session_fingerprint": fingerprint(payload.get("session_id"), salt),
        "prompt_fingerprint": fingerprint(payload.get("prompt_id"), salt),
        "tool_use_fingerprint": fingerprint(payload.get("tool_use_id"), salt),
        "agent_fingerprint": fingerprint(payload.get("agent_id"), salt),
        "agent_type": safe_enum(payload.get("agent_type"), SAFE_AGENT_TYPES)
        or ("other" if "agent_type" in payload else None),
        "source": safe_enum(payload.get("source"), SAFE_SOURCE_VALUES),
        "reason": safe_enum(payload.get("reason"), SAFE_REASON_VALUES),
        "trigger": safe_enum(payload.get("trigger"), SAFE_TRIGGER_VALUES),
        "permission_mode": safe_enum(
            payload.get("permission_mode"), SAFE_PERMISSION_MODES
        ),
        "notification_type": safe_enum(
            payload.get("notification_type"), SAFE_NOTIFICATION_TYPES
        ),
        "tool_name": safe_tool_name(payload.get("tool_name")),
        "transcript": inspect_transcript(payload.get("transcript_path"), salt),
    }
    if "agent_transcript_path" in payload:
        record["agent_transcript"] = inspect_transcript(
            payload.get("agent_transcript_path"), salt
        )
    return record


def append_record(record: dict[str, object]) -> None:
    output = Path.cwd() / ".agent-office" / "poc" / "claude-hook-events.jsonl"
    output.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps(record, sort_keys=True)
    with output.open("a", encoding="utf-8", newline="\n") as stream:
        stream.write(f"{line}\n")


def append_status(status: str) -> None:
    try:
        append_record(
            {
                "schema_version": "1.0",
                "probe_status": status,
                "hook_event_name": "unknown",
            }
        )
    except OSError:
        pass


def main() -> int:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--run-salt")
    arguments, _ = parser.parse_known_args()
    run_salt = arguments.run_salt or os.environ.get("AGENT_OFFICE_POC_RUN_SALT")
    if not run_salt:
        return 0

    raw = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
    if len(raw) > MAX_INPUT_BYTES:
        append_status("input_too_large")
        return 0
    try:
        payload = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError):
        append_status("invalid_json")
        return 0
    if not isinstance(payload, dict):
        return 0

    # stdout은 SessionStart·UserPromptSubmit에서 모델 컨텍스트로 들어가므로 쓰지 않는다.
    try:
        append_record(sanitize(payload, len(raw), run_salt))
    except Exception:  # noqa: BLE001 - Hook은 어떤 오류에도 fail-open한다.
        return 0
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

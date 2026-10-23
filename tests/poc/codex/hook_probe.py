"""Codex Hook 입력을 원문 없이 구조 통계로 축약하는 PoC 프로브."""

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
MAX_TRANSCRIPT_BYTES = 1_048_576
MAX_TRANSCRIPT_LINES = 2_000

KNOWN_HOOK_EVENTS = {
    "Interrupt",
    "PermissionRequest",
    "PostCompact",
    "PostToolUse",
    "PreCompact",
    "PreToolUse",
    "SessionEnd",
    "SessionStart",
    "Stop",
    "SubagentStart",
    "SubagentStop",
    "UserPromptSubmit",
}
SAFE_INPUT_FIELDS = {
    "agent_id",
    "agent_type",
    "cwd",
    "hook_event_name",
    "model",
    "permission_mode",
    "prompt",
    "reason",
    "session_id",
    "source",
    "tool_input",
    "tool_name",
    "tool_response",
    "tool_use_id",
    "transcript_path",
    "trigger",
    "turn_id",
}
SAFE_SOURCE_VALUES = {"startup", "resume", "clear", "compact", "fork"}
SAFE_REASON_VALUES = {"other"}
SAFE_TRIGGER_VALUES = {"manual", "auto"}
SAFE_TOOL_VALUES = {"Bash", "apply_patch"}
SAFE_TRANSCRIPT_TYPES = {
    "event_msg",
    "response_item",
    "session_meta",
    "turn_context",
}
SAFE_TRANSCRIPT_KEYS = {"timestamp", "type", "payload"}


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
        relative = path.resolve().relative_to((Path.home() / ".codex").resolve())
    except (OSError, ValueError):
        result["under_codex_home"] = False
        return result

    result["under_codex_home"] = True
    template_parts = []
    for index, part in enumerate(relative.parts):
        if index == len(relative.parts) - 1:
            template_parts.append(f"<redacted>{path.suffix.lower()}")
        elif part.isdigit():
            template_parts.append("<number>")
        elif re.fullmatch(r"[A-Za-z0-9_-]{1,32}", part):
            template_parts.append(part)
        else:
            template_parts.append("<redacted>")
    result["template"] = "/".join(template_parts)
    return result


def inspect_transcript(raw_path: object) -> dict[str, object]:
    path_shape = redacted_path_shape(raw_path)
    if (
        not path_shape.get("exists")
        or not path_shape.get("under_codex_home")
        or not isinstance(raw_path, str)
    ):
        return path_shape

    top_level_keys: set[str] = set()
    type_counts: Counter[str] = Counter()
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
                item_type = item.get("type")
                safe_type = (
                    item_type
                    if isinstance(item_type, str) and item_type in SAFE_TRANSCRIPT_TYPES
                    else "other"
                )
                type_counts[safe_type] += 1
    except OSError:
        path_shape["read_error"] = True
        return path_shape

    path_shape.update(
        {
            "valid_json_lines": valid_lines,
            "invalid_json_lines": invalid_lines,
            "top_level_keys": sorted(top_level_keys),
            "type_counts": dict(sorted(type_counts.items())),
            "truncated": truncated,
        }
    )
    return path_shape


def sanitize(payload: dict[str, Any], raw_size: int, salt: str) -> dict[str, object]:
    event_name = (
        safe_enum(payload.get("hook_event_name"), KNOWN_HOOK_EVENTS) or "unknown"
    )
    present_fields = sorted(SAFE_INPUT_FIELDS.intersection(payload))
    record: dict[str, object] = {
        "schema_version": "1.0",
        "probe_status": "captured",
        "hook_event_name": event_name,
        "present_fields": present_fields,
        "field_types": {field: value_type(payload[field]) for field in present_fields},
        "raw_size_bucket": min((raw_size // 256) * 256, MAX_INPUT_BYTES),
        "session_fingerprint": fingerprint(payload.get("session_id"), salt),
        "turn_fingerprint": fingerprint(payload.get("turn_id"), salt),
        "tool_use_fingerprint": fingerprint(payload.get("tool_use_id"), salt),
        "source": safe_enum(payload.get("source"), SAFE_SOURCE_VALUES),
        "reason": safe_enum(payload.get("reason"), SAFE_REASON_VALUES),
        "trigger": safe_enum(payload.get("trigger"), SAFE_TRIGGER_VALUES),
        "tool_name": safe_enum(payload.get("tool_name"), SAFE_TOOL_VALUES)
        or ("other" if "tool_name" in payload else None),
        "transcript": inspect_transcript(payload.get("transcript_path")),
    }
    return record


def append_record(record: dict[str, object]) -> None:
    output = Path.cwd() / ".agent-office" / "poc" / "codex-hook-events.jsonl"
    output.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps(record, sort_keys=True)
    with output.open("a", encoding="utf-8", newline="\n") as stream:
        stream.write(f"{line}\n")


def main() -> int:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--run-salt")
    arguments, _ = parser.parse_known_args()
    run_salt = arguments.run_salt or os.environ.get("AGENT_OFFICE_POC_RUN_SALT")
    if not run_salt:
        return 0

    raw = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
    if len(raw) > MAX_INPUT_BYTES:
        try:
            append_record(
                {
                    "schema_version": "1.0",
                    "probe_status": "input_too_large",
                    "hook_event_name": "unknown",
                }
            )
        except OSError:
            pass
        return 0
    try:
        payload = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError):
        try:
            append_record(
                {
                    "schema_version": "1.0",
                    "probe_status": "invalid_json",
                    "hook_event_name": "unknown",
                }
            )
        except OSError:
            pass
        return 0
    if not isinstance(payload, dict):
        return 0

    try:
        append_record(sanitize(payload, len(raw), run_salt))
    except OSError:
        return 0
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""격리된 합성 작업공간에서 Claude Code Hook/JSONL PoC를 실행한다."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import secrets
import shlex
import subprocess
import sys
import tempfile
import time
from collections import Counter
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent))

from hook_probe import (
    FIELD_NAME_PATTERN,
    KNOWN_HOOK_EVENTS,
    claude_home,
    fingerprint,
    safe_enum,
    safe_tool_name,
)

CORE_HOOK_EVENTS = (
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PermissionRequest",
    "PostToolUse",
    "PostToolUseFailure",
    "Notification",
    "SubagentStart",
    "SubagentStop",
    "PreCompact",
    "Stop",
    "SessionEnd",
)
# 설치 버전이 모르는 이벤트 이름이 있으면 print 모드는 설정 파일 전체를 조용히 무시한다.
# 그래서 확인되지 않은 이벤트는 별도 실행에서만 켠다.
EXTRA_HOOK_EVENTS = ("PermissionDenied", "StopFailure", "PostCompact")
MATCHER_EVENTS = {
    "PermissionDenied",
    "PermissionRequest",
    "PostToolUse",
    "PostToolUseFailure",
    "PreToolUse",
}
SAFE_STREAM_TYPES = {"assistant", "result", "stream_event", "system", "user"}
SAFE_STREAM_SUBTYPES = {
    "api_retry",
    "compact_boundary",
    "error_during_execution",
    "error_max_budget_usd",
    "error_max_turns",
    "hook_progress",
    "hook_response",
    "hook_started",
    "init",
    "status",
    "success",
    "task_notification",
    "task_progress",
    "task_started",
}
STDERR_CATEGORY_TERMS = {
    "error": ("error", "failed"),
    "hook": ("hook",),
    "permission": ("permission", "denied"),
    "settings": ("setting",),
    "warning": ("warn",),
}
SYNTHETIC_FILE_TEXT = "synthetic agent office probe file\n"
SUBAGENT_DEFINITION = {
    "poc-reader": {
        "description": "Synthetic probe agent that reads notes.txt once.",
        "prompt": (
            "You are a synthetic probe. Use the Read tool exactly once on "
            "notes.txt in the current directory, then reply exactly DONE."
        ),
        "tools": ["Read"],
    }
}
SCENARIOS: dict[str, dict[str, Any]] = {
    "basic": {
        "prompt": (
            "This is a synthetic lifecycle probe. Use the Read tool exactly once on "
            "notes.txt, then use the Bash tool exactly once to run git status "
            "--short, then reply exactly POC_OK."
        ),
        "tools": "Read,Bash",
        "allowed": ["Read", "Bash(git status:*)"],
        "agents": False,
    },
    "subagents": {
        "prompt": (
            "This is a synthetic lifecycle probe. In a single message, launch two "
            "poc-reader subagents in parallel with the Agent tool, each with the "
            "task 'read notes.txt'. Wait for both, then reply exactly POC_OK."
        ),
        "tools": "Agent,Read",
        "allowed": ["Agent", "Read"],
        "agents": True,
    },
    "denied": {
        "prompt": (
            "This is a synthetic permission probe. Use the Write tool exactly once "
            "to create denied.txt containing the word synthetic. If the tool is "
            "denied, do not retry and reply exactly POC_DENIED."
        ),
        "tools": "Write",
        "allowed": [],
        "agents": False,
    },
}


HOOK_FAULTS = {
    "none": "",
    # 기록 후 Hook 타임아웃(2초)을 넘겨 강제 종료되게 한다.
    "timeout": "; sleep 10",
    # 기록 후 차단 의미가 없는 비정상 종료 코드(1)를 낸다. 2는 차단이므로 쓰지 않는다.
    "error": "; exit 1",
}


def hook_settings(
    probe: Path, run_salt: str, events: tuple[str, ...], fault: str = "none"
) -> dict[str, Any]:
    command = shlex.join([sys.executable, str(probe), "--run-salt", run_salt])
    command += HOOK_FAULTS[fault]
    timeout = 2 if fault == "timeout" else 5
    handler = {"type": "command", "command": command, "timeout": timeout}
    hooks: dict[str, list[dict[str, Any]]] = {}
    for event in events:
        entry: dict[str, Any] = {"hooks": [handler]}
        if event in MATCHER_EVENTS:
            entry["matcher"] = "*"
        hooks[event] = [entry]
    return {"hooks": hooks}


def child_environment() -> dict[str, str]:
    # 이 PoC를 실행하는 Claude 세션의 내부 변수가 자식 세션에 섞이지 않게 한다.
    return {
        key: value
        for key, value in os.environ.items()
        if not (key.startswith("CLAUDE") and key != "CLAUDE_CONFIG_DIR")
    }


def sanitize_stream_jsonl(stdout: str | None, salt: str) -> dict[str, object]:
    type_counts: Counter[str] = Counter()
    subtype_counts: Counter[str] = Counter()
    hook_event_counts: Counter[str] = Counter()
    tool_counts: Counter[str] = Counter()
    top_level_keys: set[str] = set()
    tool_use_fingerprints: set[str] = set()
    session_fingerprints: set[str] = set()
    parent_tool_use_lines = 0
    valid_lines = 0
    invalid_lines = 0
    result: dict[str, object] = {}

    for line in (stdout or "").splitlines():
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            invalid_lines += 1
            continue
        if not isinstance(item, dict):
            invalid_lines += 1
            continue
        valid_lines += 1
        top_level_keys.update(key for key in item if FIELD_NAME_PATTERN.fullmatch(key))
        item_type = safe_enum(item.get("type"), SAFE_STREAM_TYPES) or "other"
        type_counts[item_type] += 1
        if "subtype" in item:
            subtype_counts[
                safe_enum(item.get("subtype"), SAFE_STREAM_SUBTYPES) or "other"
            ] += 1
        # hook_started와 hook_response가 같은 Hook을 두 번 알리므로 시작만 센다.
        if item.get("subtype") == "hook_started":
            hook_event_counts[
                safe_enum(item.get("hook_event"), KNOWN_HOOK_EVENTS) or "other"
            ] += 1
        if item.get("parent_tool_use_id"):
            parent_tool_use_lines += 1
        hashed_session = fingerprint(item.get("session_id"), salt)
        if hashed_session:
            session_fingerprints.add(hashed_session)
        message = item.get("message")
        content = message.get("content") if isinstance(message, dict) else None
        if item_type == "assistant" and isinstance(content, list):
            for block in content:
                if isinstance(block, dict) and block.get("type") == "tool_use":
                    tool_counts[safe_tool_name(block.get("name")) or "other"] += 1
                    hashed = fingerprint(block.get("id"), salt)
                    if hashed:
                        tool_use_fingerprints.add(hashed)
        if item_type == "result":
            result = {
                "subtype": safe_enum(item.get("subtype"), SAFE_STREAM_SUBTYPES)
                or "other",
                "is_error": item.get("is_error")
                if isinstance(item.get("is_error"), bool)
                else None,
                "num_turns": item.get("num_turns")
                if isinstance(item.get("num_turns"), int)
                else None,
                "total_cost_usd": item.get("total_cost_usd")
                if isinstance(item.get("total_cost_usd"), int | float)
                else None,
                "permission_denial_count": len(item["permission_denials"])
                if isinstance(item.get("permission_denials"), list)
                else None,
            }

    return {
        "valid_json_lines": valid_lines,
        "invalid_json_lines": invalid_lines,
        "top_level_keys": sorted(top_level_keys),
        "type_counts": dict(sorted(type_counts.items())),
        "subtype_counts": dict(sorted(subtype_counts.items())),
        "hook_event_counts": dict(sorted(hook_event_counts.items())),
        "tool_use_counts": dict(sorted(tool_counts.items())),
        "tool_use_fingerprints": sorted(tool_use_fingerprints),
        "session_fingerprints": sorted(session_fingerprints),
        "parent_tool_use_lines": parent_tool_use_lines,
        "result": result,
    }


def load_hook_records(workspace: Path) -> list[dict[str, Any]]:
    path = workspace / ".agent-office" / "poc" / "claude-hook-events.jsonl"
    if not path.is_file():
        return []
    records = []
    for line in path.read_text(encoding="utf-8").splitlines():
        item = json.loads(line)
        if isinstance(item, dict):
            records.append(item)
    return records


def summarize_hooks(
    records: list[dict[str, Any]], events: tuple[str, ...]
) -> dict[str, object]:
    event_counts = Counter(
        str(record.get("hook_event_name", "unknown")) for record in records
    )
    session_fingerprints = {
        record["session_fingerprint"]
        for record in records
        if isinstance(record.get("session_fingerprint"), str)
    }
    prompt_fingerprints = {
        record["prompt_fingerprint"]
        for record in records
        if isinstance(record.get("prompt_fingerprint"), str)
    }
    agent_fingerprints = {
        record["agent_fingerprint"]
        for record in records
        if isinstance(record.get("agent_fingerprint"), str)
    }
    tool_use_by_event: dict[str, set[str]] = {}
    for record in records:
        hashed = record.get("tool_use_fingerprint")
        if isinstance(hashed, str):
            tool_use_by_event.setdefault(str(record["hook_event_name"]), set()).add(
                hashed
            )
    return {
        "record_count": len(records),
        "configured_events": list(events),
        "event_counts": dict(sorted(event_counts.items())),
        "missing_events": sorted(set(events).difference(event_counts)),
        "unique_session_fingerprints": len(session_fingerprints),
        "unique_prompt_fingerprints": len(prompt_fingerprints),
        "unique_agent_fingerprints": len(agent_fingerprints),
        "tool_use_fingerprints_by_event": {
            event: sorted(values) for event, values in sorted(tool_use_by_event.items())
        },
        "records": records,
    }


def stderr_categories(stderr: str | None) -> dict[str, int]:
    counts: Counter[str] = Counter()
    for line in (stderr or "").splitlines():
        lowered = line.lower()
        matched = False
        for category, terms in STDERR_CATEGORY_TERMS.items():
            if any(term in lowered for term in terms):
                counts[category] += 1
                matched = True
        if not matched:
            counts["other"] += 1
    return dict(sorted(counts.items()))


def file_digest(path: Path) -> str | None:
    try:
        return hashlib.sha256(path.read_bytes()).hexdigest()
    except OSError:
        return None


def claude_version(claude_executable: str) -> str | None:
    result = subprocess.run(
        [claude_executable, "--version"],
        env=child_environment(),
        capture_output=True,
        text=True,
        timeout=30,
        check=False,
    )
    match = re.search(r"\d+\.\d+\.\d+", result.stdout)
    return match.group(0) if match else None


def run_probe(
    claude_executable: str,
    scenario_name: str,
    model: str,
    extra_events: bool,
    fault: str,
) -> int:
    scenario = SCENARIOS[scenario_name]
    events = CORE_HOOK_EVENTS + (EXTRA_HOOK_EVENTS if extra_events else ())
    probe = Path(__file__).with_name("hook_probe.py").resolve()
    user_settings = claude_home() / "settings.json"
    settings_before = file_digest(user_settings)

    with tempfile.TemporaryDirectory(prefix="agent-office-claude-poc-") as temp:
        workspace = Path(temp) / "workspace"
        workspace.mkdir()
        subprocess.run(
            ["git", "init", "--quiet"],
            cwd=workspace,
            check=True,
            capture_output=True,
            text=True,
        )
        (workspace / "notes.txt").write_text(SYNTHETIC_FILE_TEXT, encoding="utf-8")
        run_salt = secrets.token_hex(32)
        # 설정 파일은 작업공간 밖에 두어 project 설정 탐색과 섞이지 않게 한다.
        settings_path = Path(temp) / "probe-settings.json"
        settings_path.write_text(
            json.dumps(hook_settings(probe, run_salt, events, fault)), encoding="utf-8"
        )
        command = [
            claude_executable,
            "-p",
            "--output-format",
            "stream-json",
            "--verbose",
            "--include-hook-events",
            "--setting-sources",
            "project",
            "--settings",
            str(settings_path),
            "--strict-mcp-config",
            "--disable-slash-commands",
            "--model",
            model,
            "--permission-mode",
            "default",
            "--permission-prompts",
            "none",
            "--max-budget-usd",
            "1",
            "--tools",
            scenario["tools"],
        ]
        if scenario["allowed"]:
            command.extend(["--allowedTools", *scenario["allowed"]])
        if scenario["agents"]:
            command.extend(["--agents", json.dumps(SUBAGENT_DEFINITION)])
        command.extend(["--", scenario["prompt"]])
        started = time.monotonic()
        result = subprocess.run(
            command,
            cwd=workspace,
            env=child_environment(),
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=300,
            check=False,
        )
        summary = {
            "poc_schema_version": "1.0",
            "scenario": scenario_name,
            "model_alias": model,
            "claude_version": claude_version(claude_executable),
            "hook_fault": fault,
            "elapsed_seconds": round(time.monotonic() - started, 1),
            "claude_exit_code": result.returncode,
            "stderr_line_count": len((result.stderr or "").splitlines()),
            "stderr_categories": stderr_categories(result.stderr),
            "user_settings_unchanged": file_digest(user_settings) == settings_before,
            "workspace_file_created": (workspace / "denied.txt").exists(),
            "stream_jsonl": sanitize_stream_jsonl(result.stdout, run_salt),
            "hooks": summarize_hooks(load_hook_records(workspace), events),
        }
        print(json.dumps(summary, indent=2, sort_keys=True))
        return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--claude", default="claude", help="Claude Code executable")
    parser.add_argument("--scenario", choices=sorted(SCENARIOS), default="basic")
    parser.add_argument("--model", default="sonnet")
    parser.add_argument(
        "--extra-events",
        action="store_true",
        help="Also register hook events not yet confirmed for this version.",
    )
    parser.add_argument("--hook-fault", choices=sorted(HOOK_FAULTS), default="none")
    parser.add_argument(
        "--confirm-synthetic-run",
        action="store_true",
        help="Run the external synthetic Claude Code session.",
    )
    arguments = parser.parse_args()
    if not arguments.confirm_synthetic_run:
        parser.error("--confirm-synthetic-run is required")
    return run_probe(
        arguments.claude,
        arguments.scenario,
        arguments.model,
        arguments.extra_events,
        arguments.hook_fault,
    )


if __name__ == "__main__":
    raise SystemExit(main())

"""격리된 합성 작업공간에서 Codex CLI Hook/JSONL PoC를 실행한다."""

from __future__ import annotations

import argparse
import json
import os
import secrets
import shlex
import subprocess
import sys
import tempfile
from collections import Counter
from pathlib import Path
from typing import Any

HOOK_EVENTS = (
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "Stop",
    "SessionEnd",
)
SAFE_EXEC_EVENT_TYPES = {
    "agent_message",
    "command_execution",
    "error",
    "item.completed",
    "item.started",
    "mcp_tool_call",
    "reasoning",
    "thread.started",
    "turn.completed",
    "turn.failed",
    "turn.started",
}
SAFE_EXEC_KEYS = {"type", "thread_id", "turn_id", "item", "usage"}
SAFE_EXEC_ITEM_TYPES = {
    "agent_message",
    "command_execution",
    "file_change",
    "mcp_tool_call",
    "reasoning",
    "todo_list",
    "web_search",
}
STDERR_CATEGORY_TERMS = {
    "config": ("config",),
    "error": ("error", "failed"),
    "hook": ("hook",),
    "trust": ("trust",),
    "warning": ("warn",),
}
SYNTHETIC_PROMPT = (
    "This is a synthetic lifecycle probe. Use the shell tool exactly once to run "
    "git status --short, do not inspect file contents, then reply exactly POC_OK."
)


def toml_string(value: str) -> str:
    return json.dumps(value)


def hook_config_overrides(probe: Path, run_salt: str) -> list[str]:
    arguments = [sys.executable, str(probe), "--run-salt", run_salt]
    posix_command = shlex.join(arguments)
    windows_command = subprocess.list2cmdline(arguments)
    handler = (
        '{ type = "command", '
        f"command = {toml_string(posix_command)}, "
        f"command_windows = {toml_string(windows_command)}, "
        "timeout = 3 }"
    )
    overrides = []
    for event in HOOK_EVENTS:
        matcher = ""
        if event in {"PreToolUse", "PostToolUse"}:
            matcher = 'matcher = "^Bash$", '
        overrides.append(f"hooks.{event}=[{{ {matcher}hooks = [{handler}] }}]")
    return overrides


def sanitize_exec_jsonl(stdout: str | None) -> dict[str, object]:
    top_level_keys: set[str] = set()
    event_counts: Counter[str] = Counter()
    item_type_counts: Counter[str] = Counter()
    valid_lines = 0
    invalid_lines = 0

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
        top_level_keys.update(SAFE_EXEC_KEYS.intersection(item))
        event_type = item.get("type")
        event_counts[
            event_type if event_type in SAFE_EXEC_EVENT_TYPES else "other"
        ] += 1
        nested_item = item.get("item")
        if isinstance(nested_item, dict):
            nested_type = nested_item.get("type")
            item_type_counts[
                nested_type
                if isinstance(nested_type, str) and nested_type in SAFE_EXEC_ITEM_TYPES
                else "other"
            ] += 1

    return {
        "valid_json_lines": valid_lines,
        "invalid_json_lines": invalid_lines,
        "top_level_keys": sorted(top_level_keys),
        "event_type_counts": dict(sorted(event_counts.items())),
        "item_type_counts": dict(sorted(item_type_counts.items())),
    }


def load_hook_records(workspace: Path) -> list[dict[str, Any]]:
    path = workspace / ".agent-office" / "poc" / "codex-hook-events.jsonl"
    if not path.is_file():
        return []
    records = []
    for line in path.read_text(encoding="utf-8").splitlines():
        item = json.loads(line)
        if isinstance(item, dict):
            records.append(item)
    return records


def summarize_hooks(records: list[dict[str, Any]]) -> dict[str, object]:
    event_counts = Counter(
        str(record.get("hook_event_name", "unknown")) for record in records
    )
    session_fingerprints = {
        record["session_fingerprint"]
        for record in records
        if isinstance(record.get("session_fingerprint"), str)
    }
    return {
        "record_count": len(records),
        "configured_events": list(HOOK_EVENTS),
        "event_counts": dict(sorted(event_counts.items())),
        "missing_events": sorted(set(HOOK_EVENTS).difference(event_counts)),
        "duplicate_event_counts": {
            event: count for event, count in sorted(event_counts.items()) if count > 1
        },
        "unique_session_fingerprints": len(session_fingerprints),
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


def run_probe(codex_executable: str) -> int:
    probe = Path(__file__).with_name("hook_probe.py").resolve()
    with tempfile.TemporaryDirectory(prefix="agent-office-codex-poc-") as temp:
        workspace = Path(temp)
        subprocess.run(
            ["git", "init", "--quiet"],
            cwd=workspace,
            check=True,
            capture_output=True,
            text=True,
        )
        environment = os.environ.copy()
        run_salt = secrets.token_hex(32)
        command = [
            codex_executable,
            "--dangerously-bypass-hook-trust",
            "--no-daemon",
            "--sandbox",
            "workspace-write",
            "--ask-for-approval",
            "never",
            "--enable",
            "hooks",
        ]
        for override in hook_config_overrides(probe, run_salt):
            command.extend(["-c", override])
        command.extend(
            [
                "-C",
                str(workspace),
                "exec",
                "--ignore-user-config",
                "--json",
                SYNTHETIC_PROMPT,
            ]
        )
        result = subprocess.run(
            command,
            env=environment,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=180,
            check=False,
        )
        summary = {
            "poc_schema_version": "1.0",
            "codex_exit_code": result.returncode,
            "stderr_line_count": len((result.stderr or "").splitlines()),
            "stderr_categories": stderr_categories(result.stderr),
            "exec_jsonl": sanitize_exec_jsonl(result.stdout),
            "hooks": summarize_hooks(load_hook_records(workspace)),
        }
        print(json.dumps(summary, indent=2, sort_keys=True))
        return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", default="codex", help="Codex CLI executable")
    parser.add_argument(
        "--confirm-synthetic-run",
        action="store_true",
        help="Run the external synthetic Codex session.",
    )
    arguments = parser.parse_args()
    if not arguments.confirm_synthetic_run:
        parser.error("--confirm-synthetic-run is required")
    return run_probe(arguments.codex)


if __name__ == "__main__":
    raise SystemExit(main())

"""수동 Codex Desktop 합성 세션용 임시 작업공간을 준비·요약·정리한다.

Desktop UI는 자동화하지 않는다. 운영자가 직접 작업공간을 열고 프로젝트와
Hook을 신뢰한 뒤 합성 시나리오를 수행한다. 이 스크립트는 Hook 정의 생성,
단계 표시, 구조 통계 요약, 소유 항목만 되돌리는 정리를 담당한다.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import secrets
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

HOOK_EVENTS = (
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PermissionRequest",
    "PostToolUse",
    "SubagentStart",
    "SubagentStop",
    "PreCompact",
    "PostCompact",
    "Interrupt",
    "Stop",
    "SessionEnd",
)
OWNER_MARKER = "agent-office-desktop-poc"
WORKSPACE_PREFIX = "agent-office-desktop-poc-"
OWNED_SECTION_PREFIXES = ("[projects.", "[hooks.state.")
LABEL_PATTERN = re.compile(r"[a-z0-9-]{1,32}")
SYNTHETIC_FILE_TEXT = "Synthetic Agent Office fixture. No user data.\n"


def codex_config_path() -> Path:
    return Path.home() / ".codex" / "config.toml"


def state_dir_for(workspace: Path) -> Path:
    return workspace.with_name(f"{workspace.name}-state")


def sha256_of(path: Path) -> str | None:
    if not path.is_file():
        return None
    return hashlib.sha256(path.read_bytes()).hexdigest()


def section_headers(text: str) -> list[str]:
    return [line.strip() for line in text.splitlines() if line.strip().startswith("[")]


def hook_definitions(probe: Path, salt: str, output: Path) -> dict[str, Any]:
    arguments = [
        sys.executable,
        str(probe),
        "--run-salt",
        salt,
        "--output",
        str(output),
    ]
    handler = {
        "type": "command",
        "command": shlex.join(arguments),
        "commandWindows": subprocess.list2cmdline(arguments),
        "timeout": 3,
    }
    return {"hooks": {event: [{"hooks": [handler]}] for event in HOOK_EVENTS}}


def write_atomic(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{secrets.token_hex(4)}.tmp")
    with temporary.open("w", encoding="utf-8", newline="\n") as stream:
        stream.write(content)
    os.replace(temporary, path)


def prepare() -> int:
    probe = Path(__file__).with_name("hook_probe.py").resolve()
    workspace = Path(tempfile.mkdtemp(prefix=WORKSPACE_PREFIX)).resolve()
    state = state_dir_for(workspace)
    state.mkdir()
    subprocess.run(
        ["git", "init", "--quiet"],
        cwd=workspace,
        check=True,
        capture_output=True,
        text=True,
    )
    (workspace / "SYNTHETIC.txt").write_text(SYNTHETIC_FILE_TEXT, encoding="utf-8")

    config = codex_config_path()
    config_backup = state / "config.toml.bak"
    if config.is_file():
        shutil.copy2(config, config_backup)

    salt = secrets.token_hex(32)
    records = state / "codex-hook-events.jsonl"
    hooks = hook_definitions(probe, salt, records)
    hooks_json = json.dumps(hooks, indent=2) + "\n"
    write_atomic(workspace / ".codex" / "hooks.json", hooks_json)

    manifest = {
        "owner": OWNER_MARKER,
        "workspace": str(workspace),
        "records": str(records),
        "config_existed": config.is_file(),
        "config_sha256": sha256_of(config),
        "config_backup": str(config_backup) if config.is_file() else None,
    }
    write_atomic(state / "manifest.json", json.dumps(manifest, indent=2) + "\n")

    print(f"workspace: {workspace}")
    print(f"state: {state}")
    print("hooks.json:")
    print(hooks_json, end="")
    return 0


def load_manifest(workspace: Path) -> dict[str, Any]:
    if not workspace.name.startswith(WORKSPACE_PREFIX):
        raise SystemExit("workspace is not an Agent Office Desktop PoC workspace")
    manifest_path = state_dir_for(workspace) / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("owner") != OWNER_MARKER:
        raise SystemExit("manifest owner marker mismatch")
    if Path(manifest["workspace"]) != workspace:
        raise SystemExit("manifest workspace mismatch")
    return dict(manifest)


def load_records(path: Path) -> list[dict[str, Any]]:
    if not path.is_file():
        return []
    records = []
    for line in path.read_text(encoding="utf-8").splitlines():
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            records.append({"probe_status": "unreadable_record"})
            continue
        if isinstance(item, dict):
            records.append(item)
    return records


def mark(workspace: Path, label: str) -> int:
    if not LABEL_PATTERN.fullmatch(label):
        raise SystemExit("label must match [a-z0-9-]{1,32}")
    manifest = load_manifest(workspace)
    count = len(load_records(Path(manifest["records"])))
    markers = state_dir_for(workspace) / "markers.jsonl"
    with markers.open("a", encoding="utf-8", newline="\n") as stream:
        stream.write(json.dumps({"label": label, "record_count": count}) + "\n")
    print(json.dumps({"label": label, "record_count": count}))
    return 0


def alias_map(values: list[str | None], prefix: str) -> dict[str, str]:
    aliases: dict[str, str] = {}
    for value in values:
        if value is not None and value not in aliases:
            aliases[value] = f"{prefix}{len(aliases) + 1}"
    return aliases


def segment_records(
    records: list[dict[str, Any]], markers: list[dict[str, Any]]
) -> list[tuple[str, list[dict[str, Any]]]]:
    segments = []
    start = 0
    for marker in markers:
        end = int(marker["record_count"])
        segments.append((str(marker["label"]), records[start:end]))
        start = end
    if start < len(records):
        segments.append(("unmarked-tail", records[start:]))
    return segments


def summarize_records(
    records: list[dict[str, Any]], markers: list[dict[str, Any]]
) -> dict[str, object]:
    sessions = alias_map([r.get("session_fingerprint") for r in records], "S")
    turns = alias_map([r.get("turn_fingerprint") for r in records], "T")
    agents = alias_map([r.get("agent_fingerprint") for r in records], "A")

    def event_line(record: dict[str, Any]) -> str:
        parts = [str(record.get("hook_event_name", "unknown"))]
        session = sessions.get(record.get("session_fingerprint") or "")
        turn = turns.get(record.get("turn_fingerprint") or "")
        agent = agents.get(record.get("agent_fingerprint") or "")
        parts.extend(part for part in (session, turn, agent) if part)
        if record.get("tool_name"):
            parts.append(f"tool={record['tool_name']}")
        return " ".join(parts)

    fields_by_event: dict[str, set[str]] = defaultdict(set)
    for record in records:
        event = str(record.get("hook_event_name", "unknown"))
        fields_by_event[event].update(record.get("present_fields", []))

    tool_uses_pre = {
        r["tool_use_fingerprint"]
        for r in records
        if r.get("hook_event_name") == "PreToolUse" and r.get("tool_use_fingerprint")
    }
    tool_uses_post = {
        r["tool_use_fingerprint"]
        for r in records
        if r.get("hook_event_name") == "PostToolUse" and r.get("tool_use_fingerprint")
    }
    agents_started = {
        r["agent_fingerprint"]
        for r in records
        if r.get("hook_event_name") == "SubagentStart" and r.get("agent_fingerprint")
    }
    agents_stopped = {
        r["agent_fingerprint"]
        for r in records
        if r.get("hook_event_name") == "SubagentStop" and r.get("agent_fingerprint")
    }
    session_values = set(sessions)

    def counts(key: str) -> dict[str, int]:
        return dict(
            sorted(
                Counter(
                    f"{r.get('hook_event_name')}:{r[key]}"
                    for r in records
                    if r.get(key) is not None
                ).items()
            )
        )

    transcript_templates = Counter(
        str(r.get("transcript", {}).get("template", "<none>")) for r in records
    )
    agent_transcript_templates = Counter(
        str(r["agent_transcript"].get("template", "<none>"))
        for r in records
        if isinstance(r.get("agent_transcript"), dict)
    )
    return {
        "record_count": len(records),
        "probe_status_counts": dict(
            sorted(Counter(str(r.get("probe_status")) for r in records).items())
        ),
        "configured_events": list(HOOK_EVENTS),
        "event_counts": dict(
            sorted(Counter(str(r.get("hook_event_name")) for r in records).items())
        ),
        "missing_events": sorted(
            set(HOOK_EVENTS).difference(r.get("hook_event_name") for r in records)
        ),
        "present_fields_by_event": {
            event: sorted(fields) for event, fields in sorted(fields_by_event.items())
        },
        "segments": [
            {"label": label, "events": [event_line(r) for r in segment]}
            for label, segment in segment_records(records, markers)
        ],
        "correlation": {
            "unique_sessions": len(sessions),
            "unique_turns": len(turns),
            "unique_agents": len(agents),
            "pre_tool_uses": len(tool_uses_pre),
            "post_tool_uses": len(tool_uses_post),
            "pre_post_tool_use_matches": len(tool_uses_pre & tool_uses_post),
            "subagent_start_stop_matches": len(agents_started & agents_stopped),
            "agent_ids_equal_to_session_ids": len(set(agents) & session_values),
        },
        "tool_name_counts": counts("tool_name"),
        "agent_type_counts": counts("agent_type"),
        "permission_mode_counts": counts("permission_mode"),
        "source_counts": counts("source"),
        "reason_counts": counts("reason"),
        "transcript_templates": dict(sorted(transcript_templates.items())),
        "agent_transcript_templates": dict(sorted(agent_transcript_templates.items())),
    }


def summarize(workspace: Path) -> int:
    manifest = load_manifest(workspace)
    records = load_records(Path(manifest["records"]))
    markers_path = state_dir_for(workspace) / "markers.jsonl"
    markers = load_records(markers_path)
    config = codex_config_path()
    summary = summarize_records(records, markers)
    summary["config_changed_since_prepare"] = (
        sha256_of(config) != manifest["config_sha256"]
    )
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


def remove_owned_project_sections(text: str, workspace: Path) -> tuple[str, int]:
    """config.toml에서 이 임시 작업공간의 프로젝트·Hook 신뢰 섹션만 제거한다."""
    target = str(workspace).lower().replace("\\", "/")
    kept: list[str] = []
    removed = 0
    skipping = False
    for line in text.splitlines(keepends=True):
        stripped = line.strip()
        if stripped.startswith("["):
            header = stripped.lower().replace("\\\\", "/").replace("\\", "/")
            skipping = header.startswith(OWNED_SECTION_PREFIXES) and target in header
            removed += int(skipping)
        if not skipping:
            kept.append(line)
    return "".join(kept), removed


def remove_tree(path: Path) -> None:
    """Windows의 읽기 전용 Git 객체까지 포함해 임시 디렉터리를 지운다."""

    def make_writable(function: Any, target: str, _: BaseException) -> None:
        os.chmod(target, stat.S_IWRITE)
        function(target)

    if path.exists():
        shutil.rmtree(path, onexc=make_writable)


def cleanup(workspace: Path) -> int:
    manifest = load_manifest(workspace)
    config = codex_config_path()
    # 소유 섹션만 제거한다. 앱·사용자가 바꾼 다른 값은 되돌리지 않고 보고만 한다.
    report: dict[str, object] = {"config_restored": True}
    if config.is_file() and sha256_of(config) != manifest["config_sha256"]:
        with config.open(encoding="utf-8", newline="") as stream:
            current = stream.read()
        restored, removed = remove_owned_project_sections(current, workspace)
        report["removed_owned_project_sections"] = removed
        backup = manifest.get("config_backup")
        backup_text = ""
        if backup:
            with Path(backup).open(encoding="utf-8", newline="") as stream:
                backup_text = stream.read()
        added = set(section_headers(restored)) - set(section_headers(backup_text))
        report["unowned_section_headers_added"] = len(added)
        report["hook_related_section_headers_added"] = sum(
            "hook" in header.lower() for header in added
        )
        report["section_headers_removed_by_app"] = len(
            set(section_headers(backup_text)) - set(section_headers(restored))
        )
        if removed:
            write_atomic(config, restored)
        report["unowned_changes_preserved"] = restored != backup_text
        _, remaining = remove_owned_project_sections(
            config.read_text(encoding="utf-8"), workspace
        )
        report["config_restored"] = remaining == 0

    remove_tree(workspace)
    report["workspace_removed"] = not workspace.exists()
    state = state_dir_for(workspace)
    if report["config_restored"]:
        remove_tree(state)
    report["state_removed"] = not state.exists()
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if report["config_restored"] else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("prepare")
    for name in ("summarize", "cleanup"):
        sub = commands.add_parser(name)
        sub.add_argument("workspace", type=Path)
    mark_parser = commands.add_parser("mark")
    mark_parser.add_argument("workspace", type=Path)
    mark_parser.add_argument("label")
    arguments = parser.parse_args()
    if arguments.command == "prepare":
        return prepare()
    workspace = arguments.workspace.resolve()
    if arguments.command == "mark":
        return mark(workspace, arguments.label)
    if arguments.command == "summarize":
        return summarize(workspace)
    return cleanup(workspace)


if __name__ == "__main__":
    raise SystemExit(main())

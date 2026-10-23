import importlib.util
import json
import os
import subprocess
import sys
from pathlib import Path
from types import ModuleType

import pytest

ROOT = Path(__file__).resolve().parents[1]
PROBE = ROOT / "tests" / "poc" / "codex" / "hook_probe.py"
SESSION = ROOT / "tests" / "poc" / "codex" / "desktop_session.py"
SENTINEL = "SENSITIVE_DESKTOP_VALUE_MUST_NOT_APPEAR"


def load_session_module() -> ModuleType:
    spec = importlib.util.spec_from_file_location("desktop_session", SESSION)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run_probe(tmp_path: Path, payload: dict[str, object], output: Path) -> None:
    environment = os.environ.copy()
    environment["HOME"] = str(tmp_path)
    environment["USERPROFILE"] = str(tmp_path)
    result = subprocess.run(
        [
            sys.executable,
            str(PROBE),
            "--run-salt",
            "synthetic-test-salt",
            "--output",
            str(output),
        ],
        input=json.dumps(payload),
        cwd=tmp_path,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr == ""


def test_subagent_and_permission_fields_are_not_persisted(tmp_path: Path) -> None:
    agent_transcript = tmp_path / ".codex" / "sessions" / "agent.jsonl"
    agent_transcript.parent.mkdir(parents=True)
    agent_transcript.write_text(
        json.dumps({"type": "event_msg", "payload": {"message": SENTINEL}}) + "\n",
        encoding="utf-8",
    )
    output = tmp_path / "state" / "events.jsonl"
    run_probe(
        tmp_path,
        {
            "hook_event_name": "SubagentStop",
            "session_id": SENTINEL,
            "turn_id": SENTINEL,
            "agent_id": SENTINEL,
            "agent_type": SENTINEL,
            "agent_transcript_path": str(agent_transcript),
            "last_assistant_message": SENTINEL,
            "stop_hook_active": False,
            "permission_mode": SENTINEL,
            "cwd": SENTINEL,
        },
        output,
    )
    run_probe(
        tmp_path,
        {
            "hook_event_name": "PermissionRequest",
            "session_id": SENTINEL,
            "tool_name": f"mcp__{SENTINEL}__read",
            "tool_input": {"command": SENTINEL, "description": SENTINEL, SENTINEL: 1},
        },
        output,
    )

    captured = output.read_text(encoding="utf-8")
    assert SENTINEL not in captured
    assert str(tmp_path) not in captured
    assert not (tmp_path / ".agent-office").exists()
    stop, permission = (json.loads(line) for line in captured.splitlines())
    assert stop["agent_type"] == "other"
    assert stop["permission_mode"] == "other"
    assert stop["stop_hook_active"] is False
    assert stop["agent_fingerprint"] == stop["session_fingerprint"]
    assert stop["agent_transcript"]["valid_json_lines"] == 1
    assert "last_assistant_message" in stop["present_fields"]
    assert permission["tool_name"] == "mcp"
    assert permission["tool_input_shape"] == {
        "keys": ["command", "description"],
        "other_key_count": 1,
    }


def test_summary_uses_aliases_instead_of_fingerprints() -> None:
    session = load_session_module()
    records = [
        {
            "hook_event_name": "SessionStart",
            "session_fingerprint": "aaaa1111aaaa1111",
            "present_fields": ["session_id"],
            "probe_status": "captured",
        },
        {
            "hook_event_name": "SubagentStart",
            "session_fingerprint": "aaaa1111aaaa1111",
            "turn_fingerprint": "bbbb2222bbbb2222",
            "agent_fingerprint": "cccc3333cccc3333",
            "agent_type": "other",
            "present_fields": ["agent_id"],
            "probe_status": "captured",
        },
        {
            "hook_event_name": "SubagentStop",
            "session_fingerprint": "aaaa1111aaaa1111",
            "agent_fingerprint": "cccc3333cccc3333",
            "present_fields": ["agent_id"],
            "probe_status": "captured",
        },
    ]
    markers = [{"label": "session-start", "record_count": 1}]

    summary = session.summarize_records(records, markers)
    rendered = json.dumps(summary)

    for fingerprint in ("aaaa1111", "bbbb2222", "cccc3333"):
        assert fingerprint not in rendered
    assert summary["segments"][0] == {
        "label": "session-start",
        "events": ["SessionStart S1"],
    }
    assert summary["segments"][1]["label"] == "unmarked-tail"
    assert summary["correlation"]["subagent_start_stop_matches"] == 1
    assert "SessionEnd" in summary["missing_events"]


def test_cleanup_removes_only_owned_project_section(tmp_path: Path) -> None:
    session = load_session_module()
    workspace = tmp_path / "agent-office-desktop-poc-abc"
    original = (
        "[windows]\nsandbox = 'x'\n\n"
        "[projects.'c:\\users\\someone\\keep']\ntrust_level = \"trusted\"\n\n"
        "[features]\nhooks = true\n"
    )
    owned = (
        f"[projects.'{str(workspace).lower()}']\ntrust_level = \"trusted\"\n\n"
        f"[hooks.state.'{workspace}\\.codex\\hooks.json:stop:0:0']\n"
        'trusted_hash = "sha256:00"\n'
    )
    current = original.replace("[features]", owned + "\n[features]")

    restored, removed = session.remove_owned_project_sections(current, workspace)

    assert removed == 2
    assert restored == original


def test_hook_definitions_cover_desktop_scenario(tmp_path: Path) -> None:
    session = load_session_module()
    output = tmp_path / "events.jsonl"

    hooks = session.hook_definitions(PROBE, "salt", output)["hooks"]

    for event in (
        "SessionStart",
        "UserPromptSubmit",
        "PreToolUse",
        "PermissionRequest",
        "SubagentStart",
        "SubagentStop",
        "Stop",
        "SessionEnd",
    ):
        handler = hooks[event][0]["hooks"][0]
        assert handler["timeout"] <= 3
        assert "--output" in handler["commandWindows"]
        assert "matcher" not in hooks[event][0]


def test_cleanup_preserves_unowned_config_changes(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    session = load_session_module()
    workspace = tmp_path / "agent-office-desktop-poc-xyz"
    git_object = workspace / ".git" / "objects" / "ab" / "cdef"
    git_object.parent.mkdir(parents=True)
    git_object.write_bytes(b"synthetic")
    git_object.chmod(0o444)
    state = session.state_dir_for(workspace)
    state.mkdir()
    config = tmp_path / "config.toml"
    original = '[desktop]\r\nmode = "a"\r\n'
    config.write_bytes(original.encode())
    backup = state / "config.toml.bak"
    backup.write_bytes(original.encode())
    (state / "manifest.json").write_text(
        json.dumps(
            {
                "owner": session.OWNER_MARKER,
                "workspace": str(workspace),
                "records": str(state / "events.jsonl"),
                "config_sha256": session.sha256_of(config),
                "config_backup": str(backup),
            }
        ),
        encoding="utf-8",
    )
    app_changed = original.replace('"a"', '"b"')
    owned = f"[projects.'{str(workspace).lower()}']\r\ntrust_level = \"trusted\"\r\n"
    config.write_bytes((app_changed + owned).encode())
    monkeypatch.setattr(session, "codex_config_path", lambda: config)

    assert session.cleanup(workspace) == 0

    assert config.read_bytes() == app_changed.encode()
    assert not workspace.exists()
    assert not state.exists()

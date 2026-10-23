import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
PROBE_DIR = ROOT / "tests" / "poc" / "claude"
PROBE = PROBE_DIR / "hook_probe.py"
SENTINEL = "SENSITIVE_SYNTHETIC_VALUE_MUST_NOT_APPEAR"

sys.path.insert(0, str(PROBE_DIR))

import run_probe


def probe_environment(home: Path) -> dict[str, str]:
    environment = os.environ.copy()
    environment["AGENT_OFFICE_POC_RUN_SALT"] = "synthetic-test-salt"
    environment["HOME"] = str(home)
    environment.pop("CLAUDE_CONFIG_DIR", None)
    return environment


def run_hook(payload: object, cwd: Path, environment: dict[str, str]) -> str:
    result = subprocess.run(
        [sys.executable, str(PROBE)],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        cwd=cwd,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )
    # stdout은 모델 컨텍스트에 들어가므로 비어 있어야 하고, 어떤 입력에도 차단하지 않는다.
    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr == ""
    output = cwd / ".agent-office" / "poc" / "claude-hook-events.jsonl"
    return output.read_text(encoding="utf-8")


def test_hook_probe_does_not_persist_sensitive_values(tmp_path: Path) -> None:
    project = tmp_path / ".claude" / "projects" / SENTINEL
    transcript = project / f"{SENTINEL}.jsonl"
    subagent = project / SENTINEL / "subagents" / f"agent-{SENTINEL}.jsonl"
    subagent.parent.mkdir(parents=True)
    line = {
        "type": "assistant",
        "sessionId": SENTINEL,
        "promptId": SENTINEL,
        "uuid": SENTINEL,
        "cwd": SENTINEL,
        "newKeyName": SENTINEL,
        SENTINEL: SENTINEL,
        "message": {
            "id": SENTINEL,
            "content": [
                {"type": "text", "text": SENTINEL},
                {"type": "tool_use", "id": SENTINEL, "input": {"cmd": SENTINEL}},
            ],
        },
    }
    transcript.write_text(
        json.dumps(line) + "\n" + json.dumps({"type": "api-request"}) + "\n",
        encoding="utf-8",
    )
    subagent.write_text(
        json.dumps({"type": "user", "isSidechain": True, "agentId": SENTINEL}) + "\n",
        encoding="utf-8",
    )
    payload = {
        "session_id": SENTINEL,
        "prompt_id": SENTINEL,
        "hook_event_name": "SubagentStop",
        "cwd": SENTINEL,
        "transcript_path": str(transcript),
        "agent_transcript_path": str(subagent),
        "agent_id": SENTINEL,
        "agent_type": SENTINEL,
        "tool_name": SENTINEL,
        "tool_input": {"command": SENTINEL},
        "last_assistant_message": SENTINEL,
        "prompt": SENTINEL,
        "new_field_name": SENTINEL,
        SENTINEL: SENTINEL,
    }

    captured = run_hook(payload, tmp_path, probe_environment(tmp_path))

    assert SENTINEL not in captured
    assert str(tmp_path) not in captured
    record = json.loads(captured)
    assert record["hook_event_name"] == "SubagentStop"
    assert record["agent_type"] == "other"
    assert record["tool_name"] == "other"
    assert record["unlisted_fields"] == ["new_field_name"]
    assert record["malformed_field_name_count"] == 1
    assert record["transcript"]["template"] == "projects/<redacted>/<redacted>.jsonl"
    assert record["transcript"]["unlisted_type_counts"] == {"api-request": 1}
    assert record["transcript"]["content_block_counts"] == {"text": 1, "tool_use": 1}
    assert record["transcript"]["tool_use_fingerprints"] == [
        record["session_fingerprint"]
    ]
    shape = record["transcript"]
    assert shape["prompt_fingerprints"] == [record["prompt_fingerprint"]]
    assert shape["unlisted_top_level_keys"] == ["newKeyName"]
    assert shape["tool_use_line_types"] == {"assistant": 1}
    assert (shape["uuid_lines"], shape["unique_uuid_count"]) == (1, 1)
    assert shape["message_id_count"] == 1
    assert shape["size_bytes"] == transcript.stat().st_size
    assert set(shape["prefix_fingerprints"]) <= {"256", "1024", "4096"}
    assert shape["ends_with_newline"] is True
    assert shape["starts_with_bom"] is False
    agent_transcript = record["agent_transcript"]
    assert agent_transcript["template"] == (
        "projects/<redacted>/<redacted>/subagents/<redacted>.jsonl"
    )
    assert agent_transcript["sidechain_lines"] == 1
    assert agent_transcript["agent_fingerprints"] == [record["agent_fingerprint"]]


def test_hook_probe_ignores_transcripts_outside_claude_home(tmp_path: Path) -> None:
    outside = tmp_path / "elsewhere.jsonl"
    outside.write_text(json.dumps({"type": "user"}) + "\n", encoding="utf-8")
    payload = {"hook_event_name": "Stop", "transcript_path": str(outside)}

    record = json.loads(run_hook(payload, tmp_path, probe_environment(tmp_path)))

    assert record["transcript"]["under_claude_home"] is False
    assert "valid_json_lines" not in record["transcript"]


def test_hook_probe_fails_open_without_poc_environment(tmp_path: Path) -> None:
    environment = probe_environment(tmp_path)
    environment.pop("AGENT_OFFICE_POC_RUN_SALT")

    result = subprocess.run(
        [sys.executable, str(PROBE)],
        input="not-json",
        cwd=tmp_path,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 0
    assert result.stdout == ""
    assert not (tmp_path / ".agent-office").exists()


def test_hook_probe_reports_invalid_json_without_echoing_it(tmp_path: Path) -> None:
    captured = run_hook(SENTINEL, tmp_path, probe_environment(tmp_path))

    assert SENTINEL not in captured
    assert json.loads(captured)["probe_status"] == "invalid_json"


def test_stream_summary_keeps_only_structure() -> None:
    lines = [
        {"type": "system", "subtype": "init", "session_id": SENTINEL, "cwd": SENTINEL},
        {"type": "system", "subtype": "hook_started", "hook_event": "PreToolUse"},
        {"type": "system", "subtype": "hook_response", "hook_event": "PreToolUse"},
        {
            "type": "assistant",
            "parent_tool_use_id": SENTINEL,
            "message": {
                "content": [
                    {"type": "tool_use", "id": SENTINEL, "name": "Read"},
                    {"type": "tool_use", "id": "other", "name": "mcp__x__y"},
                ]
            },
        },
        {
            "type": "result",
            "subtype": "success",
            "is_error": False,
            "num_turns": 2,
            "result": SENTINEL,
            "permission_denials": [{"tool_input": SENTINEL}],
        },
    ]
    stdout = "\n".join(json.dumps(line) for line in lines) + "\nnot-json\n"

    summary = run_probe.sanitize_stream_jsonl(stdout, "synthetic-test-salt")

    assert SENTINEL not in json.dumps(summary)
    assert summary["invalid_json_lines"] == 1
    assert summary["hook_event_counts"] == {"PreToolUse": 1}
    assert summary["tool_use_counts"] == {"Read": 1, "mcp": 1}
    assert summary["parent_tool_use_lines"] == 1
    assert summary["result"] == {
        "subtype": "success",
        "is_error": False,
        "num_turns": 2,
        "total_cost_usd": None,
        "permission_denial_count": 1,
    }


def test_hook_settings_inject_only_probe_commands(tmp_path: Path) -> None:
    probe = tmp_path / "hook_probe.py"
    settings = run_probe.hook_settings(probe, "salt", run_probe.CORE_HOOK_EVENTS)

    assert set(settings) == {"hooks"}
    assert set(settings["hooks"]) == set(run_probe.CORE_HOOK_EVENTS)
    for event, entries in settings["hooks"].items():
        assert len(entries) == 1
        assert ("matcher" in entries[0]) == (event in run_probe.MATCHER_EVENTS)
        handler = entries[0]["hooks"][0]
        assert handler["type"] == "command"
        assert str(probe) in handler["command"]
        assert handler["timeout"] <= 5

    faulty = run_probe.hook_settings(probe, "salt", ("Stop",), "error")
    # 2는 Claude Code에서 차단 의미이므로 장애 주입에도 쓰지 않는다.
    assert faulty["hooks"]["Stop"][0]["hooks"][0]["command"].endswith("exit 1")


def test_child_environment_drops_parent_claude_session(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("CLAUDECODE", "1")
    monkeypatch.setenv("CLAUDE_CODE_SESSION_ID", "parent")
    monkeypatch.setenv("CLAUDE_CONFIG_DIR", "/synthetic")

    environment = run_probe.child_environment()

    assert "CLAUDECODE" not in environment
    assert "CLAUDE_CODE_SESSION_ID" not in environment
    assert environment["CLAUDE_CONFIG_DIR"] == "/synthetic"

import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PROBE = ROOT / "tests" / "poc" / "codex" / "hook_probe.py"


def test_hook_probe_does_not_persist_sensitive_values(tmp_path: Path) -> None:
    sentinel = "SENSITIVE_SYNTHETIC_VALUE_MUST_NOT_APPEAR"
    transcript = tmp_path / ".codex" / "sessions" / "synthetic.jsonl"
    transcript.parent.mkdir(parents=True)
    transcript.write_text(
        json.dumps(
            {
                "type": "event_msg",
                "payload": {"prompt": sentinel, "command": sentinel},
            }
        )
        + "\n",
        encoding="utf-8",
    )
    payload = {
        "session_id": sentinel,
        "turn_id": sentinel,
        "hook_event_name": "PreToolUse",
        "cwd": sentinel,
        "transcript_path": str(transcript),
        "tool_name": "Bash",
        "tool_input": {"command": sentinel},
        "prompt": sentinel,
    }
    environment = os.environ.copy()
    environment["AGENT_OFFICE_POC_RUN_SALT"] = "synthetic-test-salt"
    environment["HOME"] = str(tmp_path)
    environment["USERPROFILE"] = str(tmp_path)

    result = subprocess.run(
        [sys.executable, str(PROBE)],
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
    output = tmp_path / ".agent-office" / "poc" / "codex-hook-events.jsonl"
    captured = output.read_text(encoding="utf-8")
    assert sentinel not in captured
    assert str(tmp_path) not in captured
    record = json.loads(captured)
    assert record["hook_event_name"] == "PreToolUse"
    assert record["probe_status"] == "captured"
    assert record["tool_name"] == "Bash"
    assert record["transcript"]["valid_json_lines"] == 1


def test_hook_probe_fails_open_without_poc_environment(tmp_path: Path) -> None:
    environment = os.environ.copy()
    environment.pop("AGENT_OFFICE_POC_RUN_SALT", None)

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
    assert not (tmp_path / ".agent-office").exists()


def test_hook_probe_reports_invalid_json_without_echoing_it(tmp_path: Path) -> None:
    environment = os.environ.copy()
    environment["AGENT_OFFICE_POC_RUN_SALT"] = "synthetic-test-salt"
    sentinel = "SENSITIVE_INVALID_INPUT"

    result = subprocess.run(
        [sys.executable, str(PROBE)],
        input=sentinel,
        cwd=tmp_path,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 0
    output = tmp_path / ".agent-office" / "poc" / "codex-hook-events.jsonl"
    captured = output.read_text(encoding="utf-8")
    assert sentinel not in captured
    assert json.loads(captured)["probe_status"] == "invalid_json"

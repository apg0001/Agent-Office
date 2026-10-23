import json
import os
import re
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "fixtures" / "collector" / "claude" / "dual-source"

sys.path.insert(0, str(ROOT / "tests" / "poc" / "collector"))

import dedup_reference as dedup
import tail_reference as tail

EXPECTED = json.loads((FIXTURES / "expected.json").read_text(encoding="utf-8"))
DEVICE = EXPECTED["device_ref"]


def read_jsonl(name: str) -> list[dict[str, object]]:
    lines = (FIXTURES / name).read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines]


def hook_candidates() -> list[dedup.Candidate]:
    return [
        c for payload in read_jsonl("hooks.jsonl") for c in dedup.from_hook(payload)
    ]


def transcript_candidates() -> list[dedup.Candidate]:
    rows = read_jsonl("transcript.jsonl") + read_jsonl("subagent-transcript.jsonl")
    return [c for row in rows for c in dedup.from_transcript_line(row)]


def run(
    candidates: list[dedup.Candidate], deduplicator: dedup.Deduplicator
) -> list[str]:
    accepted = []
    for candidate in candidates:
        if deduplicator.accept(candidate):
            key = dedup.dedup_key(DEVICE, candidate)
            if key:
                accepted.append(key)
    return accepted


@pytest.mark.parametrize("hooks_first", [True, False])
def test_dual_source_dedup_is_order_independent(hooks_first: bool) -> None:
    hooks, lines = hook_candidates(), transcript_candidates()
    deduplicator = dedup.Deduplicator(DEVICE)

    accepted = run(hooks + lines if hooks_first else lines + hooks, deduplicator)

    assert sorted(accepted) == EXPECTED["accepted_dedup_keys"]
    assert len(accepted) == len(set(accepted))
    if hooks_first:
        assert deduplicator.suppressed == EXPECTED["suppressed_when_hooks_first"]


def test_hook_and_transcript_produce_identical_event_ids() -> None:
    by_key: dict[str, set[str]] = {}
    for candidate in hook_candidates() + transcript_candidates():
        key = dedup.dedup_key(DEVICE, candidate)
        if key:
            by_key.setdefault(key, set()).add(
                str(dedup.event_id_for(DEVICE, candidate))
            )

    assert set(by_key) == set(EXPECTED["accepted_dedup_keys"])
    assert all(len(ids) == 1 for ids in by_key.values())


def test_restart_with_persisted_keys_replays_nothing() -> None:
    first = dedup.Deduplicator(DEVICE)
    run(hook_candidates(), first)

    restarted = dedup.Deduplicator(DEVICE, seen=sorted(first.seen))

    assert run(transcript_candidates(), restarted) == []


def test_tool_finish_keeps_first_observed_outcome_type() -> None:
    finished = {
        c.activity_ref: c.event_type
        for c in hook_candidates()
        if c.event_type in {"tool.completed", "tool.failed"}
    }
    jsonl_finished = {
        c.activity_ref: c.event_type
        for c in transcript_candidates()
        if c.event_type in {"tool.completed", "tool.failed"}
    }

    assert finished == EXPECTED["tool_finished_event_type"]
    assert jsonl_finished == EXPECTED["tool_finished_event_type"]


def test_keyless_hook_events_are_never_suppressed() -> None:
    keyless = [c for c in hook_candidates() if dedup.dedup_key(DEVICE, c) is None]
    deduplicator = dedup.Deduplicator(DEVICE)

    assert sorted({c.event_type for c in keyless}) == sorted(
        EXPECTED["keyless_event_types_from_hooks"]
    )
    assert all(deduplicator.accept(c) for c in keyless + keyless)
    assert dedup.event_id_for(DEVICE, keyless[0]) != dedup.event_id_for(
        DEVICE, keyless[0]
    )


def test_internal_transcript_lines_are_not_activity() -> None:
    blob = {
        "type": "api-request-blob",
        "sessionId": "s",
        "message": {"content": [{"type": "tool_use", "id": "t"}]},
    }

    assert dedup.from_transcript_line(blob) == []
    assert dedup.from_hook({"hook_event_name": "PermissionRequest"}) == []


def test_subagent_tools_are_attributed_to_the_subagent() -> None:
    owners = {
        c.activity_ref: c.agent_ref
        for c in hook_candidates() + transcript_candidates()
        if c.event_type.startswith("tool.")
    }

    assert owners["toolu_synthetic_0002"] == "a0000000000000001"
    assert owners["toolu_synthetic_0001"] is None


def test_fixtures_hold_no_raw_content() -> None:
    forbidden = re.compile(
        r"(/home/|/Users/|[A-Za-z]:\\\\|/tmp/|sk-|ghp_|@[a-z]+\.[a-z]+)"
    )
    for path in FIXTURES.iterdir():
        text = path.read_text(encoding="utf-8")
        assert not forbidden.search(text), path.name
    for row in read_jsonl("hooks.jsonl"):
        for field in ("prompt", "tool_input", "tool_response", "error"):
            if field in row:
                assert json.dumps(row[field]).count("<synthetic>") >= 1


# --- tail -----------------------------------------------------------------


def append(path: Path, data: bytes) -> None:
    with path.open("ab") as stream:
        stream.write(data)


def test_partial_line_is_consumed_only_after_newline(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n{"b": ')

    first = tail.poll(path)
    append(path, b"2}\n")
    second = tail.poll(path, first.checkpoint)

    assert first.records == [{"a": 1}]
    assert first.pending_partial_bytes == len(b'{"b": ')
    assert second.records == [{"b": 2}]
    assert second.checkpoint.offset == path.stat().st_size


def test_bom_and_crlf_are_tolerated(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(tail.BOM + b'{"a": 1}\r\n\r\n{"b": 2}\r\n')

    result = tail.poll(path)

    assert result.records == [{"a": 1}, {"b": 2}]
    assert result.invalid_lines == 0


def test_invalid_lines_are_skipped_without_stalling(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\nnot-json\n[1, 2]\n\xff\xfe\n{"b": 2}\n')

    result = tail.poll(path)

    assert result.records == [{"a": 1}, {"b": 2}]
    assert result.invalid_lines == 3
    assert tail.poll(path, result.checkpoint).records == []


def test_restart_resumes_from_persisted_checkpoint(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n')
    saved = tail.checkpoint_to_json(tail.poll(path).checkpoint)

    append(path, b'{"b": 2}\n')
    resumed = tail.poll(path, tail.checkpoint_from_json(saved))

    assert resumed.records == [{"b": 2}]
    assert resumed.reset_reason is None


def test_truncation_restarts_from_beginning(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n{"b": 2}\n')
    checkpoint = tail.poll(path).checkpoint

    path.write_bytes(b'{"c": 3}\n')
    result = tail.poll(path, checkpoint)

    assert result.reset_reason == "truncated"
    assert result.records == [{"c": 3}]


def test_rewrite_with_same_size_is_detected(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n')
    checkpoint = tail.poll(path).checkpoint

    with path.open("r+b") as stream:
        stream.write(b'{"z": 9}\n')
    append(path, b'{"b": 2}\n')
    result = tail.poll(path, checkpoint)

    assert result.reset_reason == "rewritten"
    assert result.records == [{"z": 9}, {"b": 2}]


def test_rotation_reads_new_file_from_start(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n')
    checkpoint = tail.poll(path).checkpoint

    path.rename(tmp_path / "t.jsonl.1")
    path.write_bytes(b'{"b": 2}\n')
    result = tail.poll(path, checkpoint)

    assert result.reset_reason == "rotated"
    assert result.records == [{"b": 2}]


def test_missing_file_keeps_checkpoint(tmp_path: Path) -> None:
    checkpoint = tail.Checkpoint(device=1, inode=2, offset=10)

    result = tail.poll(tmp_path / "absent.jsonl", checkpoint)

    assert result.status == "missing"
    assert result.checkpoint == checkpoint


@pytest.mark.skipif(
    sys.platform == "win32" or os.geteuid() == 0,
    reason="POSIX 권한으로 읽기 거부를 재현할 수 없는 환경",
)
def test_permission_denied_fails_open(tmp_path: Path) -> None:
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n')
    path.chmod(0)
    try:
        result = tail.poll(path)
    finally:
        path.chmod(0o600)

    assert result.status == "permission_denied"
    assert result.records == []
    assert result.checkpoint.offset == 0


def test_oversized_line_is_dropped_and_tail_continues(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(tail, "MAX_LINE_BYTES", 16)
    path = tmp_path / "t.jsonl"
    path.write_bytes(b'{"a": 1}\n' + b"x" * 40)

    first = tail.poll(path)
    append(path, b'yyyy\n{"b": 2}\n')
    second = tail.poll(path, first.checkpoint)

    assert first.records == [{"a": 1}]
    assert first.oversized_lines == 1
    assert second.records == [{"b": 2}]
    assert second.oversized_lines == 0

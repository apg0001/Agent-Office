"""JSONL tail 체크포인트 규칙의 참조 구현(#20).

Go Collector(#30)가 같은 fixture로 검증할 수 있도록 규칙만 단순하게 구현한다.
원본 줄 내용은 호출자에게 파싱된 dict로만 넘기고 어디에도 저장하지 않는다.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field, replace
from pathlib import Path
from typing import Any

BOM = b"\xef\xbb\xbf"
# 체크포인트에 남기는 앞부분 지문 길이. 같은 파일이 다시 쓰였는지만 판정한다.
PREFIX_BYTES = 256
MAX_LINE_BYTES = 1_048_576


@dataclass(frozen=True)
class Checkpoint:
    """파일 하나의 읽기 위치. 로컬 SQLite에 영속화하는 단위다."""

    device: int | None = None
    inode: int | None = None
    offset: int = 0
    prefix_digest: str | None = None
    # 최대 길이를 넘은 줄을 건너뛰는 중이면 다음 개행까지 버린다.
    skipping_oversized_line: bool = False


@dataclass
class TailResult:
    checkpoint: Checkpoint
    records: list[dict[str, Any]] = field(default_factory=list)
    status: str = "ok"
    reset_reason: str | None = None
    invalid_lines: int = 0
    oversized_lines: int = 0
    pending_partial_bytes: int = 0


def _prefix_digest(path: Path, length: int) -> str | None:
    if length <= 0:
        return None
    with path.open("rb") as stream:
        return hashlib.sha256(stream.read(min(length, PREFIX_BYTES))).hexdigest()


def poll(path: Path, checkpoint: Checkpoint | None = None) -> TailResult:
    """새로 append된 완결 줄만 읽고 다음 체크포인트를 돌려준다.

    - 파일이 없거나 권한이 없으면 예외 없이 상태만 알리고 체크포인트를 유지한다.
    - inode가 바뀌면(로테이션·교체) 새 파일을 처음부터 읽는다.
    - 크기가 줄었거나 앞부분이 달라졌으면(truncate·재작성) 처음부터 읽는다.
    - 개행으로 끝나지 않은 마지막 조각은 소비하지 않는다(partial write).
    - 깨진 줄은 건너뛰고 개수만 센다. 같은 줄에서 영원히 멈추지 않는다.
    """
    current = checkpoint or Checkpoint()
    try:
        stat = path.stat()
    except FileNotFoundError:
        return TailResult(checkpoint=current, status="missing")
    except PermissionError:
        return TailResult(checkpoint=current, status="permission_denied")

    reset_reason = None
    if current.inode is not None and (stat.st_dev, stat.st_ino) != (
        current.device,
        current.inode,
    ):
        reset_reason = "rotated"
    elif stat.st_size < current.offset:
        reset_reason = "truncated"
    if reset_reason is None and current.prefix_digest is not None:
        try:
            if _prefix_digest(path, current.offset) != current.prefix_digest:
                reset_reason = "rewritten"
        except PermissionError:
            return TailResult(checkpoint=current, status="permission_denied")
    if reset_reason is not None:
        current = Checkpoint()

    result = TailResult(
        checkpoint=replace(current, device=stat.st_dev, inode=stat.st_ino),
        reset_reason=reset_reason,
    )
    try:
        with path.open("rb") as stream:
            stream.seek(current.offset)
            data = stream.read(max(stat.st_size - current.offset, 0))
    except PermissionError:
        return TailResult(checkpoint=current, status="permission_denied")

    offset = current.offset
    skipping = current.skipping_oversized_line
    position = 0
    while True:
        newline = data.find(b"\n", position)
        if newline < 0:
            break
        raw_line = data[position:newline]
        line_start = offset + position
        position = newline + 1
        if skipping:
            skipping = False
            continue
        if line_start == 0 and raw_line.startswith(BOM):
            raw_line = raw_line[len(BOM) :]
        if len(raw_line) > MAX_LINE_BYTES:
            result.oversized_lines += 1
            continue
        raw_line = raw_line.rstrip(b"\r")
        if not raw_line.strip():
            continue
        try:
            item = json.loads(raw_line)
        except (UnicodeDecodeError, json.JSONDecodeError):
            result.invalid_lines += 1
            continue
        if isinstance(item, dict):
            result.records.append(item)
        else:
            result.invalid_lines += 1

    remainder = len(data) - position
    if remainder > MAX_LINE_BYTES and not skipping:
        # 개행 없이 한도를 넘은 줄은 버리고 다음 개행까지 건너뛴다.
        result.oversized_lines += 1
        position = len(data)
        skipping = True
    elif skipping:
        position = len(data)
    else:
        result.pending_partial_bytes = remainder

    new_offset = offset + position
    result.checkpoint = replace(
        result.checkpoint,
        offset=new_offset,
        prefix_digest=_prefix_digest(path, new_offset),
        skipping_oversized_line=skipping,
    )
    return result


def checkpoint_to_json(checkpoint: Checkpoint) -> str:
    return json.dumps(checkpoint.__dict__, sort_keys=True)


def checkpoint_from_json(text: str) -> Checkpoint:
    return Checkpoint(**json.loads(text))

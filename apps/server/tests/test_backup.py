"""백업 암호화·보존·복원 시험(#58). 실제 덤프·복원 테스트는 PostgreSQL 17 클라이언트가 필요하다."""

from __future__ import annotations

import base64
import datetime as dt
import io
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest
from helpers import sign_in
from sqlalchemy import Engine, text

from agent_office import backup

KEY = os.urandom(32)


def roundtrip(data: bytes, key: bytes = KEY) -> bytes:
    sealed = io.BytesIO()
    backup.encrypt_stream(io.BytesIO(data), sealed, key)
    return b"".join(backup.decrypt_chunks(io.BytesIO(sealed.getvalue()), key))


@pytest.mark.parametrize("size", [0, 1, backup.CHUNK_SIZE, backup.CHUNK_SIZE * 2 + 7])
def test_encryption_round_trip(size: int) -> None:
    data = os.urandom(size)
    assert roundtrip(data) == data


def sealed_blob(data: bytes) -> bytes:
    out = io.BytesIO()
    backup.encrypt_stream(io.BytesIO(data), out, KEY)
    return out.getvalue()


def decrypt(blob: bytes, key: bytes = KEY) -> bytes:
    return b"".join(backup.decrypt_chunks(io.BytesIO(blob), key))


def test_tampering_truncation_and_wrong_key_are_detected() -> None:
    data = b"sentinel-secret-row" * 100_000  # 청크 2개 이상
    blob = sealed_blob(data)
    assert b"sentinel-secret-row" not in blob

    flipped = bytearray(blob)
    flipped[len(backup.MAGIC) + 40] ^= 1
    first_chunk_end = len(backup.MAGIC) + 16 + backup.CHUNK_SIZE + 16
    cases = {
        "decrypt_failed": bytes(flipped),
        "truncated": blob[:first_chunk_end],
        "trailing_data": blob + b"x",
        "not_a_backup": b"plain pg_dump",
    }
    for code, broken in cases.items():
        with pytest.raises(backup.BackupError) as error:
            decrypt(broken)
        assert error.value.code == code
    with pytest.raises(backup.BackupError):
        decrypt(blob, os.urandom(32))


def test_key_must_be_32_bytes(monkeypatch: pytest.MonkeyPatch) -> None:
    for value in ("", "short", base64.urlsafe_b64encode(os.urandom(16)).decode()):
        monkeypatch.setenv(backup.BACKUP_KEY_ENV, value)
        assert not backup.configured()
    monkeypatch.setenv(backup.BACKUP_KEY_ENV, base64.urlsafe_b64encode(KEY).decode())
    assert backup.configured() and backup.backup_key() == KEY


def touch(directory: Path, when: dt.datetime) -> Path:
    path = directory / f"agent-office-{when.strftime('%Y%m%dT%H%M%SZ')}{backup.SUFFIX}"
    path.write_bytes(b"x")
    path.with_name(path.name + ".json").write_text("{}")
    return path


def test_local_rotation_keeps_newest(tmp_path: Path) -> None:
    start = dt.datetime(2026, 10, 1, tzinfo=dt.UTC)
    for day in range(10):
        touch(tmp_path, start + dt.timedelta(days=day))
    removed = backup.rotate(tmp_path, keep=7)
    assert len(removed) == 3 and len(backup.backups(tmp_path)) == 7
    assert not list(tmp_path.glob("*20261001*"))


def test_offsite_keeps_7_daily_and_4_weekly(tmp_path: Path) -> None:
    start = dt.datetime(2026, 8, 3, 3, tzinfo=dt.UTC)  # 월요일
    files = [touch(tmp_path, start + dt.timedelta(days=d)) for d in range(60)]
    keep = backup.offsite_keep(files, 7, 4)
    assert set(files[-7:]) <= keep and len(keep) == 11
    older = sorted(keep - set(files[-7:]))
    weeks = {backup._stamp(p).isocalendar()[:2] for p in older}
    assert len(weeks) == 4


def test_libpq_args_keep_password_out_of_argv() -> None:
    args, env = backup._libpq("postgresql+psycopg://u:p%40ss@db:5433/office")
    assert args == ["-h", "db", "-p", "5433", "-U", "u", "-d", "office"]
    assert env["PGPASSWORD"] == "p@ss" and "p@ss" not in " ".join(args)


NO_PG17 = "PostgreSQL 17 클라이언트(pg_dump) 없음: 운영 이미지·로컬 테스트 이미지에서 검증"


def _pg_tools_17() -> bool:
    if not shutil.which("pg_dump"):
        return False
    output = subprocess.run(["pg_dump", "--version"], capture_output=True, text=True).stdout
    found = re.search(r"(\d+)\.", output)
    return bool(found and int(found.group(1)) >= 17)


@pytest.mark.skipif(not _pg_tools_17(), reason=NO_PG17)
def test_backup_verify_and_restore_test(
    engine: Engine, database: str, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    sign_in(engine)
    monkeypatch.setenv("AGENT_OFFICE_DATABASE_URL", database)
    monkeypatch.setenv(backup.BACKUP_KEY_ENV, base64.urlsafe_b64encode(KEY).decode())
    offsite = tmp_path / "offsite"
    monkeypatch.setenv(backup.OFFSITE_DIR_ENV, str(offsite))

    info = backup.create_backup(tmp_path / "local")
    path = tmp_path / "local" / info.name
    assert path.exists() and (offsite / info.name).exists()
    assert oct(path.stat().st_mode & 0o777) == "0o600"
    assert oct((offsite / info.name).stat().st_mode & 0o777) == "0o600"
    assert not list((tmp_path / "local").glob("*.partial"))
    backup.verify(path)

    result = backup.restore_test(path)
    assert result["migration"] == "0008" and result["tables"] > 20 and result["users"] == 1

    path.write_bytes(path.read_bytes()[:-1] + b"\0")
    with pytest.raises(backup.BackupError) as error:
        backup.verify(path)
    assert error.value.code == "checksum_mismatch"


@pytest.mark.skipif(not _pg_tools_17(), reason=NO_PG17)
def test_backup_of_dump_larger_than_pipe_buffer(
    engine: Engine, database: str, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """pg_restore --list가 목차만 읽고 먼저 끝나도(BrokenPipe) 덤프가 크면 실패하던 문제(#187)."""
    monkeypatch.setenv("AGENT_OFFICE_DATABASE_URL", database)
    monkeypatch.setenv(backup.BACKUP_KEY_ENV, base64.urlsafe_b64encode(KEY).decode())
    with engine.begin() as connection:
        connection.execute(text("CREATE TABLE bulk_187 (v text)"))
        connection.execute(
            text("INSERT INTO bulk_187 SELECT md5(g::text) FROM generate_series(1, 200000) g")
        )
    info = backup.create_backup(tmp_path / "local")
    assert info.size_bytes > 1_000_000
    backup.verify(tmp_path / "local" / info.name)


@pytest.mark.skipif(not _pg_tools_17(), reason=NO_PG17)
def test_migrate_check_upgrades_a_restored_older_backup(
    engine: Engine, database: str, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    from alembic import command
    from conftest import alembic_config

    from agent_office.monitoring import expected_revision

    command.downgrade(alembic_config(), "0005")
    monkeypatch.setenv("AGENT_OFFICE_DATABASE_URL", database)
    monkeypatch.setenv(backup.BACKUP_KEY_ENV, base64.urlsafe_b64encode(KEY).decode())
    monkeypatch.delenv(backup.OFFSITE_DIR_ENV, raising=False)

    info = backup.create_backup(tmp_path)
    result = backup.restore_test(tmp_path / info.name, migrate=True)

    assert result["migration_before"] == "0005"
    assert result["migration"] == expected_revision()
    # 운영 DB는 그대로다.
    with engine.connect() as connection:
        assert (
            connection.execute(text("SELECT version_num FROM alembic_version")).scalar() == "0005"
        )

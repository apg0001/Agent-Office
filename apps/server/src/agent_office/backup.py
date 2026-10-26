"""PostgreSQL 백업·암호화·보존·복원 시험(#58).

- `pg_dump -Fc` 출력을 곧바로 AES-256-GCM 청크 암호화해 저장한다(평문 덤프가 디스크에 남지 않음).
- 저장 직후 복호화한 스트림을 `pg_restore --list`로 읽어 덤프 무결성을 확인한다.
- 로컬 최근 7개, 외부 디렉터리(NAS·USB·rclone 마운트 등)에는 일간 7개 + 주간 4개를 둔다.
- 복원 시험은 같은 PostgreSQL에 임시 DB를 만들어 복원하고 마이그레이션 버전과 표를 확인한 뒤 지운다.

키는 `AGENT_OFFICE_BACKUP_KEY`(32바이트 URL-safe base64)로 받는다. 관리 화면 비밀 키와 따로 두어
한쪽이 유출돼도 다른 쪽을 읽을 수 없게 한다. 키를 잃으면 백업을 복원할 수 없다.
"""

from __future__ import annotations

import base64
import binascii
import contextlib
import datetime as dt
import hashlib
import json
import os
import secrets
import shutil
import struct
import subprocess
import sys
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path
from typing import IO, Any

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from sqlalchemy.engine import make_url

BACKUP_KEY_ENV = "AGENT_OFFICE_BACKUP_KEY"
BACKUP_DIR_ENV = "AGENT_OFFICE_BACKUP_DIR"
OFFSITE_DIR_ENV = "AGENT_OFFICE_BACKUP_OFFSITE_DIR"
DEFAULT_BACKUP_DIR = "/backups"
LOCAL_KEEP = 7
OFFSITE_DAILY_KEEP = 7
OFFSITE_WEEKLY_KEEP = 4
CHUNK_SIZE = 1024 * 1024
MAGIC = b"AOBK1\n"
SUFFIX = ".dump.aes"


class BackupError(Exception):
    """원인 코드만 담는다(경로·접속 정보·명령 출력은 담지 않음)."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


def backup_key() -> bytes:
    raw = os.environ.get(BACKUP_KEY_ENV, "")
    try:
        key = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)) if raw else b""
    except (binascii.Error, ValueError) as error:
        raise BackupError("backup_key_missing") from error
    if len(key) != 32:
        raise BackupError("backup_key_missing")
    return key


# --- 암호화 형식 ------------------------------------------------------------------------
# MAGIC, 그 뒤 청크 반복: [12바이트 nonce][4바이트 길이][GCM 암호문].
# AAD = MAGIC + 청크 번호 + 마지막 표시.
# 순서 바꾸기·잘라내기·덧붙이기는 태그 검사에서 실패한다.


def _aad(index: int, last: bool) -> bytes:
    return MAGIC + struct.pack(">Q?", index, last)


def encrypt_stream(source: IO[bytes], target: IO[bytes], key: bytes) -> None:
    cipher = AESGCM(key)
    target.write(MAGIC)
    index = 0
    current = source.read(CHUNK_SIZE)
    while True:
        following = source.read(CHUNK_SIZE)
        last = not following
        nonce = os.urandom(12)
        sealed = cipher.encrypt(nonce, current, _aad(index, last))
        target.write(nonce + struct.pack(">I", len(sealed)) + sealed)
        if last:
            return
        current, index = following, index + 1


def decrypt_chunks(source: IO[bytes], key: bytes) -> Iterator[bytes]:
    if source.read(len(MAGIC)) != MAGIC:
        raise BackupError("not_a_backup")
    cipher = AESGCM(key)
    index = 0
    while True:
        header = source.read(16)
        if len(header) != 16:
            # 마지막 표시 청크 전에 끝났다(잘림).
            raise BackupError("truncated")
        nonce, (length,) = header[:12], struct.unpack(">I", header[12:])
        sealed = source.read(length)
        if len(sealed) != length:
            raise BackupError("truncated")
        for last in (False, True):
            try:
                plain = cipher.decrypt(nonce, sealed, _aad(index, last))
            except InvalidTag:
                continue
            yield plain
            if last:
                if source.read(1):
                    raise BackupError("trailing_data")
                return
            break
        else:
            raise BackupError("decrypt_failed")
        index += 1


# --- PostgreSQL --------------------------------------------------------------------------


def _libpq(database_url: str, dbname: str | None = None) -> tuple[list[str], dict[str, str]]:
    """SQLAlchemy URL을 libpq 인자로. 암호는 명령줄이 아니라 환경 변수(PGPASSWORD)로만 넘긴다."""
    url = make_url(database_url)
    args = ["-h", url.host or "localhost", "-p", str(url.port or 5432), "-U", url.username or ""]
    env = {**os.environ, "PGPASSWORD": url.password or "", "PGCONNECT_TIMEOUT": "10"}
    return [*args, "-d", dbname or url.database or ""], env


def _database_url() -> str:
    from agent_office.db.config import database_url

    return database_url()


@dataclass(frozen=True)
class BackupInfo:
    name: str
    size_bytes: int
    sha256: str
    created_at: str


def _manifest(path: Path) -> Path:
    return path.with_name(path.name + ".json")


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(CHUNK_SIZE), b""):
            digest.update(block)
    return digest.hexdigest()


def _restore_list(path: Path, key: bytes) -> None:
    """복호화 스트림을 `pg_restore --list`에 넣어 덤프 목차를 읽을 수 있는지 확인한다.

    파이프로 받은 custom 형식은 목차만 읽고 끝나므로, 덤프가 파이프 버퍼(약 64KiB)보다 크면
    나머지를 쓰다 BrokenPipeError가 난다(#187: 운영 DB 약 300KB에서 백업이 항상 실패). 그때도
    파일 끝까지 복호화해 모든 청크의 AES-GCM 인증을 확인하고, 판정은 pg_restore 종료 코드로 한다.
    """
    process = subprocess.Popen(  # noqa: S603 - 고정 인자
        ["pg_restore", "--list"],
        stdin=subprocess.PIPE,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    assert process.stdin is not None
    reader_open = True
    try:
        with path.open("rb") as handle:
            for chunk in decrypt_chunks(handle, key):
                if not reader_open:
                    continue
                try:
                    process.stdin.write(chunk)
                except BrokenPipeError:
                    reader_open = False
    finally:
        with contextlib.suppress(BrokenPipeError):
            process.stdin.close()
    if process.wait() != 0:
        raise BackupError("dump_unreadable")


def create_backup(directory: Path | None = None, now: dt.datetime | None = None) -> BackupInfo:
    key = backup_key()
    directory = directory or Path(os.environ.get(BACKUP_DIR_ENV, DEFAULT_BACKUP_DIR))
    try:
        directory.mkdir(parents=True, exist_ok=True)
        probe = directory / ".write-test"
        probe.write_bytes(b"")
        probe.unlink()
    except OSError as error:
        raise BackupError("backup_dir_unwritable") from error
    stamp = (now or dt.datetime.now(dt.UTC)).strftime("%Y%m%dT%H%M%SZ")
    final = directory / f"agent-office-{stamp}{SUFFIX}"
    partial = final.with_name(final.name + ".partial")
    args, env = _libpq(_database_url())
    process = subprocess.Popen(  # noqa: S603 - 고정 인자, 암호는 환경 변수
        ["pg_dump", "-Fc", "--no-owner", "--no-privileges", *args],
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        env=env,
    )
    assert process.stdout is not None
    try:
        fd = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as target:
            encrypt_stream(process.stdout, target, key)
            target.flush()
            os.fsync(target.fileno())
        if process.wait() != 0:
            raise BackupError("pg_dump_failed")
        _restore_list(partial, key)
    except BaseException:
        process.kill()
        partial.unlink(missing_ok=True)
        raise
    os.replace(partial, final)
    info = BackupInfo(
        name=final.name,
        size_bytes=final.stat().st_size,
        sha256=_sha256(final),
        created_at=stamp,
    )
    _manifest(final).write_text(json.dumps(info.__dict__), encoding="utf-8")
    rotate(directory, keep=LOCAL_KEEP)
    offsite = os.environ.get(OFFSITE_DIR_ENV)
    if offsite:
        # 외부 복제가 실패해도 로컬 백업은 남긴다. 호출자가 오류 코드를 기록한다.
        try:
            copy_offsite(final, Path(offsite))
        except OSError as error:
            raise BackupError("offsite_unwritable") from error
    return info


def backups(directory: Path) -> list[Path]:
    return sorted(p for p in directory.glob(f"agent-office-*{SUFFIX}") if p.is_file())


def _remove(path: Path) -> None:
    path.unlink(missing_ok=True)
    _manifest(path).unlink(missing_ok=True)


def rotate(directory: Path, keep: int) -> list[str]:
    files = backups(directory)
    removed = files[:-keep] if keep > 0 else files
    for path in removed:
        _remove(path)
    return [p.name for p in removed]


def _stamp(path: Path) -> dt.datetime:
    text = path.name.removeprefix("agent-office-").removesuffix(SUFFIX)
    return dt.datetime.strptime(text, "%Y%m%dT%H%M%SZ").replace(tzinfo=dt.UTC)


def offsite_keep(files: list[Path], daily: int, weekly: int) -> set[Path]:
    """최근 일간 `daily`개와, 그보다 오래된 것 중 ISO 주마다 마지막 1개씩 `weekly`주."""
    ordered = sorted(files, key=_stamp)
    keep = set(ordered[-daily:]) if daily else set()
    weeks: dict[tuple[int, int], Path] = {}
    for path in ordered[: max(0, len(ordered) - daily)]:
        iso = _stamp(path).isocalendar()
        weeks[(iso.year, iso.week)] = path
    for week in sorted(weeks)[-weekly:] if weekly else []:
        keep.add(weeks[week])
    return keep


def copy_offsite(source: Path, directory: Path) -> None:
    """같은 디스크에만 남지 않도록 외부 디렉터리에 복사하고 일간·주간 보존을 적용한다."""
    directory.mkdir(parents=True, exist_ok=True)
    partial = directory / (source.name + ".partial")
    shutil.copyfile(source, partial)
    os.chmod(partial, 0o600)
    if _sha256(partial) != _sha256(source):
        partial.unlink(missing_ok=True)
        raise BackupError("offsite_copy_mismatch")
    os.replace(partial, directory / source.name)
    shutil.copyfile(_manifest(source), _manifest(directory / source.name))
    files = backups(directory)
    keep = offsite_keep(files, OFFSITE_DAILY_KEEP, OFFSITE_WEEKLY_KEEP)
    for path in files:
        if path not in keep:
            _remove(path)


def verify(path: Path) -> None:
    manifest = _manifest(path)
    if manifest.exists():
        expected = json.loads(manifest.read_text(encoding="utf-8")).get("sha256")
        if expected and expected != _sha256(path):
            raise BackupError("checksum_mismatch")
    _restore_list(path, backup_key())


def _run_migrations(scratch_url: str) -> None:
    """이 이미지의 마이그레이션을 임시 DB에 적용한다(배포 사전 점검, #60)."""
    from alembic import command
    from alembic.config import Config

    config = Config()
    config.set_main_option("script_location", "agent_office.db:migrations")
    previous = os.environ.get("AGENT_OFFICE_DATABASE_URL")
    os.environ["AGENT_OFFICE_DATABASE_URL"] = scratch_url
    try:
        command.upgrade(config, "head")
    except Exception as error:  # noqa: BLE001 - 어떤 실패든 배포를 막는 코드로 바꾼다.
        raise BackupError("migration_dry_run_failed") from error
    finally:
        if previous is None:
            os.environ.pop("AGENT_OFFICE_DATABASE_URL", None)
        else:
            os.environ["AGENT_OFFICE_DATABASE_URL"] = previous


def restore_test(path: Path, *, migrate: bool = False) -> dict[str, Any]:
    """임시 DB에 복원해 마이그레이션 버전과 표 수를 확인하고 지운다. 운영 DB는 건드리지 않는다.

    `migrate=True`면 복원한 DB에 이 이미지의 마이그레이션을 적용해 본다(배포 전 사전 실행).
    결과에 복원 직후 버전(`migration_before`)과 적용 후 버전(`migration`)이 담긴다.
    """
    key = backup_key()
    verify(path)
    url = _database_url()
    scratch = f"ao_restore_test_{secrets.token_hex(4)}"
    admin_args, env = _libpq(url, "postgres")

    def psql(sql: str, stage: str, db_args: list[str] = admin_args) -> str:
        result = subprocess.run(  # noqa: S603 - 고정 인자
            ["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", *db_args, "-c", sql],
            capture_output=True,
            env=env,
            text=True,
            check=False,
        )
        if result.returncode != 0:
            raise BackupError(f"restore_test_{stage}_failed")
        return result.stdout.strip()

    psql(f'CREATE DATABASE "{scratch}"', "create")
    try:
        scratch_args, _ = _libpq(url, scratch)
        process = subprocess.Popen(  # noqa: S603 - 고정 인자
            ["pg_restore", "--no-owner", "--no-privileges", "--exit-on-error", *scratch_args],
            stdin=subprocess.PIPE,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            env=env,
        )
        assert process.stdin is not None
        try:
            with path.open("rb") as handle:
                for chunk in decrypt_chunks(handle, key):
                    process.stdin.write(chunk)
        except BrokenPipeError as error:
            raise BackupError("restore_test_restore_failed") from error
        finally:
            process.stdin.close()
        if process.wait() != 0:
            raise BackupError("restore_test_restore_failed")
        before = psql("SELECT version_num FROM alembic_version", "check", scratch_args)
        if migrate:
            _run_migrations(str(make_url(url).set(database=scratch).render_as_string(False)))
        revision = psql("SELECT version_num FROM alembic_version", "check", scratch_args)
        tables = int(
            psql(
                "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'",
                "check",
                scratch_args,
            )
        )
        users = int(psql("SELECT count(*) FROM users", "check", scratch_args))
    finally:
        psql(f'DROP DATABASE IF EXISTS "{scratch}" WITH (FORCE)', "drop")
    return {"migration_before": before, "migration": revision, "tables": tables, "users": users}


def decrypt_to(path: Path, out: Path) -> None:
    """수동 복원용: 복호화한 `pg_dump -Fc` 파일을 만든다(0600). 복원 절차는 문서 참고."""
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as target, path.open("rb") as source:
        for chunk in decrypt_chunks(source, backup_key()):
            target.write(chunk)


def _latest(directory: Path) -> Path:
    files = backups(directory)
    if not files:
        raise BackupError("no_backup")
    return files[-1]


USAGE = """사용법: agent-office-backup <명령>
  create                  지금 백업(암호화·검사·로컬 7개·외부 일7/주4 보존)
  list                    백업 목록
  verify [파일]           체크섬과 덤프 목차 확인(기본: 최신)
  restore-test [파일]     임시 DB에 복원해 확인 후 삭제(기본: 최신)
  migrate-check [파일]    임시 DB에 복원하고 이 이미지의 마이그레이션을 적용해 본 뒤 삭제(배포 전)
  expected-revision       이 이미지가 기대하는 DB 마이그레이션 버전
  decrypt <파일> <출력>   수동 복원용 복호화"""


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    directory = Path(os.environ.get(BACKUP_DIR_ENV, DEFAULT_BACKUP_DIR))
    if not args:
        print(USAGE)
        return 2
    command, rest = args[0], args[1:]

    def pick() -> Path:
        return directory / Path(rest[0]).name if rest else _latest(directory)

    try:
        if command == "create":
            info = create_backup(directory)
            print(f"{info.name} {info.size_bytes} bytes sha256={info.sha256}")
        elif command == "list":
            for path in backups(directory):
                print(f"{path.name} {path.stat().st_size}")
        elif command == "verify":
            verify(pick())
            print("ok")
        elif command == "restore-test":
            print(json.dumps(restore_test(pick())))
        elif command == "migrate-check":
            print(json.dumps(restore_test(pick(), migrate=True)))
        elif command == "expected-revision":
            from agent_office.monitoring import expected_revision

            print(expected_revision() or "")
        elif command == "decrypt" and len(rest) == 2:
            decrypt_to(directory / Path(rest[0]).name, Path(rest[1]))
            print("ok")
        else:
            print(USAGE)
            return 2
    except BackupError as error:
        print(f"실패: {error.code}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())


# --- 서버 안 주기 실행 ----------------------------------------------------------------

STATUS_KEY = "backup.status"
FIRST_RUN_DELAY_SECONDS = 600
BACKUP_INTERVAL = dt.timedelta(days=1)
RESTORE_TEST_INTERVAL = dt.timedelta(days=30)
_LOCK_KEY = "agent_office.backup"


def configured() -> bool:
    try:
        backup_key()
    except BackupError:
        return False
    return True


def read_status(connection: Any) -> dict[str, Any]:
    from agent_office import instance

    return instance.get_dict(connection, STATUS_KEY)


def _record(**fields: Any) -> None:
    from agent_office import instance
    from agent_office.db.engine import get_engine

    with get_engine().begin() as connection:
        current = instance.get_dict(connection, STATUS_KEY)
        instance.put_value(connection, STATUS_KEY, {**current, **fields})


def _due(value: Any, interval: dt.timedelta, now: dt.datetime) -> bool:
    if not value:
        return True
    try:
        return now - dt.datetime.fromisoformat(str(value)) >= interval
    except ValueError:
        return True


def notify_safely(kind: str, code: str | None = None) -> None:
    from agent_office.notifications.manager import notify_safely as send

    send(kind, code)


def run_scheduled_once(now: dt.datetime | None = None) -> None:
    """하루 한 번 백업, 30일마다 복원 시험. 여러 서버 프로세스여도 advisory lock으로 하나만."""
    from sqlalchemy import func, select

    from agent_office.db.engine import get_engine

    now = now or dt.datetime.now(dt.UTC)
    with get_engine().connect() as lock_connection:
        if not lock_connection.execute(
            select(func.pg_try_advisory_lock(func.hashtext(_LOCK_KEY)))
        ).scalar_one():
            return
        try:
            with get_engine().connect() as connection:
                status = read_status(connection)
            if _due(status.get("last_backup_at"), BACKUP_INTERVAL, now):
                try:
                    info = create_backup()
                    _record(
                        last_backup_at=now.isoformat(),
                        last_backup_ok=True,
                        last_backup_size_bytes=info.size_bytes,
                        last_error=None,
                    )
                    notify_safely("backup.recovered")
                except BackupError as error:
                    _record(
                        last_backup_at=now.isoformat(), last_backup_ok=False, last_error=error.code
                    )
                    notify_safely("backup.failed", error.code)
                    return
            if _due(status.get("last_restore_test_at"), RESTORE_TEST_INTERVAL, now):
                directory = Path(os.environ.get(BACKUP_DIR_ENV, DEFAULT_BACKUP_DIR))
                try:
                    restore_test(_latest(directory))
                    _record(last_restore_test_at=now.isoformat(), last_restore_test_ok=True)
                    notify_safely("restore_test.recovered")
                except BackupError as error:
                    _record(
                        last_restore_test_at=now.isoformat(),
                        last_restore_test_ok=False,
                        last_error=error.code,
                    )
                    notify_safely("restore_test.failed", error.code)
        finally:
            lock_connection.execute(select(func.pg_advisory_unlock(func.hashtext(_LOCK_KEY))))


async def run_periodically() -> None:
    """키를 설정했을 때만 돈다. 기동 10분 뒤부터 한 시간마다 할 일이 있는지 본다."""
    import asyncio
    import logging

    from starlette.concurrency import run_in_threadpool

    if not configured():
        return
    await asyncio.sleep(FIRST_RUN_DELAY_SECONDS)
    while True:
        try:
            await run_in_threadpool(run_scheduled_once)
        except Exception as error:  # noqa: BLE001 - 백업 실패가 서버를 멈추면 안 된다.
            logging.getLogger(__name__).warning("backup run failed: %s", type(error).__name__)
        await asyncio.sleep(3600)

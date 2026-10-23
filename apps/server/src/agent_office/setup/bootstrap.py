"""일회성 최초 관리자 bootstrap(#66).

토큰은 운영자가 서버 콘솔에서 실행한 명령의 출력으로만 한 번 나간다(컨테이너 로그에 남기지 않음).
DB에는 SHA-256 해시와 만료 시각만 둔다. 최초 SUPER_ADMIN을 만들면 토큰을 폐기하고, 설치 완료
후에는 다시 발급할 수 없다.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import secrets
import sys
import uuid
from typing import Any

from sqlalchemy import Connection, func, select

from agent_office import instance
from agent_office.db import schema
from agent_office.security import constant_time_equal

BOOTSTRAP_KEY = "setup.bootstrap"
COMPLETED_KEY = "setup.completed"
BOOTSTRAP_TOKEN_PREFIX = "aob_"
BOOTSTRAP_LIFETIME = dt.timedelta(hours=1)


class BootstrapUnavailable(Exception):
    """이미 관리자가 있거나 설치가 끝나 토큰을 발급할 수 없다."""


_get = instance.get_value
_delete = instance.delete_value


def _put(connection: Connection, key: str, value: Any) -> None:
    instance.put_value(connection, key, value)


def setup_completed(connection: Connection) -> bool:
    return _get(connection, COMPLETED_KEY) is True


def super_admin_exists(connection: Connection) -> bool:
    users = schema.users
    return (
        connection.execute(
            select(users.c.id).where(users.c.instance_role == "SUPER_ADMIN").limit(1)
        ).first()
        is not None
    )


def _digest(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()


def issue_token(connection: Connection) -> str:
    """새 토큰을 발급한다. 이전 토큰은 무효가 된다."""
    _get(connection, BOOTSTRAP_KEY, lock=True)
    if setup_completed(connection) or super_admin_exists(connection):
        raise BootstrapUnavailable
    token = BOOTSTRAP_TOKEN_PREFIX + secrets.token_urlsafe(32)
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    _put(
        connection,
        BOOTSTRAP_KEY,
        {"token_sha256": _digest(token), "expires_at": (now + BOOTSTRAP_LIFETIME).isoformat()},
    )
    return token


def bootstrap_pending(connection: Connection) -> bool:
    value = _get(connection, BOOTSTRAP_KEY)
    if not isinstance(value, dict):
        return False
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    return dt.datetime.fromisoformat(str(value.get("expires_at"))) > now


def consume_token(connection: Connection, token: str) -> bool:
    """토큰이 맞고 만료 전이면 폐기하고 True. 행 잠금으로 동시 요청 중 하나만 성공한다."""
    value = _get(connection, BOOTSTRAP_KEY, lock=True)
    if not isinstance(value, dict) or not token or len(token) > 256:
        return False
    if setup_completed(connection) or super_admin_exists(connection):
        return False
    now: dt.datetime = connection.execute(select(func.now())).scalar_one()
    if dt.datetime.fromisoformat(str(value.get("expires_at"))) <= now:
        return False
    if not constant_time_equal(_digest(token).encode(), str(value.get("token_sha256")).encode()):
        return False
    _delete(connection, BOOTSTRAP_KEY)
    return True


def complete_setup(connection: Connection, user_id: uuid.UUID) -> None:
    """설치 완료 잠금. 이후 bootstrap 토큰 발급은 거부된다."""
    instance.put_value(connection, COMPLETED_KEY, True, user_id)
    _delete(connection, BOOTSTRAP_KEY)


def main() -> int:
    """`docker compose exec server agent-office-bootstrap`: 토큰을 이 콘솔에만 출력한다."""
    from agent_office.db.engine import get_engine

    try:
        with get_engine().begin() as connection:
            token = issue_token(connection)
    except BootstrapUnavailable:
        print("이미 최초 관리자가 있거나 설치가 완료되어 bootstrap 토큰을 발급할 수 없습니다.")
        return 1
    print("최초 관리자 bootstrap 토큰(1시간 유효, 한 번만 사용, 다시 실행하면 이전 토큰 무효):")
    print(token)
    return 0


if __name__ == "__main__":
    sys.exit(main())

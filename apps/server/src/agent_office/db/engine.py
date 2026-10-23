"""요청 단위 트랜잭션을 여는 FastAPI 의존성."""

from __future__ import annotations

from collections.abc import Iterator
from functools import lru_cache

from sqlalchemy import Connection, Engine, create_engine

from agent_office.db.config import database_url


@lru_cache(maxsize=1)
def get_engine() -> Engine:
    # DB가 필요한 요청이 처음 올 때 만든다. health 같은 DB 없는 엔드포인트는 영향이 없다.
    return create_engine(database_url(), pool_pre_ping=True)


def get_connection() -> Iterator[Connection]:
    """요청 하나를 트랜잭션 하나로 처리한다. 예외가 나면 전체를 되돌린다."""
    with get_engine().begin() as connection:
        yield connection

"""Alembic 실행 환경. 대상 메타데이터는 `agent_office.db.schema`다."""

from __future__ import annotations

from alembic import context
from sqlalchemy import create_engine, pool

from agent_office.db.config import database_url
from agent_office.db.schema import metadata

config = context.config


def run_migrations_offline() -> None:
    context.configure(
        url=database_url(),
        target_metadata=metadata,
        literal_binds=True,
        dialect_opts={"paramstyle": "named"},
        compare_type=True,
    )
    with context.begin_transaction():
        context.run_migrations()


def run_migrations_online() -> None:
    engine = create_engine(database_url(), poolclass=pool.NullPool)
    with engine.connect() as connection:
        context.configure(
            connection=connection,
            target_metadata=metadata,
            compare_type=True,
            # 마이그레이션 하나가 실패하면 그 리비전 전체를 되돌린다.
            transaction_per_migration=True,
        )
        with context.begin_transaction():
            context.run_migrations()
    engine.dispose()


if context.is_offline_mode():
    run_migrations_offline()
else:
    run_migrations_online()

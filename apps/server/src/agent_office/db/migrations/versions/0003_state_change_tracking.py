"""실시간 전달용 상태 변경 추적(#37).

전역 시퀀스 `state_change_seq`와 세션·에이전트 상태의 `change_seq`, 변경 시각 인덱스를 추가한다.
기존 행은 시퀀스 기본값으로 채워진다.

Revision ID: 0003
Revises: 0002
Create Date: 2026-10-23 20:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0003"
down_revision: str | None = "0002"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.execute(sa.schema.CreateSequence(sa.Sequence("state_change_seq")))
    seq_default = sa.text("nextval('state_change_seq')")
    op.add_column(
        "agent_sessions",
        sa.Column("change_seq", sa.BigInteger(), server_default=seq_default, nullable=False),
    )
    op.add_column(
        "agent_sessions",
        sa.Column(
            "changed_at",
            sa.DateTime(timezone=True),
            server_default=sa.text("now()"),
            nullable=False,
        ),
    )
    op.add_column(
        "agent_current_states",
        sa.Column("change_seq", sa.BigInteger(), server_default=seq_default, nullable=False),
    )
    op.create_index("ix_agent_sessions_changed_at", "agent_sessions", ["changed_at"])
    op.create_index("ix_agent_current_states_updated_at", "agent_current_states", ["updated_at"])


def downgrade() -> None:
    op.drop_index("ix_agent_current_states_updated_at", table_name="agent_current_states")
    op.drop_index("ix_agent_sessions_changed_at", table_name="agent_sessions")
    op.drop_column("agent_current_states", "change_seq")
    op.drop_column("agent_sessions", "changed_at")
    op.drop_column("agent_sessions", "change_seq")
    op.execute(sa.schema.DropSequence(sa.Sequence("state_change_seq")))

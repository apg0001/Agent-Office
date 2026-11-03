"""승인 대기 종류: 권한 승인·질문·계획 확인(#262).

Revision ID: 0010
Revises: 0009
Create Date: 2026-11-03 23:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0010"
down_revision: str | None = "0009"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # 기존 승인 대기 행은 비워 둔다. 화면은 null을 "승인 대기"(권한)로 본다.
    op.add_column(
        "agent_current_states", sa.Column("wait_kind", sa.String(length=16), nullable=True)
    )
    op.create_check_constraint(
        "ck_agent_current_states_wait_kind",
        "agent_current_states",
        "wait_kind IN ('permission', 'question', 'plan_review')",
    )


def downgrade() -> None:
    op.drop_constraint("ck_agent_current_states_wait_kind", "agent_current_states", type_="check")
    op.drop_column("agent_current_states", "wait_kind")

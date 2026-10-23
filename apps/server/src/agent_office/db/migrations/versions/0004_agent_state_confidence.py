"""현재 에이전트 활동의 관측·추정 구분(#47).

Revision ID: 0004
Revises: 0003
Create Date: 2026-10-23 22:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0004"
down_revision: str | None = "0003"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "agent_current_states",
        sa.Column("confidence", sa.String(length=16), server_default="observed", nullable=False),
    )
    op.create_check_constraint(
        "ck_agent_current_states_confidence",
        "agent_current_states",
        "confidence IN ('observed', 'inferred')",
    )


def downgrade() -> None:
    op.drop_constraint("ck_agent_current_states_confidence", "agent_current_states", type_="check")
    op.drop_column("agent_current_states", "confidence")

"""세션 작업 폴더 이름(#181).

Collector가 PC별 동의를 받았을 때만 보내는 작업 폴더의 마지막 이름을 세션에 둔다. 전체 경로는
받지 않으므로 경로 구분자·제어 문자는 DB 제약으로도 막는다. 기존 행은 NULL이다.

Revision ID: 0008
Revises: 0007
Create Date: 2026-10-26 12:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0008"
down_revision: str | None = "0007"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

# schema.WORKSPACE_NAME_CHECK와 같아야 한다(test_db_schema가 메타데이터와 비교).
WORKSPACE_NAME_CHECK = (
    "workspace_name IS NULL OR (char_length(workspace_name) BETWEEN 1 AND 64"
    " AND workspace_name !~ '[/\\\\[:cntrl:]]')"
)


def upgrade() -> None:
    op.add_column(
        "agent_sessions",
        sa.Column("workspace_name", sa.String(length=64), nullable=True),
    )
    op.create_check_constraint(
        "ck_agent_sessions_workspace_name", "agent_sessions", WORKSPACE_NAME_CHECK
    )


def downgrade() -> None:
    op.drop_constraint("ck_agent_sessions_workspace_name", "agent_sessions", type_="check")
    op.drop_column("agent_sessions", "workspace_name")

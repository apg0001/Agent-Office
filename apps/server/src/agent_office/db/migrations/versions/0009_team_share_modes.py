"""팀 공유 범위와 폴더 이름 프로젝트(#197).

- `team_members.share_mode`: 구성원이 팀에 보여 줄 범위(`all`·`selected`·`none`). 이미 있는 구성원은
  지금까지의 규칙(공유한 프로젝트만)을 유지하도록 `selected`로 채우고, 이후 새 행의 기본값은 `all`이다
  (가입·생성 화면에서 미리 체크된 동의를 받아 보낸다).
- `projects.origin`: 작업 폴더 이름으로 자동 생성한 프로젝트는 `workspace`.
- `agent_sessions.project_manual`: 사용자가 직접 정한 배정. 지금까지의 배정은 모두 직접 정한 것이다.

Revision ID: 0009
Revises: 0008
Create Date: 2026-10-26 18:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0009"
down_revision: str | None = "0008"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # 기존 구성원은 'selected'로 채운 뒤 기본값만 'all'로 바꾼다.
    op.add_column(
        "team_members",
        sa.Column("share_mode", sa.String(length=16), server_default="selected", nullable=False),
    )
    op.alter_column("team_members", "share_mode", server_default="all")
    op.create_check_constraint(
        "ck_team_members_share_mode",
        "team_members",
        "share_mode IN ('all', 'selected', 'none')",
    )
    op.add_column(
        "projects",
        sa.Column("origin", sa.String(length=16), server_default="manual", nullable=False),
    )
    op.create_check_constraint(
        "ck_projects_origin", "projects", "origin IN ('manual', 'workspace')"
    )
    op.add_column(
        "agent_sessions",
        sa.Column("project_manual", sa.Boolean(), server_default="false", nullable=False),
    )
    op.execute("UPDATE agent_sessions SET project_manual = true WHERE project_id IS NOT NULL")


def downgrade() -> None:
    op.drop_column("agent_sessions", "project_manual")
    op.drop_constraint("ck_projects_origin", "projects", type_="check")
    op.drop_column("projects", "origin")
    op.drop_constraint("ck_team_members_share_mode", "team_members", type_="check")
    op.drop_column("team_members", "share_mode")

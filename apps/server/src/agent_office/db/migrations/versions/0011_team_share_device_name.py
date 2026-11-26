"""팀별 'PC 이름 공개' 옵션.

- `team_members.share_device_name`: 이 구성원이 이 팀의 팀원에게 자기 PC 이름을 보여 줄지.
  기존 구성원과 새 구성원 모두 기본은 끔(false)이라 지금까지의 화면(소유자 표시 이름만)이 그대로다.

열 하나를 더하기만 하는 가역 변경이다.

Revision ID: 0011
Revises: 0010
Create Date: 2026-11-26 12:00:00
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0011"
down_revision: str | None = "0010"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "team_members",
        sa.Column("share_device_name", sa.Boolean(), server_default="false", nullable=False),
    )


def downgrade() -> None:
    op.drop_column("team_members", "share_device_name")

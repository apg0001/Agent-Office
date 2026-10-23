"""이메일 계정: 가입 정책, 사용자 생성, 일회용 토큰(#23).

최초 관리자 생성(#66)도 `create_user`를 쓴다.
"""

from __future__ import annotations

import datetime as dt
import re
import secrets
import uuid
from typing import Literal

from sqlalchemy import Connection, Table, delete, func, select, update

from agent_office.auth.passwords import hash_password
from agent_office.db import schema
from agent_office.security import token_hash

SIGNUP_POLICY_KEY = "auth.signup_policy"
SignupPolicy = Literal["PUBLIC_VERIFIED_EMAIL", "INVITE_ONLY", "ADMIN_APPROVAL", "CLOSED"]
SIGNUP_POLICIES: tuple[SignupPolicy, ...] = (
    "PUBLIC_VERIFIED_EMAIL",
    "INVITE_ONLY",
    "ADMIN_APPROVAL",
    "CLOSED",
)
# 설치 마법사(#67)에서 관리자가 고르기 전까지는 가입을 받지 않는다.
DEFAULT_SIGNUP_POLICY: SignupPolicy = "CLOSED"
EMAIL_VERIFICATION_LIFETIME = dt.timedelta(hours=24)
PASSWORD_RESET_LIFETIME = dt.timedelta(hours=1)
MAX_EMAIL_LENGTH = 320

_EMAIL = re.compile(r"^[^@\s]{1,64}@[^@\s.]+(\.[^@\s.]+)+$")


def normalize_email(value: str) -> str | None:
    email = value.strip().lower()
    if len(email) > MAX_EMAIL_LENGTH or not _EMAIL.fullmatch(email):
        return None
    return email


def signup_policy(connection: Connection) -> SignupPolicy:
    value = connection.execute(
        select(schema.instance_settings.c.value).where(
            schema.instance_settings.c.key == SIGNUP_POLICY_KEY
        )
    ).scalar_one_or_none()
    for policy in SIGNUP_POLICIES:
        if value == policy:
            return policy
    return DEFAULT_SIGNUP_POLICY


def create_user(
    connection: Connection,
    *,
    email: str,
    display_name: str,
    password: str | None,
    instance_role: str = "USER",
    status: str = "pending",
    email_verified: bool = False,
) -> uuid.UUID:
    """이메일은 정규화된 값이어야 한다. 중복이면 IntegrityError(호출자가 처리)."""
    return uuid.UUID(
        str(
            connection.execute(
                schema.users.insert()
                .values(
                    email=email,
                    display_name=display_name,
                    password_hash=hash_password(password) if password is not None else None,
                    instance_role=instance_role,
                    status=status,
                    email_verified_at=func.now() if email_verified else None,
                )
                .returning(schema.users.c.id)
            ).scalar_one()
        )
    )


def issue_one_time_token(
    connection: Connection, table: Table, user_id: uuid.UUID, lifetime: dt.timedelta
) -> str:
    """새 토큰을 발급하고 같은 사용자의 이전 미사용 토큰은 지운다. 원문은 메일로만 나간다."""
    connection.execute(
        delete(table).where(table.c.user_id == user_id, table.c.consumed_at.is_(None))
    )
    token = secrets.token_urlsafe(32)
    connection.execute(
        table.insert().values(
            user_id=user_id, token_hash=token_hash(token), expires_at=func.now() + lifetime
        )
    )
    return token


def consume_one_time_token(connection: Connection, table: Table, token: str) -> uuid.UUID | None:
    """만료 전·미사용 토큰을 원자적으로 소비한다. 동시에 두 번 써도 한 번만 성공한다."""
    if not token or len(token) > 256:
        return None
    user_id = connection.execute(
        update(table)
        .where(
            table.c.token_hash == token_hash(token),
            table.c.consumed_at.is_(None),
            table.c.expires_at > func.now(),
        )
        .values(consumed_at=func.now())
        .returning(table.c.user_id)
    ).scalar_one_or_none()
    return uuid.UUID(str(user_id)) if user_id is not None else None

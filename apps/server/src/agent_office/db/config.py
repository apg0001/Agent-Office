"""데이터베이스 연결 설정. 비밀 값은 환경 변수로만 받는다."""

from __future__ import annotations

import os

DATABASE_URL_ENV = "AGENT_OFFICE_DATABASE_URL"


def database_url() -> str:
    """SQLAlchemy URL(`postgresql+psycopg://...`)을 환경 변수에서 읽는다."""
    url = os.environ.get(DATABASE_URL_ENV)
    if not url:
        raise RuntimeError(f"{DATABASE_URL_ENV} is not set")
    if not url.startswith("postgresql+psycopg://"):
        raise RuntimeError(f"{DATABASE_URL_ENV} must use the postgresql+psycopg driver")
    return url

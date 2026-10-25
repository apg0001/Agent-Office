"""교차 사이트 요청 방지용 Origin 검사. WebSocket과 로그인 전 POST가 함께 쓴다."""

from __future__ import annotations

import os
from urllib.parse import urlsplit

ALLOWED_ORIGINS_ENV = "AGENT_OFFICE_ALLOWED_ORIGINS"


def origin_allowed(origin: str | None, host: str | None) -> bool:
    """브라우저가 보낸 Origin은 같은 호스트이거나 허용 목록에 있어야 한다."""
    if origin is None:
        # Origin이 없는 비브라우저 클라이언트는 피해자 쿠키를 쓸 수 없다.
        return True
    allowed = {o.strip() for o in os.environ.get(ALLOWED_ORIGINS_ENV, "").split(",") if o.strip()}
    if origin in allowed:
        return True
    return urlsplit(origin).netloc == host

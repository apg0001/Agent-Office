"""요청 본문 크기 제한. FastAPI가 본문을 읽기 전에 Content-Length로 거부한다."""

from __future__ import annotations

import json

from starlette.types import ASGIApp, Message, Receive, Scope, Send

DEFAULT_MAX_BODY_BYTES = 65_536
# 경로 접두사별 상한. 이벤트 배치는 최대 500개라 더 크게 둔다.
PATH_MAX_BODY_BYTES = {"/api/v1/events/batch": 1_048_576}
BODY_METHODS = {"POST", "PUT", "PATCH"}


class BodySizeLimitMiddleware:
    def __init__(self, app: ASGIApp) -> None:
        self.app = app

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http" or scope["method"] not in BODY_METHODS:
            await self.app(scope, receive, send)
            return
        limit = PATH_MAX_BODY_BYTES.get(scope["path"], DEFAULT_MAX_BODY_BYTES)
        headers = dict(scope["headers"])
        raw_length = headers.get(b"content-length")
        if raw_length is None:
            if headers.get(b"transfer-encoding"):
                # 길이를 모르는 chunked 본문은 끝까지 읽기 전에는 크기를 알 수 없다.
                await _reject(send, 411, "length_required")
                return
            await self.app(scope, receive, send)
            return
        try:
            length = int(raw_length)
        except ValueError:
            await _reject(send, 400, "invalid_content_length")
            return
        if length > limit:
            await _reject(send, 413, "payload_too_large")
            return
        await self.app(scope, receive, send)


async def _reject(send: Send, status: int, detail: str) -> None:
    body = json.dumps({"detail": detail}).encode()
    start: Message = {
        "type": "http.response.start",
        "status": status,
        "headers": [
            (b"content-type", b"application/json"),
            (b"content-length", str(len(body)).encode()),
        ],
    }
    await send(start)
    await send({"type": "http.response.body", "body": body})

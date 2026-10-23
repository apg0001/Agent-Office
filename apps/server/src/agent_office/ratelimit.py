"""메모리 고정 창 속도 제한. 단일 서버 프로세스(Pi 목표) 기준이다."""

from __future__ import annotations

import threading
import time
from collections.abc import Callable


class KeyedRateLimiter:
    def __init__(
        self, limit: int, window_seconds: float, clock: Callable[[], float] = time.monotonic
    ) -> None:
        self.limit = limit
        self.window = window_seconds
        self.clock = clock
        self._hits: dict[str, tuple[float, int]] = {}
        self._lock = threading.Lock()

    def hit(self, key: str) -> float | None:
        """허용이면 None, 초과면 다시 시도할 때까지 남은 초."""
        now = self.clock()
        with self._lock:
            if len(self._hits) > 10_000:
                # 오래된 창을 정리해 메모리를 묶어 둔다.
                self._hits = {k: v for k, v in self._hits.items() if now - v[0] < self.window}
            start, count = self._hits.get(key, (now, 0))
            if now - start >= self.window:
                start, count = now, 0
            if count >= self.limit:
                return self.window - (now - start)
            self._hits[key] = (start, count + 1)
            return None

    def reset(self) -> None:
        with self._lock:
            self._hits.clear()

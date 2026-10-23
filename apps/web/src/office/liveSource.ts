/**
 * 화면 데이터 출처. 로그인 세션이 있으면 서버 실시간(`WS /api/v1/live`), 없으면 합성 데이터.
 * 브라우저 전용 코드다(단위 테스트는 `liveStore`·`smoothing`으로 한다).
 */

import { applyDelta, applySnapshot, isOfficeMessage } from "./liveStore";
import type { OfficeState } from "./types";

const MAX_BACKOFF_MS = 30_000;

export interface LiveHandle {
  close: () => void;
}

export async function hasSession(fetcher: typeof fetch = fetch): Promise<boolean> {
  try {
    const response = await fetcher("/api/v1/auth/session", { credentials: "same-origin" });
    return response.ok;
  } catch {
    return false;
  }
}

export function connectLive(onState: (state: OfficeState) => void, onStatus: (online: boolean) => void): LiveHandle {
  let socket: WebSocket | undefined;
  let state: OfficeState | undefined;
  let closed = false;
  let backoff = 1000;
  let timer: number | undefined;

  const open = () => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/live`);
    socket.onopen = () => {
      backoff = 1000;
      onStatus(true);
    };
    socket.onmessage = (event: MessageEvent<string>) => {
      let message: unknown;
      try {
        message = JSON.parse(event.data);
      } catch {
        return;
      }
      if (!isOfficeMessage(message)) {
        return;
      }
      // 재접속 때마다 서버가 스냅샷부터 보내므로 끊긴 동안의 변경도 복원된다.
      state = message.type === "snapshot" ? applySnapshot(message) : state && applyDelta(state, message);
      if (state) {
        onState(state);
      }
    };
    socket.onclose = () => {
      onStatus(false);
      if (closed) {
        return;
      }
      timer = window.setTimeout(open, backoff);
      backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
    };
  };
  open();
  return {
    close: () => {
      closed = true;
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
      socket?.close();
    },
  };
}

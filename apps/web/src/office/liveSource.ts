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

/** 실시간 주소. 팀 범위면 `?team=`을 붙인다(#54). */
export function liveUrl(protocol: string, host: string, teamId: string | null): string {
  const scheme = protocol === "https:" ? "wss:" : "ws:";
  const query = teamId ? `?team=${encodeURIComponent(teamId)}` : "";
  return `${scheme}//${host}/api/v1/live${query}`;
}

/** 현재 주소의 사무실 범위. 팀 ID가 아니면 개인 사무실. */
export function scopeFromSearch(search: string): string | null {
  const team = new URLSearchParams(search).get("team");
  return team && /^[0-9a-f-]{36}$/i.test(team) ? team : null;
}

export function connectLive(
  onState: (state: OfficeState) => void,
  onStatus: (online: boolean) => void,
  teamId: string | null = null,
): LiveHandle {
  let socket: WebSocket | undefined;
  let state: OfficeState | undefined;
  let closed = false;
  let backoff = 1000;
  let timer: number | undefined;

  const open = () => {
    socket = new WebSocket(liveUrl(window.location.protocol, window.location.host, teamId));
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

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

/**
 * 사무실 범위(#53). 이벤트는 한 번만 저장되고 범위마다 화면 배치만 다르다.
 * personal: 내 사무실, team: 팀 사무실, combined: 내 것 + 내 팀들에 공유된 것을 한 사무실에,
 * side: 개인·팀 사무실을 나란히(각각 따로 연결).
 */
export type OfficeScope =
  | { kind: "personal" }
  | { kind: "team"; teamId: string }
  | { kind: "combined" }
  | { kind: "side" };

export const PERSONAL: OfficeScope = { kind: "personal" };

/** 실시간 주소. */
export function liveUrl(protocol: string, host: string, scope: OfficeScope = PERSONAL): string {
  const scheme = protocol === "https:" ? "wss:" : "ws:";
  const query =
    scope.kind === "team" ? `?team=${encodeURIComponent(scope.teamId)}` : scope.kind === "combined" ? "?view=combined" : "";
  return `${scheme}//${host}/api/v1/live${query}`;
}

/** 현재 주소의 사무실 범위. 알 수 없는 값은 개인 사무실. */
export function scopeFromSearch(search: string): OfficeScope {
  const params = new URLSearchParams(search);
  const team = params.get("team");
  if (team && /^[0-9a-f-]{36}$/i.test(team)) {
    return { kind: "team", teamId: team };
  }
  const view = params.get("view");
  if (view === "combined" || view === "side") {
    return { kind: view };
  }
  return PERSONAL;
}

export function searchFor(scope: OfficeScope): string {
  if (scope.kind === "team") {
    return `?team=${encodeURIComponent(scope.teamId)}`;
  }
  return scope.kind === "personal" ? "" : `?view=${scope.kind}`;
}

export function connectLive(
  onState: (state: OfficeState) => void,
  onStatus: (online: boolean) => void,
  scope: OfficeScope = PERSONAL,
): LiveHandle {
  let socket: WebSocket | undefined;
  let state: OfficeState | undefined;
  let closed = false;
  let backoff = 1000;
  let timer: number | undefined;

  const open = () => {
    socket = new WebSocket(liveUrl(window.location.protocol, window.location.host, scope));
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

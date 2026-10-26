/**
 * 세션 이름(작업 폴더의 마지막 이름, #181). 사용자가 그 PC에서 켰을 때만 서버에 오며,
 * 서버는 본인 장치의 세션에만 값을 준다. 이전 서버는 필드를 보내지 않으므로 없으면 null이다.
 */

import type { SessionState } from "./types";

/** 이벤트 v1 `workspace_name` 최대 길이(코드 포인트). */
export const MAX_WORKSPACE_NAME = 64;
/** 사무실 캐릭터 아래에 그릴 최대 글자 수. 넘으면 말줄임표로 줄인다. */
export const MAX_WORKSPACE_LABEL = 20;

// 제어 문자, 경로 구분자, 줄 구분자, 보이지 않는 서식 문자(방향 바꿈 등).
const FORBIDDEN = /[\u0000-\u001f\u007f-\u009f/\\\u00ad\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff\ufff9-\ufffb]/u;

/** 서버 값이 계약(1..64자, 경로 구분자·제어 문자 없음, `.`/`..` 아님)에 맞을 때만 돌려준다. */
export function workspaceName(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const length = Array.from(value).length;
  if (length < 1 || length > MAX_WORKSPACE_NAME || FORBIDDEN.test(value)) {
    return null;
  }
  if (value.trim() === "" || value === "." || value === "..") {
    return null;
  }
  return value;
}

/** 화면 표시용으로 줄인 이름. */
export function shortWorkspace(name: string, max = MAX_WORKSPACE_LABEL): string {
  const chars = Array.from(name);
  return chars.length > max ? `${chars.slice(0, max - 1).join("").trimEnd()}…` : name;
}

/** 서버에서 받은 세션의 workspace_name을 검증된 값 또는 null로 맞춘다. */
export function withWorkspaceName<T extends SessionState>(session: T): T {
  const name = workspaceName((session as { workspace_name?: unknown }).workspace_name);
  return session.workspace_name === name ? session : { ...session, workspace_name: name };
}

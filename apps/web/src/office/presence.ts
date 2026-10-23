/**
 * 사실 상태 → 화면 존재 방식(#50). 세 상태축을 섞지 않는다.
 * - 세션 종료·하위 에이전트 완료: 출입구로 걸어가 퇴근
 * - Collector 연결 끊김: 퇴근시키지 않고 흐리게 + 연결 끊김 표시
 * - 열림 미확인(UNKNOWN): 옅게 흐리게 + "마지막 활동 이후 대기"
 */

import type { AgentState, OfficeState } from "./types";

export type Presence = "present" | "leaving";
export type Badge = "disconnected" | "unconfirmed" | null;

export interface PresenceView {
  presence: Presence;
  alpha: number;
  badge: Badge;
}

export const BADGE_LABELS: Record<Exclude<Badge, null>, string> = {
  disconnected: "연결 끊김",
  unconfirmed: "마지막 활동 이후 대기",
};

export function presenceOf(agent: AgentState, state: OfficeState): PresenceView {
  const session = state.sessions.find((item) => item.id === agent.session_id);
  if (!session || session.lifecycle === "ENDED" || agent.ended_at !== null) {
    return { presence: "leaving", alpha: 1, badge: null };
  }
  const device = state.devices.find((item) => item.id === session.device_id);
  if (device && device.connection === "DISCONNECTED") {
    return { presence: "present", alpha: 0.4, badge: "disconnected" };
  }
  if (session.lifecycle === "UNKNOWN") {
    return { presence: "present", alpha: 0.6, badge: "unconfirmed" };
  }
  return { presence: "present", alpha: agent.confidence === "inferred" ? 0.55 : 1, badge: null };
}

/** 휴게실 생활 연출: 일정 주기마다 다른 칸을 고른다(결정적, 연출 전용). */
export const IDLE_WANDER_MS = 8000;

/** FNV-1a 후 섞기. 끝 글자만 다른 연속 ID도 고르게 퍼진다. */
export function mixHash(value: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash = Math.imul(hash ^ value.charCodeAt(index), 0x01000193) >>> 0;
  }
  hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b) >>> 0;
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35) >>> 0;
  return (hash ^ (hash >>> 16)) >>> 0;
}

export function idleCycle(now: number, agentId: string): number {
  // 캐릭터마다 시작 시점을 엇갈려 한꺼번에 움직이지 않게 한다.
  return Math.floor((now + (mixHash(agentId) % IDLE_WANDER_MS)) / IDLE_WANDER_MS);
}

/**
 * 캐릭터 머리 위에 작게 띄우는 현재 상태(#162). 사실 상태(턴·활동)만 쓰고 내용은 없다.
 * 연결 끊김 같은 존재 배지(presence.ts)가 있으면 그쪽이 우선이다.
 */

import { mixHash } from "./presence";
import type { Activity, AgentState } from "./types";

export const ACTIVITY_HEADLINES: Record<Activity, string> = {
  reading: "📖 읽는 중",
  browsing: "🌐 검색 중",
  coding: "⌨️ 코딩 중",
  testing: "🧪 테스트 중",
  planning: "📝 계획 중",
  delegating: "📨 위임 중",
  reporting: "📋 보고 중",
  thinking: "💭 생각 중",
  waiting_user: "✋ 승인 대기",
  idle: "☕ 쉬는 중",
  unknown: "⚙️ 작업 중",
};

export function headlineFor(agent: Pick<AgentState, "activity" | "turn_state">): string {
  switch (agent.turn_state) {
    case "WAITING_USER":
      return ACTIVITY_HEADLINES.waiting_user;
    case "IDLE":
      return ACTIVITY_HEADLINES.idle;
    case "THINKING":
      return agent.activity === "unknown" ? ACTIVITY_HEADLINES.thinking : ACTIVITY_HEADLINES[agent.activity];
    default:
      return ACTIVITY_HEADLINES[agent.activity];
  }
}

/**
 * 쉬는 캐릭터의 생활 연출(#233). 사실 상태는 "쉬는 중"이고, 지도에서만 캐릭터마다 다른 소일거리를
 * 보여 준다. 브라우저에서만 계산하며 서버로 보내는 정보는 없다(세션 목록은 사실 상태 그대로).
 */
export const IDLE_PASTIMES = [
  "☕ 커피 타임",
  "🍩 간식 먹는 중",
  "📱 폰 보는 중",
  "🧘 스트레칭",
  "🎧 음악 감상",
  "📰 신문 보는 중",
  "🪴 화분 물 주기",
  "🎮 게임 중",
  "🐱 고양이 영상",
  "🛋️ 소파에 누움",
] as const;
export const CHATTING = "💬 수다 떠는 중";
export const DOZING = "😪 조는 중";
export const SLEEPING = "💤 자는 중";
/** 소일거리가 바뀌는 주기. 캐릭터마다 바뀌는 때를 엇갈린다. */
export const PASTIME_MS = 20_000;
/** 마지막 활동 뒤 이만큼 지나면 졸고, 더 지나면 잔다. */
export const DOZE_AFTER_MS = 5 * 60_000;
export const SLEEP_AFTER_MS = 15 * 60_000;

export function idleHeadline(agentId: string, now: number, idleSince: number | null, chatting: boolean): string {
  const idle = idleSince === null ? 0 : now - idleSince;
  if (idle >= SLEEP_AFTER_MS) {
    return SLEEPING;
  }
  if (idle >= DOZE_AFTER_MS) {
    return DOZING;
  }
  if (chatting) {
    return CHATTING;
  }
  const seed = mixHash(agentId);
  const turn = Math.floor((now + (seed % PASTIME_MS)) / PASTIME_MS);
  return IDLE_PASTIMES[(seed + turn) % IDLE_PASTIMES.length]!;
}

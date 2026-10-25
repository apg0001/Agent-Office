/**
 * 캐릭터 머리 위에 작게 띄우는 현재 상태(#162). 사실 상태(턴·활동)만 쓰고 내용은 없다.
 * 연결 끊김 같은 존재 배지(presence.ts)가 있으면 그쪽이 우선이다.
 */

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

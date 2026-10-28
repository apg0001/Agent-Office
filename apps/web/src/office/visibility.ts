/**
 * 활동 없는 세션 숨기기(#245). 사실 상태는 그대로 두고 화면에 넘기기 전에만 거른다.
 * - 마지막 활동(`last_event_at`, 없으면 `started_at`)이 관리자가 정한 분보다 오래된 세션을 숨긴다.
 * - 승인 대기(WAITING_USER) 에이전트가 있는 세션은 숨기지 않는다(사람이 봐야 한다).
 * - 종료된 세션은 원래대로 퇴근 연출을 하므로 건드리지 않는다.
 * 숨긴 세션과 그 에이전트를 상태에서 빼면 지도 캐릭터는 출입구로 걸어 나가고(OfficeScene),
 * 활성 세션 목록에서도 빠진다. 새 활동으로 `last_event_at`이 갱신되면 다시 나타난다.
 */

import type { OfficeState } from "./types";

const MINUTE_MS = 60_000;

/** 숨길 세션 ID. `minutes`가 0 이하이거나 없으면 아무것도 숨기지 않는다. */
export function hiddenSessionIds(state: OfficeState, minutes: number | undefined, now: number): Set<string> {
  const hidden = new Set<string>();
  if (minutes === undefined || !Number.isFinite(minutes) || minutes <= 0) {
    return hidden;
  }
  const cutoff = now - minutes * MINUTE_MS;
  const waiting = new Set(
    state.agents
      .filter((agent) => agent.turn_state === "WAITING_USER" && agent.ended_at === null)
      .map((agent) => agent.session_id),
  );
  for (const session of state.sessions) {
    if (session.lifecycle === "ENDED" || session.ended_at !== null || waiting.has(session.id)) {
      continue;
    }
    const last = Date.parse(session.last_event_at ?? session.started_at ?? "");
    // 시각을 모르면 숨기지 않는다(잘못 숨기는 것보다 보이는 편이 낫다).
    if (!Number.isNaN(last) && last < cutoff) {
      hidden.add(session.id);
    }
  }
  return hidden;
}

/** 숨길 세션과 그 에이전트를 뺀 화면용 상태. 숨길 것이 없으면 같은 객체를 돌려준다. */
export function withoutHidden(state: OfficeState, hidden: ReadonlySet<string>): OfficeState {
  if (hidden.size === 0) {
    return state;
  }
  return {
    ...state,
    sessions: state.sessions.filter((session) => !hidden.has(session.id)),
    agents: state.agents.filter((agent) => !hidden.has(agent.session_id)),
  };
}

/** 상태에 실린 관리자 설정으로 활동 없는 세션을 뺀다. */
export function visibleOffice(state: OfficeState, now: number): OfficeState {
  return withoutHidden(state, hiddenSessionIds(state, state.settings?.hide_idle_after_minutes, now));
}

/** 숨김 집합이 바뀌었는지 비교하기 위한 키(시간이 흘러 새로 숨길 때만 다시 그린다). */
export function hiddenKey(hidden: ReadonlySet<string>): string {
  return [...hidden].sort().join(",");
}

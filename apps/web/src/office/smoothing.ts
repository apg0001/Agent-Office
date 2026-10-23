/**
 * 이벤트 폭주 병합. Read·Edit가 연달아 오면 캐릭터가 구역 사이를 왕복하지 않도록 목적지 변경에
 * 최소 체류 시간을 둔다. 중요한 전환(승인 대기·유휴·종료)은 즉시 반영한다.
 * 사실 상태는 그대로이고, 화면 목적지만 늦춘다.
 */

import type { ZoneKind } from "./grid";
import type { AgentState } from "./types";
import { zoneForActivity } from "./behavior";

export const MIN_DWELL_MS = 1500;

export interface DisplayedZone {
  zone: ZoneKind;
  since: number;
}

function isUrgent(agent: AgentState): boolean {
  return (
    agent.turn_state === "WAITING_USER" ||
    agent.turn_state === "IDLE" ||
    agent.ended_at !== null
  );
}

/** 다음에 화면에 보일 구역. 직전 표시 구역과 시각을 받아 결정적으로 계산한다. */
export function nextDisplayedZone(
  previous: DisplayedZone | undefined,
  agent: AgentState,
  now: number,
): DisplayedZone {
  const wanted = zoneForActivity(agent.activity, agent.turn_state);
  if (!previous || previous.zone === wanted) {
    return previous ?? { zone: wanted, since: now };
  }
  if (isUrgent(agent) || now - previous.since >= MIN_DWELL_MS) {
    return { zone: wanted, since: now };
  }
  return previous;
}

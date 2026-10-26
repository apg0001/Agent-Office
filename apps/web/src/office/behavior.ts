/**
 * 사실 상태 → 목적지 구역. 연출은 이 결과를 따라 움직일 뿐 사실을 바꾸지 않는다.
 * 정식 책상 배정·재사용은 #48, 경로 탐색은 #49, 세부 연출은 #47·#50에서 확장한다.
 */

import type { OfficeMap, Point, ZoneKind } from "./grid";
import { isWalkable, walkableTiles } from "./grid";
import { mixHash } from "./presence";
import type { Activity, AgentState, OfficeState, TurnState } from "./types";

export function zoneForActivity(activity: Activity, turnState: TurnState): ZoneKind {
  if (turnState === "IDLE") {
    return "lounge";
  }
  switch (activity) {
    case "reading":
      return "library";
    case "browsing":
      return "internet";
    case "testing":
      return "testing";
    case "planning":
      return "meeting";
    default:
      // 코딩·생각·승인 대기·위임·보고·알 수 없음은 자기 자리에서 한다.
      return "desk";
  }
}

export const DESKS_PER_ROW = 7;

/** 책상 번호를 모두 담을 업무석 줄 수(최소 2줄). 필요할 때만 늘어난다. */
export function deskRowsFor(state: OfficeState): number {
  const highest = Math.max(-1, ...state.sessions.map((session) => session.desk_slot ?? -1));
  return Math.max(2, Math.ceil((highest + 1) / DESKS_PER_ROW));
}

/** 세션별 자리. 서버가 배정한 책상 번호를 그대로 쓴다(새로고침·다른 브라우저에서도 같은 자리). */
export function assignSeats(map: OfficeMap, state: OfficeState): Map<string, Point> {
  const seats = new Map<string, Point>();
  for (const session of state.sessions) {
    if (session.desk_slot === null) {
      continue;
    }
    const seat = map.seats[session.desk_slot];
    if (seat) {
      seats.set(session.id, seat);
    }
  }
  return seats;
}

/** 에이전트의 목적지 칸. 자리 구역이면 세션 자리, 그 밖은 구역 안 칸을 에이전트별로 고정 분산. */
export function targetTile(
  map: OfficeMap,
  agent: AgentState,
  seats: Map<string, Point>,
  zone: ZoneKind = zoneForActivity(agent.activity, agent.turn_state),
  variant = 0,
  /** 같은 세션 하위 에이전트 중 몇 번째인지(subagentIndexOf). */
  subagentIndex = 0,
): Point {
  const seat = seats.get(agent.session_id);
  if (zone === "desk" && seat) {
    if (agent.role === "lead") {
      return seat;
    }
    // 하위 에이전트는 대장 주변 칸에 하나씩 나눠 선다(같은 칸에 겹쳐 하나로 보이지 않게, #182).
    return subagentSpot(map, seat, subagentIndex);
  }
  const tiles = walkableTiles(map, zone === "desk" ? "corridor" : zone);
  if (tiles.length === 0) {
    return map.entrance;
  }
  return tiles[(mixHash(agent.id) + variant) % tiles.length] ?? map.entrance;
}

/** 대장 자리 주변에서 하위 에이전트가 설 칸 후보(가까운 순). 책상 칸은 걸을 수 없어 거른다. */
const SUBAGENT_OFFSETS: ReadonlyArray<Point> = [
  { x: 1, y: 0 },
  { x: 0, y: 1 },
  { x: 1, y: 1 },
  { x: -1, y: 1 },
  { x: -1, y: 0 },
  { x: 2, y: 1 },
  { x: -2, y: 1 },
  { x: 0, y: 2 },
];

export function subagentSpot(map: OfficeMap, seat: Point, index: number): Point {
  const spots = SUBAGENT_OFFSETS.map((o) => ({ x: seat.x + o.x, y: seat.y + o.y })).filter((p) =>
    isWalkable(map, p),
  );
  if (spots.length === 0) {
    return seat;
  }
  return spots[index % spots.length]!;
}

/** 같은 세션의 살아 있는 하위 에이전트 중 이 에이전트의 순번(ID 순이라 매 프레임 같다). */
export function subagentIndexOf(state: OfficeState, agent: AgentState): number {
  if (agent.role !== "subagent") {
    return 0;
  }
  const siblings = state.agents
    .filter((a) => a.session_id === agent.session_id && a.role === "subagent" && a.ended_at === null)
    .map((a) => a.id)
    .sort();
  return Math.max(0, siblings.indexOf(agent.id));
}

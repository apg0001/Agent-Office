/**
 * 사실 상태 → 목적지 구역. 연출은 이 결과를 따라 움직일 뿐 사실을 바꾸지 않는다.
 * 정식 책상 배정·재사용은 #48, 경로 탐색은 #49, 세부 연출은 #47·#50에서 확장한다.
 */

import type { OfficeMap, Point, ZoneKind } from "./grid";
import { canStep, isWalkable, tileAt, walkableTiles } from "./grid";
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

export interface TargetWant {
  id: string;
  point: Point;
  /** 서버가 정한 고유 자리(대장 책상)라 다른 캐릭터에게 양보하지 않는다. */
  fixed: boolean;
}

/**
 * 캐릭터끼리 같은 칸에 서지 않게 목적지를 나눈다(#194). 고정 자리를 먼저 잡고, 나머지는 ID 순으로
 * 원하는 칸이 비어 있으면 그대로, 차 있으면 같은 구역 안에서 걸어서 가장 가까운 빈 칸을 준다
 * (같은 구역에 빈 칸이 없으면 아무 구역이나). 매번 같은 입력이면 같은 결과다.
 */
export function spreadTargets(map: OfficeMap, wants: TargetWant[]): Map<string, Point> {
  const key = (p: Point) => `${p.x},${p.y}`;
  const taken = new Set<string>();
  const result = new Map<string, Point>();
  const ordered = [...wants].sort((a, b) => Number(b.fixed) - Number(a.fixed) || a.id.localeCompare(b.id));
  for (const want of ordered) {
    if (want.fixed || !taken.has(key(want.point))) {
      taken.add(key(want.point));
      result.set(want.id, want.point);
      continue;
    }
    const zone = tileAt(map, want.point)?.zone ?? null;
    const free = nearestFree(map, want.point, taken, zone) ?? nearestFree(map, want.point, taken, undefined);
    const chosen = free ?? want.point;
    taken.add(key(chosen));
    result.set(want.id, chosen);
  }
  return result;
}

/** start에서 걸어서(칸막이·가구 피함) 가장 가까운 빈 칸. zone을 주면 그 구역 칸만 고른다. */
function nearestFree(
  map: OfficeMap,
  start: Point,
  taken: Set<string>,
  zone: string | null | undefined,
): Point | undefined {
  const key = (p: Point) => `${p.x},${p.y}`;
  const seen = new Set([key(start)]);
  const queue = [start];
  while (queue.length > 0) {
    const here = queue.shift()!;
    for (const [dx, dy] of [[1, 0], [-1, 0], [0, 1], [0, -1]] as const) {
      const next = { x: here.x + dx, y: here.y + dy };
      const id = key(next);
      if (seen.has(id) || !canStep(map, here, next)) {
        continue;
      }
      seen.add(id);
      queue.push(next);
      if (!taken.has(id) && isWalkable(map, next) && (zone === undefined || tileAt(map, next)?.zone === zone)) {
        return next;
      }
    }
  }
  return undefined;
}

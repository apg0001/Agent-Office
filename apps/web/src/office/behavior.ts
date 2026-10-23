/**
 * 사실 상태 → 목적지 구역. 연출은 이 결과를 따라 움직일 뿐 사실을 바꾸지 않는다.
 * 정식 책상 배정·재사용은 #48, 경로 탐색은 #49, 세부 연출은 #47·#50에서 확장한다.
 */

import type { OfficeMap, Point, ZoneKind } from "./grid";
import { walkableTiles } from "./grid";
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

/** 세션별 자리(임시 규칙): 세션 ID 정렬 순서대로 빈 자리를 채운다. #48에서 재사용 규칙으로 교체. */
export function assignSeats(map: OfficeMap, state: OfficeState): Map<string, Point> {
  const seats = new Map<string, Point>();
  const sessions = [...state.sessions]
    .filter((session) => session.lifecycle !== "ENDED")
    .sort((a, b) => a.id.localeCompare(b.id));
  sessions.forEach((session, index) => {
    const seat = map.seats[index];
    if (seat) {
      seats.set(session.id, seat);
    }
  });
  return seats;
}

/** 에이전트의 목적지 칸. 자리 구역이면 세션 자리, 그 밖은 구역 안 칸을 에이전트별로 고정 분산. */
export function targetTile(
  map: OfficeMap,
  agent: AgentState,
  seats: Map<string, Point>,
): Point {
  const zone = zoneForActivity(agent.activity, agent.turn_state);
  const seat = seats.get(agent.session_id);
  if (zone === "desk" && seat) {
    if (agent.role === "lead") {
      return seat;
    }
    // 하위 에이전트는 대장 옆 임시 작업 공간(같은 줄 바로 옆 칸)에 선다.
    return { x: seat.x + 1, y: seat.y };
  }
  const tiles = walkableTiles(map, zone === "desk" ? "corridor" : zone);
  if (tiles.length === 0) {
    return map.entrance;
  }
  return tiles[hash(agent.id) % tiles.length] ?? map.entrance;
}

function hash(value: string): number {
  let result = 0;
  for (let index = 0; index < value.length; index += 1) {
    result = (result * 31 + value.charCodeAt(index)) >>> 0;
  }
  return result;
}

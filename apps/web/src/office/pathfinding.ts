/** 논리 그리드 A* 경로 탐색(4방향). 가구·벽을 피한다. */

import type { OfficeMap, Point } from "./grid";
import { isWalkable } from "./grid";

const NEIGHBORS: ReadonlyArray<Point> = [
  { x: 1, y: 0 },
  { x: -1, y: 0 },
  { x: 0, y: 1 },
  { x: 0, y: -1 },
];

const key = (point: Point) => `${point.x},${point.y}`;
const manhattan = (a: Point, b: Point) => Math.abs(a.x - b.x) + Math.abs(a.y - b.y);

/** 목적지가 막혀 있으면 가장 가까운 걸을 수 있는 칸으로 바꾼다(예: 가구 앞). */
export function nearestWalkable(map: OfficeMap, goal: Point): Point | undefined {
  if (isWalkable(map, goal)) {
    return goal;
  }
  let best: Point | undefined;
  let bestDistance = Number.POSITIVE_INFINITY;
  for (const tile of map.tiles) {
    if (!tile.walkable) {
      continue;
    }
    const distance = manhattan(tile, goal);
    if (distance < bestDistance) {
      best = { x: tile.x, y: tile.y };
      bestDistance = distance;
    }
  }
  return best;
}

/**
 * 시작 칸에서 목적지까지의 칸 목록(시작 칸 제외, 목적지 포함). 도달할 수 없으면 빈 배열.
 * 시작 칸이 막혀 있으면(가구 위에 서 있는 연출 중 등) 가장 가까운 칸에서 출발한다.
 */
export function findPath(map: OfficeMap, start: Point, goal: Point): Point[] {
  const from = nearestWalkable(map, start);
  const to = nearestWalkable(map, goal);
  if (!from || !to) {
    return [];
  }
  if (from.x === to.x && from.y === to.y) {
    return from.x === start.x && from.y === start.y ? [] : [to];
  }
  const open: Point[] = [from];
  const cameFrom = new Map<string, Point>();
  const cost = new Map<string, number>([[key(from), 0]]);
  const score = new Map<string, number>([[key(from), manhattan(from, to)]]);
  const closed = new Set<string>();

  while (open.length > 0) {
    open.sort((a, b) => (score.get(key(a)) ?? 0) - (score.get(key(b)) ?? 0));
    const current = open.shift()!;
    const currentKey = key(current);
    if (current.x === to.x && current.y === to.y) {
      const path: Point[] = [current];
      let cursor = cameFrom.get(currentKey);
      while (cursor && key(cursor) !== key(from)) {
        path.unshift(cursor);
        cursor = cameFrom.get(key(cursor));
      }
      if (from.x !== start.x || from.y !== start.y) {
        path.unshift(from);
      }
      return path;
    }
    closed.add(currentKey);
    for (const offset of NEIGHBORS) {
      const next = { x: current.x + offset.x, y: current.y + offset.y };
      const nextKey = key(next);
      if (closed.has(nextKey) || !isWalkable(map, next)) {
        continue;
      }
      const tentative = (cost.get(currentKey) ?? 0) + 1;
      if (tentative < (cost.get(nextKey) ?? Number.POSITIVE_INFINITY)) {
        cameFrom.set(nextKey, current);
        cost.set(nextKey, tentative);
        score.set(nextKey, tentative + manhattan(next, to));
        if (!open.some((point) => key(point) === nextKey)) {
          open.push(next);
        }
      }
    }
  }
  return [];
}

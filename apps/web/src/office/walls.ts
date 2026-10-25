/**
 * 뒤쪽 바깥 벽(#153). 그림을 쓰면 Kenney 렌더 각도(30°)와 우리 2:1 바닥이 달라 이음매가 어긋나므로
 * 바닥과 같은 투영으로 직접 계산한다. 순수 함수라 Phaser 없이 이어짐을 테스트한다.
 */

import type { OfficeMap, Point } from "./grid";
import { type ScreenPoint, TILE_HEIGHT, TILE_WIDTH, gridToScreen } from "./iso";

export const WALL_HEIGHT = 56;
/** 몇 칸마다 창문을 낼지. */
export const WINDOW_EVERY = 3;

export type WallSide = "left" | "right";

export interface WallSegment {
  tile: Point;
  /** left: x = 0 칸의 왼쪽 위 변, right: y = 0 칸의 오른쪽 위 변. */
  side: WallSide;
  window: boolean;
}

/** 출입구·복도 끝은 열어 둔다. */
function isOpening(map: OfficeMap, point: Point): boolean {
  const zone = map.tiles[point.y * map.width + point.x]?.zone;
  return zone === "entrance" || zone === "corridor";
}

export function wallSegments(map: OfficeMap): WallSegment[] {
  const segments: WallSegment[] = [];
  for (let y = 0; y < map.height; y += 1) {
    if (!isOpening(map, { x: 0, y })) {
      segments.push({ tile: { x: 0, y }, side: "left", window: y % WINDOW_EVERY === 1 });
    }
  }
  for (let x = 0; x < map.width; x += 1) {
    if (!isOpening(map, { x, y: 0 })) {
      segments.push({ tile: { x, y: 0 }, side: "right", window: x % WINDOW_EVERY === 1 });
    }
  }
  return segments;
}

/** 벽 밑변의 두 끝(바닥 위). 왼쪽 벽은 칸의 왼쪽 꼭짓점→위 꼭짓점, 오른쪽 벽은 위→오른쪽. */
export function wallBase(segment: WallSegment): [ScreenPoint, ScreenPoint] {
  const c = gridToScreen(segment.tile);
  const top = { sx: c.sx, sy: c.sy - TILE_HEIGHT / 2 };
  return segment.side === "left"
    ? [{ sx: c.sx - TILE_WIDTH / 2, sy: c.sy }, top]
    : [top, { sx: c.sx + TILE_WIDTH / 2, sy: c.sy }];
}

/** 밑변 위 비율 t(0~1), 바닥에서 높이 h인 점. */
export function wallPoint(segment: WallSegment, t: number, h: number): ScreenPoint {
  const [a, b] = wallBase(segment);
  return { sx: a.sx + (b.sx - a.sx) * t, sy: a.sy + (b.sy - a.sy) * t - h };
}

/** 사각형 면(밑변 t0~t1, 높이 h0~h1)의 네 꼭짓점. */
export function wallQuad(segment: WallSegment, t0: number, t1: number, h0: number, h1: number): ScreenPoint[] {
  return [
    wallPoint(segment, t0, h0),
    wallPoint(segment, t1, h0),
    wallPoint(segment, t1, h1),
    wallPoint(segment, t0, h1),
  ];
}

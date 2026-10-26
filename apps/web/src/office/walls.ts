/**
 * 뒤쪽 바깥 벽(#153). 벽 그림은 조각 이음매가 맞지 않아 바닥과 같은 투영(iso.ts)으로 직접 계산한다. 순수 함수라 Phaser 없이 이어짐을 테스트한다.
 */

import type { OfficeMap, Partition, Point } from "./grid";
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
  /** 출입문(출입구 칸의 바깥 벽). */
  door: boolean;
}

export function wallSegments(map: OfficeMap): WallSegment[] {
  const segments: WallSegment[] = [];
  const isEntrance = (p: Point) => p.x === map.entrance.x && p.y === map.entrance.y;
  for (let y = 0; y < map.height; y += 1) {
    const tile = { x: 0, y };
    const door = isEntrance(tile);
    segments.push({ tile, side: "left", window: !door && y % WINDOW_EVERY === 1, door });
  }
  for (let x = 0; x < map.width; x += 1) {
    const tile = { x, y: 0 };
    const door = isEntrance(tile);
    segments.push({ tile, side: "right", window: !door && x % WINDOW_EVERY === 1, door });
  }
  return segments;
}

/** 칸막이 높이. 바깥 벽보다 낮아 뒤쪽 캐릭터 머리가 보인다. */
export const PARTITION_HEIGHT = 30;

/** 칸막이 밑변의 두 끝: 두 칸이 맞닿은 변(앞쪽 칸 쪽에서 본 면). */
export function partitionBase(partition: Partition): [ScreenPoint, ScreenPoint] {
  const [a, b] =
    partition.a.x + partition.a.y <= partition.b.x + partition.b.y
      ? [partition.a, partition.b]
      : [partition.b, partition.a];
  const c = gridToScreen(a);
  const bottom = { sx: c.sx, sy: c.sy + TILE_HEIGHT / 2 };
  // b가 x + 1이면 a의 오른쪽 아래 변, y + 1이면 왼쪽 아래 변.
  return b.x > a.x
    ? [{ sx: c.sx + TILE_WIDTH / 2, sy: c.sy }, bottom]
    : [{ sx: c.sx - TILE_WIDTH / 2, sy: c.sy }, bottom];
}

/** 칸막이의 앞쪽 칸(화면에서 더 가까운 칸). 그리기 깊이에 쓴다. */
export function partitionFront(partition: Partition): Point {
  return partition.a.x + partition.a.y > partition.b.x + partition.b.y ? partition.a : partition.b;
}

export function partitionQuad(partition: Partition, t0: number, t1: number, h0: number, h1: number): ScreenPoint[] {
  const [a, b] = partitionBase(partition);
  const at = (t: number, h: number) => ({ sx: a.sx + (b.sx - a.sx) * t, sy: a.sy + (b.sy - a.sy) * t - h });
  return [at(t0, h0), at(t1, h0), at(t1, h1), at(t0, h1)];
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

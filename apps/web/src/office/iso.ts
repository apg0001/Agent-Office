/**
 * 논리 그리드 ↔ 아이소메트릭 화면 좌표 투영.
 * 바닥 마름모 기울기는 가구 그림(Kenney Furniture Kit 렌더)과 같아야 한다. 그 바닥 타일 윗면을
 * 픽셀로 재면 기울기가 약 0.69~0.71이라 64×44를 쓴다(2:1인 64×32를 쓰면 가구가 약 10° 틀어져 보인다, #153).
 */

import type { Point } from "./grid";

export const TILE_WIDTH = 64;
export const TILE_HEIGHT = 44;

export interface ScreenPoint {
  sx: number;
  sy: number;
}

export function gridToScreen(point: Point): ScreenPoint {
  return {
    sx: ((point.x - point.y) * TILE_WIDTH) / 2,
    sy: ((point.x + point.y) * TILE_HEIGHT) / 2,
  };
}

/** 화면 좌표를 실수 그리드 좌표로 되돌린다. 칸을 고를 때는 `Math.floor`한다. */
export function screenToGrid(screen: ScreenPoint): Point {
  const a = screen.sx / (TILE_WIDTH / 2);
  const b = screen.sy / (TILE_HEIGHT / 2);
  return { x: (a + b) / 2, y: (b - a) / 2 };
}

/** 뒤(위)에 있는 것부터 그리도록 깊이를 정한다. 같은 칸에서는 높이가 높은 것이 앞이다. */
export function depthOf(point: Point, layer = 0): number {
  return (point.x + point.y) * 10 + layer;
}

/** 타일 마름모의 네 꼭짓점(화면 좌표). */
export function tileDiamond(point: Point): ScreenPoint[] {
  const center = gridToScreen(point);
  return [
    { sx: center.sx, sy: center.sy - TILE_HEIGHT / 2 },
    { sx: center.sx + TILE_WIDTH / 2, sy: center.sy },
    { sx: center.sx, sy: center.sy + TILE_HEIGHT / 2 },
    { sx: center.sx - TILE_WIDTH / 2, sy: center.sy },
  ];
}

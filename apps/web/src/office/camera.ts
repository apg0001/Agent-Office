/**
 * 카메라 규칙(순수). 수동 조작은 자동 초점을 멈추고, 사용자가 다시 켤 때까지 유지한다.
 */

import type { Point } from "./grid";

export const MIN_ZOOM = 0.5;
export const MAX_ZOOM = 2.5;

export interface CameraState {
  autoFocus: boolean;
  zoom: number;
}

export const initialCamera: CameraState = { autoFocus: true, zoom: 1 };

export function onManualControl(state: CameraState): CameraState {
  return state.autoFocus ? { ...state, autoFocus: false } : state;
}

export function resumeAutoFocus(state: CameraState): CameraState {
  return { ...state, autoFocus: true };
}

export function zoomBy(state: CameraState, factor: number): CameraState {
  const zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, state.zoom * factor));
  return { ...onManualControl(state), zoom };
}

/** 두 손가락 사이 거리 변화만큼 확대·축소한다(폰 핀치, #255). 이전 거리가 없으면 그대로. */
export function pinchZoom(state: CameraState, previous: number, current: number): CameraState {
  return previous > 0 && current > 0 ? zoomBy(state, current / previous) : state;
}

/**
 * 전체 화면이 아닐 때 지도 높이(#255). 화면 높이의 72%를 쓰되 300~560px로 맞춘다: 데스크톱은 560px,
 * 가로로 눕힌 폰은 300px. 작은 지도(compact)는 320px 고정. CSS `.office-canvas` 최소 높이와 같은 규칙.
 */
export function mapHeight(compact: boolean, viewportHeight: number): number {
  return compact ? 320 : Math.round(Math.min(560, Math.max(300, viewportHeight * 0.72)));
}

/** 자동 초점 대상: 활동 중인 캐릭터들의 중심, 없으면 사무실 중앙. */
export function focusPoint(active: Point[], fallback: Point): Point {
  if (active.length === 0) {
    return fallback;
  }
  const sum = active.reduce((acc, point) => ({ x: acc.x + point.x, y: acc.y + point.y }), {
    x: 0,
    y: 0,
  });
  return { x: sum.x / active.length, y: sum.y / active.length };
}

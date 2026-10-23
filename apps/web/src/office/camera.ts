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

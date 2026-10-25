import { describe, expect, it } from "vitest";

import {
  MAX_ZOOM,
  MIN_ZOOM,
  focusPoint,
  initialCamera,
  onManualControl,
  resumeAutoFocus,
  zoomBy,
} from "./camera";
import { createDefaultOffice, isWalkable } from "./grid";
import { findPath, nearestWalkable } from "./pathfinding";

describe("pathfinding", () => {
  const map = createDefaultOffice(2);

  it("walks from the entrance to every seat without crossing obstacles", () => {
    for (const seat of map.seats) {
      const path = findPath(map, map.entrance, seat);
      expect(path.length, `${seat.x},${seat.y}`).toBeGreaterThan(0);
      expect(path.at(-1)).toEqual(seat);
      for (const step of path) {
        expect(isWalkable(map, step)).toBe(true);
      }
      // 4방향 한 칸씩만 이동한다.
      let previous = map.entrance;
      for (const step of path) {
        expect(Math.abs(step.x - previous.x) + Math.abs(step.y - previous.y)).toBe(1);
        previous = step;
      }
    }
  });

  it("detours around furniture instead of passing through it", () => {
    const desk = map.tiles.find((tile) => tile.furniture === "desk")!;
    const left = { x: desk.x - 1, y: desk.y };
    const right = { x: desk.x + 1, y: desk.y };
    const path = findPath(map, left, right);
    expect(path.length).toBeGreaterThan(2);
    expect(path.some((step) => step.x === desk.x && step.y === desk.y)).toBe(false);
  });

  it("redirects blocked goals to the nearest walkable tile", () => {
    const shelf = map.tiles.find((tile) => tile.furniture === "shelf")!;
    const goal = nearestWalkable(map, shelf)!;
    expect(isWalkable(map, goal)).toBe(true);
    expect(findPath(map, map.entrance, shelf).at(-1)).toEqual(goal);
  });

  it("returns an empty path when already there", () => {
    expect(findPath(map, map.entrance, map.entrance)).toEqual([]);
  });
});

describe("camera", () => {
  it("pauses auto focus on manual control until resumed", () => {
    const manual = onManualControl(initialCamera);
    expect(manual.autoFocus).toBe(false);
    expect(onManualControl(manual)).toBe(manual);
    expect(resumeAutoFocus(manual).autoFocus).toBe(true);
  });

  it("clamps zoom and treats zooming as manual control", () => {
    let state = initialCamera;
    for (let index = 0; index < 50; index += 1) {
      state = zoomBy(state, 1.5);
    }
    expect(state.zoom).toBe(MAX_ZOOM);
    expect(state.autoFocus).toBe(false);
    for (let index = 0; index < 50; index += 1) {
      state = zoomBy(state, 0.5);
    }
    expect(state.zoom).toBe(MIN_ZOOM);
  });

  it("focuses on the centroid of active characters", () => {
    expect(focusPoint([], { x: 8, y: 7 })).toEqual({ x: 8, y: 7 });
    expect(
      focusPoint(
        [
          { x: 0, y: 0 },
          { x: 4, y: 2 },
        ],
        { x: 8, y: 7 },
      ),
    ).toEqual({ x: 2, y: 1 });
  });
});

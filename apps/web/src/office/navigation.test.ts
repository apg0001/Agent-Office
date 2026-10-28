import { describe, expect, it } from "vitest";

import {
  MAX_ZOOM,
  MIN_ZOOM,
  focusPoint,
  initialCamera,
  mapHeight,
  onManualControl,
  pinchZoom,
  resumeAutoFocus,
  zoomBy,
} from "./camera";
import { canStep, createDefaultOffice, isBlockedEdge, isWalkable } from "./grid";
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

  it("zooms by the change in pinch distance", () => {
    expect(pinchZoom(initialCamera, 0, 120)).toBe(initialCamera);
    const wider = pinchZoom(initialCamera, 100, 150);
    expect(wider.zoom).toBeCloseTo(1.5);
    expect(wider.autoFocus).toBe(false);
    expect(pinchZoom(wider, 150, 100).zoom).toBeCloseTo(1);
  });

  it("sizes the map to the viewport height within bounds", () => {
    expect(mapHeight(false, 1000)).toBe(560);
    expect(mapHeight(false, 390)).toBe(300);
    expect(mapHeight(false, 600)).toBe(432);
    expect(mapHeight(true, 1000)).toBe(320);
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

describe("partitions and doors (#164)", () => {
  for (const rows of [2, 3]) {
    const map = createDefaultOffice(rows);

    it(`reaches every walkable tile from the entrance through doors only (${rows} desk rows)`, () => {
      const seen = new Set([`${map.entrance.x},${map.entrance.y}`]);
      const queue = [map.entrance];
      while (queue.length > 0) {
        const here = queue.shift()!;
        for (const [dx, dy] of [[1, 0], [-1, 0], [0, 1], [0, -1]] as const) {
          const next = { x: here.x + dx, y: here.y + dy };
          const id = `${next.x},${next.y}`;
          if (!seen.has(id) && canStep(map, here, next)) {
            seen.add(id);
            queue.push(next);
          }
        }
      }
      const unreachable = map.tiles.filter((t) => t.walkable && !seen.has(`${t.x},${t.y}`));
      expect(unreachable.map((t) => `${t.zone}@${t.x},${t.y}`)).toEqual([]);
    });

    it(`never steps through a wall segment (${rows} desk rows)`, () => {
      const walls = map.partitions.filter((p) => !p.door);
      expect(walls.length).toBeGreaterThan(20);
      expect(map.partitions.filter((p) => p.door).length).toBe(5);
      for (const wall of walls) {
        expect(isBlockedEdge(map, wall.a, wall.b)).toBe(true);
        expect(isBlockedEdge(map, wall.b, wall.a)).toBe(true);
      }
      for (const zone of ["library", "internet", "testing", "meeting", "lounge"] as const) {
        const tiles = map.tiles.filter((t) => t.zone === zone && t.walkable);
        const target = tiles[tiles.length - 1]!;
        const path = findPath(map, map.entrance, target);
        expect(path.at(-1), zone).toEqual({ x: target.x, y: target.y });
        let previous = map.entrance;
        for (const step of path) {
          expect(isBlockedEdge(map, previous, step), `${zone} ${step.x},${step.y}`).toBe(false);
          previous = step;
        }
      }
    });
  }
});

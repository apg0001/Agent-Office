import { describe, expect, it } from "vitest";

import { createDefaultOffice } from "./grid";
import { WALL_HEIGHT, wallBase, wallPoint, wallSegments } from "./walls";

describe("back walls (#153)", () => {
  const map = createDefaultOffice(2);
  const segments = wallSegments(map);

  it("runs along both back edges and leaves the corridor and entrance open", () => {
    const left = segments.filter((s) => s.side === "left");
    const right = segments.filter((s) => s.side === "right");
    expect(right).toHaveLength(map.width);
    expect(left.length).toBeGreaterThan(0);
    for (const segment of left) {
      const zone = map.tiles[segment.tile.y * map.width]!.zone;
      expect(zone === "corridor" || zone === "entrance").toBe(false);
    }
  });

  it("joins neighbouring segments exactly so the wall is continuous", () => {
    for (const side of ["left", "right"] as const) {
      const run = segments
        .filter((s) => s.side === side)
        .sort((a, b) => (side === "left" ? b.tile.y - a.tile.y : a.tile.x - b.tile.x));
      for (let i = 1; i < run.length; i += 1) {
        const previous = run[i - 1]!;
        const current = run[i]!;
        const adjacent = side === "left"
          ? previous.tile.y - current.tile.y === 1
          : current.tile.x - previous.tile.x === 1;
        if (adjacent) {
          expect(wallBase(current)[0]).toEqual(wallBase(previous)[1]);
        }
      }
    }
    // 두 벽은 (0, 0) 칸의 위 꼭짓점에서 만난다.
    const corner = segments.filter((s) => s.tile.x === 0 && s.tile.y === 0);
    expect(wallBase(corner.find((s) => s.side === "left")!)[1]).toEqual(
      wallBase(corner.find((s) => s.side === "right")!)[0],
    );
  });

  it("follows the 2:1 floor slope", () => {
    for (const segment of segments) {
      const [a, b] = wallBase(segment);
      expect(Math.abs((b.sy - a.sy) / (b.sx - a.sx))).toBeCloseTo(0.5);
      expect(wallPoint(segment, 0, WALL_HEIGHT).sy).toBe(a.sy - WALL_HEIGHT);
    }
  });
});

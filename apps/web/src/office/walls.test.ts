import { describe, expect, it } from "vitest";

import { createDefaultOffice } from "./grid";
import { TILE_HEIGHT, TILE_WIDTH, gridToScreen } from "./iso";
import { WALL_HEIGHT, partitionBase, partitionFront, wallBase, wallPoint, wallSegments } from "./walls";

describe("back walls (#153)", () => {
  const map = createDefaultOffice(2);
  const segments = wallSegments(map);

  it("runs along both back edges with a door at the entrance", () => {
    expect(segments.filter((s) => s.side === "right")).toHaveLength(map.width);
    expect(segments.filter((s) => s.side === "left")).toHaveLength(map.height);
    const doors = segments.filter((s) => s.door);
    expect(doors).toHaveLength(1);
    expect(doors[0]!.tile).toEqual(map.entrance);
    expect(doors[0]!.window).toBe(false);
  });

  it("puts every partition on the edge shared by its two tiles", () => {
    for (const partition of map.partitions) {
      const [p, q] = partitionBase(partition);
      expect(Math.abs((q.sy - p.sy) / (q.sx - p.sx))).toBeCloseTo(TILE_HEIGHT / TILE_WIDTH);
      const front = partitionFront(partition);
      const c = gridToScreen(front);
      // 앞쪽 칸의 위쪽 두 변 중 하나다.
      expect(Math.max(p.sy, q.sy)).toBeCloseTo(c.sy);
      expect(Math.min(p.sy, q.sy)).toBeCloseTo(c.sy - TILE_HEIGHT / 2);
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

  it("follows the floor slope", () => {
    for (const segment of segments) {
      const [a, b] = wallBase(segment);
      expect(Math.abs((b.sy - a.sy) / (b.sx - a.sx))).toBeCloseTo(TILE_HEIGHT / TILE_WIDTH);
      expect(wallPoint(segment, 0, WALL_HEIGHT).sy).toBe(a.sy - WALL_HEIGHT);
    }
  });
});

import { describe, expect, it } from "vitest";

import { FURNITURE_ART, artImages } from "./furnitureArt";
import { createDefaultOffice } from "./grid";
import { LAB_PIXEL_ART, labColor } from "./lab";

// public/office/kenney의 실제 파일 목록(불러오지 않고 경로만 본다)과 라이선스 본문.
const shipped = Object.keys(import.meta.glob("../../public/office/kenney/*.png"))
  .map((path) => path.replace(/^.*\/(.+)\.png$/, "$1"))
  .sort();
const license = Object.values(
  import.meta.glob<string>("../../public/office/kenney/License.txt", {
    eager: true,
    query: "?raw",
    import: "default",
  }),
)[0];

describe("furniture art (#153)", () => {
  it("has art for every furniture placed in the default office", () => {
    for (const tile of createDefaultOffice(3).tiles) {
      if (tile.furniture) {
        expect(FURNITURE_ART[tile.furniture].length, tile.furniture).toBeGreaterThan(0);
      }
    }
  });

  it("ships exactly the images it uses, with the CC0 license", () => {
    expect(shipped).toEqual(artImages());
    expect(license).toContain("Creative Commons Zero, CC0");
  });

  it("keeps decorations on walkable neighbours only", () => {
    const map = createDefaultOffice(2);
    for (const tile of map.tiles) {
      for (const piece of tile.furniture ? FURNITURE_ART[tile.furniture] : []) {
        if (piece.cell) {
          const at = map.tiles[(tile.y + piece.cell.y) * map.width + tile.x + piece.cell.x];
          expect(at?.walkable, `${tile.furniture} at ${tile.x},${tile.y}`).toBe(true);
        }
      }
    }
  });
});

describe("testing lab (#235)", () => {
  it("places lab benches without blocking the door or cutting off the room", () => {
    const map = createDefaultOffice(2);
    const lab = map.tiles.filter((t) => t.zone === "testing");
    expect(lab.filter((t) => t.furniture === "labBench" || t.furniture === "labScope")).toHaveLength(2);
    // 문 칸(testing.x + 1, 첫 줄)은 비어 있다.
    const door = map.tiles[map.zones.testing.y * map.width + map.zones.testing.x + 1];
    expect(door?.walkable).toBe(true);
    expect(lab.filter((t) => t.walkable).length).toBeGreaterThanOrEqual(6);
  });

  it("draws every lab pixel with a known colour and equal-width rows", () => {
    for (const pieces of Object.values(LAB_PIXEL_ART)) {
      for (const piece of pieces ?? []) {
        const width = piece.rows[0]!.length;
        for (const row of piece.rows) {
          expect(row.length).toBe(width);
          for (const code of row) {
            expect(code === "." || labColor(code) !== null, code).toBe(true);
          }
        }
      }
    }
  });
});

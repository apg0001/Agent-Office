import { describe, expect, it } from "vitest";

import { FURNITURE_ART, artImages } from "./furnitureArt";
import { createDefaultOffice } from "./grid";

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

import { describe, expect, it } from "vitest";

import { createSyntheticOffice } from "./synthetic";
import {
  SHIRT_COLORS,
  labelFor,
  pixelColor,
  shirtIndex,
  spriteRows,
  textureKey,
  walkFrame,
} from "./sprites";
import type { DeviceState } from "./types";

const office = createSyntheticOffice({ sessions: 1, subagentsPerSession: 1 });
const lead = office.agents.find((a) => a.role === "lead")!;
const sub = office.agents.find((a) => a.role === "subagent")!;

describe("sprites", () => {
  it("draws every frame as a rectangular grid of known codes", () => {
    for (const role of ["lead", "subagent"] as const) {
      for (const frame of [0, 1] as const) {
        const rows = spriteRows(role, frame);
        expect(new Set(rows.map((r) => r.length)).size).toBe(1);
        for (const code of rows.join("")) {
          expect(code === "." || pixelColor(code, 0) !== null).toBe(true);
        }
      }
    }
  });

  it("tells lead and subagent apart by shape, not only colour", () => {
    const leadRows = spriteRows("lead", 0);
    const subRows = spriteRows("subagent", 0);
    expect(leadRows.join("")).toContain("k");
    expect(subRows.join("")).not.toContain("k");
    expect(subRows.join("")).toContain("b");
    expect(leadRows.length).toBeGreaterThan(subRows.length);
    expect(spriteRows("lead", 1)).not.toEqual(leadRows);
  });

  it("keeps the shirt colour stable per device and in range", () => {
    expect(shirtIndex("device-a")).toBe(shirtIndex("device-a"));
    const used = new Set(Array.from({ length: 64 }, (_, i) => shirtIndex(`device-${i}`)));
    expect(used.size).toBeGreaterThan(4);
    for (const index of used) {
      expect(index).toBeLessThan(SHIRT_COLORS.length);
    }
    expect(textureKey("lead", 3, 1)).toBe("agent-lead-3-1");
  });

  it("alternates legs only while walking", () => {
    expect(walkFrame(false, 200)).toBe(0);
    expect(walkFrame(true, 100)).toBe(0);
    expect(walkFrame(true, 200)).toBe(1);
  });
});

describe("labelFor", () => {
  const device = (name: string): DeviceState => ({ id: "d", name, connection: "ONLINE" });

  it("uses a short, cleaned PC name for leads", () => {
    expect(labelFor(lead, device("Work PC"))).toBe("Work PC");
    expect(labelFor(lead, device("  아주 긴 이름의 개발용 노트북  "))).toBe("아주 긴 이름의…");
    expect(labelFor(lead, device("a\u0000b\nc"))).toBe("ab c");
    expect(labelFor(lead, device(""))).toBe("리드");
    expect(labelFor(lead, undefined)).toBe("리드");
  });

  it("labels subagents by role only", () => {
    expect(labelFor(sub, device("Work PC"))).toBe("하위");
  });
});

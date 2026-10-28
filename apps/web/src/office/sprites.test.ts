import { describe, expect, it } from "vitest";

import { createSyntheticOffice } from "./synthetic";
import {
  SHIRT_COLORS,
  labelFor,
  lookFor,
  pixelColor,
  shirtForSession,
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
    expect(textureKey("lead", 3, 1)).toBe("agent-lead-plain-3-1");
    expect(textureKey("subagent", 2, 0, "codex")).toBe("agent-subagent-codex-2-0");
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
    expect(labelFor(sub, { ...device("Work PC"), owner_name: "Bob" })).toBe("하위");
  });

  it("shows the person in team and combined views (#206)", () => {
    // 팀 화면: 서버가 이름 자리에도 표시 이름을 보낸다.
    expect(labelFor(lead, { ...device("Bob"), owner_name: "Bob" })).toBe("Bob");
    // 통합의 내 장치: PC 이름 대신 내 이름.
    expect(labelFor(lead, { ...device("alice-pc"), owner_name: "Alice" })).toBe("Alice");
    expect(labelFor(lead, { ...device("pc"), owner_name: "아주 긴 이름의 팀원 사용자" })).toBe("아주 긴 이름의…");
    // 개인 사무실(사람 이름 없음)·빈 이름은 PC 이름으로.
    expect(labelFor(lead, { ...device("Work PC"), owner_name: null })).toBe("Work PC");
    expect(labelFor(lead, { ...device("Work PC"), owner_name: " \n " })).toBe("Work PC");
  });
});

describe("provider looks and session colors (#192)", () => {
  it("gives Claude a cap and Codex glasses on every role and frame", () => {
    for (const role of ["lead", "subagent"] as const) {
      for (const frame of [0, 1] as const) {
        const plain = spriteRows(role, frame).join("");
        const claude = spriteRows(role, frame, "claude").join("");
        const codex = spriteRows(role, frame, "codex").join("");
        expect(plain).not.toMatch(/[ag]/);
        expect(claude).toMatch(/a/);
        expect(codex).toMatch(/g/);
        expect(claude.length).toBe(plain.length);
        expect(codex.length).toBe(plain.length);
        expect(pixelColor("a", 0)).not.toBeNull();
        expect(pixelColor("g", 0)).not.toBeNull();
      }
    }
  });

  it("colors active sessions by desk number so they never collide", () => {
    const colors = Array.from({ length: SHIRT_COLORS.length }, (_, i) =>
      shirtForSession({ id: `s-${i}`, desk_slot: i }, "x"),
    );
    expect(new Set(colors).size).toBe(SHIRT_COLORS.length);
    expect(shirtForSession({ id: "s", desk_slot: null }, "x")).toBe(shirtForSession({ id: "s", desk_slot: null }, "y"));
    expect(lookFor("claude")).toBe("claude");
    expect(lookFor("codex")).toBe("codex");
    expect(lookFor("unknown")).toBe("plain");
  });
});

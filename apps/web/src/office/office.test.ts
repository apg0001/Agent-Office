import { describe, expect, it } from "vitest";

import { assignSeats, deskRowsFor, spreadTargets, targetTile, zoneForActivity } from "./behavior";
import { ZONE_COLORS, ZONE_GUIDE, contains, createDefaultOffice, isWalkable, tileAt, walkableTiles } from "./grid";
import { depthOf, gridToScreen, screenToGrid, tileDiamond } from "./iso";
import { advanceSynthetic, createSyntheticOffice } from "./synthetic";
import type { ZoneKind } from "./grid";
import type { Activity } from "./types";

describe("isometric projection", () => {
  it("round-trips grid points", () => {
    for (const point of [
      { x: 0, y: 0 },
      { x: 3, y: 7 },
      { x: 15, y: 2 },
    ]) {
      const back = screenToGrid(gridToScreen(point));
      expect(back.x).toBeCloseTo(point.x);
      expect(back.y).toBeCloseTo(point.y);
    }
  });

  it("draws farther tiles first", () => {
    expect(depthOf({ x: 1, y: 1 })).toBeLessThan(depthOf({ x: 2, y: 1 }));
    expect(depthOf({ x: 2, y: 2 }, 0)).toBeLessThan(depthOf({ x: 2, y: 2 }, 5));
    expect(tileDiamond({ x: 0, y: 0 })).toHaveLength(4);
  });
});

describe("office grid", () => {
  const map = createDefaultOffice(2);

  it("has every zone with at least one walkable tile", () => {
    const zones: ZoneKind[] = ["desk", "library", "internet", "meeting", "testing", "lounge", "corridor"];
    for (const zone of zones) {
      expect(walkableTiles(map, zone).length, zone).toBeGreaterThan(0);
    }
    expect(isWalkable(map, map.entrance)).toBe(true);
  });

  it("blocks furniture and outer walls", () => {
    const desk = map.tiles.find((tile) => tile.furniture === "desk");
    expect(desk && isWalkable(map, desk)).toBe(false);
    expect(isWalkable(map, { x: 5, y: 0 })).toBe(false);
    expect(tileAt(map, { x: -1, y: 0 })).toBeUndefined();
  });

  it("puts every seat on a walkable desk-zone tile", () => {
    expect(map.seats.length).toBe(14);
    for (const seat of map.seats) {
      expect(isWalkable(map, seat)).toBe(true);
      expect(contains(map.zones.desk, seat)).toBe(true);
    }
  });

  it("grows desk rows without moving other zones relative to desks", () => {
    const larger = createDefaultOffice(3);
    expect(larger.seats.slice(0, map.seats.length)).toEqual(map.seats);
    expect(larger.height).toBe(map.height + 3);
  });
});

describe("behavior", () => {
  const map = createDefaultOffice(2);

  it("maps activities to zones and idle to the lounge", () => {
    expect(zoneForActivity("reading", "WORKING")).toBe("library");
    expect(zoneForActivity("browsing", "WORKING")).toBe("internet");
    expect(zoneForActivity("testing", "WORKING")).toBe("testing");
    expect(zoneForActivity("planning", "WORKING")).toBe("meeting");
    expect(zoneForActivity("coding", "WORKING")).toBe("desk");
    expect(zoneForActivity("waiting_user", "WAITING_USER")).toBe("desk");
    expect(zoneForActivity("coding", "IDLE")).toBe("lounge");
  });

  it("seats leads at their session desk and subagents beside them", () => {
    const state = createSyntheticOffice({ sessions: 2, subagentsPerSession: 1 });
    const coding = {
      ...state,
      agents: state.agents.map((agent) => ({
        ...agent,
        turn_state: "WORKING" as const,
        activity: "coding" as const,
      })),
    };
    const seats = assignSeats(map, coding);
    const [lead, child] = coding.agents;
    const seat = seats.get(lead!.session_id)!;
    expect(targetTile(map, lead!, seats)).toEqual(seat);
    expect(targetTile(map, child!, seats)).toEqual({ x: seat.x + 1, y: seat.y });
    // 둘째·셋째 하위 에이전트는 다른 칸에 선다(#182).
    const spots = [0, 1, 2, 3].map((i) => targetTile(map, child!, seats, "desk", 0, i));
    expect(new Set(spots.map((p) => `${p.x},${p.y}`)).size).toBe(4);
    for (const spot of spots) {
      expect(isWalkable(map, spot)).toBe(true);
    }
  });

  it("uses the server desk slot and leaves returned desks empty", () => {
    const state = createSyntheticOffice({ sessions: 3, subagentsPerSession: 0 });
    state.sessions[0]!.desk_slot = 5;
    state.sessions[1]!.desk_slot = null;
    const seats = assignSeats(map, state);
    expect(seats.get(state.sessions[0]!.id)).toEqual(map.seats[5]);
    expect(seats.has(state.sessions[1]!.id)).toBe(false);
  });

  it("grows desk rows only when slots exceed capacity", () => {
    const state = createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 });
    expect(deskRowsFor(state)).toBe(2);
    state.sessions[0]!.desk_slot = 13;
    expect(deskRowsFor(state)).toBe(2);
    state.sessions[0]!.desk_slot = 29;
    expect(deskRowsFor(state)).toBe(5);
    expect(createDefaultOffice(5).seats).toHaveLength(35);
  });
});

describe("synthetic adapter", () => {
  it("is labeled synthetic and deterministic", () => {
    const first = createSyntheticOffice({ sessions: 3, subagentsPerSession: 2 });
    expect(first.source).toBe("synthetic");
    expect(first.agents).toHaveLength(9);
    expect(advanceSynthetic(first, 4)).toEqual(advanceSynthetic(first, 4));
    expect(advanceSynthetic(first, 1).agents).not.toEqual(first.agents);
  });

  it("creates unique, well-formed synthetic ids", () => {
    const state = createSyntheticOffice({ sessions: 20, subagentsPerSession: 2 });
    const ids = [...state.sessions, ...state.agents].map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) {
      expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$/);
    }
  });

  it("refuses to advance server data", () => {
    const server = { ...createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 }), source: "server" as const };
    expect(() => advanceSynthetic(server, 1)).toThrow();
  });
});

describe("zone guide (#160)", () => {
  it("explains every zone a character can be sent to, with the map color", () => {
    const activities: Activity[] = [
      "reading", "browsing", "coding", "testing", "planning", "delegating",
      "reporting", "thinking", "waiting_user", "idle", "unknown",
    ];
    const reachable = new Set(
      activities.flatMap((a) => [zoneForActivity(a, "WORKING"), zoneForActivity(a, "IDLE")]),
    );
    const explained = new Set(ZONE_GUIDE.map((g) => g.zone));
    for (const zone of reachable) {
      expect(explained.has(zone), zone).toBe(true);
      expect(ZONE_COLORS[zone]).toBeTypeOf("number");
    }
  });
});

describe("no overlapping characters (#194)", () => {
  const map = createDefaultOffice(2);

  it("gives every character its own tile, keeps fixed seats and stays in the wanted zone", () => {
    const lounge = walkableTiles(map, "lounge")[0]!;
    const seat = map.seats[0]!;
    const wants = [
      ...Array.from({ length: 6 }, (_, i) => ({ id: `idle-${i}`, point: lounge, fixed: false })),
      { id: "lead", point: seat, fixed: true },
      { id: "visitor", point: seat, fixed: false },
    ];
    const targets = spreadTargets(map, wants);
    const keys = [...targets.values()].map((p) => `${p.x},${p.y}`);
    expect(new Set(keys).size).toBe(wants.length);
    expect(targets.get("lead")).toEqual(seat);
    for (let i = 0; i < 6; i += 1) {
      const p = targets.get(`idle-${i}`)!;
      expect(tileAt(map, p)?.zone).toBe("lounge");
      expect(isWalkable(map, p)).toBe(true);
    }
    expect(targets.get("visitor")).not.toEqual(seat);
    // 같은 입력이면 같은 결과(매 프레임 흔들리지 않음).
    expect(spreadTargets(map, [...wants].reverse())).toEqual(targets);
  });
});

import { describe, expect, it } from "vitest";

import { assignSeats, targetTile, zoneForActivity } from "./behavior";
import { contains, createDefaultOffice, isWalkable, tileAt, walkableTiles } from "./grid";
import { depthOf, gridToScreen, screenToGrid, tileDiamond } from "./iso";
import { advanceSynthetic, createSyntheticOffice } from "./synthetic";
import type { ZoneKind } from "./grid";

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
  });

  it("does not seat ended sessions", () => {
    const state = createSyntheticOffice({ sessions: 2, subagentsPerSession: 0 });
    state.sessions[0]!.lifecycle = "ENDED";
    expect(assignSeats(map, state).size).toBe(1);
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

  it("refuses to advance server data", () => {
    const server = { ...createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 }), source: "server" as const };
    expect(() => advanceSynthetic(server, 1)).toThrow();
  });
});

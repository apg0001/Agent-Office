import { describe, expect, it } from "vitest";

import { activeSessions } from "./SessionList";
import { createSyntheticOffice } from "./synthetic";
import type { AgentState, OfficeState, SessionState } from "./types";
import { hiddenKey, hiddenSessionIds, visibleOffice, withoutHidden } from "./visibility";

const NOW = Date.parse("2026-10-28T12:00:00Z");

function minutesAgo(minutes: number): string {
  return new Date(NOW - minutes * 60_000).toISOString();
}

function session(id: string, lastMinutesAgo: number | null, overrides: Partial<SessionState> = {}): SessionState {
  return {
    id,
    device_id: "d1",
    provider: "claude",
    lifecycle: "OPEN",
    started_at: minutesAgo(600),
    ended_at: null,
    last_event_at: lastMinutesAgo === null ? null : minutesAgo(lastMinutesAgo),
    desk_slot: 0,
    change_seq: 1,
    ...overrides,
  };
}

function agent(id: string, sessionId: string, overrides: Partial<AgentState> = {}): AgentState {
  return {
    id,
    session_id: sessionId,
    parent_agent_id: null,
    role: "lead",
    turn_state: "IDLE",
    activity: "idle",
    confidence: "observed",
    state_version: 1,
    started_at: null,
    ended_at: null,
    change_seq: 1,
    ...overrides,
  };
}

function office(sessions: SessionState[], agents: AgentState[], minutes = 60): OfficeState {
  return {
    source: "server",
    settings: { hide_idle_after_minutes: minutes },
    devices: [{ id: "d1", name: "PC", connection: "ONLINE" }],
    sessions,
    agents,
  };
}

describe("hiddenSessionIds", () => {
  it("hides sessions idle longer than the admin setting", () => {
    const state = office(
      [session("fresh", 5), session("edge", 60), session("stale", 61)],
      [agent("a1", "fresh"), agent("a2", "edge"), agent("a3", "stale")],
    );
    expect([...hiddenSessionIds(state, 60, NOW)]).toEqual(["stale"]);
  });

  it("never hides sessions waiting for user approval", () => {
    const state = office(
      [session("waiting", 300), session("idle", 300)],
      [
        agent("lead1", "waiting"),
        agent("sub1", "waiting", { role: "subagent", parent_agent_id: "lead1", turn_state: "WAITING_USER" }),
        agent("lead2", "idle"),
      ],
    );
    expect([...hiddenSessionIds(state, 60, NOW)]).toEqual(["idle"]);
  });

  it("ignores ended agents that were waiting", () => {
    const state = office([session("s1", 300)], [agent("a1", "s1", { turn_state: "WAITING_USER", ended_at: minutesAgo(299) })]);
    expect(hiddenSessionIds(state, 60, NOW).has("s1")).toBe(true);
  });

  it("does nothing when off, unset or the time is unknown", () => {
    const state = office([session("s1", 300), session("unknown", null, { started_at: null })], []);
    expect(hiddenSessionIds(state, 0, NOW).size).toBe(0);
    expect(hiddenSessionIds(state, undefined, NOW).size).toBe(0);
    expect(hiddenSessionIds(state, Number.NaN, NOW).size).toBe(0);
    expect([...hiddenSessionIds(state, 60, NOW)]).toEqual(["s1"]);
  });

  it("falls back to started_at and leaves ended sessions to the normal exit", () => {
    const state = office(
      [
        session("never", null, { started_at: minutesAgo(120) }),
        session("ended", 300, { lifecycle: "ENDED", ended_at: minutesAgo(5) }),
      ],
      [],
    );
    expect([...hiddenSessionIds(state, 60, NOW)]).toEqual(["never"]);
  });
});

describe("visibleOffice", () => {
  it("removes hidden sessions and their agents from the list and the map", () => {
    const state = office(
      [session("busy", 1), session("stale", 120)],
      [agent("a1", "busy", { turn_state: "WORKING" }), agent("a2", "stale"), agent("a3", "stale", { role: "subagent" })],
    );
    const visible = visibleOffice(state, NOW);
    expect(visible.sessions.map((s) => s.id)).toEqual(["busy"]);
    expect(visible.agents.map((a) => a.id)).toEqual(["a1"]);
    expect(activeSessions(visible).map((row) => row.session.id)).toEqual(["busy"]);
    expect(visible.settings).toEqual(state.settings);
  });

  it("shows the session again when new activity arrives", () => {
    const stale = office([session("s1", 120)], [agent("a1", "s1")]);
    expect(visibleOffice(stale, NOW).sessions).toEqual([]);
    const active = office([session("s1", 0, { change_seq: 2 })], [agent("a1", "s1", { turn_state: "WORKING" })]);
    expect(visibleOffice(active, NOW).agents.map((a) => a.id)).toEqual(["a1"]);
  });

  it("returns the same state when nothing is hidden", () => {
    const state = office([session("s1", 1)], [agent("a1", "s1")]);
    expect(visibleOffice(state, NOW)).toBe(state);
    expect(withoutHidden(state, new Set())).toBe(state);
  });

  it("keeps synthetic demo data visible", () => {
    const demo = createSyntheticOffice({ sessions: 2, subagentsPerSession: 1 });
    expect(demo.settings).toEqual({ hide_idle_after_minutes: 0 });
    expect(visibleOffice(demo, Date.parse("2100-01-01T00:00:00Z"))).toBe(demo);
  });
});

describe("hiddenKey", () => {
  it("is order independent", () => {
    expect(hiddenKey(new Set(["b", "a"]))).toBe(hiddenKey(new Set(["a", "b"])));
    expect(hiddenKey(new Set())).toBe("");
  });
});

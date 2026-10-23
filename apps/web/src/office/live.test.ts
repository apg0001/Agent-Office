import { describe, expect, it } from "vitest";

import { applyDelta, applySnapshot, isOfficeMessage } from "./liveStore";
import { MIN_DWELL_MS, nextDisplayedZone } from "./smoothing";
import type { AgentState } from "./types";

function agent(overrides: Partial<AgentState> = {}): AgentState {
  return {
    id: "a1",
    session_id: "s1",
    parent_agent_id: null,
    role: "lead",
    turn_state: "WORKING",
    activity: "coding",
    confidence: "observed",
    state_version: 1,
    started_at: null,
    ended_at: null,
    change_seq: 10,
    ...overrides,
  };
}

const empty = { devices: [], sessions: [] };

describe("live store", () => {
  it("replaces state on snapshot and marks it as server data", () => {
    const state = applySnapshot({ type: "snapshot", ...empty, agents: [agent()] });
    expect(state.source).toBe("server");
    expect(state.agents).toHaveLength(1);
  });

  it("applies only newer change_seq per id", () => {
    const state = applySnapshot({ type: "snapshot", ...empty, agents: [agent()] });
    const older = applyDelta(state, {
      type: "delta",
      ...empty,
      agents: [agent({ activity: "reading", change_seq: 9 })],
    });
    expect(older.agents[0]?.activity).toBe("coding");

    const newer = applyDelta(older, {
      type: "delta",
      ...empty,
      agents: [agent({ activity: "reading", change_seq: 11 }), agent({ id: "a2", change_seq: 12 })],
    });
    expect(newer.agents.find((a) => a.id === "a1")?.activity).toBe("reading");
    expect(newer.agents).toHaveLength(2);
  });

  it("never mixes server deltas into synthetic state", () => {
    const synthetic = { source: "synthetic" as const, devices: [], sessions: [], agents: [] };
    expect(applyDelta(synthetic, { type: "delta", ...empty, agents: [agent()] })).toBe(synthetic);
  });

  it("validates message shape", () => {
    expect(isOfficeMessage({ type: "ping" })).toBe(false);
    expect(isOfficeMessage({ type: "delta", devices: [], sessions: [], agents: [] })).toBe(true);
    expect(isOfficeMessage(null)).toBe(false);
  });
});

describe("burst smoothing", () => {
  it("holds the displayed zone during a burst of activity changes", () => {
    const first = nextDisplayedZone(undefined, agent({ activity: "reading" }), 0);
    expect(first.zone).toBe("library");
    const burst = nextDisplayedZone(first, agent({ activity: "coding" }), 400);
    expect(burst).toBe(first);
    const settled = nextDisplayedZone(first, agent({ activity: "coding" }), MIN_DWELL_MS);
    expect(settled.zone).toBe("desk");
  });

  it("applies urgent transitions immediately", () => {
    const first = nextDisplayedZone(undefined, agent({ activity: "reading" }), 0);
    const idle = nextDisplayedZone(first, agent({ turn_state: "IDLE", activity: "idle" }), 100);
    expect(idle.zone).toBe("lounge");
    const waiting = nextDisplayedZone(
      first,
      agent({ turn_state: "WAITING_USER", activity: "waiting_user" }),
      100,
    );
    expect(waiting.zone).toBe("desk");
  });
});

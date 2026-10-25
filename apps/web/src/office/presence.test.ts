import { describe, expect, it } from "vitest";

import { IDLE_WANDER_MS, idleCycle, mixHash, presenceOf } from "./presence";
import { createSyntheticOffice } from "./synthetic";
import type { OfficeState } from "./types";

function office(): OfficeState {
  return createSyntheticOffice({ sessions: 1, subagentsPerSession: 1 });
}

describe("presence", () => {
  it("keeps disconnected collectors in the office, dimmed with a badge", () => {
    const state = office();
    state.devices[0]!.connection = "DISCONNECTED";
    const view = presenceOf(state.agents[0]!, state);
    expect(view).toEqual({ presence: "present", alpha: 0.4, badge: "disconnected" });
  });

  it("marks unconfirmed sessions without sending them home", () => {
    const state = office();
    state.sessions[0]!.lifecycle = "UNKNOWN";
    expect(presenceOf(state.agents[0]!, state)).toMatchObject({
      presence: "present",
      badge: "unconfirmed",
    });
  });

  it("sends agents home only when the session ended or the subagent completed", () => {
    const state = office();
    const [lead, child] = state.agents;
    child!.ended_at = "2026-10-23T00:00:00Z";
    expect(presenceOf(lead!, state).presence).toBe("present");
    expect(presenceOf(child!, state).presence).toBe("leaving");
    state.sessions[0]!.lifecycle = "ENDED";
    expect(presenceOf(lead!, state).presence).toBe("leaving");
  });

  it("draws inferred activity normally (the detail panel labels it)", () => {
    const state = office();
    state.agents[0]!.confidence = "inferred";
    expect(presenceOf(state.agents[0]!, state)).toEqual({ presence: "present", alpha: 1, badge: null });
  });

  it("changes idle spots periodically and staggers characters", () => {
    expect(idleCycle(0, "a")).not.toBe(idleCycle(IDLE_WANDER_MS * 2, "a"));
    // 끝 글자만 다른 연속 ID(합성 데이터)도 시작 시점이 엇갈린다.
    const ids = Array.from({ length: 8 }, (_, index) => `agent-${index}`);
    const phases = new Set(ids.map((id) => mixHash(id) % IDLE_WANDER_MS));
    expect(phases.size).toBe(ids.length);
    const spread = Math.max(...phases) - Math.min(...phases);
    expect(spread).toBeGreaterThan(IDLE_WANDER_MS / 2);
  });
});

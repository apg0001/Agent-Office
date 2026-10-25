import { describe, expect, it } from "vitest";

import { activeSessions } from "./SessionList";
import { createSyntheticOffice } from "./synthetic";

describe("active session list (#162)", () => {
  it("counts only sessions that have not ended and summarises the lead", () => {
    const state = createSyntheticOffice({ sessions: 3, subagentsPerSession: 1 });
    state.sessions[0] = { ...state.sessions[0]!, lifecycle: "ENDED", ended_at: "2026-10-26T00:00:00Z" };
    const list = activeSessions(state);
    expect(list).toHaveLength(2);
    for (const item of list) {
      expect(item.leadId).not.toBeNull();
      expect(item.headline.length).toBeGreaterThan(0);
      expect(item.subagents).toBe(1);
    }
  });
});

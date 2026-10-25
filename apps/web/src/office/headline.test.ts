import { describe, expect, it } from "vitest";

import { ACTIVITY_HEADLINES, headlineFor } from "./headline";

describe("headline above characters (#162)", () => {
  it("names the current activity briefly", () => {
    expect(headlineFor({ activity: "reading", turn_state: "WORKING" })).toBe("📖 읽는 중");
    expect(headlineFor({ activity: "reporting", turn_state: "WORKING" })).toBe("📋 보고 중");
    expect(headlineFor({ activity: "browsing", turn_state: "WORKING" })).toBe("🌐 검색 중");
  });

  it("lets turn state win for waiting and idle", () => {
    expect(headlineFor({ activity: "coding", turn_state: "WAITING_USER" })).toBe("✋ 승인 대기");
    expect(headlineFor({ activity: "coding", turn_state: "IDLE" })).toBe("☕ 쉬는 중");
    expect(headlineFor({ activity: "unknown", turn_state: "THINKING" })).toBe("💭 생각 중");
  });

  it("keeps every label short", () => {
    for (const label of Object.values(ACTIVITY_HEADLINES)) {
      expect([...label].length).toBeLessThanOrEqual(8);
    }
  });
});

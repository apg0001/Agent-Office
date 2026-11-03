import { describe, expect, it } from "vitest";

import {
  ACTIVITY_HEADLINES,
  CHATTING,
  DOZE_AFTER_MS,
  DOZING,
  IDLE_PASTIMES,
  PASTIME_MS,
  SLEEPING,
  SLEEP_AFTER_MS,
  WAIT_HEADLINES,
  headlineFor,
  idleHeadline,
  waitMark,
} from "./headline";

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

  it("tells what a waiting agent waits for (#262)", () => {
    const waiting = { activity: "waiting_user", turn_state: "WAITING_USER" } as const;
    expect(headlineFor({ ...waiting, wait_kind: "question" })).toBe("❓ 질문 대기");
    expect(headlineFor({ ...waiting, wait_kind: "plan_review" })).toBe("📋 계획 확인 대기");
    expect(headlineFor({ ...waiting, wait_kind: "permission" })).toBe("✋ 승인 대기");
    // 이전 서버(종류 없음)는 권한 승인.
    expect(headlineFor({ ...waiting, wait_kind: null })).toBe("✋ 승인 대기");
    expect(waitMark({ wait_kind: "question" })).toBe("?");
    expect(waitMark({ wait_kind: "plan_review" })).toBe("!");
    expect(waitMark({})).toBe("!");
    for (const label of Object.values(WAIT_HEADLINES)) {
      expect(Array.from(label).length).toBeLessThanOrEqual(12);
    }
  });

  it("keeps every label short", () => {
    for (const label of Object.values(ACTIVITY_HEADLINES)) {
      expect([...label].length).toBeLessThanOrEqual(8);
    }
  });
});

describe("idle pastimes (#233)", () => {
  const now = 1_800_000_000_000;

  it("dozes and then sleeps the longer the session has been quiet", () => {
    expect(idleHeadline("a", now, now - DOZE_AFTER_MS, false)).toBe(DOZING);
    expect(idleHeadline("a", now, now - SLEEP_AFTER_MS, true)).toBe(SLEEPING);
    expect(IDLE_PASTIMES).toContain(idleHeadline("a", now, now - 1000, false));
  });

  it("chats with a neighbour before dozing off", () => {
    expect(idleHeadline("a", now, now - 1000, true)).toBe(CHATTING);
    expect(idleHeadline("a", now, now - DOZE_AFTER_MS, true)).toBe(DOZING);
  });

  it("changes pastime over time and differs between characters", () => {
    const seen = new Set<string>();
    for (let step = 0; step < 12; step += 1) {
      seen.add(idleHeadline("agent-1", now + step * PASTIME_MS, null, false));
    }
    expect(seen.size).toBeGreaterThan(3);
    const atOnce = new Set(["a", "b", "c", "d", "e", "f"].map((id) => idleHeadline(id, now, null, false)));
    expect(atOnce.size).toBeGreaterThan(1);
  });

  it("keeps every idle label short", () => {
    for (const label of [...IDLE_PASTIMES, CHATTING, DOZING, SLEEPING]) {
      expect([...label].length).toBeLessThanOrEqual(10);
    }
  });
});

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { SessionList, activeSessions, sortSessions, whoFor } from "./SessionList";
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

describe("session list person names (#206)", () => {
  const device = { id: "d", name: "Work PC", connection: "ONLINE" as const };

  it("shows only the PC name in the personal office", () => {
    expect(whoFor(device)).toBe("Work PC");
    expect(whoFor(undefined)).toBe("PC");
  });

  it("shows the person in team and combined views", () => {
    expect(whoFor({ ...device, name: "Bob", owner_name: "Bob" })).toBe("Bob");
    expect(whoFor({ ...device, owner_name: "Alice" })).toBe("Alice · Work PC");
    expect(whoFor({ ...device, name: "", owner_name: "Alice" })).toBe("Alice");
  });

  it("uses the owner name for rows of the active list", () => {
    const state = createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 });
    state.devices = state.devices.map((d) => ({ ...d, name: "Bob", owner_name: "Bob" }));
    expect(activeSessions(state)[0]!.deviceName).toBe("Bob");
  });
});

describe("session list selection", () => {
  it("marks only the selected session's lead as pressed", () => {
    const state = createSyntheticOffice({ sessions: 2, subagentsPerSession: 0 });
    const lead = activeSessions(state)[1]!.leadId!;
    const html = renderToStaticMarkup(<SessionList state={state} onSelect={() => undefined} selected={lead} />);
    expect(html.match(/aria-pressed="true"/g)).toHaveLength(1);
    expect(html.match(/aria-pressed="false"/g)).toHaveLength(1);
  });
});

describe("session list sort (#244)", () => {
  const state = createSyntheticOffice({ sessions: 3, subagentsPerSession: 0 });
  state.devices = ["다", "가", "나"].map((name, i) => ({ id: `d${i}`, name, connection: "ONLINE" as const }));
  state.sessions = state.sessions.map((s, i) => ({
    ...s,
    device_id: `d${i}`,
    started_at: `2026-10-28T0${i}:00:00Z`,
    // 가장 늦게 시작한 세션이 가장 최근 활동.
    last_event_at: `2026-10-28T1${i}:00:00Z`,
  }));
  const list = activeSessions(state);
  const ids = (sort: "started" | "recent" | "name") => sortSessions(list, sort).map((item) => item.session.id);
  const [first, second, third] = state.sessions.map((s) => s.id);

  it("keeps rows in start order so they do not jump around", () => {
    expect(ids("started")).toEqual([first, second, third]);
  });

  it("can show the most recent activity first", () => {
    expect(ids("recent")).toEqual([third, second, first]);
  });

  it("can sort by person or PC name", () => {
    expect(sortSessions(list, "name").map((item) => item.deviceName)).toEqual(["가", "나", "다"]);
  });
});

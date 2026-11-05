import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { SessionList, activeSessions, groupSessions, sortSessions, whoFor } from "./SessionList";
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
  const ids = (sort: "status" | "started" | "recent" | "name") => sortSessions(list, sort).map((item) => item.session.id);
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

  it("puts waiting and working sessions above idle ones by default", () => {
    const leads = state.agents.filter((a) => a.role === "lead");
    const turn = ["IDLE", "WORKING", "WAITING_USER"] as const;
    const mixed = {
      ...state,
      agents: state.agents.map((a) => (a.role === "lead" ? { ...a, turn_state: turn[leads.indexOf(a)]! } : a)),
    };
    const order = sortSessions(activeSessions(mixed), "status").map((item) => item.turnState);
    expect(order).toEqual(["WAITING_USER", "WORKING", "IDLE"]);
  });
});

describe("session list PC groups (#271)", () => {
  const state = createSyntheticOffice({ sessions: 4, subagentsPerSession: 0 });
  state.devices = [
    { id: "pi", name: "Pi", connection: "ONLINE" as const },
    { id: "win", name: "Win", connection: "ONLINE" as const },
  ];
  const place = [
    ["win", "blog"],
    ["pi", "Agent-Office"],
    ["pi", "infra"],
    ["pi", "Agent-Office"],
  ] as const;
  state.sessions = state.sessions.map((s, i) => ({
    ...s,
    device_id: place[i]![0],
    workspace_name: place[i]![1],
    started_at: `2026-11-06T0${i}:00:00Z`,
  }));
  const leads = state.agents.filter((a) => a.role === "lead");
  const turn = ["IDLE", "WORKING", "IDLE", "WAITING_USER"] as const;
  const mixed = {
    ...state,
    agents: state.agents.map((a) => (a.role === "lead" ? { ...a, turn_state: turn[leads.indexOf(a)]! } : a)),
  };

  it("puts the PC with a waiting session first and summarises it", () => {
    const groups = groupSessions(sortSessions(activeSessions(mixed), "status"));
    expect(groups.map((g) => [g.deviceId, g.status, g.count, g.waiting])).toEqual([
      ["pi", "waiting", 3, 1],
      ["win", "idle", 1, 0],
    ]);
  });

  it("adds a project heading only when a PC has two sessions of it", () => {
    const pi = groupSessions(sortSessions(activeSessions(mixed), "started"))[1]!;
    expect(pi.projects.map((p) => [p.workspace, p.rows.length])).toEqual([
      ["Agent-Office", 2],
      [null, 1],
    ]);
  });

  it("does not group sessions without a name", () => {
    const blank = { ...mixed, sessions: mixed.sessions.map((s) => ({ ...s, workspace_name: null, project_name: null })) };
    const pi = groupSessions(activeSessions(blank)).find((g) => g.deviceId === "pi")!;
    expect(pi.projects.every((p) => p.workspace === null)).toBe(true);
  });

  it("renders PC headers and drops the PC name from rows", () => {
    const html = renderToStaticMarkup(<SessionList state={mixed} onSelect={() => undefined} />);
    expect(html.match(/class="device-head"/g)).toHaveLength(2);
    expect(html).toContain("✋1");
    expect(html).toContain('class="project-name"');
    expect(html).not.toContain("Pi · ");
  });

  it("keeps the flat list when there is only one PC", () => {
    const one = createSyntheticOffice({ sessions: 2, subagentsPerSession: 0 });
    const html = renderToStaticMarkup(<SessionList state={one} onSelect={() => undefined} />);
    expect(html).not.toContain("device-head");
  });
});

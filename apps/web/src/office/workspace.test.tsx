import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { AgentPanel } from "./AgentPanel";
import { detailFromState, fetchAgentDetail } from "./detail";
import { applyDelta, applySnapshot } from "./liveStore";
import { SessionList } from "./SessionList";
import { createSyntheticOffice } from "./synthetic";
import type { SessionState } from "./types";
import { shortWorkspace, workspaceName } from "./workspace";

function session(overrides: Partial<SessionState> & Record<string, unknown> = {}): SessionState {
  return {
    id: "s1",
    device_id: "d1",
    provider: "claude",
    lifecycle: "OPEN",
    started_at: null,
    ended_at: null,
    last_event_at: null,
    desk_slot: 0,
    change_seq: 1,
    ...overrides,
  } as SessionState;
}

describe("workspace name (#181)", () => {
  it("accepts only short names without separators or control characters", () => {
    expect(workspaceName("Agent-Office")).toBe("Agent-Office");
    expect(workspaceName("한글 폴더")).toBe("한글 폴더");
    expect(workspaceName("가".repeat(64))).toBe("가".repeat(64));
    for (const bad of [
      undefined,
      null,
      42,
      "",
      " ",
      ".",
      "..",
      "a/b",
      "C:\\Users",
      "line\nbreak",
      "nul\u0000",
      "bidi\u202e",
      "가".repeat(65),
    ]) {
      expect(workspaceName(bad)).toBeNull();
    }
  });

  it("shortens long names with an ellipsis", () => {
    expect(shortWorkspace("short")).toBe("short");
    const long = shortWorkspace("a-very-long-workspace-folder-name");
    expect(Array.from(long)).toHaveLength(20);
    expect(long.endsWith("…")).toBe(true);
  });

  it("normalizes snapshot and delta sessions; missing field from old servers is null", () => {
    const state = applySnapshot({
      type: "snapshot",
      devices: [],
      agents: [],
      sessions: [session(), session({ id: "s2", workspace_name: "../etc" }), session({ id: "s3", workspace_name: "blog" })],
    });
    expect(state.sessions.map((s) => s.workspace_name)).toEqual([null, null, "blog"]);
    const next = applyDelta(state, {
      type: "delta",
      devices: [],
      agents: [],
      sessions: [session({ workspace_name: "Agent-Office", change_seq: 2 })],
    });
    expect(next.sessions.find((s) => s.id === "s1")?.workspace_name).toBe("Agent-Office");
  });

  it("shows the name in the session list and the detail panel, escaped", () => {
    const office = createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 });
    office.sessions[0] = { ...office.sessions[0]!, workspace_name: "<i>x" };
    const list = renderToString(<SessionList state={office} onSelect={() => undefined} />);
    expect(list).toContain("&lt;i&gt;x");
    expect(list).not.toContain("<i>x");

    const detail = detailFromState(office, office.agents[0]!.id)!;
    const panel = renderToString(
      <AgentPanel
        detail={detail}
        source="server"
        now={Date.parse("2026-10-23T12:00:00Z")}
        onClose={() => undefined}
        onSelect={() => undefined}
      />,
    );
    expect(panel).toContain("세션 이름");
    expect(panel).toContain("&lt;i&gt;x");

    const none = { ...detail, session: { ...detail.session, workspace_name: null } };
    const hidden = renderToString(
      <AgentPanel detail={none} source="server" now={0} onClose={() => undefined} onSelect={() => undefined} />,
    );
    expect(hidden).not.toContain("세션 이름");
  });

  it("synthetic demo data includes a couple of names", () => {
    const office = createSyntheticOffice({ sessions: 3, subagentsPerSession: 0 });
    expect(office.sessions.map((s) => s.workspace_name)).toEqual(["Agent-Office", "blog", null]);
  });

  it("drops invalid names from the server detail response", async () => {
    const office = createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 });
    const detail = detailFromState(office, office.agents[0]!.id)!;
    const body = { ...detail, session: { ...detail.session, workspace_name: "/home/user/secret" } };
    const fetcher = (async () => new Response(JSON.stringify(body), { status: 200 })) as unknown as typeof fetch;
    const fetched = await fetchAgentDetail("x", fetcher);
    expect(fetched?.session.workspace_name).toBeNull();
  });
});

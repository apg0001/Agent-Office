import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { AgentPanel } from "./AgentPanel";
import { detailFromState, fetchAgentDetail, relativeTime, type AgentDetail } from "./detail";
import { createSyntheticOffice } from "./synthetic";

const NOW = Date.parse("2026-10-23T12:00:00Z");

describe("detailFromState", () => {
  it("links a lead with its subagents and its device", () => {
    const office = createSyntheticOffice({ sessions: 2, subagentsPerSession: 2 });
    const lead = office.agents.find((a) => a.role === "lead")!;

    const detail = detailFromState(office, lead.id)!;

    expect(detail.parent).toBeNull();
    expect(detail.children.map((c) => c.parent_agent_id)).toEqual([lead.id, lead.id]);
    expect(detail.device.id).toBe(detail.session.device_id);
    expect(detailFromState(office, detail.children[0].id)!.parent?.id).toBe(lead.id);
    expect(detailFromState(office, "missing")).toBeNull();
  });
});

describe("relativeTime", () => {
  it("formats short relative times and tolerates clock skew", () => {
    expect(relativeTime(null, NOW)).toBe("—");
    expect(relativeTime("2026-10-23T12:00:05Z", NOW)).toBe("방금");
    expect(relativeTime("2026-10-23T11:59:30Z", NOW)).toBe("30초 전");
    expect(relativeTime("2026-10-23T11:57:00Z", NOW)).toBe("3분 전");
    expect(relativeTime("2026-10-23T09:00:00Z", NOW)).toBe("3시간 전");
    expect(relativeTime("2026-10-21T12:00:00Z", NOW)).toBe("2일 전");
  });
});

describe("AgentPanel", () => {
  const office = createSyntheticOffice({ sessions: 1, subagentsPerSession: 1 });
  const base = detailFromState(office, office.agents[0].id)!;
  const render = (detail: AgentDetail, source: "server" | "synthetic") =>
    renderToString(
      <AgentPanel detail={detail} source={source} now={NOW} onClose={() => undefined} onSelect={() => undefined} />,
    );

  it("labels synthetic data and has no timeline for it", () => {
    const html = render(base, "synthetic");
    expect(html).toContain("실제 관측이 아닙니다");
    expect(html).toContain("합성 데이터에는 타임라인이 없습니다");
    expect(html).toContain(base.device.name);
  });

  it("shows server timeline and plan steps, marking inferred entries", () => {
    const detail: AgentDetail = {
      ...base,
      agent: { ...base.agent, turn_state: "WORKING", activity: "reading", confidence: "inferred" },
      timeline: [
        {
          event_type: "tool.started",
          activity: "reading",
          confidence: "inferred",
          source: "inferred",
          occurred_at: "2026-10-23T11:58:00Z",
        },
      ],
      plan_steps: [
        { step_index: 0, status: "completed" },
        { step_index: 1, status: "in_progress" },
      ],
    };
    const html = render(detail, "server");
    expect(html).not.toContain("실제 관측이 아닙니다");
    expect(html).toContain("작업 중");
    expect(html).toContain("도구 시작");
    expect(html).toContain("2분 전");
    expect(html).toContain("(추정)");
    expect(html).toContain("2단계 · 진행 중");
  });
});

describe("fetchAgentDetail", () => {
  it("returns null for missing or foreign agents and network errors", async () => {
    const notFound = (async () => new Response("{}", { status: 404 })) as unknown as typeof fetch;
    const broken = (async () => {
      throw new Error("offline");
    }) as unknown as typeof fetch;

    expect(await fetchAgentDetail("x", notFound)).toBeNull();
    expect(await fetchAgentDetail("x", broken)).toBeNull();
  });
});

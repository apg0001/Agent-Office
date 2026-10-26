/**
 * 서버 없이 화면을 개발하기 위한 합성 사무실 상태. 실제 관측이 아니며 `source: "synthetic"`이다.
 * 화면은 이 출처일 때 항상 "합성 데이터" 배지를 보여야 한다.
 */

import type { Activity, AgentState, OfficeState, TurnState } from "./types";

const ACTIVITY_CYCLE: ReadonlyArray<[TurnState, Activity]> = [
  ["THINKING", "thinking"],
  ["WORKING", "reading"],
  ["WORKING", "coding"],
  ["WORKING", "testing"],
  ["WORKING", "browsing"],
  ["WORKING", "planning"],
  ["WAITING_USER", "waiting_user"],
  ["IDLE", "idle"],
];

const ID_PREFIX = { device: "d", session: "5", agent: "a" } as const;

function syntheticId(kind: keyof typeof ID_PREFIX, index: number): string {
  // 예약 형식의 합성 UUID(모두 0으로 시작). 종류 접두사 1자 + 11자리로 정확히 36자다.
  const tail = index.toString(16).padStart(11, "0");
  return `00000000-0000-4000-8000-${ID_PREFIX[kind]}${tail}`;
}

export interface SyntheticOptions {
  sessions: number;
  subagentsPerSession: number;
}

/** 합성 세션 이름. 몇 개는 비워 두어 이름이 없는 세션도 보인다. */
const SYNTHETIC_WORKSPACES: (string | null)[] = ["Agent-Office", "blog", null];

export function createSyntheticOffice(options: SyntheticOptions): OfficeState {
  const device = { id: syntheticId("device", 1), name: "합성 PC", connection: "ONLINE" as const };
  const sessions = [];
  const agents: AgentState[] = [];
  for (let s = 0; s < options.sessions; s += 1) {
    const sessionId = syntheticId("session", s);
    sessions.push({
      id: sessionId,
      device_id: device.id,
      provider: s % 2 === 0 ? ("claude" as const) : ("codex" as const),
      lifecycle: "OPEN" as const,
      started_at: null,
      ended_at: null,
      last_event_at: null,
      desk_slot: s,
      // 합성 세션 이름(#181). 실제 PC 값이 아니다.
      workspace_name: SYNTHETIC_WORKSPACES[s % SYNTHETIC_WORKSPACES.length],
      change_seq: 1,
    });
    const leadId = syntheticId("agent", s * 100);
    agents.push(agentAt(leadId, sessionId, null, s, 0));
    for (let c = 1; c <= options.subagentsPerSession; c += 1) {
      agents.push(agentAt(syntheticId("agent", s * 100 + c), sessionId, leadId, s + c, 0));
    }
  }
  return { source: "synthetic", devices: [device], sessions, agents };
}

function agentAt(
  id: string,
  sessionId: string,
  parent: string | null,
  offset: number,
  tick: number,
): AgentState {
  const [turnState, activity] = ACTIVITY_CYCLE[(offset + tick) % ACTIVITY_CYCLE.length] ?? [
    "UNKNOWN",
    "unknown",
  ];
  return {
    id,
    session_id: sessionId,
    parent_agent_id: parent,
    role: parent ? "subagent" : "lead",
    turn_state: turnState,
    activity,
    // 합성 데이터에서도 추정 표시를 확인할 수 있게 일부를 추정으로 둔다.
    confidence: offset % 4 === 3 ? "inferred" : "observed",
    state_version: tick,
    started_at: null,
    ended_at: null,
    change_seq: tick + 1,
  };
}

/** 한 단계 진행한 합성 상태. 결정적이라 테스트와 재현이 가능하다. */
export function advanceSynthetic(state: OfficeState, tick: number): OfficeState {
  if (state.source !== "synthetic") {
    throw new Error("합성 상태만 진행할 수 있습니다.");
  }
  const agents = state.agents.map((agent, index) => {
    const next = agentAt(agent.id, agent.session_id, agent.parent_agent_id, index, tick);
    return { ...next, role: agent.role };
  });
  return { ...state, agents };
}

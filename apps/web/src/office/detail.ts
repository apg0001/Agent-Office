/**
 * 캐릭터 상세 패널 데이터(#51). 서버 `GET /api/v1/office/agents/{id}`와 같은 모양이다.
 * 원문(프롬프트·명령·경로·계획 본문)은 서버에도 없으므로 여기에도 없다.
 */

import { type OfficeScope, PERSONAL } from "./liveSource";
import type { Activity, AgentState, DeviceState, OfficeState, SessionState } from "./types";

export interface TimelineEntry {
  event_type: string;
  activity: Activity;
  confidence: "observed" | "inferred";
  source: string;
  occurred_at: string;
}

export interface PlanStep {
  step_index: number;
  status: "pending" | "in_progress" | "completed" | "unknown";
}

export interface AgentDetail {
  agent: AgentState;
  session: SessionState;
  device: DeviceState;
  parent: AgentState | null;
  children: AgentState[];
  timeline: TimelineEntry[];
  plan_steps: PlanStep[];
}

/** 화면이 이미 가진 상태로 만든 상세. 타임라인·계획은 서버에서만 온다. */
export function detailFromState(state: OfficeState, agentId: string): AgentDetail | null {
  const agent = state.agents.find((a) => a.id === agentId);
  const session = agent && state.sessions.find((s) => s.id === agent.session_id);
  const device = session && state.devices.find((d) => d.id === session.device_id);
  if (!agent || !session || !device) {
    return null;
  }
  return {
    agent,
    session,
    device,
    parent: state.agents.find((a) => a.id === agent.parent_agent_id) ?? null,
    children: state.agents.filter((a) => a.parent_agent_id === agent.id),
    timeline: [],
    plan_steps: [],
  };
}

export function detailPath(agentId: string, scope: OfficeScope = PERSONAL): string {
  const agent = encodeURIComponent(agentId);
  if (scope.kind === "team") {
    return `/api/v1/teams/${encodeURIComponent(scope.teamId)}/office/agents/${agent}`;
  }
  return scope.kind === "combined" ? `/api/v1/office/combined/agents/${agent}` : `/api/v1/office/agents/${agent}`;
}

export async function fetchAgentDetail(
  agentId: string,
  fetcher: typeof fetch = fetch,
  scope: OfficeScope = PERSONAL,
): Promise<AgentDetail | null> {
  try {
    const response = await fetcher(detailPath(agentId, scope), {
      credentials: "same-origin",
    });
    return response.ok ? ((await response.json()) as AgentDetail) : null;
  } catch {
    return null;
  }
}

export const TURN_LABELS: Record<string, string> = {
  WORKING: "작업 중",
  THINKING: "생각 중",
  WAITING_USER: "사용자 승인 대기",
  IDLE: "대기",
  UNKNOWN: "알 수 없음",
};

export const ACTIVITY_LABELS: Record<string, string> = {
  reading: "읽기",
  browsing: "탐색",
  coding: "코드 작성",
  testing: "테스트",
  planning: "계획",
  delegating: "위임",
  reporting: "보고",
  thinking: "생각",
  waiting_user: "승인 대기",
  idle: "유휴",
  unknown: "알 수 없음",
};

export const LIFECYCLE_LABELS: Record<string, string> = {
  DISCOVERED: "발견됨",
  OPEN: "열림",
  ENDED: "종료",
  UNKNOWN: "열림 미확인",
};

export const EVENT_LABELS: Record<string, string> = {
  "session.discovered": "세션 발견",
  "session.started": "세션 시작",
  "session.ended": "세션 종료",
  "turn.started": "턴 시작",
  "turn.completed": "턴 완료",
  "turn.interrupted": "턴 중단",
  "tool.started": "도구 시작",
  "tool.completed": "도구 완료",
  "tool.failed": "도구 실패",
  "approval.requested": "승인 요청",
  "approval.resolved": "승인 처리",
  "agent.started": "하위 에이전트 시작",
  "agent.completed": "하위 에이전트 완료",
  "plan.updated": "계획 갱신",
  unknown: "알 수 없는 이벤트",
};

export const PLAN_LABELS: Record<PlanStep["status"], string> = {
  pending: "대기",
  in_progress: "진행 중",
  completed: "완료",
  unknown: "알 수 없음",
};

export function label(map: Record<string, string>, key: string): string {
  return map[key] ?? key;
}

/** "3분 전"처럼 짧게. 미래 시각(시계 차이)은 "방금"으로 본다. */
export function relativeTime(iso: string | null, now: number): string {
  if (!iso) {
    return "—";
  }
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (seconds < 10) {
    return "방금";
  }
  if (seconds < 60) {
    return `${seconds}초 전`;
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}분 전`;
  }
  if (seconds < 86_400) {
    return `${Math.floor(seconds / 3600)}시간 전`;
  }
  return `${Math.floor(seconds / 86_400)}일 전`;
}

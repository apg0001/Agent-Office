export const EVENT_SCHEMA_VERSION = "1.0" as const;

export const EVENT_PROVIDERS = ["codex", "claude", "unknown"] as const;
export type EventProvider = (typeof EVENT_PROVIDERS)[number];

export const EVENT_SOURCES = ["hook", "jsonl", "app_server", "inferred"] as const;
export type EventSource = (typeof EVENT_SOURCES)[number];

export const EVENT_TYPES = [
  "session.discovered",
  "session.started",
  "session.ended",
  "turn.started",
  "turn.completed",
  "turn.interrupted",
  "tool.started",
  "tool.completed",
  "tool.failed",
  "approval.requested",
  "approval.resolved",
  "agent.started",
  "agent.completed",
  "plan.updated",
  "unknown",
] as const;
export type EventType = (typeof EVENT_TYPES)[number];

export const EVENT_ACTIVITIES = [
  "reading",
  "browsing",
  "coding",
  "testing",
  "planning",
  "delegating",
  "reporting",
  "thinking",
  "waiting_user",
  "idle",
  "unknown",
] as const;
export type EventActivity = (typeof EVENT_ACTIVITIES)[number];

export const EVENT_CONFIDENCE_LEVELS = ["observed", "inferred"] as const;
export type EventConfidence = (typeof EVENT_CONFIDENCE_LEVELS)[number];

export const PLAN_STEP_STATUSES = ["pending", "in_progress", "completed", "unknown"] as const;
export type PlanStepStatus = (typeof PLAN_STEP_STATUSES)[number];

export interface AgentOfficeEventV1 {
  schema_version: typeof EVENT_SCHEMA_VERSION;
  event_id: string;
  device_id: string;
  session_id: string;
  agent_id: string;
  parent_agent_id: string | null;
  provider: EventProvider;
  source: EventSource;
  event_type: EventType;
  activity: EventActivity;
  confidence: EventConfidence;
  occurred_at: string;
  sequence: number;
  /** plan.updated에서만: 계획 단계 상태만 순서대로(#38). 단계 문장은 담지 않는다. */
  plan_steps?: PlanStepStatus[];
  /**
   * 작업 폴더의 마지막 이름만(#181, 1..64자). 사용자가 PC별로 켰을 때만 온다.
   * 경로 구분자(`/`, `\\`)와 제어 문자는 담지 않는다. 이전 Collector는 보내지 않는다.
   */
  workspace_name?: string;
}

/** 이벤트 v1 `workspace_name` 최대 길이(코드 포인트). */
export const WORKSPACE_NAME_MAX_LENGTH = 64;

export interface CollectorEventBatchRequestV1 {
  schema_version: typeof EVENT_SCHEMA_VERSION;
  events: AgentOfficeEventV1[];
}

export const EVENT_REJECTION_CODES = [
  "unauthorized_device",
  "unauthorized_scope",
  "unsupported_schema_version",
  "retention_window_exceeded",
  "event_id_conflict",
  "parent_agent_unknown",
  "parent_agent_conflict",
  "occurred_at_out_of_range",
  "internal_error",
] as const;
export type EventRejectionCode = (typeof EVENT_REJECTION_CODES)[number];

export interface RejectedEventV1 {
  event_id: string;
  code: EventRejectionCode;
  retryable: boolean;
}

export interface CollectorEventBatchResponseV1 {
  schema_version: typeof EVENT_SCHEMA_VERSION;
  accepted_event_ids: string[];
  duplicate_event_ids: string[];
  rejected_events: RejectedEventV1[];
}

/**
 * 서버 사무실 스냅샷·증분(`docs/api/office-live-v1.md`)과 같은 모양의 타입.
 * 렌더러는 이 사실 상태만 입력으로 받고, 연출 상태는 따로 둔다.
 */

export type Connection = "ONLINE" | "DISCONNECTED";
export type Lifecycle = "DISCOVERED" | "OPEN" | "ENDED" | "UNKNOWN";
export type TurnState = "WORKING" | "THINKING" | "WAITING_USER" | "IDLE" | "UNKNOWN";
export type Activity =
  | "reading"
  | "browsing"
  | "coding"
  | "testing"
  | "planning"
  | "delegating"
  | "reporting"
  | "thinking"
  | "waiting_user"
  | "idle"
  | "unknown";

export interface DeviceState {
  id: string;
  name: string;
  connection: Connection;
}

export interface SessionState {
  id: string;
  device_id: string;
  provider: "codex" | "claude" | "unknown";
  lifecycle: Lifecycle;
  started_at: string | null;
  ended_at: string | null;
  last_event_at: string | null;
  /** 개인 사무실 책상 번호(서버가 배정·재사용, #48). 퇴근 후 반납되면 null. */
  desk_slot: number | null;
  /** 사용자가 지정한 프로젝트. 팀에 공유된 프로젝트의 세션만 팀 사무실에 나온다(#54). */
  project_id?: string | null;
  /**
   * 세션 이름: 작업 폴더의 마지막 이름(#181). 사용자가 그 PC에서 켰을 때만 오고, 본인 장치 세션에만 있다.
   * 이전 서버는 보내지 않는다(없음 = null).
   */
  workspace_name?: string | null;
  change_seq: number;
}

export interface AgentState {
  id: string;
  session_id: string;
  parent_agent_id: string | null;
  role: "lead" | "subagent";
  turn_state: TurnState;
  activity: Activity;
  /** 현재 활동을 만든 이벤트가 관측인지 추정인지. 추정은 화면에서 구분해 그린다. */
  confidence: "observed" | "inferred";
  state_version: number;
  started_at: string | null;
  ended_at: string | null;
  change_seq: number;
}

/** 데이터 출처. 합성 데이터는 화면에 반드시 합성이라고 표시한다. */
export type DataSource = "server" | "synthetic";

export interface OfficeState {
  source: DataSource;
  devices: DeviceState[];
  sessions: SessionState[];
  agents: AgentState[];
}

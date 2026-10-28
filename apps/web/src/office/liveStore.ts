/**
 * 서버 스냅샷·증분을 화면 상태로 합친다(`docs/api/office-live-v1.md`).
 * 같은 ID는 `change_seq`가 더 큰 갱신만 적용해, 순서가 뒤바뀐 메시지가 과거로 되돌리지 않는다.
 */

import type { AgentState, DeviceState, OfficeSettings, OfficeState, SessionState } from "./types";
import { withWorkspaceName } from "./workspace";

export interface SnapshotMessage {
  type: "snapshot";
  /** 관리자 화면 설정(#245). 이전 서버는 보내지 않는다. 형식은 `parseOfficeSettings`로 검사한다. */
  settings?: unknown;
  devices: DeviceState[];
  sessions: SessionState[];
  agents: AgentState[];
}

export interface DeltaMessage {
  type: "delta";
  devices: DeviceState[];
  sessions: SessionState[];
  agents: AgentState[];
}

/** 숨기기 기준 최대값(분, 7일). 서버 검증과 같다. */
export const MAX_HIDE_IDLE_MINUTES = 10080;

/** 스냅샷의 `settings`. 없거나 형식이 틀리면 숨기지 않는다(0). */
export function parseOfficeSettings(value: unknown): OfficeSettings {
  const minutes =
    typeof value === "object" && value !== null
      ? (value as Record<string, unknown>).hide_idle_after_minutes
      : undefined;
  const valid =
    typeof minutes === "number" && Number.isInteger(minutes) && minutes >= 0 && minutes <= MAX_HIDE_IDLE_MINUTES;
  return { hide_idle_after_minutes: valid ? minutes : 0 };
}

export function applySnapshot(message: SnapshotMessage): OfficeState {
  return {
    source: "server",
    settings: parseOfficeSettings(message.settings),
    devices: message.devices,
    sessions: message.sessions.map(withWorkspaceName),
    agents: message.agents,
  };
}

function mergeBySeq<T extends { id: string; change_seq: number }>(current: T[], updates: T[]): T[] {
  const byId = new Map(current.map((item) => [item.id, item]));
  for (const update of updates) {
    const existing = byId.get(update.id);
    if (!existing || update.change_seq > existing.change_seq) {
      byId.set(update.id, update);
    }
  }
  return [...byId.values()];
}

export function applyDelta(state: OfficeState, message: DeltaMessage): OfficeState {
  if (state.source !== "server") {
    // 합성 상태에 서버 증분을 섞지 않는다.
    return state;
  }
  const devices = new Map(state.devices.map((device) => [device.id, device]));
  for (const device of message.devices) {
    devices.set(device.id, device);
  }
  return {
    source: "server",
    // 증분에는 설정이 없다. 설정은 다음 스냅샷까지 유지한다.
    settings: state.settings,
    devices: [...devices.values()],
    sessions: mergeBySeq(state.sessions, message.sessions.map(withWorkspaceName)),
    agents: mergeBySeq(state.agents, message.agents),
  };
}

export function isOfficeMessage(value: unknown): value is SnapshotMessage | DeltaMessage {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return (
    (record.type === "snapshot" || record.type === "delta") &&
    Array.isArray(record.devices) &&
    Array.isArray(record.sessions) &&
    Array.isArray(record.agents)
  );
}

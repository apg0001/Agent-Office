/**
 * 서버 스냅샷·증분을 화면 상태로 합친다(`docs/api/office-live-v1.md`).
 * 같은 ID는 `change_seq`가 더 큰 갱신만 적용해, 순서가 뒤바뀐 메시지가 과거로 되돌리지 않는다.
 */

import type { AgentState, DeviceState, OfficeState, SessionState } from "./types";
import { withWorkspaceName } from "./workspace";

export interface SnapshotMessage {
  type: "snapshot";
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

export function applySnapshot(message: SnapshotMessage): OfficeState {
  return {
    source: "server",
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

import { useState } from "react";

import { headlineFor } from "./headline";
import { shortLabel } from "./sprites";
import type { AgentState, DeviceState, OfficeState, SessionState } from "./types";
import { sessionName, shortWorkspace } from "./workspace";

const PROVIDER_LABELS: Record<SessionState["provider"], string> = {
  claude: "Claude",
  codex: "Codex",
  unknown: "도구",
};

export interface ActiveSession {
  session: SessionState;
  deviceId: string;
  /** 행 맨 앞의 "누구" 이름. 팀·통합은 사람 이름(통합의 내 PC는 "내 이름 · PC 이름", #206), 개인은 PC 이름. */
  deviceName: string;
  leadId: string | null;
  /** 대장의 턴 상태(상태순 정렬용). 대장이 없으면 null. */
  turnState: AgentState["turn_state"] | null;
  headline: string;
  subagents: number;
}

/** 세션 목록 행의 "누구". 사람 이름이 있으면 앞에 두고, PC 이름이 따로 있으면(통합의 내 장치) 뒤에 붙인다. */
export function whoFor(device: DeviceState | undefined): string {
  const pc = shortLabel(device?.name, 24);
  const owner = shortLabel(device?.owner_name, 16);
  if (!owner) {
    return pc || "PC";
  }
  return pc && pc !== owner ? `${owner} · ${pc}` : owner;
}

/**
 * 목록 정렬(#244). 기본 상태순: 승인 대기 → 일하는 중 → 쉬는 중, 같은 무리 안은 시작 순이라
 * 상태가 바뀔 때만 움직인다. 시작 순은 자리가 완전히 고정된다.
 */
export type SessionSort = "status" | "started" | "recent" | "name";
export const SESSION_SORT_LABELS: Record<SessionSort, string> = {
  status: "상태순",
  started: "시작 순",
  recent: "최근 활동",
  name: "이름순",
};
const SORT_STORAGE_KEY = "agentOffice.sessionSort";
const COLLAPSED_STORAGE_KEY = "agentOffice.sessionCollapsed";

function byStart(a: ActiveSession, b: ActiveSession): number {
  return (a.session.started_at ?? "").localeCompare(b.session.started_at ?? "") || a.session.id.localeCompare(b.session.id);
}

function statusRank(turnState: AgentState["turn_state"] | null): number {
  switch (turnState) {
    case "WAITING_USER":
      return 0;
    case "IDLE":
      return 2;
    case null:
      return 3;
    default:
      return 1;
  }
}

export function sortSessions(list: ActiveSession[], sort: SessionSort): ActiveSession[] {
  const sorted = [...list];
  switch (sort) {
    case "status":
      return sorted.sort((a, b) => statusRank(a.turnState) - statusRank(b.turnState) || byStart(a, b));
    case "started":
      return sorted.sort(byStart);
    case "recent":
      return sorted.sort((a, b) => (b.session.last_event_at ?? "").localeCompare(a.session.last_event_at ?? "") || byStart(a, b));
    case "name":
      return sorted.sort((a, b) => a.deviceName.localeCompare(b.deviceName, "ko") || byStart(a, b));
    default:
      return sorted;
  }
}

/** PC 하나의 세션 묶음(#271). 같은 프로젝트 세션이 2개 이상이면 `workspace`로 소제목을 붙인다. */
export interface SessionGroup {
  deviceId: string;
  name: string;
  /** 가장 급한 상태(대기 → 작업 → 쉼). 머리줄 상태 점 색. */
  status: "waiting" | "working" | "idle";
  waiting: number;
  count: number;
  projects: { workspace: string | null; rows: ActiveSession[] }[];
}

const GROUP_STATUS = ["waiting", "working", "idle", "idle"] as const;

/**
 * 정렬된 목록을 PC별로 묶는다. PC 순서는 정렬에서 그 PC 세션이 처음 나온 자리라 상태순이면
 * 승인 대기가 있는 PC가 위로 온다. PC 안의 순서도 정렬을 그대로 따른다.
 */
export function groupSessions(sorted: ActiveSession[]): SessionGroup[] {
  const groups = new Map<string, ActiveSession[]>();
  for (const row of sorted) {
    groups.set(row.deviceId, [...(groups.get(row.deviceId) ?? []), row]);
  }
  return [...groups.entries()].map(([deviceId, rows]) => {
    // 이름 없는 세션끼리는 묶지 않는다("(이름 없음)" 소제목은 뜻이 없다).
    const names = rows.map((row) => sessionName(row.session));
    const repeated = new Set(names.filter((name, i) => name && names.indexOf(name) !== i));
    const projects: SessionGroup["projects"] = [];
    rows.forEach((row, i) => {
      const name = names[i];
      const shared = name && repeated.has(name) ? name : null;
      const bucket = shared ? projects.find((project) => project.workspace === shared) : undefined;
      if (bucket) {
        bucket.rows.push(row);
      } else {
        projects.push({ workspace: shared, rows: [row] });
      }
    });
    const rank = Math.min(...rows.map((row) => statusRank(row.turnState)));
    return {
      deviceId,
      name: rows[0]!.deviceName,
      status: GROUP_STATUS[rank]!,
      waiting: rows.filter((row) => row.turnState === "WAITING_USER").length,
      count: rows.length,
      projects,
    };
  });
}

function savedCollapsed(): Set<string> {
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(COLLAPSED_STORAGE_KEY) ?? "[]");
    return new Set(Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : []);
  } catch {
    return new Set();
  }
}

function savedSort(): SessionSort {
  try {
    const value = window.localStorage.getItem(SORT_STORAGE_KEY);
    return value === "recent" || value === "name" || value === "started" || value === "status" ? value : "status";
  } catch {
    return "status";
  }
}

/** 지금 활성(종료되지 않은) 세션 목록. 최근 활동 순(#162). 화면 정렬은 sortSessions(#244). */
export function activeSessions(state: OfficeState): ActiveSession[] {
  return state.sessions
    .filter((session) => session.lifecycle !== "ENDED" && session.ended_at === null)
    .map((session) => {
      const agents = state.agents.filter((agent) => agent.session_id === session.id && agent.ended_at === null);
      const lead = agents.find((agent) => agent.role === "lead") ?? null;
      return {
        session,
        deviceId: session.device_id,
        deviceName: whoFor(state.devices.find((device) => device.id === session.device_id)),
        leadId: lead?.id ?? null,
        turnState: lead?.turn_state ?? null,
        headline: lead ? headlineFor(lead) : "",
        subagents: agents.filter((agent) => agent.role === "subagent").length,
      };
    })
    .sort((a, b) => (b.session.last_event_at ?? "").localeCompare(a.session.last_event_at ?? ""));
}

/**
 * 사무실 왼쪽 위에 겹쳐 보이는 활성 세션 목록. 누르면 그 세션 대장의 상세를 연다.
 * PC가 2대 이상이면 PC별로 묶고 머리줄로 접고 편다(#271). PC가 1대면 예전처럼 한 줄 목록.
 * `unnamed`(팀·통합 화면)이면 이름이 없는 세션을 "(이름 없음)"으로 보여 준다(#197).
 * `selected`가 세션 대장이면 그 항목을 강조한다(지도에서도 그 캐릭터를 따라가고 강조).
 * `onHoverDevice`는 PC 머리줄에 마우스를 올리면 그 PC를, 내리면 null을 알린다(지도 강조).
 */
export function SessionList({
  state,
  onSelect,
  onHoverDevice = () => undefined,
  unnamed = false,
  selected = null,
}: {
  state: OfficeState;
  onSelect: (agentId: string) => void;
  onHoverDevice?: (deviceId: string | null) => void;
  unnamed?: boolean;
  selected?: string | null;
}) {
  const [sort, setSort] = useState<SessionSort>(() => (typeof window === "undefined" ? "status" : savedSort()));
  const [collapsed, setCollapsed] = useState<Set<string>>(() => (typeof window === "undefined" ? new Set() : savedCollapsed()));
  const sessions = sortSessions(activeSessions(state), sort);
  const groups = groupSessions(sessions);
  const grouped = groups.length > 1;
  const choose = (next: SessionSort) => {
    setSort(next);
    try {
      window.localStorage.setItem(SORT_STORAGE_KEY, next);
    } catch {
      // 저장할 수 없으면 이번 화면에서만 쓴다.
    }
  };
  const toggle = (deviceId: string) => {
    const next = new Set(collapsed);
    if (!next.delete(deviceId)) {
      next.add(deviceId);
    }
    setCollapsed(next);
    try {
      window.localStorage.setItem(COLLAPSED_STORAGE_KEY, JSON.stringify([...next]));
    } catch {
      // 저장할 수 없으면 이번 화면에서만 쓴다.
    }
  };
  const row = ({ session, deviceName, leadId, headline, subagents }: ActiveSession, inProject: boolean) => {
    const workspace = inProject ? "" : sessionName(session, unnamed);
    return (
      <li key={session.id}>
        <button
          type="button"
          disabled={!leadId}
          aria-pressed={leadId !== null && leadId === selected}
          onClick={() => leadId && onSelect(leadId)}
        >
          <span className="who">
            {grouped ? PROVIDER_LABELS[session.provider] : `${deviceName} · ${PROVIDER_LABELS[session.provider]}`}
          </span>
          {workspace && (
            <span className="workspace" title={workspace}>
              {shortWorkspace(workspace, 32)}
            </span>
          )}
          <span className="what">
            {headline}
            {subagents > 0 && ` · 하위 ${subagents}`}
          </span>
        </button>
      </li>
    );
  };
  return (
    <aside className="session-list" aria-label="활성 세션">
      <h2>
        활성 세션 <span className="count">{sessions.length}</span>
        <select className="session-sort" aria-label="세션 정렬" value={sort} onChange={(event) => choose(event.target.value as SessionSort)}>
          {(Object.keys(SESSION_SORT_LABELS) as SessionSort[]).map((key) => (
            <option key={key} value={key}>
              {SESSION_SORT_LABELS[key]}
            </option>
          ))}
        </select>
      </h2>
      {sessions.length === 0 ? (
        <p className="empty">지금 작업 중인 세션이 없습니다</p>
      ) : !grouped ? (
        <ul>{groups[0]!.projects.flatMap((project) => project.rows.map((item) => row(item, false)))}</ul>
      ) : (
        <ul>
          {groups.map((group) => {
            const open = !collapsed.has(group.deviceId);
            return (
              <li key={group.deviceId} className="device-group">
                <button
                  type="button"
                  className="device-head"
                  aria-expanded={open}
                  onClick={() => toggle(group.deviceId)}
                  onMouseEnter={() => onHoverDevice(group.deviceId)}
                  onMouseLeave={() => onHoverDevice(null)}
                  onFocus={() => onHoverDevice(group.deviceId)}
                  onBlur={() => onHoverDevice(null)}
                >
                  <span className="caret" aria-hidden="true">
                    {open ? "▾" : "▸"}
                  </span>
                  <span className={`dot ${group.status}`} aria-hidden="true" />
                  <span className="device-name" title={group.name}>
                    {group.name}
                  </span>
                  <span className="device-count">{group.count}</span>
                  {group.waiting > 0 && (
                    <span className="device-waiting" aria-label={`승인 대기 ${group.waiting}`}>
                      ✋{group.waiting}
                    </span>
                  )}
                </button>
                {open && (
                  <ul>
                    {group.projects.map((project) =>
                      project.workspace ? (
                        <li key={`p:${project.workspace}`} className="project-group">
                          <span className="project-name" title={project.workspace}>
                            {shortWorkspace(project.workspace, 28)}
                          </span>
                          <ul>{project.rows.map((item) => row(item, true))}</ul>
                        </li>
                      ) : (
                        project.rows.map((item) => row(item, false))
                      ),
                    )}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </aside>
  );
}

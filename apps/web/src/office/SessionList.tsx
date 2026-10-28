import { headlineFor } from "./headline";
import { shortLabel } from "./sprites";
import type { DeviceState, OfficeState, SessionState } from "./types";
import { sessionName, shortWorkspace } from "./workspace";

const PROVIDER_LABELS: Record<SessionState["provider"], string> = {
  claude: "Claude",
  codex: "Codex",
  unknown: "도구",
};

export interface ActiveSession {
  session: SessionState;
  /** 행 맨 앞의 "누구" 이름. 팀·통합은 사람 이름(통합의 내 PC는 "내 이름 · PC 이름", #206), 개인은 PC 이름. */
  deviceName: string;
  leadId: string | null;
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

/** 지금 활성(종료되지 않은) 세션 목록. 최근 활동 순(#162). */
export function activeSessions(state: OfficeState): ActiveSession[] {
  return state.sessions
    .filter((session) => session.lifecycle !== "ENDED" && session.ended_at === null)
    .map((session) => {
      const agents = state.agents.filter((agent) => agent.session_id === session.id && agent.ended_at === null);
      const lead = agents.find((agent) => agent.role === "lead") ?? null;
      return {
        session,
        deviceName: whoFor(state.devices.find((device) => device.id === session.device_id)),
        leadId: lead?.id ?? null,
        headline: lead ? headlineFor(lead) : "",
        subagents: agents.filter((agent) => agent.role === "subagent").length,
      };
    })
    .sort((a, b) => (b.session.last_event_at ?? "").localeCompare(a.session.last_event_at ?? ""));
}

/**
 * 사무실 왼쪽 위에 겹쳐 보이는 활성 세션 목록. 누르면 그 세션 대장의 상세를 연다.
 * `unnamed`(팀·통합 화면)이면 이름이 없는 세션을 "(이름 없음)"으로 보여 준다(#197).
 */
export function SessionList({ state, onSelect, unnamed = false }: { state: OfficeState; onSelect: (agentId: string) => void; unnamed?: boolean }) {
  const sessions = activeSessions(state);
  return (
    <aside className="session-list" aria-label="활성 세션">
      <h2>
        활성 세션 <span className="count">{sessions.length}</span>
      </h2>
      {sessions.length === 0 ? (
        <p className="empty">지금 작업 중인 세션이 없습니다</p>
      ) : (
        <ul>
          {sessions.map(({ session, deviceName, leadId, headline, subagents }) => {
            const workspace = sessionName(session, unnamed);
            return (
              <li key={session.id}>
                <button type="button" disabled={!leadId} onClick={() => leadId && onSelect(leadId)}>
                  <span className="who">
                    {deviceName} · {PROVIDER_LABELS[session.provider]}
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
          })}
        </ul>
      )}
    </aside>
  );
}

import { headlineFor } from "./headline";
import type { OfficeState, SessionState } from "./types";

const PROVIDER_LABELS: Record<SessionState["provider"], string> = {
  claude: "Claude",
  codex: "Codex",
  unknown: "도구",
};

export interface ActiveSession {
  session: SessionState;
  deviceName: string;
  leadId: string | null;
  headline: string;
  subagents: number;
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
        deviceName: state.devices.find((device) => device.id === session.device_id)?.name ?? "PC",
        leadId: lead?.id ?? null,
        headline: lead ? headlineFor(lead) : "",
        subagents: agents.filter((agent) => agent.role === "subagent").length,
      };
    })
    .sort((a, b) => (b.session.last_event_at ?? "").localeCompare(a.session.last_event_at ?? ""));
}

/** 사무실 왼쪽 위에 겹쳐 보이는 활성 세션 목록. 누르면 그 세션 대장의 상세를 연다. */
export function SessionList({ state, onSelect }: { state: OfficeState; onSelect: (agentId: string) => void }) {
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
          {sessions.map(({ session, deviceName, leadId, headline, subagents }) => (
            <li key={session.id}>
              <button type="button" disabled={!leadId} onClick={() => leadId && onSelect(leadId)}>
                <span className="who">
                  {deviceName} · {PROVIDER_LABELS[session.provider]}
                </span>
                <span className="what">
                  {headline}
                  {subagents > 0 && ` · 하위 ${subagents}`}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </aside>
  );
}

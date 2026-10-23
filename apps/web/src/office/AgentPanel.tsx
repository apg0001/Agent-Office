import { useEffect } from "react";

import {
  ACTIVITY_LABELS,
  EVENT_LABELS,
  LIFECYCLE_LABELS,
  PLAN_LABELS,
  TURN_LABELS,
  label,
  relativeTime,
  type AgentDetail,
} from "./detail";
import type { AgentState, DataSource } from "./types";

interface Props {
  detail: AgentDetail;
  source: DataSource;
  now: number;
  onClose: () => void;
  onSelect: (agentId: string) => void;
}

function roleLabel(agent: AgentState): string {
  return agent.role === "lead" ? "리드" : "하위 에이전트";
}

/** 화면 구분용 짧은 ID(끝 8자). 서버 ID라서 Collector 원본 ID와 무관하다. */
function shortId(id: string): string {
  return id.slice(-8);
}

/** 캐릭터 상세 패널(#51). 사실 상태만 보여 주고 추정은 추정이라고 표시한다. */
export function AgentPanel({ detail, source, now, onClose, onSelect }: Props) {
  const { agent, session, device, parent, children, timeline, plan_steps: plan } = detail;

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const related = (other: AgentState) => (
    <button type="button" className="link-button" onClick={() => onSelect(other.id)}>
      {roleLabel(other)} {shortId(other.id)} · {label(TURN_LABELS, other.turn_state)}
    </button>
  );

  return (
    <aside className="agent-panel" aria-label="에이전트 상세">
      <header>
        <h2>
          {roleLabel(agent)} <code>{shortId(agent.id)}</code>
        </h2>
        <button type="button" onClick={onClose} aria-label="상세 닫기">
          ×
        </button>
      </header>
      {source === "synthetic" && <p className="panel-note">합성 데이터(데모) — 실제 관측이 아닙니다</p>}
      <dl>
        <dt>상태</dt>
        <dd>
          {label(TURN_LABELS, agent.turn_state)} · {label(ACTIVITY_LABELS, agent.activity)}
          {agent.confidence === "inferred" && <span className="inferred"> (추정)</span>}
        </dd>
        <dt>세션</dt>
        <dd>
          {session.provider} · {label(LIFECYCLE_LABELS, session.lifecycle)} · 마지막 활동{" "}
          {relativeTime(session.last_event_at, now)}
        </dd>
        <dt>PC</dt>
        <dd>
          {device.name} · {device.connection === "ONLINE" ? "연결됨" : "연결 끊김"}
        </dd>
        <dt>책상</dt>
        <dd>{session.desk_slot === null ? "없음" : `${session.desk_slot + 1}번`}</dd>
        {parent && (
          <>
            <dt>상위</dt>
            <dd>{related(parent)}</dd>
          </>
        )}
        {children.length > 0 && (
          <>
            <dt>하위</dt>
            <dd>
              <ul>
                {children.map((child) => (
                  <li key={child.id}>{related(child)}</li>
                ))}
              </ul>
            </dd>
          </>
        )}
      </dl>
      {plan.length > 0 && (
        <section aria-label="계획 단계">
          <h3>계획</h3>
          <ol>
            {plan.map((step) => (
              <li key={step.step_index}>
                {`${step.step_index + 1}단계 · ${label(PLAN_LABELS, step.status)}`}
              </li>
            ))}
          </ol>
        </section>
      )}
      <section aria-label="타임라인">
        <h3>타임라인</h3>
        {timeline.length === 0 ? (
          <p className="panel-note">{source === "server" ? "기록된 이벤트가 없습니다" : "합성 데이터에는 타임라인이 없습니다"}</p>
        ) : (
          <ol className="timeline">
            {timeline.map((entry, index) => (
              <li key={`${entry.occurred_at}-${index}`}>
                <time dateTime={entry.occurred_at}>{relativeTime(entry.occurred_at, now)}</time>{" "}
                {label(EVENT_LABELS, entry.event_type)}
                {entry.activity !== "unknown" && ` · ${label(ACTIVITY_LABELS, entry.activity)}`}
                {entry.confidence === "inferred" && <span className="inferred"> (추정)</span>}
              </li>
            ))}
          </ol>
        )}
      </section>
    </aside>
  );
}

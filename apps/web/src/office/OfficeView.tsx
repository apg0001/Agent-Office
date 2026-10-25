import { useCallback, useEffect, useRef, useState } from "react";

import { AgentPanel } from "./AgentPanel";

import { deskRowsFor } from "./behavior";
import { detailFromState, fetchAgentDetail, type AgentDetail } from "./detail";
import { createDefaultOffice } from "./grid";
import { type OfficeScope, PERSONAL, connectLive, hasSession, scopeFromSearch, searchFor } from "./liveSource";
import { advanceSynthetic, createSyntheticOffice } from "./synthetic";
import type { DataSource, OfficeState } from "./types";

/**
 * 사무실 화면. 로그인 세션이 있으면 서버 실시간 상태를, 없으면 합성 데이터를 그린다.
 * Phaser는 브라우저에서만 지연 로드한다(테스트 환경에서 캔버스를 쓰지 않도록).
 */
interface TeamOption {
  id: string;
  name: string;
}

/** 내가 속한 팀 목록. 범위 선택·나란히 보기가 쓴다. */
export function useTeams(): TeamOption[] {
  const [teams, setTeams] = useState<TeamOption[]>([]);
  useEffect(() => {
    void fetch("/api/v1/teams", { credentials: "same-origin" })
      .then((response) => (response.ok ? response.json() : []))
      .then((list: TeamOption[]) => setTeams(list))
      .catch(() => setTeams([]));
  }, []);
  return teams;
}

function scopeValue(scope: OfficeScope): string {
  return scope.kind === "team" ? `team:${scope.teamId}` : scope.kind;
}

/** 내 사무실·통합·나란히·팀 사무실 중 하나를 고른다. 바꾸면 그 범위로 다시 연결한다. */
export function ScopePicker({ scope }: { scope: OfficeScope }) {
  const teams = useTeams();
  if (teams.length === 0) {
    return null;
  }
  return (
    <label className="scope-picker">
      사무실{" "}
      <select
        value={scopeValue(scope)}
        onChange={(event) => {
          const value = event.target.value;
          const next: OfficeScope = value.startsWith("team:")
            ? { kind: "team", teamId: value.slice(5) }
            : { kind: value as "personal" | "combined" | "side" };
          window.location.assign(`/${searchFor(next)}`);
        }}
      >
        <option value="personal">내 사무실</option>
        <option value="combined">통합 사무실(내 것 + 팀 공유)</option>
        <option value="side">나란히 보기</option>
        {teams.map((option) => (
          <option key={option.id} value={`team:${option.id}`}>
            팀: {option.name}
          </option>
        ))}
      </select>
    </label>
  );
}

const SCOPE_NOTES: Record<OfficeScope["kind"], string> = {
  personal: "",
  team: " · 팀 사무실(공유된 프로젝트만)",
  combined: " · 통합 사무실(내 것 + 팀에 공유된 것, 중복 없이)",
  side: "",
};

interface OfficeViewProps {
  /** 생략하면 주소(`?team=`·`?view=`)에서 읽는다. */
  scope?: OfficeScope;
  /** 나란히 보기용 작은 화면: 안내·선택 상자를 숨기고 높이를 줄인다. */
  compact?: boolean;
  title?: string;
}

export function OfficeView({ scope: given, compact = false, title }: OfficeViewProps = {}) {
  const host = useRef<HTMLDivElement>(null);
  const [scope] = useState<OfficeScope>(
    () => given ?? (typeof window === "undefined" ? PERSONAL : scopeFromSearch(window.location.search)),
  );
  const [source, setSource] = useState<DataSource>("synthetic");
  const [online, setOnline] = useState(false);
  const [autoFocus, setAutoFocus] = useState(true);
  const resume = useRef<() => void>(() => undefined);
  const [office, setOffice] = useState<OfficeState | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [remote, setRemote] = useState<AgentDetail | null>(null);
  const close = useCallback(() => setSelected(null), []);

  useEffect(() => {
    const element = host.current;
    if (!element) {
      return undefined;
    }
    let disposed = false;
    let game: { destroy: (removeCanvas: boolean) => void } | undefined;
    let stop: (() => void) | undefined;
    void (async () => {
      const [{ default: Phaser }, { OfficeScene }, live] = await Promise.all([
        import("phaser"),
        import("./OfficeScene"),
        hasSession(),
      ]);
      if (disposed) {
        return;
      }
      const initial = createSyntheticOffice({ sessions: 4, subagentsPerSession: 1 });
      const scene = new OfficeScene(
        createDefaultOffice(2),
        live ? { source: "server", devices: [], sessions: [], agents: [] } : initial,
      );
      scene.onCameraChange = (camera) => setAutoFocus(camera.autoFocus);
      resume.current = () => scene.resumeFocus();
      scene.onSelect = setSelected;
      game = new Phaser.Game({
        type: Phaser.AUTO,
        parent: element,
        width: element.clientWidth || 960,
        height: compact ? 320 : 560,
        backgroundColor: "#2b2d31",
        pixelArt: true,
        scene,
      });
      let rows = 2;
      const show = (state: Parameters<typeof scene.applyState>[0]) => {
        const needed = deskRowsFor(state);
        if (needed !== rows) {
          // 책상이 모자랄 때만 사무실을 넓힌다. 기존 자리 좌표는 바뀌지 않는다.
          rows = needed;
          scene.setMap(createDefaultOffice(rows));
        }
        scene.applyState(state);
        setOffice(state);
      };
      if (live) {
        setSource("server");
        const handle = connectLive(show, setOnline, scope);
        stop = handle.close;
        return;
      }
      let tick = 0;
      let current = initial;
      const timer = window.setInterval(() => {
        tick += 1;
        current = advanceSynthetic(current, tick);
        show(current);
      }, 2500);
      stop = () => window.clearInterval(timer);
    })();
    return () => {
      disposed = true;
      stop?.();
      game?.destroy(true);
    };
  }, []);

  const local = office && selected ? detailFromState(office, selected) : null;
  const changeSeq = local?.agent.change_seq;
  useEffect(() => {
    // 서버 모드는 타임라인·계획을 위해 상세를 다시 가져온다(선택 대상 상태가 바뀔 때마다).
    if (source !== "server" || !selected) {
      setRemote(null);
      return undefined;
    }
    let current = true;
    void fetchAgentDetail(selected, fetch, scope).then((detail) => {
      if (current) {
        setRemote(detail);
      }
    });
    return () => {
      current = false;
    };
  }, [source, selected, changeSeq, scope]);
  // 실시간 상태가 최신이므로 기본 정보는 로컬, 타임라인·계획만 서버 응답을 쓴다.
  const detail =
    local && remote?.agent.id === local.agent.id
      ? { ...local, timeline: remote.timeline, plan_steps: remote.plan_steps }
      : local;

  return (
    <section className={compact ? "office compact" : "office"} aria-label={title ?? "사무실"}>
      {source === "synthetic" ? (
        <p className="synthetic-badge" role="status">
          합성 데이터(데모) — 실제 관측이 아닙니다
        </p>
      ) : (
        <>
          <p className="live-badge" role="status">
            {online ? "실시간 연결됨" : "서버 연결 끊김 — 재접속 중"}
            {SCOPE_NOTES[scope.kind]}
          </p>
          {!compact && <ScopePicker scope={scope} />}
        </>
      )}
      {title && <h3 className="office-title">{title}</h3>}
      {!autoFocus && (
        <button type="button" className="focus-button" onClick={() => resume.current()}>
          자동 초점 다시 켜기
        </button>
      )}
      <div ref={host} className="office-canvas" tabIndex={0} aria-label="사무실 지도" />
      {!compact && <p className="office-help">드래그·방향키로 이동, 휠·+/−로 확대, F로 자동 초점</p>}
      {detail && (
        <AgentPanel detail={detail} source={source} now={Date.now()} onClose={close} onSelect={setSelected} />
      )}
    </section>
  );
}

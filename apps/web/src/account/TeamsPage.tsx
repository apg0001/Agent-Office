import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";

import { Card, Field, Notice } from "./AccountPages";
import { type SessionInfo, call, messageFor } from "./api";
import { ShareConsent } from "./ShareConsent";
import {
  DEVICE_NAME_SHARE_HELP,
  DEVICE_NAME_SHARE_IDLE,
  DEVICE_NAME_SHARE_LABEL,
  DEVICE_NAME_SHARE_NONE_NOTE,
  type DeviceNameShareView,
  deviceNameShareShown,
  loadUnlessChanged,
  settleDeviceNameShare,
  ROLE_LABELS,
  SHARE_MODES,
  SHARE_MODE_HELP,
  SHARE_MODE_LABELS,
  SHARE_VISIBLE_TEXT,
  type ShareMode,
  type TeamRole,
  canRemove,
  shareModeFromConsent,
  supportsDeviceNameShare,
  teamPermissions,
  toggleShareDeviceName,
} from "./teams";

export interface TeamSummary {
  id: string;
  name: string;
  description: string | null;
  role: TeamRole;
  /** 내가 이 팀에 보여 주는 범위(#197). */
  share_mode: ShareMode;
  /** 내가 이 팀에 PC 이름을 보여 주는지(기본 끔). 이전 서버는 보내지 않는다. */
  share_device_name?: boolean;
}

interface Member {
  user_id: string;
  display_name: string;
  role: TeamRole;
}

interface TeamDetail extends TeamSummary {
  members: Member[];
}

interface Invitation {
  id: string;
  email: string;
  role: TeamRole;
  expires_at: string;
}

interface Project {
  id: string;
  display_name: string;
  /** "workspace"면 작업 폴더 이름으로 자동 생성한 프로젝트(#197). */
  origin?: "manual" | "workspace";
  shared_team_ids: string[];
}

interface OfficeSession {
  id: string;
  provider: string;
  lifecycle: string;
  started_at: string | null;
  project_id?: string | null;
  workspace_name?: string | null;
}

function ProjectName({ project }: { project: Project }) {
  return (
    <>
      {project.display_name}
      {project.origin === "workspace" && <span className="origin-chip">폴더</span>}
    </>
  );
}

/**
 * 팀별 내 공유 범위(#197). 바꾸면 팀 사무실 스냅샷·실시간·상세에 바로 반영된다.
 * "선택한 프로젝트만"이면 이 팀에 공유할 프로젝트를 체크한다(폴더 이름 자동 프로젝트 포함).
 */
function ShareSettings({ team, csrf, act, onChanged }: { team: TeamSummary; csrf: string; act: Act; onChanged: () => void }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const mode = team.share_mode;

  const loadProjects = useCallback(async () => {
    setProjects((await call<Project[]>("GET", "/api/v1/projects")).data ?? []);
  }, []);

  useEffect(() => {
    if (mode === "selected") {
      void loadProjects();
    }
  }, [mode, loadProjects]);

  return (
    <fieldset className="choices share-settings">
      <legend>이 팀에 보여 줄 내 작업</legend>
      <p className="hint">{SHARE_VISIBLE_TEXT}</p>
      {SHARE_MODES.map((option) => (
        <label key={option}>
          <input
            type="radio"
            name={`share-mode-${team.id}`}
            value={option}
            checked={mode === option}
            onChange={() =>
              void act(() => call("PUT", `/api/v1/teams/${team.id}/share-mode`, { share_mode: option }, csrf)).then((ok) => {
                if (ok) {
                  onChanged();
                }
              })
            }
          />{" "}
          {SHARE_MODE_LABELS[option]}
          <span className="hint"> — {SHARE_MODE_HELP[option]}</span>
        </label>
      ))}
      {mode === "selected" && (
        <ul className="project-checklist" aria-label={`${team.name}에 공유할 프로젝트`}>
          {projects.length === 0 && <li className="hint">아직 프로젝트가 없습니다. 폴더 이름을 켠 PC에서 작업하면 자동으로 생깁니다.</li>}
          {projects.map((project) => {
            const shared = project.shared_team_ids.includes(team.id);
            return (
              <li key={project.id}>
                <label>
                  <input
                    type="checkbox"
                    checked={shared}
                    onChange={() =>
                      void act(() =>
                        call(shared ? "DELETE" : "PUT", `/api/v1/projects/${project.id}/shares/${team.id}`, undefined, csrf),
                      ).then(async (ok) => {
                        if (ok) {
                          await loadProjects();
                        }
                      })
                    }
                  />{" "}
                  <ProjectName project={project} />
                </label>
              </li>
            );
          })}
        </ul>
      )}
    </fieldset>
  );
}

/**
 * 저장에 성공한 팀의 `share_device_name`만 바꾼 새 목록. 다른 팀과 다른 필드는 그대로다.
 * PC 이름 공개는 이 방식으로 목록 값만 고친다. reload()로 version을 올리면 카드가 이전 응답으로 다시 만들어진다.
 */
export function patchShareDeviceName<T extends { id: string; share_device_name?: boolean }>(teams: T[], teamId: string, value: boolean): T[] {
  return teams.map((team) => (team.id === teamId ? { ...team, share_device_name: value } : team));
}

export type Act = (action: () => Promise<{ ok: boolean; error: string | null }>) => Promise<boolean>;

/**
 * 팀별 "PC 이름 공개" 체크 상자. 기본은 꺼져 있고 내 값만 바꾼다. 저장에 실패하면 체크를 되돌리고
 * 화면 위쪽 오류 알림(`act`)에 이유를 보여 준다. 서버가 이 필드를 보내지 않으면(이전 서버) 그리지 않는다.
 */
export function DeviceNameShare({
  team,
  csrf,
  act,
  onSaved,
}: {
  team: TeamSummary;
  csrf: string;
  act: Act;
  /** 저장에 성공한 값. 부모가 팀 목록의 이 팀 값을 바로 고친다(카드를 다시 만들지 않음). */
  onSaved: (value: boolean) => void;
}) {
  // 표시 값은 props(서버 값)에서 얻고, 저장 중·저장 직후에만 덮어쓴다. 값을 state로 복사해 두면
  // 카드가 이전 응답으로 다시 만들어질 때 저장 전 값으로 돌아간다.
  const [view, setView] = useState<DeviceNameShareView>(DEVICE_NAME_SHARE_IDLE);
  const server = team.share_device_name === true;
  useEffect(() => {
    setView((current) => settleDeviceNameShare(server, current));
  }, [server]);
  return (
    <DeviceNameShareBox
      team={team}
      checked={deviceNameShareShown(server, view)}
      saving={view.pending !== null}
      onToggle={(next) =>
        void act(() =>
          toggleShareDeviceName({
            teamId: team.id,
            current: server,
            next,
            csrf,
            setView,
            onSaved,
            request: (method, path, body, token) => call<unknown>(method, path, body, token),
          }),
        )
      }
    />
  );
}

/** 체크 상자의 모양만. 값과 저장 중 여부는 밖에서 받는다. */
export function DeviceNameShareBox({
  team,
  checked,
  saving,
  onToggle,
}: {
  team: TeamSummary;
  checked: boolean;
  saving: boolean;
  onToggle: (next: boolean) => void;
}) {
  if (!supportsDeviceNameShare(team)) {
    return null;
  }
  const helpId = `device-name-share-help-${team.id}`;
  return (
    <div className="device-name-share">
      <label>
        <input
          type="checkbox"
          checked={checked}
          disabled={saving}
          aria-describedby={helpId}
          onChange={(event) => onToggle(event.target.checked)}
        />{" "}
        {DEVICE_NAME_SHARE_LABEL}
      </label>
      <p className="hint" id={helpId}>
        {DEVICE_NAME_SHARE_HELP}
        {team.share_mode === "none" && ` ${DEVICE_NAME_SHARE_NONE_NOTE}`}
      </p>
    </div>
  );
}

function TeamCard({
  team,
  me,
  csrf,
  act,
  reload,
  onDeviceNameSaved,
}: {
  team: TeamSummary;
  me: string;
  csrf: string;
  act: Act;
  reload: () => void;
  onDeviceNameSaved: (value: boolean) => void;
}) {
  const [detail, setDetail] = useState<TeamDetail | null>(null);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<TeamRole>("MEMBER");
  const [link, setLink] = useState<{ url: string; mailed: boolean } | null>(null);
  const can = teamPermissions(team.role);
  const base = `/api/v1/teams/${team.id}`;

  const load = useCallback(async () => {
    const result = await call<TeamDetail>("GET", base);
    setDetail(result.data);
    if (can.manageInvitations) {
      setInvitations((await call<Invitation[]>("GET", `${base}/invitations`)).data ?? []);
    }
  }, [base, can.manageInvitations]);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (action: () => Promise<{ ok: boolean; error: string | null }>, afterTeamChange = false) => {
    if (await act(action)) {
      if (afterTeamChange) {
        reload();
      } else {
        await load();
      }
    }
  };

  const invite = (event: FormEvent) => {
    event.preventDefault();
    void (async () => {
      const result = await call<{ invite_url: string; mailed: boolean }>("POST", `${base}/invitations`, { email, role }, csrf);
      if (await act(async () => result)) {
        setLink(result.data ? { url: result.data.invite_url, mailed: result.data.mailed } : null);
        setEmail("");
        await load();
      }
    })();
  };

  return (
    <article className="team-card" aria-label={`팀 ${team.name}`}>
      <header>
        <h3>{team.name}</h3>
        <span className="role-chip">{ROLE_LABELS[team.role]}</span>
        <span className="role-chip">{SHARE_MODE_LABELS[team.share_mode]}</span>
        <a href={`/?team=${team.id}`}>팀 사무실 보기</a>
      </header>
      <ShareSettings team={team} csrf={csrf} act={act} onChanged={reload} />
      <DeviceNameShare team={team} csrf={csrf} act={act} onSaved={onDeviceNameSaved} />
      {detail && (
        <table className="members">
          <tbody>
            {detail.members.map((member) => (
              <tr key={member.user_id}>
                <td>
                  {member.display_name}
                  {member.user_id === me ? " (나)" : ""}
                </td>
                <td>
                  {can.changeRoles && member.role !== "OWNER" ? (
                    <select
                      aria-label={`${member.display_name} 역할`}
                      value={member.role}
                      onChange={(event) =>
                        void run(() => call("PATCH", `${base}/members/${member.user_id}`, { role: event.target.value }, csrf))
                      }
                    >
                      <option value="MEMBER">{ROLE_LABELS.MEMBER}</option>
                      <option value="MANAGER">{ROLE_LABELS.MANAGER}</option>
                    </select>
                  ) : (
                    ROLE_LABELS[member.role]
                  )}
                </td>
                <td className="actions">
                  {member.user_id !== me && canRemove(team.role, member.role) && (
                    <button
                      type="button"
                      onClick={() =>
                        window.confirm(`${member.display_name}님을 팀에서 내보낼까요? 이 사람의 공유도 함께 해제됩니다.`) &&
                        void run(() => call("DELETE", `${base}/members/${member.user_id}`, undefined, csrf))
                      }
                    >
                      내보내기
                    </button>
                  )}
                  {can.transfer && member.user_id !== me && (
                    <button
                      type="button"
                      onClick={() =>
                        window.confirm(`${member.display_name}님에게 소유권을 넘길까요? 나는 관리자가 됩니다.`) &&
                        void run(() => call("POST", `${base}/transfer`, { user_id: member.user_id }, csrf), true)
                      }
                    >
                      소유권 넘기기
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {can.invite && (
        <form className="inline-form" onSubmit={invite}>
          <Field label="초대할 이메일" name="invite_email" type="email" value={email} onChange={setEmail} />
          {can.inviteRoles.length > 1 && (
            <select aria-label="초대 역할" value={role} onChange={(event) => setRole(event.target.value as TeamRole)}>
              {can.inviteRoles.map((option) => (
                <option key={option} value={option}>
                  {ROLE_LABELS[option]}
                </option>
              ))}
            </select>
          )}
          <button type="submit">초대</button>
        </form>
      )}
      {link && (
        <div className="invite-link">
          <p>{link.mailed ? "초대 메일을 보냈습니다. 아래 링크를 직접 전해도 됩니다(7일 유효, 한 번만)." : "메일 설정이 없어 메일을 보내지 않았습니다. 아래 링크를 초대받는 사람에게 직접 전하세요(7일 유효, 한 번만)."}</p>
          <code>{link.url}</code>
          <button type="button" onClick={() => void navigator.clipboard?.writeText(link.url)}>
            복사
          </button>
        </div>
      )}
      {can.manageInvitations && invitations.length > 0 && (
        <ul className="invitations" aria-label="열린 초대">
          {invitations.map((invitation) => (
            <li key={invitation.id}>
              {invitation.email} · {ROLE_LABELS[invitation.role]}{" "}
              <button type="button" onClick={() => void run(() => call("DELETE", `${base}/invitations/${invitation.id}`, undefined, csrf))}>
                취소
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="actions">
        {can.leave && (
          <button
            type="button"
            onClick={() =>
              window.confirm("이 팀에서 나갈까요? 내가 이 팀에 공유한 프로젝트도 공유가 해제됩니다.") &&
              void run(() => call("DELETE", `${base}/members/${me}`, undefined, csrf), true)
            }
          >
            팀 나가기
          </button>
        )}
        {!can.leave && <span className="hint">소유자는 소유권을 넘긴 뒤에 나갈 수 있습니다.</span>}
        {can.deleteTeam && (
          <button
            type="button"
            className="danger"
            onClick={() =>
              window.confirm(`'${team.name}' 팀을 삭제할까요? 되돌릴 수 없습니다.`) &&
              void run(() => call("DELETE", base, undefined, csrf), true)
            }
          >
            팀 삭제
          </button>
        )}
      </div>
    </article>
  );
}

function Projects({ teams, csrf, act }: { teams: TeamSummary[]; csrf: string; act: Act }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [sessions, setSessions] = useState<OfficeSession[]>([]);
  const [name, setName] = useState("");

  const load = useCallback(async () => {
    setProjects((await call<Project[]>("GET", "/api/v1/projects")).data ?? []);
    const office = await call<{ sessions: OfficeSession[] }>("GET", "/api/v1/office");
    setSessions(office.data?.sessions.filter((s) => s.lifecycle !== "ENDED") ?? []);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (action: () => Promise<{ ok: boolean; error: string | null }>) => {
    if (await act(action)) {
      await load();
    }
  };

  return (
    <Card title="내 프로젝트와 공유">
      <p className="hint">
        PC에서 폴더 이름을 켜 두면 세션이 그 폴더 이름의 프로젝트에 자동으로 들어갑니다(폴더 표시). 직접 고른 프로젝트가 자동 배정보다
        우선합니다. 팀에 무엇이 보이는지는 팀마다 고른 공유 범위로 정해집니다.
      </p>
      <form
        className="inline-form"
        onSubmit={(event) => {
          event.preventDefault();
          void run(() => call("POST", "/api/v1/projects", { display_name: name }, csrf)).then(() => setName(""));
        }}
      >
        <Field label="새 프로젝트 이름" name="project_name" value={name} onChange={setName} />
        <button type="submit">만들기</button>
      </form>
      {projects.map((project) => (
        <fieldset key={project.id} className="choices">
          <legend>
            <ProjectName project={project} />
          </legend>
          {teams.map((team) => {
            const shared = project.shared_team_ids.includes(team.id);
            return (
              <label key={team.id}>
                <input
                  type="checkbox"
                  checked={shared}
                  onChange={() =>
                    void run(() => call(shared ? "DELETE" : "PUT", `/api/v1/projects/${project.id}/shares/${team.id}`, undefined, csrf))
                  }
                />{" "}
                {team.name}에 공유
              </label>
            );
          })}
        </fieldset>
      ))}
      {sessions.length > 0 && projects.length > 0 && (
        <table className="members" aria-label="열린 세션">
          <tbody>
            {sessions.map((session) => (
              <tr key={session.id}>
                <td>
                  {session.provider} 세션 {session.workspace_name ?? session.id.slice(-6)}
                </td>
                <td>
                  <select
                    aria-label="세션 프로젝트"
                    value={session.project_id ?? ""}
                    onChange={(event) =>
                      void run(() =>
                        call("PUT", `/api/v1/office/sessions/${session.id}/project`, { project_id: event.target.value || null }, csrf),
                      )
                    }
                  >
                    <option value="">프로젝트 없음</option>
                    {projects.map((project) => (
                      <option key={project.id} value={project.id}>
                        {project.display_name}
                      </option>
                    ))}
                  </select>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

/** 팀 관리(#52): 만들기·초대·역할·내보내기·소유권 이전·나가기·삭제, 프로젝트 공유. */
export function TeamsPage() {
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);
  const [teams, setTeams] = useState<TeamSummary[]>([]);
  const [name, setName] = useState("");
  const [shareAll, setShareAll] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => setSession(result.data));
  }, []);

  // PC 이름 공개를 저장할 때마다 올린다. 저장보다 먼저 나간 GET /teams 응답이 늦게 와서
  // 방금 고친 목록을 저장 전 값으로 덮지 않게 한다.
  const savedEpoch = useRef(0);

  useEffect(() => {
    if (!session) {
      return undefined;
    }
    let cancelled = false;
    void loadUnlessChanged(
      () => savedEpoch.current,
      () => call<TeamSummary[]>("GET", "/api/v1/teams"),
    ).then((loaded) => {
      // 끝까지 저장과 겹쳤으면 지금 목록(저장 값이 반영된 것)을 그대로 둔다.
      if (!cancelled && loaded.fresh) {
        setTeams(loaded.value.data ?? []);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [session, version]);

  const act: Act = async (action) => {
    setError(null);
    const result = await action();
    if (!result.ok) {
      setError(messageFor(result.error));
    }
    return result.ok;
  };
  const reload = () => setVersion((value) => value + 1);

  if (session === undefined) {
    return <Card title="팀">{null}</Card>;
  }
  if (session === null) {
    return (
      <Card title="팀">
        <Notice error={messageFor("unauthorized")} />
        <p className="account-links">
          <a href="/login">로그인</a>
        </p>
      </Card>
    );
  }
  const csrf = session.csrf_token;
  return (
    <>
      <Card title="팀">
        <Notice error={error} />
        <form
          className="inline-form"
          onSubmit={(event) => {
            event.preventDefault();
            void act(() => call("POST", "/api/v1/teams", { name, share_mode: shareModeFromConsent(shareAll) }, csrf)).then((ok) => {
              if (ok) {
                setName("");
                setShareAll(true);
                reload();
              }
            });
          }}
        >
          <Field label="새 팀 이름" name="team_name" value={name} onChange={setName} />
          <button type="submit">팀 만들기</button>
        </form>
        <ShareConsent checked={shareAll} onChange={setShareAll} />
        {teams.length === 0 && <p className="hint">아직 속한 팀이 없습니다. 팀을 만들거나 초대 링크를 받으세요.</p>}
        {teams.map((team) => (
          <TeamCard
            key={`${team.id}-${team.role}-${team.share_mode}-${version}`}
            team={team}
            me={session.user.id}
            csrf={csrf}
            act={act}
            reload={reload}
            onDeviceNameSaved={(value) => {
              savedEpoch.current += 1;
              setTeams((list) => patchShareDeviceName(list, team.id, value));
            }}
          />
        ))}
      </Card>
      <Projects key={version} teams={teams} csrf={csrf} act={act} />
    </>
  );
}

import { useCallback, useEffect, useState } from "react";

import { Card, Notice } from "./AccountPages";
import { call, messageFor } from "./api";

interface AdminUser {
  id: string;
  email: string;
  display_name: string;
  instance_role: "SUPER_ADMIN" | "ADMIN" | "USER";
  status: "pending" | "active" | "disabled";
  email_verified: boolean;
  created_at: string;
}

interface AuditEntry {
  id: number;
  occurred_at: string;
  action: string;
  outcome: string;
  actor_display_name: string | null;
  actor_device: boolean;
  target_type: string | null;
  details: Record<string, string>;
}

const ROLE_LABELS = { SUPER_ADMIN: "최고 관리자", ADMIN: "관리자", USER: "사용자" } as const;

/** 상태 표시. 이메일 확인 뒤 승인을 기다리는 사용자를 따로 구분한다. */
export function statusLabel(user: Pick<AdminUser, "status" | "email_verified">): string {
  if (user.status === "pending") {
    return user.email_verified ? "승인 대기" : "이메일 확인 전";
  }
  return user.status === "active" ? "활성" : "비활성";
}

/** 승인 대기를 맨 위로, 그다음 최근 가입 순. */
export function sortUsers(users: AdminUser[]): AdminUser[] {
  const awaiting = (u: AdminUser) => (u.status === "pending" && u.email_verified ? 0 : 1);
  return [...users].sort((a, b) => awaiting(a) - awaiting(b) || b.created_at.localeCompare(a.created_at));
}

export function AdminUsers({ me, myRole, csrf }: { me: string; myRole: string; csrf: string }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [pending, setPending] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const result = await call<{ users: AdminUser[]; pending_approval: number }>("GET", "/api/v1/admin/users?limit=200");
    setUsers(result.data ? sortUsers(result.data.users) : []);
    setPending(result.data?.pending_approval ?? 0);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const act = async (method: string, path: string, body?: unknown) => {
    setError(null);
    const result = await call(method, path, body, csrf);
    if (!result.ok) {
      setError(
        result.error === "last_super_admin"
          ? "마지막 최고 관리자는 강등·비활성화할 수 없습니다."
          : result.error === "cannot_modify_self"
            ? "자기 계정은 비활성화할 수 없습니다."
            : messageFor(result.error),
      );
    }
    await load();
  };

  return (
    <Card title={`사용자${pending ? ` · 승인 대기 ${pending}명` : ""}`}>
      <Notice error={error} />
      <table className="members">
        <tbody>
          {users.map((user) => {
            const base = `/api/v1/admin/users/${user.id}`;
            return (
              <tr key={user.id}>
                <td>
                  {user.display_name}
                  {user.id === me ? " (나)" : ""}
                  <br />
                  <span className="hint">{user.email}</span>
                </td>
                <td>{statusLabel(user)}</td>
                <td>
                  {myRole === "SUPER_ADMIN" ? (
                    <select
                      aria-label={`${user.display_name} 역할`}
                      value={user.instance_role}
                      onChange={(event) => void act("PUT", `${base}/role`, { instance_role: event.target.value })}
                    >
                      {Object.entries(ROLE_LABELS).map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </select>
                  ) : (
                    ROLE_LABELS[user.instance_role]
                  )}
                </td>
                <td className="actions">
                  {user.status === "pending" && user.email_verified && (
                    <button type="button" onClick={() => void act("POST", `${base}/approve`)}>
                      승인
                    </button>
                  )}
                  {user.status !== "disabled" && user.id !== me && (
                    <button
                      type="button"
                      onClick={() =>
                        window.confirm(`${user.display_name} 계정을 비활성화할까요? 로그인 중인 세션도 끊깁니다.`) &&
                        void act("POST", `${base}/disable`)
                      }
                    >
                      비활성화
                    </button>
                  )}
                  {user.status === "disabled" && (
                    <button type="button" onClick={() => void act("POST", `${base}/enable`)}>
                      다시 활성화
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </Card>
  );
}

export function AuditLog() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [next, setNext] = useState<number | null>(null);
  const [filter, setFilter] = useState("");

  const load = useCallback(async (before: number | null, prefix: string) => {
    const query = new URLSearchParams({ limit: "50" });
    if (before) {
      query.set("before", String(before));
    }
    if (/^[a-z][a-z0-9_.]*$/.test(prefix)) {
      query.set("action", prefix);
    }
    const result = await call<{ entries: AuditEntry[]; next_before: number | null }>("GET", `/api/v1/admin/audit?${query}`);
    const page = result.data?.entries ?? [];
    setEntries((current) => (before ? [...current, ...page] : page));
    setNext(result.data?.next_before ?? null);
  }, []);

  useEffect(() => {
    void load(null, "");
  }, [load]);

  return (
    <Card title="감사 로그">
      <form
        className="inline-form"
        onSubmit={(event) => {
          event.preventDefault();
          void load(null, filter.trim());
        }}
      >
        <label className="field">
          <span>동작 접두어(예: auth., team., settings.)</span>
          <input name="audit_filter" value={filter} onChange={(event) => setFilter(event.target.value)} />
        </label>
        <button type="submit">거르기</button>
      </form>
      <table className="members">
        <tbody>
          {entries.map((entry) => (
            <tr key={entry.id}>
              <td>{new Date(entry.occurred_at).toLocaleString()}</td>
              <td>
                <code>{entry.action}</code> · {entry.outcome === "success" ? "성공" : entry.outcome}
              </td>
              <td>{entry.actor_display_name ?? (entry.actor_device ? "Collector" : "—")}</td>
              <td className="hint">{Object.entries(entry.details).map(([k, v]) => `${k}=${v}`).join(", ")}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {next && (
        <button type="button" onClick={() => void load(next, filter.trim())}>
          더 보기
        </button>
      )}
    </Card>
  );
}

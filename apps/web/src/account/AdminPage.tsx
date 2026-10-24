import { type FormEvent, useCallback, useEffect, useState } from "react";

import { Card, Field, Notice } from "./AccountPages";
import { type SessionInfo, call, messageFor, smtpMessage } from "./api";
import { type AdminSettings, type Metrics, SETTING_BOUNDARIES, formatBytes, warningsFor } from "./admin";
import { AdminUsers, AuditLog } from "./AdminUsers";
import { NotificationChannels } from "./NotificationChannels";
import { POLICY_LABELS } from "./wizard";

const BACKUP_ERRORS: Record<string, string> = {
  backup_key_missing: "백업 키가 없습니다(AGENT_OFFICE_BACKUP_KEY).",
  no_backup: "아직 백업이 없습니다.",
  pg_dump_failed: "덤프 실패(DB 접속 확인)",
  dump_unreadable: "덤프를 읽을 수 없음",
  backup_dir_unwritable: "백업 위치에 쓸 수 없음(권한 확인)",
  offsite_unwritable: "외부 복제 위치에 쓸 수 없음(로컬 백업은 남음)",
  offsite_copy_mismatch: "외부 복제 체크섬 불일치",
  checksum_mismatch: "백업 파일 손상",
};

function when(iso: string | null | undefined): string {
  return iso ? new Date(iso).toLocaleString() : "—";
}

function okText(value: boolean | null | undefined): string {
  return value === true ? "성공" : value === false ? "실패" : "—";
}

function Diagnostics({ metrics, settings, csrf, reload }: { metrics: Metrics; settings: AdminSettings; csrf: string; reload: () => void }) {
  const [info, setInfo] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const warnings = warningsFor(metrics, settings, Date.now());
  const { host, database, collection, backup } = metrics;

  const run = async (action: "run" | "restore-test") => {
    setBusy(true);
    setInfo(null);
    setError(null);
    const result = await call<{ ok: boolean; error: string | null; name: string | null }>(
      "POST",
      `/api/v1/admin/backup/${action}`,
      undefined,
      csrf,
    );
    setBusy(false);
    if (result.data?.ok) {
      setInfo(action === "run" ? `백업 완료: ${result.data.name}` : "복원 시험 성공");
    } else {
      const code = result.data?.error ?? result.error;
      setError(code ? (BACKUP_ERRORS[code] ?? messageFor(code)) : "실패");
    }
    reload();
  };

  return (
    <Card title="상태·진단">
      {warnings.length === 0 ? (
        <Notice info="확인할 문제가 없습니다." />
      ) : (
        <ul className="warnings" aria-label="확인할 문제">
          {warnings.map((warning) => (
            <li key={warning.text} className={warning.level}>
              {warning.level === "critical" ? "긴급" : "주의"} · {warning.text}
            </li>
          ))}
        </ul>
      )}
      <table className="members metrics">
        <tbody>
          <tr>
            <th>준비 상태</th>
            <td>{metrics.readiness.ready ? "정상" : "준비 안 됨"} (DB 마이그레이션 {database.migration ?? "—"})</td>
          </tr>
          <tr>
            <th>호스트</th>
            <td>
              CPU {host.cpu_count ?? "—"}개 · 부하 {host.load_average?.join(" / ") ?? "—"} · 메모리 여유{" "}
              {formatBytes(host.memory?.available_bytes)}/{formatBytes(host.memory?.total_bytes)} · 디스크 여유{" "}
              {formatBytes(host.disk.free_bytes)}/{formatBytes(host.disk.total_bytes)}
            </td>
          </tr>
          <tr>
            <th>DB</th>
            <td>
              크기 {formatBytes(database.size_bytes)} · 응답 {database.latency_ms} ms
            </td>
          </tr>
          <tr>
            <th>수집</th>
            <td>
              사용자 {collection.users} · PC {collection.devices_online}/{collection.devices} 연결 · 열린 세션{" "}
              {collection.open_sessions} · 최근 1시간 이벤트 {collection.events_last_hour} · 지연 p95{" "}
              {collection.ingest_lag_p95_seconds ?? "—"}초 · 최대 큐 {collection.max_collector_queue_depth ?? "—"}
            </td>
          </tr>
          <tr>
            <th>Collector 버전</th>
            <td>
              {Object.entries(collection.collector_versions)
                .map(([version, count]) => `${version} ×${count}`)
                .join(", ") || "—"}
            </td>
          </tr>
          <tr>
            <th>외부 Heartbeat</th>
            <td>
              {metrics.external_heartbeat.configured
                ? `${okText(metrics.external_heartbeat.last_ok)} · ${when(metrics.external_heartbeat.last_at)}`
                : "설정 안 됨"}
            </td>
          </tr>
          <tr>
            <th>보존 정리</th>
            <td>
              {okText(metrics.retention.last_ok)} · {when(metrics.retention.last_at)}
            </td>
          </tr>
          <tr>
            <th>백업</th>
            <td>
              {backup.configured
                ? `${okText(backup.last_backup_ok)} · ${when(backup.last_backup_at)} · ${formatBytes(backup.last_backup_size_bytes)}`
                : "꺼짐(백업 키 없음)"}
            </td>
          </tr>
          <tr>
            <th>복원 시험</th>
            <td>
              {okText(backup.last_restore_test_ok)} · {when(backup.last_restore_test_at)}
            </td>
          </tr>
        </tbody>
      </table>
      <Notice error={error} info={info} />
      <div className="actions">
        <button type="button" disabled={busy || !backup.configured} onClick={() => void run("run")}>
          지금 백업
        </button>
        <button type="button" disabled={busy || !backup.configured} onClick={() => void run("restore-test")}>
          복원 시험
        </button>
        <button type="button" disabled={busy} onClick={reload}>
          새로고침
        </button>
      </div>
    </Card>
  );
}

function SettingsForms({ settings, csrf, reload }: { settings: AdminSettings; csrf: string; reload: () => void }) {
  const [serviceName, setServiceName] = useState(settings.site.service_name);
  const [publicUrl, setPublicUrl] = useState(settings.site.public_url ?? "");
  const [policy, setPolicy] = useState(settings.signup_policy);
  const [smtp, setSmtp] = useState({
    host: settings.smtp?.host ?? "",
    port: String(settings.smtp?.port ?? 587),
    username: settings.smtp?.username ?? "",
    sender: settings.smtp?.sender ?? "",
    password: "",
  });
  const [blocked, setBlocked] = useState(settings.blocked_email_domains.join("\n"));
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);

  const save = (action: () => Promise<{ ok: boolean; error: string | null }>, done: string) => async (event?: FormEvent) => {
    event?.preventDefault();
    setError(null);
    setInfo(null);
    const result = await action();
    if (result.ok) {
      setInfo(done);
      reload();
    } else {
      setError(messageFor(result.error));
    }
  };
  const update = (key: keyof typeof smtp) => (value: string) => setSmtp({ ...smtp, [key]: value });

  return (
    <Card title="인스턴스 설정">
      <Notice error={error} info={info} />
      <form
        onSubmit={save(
          () => call("PUT", "/api/v1/admin/settings/site", { service_name: serviceName, public_url: publicUrl || null }, csrf),
          "사이트 설정을 저장했습니다.",
        )}
      >
        <h3>사이트</h3>
        <Field label="서비스 이름" name="service_name" value={serviceName} onChange={setServiceName} />
        <Field label="접속 주소" name="public_url" type="url" value={publicUrl} onChange={setPublicUrl} required={false} />
        <button type="submit">저장</button>
      </form>
      <form onSubmit={save(() => call("PUT", "/api/v1/admin/settings/signup-policy", { policy }, csrf), "가입 정책을 저장했습니다.")}>
        <h3>가입 정책</h3>
        <fieldset className="choices">
          <legend>누가 가입할 수 있나요?</legend>
          {Object.entries(POLICY_LABELS).map(([value, label]) => (
            <label key={value}>
              <input type="radio" name="admin_policy" value={value} checked={policy === value} onChange={() => setPolicy(value)} /> {label}
            </label>
          ))}
        </fieldset>
        <button type="submit">저장</button>
      </form>
      <form
        onSubmit={save(
          () =>
            call(
              "PUT",
              "/api/v1/admin/settings/blocked-domains",
              { domains: blocked.split(/[\s,]+/).filter(Boolean) },
              csrf,
            ),
          "차단 도메인을 저장했습니다.",
        )}
      >
        <h3>가입 차단 이메일 도메인</h3>
        <p className="hint">한 줄에 하나(일회용 메일 등). 하위 도메인도 함께 막습니다.</p>
        <textarea
          className="domains"
          aria-label="차단 도메인"
          rows={4}
          value={blocked}
          onChange={(event) => setBlocked(event.target.value)}
        />
        <button type="submit">저장</button>
      </form>
      <form
        onSubmit={save(
          () =>
            call(
              "PUT",
              "/api/v1/admin/settings/smtp",
              {
                host: smtp.host,
                port: Number(smtp.port),
                username: smtp.username || null,
                sender: smtp.sender,
                starttls: true,
                password: smtp.password || null,
              },
              csrf,
            ),
          "SMTP 설정을 저장했습니다.",
        )}
      >
        <h3>메일(SMTP)</h3>
        <p className="hint">
          현재 출처: {settings.smtp_source === "settings" ? "이 화면" : settings.smtp_source === "environment" ? "환경 변수" : "없음"}
          {settings.smtp?.password_set ? " · 암호 저장됨(다시 표시하지 않음)" : ""}
        </p>
        <Field label="호스트" name="smtp_host" value={smtp.host} onChange={update("host")} />
        <Field label="포트" name="smtp_port" type="number" value={smtp.port} onChange={update("port")} />
        <Field label="사용자 이름" name="smtp_username" value={smtp.username} onChange={update("username")} required={false} />
        <Field
          label="새 암호(비우면 기존 유지)"
          name="smtp_password"
          type="password"
          value={smtp.password}
          onChange={update("password")}
          autoComplete="new-password"
          required={false}
        />
        <Field label="발신 주소" name="smtp_sender" type="email" value={smtp.sender} onChange={update("sender")} />
        <div className="actions">
          <button type="submit">저장</button>
          <button
            type="button"
            onClick={() =>
              void (async () => {
                setError(null);
                setInfo(null);
                const result = await call<{ ok: boolean; error: string | null }>("POST", "/api/v1/admin/settings/smtp/test", undefined, csrf);
                if (result.data?.ok) {
                  setInfo("내 주소로 시험 메일을 보냈습니다.");
                } else {
                  setError(result.data ? `시험 실패: ${smtpMessage(result.data.error)}` : messageFor(result.error));
                }
              })()
            }
          >
            시험 메일
          </button>
        </div>
      </form>
      <h3>OAuth</h3>
      <p className="hint">로그인 버튼 연동은 #24입니다. 지금은 client 정보만 저장합니다(secret은 다시 표시하지 않음).</p>
      {Object.entries(settings.oauth).map(([provider, value]) => (
        <OAuthForm key={provider} provider={provider} value={value} csrf={csrf} onSaved={save} />
      ))}
    </Card>
  );
}

function OAuthForm({
  provider,
  value,
  csrf,
  onSaved,
}: {
  provider: string;
  value: { enabled: boolean; client_id: string; client_secret_set: boolean };
  csrf: string;
  onSaved: (action: () => Promise<{ ok: boolean; error: string | null }>, done: string) => (event?: FormEvent) => Promise<void>;
}) {
  const [clientId, setClientId] = useState(value.client_id);
  const [secret, setSecret] = useState("");
  const [enabled, setEnabled] = useState(value.enabled);
  return (
    <form
      className="inline-form"
      onSubmit={onSaved(
        () => call("PUT", `/api/v1/admin/settings/oauth/${provider}`, { enabled, client_id: clientId, client_secret: secret || null }, csrf),
        `${provider} 설정을 저장했습니다.`,
      )}
    >
      <Field label={`${provider} client ID`} name={`${provider}_client_id`} value={clientId} onChange={setClientId} required={false} />
      <Field
        label={value.client_secret_set ? "새 secret(비우면 유지)" : "client secret"}
        name={`${provider}_secret`}
        type="password"
        value={secret}
        onChange={setSecret}
        autoComplete="new-password"
        required={false}
      />
      <label>
        <input type="checkbox" checked={enabled} onChange={() => setEnabled(!enabled)} /> 사용
      </label>
      <button type="submit">저장</button>
    </form>
  );
}

/** 운영 관리(#68): 진단·백업·인스턴스 설정·설정 경계 안내. 관리자만. */
export function AdminPage() {
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [settings, setSettings] = useState<AdminSettings | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const [m, s] = await Promise.all([
      call<Metrics>("GET", "/api/v1/admin/metrics"),
      call<AdminSettings>("GET", "/api/v1/admin/settings"),
    ]);
    setMetrics(m.data);
    setSettings(s.data);
    setError(m.error ?? s.error);
  }, []);

  useEffect(() => {
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => {
      setSession(result.data);
      if (result.data) {
        void load();
      }
    });
  }, [load]);

  if (session === undefined) {
    return <Card title="관리">{null}</Card>;
  }
  if (session === null || !["SUPER_ADMIN", "ADMIN"].includes(session.user.instance_role)) {
    return (
      <Card title="관리">
        <Notice error={messageFor(session === null ? "unauthorized" : "forbidden")} />
        <p className="account-links">
          <a href={session === null ? "/login" : "/"}>{session === null ? "로그인" : "사무실로"}</a>
        </p>
      </Card>
    );
  }
  return (
    <>
      <Notice error={error ? messageFor(error) : null} />
      {metrics && settings && <Diagnostics metrics={metrics} settings={settings} csrf={session.csrf_token} reload={() => void load()} />}
      <AdminUsers me={session.user.id} myRole={session.user.instance_role} csrf={session.csrf_token} />
      {settings && <SettingsForms key={JSON.stringify(settings)} settings={settings} csrf={session.csrf_token} reload={() => void load()} />}
      <NotificationChannels csrf={session.csrf_token} />
      <AuditLog />
      <Card title="설정은 어디서 바꾸나요">
        <table className="members">
          <tbody>
            {SETTING_BOUNDARIES.map((row) => (
              <tr key={row.scope}>
                <th>{row.scope}</th>
                <td>
                  <strong>{row.where}</strong>
                  <br />
                  {row.items}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="hint">
          인프라 항목은 보안상 화면에서 바꾸지 않습니다. 절차: docs/installation/remote-access.md, docs/operations/backup.md, docs/operations/monitoring.md.
        </p>
      </Card>
    </>
  );
}

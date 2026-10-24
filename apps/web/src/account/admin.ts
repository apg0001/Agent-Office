/** 관리 화면(#68) 데이터 해석. 서버 `GET /api/v1/admin/metrics`·`/settings`와 같은 모양. */

export interface Metrics {
  host: {
    cpu_count: number | null;
    load_average: number[] | null;
    memory: { total_bytes: number; available_bytes: number } | null;
    disk: { total_bytes: number; free_bytes: number };
  };
  database: { latency_ms: number; size_bytes: number; migration: string | null };
  collection: {
    users: number;
    devices: number;
    devices_online: number;
    open_sessions: number;
    events_last_hour: number;
    ingest_lag_p95_seconds: number | null;
    max_collector_queue_depth: number | null;
    collector_versions: Record<string, number>;
  };
  readiness: { ready: boolean; checks: Record<string, string> };
  external_heartbeat: { configured: boolean; last_at: string | null; last_ok: boolean | null };
  retention: { last_at: string | null; last_ok: boolean | null };
  backup: {
    configured: boolean;
    last_backup_at?: string | null;
    last_backup_ok?: boolean | null;
    last_backup_size_bytes?: number | null;
    last_restore_test_at?: string | null;
    last_restore_test_ok?: boolean | null;
    last_error?: string | null;
  };
}

export interface AdminSettings {
  site: { service_name: string; public_url: string | null };
  signup_policy: string;
  smtp: {
    host: string;
    port: number;
    username: string | null;
    sender: string;
    starttls: boolean;
    password_set: boolean;
  } | null;
  smtp_source: "settings" | "environment" | "none";
  oauth: Record<string, { enabled: boolean; client_id: string; client_secret_set: boolean }>;
  secret_storage_available: boolean;
}

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined) {
    return "—";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

export interface Warning {
  level: "critical" | "warning";
  text: string;
}

const DAY = 86_400_000;

function olderThan(iso: string | null | undefined, days: number, now: number): boolean {
  return !iso || now - Date.parse(iso) > days * DAY;
}

/** 운영자가 먼저 봐야 할 문제. 인프라 설정이 필요한 것은 무엇을 설정할지 함께 적는다. */
export function warningsFor(metrics: Metrics, settings: AdminSettings, now: number): Warning[] {
  const out: Warning[] = [];
  if (!metrics.readiness.ready) {
    out.push({
      level: "critical",
      text: `서버 준비 안 됨(DB ${metrics.readiness.checks.database}, 마이그레이션 ${metrics.readiness.checks.migrations}). alembic upgrade head를 실행하세요.`,
    });
  }
  const disk = metrics.host.disk;
  if (disk.total_bytes > 0 && disk.free_bytes / disk.total_bytes < 0.1) {
    out.push({ level: "critical", text: `디스크 여유가 10% 미만입니다(${formatBytes(disk.free_bytes)}).` });
  }
  const backup = metrics.backup;
  if (!backup.configured) {
    out.push({ level: "warning", text: "백업 키(AGENT_OFFICE_BACKUP_KEY)가 없어 자동 백업이 꺼져 있습니다." });
  } else {
    if (backup.last_backup_ok === false) {
      out.push({ level: "critical", text: `마지막 백업 실패(${backup.last_error ?? "원인 미상"}).` });
    } else if (olderThan(backup.last_backup_at, 2, now)) {
      out.push({ level: "warning", text: "최근 2일 안에 성공한 백업이 없습니다." });
    }
    if (backup.last_restore_test_ok === false) {
      out.push({ level: "critical", text: "마지막 복원 시험이 실패했습니다." });
    } else if (olderThan(backup.last_restore_test_at, 31, now)) {
      out.push({ level: "warning", text: "최근 31일 안에 복원 시험을 하지 않았습니다." });
    }
  }
  if (!metrics.external_heartbeat.configured) {
    out.push({
      level: "warning",
      text: "외부 Heartbeat(AGENT_OFFICE_HEARTBEAT_URL)가 없어 호스트가 꺼져도 알 수 없습니다.",
    });
  } else if (metrics.external_heartbeat.last_ok === false) {
    out.push({ level: "critical", text: "외부 Heartbeat 전송이 실패하고 있습니다." });
  }
  if (!settings.secret_storage_available) {
    out.push({
      level: "warning",
      text: "비밀 암호화 키(AGENT_OFFICE_SECRET_KEY)가 없어 SMTP 암호·OAuth secret을 저장할 수 없습니다.",
    });
  }
  const needsMail = settings.signup_policy === "PUBLIC_VERIFIED_EMAIL" || settings.signup_policy === "ADMIN_APPROVAL";
  if (needsMail && settings.smtp_source === "none") {
    out.push({ level: "critical", text: "가입 정책이 이메일 확인을 요구하지만 SMTP 설정이 없습니다." });
  }
  if (!settings.site.public_url) {
    out.push({ level: "warning", text: "접속 주소가 없어 메일 링크를 만들 수 없습니다." });
  }
  return out;
}

/** 설정이 어디서 바뀌는지(인프라/인스턴스/개인). UI에서 못 바꾸는 항목을 안내한다. */
export const SETTING_BOUNDARIES: { scope: string; where: string; items: string }[] = [
  {
    scope: "인프라",
    where: "deploy/.env.production → 서버 재시작",
    items:
      "DB 암호, AGENT_OFFICE_SECRET_KEY, AGENT_OFFICE_BACKUP_KEY·백업 경로, AGENT_OFFICE_HEARTBEAT_URL, 바인딩 주소·포트, 도메인 HTTPS, 쿠키 Secure",
  },
  {
    scope: "인스턴스",
    where: "이 관리 화면",
    items: "서비스 이름·접속 주소, 가입 정책, SMTP·시험 메일, OAuth client, 지금 백업·복원 시험",
  },
  {
    scope: "개인",
    where: "/teams, 계정, Collector(PC)",
    items: "팀·초대·프로젝트 공유, 비밀번호·계정 삭제, PC 등록·Hook 설치·일시중지",
  },
];

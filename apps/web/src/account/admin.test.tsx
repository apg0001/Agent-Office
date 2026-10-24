import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { type AdminSettings, type Metrics, formatBytes, warningsFor } from "./admin";
import { routeFor } from "./api";

const NOW = Date.parse("2026-10-24T12:00:00Z");

function metrics(overrides: Partial<Metrics> = {}): Metrics {
  return {
    host: {
      cpu_count: 4,
      load_average: [1, 1, 1],
      memory: { total_bytes: 8e9, available_bytes: 4e9 },
      disk: { total_bytes: 100, free_bytes: 50 },
    },
    database: { latency_ms: 1, size_bytes: 1e7, migration: "0006" },
    collection: {
      users: 1,
      devices: 1,
      devices_online: 1,
      open_sessions: 0,
      events_last_hour: 0,
      ingest_lag_p95_seconds: null,
      max_collector_queue_depth: null,
      collector_versions: {},
    },
    readiness: { ready: true, checks: { database: "ok", migrations: "ok" } },
    external_heartbeat: { configured: true, last_at: "2026-10-24T11:59:00Z", last_ok: true },
    retention: { last_at: null, last_ok: null },
    backup: {
      configured: true,
      last_backup_at: "2026-10-24T03:00:00Z",
      last_backup_ok: true,
      last_restore_test_at: "2026-10-10T03:00:00Z",
      last_restore_test_ok: true,
    },
    ...overrides,
  };
}

const settings: AdminSettings = {
  site: { service_name: "Agent Office", public_url: "https://office.example" },
  signup_policy: "INVITE_ONLY",
  smtp: null,
  smtp_source: "none",
  oauth: {},
  secret_storage_available: true,
  blocked_email_domains: [],
};

describe("admin warnings", () => {
  it("is quiet for a healthy instance", () => {
    expect(warningsFor(metrics(), settings, NOW)).toEqual([]);
  });

  it("flags infra gaps and failures with what to configure", () => {
    const texts = warningsFor(
      metrics({
        readiness: { ready: false, checks: { database: "ok", migrations: "outdated" } },
        host: { cpu_count: 4, load_average: null, memory: null, disk: { total_bytes: 100, free_bytes: 5 } },
        external_heartbeat: { configured: false, last_at: null, last_ok: null },
        backup: { configured: true, last_backup_ok: false, last_error: "pg_dump_failed" },
      }),
      { ...settings, signup_policy: "PUBLIC_VERIFIED_EMAIL", secret_storage_available: false, site: { service_name: "x", public_url: null } },
      NOW,
    ).map((w) => `${w.level}:${w.text}`);

    expect(texts.some((t) => t.startsWith("critical:서버 준비 안 됨") && t.includes("outdated"))).toBe(true);
    expect(texts.some((t) => t.includes("디스크 여유가 10% 미만"))).toBe(true);
    expect(texts.some((t) => t.includes("pg_dump_failed"))).toBe(true);
    expect(texts.some((t) => t.includes("AGENT_OFFICE_HEARTBEAT_URL"))).toBe(true);
    expect(texts.some((t) => t.includes("AGENT_OFFICE_SECRET_KEY"))).toBe(true);
    expect(texts.some((t) => t.startsWith("critical:가입 정책이 이메일 확인"))).toBe(true);
    expect(texts.some((t) => t.includes("접속 주소가 없어"))).toBe(true);
  });

  it("raises high security findings to the top", () => {
    const texts = warningsFor(
      metrics({ security: [{ code: "cookie_not_secure", level: "high", text: "쿠키" }, { code: "setup_incomplete", level: "info", text: "설치" }] }),
      settings,
      NOW,
    ).map((w) => w.text);
    expect(texts).toEqual(["보안: 쿠키"]);
  });

  it("warns about stale backups and restore tests", () => {
    const texts = warningsFor(
      metrics({
        backup: {
          configured: true,
          last_backup_at: "2026-10-20T00:00:00Z",
          last_backup_ok: true,
          last_restore_test_at: null,
        },
      }),
      settings,
      NOW,
    ).map((w) => w.text);
    expect(texts).toContain("최근 2일 안에 성공한 백업이 없습니다.");
    expect(texts).toContain("최근 31일 안에 복원 시험을 하지 않았습니다.");
    expect(warningsFor(metrics({ backup: { configured: false } }), settings, NOW)[0]?.text).toContain(
      "AGENT_OFFICE_BACKUP_KEY",
    );
  });
});

describe("formatBytes", () => {
  it("formats human sizes", () => {
    expect(formatBytes(null)).toBe("—");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(84823)).toBe("83 KB");
    expect(formatBytes(1.5 * 1024 ** 3)).toBe("1.5 GB");
  });
});

describe("admin route", () => {
  it("renders the admin page shell", () => {
    expect(routeFor("/admin")).toBe("admin");
    expect(renderToString(<App pathname="/admin" />)).toContain("관리");
  });
});

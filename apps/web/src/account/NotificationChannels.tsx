import { type FormEvent, useCallback, useEffect, useState } from "react";

import { Card, Field, Notice } from "./AccountPages";
import { call, messageFor } from "./api";

type Provider = "discord" | "telegram" | "teams_workflows" | "email_smtp";
type Severity = "critical" | "high" | "warning" | "info";

interface Channel {
  id: string;
  provider: Provider;
  display_name: string;
  enabled: boolean;
  config: { chat_id?: string; recipients?: string[] };
  secret_set: boolean;
  severities: Severity[];
}

export const PROVIDER_LABELS: Record<Provider, string> = {
  discord: "Discord Webhook",
  telegram: "Telegram Bot",
  teams_workflows: "Microsoft Teams Workflows",
  email_smtp: "이메일(SMTP)",
};

const SEVERITY_LABELS: Record<Severity, string> = {
  critical: "긴급",
  high: "높음",
  warning: "주의",
  info: "정보(복구 포함)",
};

const ERRORS: Record<string, string> = {
  webhook_url_not_allowed: "허용되지 않는 Webhook 주소입니다(https와 공급자 공식 도메인만).",
  secret_required: "Webhook 주소나 봇 토큰이 필요합니다.",
  invalid_bot_token: "Telegram 봇 토큰 형식이 올바르지 않습니다.",
  invalid_chat_id: "Telegram chat ID 형식이 올바르지 않습니다.",
  invalid_recipients: "수신자는 1~10명의 올바른 이메일이어야 합니다.",
  too_many_channels: "채널은 20개까지 등록할 수 있습니다.",
  mail_not_configured: "SMTP 설정이 없어 이메일을 보낼 수 없습니다.",
  rate_limited: "이 채널의 시간당 전송 한도를 넘었습니다.",
  connect_failed: "공급자에 연결할 수 없습니다.",
  secret_unreadable: "저장된 비밀을 읽을 수 없습니다(비밀 키가 바뀌었을 수 있음). 다시 입력하세요.",
};

export function channelError(code: string | null): string {
  if (!code) {
    return "";
  }
  if (/^http_\d{3}$/.test(code)) {
    return `공급자가 거부했습니다(${code.slice(5)}).`;
  }
  return ERRORS[code] ?? messageFor(code);
}

/** 공급자별 비밀·설정 입력값을 API 본문으로. 비밀은 비우면 보내지 않는다(수정 시 유지). */
export function channelBody(
  provider: Provider,
  form: { name: string; secret: string; chatId: string; recipients: string; severities: Severity[] },
): Record<string, unknown> {
  const config =
    provider === "telegram"
      ? { chat_id: form.chatId.trim() }
      : provider === "email_smtp"
        ? { recipients: form.recipients.split(/[\s,]+/).filter(Boolean) }
        : {};
  return {
    provider,
    display_name: form.name,
    config,
    ...(form.secret ? { secret: form.secret } : {}),
    severities: form.severities,
  };
}

export function NotificationChannels({ csrf }: { csrf: string }) {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [provider, setProvider] = useState<Provider>("discord");
  const [form, setForm] = useState({ name: "", secret: "", chatId: "", recipients: "", severities: ["critical", "high"] as Severity[] });
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);

  const load = useCallback(async () => {
    setChannels((await call<Channel[]>("GET", "/api/v1/admin/notifications")).data ?? []);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (action: () => Promise<{ ok: boolean; error: string | null }>, done?: string) => {
    setError(null);
    setInfo(null);
    const result = await action();
    if (result.ok) {
      setInfo(done ?? null);
    } else {
      setError(channelError(result.error));
    }
    await load();
  };

  const create = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      const result = await call("POST", "/api/v1/admin/notifications", channelBody(provider, form), csrf);
      if (result.ok) {
        setForm({ ...form, name: "", secret: "", chatId: "", recipients: "" });
      }
      return result;
    }, "채널을 추가했습니다. 시험 알림으로 확인하세요.");
  };

  const toggleSeverity = (severity: Severity) =>
    setForm({
      ...form,
      severities: form.severities.includes(severity) ? form.severities.filter((s) => s !== severity) : [...form.severities, severity],
    });

  return (
    <Card title="알림 채널">
      <p className="hint">
        백업 실패·복원 시험 실패·보존 정리 실패·외부 Heartbeat 전송 실패와 복구를 보냅니다. 같은 장애는 1시간 안에 반복하지 않습니다. 호스트가 꺼진 것은
        외부 Heartbeat 서비스가 알립니다.
      </p>
      <Notice error={error} info={info} />
      <table className="members">
        <tbody>
          {channels.map((channel) => {
            const base = `/api/v1/admin/notifications/${channel.id}`;
            return (
              <tr key={channel.id}>
                <td>
                  {channel.display_name}
                  <br />
                  <span className="hint">
                    {PROVIDER_LABELS[channel.provider]} · {channel.severities.map((s) => SEVERITY_LABELS[s]).join(", ")}
                  </span>
                </td>
                <td>{channel.enabled ? "켜짐" : "꺼짐"}</td>
                <td className="actions">
                  <button
                    type="button"
                    onClick={() =>
                      void run(async () => {
                        const result = await call<{ ok: boolean; error: string | null }>("POST", `${base}/test`, undefined, csrf);
                        return result.data ?? result;
                      }, "시험 알림을 보냈습니다.")
                    }
                  >
                    시험
                  </button>
                  <button type="button" onClick={() => void run(() => call("PATCH", base, { enabled: !channel.enabled }, csrf))}>
                    {channel.enabled ? "끄기" : "켜기"}
                  </button>
                  <button
                    type="button"
                    className="danger"
                    onClick={() => window.confirm(`'${channel.display_name}' 채널을 삭제할까요?`) && void run(() => call("DELETE", base, undefined, csrf))}
                  >
                    삭제
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <form onSubmit={create}>
        <h3>채널 추가</h3>
        <label className="field">
          <span>공급자</span>
          <select value={provider} onChange={(event) => setProvider(event.target.value as Provider)}>
            {Object.entries(PROVIDER_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <Field label="이름" name="channel_name" value={form.name} onChange={(name) => setForm({ ...form, name })} />
        {provider !== "email_smtp" && (
          <Field
            label={provider === "telegram" ? "봇 토큰(저장 후 다시 표시하지 않음)" : "Webhook 주소(저장 후 다시 표시하지 않음)"}
            name="channel_secret"
            type="password"
            value={form.secret}
            onChange={(secret) => setForm({ ...form, secret })}
            autoComplete="off"
          />
        )}
        {provider === "telegram" && (
          <Field label="chat ID" name="chat_id" value={form.chatId} onChange={(chatId) => setForm({ ...form, chatId })} />
        )}
        {provider === "email_smtp" && (
          <Field
            label="수신자(쉼표로 구분, 최대 10명)"
            name="recipients"
            value={form.recipients}
            onChange={(recipients) => setForm({ ...form, recipients })}
          />
        )}
        <fieldset className="choices">
          <legend>받을 심각도</legend>
          {(Object.keys(SEVERITY_LABELS) as Severity[]).map((severity) => (
            <label key={severity}>
              <input type="checkbox" checked={form.severities.includes(severity)} onChange={() => toggleSeverity(severity)} />{" "}
              {SEVERITY_LABELS[severity]}
            </label>
          ))}
        </fieldset>
        <button type="submit" disabled={form.severities.length === 0}>
          추가
        </button>
      </form>
    </Card>
  );
}

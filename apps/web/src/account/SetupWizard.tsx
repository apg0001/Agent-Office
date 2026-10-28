import { type FormEvent, useEffect, useState } from "react";

import { Card, Field, Notice } from "./AccountPages";
import { type SessionInfo, call, messageFor, smtpMessage } from "./api";
import { POLICY_LABELS, type SetupStatus, type WizardStep, nextStep, startStep } from "./wizard";

const BOOTSTRAP_COMMAND = "docker compose -f deploy/compose.production.yaml exec server agent-office-bootstrap";

/** 설치 마법사: bootstrap 토큰 → 최초 관리자 → 사이트 → 가입 정책 → 메일 → 완료 잠금. */
export function SetupWizard() {
  const [step, setStep] = useState<WizardStep | null>(null);
  const [csrf, setCsrf] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const [token, setToken] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [name, setName] = useState("");
  const [serviceName, setServiceName] = useState("Agent Office");
  const [publicUrl, setPublicUrl] = useState(() => window.location.origin);
  const [policy, setPolicy] = useState("CLOSED");
  const [smtp, setSmtp] = useState({ host: "", port: "587", username: "", sender: "", password: "" });
  const [smtpSaved, setSmtpSaved] = useState(false);

  useEffect(() => {
    void (async () => {
      const status = await call<SetupStatus>("GET", "/api/v1/setup/status");
      const session = await call<SessionInfo>("GET", "/api/v1/auth/session");
      if (!status.data) {
        setError(messageFor(status.error));
        return;
      }
      if (session.data) {
        setCsrf(session.data.csrf_token);
      }
      setStep(startStep(status.data, session.data?.user.instance_role ?? null));
    })();
  }, []);

  const act = async (action: () => Promise<{ ok: boolean; error: string | null }>, then?: () => void) => {
    setBusy(true);
    setError(null);
    setInfo(null);
    const result = await action();
    setBusy(false);
    if (!result.ok) {
      setError(messageFor(result.error));
      return;
    }
    then?.();
  };
  const advance = () => setStep((current) => (current ? nextStep(current) : current));
  const submit = (action: () => Promise<{ ok: boolean; error: string | null }>, then?: () => void) =>
    (event: FormEvent) => {
      event.preventDefault();
      void act(action, then);
    };

  if (step === null) {
    return (
      <Card title="설치">
        <Notice error={error} info={error ? null : "설치 상태를 확인하는 중입니다…"} />
      </Card>
    );
  }

  if (step === "completed") {
    return (
      <Card title="설치">
        <Notice info="설치가 이미 완료되었습니다." />
        <p className="account-links">
          <a href="/login">로그인</a>
        </p>
      </Card>
    );
  }

  if (step === "login") {
    return (
      <Card title="설치 계속하기">
        <Notice info="최초 관리자가 이미 만들어졌습니다. 그 계정으로 로그인한 뒤 이 화면으로 돌아오세요." />
        <p className="account-links">
          <a href="/login">로그인</a>
        </p>
      </Card>
    );
  }

  if (step === "admin") {
    return (
      <Card title="1. 최초 관리자">
        <p>서버 콘솔에서 아래 명령을 실행해 일회용 토큰(1시간 유효)을 받으세요.</p>
        <pre className="command">{BOOTSTRAP_COMMAND}</pre>
        <form
          onSubmit={submit(
            async () => {
              if (password !== confirmPassword) {
                return { ok: false, error: "password_mismatch" };
              }
              const result = await call<SessionInfo>("POST", "/api/v1/setup/admin", {
                bootstrap_token: token.trim(),
                email,
                password,
                display_name: name,
              });
              if (result.data) {
                setCsrf(result.data.csrf_token);
              }
              return result;
            },
            () => setStep("site"),
          )}
        >
          <Field label="bootstrap 토큰" name="token" value={token} onChange={setToken} autoComplete="off" />
          <Field label="이름" name="name" value={name} onChange={setName} autoComplete="name" />
          <Field label="이메일" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
          <Field
            label="비밀번호(10자 이상)"
            name="password"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete="new-password"
          />
          <Field
            label="비밀번호 확인"
            name="password-confirm"
            type="password"
            value={confirmPassword}
            onChange={setConfirmPassword}
            autoComplete="new-password"
          />
          <Notice error={error} />
          <button type="submit" disabled={busy}>
            관리자 만들기
          </button>
        </form>
      </Card>
    );
  }

  if (step === "site") {
    return (
      <Card title="2. 사이트">
        <form
          onSubmit={submit(
            () =>
              call("PUT", "/api/v1/admin/settings/site", { service_name: serviceName, public_url: publicUrl || null }, csrf),
            advance,
          )}
        >
          <Field label="서비스 이름" name="service_name" value={serviceName} onChange={setServiceName} />
          <Field
            label="접속 주소(메일 링크에 사용)"
            name="public_url"
            type="url"
            value={publicUrl}
            onChange={setPublicUrl}
            required={false}
          />
          <p className="hint">HTTPS·Tunnel은 운영자가 설정합니다. 비워 두면 메일 기능이 꺼집니다.</p>
          <Notice error={error} />
          <button type="submit" disabled={busy}>
            다음
          </button>
        </form>
      </Card>
    );
  }

  if (step === "policy") {
    return (
      <Card title="3. 가입 정책">
        <form onSubmit={submit(() => call("PUT", "/api/v1/admin/settings/signup-policy", { policy }, csrf), advance)}>
          <fieldset className="choices">
            <legend>누가 가입할 수 있나요?</legend>
            {Object.entries(POLICY_LABELS).map(([value, label]) => (
              <label key={value}>
                <input
                  type="radio"
                  name="policy"
                  value={value}
                  checked={policy === value}
                  onChange={() => setPolicy(value)}
                />{" "}
                {label}
              </label>
            ))}
          </fieldset>
          <p className="hint">공개 가입·관리자 승인은 다음 단계의 메일 설정이 있어야 동작합니다.</p>
          <Notice error={error} />
          <button type="submit" disabled={busy}>
            다음
          </button>
        </form>
      </Card>
    );
  }

  if (step === "mail") {
    const update = (key: keyof typeof smtp) => (value: string) => setSmtp({ ...smtp, [key]: value });
    return (
      <Card title="4. 메일(SMTP, 선택)">
        <form
          onSubmit={submit(
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
            () => {
              setSmtpSaved(true);
              setSmtp({ ...smtp, password: "" });
              setInfo("저장했습니다. 시험 메일로 확인하세요.");
            },
          )}
        >
          <Field label="SMTP 호스트" name="host" value={smtp.host} onChange={update("host")} />
          <Field label="포트" name="port" type="number" value={smtp.port} onChange={update("port")} />
          <Field label="사용자 이름" name="username" value={smtp.username} onChange={update("username")} required={false} />
          <Field
            label="암호(저장 후 다시 표시하지 않음)"
            name="smtp_password"
            type="password"
            value={smtp.password}
            onChange={update("password")}
            autoComplete="new-password"
            required={false}
          />
          <Field label="발신 주소" name="sender" type="email" value={smtp.sender} onChange={update("sender")} />
          <Notice error={error} info={info} />
          <div className="actions">
            <button type="submit" disabled={busy}>
              저장
            </button>
            <button
              type="button"
              disabled={busy || !smtpSaved}
              onClick={() =>
                void (async () => {
                  setBusy(true);
                  setError(null);
                  setInfo(null);
                  const result = await call<{ ok: boolean; error: string | null }>(
                    "POST",
                    "/api/v1/admin/settings/smtp/test",
                    undefined,
                    csrf,
                  );
                  setBusy(false);
                  if (result.data?.ok) {
                    setInfo("시험 메일을 보냈습니다. 받은편지함을 확인하세요.");
                  } else {
                    setError(
                      result.data ? `시험 실패: ${smtpMessage(result.data.error)}` : messageFor(result.error),
                    );
                  }
                })()
              }
            >
              시험 메일 보내기
            </button>
            <button type="button" disabled={busy} onClick={advance}>
              {smtpSaved ? "다음" : "건너뛰기"}
            </button>
          </div>
        </form>
      </Card>
    );
  }

  return (
    <Card title="5. 완료">
      <p>설정을 마치면 bootstrap 토큰을 다시 발급할 수 없고 설치 화면이 잠깁니다. 설정은 나중에 관리 화면에서 바꿀 수 있습니다.</p>
      <Notice error={error} />
      <button
        type="button"
        disabled={busy}
        onClick={() =>
          void act(
            () => call("POST", "/api/v1/setup/complete", undefined, csrf),
            () => window.location.assign("/"),
          )
        }
      >
        설치 완료
      </button>
    </Card>
  );
}


import { type FormEvent, type ReactNode, useEffect, useState } from "react";

import {
  OAUTH_LABELS,
  type SessionInfo,
  call,
  messageFor,
  oauthResult,
  oauthStartUrl,
  pendingInvite,
  tokenFromHash,
} from "./api";

interface FieldProps {
  label: string;
  name: string;
  type?: string;
  value: string;
  onChange: (value: string) => void;
  autoComplete?: string;
  required?: boolean;
}

export function Field({ label, name, type = "text", value, onChange, autoComplete, required = true }: FieldProps) {
  return (
    <label className="field">
      <span>{label}</span>
      <input
        name={name}
        type={type}
        value={value}
        required={required}
        autoComplete={autoComplete}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

export function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="account-card" aria-label={title}>
      <h2>{title}</h2>
      {children}
    </section>
  );
}

export function Notice({ error, info }: { error?: string | null; info?: string | null }) {
  if (error) {
    return (
      <p className="notice error" role="alert">
        {error}
      </p>
    );
  }
  return info ? (
    <p className="notice" role="status">
      {info}
    </p>
  ) : null;
}

/** 폼 제출 공통: 중복 제출을 막고 오류 코드를 문구로 바꾼다. */
function useSubmit() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async (action: () => Promise<string | null>) => {
    setBusy(true);
    setError(null);
    const code = await action();
    setError(code ? messageFor(code) : null);
    setBusy(false);
  };
  return { busy, error, run };
}

/** 서버 OAuth 콜백이 돌려준 `#oauth=<코드>`를 한 번 읽고 주소창에서 지운다(#24). */
export function useOAuthResult() {
  const [result, setResult] = useState<{ error: string | null; info: string | null } | null>(null);
  useEffect(() => {
    const found = oauthResult(window.location.hash);
    if (found) {
      window.history.replaceState(null, "", window.location.pathname);
      setResult(found);
    }
  }, []);
  return result;
}

/** 관리자가 설정을 끝낸 제공자만 버튼으로 보인다. 설정이 없으면 아무것도 그리지 않는다. */
export function OAuthButtons({ verb }: { verb: string }) {
  const [providers, setProviders] = useState<string[]>([]);
  useEffect(() => {
    void call<{ providers: string[] }>("GET", "/api/v1/auth/oauth/providers").then((result) =>
      setProviders(result.data?.providers ?? []),
    );
  }, []);
  if (providers.length === 0) {
    return null;
  }
  return (
    <div className="oauth-buttons">
      {providers.map((provider) => (
        <a key={provider} className="button" href={oauthStartUrl(provider, "login", pendingInvite())}>
          {OAUTH_LABELS[provider] ?? provider}(으)로 {verb}
        </a>
      ))}
    </div>
  );
}

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { busy, error, run } = useSubmit();
  const oauth = useOAuthResult();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      const result = await call("POST", "/api/v1/auth/login", { email, password });
      if (result.ok) {
        window.location.assign("/");
      }
      return result.error;
    });
  };

  return (
    <Card title="로그인">
      <form onSubmit={submit}>
        <Field label="이메일" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
        <Field
          label="비밀번호"
          name="password"
          type="password"
          value={password}
          onChange={setPassword}
          autoComplete="current-password"
        />
        <Notice error={error ?? oauth?.error} info={oauth?.info} />
        <button type="submit" disabled={busy}>
          로그인
        </button>
      </form>
      <OAuthButtons verb="로그인" />
      <p className="account-links">
        <a href="/signup">가입</a> · <a href="/reset-password">비밀번호 찾기</a>
      </p>
    </Card>
  );
}

export function SignupPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [name, setName] = useState("");
  // 사람에게는 보이지 않는 입력란. 자동 가입 봇이 채우면 서버가 조용히 무시한다(#28).
  const [website, setWebsite] = useState("");
  const [done, setDone] = useState(false);
  const { busy, error, run } = useSubmit();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      // 비밀번호 확인은 화면에서만 비교한다(서버에는 비밀번호 하나만 보낸다, #215).
      if (password !== confirmPassword) {
        return "password_mismatch";
      }
      const invite = pendingInvite();
      const result = await call("POST", "/api/v1/auth/signup", {
        email,
        password,
        display_name: name,
        ...(invite ? { invite_token: invite } : {}),
        ...(website ? { website } : {}),
      });
      setDone(result.ok);
      return result.error;
    });
  };

  if (done) {
    return (
      <Card title="가입">
        <Notice info="확인 메일을 보냈습니다. 24시간 안에 메일의 링크를 열어 주세요." />
      </Card>
    );
  }
  return (
    <Card title="가입">
      <form onSubmit={submit}>
        <Field label="이름" name="name" value={name} onChange={setName} autoComplete="name" />
        <label className="trap" aria-hidden="true">
          웹사이트(비워 두세요)
          <input
            name="website"
            tabIndex={-1}
            autoComplete="off"
            value={website}
            onChange={(event) => setWebsite(event.target.value)}
          />
        </label>
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
          가입
        </button>
      </form>
      <OAuthButtons verb="가입" />
      <p className="account-links">
        <a href="/login">로그인</a>
      </p>
    </Card>
  );
}

export function VerifyPage() {
  const [state, setState] = useState<"working" | "done" | "failed">("working");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const token = tokenFromHash(window.location.hash);
    // 토큰이 주소창·기록에 남지 않게 지운다.
    window.history.replaceState(null, "", window.location.pathname);
    if (!token) {
      setState("failed");
      setError(messageFor("invalid_token"));
      return;
    }
    void call("POST", "/api/v1/auth/verify-email", { token }).then((result) => {
      setState(result.ok ? "done" : "failed");
      setError(messageFor(result.error));
    });
  }, []);

  return (
    <Card title="이메일 확인">
      {state === "working" && <Notice info="확인 중입니다…" />}
      {state === "done" && <Notice info="이메일을 확인했습니다. 이제 로그인할 수 있습니다." />}
      <Notice error={error} />
      <p className="account-links">
        <a href="/login">로그인</a>
      </p>
    </Card>
  );
}

export function ResetPage() {
  const [token] = useState(() => tokenFromHash(window.location.hash));
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [info, setInfo] = useState<string | null>(null);
  const { busy, error, run } = useSubmit();

  useEffect(() => {
    if (window.location.hash) {
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, []);

  const request = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      const result = await call("POST", "/api/v1/auth/password-reset/request", { email });
      if (result.ok) {
        setInfo("가입된 주소라면 재설정 메일을 보냈습니다. 1시간 안에 링크를 열어 주세요.");
      }
      return result.error;
    });
  };
  const confirm = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      if (password !== confirmPassword) {
        return "password_mismatch";
      }
      const result = await call("POST", "/api/v1/auth/password-reset/confirm", { token, password });
      if (result.ok) {
        setInfo("비밀번호를 바꿨습니다. 다른 기기의 로그인은 모두 끊겼습니다.");
      }
      return result.error;
    });
  };

  return (
    <Card title="비밀번호 재설정">
      {token ? (
        <form onSubmit={confirm}>
          <Field
            label="새 비밀번호(10자 이상)"
            name="password"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete="new-password"
          />
          <Field
            label="새 비밀번호 확인"
            name="password-confirm"
            type="password"
            value={confirmPassword}
            onChange={setConfirmPassword}
            autoComplete="new-password"
          />
          <Notice error={error} info={info} />
          <button type="submit" disabled={busy || info !== null}>
            바꾸기
          </button>
        </form>
      ) : (
        <form onSubmit={request}>
          <Field label="이메일" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
          <Notice error={error} info={info} />
          <button type="submit" disabled={busy}>
            재설정 메일 받기
          </button>
        </form>
      )}
      <p className="account-links">
        <a href="/login">로그인</a>
      </p>
    </Card>
  );
}

interface IdentitiesInfo {
  password_set: boolean;
  identities: { provider: string; linked_at: string }[];
}

/** 계정 화면: Google/GitHub 연결·해제(#24). 이메일이 같아도 자동 연결하지 않으므로 여기서 연결한다. */
export function AccountPage() {
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);
  const [info, setInfo] = useState<IdentitiesInfo | null>(null);
  const [providers, setProviders] = useState<string[]>([]);
  const oauth = useOAuthResult();
  const { busy, error, run } = useSubmit();

  const load = async () => {
    const result = await call<IdentitiesInfo>("GET", "/api/v1/auth/identities");
    setInfo(result.data);
  };

  useEffect(() => {
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => {
      setSession(result.data);
      if (result.data) {
        void load();
      }
    });
    void call<{ providers: string[] }>("GET", "/api/v1/auth/oauth/providers").then((result) =>
      setProviders(result.data?.providers ?? []),
    );
  }, []);

  if (session === undefined) {
    return <Card title="계정">{null}</Card>;
  }
  if (session === null) {
    return (
      <Card title="계정">
        <Notice error={oauth?.error ?? messageFor("unauthorized")} />
        <p className="account-links">
          <a href="/login">로그인</a>
        </p>
      </Card>
    );
  }
  const linked = new Set(info?.identities.map((identity) => identity.provider) ?? []);
  const unlink = (provider: string) =>
    void run(async () => {
      const result = await call("DELETE", `/api/v1/auth/identities/${provider}`, undefined, session.csrf_token);
      await load();
      return result.error;
    });
  const all = Array.from(new Set([...providers, ...linked]));

  return (
    <Card title="계정">
      <p>{session.user.display_name}</p>
      <h3>로그인 수단</h3>
      <p>비밀번호: {info?.password_set ? "설정됨" : "없음"}</p>
      {all.length === 0 ? (
        <p>연결할 수 있는 외부 로그인이 없습니다.</p>
      ) : (
        <ul className="identity-list">
          {all.map((provider) => (
            <li key={provider}>
              <span>{OAUTH_LABELS[provider] ?? provider}</span>{" "}
              {linked.has(provider) ? (
                <button type="button" disabled={busy} onClick={() => unlink(provider)}>
                  연결 해제
                </button>
              ) : providers.includes(provider) ? (
                <a className="button" href={oauthStartUrl(provider, "link")}>
                  연결
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      <Notice error={error ?? oauth?.error} info={oauth?.info} />
      <p className="account-links">
        <a href="/">사무실</a>
      </p>
    </Card>
  );
}

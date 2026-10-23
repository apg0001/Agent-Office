import { type FormEvent, type ReactNode, useEffect, useState } from "react";

import { call, messageFor, tokenFromHash } from "./api";

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

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { busy, error, run } = useSubmit();

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
        <Notice error={error} />
        <button type="submit" disabled={busy}>
          로그인
        </button>
      </form>
      <p className="account-links">
        <a href="/signup">가입</a> · <a href="/reset-password">비밀번호 찾기</a>
      </p>
    </Card>
  );
}

export function SignupPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [done, setDone] = useState(false);
  const { busy, error, run } = useSubmit();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      const result = await call("POST", "/api/v1/auth/signup", { email, password, display_name: name });
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
        <Field label="이메일" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
        <Field
          label="비밀번호(10자 이상)"
          name="password"
          type="password"
          value={password}
          onChange={setPassword}
          autoComplete="new-password"
        />
        <Notice error={error} />
        <button type="submit" disabled={busy}>
          가입
        </button>
      </form>
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

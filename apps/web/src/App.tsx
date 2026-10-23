import { useEffect, useState } from "react";

import { LoginPage, ResetPage, SignupPage, VerifyPage } from "./account/AccountPages";
import { InvitePage } from "./account/InvitePage";
import { TeamsPage } from "./account/TeamsPage";
import { SetupWizard } from "./account/SetupWizard";
import { type SessionInfo, call, routeFor } from "./account/api";
import { OfficeView } from "./office/OfficeView";
import { SideBySide } from "./office/SideBySide";
import { scopeFromSearch } from "./office/liveSource";

const principles = [
  ["관측 우선", "실제 이벤트와 추정·연출을 분리합니다."],
  ["로컬 원문", "프롬프트와 코드, 로그 원문은 개발 PC에만 둡니다."],
  ["비침습", "Collector 장애가 에이전트 작업을 막지 않습니다."],
];

function SessionBar() {
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);

  useEffect(() => {
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => setSession(result.data));
  }, []);

  if (session === undefined) {
    return null;
  }
  if (session === null) {
    return (
      <nav className="session-bar" aria-label="계정">
        <a href="/login">로그인</a>
      </nav>
    );
  }
  const logout = async () => {
    await call("POST", "/api/v1/auth/logout", undefined, session.csrf_token);
    window.location.assign("/login");
  };
  return (
    <nav className="session-bar" aria-label="계정">
      <a href="/teams">팀</a>
      <span>{session.user.display_name}</span>
      <button type="button" onClick={() => void logout()}>
        로그아웃
      </button>
    </nav>
  );
}

export function App({ pathname }: { pathname?: string }) {
  const route = routeFor(pathname ?? (typeof window === "undefined" ? "/" : window.location.pathname));

  if (route !== "office") {
    const pages = {
      login: LoginPage,
      signup: SignupPage,
      verify: VerifyPage,
      reset: ResetPage,
      setup: SetupWizard,
      invite: InvitePage,
      teams: TeamsPage,
    };
    const Page = pages[route];
    return (
      <main className={`shell account-shell${route === "teams" ? " wide" : ""}`}>
        <p className="eyebrow">
          <a href="/">AGENT OFFICE</a>
        </p>
        <Page />
      </main>
    );
  }

  return (
    <main className="shell">
      <SessionBar />
      <section className="hero">
        <p className="eyebrow">SELF-HOSTED · MULTI-USER · PRIVACY-FIRST</p>
        <h1>Agent Office</h1>
        <p className="lede">
          코딩 에이전트의 안전한 활동 메타데이터를 하나의 아이소메트릭 사무실로
          연결합니다.
        </p>
        <span className="status">기반 구축 중 · 실제 수집 비활성</span>
      </section>
      {typeof window !== "undefined" && scopeFromSearch(window.location.search).kind === "side" ? <SideBySide /> : <OfficeView />}
      <section className="principles" aria-label="프로젝트 원칙">
        {principles.map(([title, description]) => (
          <article key={title}>
            <h2>{title}</h2>
            <p>{description}</p>
          </article>
        ))}
      </section>
    </main>
  );
}

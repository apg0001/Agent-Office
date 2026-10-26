import { useEffect, useState } from "react";

import { AccountPage, LoginPage, ResetPage, SignupPage, VerifyPage } from "./account/AccountPages";
import { InvitePage } from "./account/InvitePage";
import { AdminPage } from "./account/AdminPage";
import { TeamsPage } from "./account/TeamsPage";
import { DevicesPage } from "./account/DevicesPage";
import { SetupWizard } from "./account/SetupWizard";
import { type SessionInfo, call, routeFor } from "./account/api";
import { OfficeView } from "./office/OfficeView";
import { SideBySide } from "./office/SideBySide";
import { scopeFromSearch } from "./office/liveSource";

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
      <a href="/devices">PC</a>
      <a href="/account">계정</a>
      {["SUPER_ADMIN", "ADMIN"].includes(session.user.instance_role) && <a href="/admin">관리</a>}
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
      devices: DevicesPage,
      admin: AdminPage,
      account: AccountPage,
    };
    const Page = pages[route];
    return (
      <main className={`shell account-shell${route === "teams" || route === "devices" || route === "admin" ? " wide" : ""}`}>
        <p className="eyebrow">
          <a href="/">AGENT OFFICE</a>
        </p>
        <Page />
      </main>
    );
  }

  return (
    <main className="shell office-shell">
      <header className="topbar">
        <a className="brand" href="/">
          Agent Office
        </a>
        <SessionBar />
      </header>
      {typeof window !== "undefined" && scopeFromSearch(window.location.search).kind === "side" ? <SideBySide /> : <OfficeView />}
    </main>
  );
}

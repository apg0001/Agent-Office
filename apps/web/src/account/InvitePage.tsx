import { useEffect, useState } from "react";

import { Card, Notice } from "./AccountPages";
import { type SessionInfo, call, messageFor, pendingInvite, rememberInvite, tokenFromHash } from "./api";
import { ShareConsent } from "./ShareConsent";
import { shareModeFromConsent } from "./teams";

/** 팀 초대 수락(#27). 초대받은 이메일로 로그인해야 수락된다. */
export function InvitePage() {
  const [token] = useState(() => tokenFromHash(window.location.hash) ?? pendingInvite());
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);
  const [joined, setJoined] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // 새 팀원의 기본은 "모두 공유"이고, 화면에서 미리 체크한 것을 그대로 보낸다(#197).
  const [shareAll, setShareAll] = useState(true);

  useEffect(() => {
    if (window.location.hash) {
      window.history.replaceState(null, "", window.location.pathname);
    }
    rememberInvite(token);
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => setSession(result.data));
  }, [token]);

  if (!token) {
    return (
      <Card title="팀 초대">
        <Notice error={messageFor("invalid_token")} />
      </Card>
    );
  }
  if (session === undefined) {
    return (
      <Card title="팀 초대">
        <Notice info="확인 중입니다…" />
      </Card>
    );
  }
  if (session === null) {
    return (
      <Card title="팀 초대">
        <p>초대받은 이메일 주소로 로그인하거나 가입한 뒤 이 링크를 다시 열면 팀에 합류합니다.</p>
        <p className="account-links">
          <a href="/login">로그인</a> · <a href="/signup">가입</a>
        </p>
      </Card>
    );
  }
  const accept = async () => {
    const result = await call<{ name: string }>("POST", "/api/v1/invitations/accept", { token, share_mode: shareModeFromConsent(shareAll) }, session.csrf_token);
    if (result.data) {
      rememberInvite(null);
      setJoined(result.data.name);
    } else {
      setError(
        result.error === "invalid_token"
          ? "초대가 만료·취소되었거나, 초대받은 이메일과 다른 계정으로 로그인했습니다."
          : messageFor(result.error),
      );
    }
  };
  return (
    <Card title="팀 초대">
      {joined ? (
        <>
          <Notice info={`'${joined}' 팀에 합류했습니다.`} />
          <p className="account-links">
            <a href="/">사무실로</a>
          </p>
        </>
      ) : (
        <>
          <p>{session.user.display_name} 계정으로 초대를 수락합니다.</p>
          <ShareConsent checked={shareAll} onChange={setShareAll} />
          <Notice error={error} />
          <button type="button" onClick={() => void accept()}>
            수락
          </button>
        </>
      )}
    </Card>
  );
}

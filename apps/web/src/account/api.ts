/**
 * 계정·설치 API 호출(#23, #66, #67). 서버 오류는 코드(`detail`)만 받아 한국어 문구로 바꾼다.
 */

export interface ApiResult<T> {
  ok: boolean;
  status: number;
  data: T | null;
  error: string | null;
}

export interface SessionInfo {
  user: { id: string; display_name: string; instance_role: string };
  csrf_token: string;
  expires_at: string;
}

export async function call<T>(
  method: string,
  path: string,
  body?: unknown,
  csrf?: string,
  fetcher: typeof fetch = fetch,
): Promise<ApiResult<T>> {
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (csrf) {
    headers["X-CSRF-Token"] = csrf;
  }
  try {
    const response = await fetcher(path, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    let parsed: unknown = null;
    try {
      parsed = text ? JSON.parse(text) : null;
    } catch {
      parsed = null;
    }
    if (response.ok) {
      return { ok: true, status: response.status, data: parsed as T, error: null };
    }
    const detail = (parsed as { detail?: unknown } | null)?.detail;
    return {
      ok: false,
      status: response.status,
      data: null,
      error: typeof detail === "string" ? detail : `http_${response.status}`,
    };
  } catch {
    return { ok: false, status: 0, data: null, error: "network_error" };
  }
}

const MESSAGES: Record<string, string> = {
  invalid_credentials: "이메일 또는 비밀번호가 맞지 않습니다.",
  email_not_verified: "이메일 확인이 아직 끝나지 않았습니다. 받은 메일의 링크를 열어 주세요.",
  approval_pending: "관리자 승인을 기다리고 있습니다.",
  signup_closed: "지금은 가입을 받지 않습니다.",
  invite_required: "초대받은 사용자만 가입할 수 있습니다. 받은 초대 링크를 먼저 열고, 초대받은 이메일로 가입하세요.",
  invalid_email: "이메일 형식이 올바르지 않습니다.",
  password_too_short: "비밀번호는 10자 이상이어야 합니다.",
  password_too_long: "비밀번호는 128자 이하여야 합니다.",
  mail_not_configured: "서버 메일 설정이 없어 지금은 처리할 수 없습니다. 관리자에게 문의하세요.",
  invalid_token: "링크가 만료되었거나 이미 사용되었습니다.",
  rate_limited: "요청이 너무 많습니다. 잠시 후 다시 시도하세요.",
  cross_site_rejected: "다른 사이트에서 보낸 요청은 처리하지 않습니다.",
  bootstrap_rejected: "bootstrap 토큰이 맞지 않거나 만료되었습니다. 서버 콘솔에서 새 토큰을 받으세요.",
  email_already_registered: "이미 가입된 이메일입니다. 다른 주소를 쓰세요.",
  invalid_public_url: "주소는 https://호스트[:포트] 형식이어야 합니다(경로 없이).",
  invalid_sender: "발신 주소 형식이 올바르지 않습니다.",
  invalid_host: "SMTP 호스트 형식이 올바르지 않습니다.",
  secret_key_missing: "서버에 비밀 암호화 키(AGENT_OFFICE_SECRET_KEY)가 없어 암호를 저장할 수 없습니다.",
  forbidden: "권한이 없습니다.",
  csrf_required: "세션이 바뀌었습니다. 새로고침 후 다시 시도하세요.",
  unauthorized: "로그인이 필요합니다.",
  network_error: "서버에 연결할 수 없습니다.",
  already_member: "이미 팀 구성원입니다.",
  owner_must_transfer: "소유자는 소유권을 넘긴 뒤에 나갈 수 있습니다.",
  use_transfer: "소유자 지정은 소유권 넘기기로만 할 수 있습니다.",
  too_many_teams: "속할 수 있는 팀 수(50개)를 넘었습니다.",
  not_found: "찾을 수 없거나 권한이 없습니다.",
};

export function messageFor(code: string | null): string {
  if (!code) {
    return "";
  }
  return MESSAGES[code] ?? `요청을 처리하지 못했습니다(${code}).`;
}

const SMTP_ERRORS: Record<string, string> = {
  auth_failed: "SMTP 로그인 실패(사용자 이름·암호 확인)",
  connect_failed: "SMTP 서버에 연결할 수 없음(호스트·포트 확인)",
  timeout: "SMTP 서버 응답 시간 초과",
  tls_failed: "TLS 연결 실패",
  starttls_unsupported: "서버가 STARTTLS를 지원하지 않음",
  recipient_refused: "받는 주소가 거부됨",
  smtp_error: "SMTP 오류",
  not_configured: "SMTP 설정 없음",
};

export function smtpMessage(code: string | null): string {
  return code ? (SMTP_ERRORS[code] ?? code) : "";
}

/** 메일 링크의 `#token=…`. 토큰은 서버 로그에 남지 않도록 URL 조각으로 온다. */
export function tokenFromHash(hash: string): string | null {
  const match = /(?:^#|&)token=([A-Za-z0-9_-]{1,256})(?:&|$)/.exec(hash);
  return match ? match[1]! : null;
}

export type Route = "office" | "login" | "signup" | "verify" | "reset" | "setup" | "invite" | "teams" | "admin";

export function routeFor(pathname: string): Route {
  const routes: Record<string, Route> = {
    "/login": "login",
    "/signup": "signup",
    "/verify-email": "verify",
    "/reset-password": "reset",
    "/setup": "setup",
    "/invite": "invite",
    "/teams": "teams",
    "/admin": "admin",
  };
  return routes[pathname.replace(/\/+$/, "") || "/"] ?? "office";
}

export const INVITE_STORAGE_KEY = "ao_invite_token";

/** 초대 토큰을 이 탭에만 잠시 보관한다(가입·로그인 후 돌아와 수락). */
export function pendingInvite(): string | null {
  try {
    return window.sessionStorage.getItem(INVITE_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function rememberInvite(token: string | null): void {
  try {
    if (token) {
      window.sessionStorage.setItem(INVITE_STORAGE_KEY, token);
    } else {
      window.sessionStorage.removeItem(INVITE_STORAGE_KEY);
    }
  } catch {
    // 저장소를 못 쓰면 링크를 다시 열면 된다.
  }
}

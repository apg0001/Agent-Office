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
  password_mismatch: "비밀번호와 비밀번호 확인이 다릅니다.",
  smtp_username_not_ascii: "SMTP 사용자 이름은 영문 계정이어야 합니다(Gmail은 전체 메일 주소, 예: name@gmail.com).",
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
  email_domain_blocked: "이 이메일 도메인으로는 가입할 수 없습니다.",
  invalid_domain: "도메인 형식이 올바르지 않습니다.",
  not_awaiting_approval: "승인 대기 상태가 아닙니다(이메일 확인 전일 수 있음).",
  // OAuth 결과 코드(#24). 서버는 `#oauth=<코드>`만 돌려준다.
  provider_disabled: "이 로그인 수단은 지금 쓸 수 없습니다. 관리자에게 문의하세요.",
  state_invalid: "로그인 요청이 만료되었거나 다른 브라우저에서 시작되었습니다. 처음부터 다시 시도하세요.",
  denied: "제공자 화면에서 로그인이 취소되었습니다.",
  exchange_failed: "제공자와 로그인 확인에 실패했습니다. 다시 시도하세요.",
  identity_failed: "제공자에서 계정 정보를 받지 못했습니다. 다시 시도하세요.",
  provider_unreachable: "제공자에 연결할 수 없습니다. 잠시 후 다시 시도하세요.",
  provider_error: "제공자 응답이 올바르지 않습니다.",
  email_unverified: "제공자 계정의 이메일이 확인되지 않아 가입할 수 없습니다.",
  account_exists_link_required:
    "이 이메일로 가입된 계정이 이미 있습니다. 기존 방법으로 로그인한 뒤 계정 화면에서 연결하세요.",
  login_failed: "로그인할 수 없는 계정입니다.",
  login_required: "먼저 로그인하세요.",
  invalid_invite: "초대 링크가 만료되었거나 이미 사용되었습니다.",
  identity_in_use: "이 제공자 계정은 이미 다른 사용자에 연결되어 있습니다.",
  provider_already_linked: "이 제공자의 다른 계정이 이미 연결되어 있습니다. 먼저 연결을 해제하세요.",
  too_many_open_codes: "아직 쓰지 않은 등록 코드가 너무 많습니다(최대 5개). 기존 코드가 만료(10분)된 뒤 다시 시도하세요.",
  device_name_conflict: "같은 이름의 PC가 이미 있습니다. 다른 이름을 쓰세요.",
  device_revoked: "폐기된 PC는 이름을 바꿀 수 없습니다.",
  last_login_method: "비밀번호가 없는 계정의 마지막 로그인 수단은 해제할 수 없습니다.",
};

const OAUTH_SUCCESS: Record<string, string> = {
  linked: "계정을 연결했습니다.",
  already_linked: "이미 연결된 계정입니다.",
  signed_in: "로그인했습니다.",
  signed_up: "가입하고 로그인했습니다.",
};

export const OAUTH_LABELS: Record<string, string> = { google: "Google", github: "GitHub" };

/** `#oauth=<코드>`의 결과. 성공이면 info, 실패면 error 문구. 모르는 형식은 무시한다. */
export function oauthResult(hash: string): { error: string | null; info: string | null } | null {
  const match = /(?:^#|&)oauth=([a-z_]{1,64})(?:&|$)/.exec(hash);
  if (!match) {
    return null;
  }
  const code = match[1]!;
  return code in OAUTH_SUCCESS ? { error: null, info: OAUTH_SUCCESS[code]! } : { error: messageFor(code), info: null };
}

/** OAuth 시작 주소. 초대 토큰은 초대 전용 정책 가입에만 붙인다. */
export function oauthStartUrl(provider: string, intent: "login" | "link", invite?: string | null): string {
  const params = new URLSearchParams({ intent });
  if (invite) {
    params.set("invite", invite);
  }
  return `/api/v1/auth/oauth/${encodeURIComponent(provider)}/start?${params.toString()}`;
}

export function messageFor(code: string | null): string {
  if (!code) {
    return "";
  }
  return MESSAGES[code] ?? `요청을 처리하지 못했습니다(${code}).`;
}

const SMTP_ERRORS: Record<string, string> = {
  auth_failed: "SMTP 로그인 실패(사용자 이름·암호 확인. Gmail은 전체 메일 주소와 앱 비밀번호)",
  non_ascii_credentials: "사용자 이름·암호에 영문이 아닌 문자가 있음(Gmail은 사용자 이름에 전체 메일 주소)",
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

export type Route =
  | "office"
  | "login"
  | "signup"
  | "verify"
  | "reset"
  | "setup"
  | "invite"
  | "teams"
  | "devices"
  | "admin"
  | "account";

export function routeFor(pathname: string): Route {
  const routes: Record<string, Route> = {
    "/login": "login",
    "/signup": "signup",
    "/verify-email": "verify",
    "/reset-password": "reset",
    "/setup": "setup",
    "/invite": "invite",
    "/teams": "teams",
    "/devices": "devices",
    "/admin": "admin",
    "/account": "account",
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

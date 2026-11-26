/** 팀 화면 권한 규칙(#52). 서버가 최종 판단하고, 화면은 할 수 없는 동작을 숨긴다. */

export type TeamRole = "OWNER" | "MANAGER" | "MEMBER";

export interface TeamPermissions {
  edit: boolean;
  invite: boolean;
  inviteRoles: TeamRole[];
  manageInvitations: boolean;
  changeRoles: boolean;
  transfer: boolean;
  deleteTeam: boolean;
  /** OWNER는 이전하기 전에는 떠날 수 없다(마지막 OWNER 보호). */
  leave: boolean;
}

export function teamPermissions(role: TeamRole): TeamPermissions {
  const owner = role === "OWNER";
  const manager = role === "MANAGER";
  return {
    edit: owner || manager,
    invite: owner || manager,
    inviteRoles: owner ? ["MEMBER", "MANAGER"] : manager ? ["MEMBER"] : [],
    manageInvitations: owner || manager,
    changeRoles: owner,
    transfer: owner,
    deleteTeam: owner,
    leave: !owner,
  };
}

/** 내가 이 구성원을 내보낼 수 있는가. 자기 자신은 "떠나기"로 따로 처리한다. */
export function canRemove(me: TeamRole, target: TeamRole): boolean {
  if (me === "OWNER") {
    return target !== "OWNER";
  }
  return me === "MANAGER" && target === "MEMBER";
}

export const ROLE_LABELS: Record<TeamRole, string> = {
  OWNER: "소유자",
  MANAGER: "관리자",
  MEMBER: "구성원",
};

/** 팀원별 공유 범위(#197). 서버가 매 요청·매 주기 이 값으로 팀 화면을 다시 거른다. */
export type ShareMode = "all" | "selected" | "none";

export const SHARE_MODES: ShareMode[] = ["all", "selected", "none"];

export const SHARE_MODE_LABELS: Record<ShareMode, string> = {
  all: "모두 공유",
  selected: "선택한 프로젝트만",
  none: "공유 안 함",
};

export const SHARE_MODE_HELP: Record<ShareMode, string> = {
  all: "내 모든 세션이 이 팀 사무실에 보입니다. 프로젝트가 없는 세션은 '(이름 없음)'으로 보입니다.",
  selected: "아래에서 체크한 프로젝트의 세션만 보입니다. 폴더 이름으로 자동으로 만든 프로젝트도 고를 수 있습니다.",
  none: "이 팀에는 내 세션이 하나도 보이지 않습니다. 팀원의 세션은 계속 볼 수 있습니다.",
};

/** 가입·생성 화면과 설정에 함께 보이는 동의 설명(#197). */
export const SHARE_VISIBLE_TEXT =
  "팀원에게 보이는 것: 상태·활동 종류·표시 이름·프로젝트 이름. 대화·코드·경로·폴더 전체 경로는 보이지 않습니다.";

/**
 * 팀별 "PC 이름 공개" 옵션(서버 필드 `share_device_name`, 기본 끔). 켠 팀의 팀원에게만
 * 세션 목록·상세에 내 PC 이름이 함께 보인다. 캐릭터 머리 위 이름표는 사람 이름 그대로다.
 */
export const DEVICE_NAME_SHARE_LABEL = "이 팀에 내 PC 이름 보이기";

export const DEVICE_NAME_SHARE_HELP =
  "켜면 이 팀 팀원에게 세션 목록과 상세에서 '이름 · PC 이름'으로 보이고, 끄면 이름만 보입니다. 이 팀에만 적용됩니다.";

export const DEVICE_NAME_SHARE_NONE_NOTE = "지금은 '공유 안 함'이라 이 팀에 보이는 내 세션이 없습니다.";

/** 이전 서버는 `share_device_name`을 보내지 않는다. 없거나 불리언이 아니면 이 옵션을 화면에 내지 않는다. */
export function supportsDeviceNameShare(team: { share_device_name?: unknown }): boolean {
  return typeof team.share_device_name === "boolean";
}

/**
 * 체크 상자의 화면 상태. 표시 값의 기준은 항상 서버 값(props의 `team.share_device_name`)이고,
 * 이 상태는 그 위에 잠시 덮어쓰는 값만 가진다.
 * - `pending`: 저장 요청 중인 값(낙관적 표시). 요청이 끝나면 null.
 * - `saved`: 방금 저장에 성공한 값. props가 아직 저장 전 값(`from`)이면 `to`를 보여 주고,
 *   props가 `from`에서 벗어나면(새 응답 도착) 버리고 props를 따른다.
 */
export interface DeviceNameShareView {
  pending: boolean | null;
  saved: { from: boolean; to: boolean } | null;
}

export const DEVICE_NAME_SHARE_IDLE: DeviceNameShareView = { pending: null, saved: null };

/** 지금 체크 상자에 보일 값. */
export function deviceNameShareShown(server: boolean, view: DeviceNameShareView): boolean {
  if (view.pending !== null) {
    return view.pending;
  }
  return view.saved && view.saved.from === server ? view.saved.to : server;
}

/** 새 props가 오면 호출한다. 서버 값이 저장 전 값에서 벗어났으면 덮어쓰기를 버린다(같은 객체면 변화 없음). */
export function settleDeviceNameShare(server: boolean, view: DeviceNameShareView): DeviceNameShareView {
  return view.saved && view.saved.from !== server ? { ...view, saved: null } : view;
}

interface ShareResult {
  ok: boolean;
  error: string | null;
  data?: unknown;
}

/**
 * "PC 이름 공개"를 바꾼다. 먼저 새 값을 보여 주고(`pending`), 성공하면 서버가 돌려준 값(없으면 보낸 값)을
 * `saved`로 남기고 `onSaved`로 알린다. 실패하면 덮어쓰기를 모두 지워 서버 값(이전 값)으로 되돌린다.
 * `share_mode`는 보내지 않으므로 공유 범위는 그대로다.
 */
export async function toggleShareDeviceName<R extends ShareResult>(options: {
  teamId: string;
  /** 바꾸기 전 서버 값. */
  current: boolean;
  next: boolean;
  csrf: string;
  setView: (view: DeviceNameShareView) => void;
  onSaved?: (value: boolean) => void;
  request: (method: string, path: string, body: unknown, csrf: string) => Promise<R>;
}): Promise<R> {
  const { teamId, current, next, csrf, setView, onSaved, request } = options;
  setView({ pending: next, saved: null });
  const result = await request("PUT", `/api/v1/teams/${encodeURIComponent(teamId)}/share-mode`, { share_device_name: next }, csrf);
  if (!result.ok) {
    setView(DEVICE_NAME_SHARE_IDLE);
    return result;
  }
  const returned = (result.data as { share_device_name?: unknown } | null | undefined)?.share_device_name;
  const value = typeof returned === "boolean" ? returned : next;
  setView({ pending: null, saved: { from: current, to: value } });
  onSaved?.(value);
  return result;
}

/**
 * 목록을 받는 동안 `epoch()`가 바뀌었으면(그 사이 저장이 끝남) 그 응답은 저장 전 값일 수 있으므로 버리고
 * 다시 받는다. `attempts`번 모두 겹치면 `fresh: false`를 돌려주고, 부르는 쪽은 지금 목록을 그대로 둔다.
 */
export async function loadUnlessChanged<T>(
  epoch: () => number,
  load: () => Promise<T>,
  attempts = 3,
): Promise<{ fresh: true; value: T } | { fresh: false }> {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const started = epoch();
    const value = await load();
    if (epoch() === started) {
      return { fresh: true, value };
    }
  }
  return { fresh: false };
}

/** 가입·생성 화면의 "모두 공유" 체크(기본 체크)를 서버에 보낼 범위로 바꾼다. */
export function shareModeFromConsent(shareAll: boolean): ShareMode {
  return shareAll ? "all" : "selected";
}

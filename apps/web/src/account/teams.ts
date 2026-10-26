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

/** 가입·생성 화면의 "모두 공유" 체크(기본 체크)를 서버에 보낼 범위로 바꾼다. */
export function shareModeFromConsent(shareAll: boolean): ShareMode {
  return shareAll ? "all" : "selected";
}

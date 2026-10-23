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

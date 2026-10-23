/** 설치 마법사 단계 결정(#66, #67). 화면과 분리해 테스트한다. */

export interface SetupStatus {
  setup_completed: boolean;
  admin_exists: boolean;
  bootstrap_pending: boolean;
}

export type WizardStep = "completed" | "admin" | "login" | "site" | "policy" | "mail" | "finish";

export const SETTINGS_STEPS: WizardStep[] = ["site", "policy", "mail", "finish"];

export function startStep(status: SetupStatus, role: string | null): WizardStep {
  if (status.setup_completed) {
    return "completed";
  }
  if (!status.admin_exists) {
    return "admin";
  }
  // 관리자가 이미 있으면 그 관리자로 로그인해야 이어서 설정할 수 있다.
  return role === "SUPER_ADMIN" ? "site" : "login";
}

export function nextStep(step: WizardStep): WizardStep {
  const index = SETTINGS_STEPS.indexOf(step);
  return index >= 0 && index < SETTINGS_STEPS.length - 1 ? SETTINGS_STEPS[index + 1]! : step;
}

export const POLICY_LABELS: Record<string, string> = {
  PUBLIC_VERIFIED_EMAIL: "공개 가입(이메일 확인)",
  ADMIN_APPROVAL: "가입 후 관리자 승인",
  INVITE_ONLY: "초대한 사용자만",
  CLOSED: "가입 중단",
};

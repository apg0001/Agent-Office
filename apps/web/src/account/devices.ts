/**
 * PC(Collector 장치) 화면 도우미(#41). 등록 코드는 화면에만 보여 주고 저장하지 않는다.
 */

export interface DeviceSummary {
  id: string;
  name: string;
  platform: "windows" | "macos" | "linux" | "unknown";
  collector_version: string | null;
  created_at: string;
  last_seen_at: string | null;
  revoked_at: string | null;
  connection: "ONLINE" | "DISCONNECTED";
}

export interface RegistrationCode {
  code: string;
  expires_at: string;
}

export const PLATFORM_LABELS: Record<DeviceSummary["platform"], string> = {
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
  unknown: "알 수 없음",
};

/** 장치 상태 문구. 폐기가 연결 상태보다 우선한다. */
export function deviceStatus(device: DeviceSummary): string {
  if (device.revoked_at) {
    return "폐기됨";
  }
  return device.connection === "ONLINE" ? "연결됨" : "연결 끊김";
}

/** 만료까지 남은 시간(분:초). 지났으면 null. */
export function remaining(expiresAt: string, now: number): string | null {
  const left = Math.floor((Date.parse(expiresAt) - now) / 1000);
  if (!Number.isFinite(left) || left <= 0) {
    return null;
  }
  const minutes = Math.floor(left / 60);
  const seconds = left % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

/** 활성 장치를 먼저, 그 안에서는 이름순으로 정렬한다. */
export function sortDevices(devices: DeviceSummary[]): DeviceSummary[] {
  return [...devices].sort((a, b) => {
    if (!a.revoked_at !== !b.revoked_at) {
      return a.revoked_at ? 1 : -1;
    }
    return a.name.localeCompare(b.name, "ko");
  });
}

export const MAX_DEVICE_NAME = 80;

/**
 * PC 이름 변경(#206) 입력 검사. 서버와 같은 규칙(앞뒤 공백 제거 후 1~80자, 제어 문자 없음).
 * 맞으면 다듬은 이름, 아니면 오류 문구.
 */
export function checkDeviceName(input: string): { name: string } | { error: string } {
  const name = input.trim();
  if (name.length === 0) {
    return { error: "이름을 입력하세요." };
  }
  if (Array.from(name).length > MAX_DEVICE_NAME) {
    return { error: `이름은 ${MAX_DEVICE_NAME}자 이하로 입력하세요.` };
  }
  if (/[\u0000-\u001f\u007f-\u009f]/.test(name)) {
    return { error: "줄바꿈이나 제어 문자는 쓸 수 없습니다." };
  }
  return { name };
}

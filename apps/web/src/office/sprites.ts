/**
 * 픽셀 캐릭터(#46). 외부 에셋 없이 문자 격자로 그린다(라이선스·저장소 크기 부담 없음).
 * 리드와 하위 에이전트는 색이 아니라 모양(왕관·크기·넥타이)으로도 구분한다.
 */

import { mixHash } from "./presence";
import type { AgentState, DeviceState } from "./types";

export type Frame = 0 | 1;

/** `.` 투명, `k` 왕관, `h` 머리카락, `s` 피부, `e` 눈, `c` 옷(PC별 색), `b` 넥타이, `p` 바지, `o` 신발. */
const LEAD_BODY = [
  "...k.kk.k...",
  "...kkkkkk...",
  "...hhhhhh...",
  "..hhhhhhhh..",
  "..hssssssh..",
  "..sesssses..",
  "..ssssssss..",
  "...ssssss...",
  "..cccccccc..",
  ".cccccccccc.",
  ".sccccccccs.",
  ".sccccccccs.",
  "..cccccccc..",
  "..pppppppp..",
];
const LEAD_LEGS: Record<Frame, string[]> = {
  0: ["..ppp..ppp..", "..ppp..ppp..", "..pp....pp..", "..oo....oo.."],
  1: ["..ppp..ppp..", "...pp..pp...", "...pp..pp...", "...oo..oo..."],
};
const SUB_BODY = [
  "..hhhhhh..",
  ".hhhhhhhh.",
  ".hssssssh.",
  ".sesssses.",
  "..ssssss..",
  ".cccbbccc.",
  "scccbbcccs",
  "scccbbcccs",
  ".cccccccc.",
  ".pppppppp.",
];
const SUB_LEGS: Record<Frame, string[]> = {
  0: [".ppp..ppp.", ".pp....pp.", ".oo....oo."],
  1: ["..pp..pp..", "..pp..pp..", "..oo..oo.."],
};

export function spriteRows(role: AgentState["role"], frame: Frame): string[] {
  return role === "lead" ? [...LEAD_BODY, ...LEAD_LEGS[frame]] : [...SUB_BODY, ...SUB_LEGS[frame]];
}

/** 어두운 바닥에서도 구분되는 옷 색 8개. 색만으로 의미를 전달하지 않는다(PC 구분 보조용). */
export const SHIRT_COLORS = [
  0x3f6fd0, 0xd0603f, 0x3fa060, 0xa04fc0, 0xc0a03f, 0x3fa0b0, 0xc04f7f, 0x7f8f3f,
] as const;

const FIXED_COLORS: Record<string, number> = {
  k: 0xf0d040,
  h: 0x4a3020,
  s: 0xf0c8a0,
  e: 0x202020,
  b: 0xf0f0f0,
  p: 0x303848,
  o: 0x181818,
};

/** 같은 PC의 캐릭터는 같은 옷 색. 장치 ID 해시로 정하므로 새로고침해도 같다. */
export function shirtIndex(deviceId: string): number {
  return mixHash(deviceId) % SHIRT_COLORS.length;
}

export function pixelColor(code: string, shirt: number): number | null {
  if (code === ".") {
    return null;
  }
  if (code === "c") {
    return SHIRT_COLORS[shirt] ?? SHIRT_COLORS[0];
  }
  return FIXED_COLORS[code] ?? null;
}

export function textureKey(role: AgentState["role"], shirt: number, frame: Frame): string {
  return `agent-${role}-${shirt}-${frame}`;
}

/** 걷는 중이면 180ms마다 다리를 바꾼다. 서 있으면 첫 프레임. */
export function walkFrame(moving: boolean, time: number): Frame {
  return moving && Math.floor(time / 180) % 2 === 1 ? 1 : 0;
}

const MAX_LABEL = 10;

/**
 * 머리 위 짧은 이름. 사용자가 붙인 PC 이름만 쓰며, 제어 문자를 지우고 10자로 줄인다.
 * 프롬프트·경로·명령은 서버에 없으므로 라벨에도 나올 수 없다.
 */
export function labelFor(agent: AgentState, device: DeviceState | undefined): string {
  if (agent.role === "subagent") {
    return "하위";
  }
  const name = (device?.name ?? "")
    .replace(/\s+/g, " ")
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, "")
    .trim();
  const chars = Array.from(name);
  if (chars.length === 0) {
    return "리드";
  }
  return chars.length > MAX_LABEL ? `${chars.slice(0, MAX_LABEL - 1).join("").trimEnd()}…` : name;
}

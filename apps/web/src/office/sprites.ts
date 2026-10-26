/**
 * 픽셀 캐릭터(#46). 외부 에셋 없이 문자 격자로 그린다(라이선스·저장소 크기 부담 없음).
 * 리드와 하위 에이전트는 색이 아니라 모양(왕관·크기·넥타이)으로, Claude·Codex는 모자·안경으로 구분한다.
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

/** 도구별 겉모습(#192): Claude는 주황 모자, Codex는 검은 안경. 색이 아니라 모양으로도 구분한다. */
export type Look = "claude" | "codex" | "plain";

export function lookFor(provider: string | undefined): Look {
  return provider === "claude" || provider === "codex" ? provider : "plain";
}

/** 눈 줄(`e`가 있는 줄)을 안경으로: 두 눈을 g로 감싼다. */
function withGlasses(row: string): string {
  const chars = Array.from(row);
  chars.forEach((c, i) => {
    if (c === "e") {
      if (chars[i - 1] === "s") chars[i - 1] = "g";
      if (chars[i + 1] === "s") chars[i + 1] = "g";
    }
  });
  return chars.join("");
}

function applyLook(rows: string[], look: Look): string[] {
  if (look === "claude") {
    // 머리카락 위 두 줄을 모자로.
    let capped = 0;
    return rows.map((row) => {
      if (capped < 2 && row.includes("h") && !row.includes("s")) {
        capped += 1;
        return row.replaceAll("h", "a");
      }
      return row;
    });
  }
  if (look === "codex") {
    return rows.map((row) => (row.includes("e") ? withGlasses(row) : row));
  }
  return rows;
}

export function spriteRows(role: AgentState["role"], frame: Frame, look: Look = "plain"): string[] {
  const rows = role === "lead" ? [...LEAD_BODY, ...LEAD_LEGS[frame]] : [...SUB_BODY, ...SUB_LEGS[frame]];
  return applyLook(rows, look);
}

/** 어두운 바닥에서도 구분되는 옷 색 8개. 색만으로 의미를 전달하지 않는다(세션 구분 보조용). */
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
  a: 0xe0783a,
  g: 0x1a1a22,
};

/** 해시로 정한 옷 색(책상 번호가 없을 때). */
export function shirtIndex(id: string): number {
  return mixHash(id) % SHIRT_COLORS.length;
}

/**
 * 세션 옷 색(#192). 서버가 활성 세션마다 겹치지 않게 주는 책상 번호를 쓰므로 동시에 일하는 세션 8개까지는
 * 색이 모두 다르다(PC 해시를 쓰면 서로 다른 PC가 같은 색이 될 수 있었다). 같은 세션의 대장·하위는 같은 색.
 */
export function shirtForSession(session: { id: string; desk_slot: number | null } | undefined, fallback: string): number {
  if (session && session.desk_slot !== null && session.desk_slot >= 0) {
    return session.desk_slot % SHIRT_COLORS.length;
  }
  return shirtIndex(session?.id ?? fallback);
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

export function textureKey(role: AgentState["role"], shirt: number, frame: Frame, look: Look = "plain"): string {
  return `agent-${role}-${look}-${shirt}-${frame}`;
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

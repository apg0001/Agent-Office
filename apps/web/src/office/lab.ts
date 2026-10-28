/**
 * 테스트 구역 실험실 소품(#235). Kenney 키트에 맞는 그림이 없어 캐릭터(sprites.ts)처럼 문자 격자로
 * 그린 픽셀 그림이다. 한 글자가 픽셀 하나이고 `.`은 투명이다. 실험대(흰 책상) 위에 올린다.
 */

import type { Furniture } from "./grid";

export const LAB_PALETTE: Record<string, number> = {
  g: 0xcfe6ee, // 유리 테두리
  w: 0xffffff, // 유리 반사
  G: 0x56e070, // 초록 용액
  B: 0x4fa8ff, // 파란 용액
  P: 0xff6fb0, // 분홍 용액
  Y: 0xffd84a, // 노란 용액
  k: 0x2a2f38, // 검은 금속
  m: 0x8a95a5, // 회색 금속
  d: 0x13241a, // 꺼진 화면
  r: 0xe0503a, // 빨간 표시
};

/** 삼각 플라스크 + 시험관 꽂이. */
const FLASKS = [
  "....gg................",
  "....gg..........g.g.g.",
  "....gg..........g.g.g.",
  "...gGGg.........P.B.Y.",
  "..gGGGGg........P.B.Y.",
  ".gGGwGGGg.......P.B.Y.",
  ".gGGGGGGg.....mmPmBmYm",
  "..ggggggg.....m......m",
];

/** 현미경 + 결과 화면(초록 체크). */
const SCOPE = [
  "..kk..........kkkkkkkk",
  "..kmk.........kddddddk",
  "...km.........kddddGdk",
  "...kmm........kdGddGdk",
  "..kkmk........kddGGddk",
  "..k.mk........kddddddk",
  ".kkkkkk.......kkkkkkkk",
  "kmmmmmmk.........kk...",
];

export interface PixelPiece {
  rows: string[];
  /** 칸 중심(화면 좌표)에서 그림 아래 가운데까지(px). */
  dx: number;
  dy: number;
  layer: number;
}

export const LAB_PIXEL_ART: Partial<Record<Furniture, PixelPiece[]>> = {
  labBench: [{ rows: FLASKS, dx: 2, dy: -6, layer: 3 }],
  labScope: [{ rows: SCOPE, dx: 2, dy: -6, layer: 3 }],
};

/** 플라스크 거품이 올라오는 위치(실험대 칸 중심 기준, px). */
export const BUBBLE_ORIGIN = { dx: -9, dy: -26 };

export function labColor(code: string): number | null {
  return code === "." ? null : (LAB_PALETTE[code] ?? null);
}

export function pixelTextureKey(furniture: Furniture, index: number): string {
  return `lab-${furniture}-${index}`;
}

/**
 * 가구·벽 그림 배치(#153). 그림은 Kenney Furniture Kit 2.0(CC0, `public/office/kenney/License.txt`)의
 * 아이소메트릭 PNG다. 논리 그리드(가구 칸은 걸을 수 없음)는 grid.ts가 정하고, 여기는 칸마다 어떤
 * 그림을 어디에 겹쳐 그릴지만 정하는 순수 데이터다.
 *
 * 방향: 업무석 의자는 책상 아래 왼쪽 칸에 있으므로 책상·모니터는 SW 그림, 의자는 책상을 보는 NW 그림이다
 * (Kenney 방향 이름은 바라보는 쪽과 다를 수 있어 스크린샷으로 확인했다).
 */

import type { Furniture } from "./grid";

/** Kenney 바닥 한 칸(약 208px)을 우리 타일(64px)에 맞추는 배율. 바닥 기울기도 같다(iso.ts). */
export const ART_SCALE = 64 / 208;
export const ART_BASE_URL = "/office/kenney/";

export interface ArtPiece {
  image: string;
  /** 칸 중심(화면 좌표)에서 그림 아래 가운데까지의 거리(px). */
  dx: number;
  dy: number;
  /** 같은 칸 안에서 위에 그릴수록 크다. */
  layer: number;
  tint?: number;
  /** 기본 ART_SCALE 대신 쓸 배율(여러 칸을 이어 보이게 할 때). */
  scale?: number;
  /** 이 칸이 아니라 옆 칸(걸을 수 있는 칸)에 그리는 장식. 깊이도 그 칸 기준이다. */
  cell?: { x: number; y: number };
}

/** 서버 랙은 닫힌 책장을 어두운 금속색으로 칠해 쓴다. */
const RACK_TINT = 0x5a6472;

export const FURNITURE_ART: Record<Furniture, ArtPiece[]> = {
  desk: [
    { image: "desk_SW", dx: 0, dy: 14, layer: 2 },
    { image: "computerScreen_SW", dx: 2, dy: -8, layer: 3 },
  ],
  shelf: [{ image: "bookcaseClosedWide_SW", dx: 0, dy: 12, layer: 2 }],
  terminal: [
    { image: "desk_SW", dx: 0, dy: 14, layer: 2 },
    { image: "laptop_SW", dx: 0, dy: -8, layer: 3 },
  ],
  rack: [
    { image: "bookcaseClosedDoors_SW", dx: -6, dy: 12, layer: 2, tint: RACK_TINT },
    { image: "bookcaseClosedDoors_SW", dx: 8, dy: 19, layer: 3, tint: RACK_TINT },
  ],
  whiteboard: [
    { image: "cabinetTelevision_SW", dx: 0, dy: 14, layer: 2 },
    { image: "televisionModern_SW", dx: 0, dy: -6, layer: 3 },
  ],
  // 2칸을 이어 긴 테이블로 보이게 조금 크게 그리고, 앞(y + 1) 칸에 의자를 둔다.
  table: [
    { image: "table_SW", dx: 0, dy: 18, layer: 2, scale: 0.42 },
    { image: "chair_NW", dx: 0, dy: 8, layer: 1, cell: { x: 0, y: 1 } },
  ],
  sofa: [{ image: "loungeSofa_SW", dx: 0, dy: 16, layer: 2 }],
  coffee: [
    { image: "kitchenBar_SW", dx: 0, dy: 14, layer: 2 },
    { image: "kitchenCoffeeMachine_SW", dx: 0, dy: -6, layer: 3 },
  ],
  fridge: [{ image: "kitchenFridgeLarge_SW", dx: 0, dy: 14, layer: 2 }],
  plant: [{ image: "pottedPlant_SW", dx: 0, dy: 12, layer: 2 }],
};

/** 업무석 자리(걸을 수 있는 칸)에 그리는 의자. 등받이가 화면 왼쪽 아래(사람 쪽), 앉는 면이 책상(오른쪽 위)을 본다. 캐릭터보다 먼저 그린다. */
export const SEAT_ART: ArtPiece = { image: "chairDesk_NW", dx: 0, dy: 10, layer: 1 };

/** 미리 불러올 그림 이름 전체. */
export function artImages(): string[] {
  const names = new Set<string>([SEAT_ART.image]);
  for (const pieces of Object.values(FURNITURE_ART)) {
    for (const piece of pieces) {
      names.add(piece.image);
    }
  }
  return [...names].sort();
}

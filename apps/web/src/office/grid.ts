/**
 * 사무실 논리 그리드. 화면 투영과 분리된 순수 데이터라 Phaser 없이 테스트한다.
 * 좌표는 (x, y) 타일 단위이며 x는 오른쪽 아래, y는 왼쪽 아래 방향이다.
 */

export type ZoneKind =
  | "desk"
  | "library"
  | "internet"
  | "meeting"
  | "testing"
  | "lounge"
  | "corridor"
  | "entrance";

export type Furniture =
  | "desk"
  | "shelf"
  | "terminal"
  | "whiteboard"
  | "table"
  | "rack"
  | "labBench"
  | "labScope"
  | "sofa"
  | "coffee"
  | "fridge"
  | "plant";

export interface Point {
  x: number;
  y: number;
}

export interface Rect extends Point {
  width: number;
  height: number;
}

export interface Tile extends Point {
  zone: ZoneKind | null;
  furniture: Furniture | null;
  walkable: boolean;
}

/** 두 이웃 칸 사이의 칸막이 벽 한 조각. 문이면 지나갈 수 있다(#164). */
export interface Partition {
  a: Point;
  b: Point;
  door: boolean;
}

export interface OfficeMap {
  width: number;
  height: number;
  tiles: Tile[];
  zones: Record<ZoneKind, Rect>;
  entrance: Point;
  /** 구역 사이 칸막이(없으면 빈 배열). 벽 조각은 이동을 막고 문 조각은 통과한다. */
  partitions: Partition[];
  /** 책상 자리(사람이 앉는 칸). 책상 가구 바로 앞의 걸을 수 있는 칸이다. */
  seats: Point[];
}

export const ZONE_LABELS: Record<ZoneKind, string> = {
  desk: "업무석",
  library: "도서관",
  internet: "인터넷",
  meeting: "회의실",
  testing: "테스트 구역",
  lounge: "휴게실",
  corridor: "복도",
  entrance: "출입구",
};

/** 바닥 색(지도와 범례가 같은 값을 쓴다). */
export const ZONE_COLORS: Record<ZoneKind, number> = {
  desk: 0xd8c8a8,
  library: 0xb8c8e0,
  internet: 0xb8e0d0,
  meeting: 0xe0c8e0,
  testing: 0xa7b0ba,
  lounge: 0xc8e0b0,
  corridor: 0xcfcfcf,
  entrance: 0xa0a0a0,
};

/** 구역이 뜻하는 것(behavior.ts의 활동 → 구역 규칙과 같다). 화면 범례에 쓴다. */
export const ZONE_GUIDE: { zone: ZoneKind; meaning: string }[] = [
  { zone: "desk", meaning: "자기 자리. 코드 편집·생각 중·승인 대기·하위 에이전트 위임·결과 정리, 무엇인지 모를 때" },
  { zone: "library", meaning: "파일 읽기·코드 검색" },
  { zone: "internet", meaning: "웹 검색·웹 페이지 가져오기" },
  { zone: "testing", meaning: "테스트 실행" },
  { zone: "meeting", meaning: "계획 세우기(할 일 목록·계획 도구)" },
  { zone: "lounge", meaning: "할 일 없이 쉬는 중(턴이 끝남). 가끔 자리를 옮김" },
  { zone: "entrance", meaning: "세션이 시작되면 들어오고, 끝나면 여기로 퇴근" },
];

export function contains(rect: Rect, point: Point): boolean {
  return (
    point.x >= rect.x &&
    point.x < rect.x + rect.width &&
    point.y >= rect.y &&
    point.y < rect.y + rect.height
  );
}

export function tileAt(map: OfficeMap, point: Point): Tile | undefined {
  if (point.x < 0 || point.y < 0 || point.x >= map.width || point.y >= map.height) {
    return undefined;
  }
  return map.tiles[point.y * map.width + point.x];
}

export function isWalkable(map: OfficeMap, point: Point): boolean {
  return tileAt(map, point)?.walkable ?? false;
}

const edgeKey = (a: Point, b: Point) =>
  a.x < b.x || (a.x === b.x && a.y < b.y) ? `${a.x},${a.y}|${b.x},${b.y}` : `${b.x},${b.y}|${a.x},${a.y}`;
const blockedCache = new WeakMap<OfficeMap, Set<string>>();

/** 이웃한 두 칸 사이가 칸막이 벽으로 막혀 있는지(문은 막히지 않음). */
export function isBlockedEdge(map: OfficeMap, a: Point, b: Point): boolean {
  let blocked = blockedCache.get(map);
  if (!blocked) {
    blocked = new Set(map.partitions.filter((p) => !p.door).map((p) => edgeKey(p.a, p.b)));
    blockedCache.set(map, blocked);
  }
  return blocked.has(edgeKey(a, b));
}

/** a에서 이웃 b로 한 칸 걸어갈 수 있는지. */
export function canStep(map: OfficeMap, a: Point, b: Point): boolean {
  return isWalkable(map, b) && !isBlockedEdge(map, a, b);
}

/**
 * 기본 사무실. 위쪽 띠에 업무석, 아래쪽에 활동 구역, 가운데 복도, 왼쪽 아래 출입구.
 * `deskRows`를 늘리면 업무석만 아래로 늘어나 활동 중 다른 자리가 움직이지 않는다(#48).
 */
export function createDefaultOffice(deskRows = 2): OfficeMap {
  const width = 16;
  const deskHeight = deskRows * 3;
  const height = deskHeight + 9;
  const zones: Record<ZoneKind, Rect> = {
    desk: { x: 1, y: 1, width: 14, height: deskHeight },
    corridor: { x: 0, y: deskHeight + 1, width: 16, height: 1 },
    library: { x: 1, y: deskHeight + 2, width: 4, height: 3 },
    internet: { x: 6, y: deskHeight + 2, width: 4, height: 3 },
    testing: { x: 11, y: deskHeight + 2, width: 4, height: 3 },
    meeting: { x: 1, y: deskHeight + 5, width: 6, height: 3 },
    lounge: { x: 8, y: deskHeight + 5, width: 7, height: 3 },
    entrance: { x: 0, y: height - 1, width: 1, height: 1 },
  };

  const tiles: Tile[] = [];
  for (let y = 0; y < height; y += 1) {
    for (let x = 0; x < width; x += 1) {
      const zone =
        (Object.keys(zones) as ZoneKind[]).find((kind) => contains(zones[kind], { x, y })) ?? null;
      const border = x === 0 || y === 0 || x === width - 1 || y === height - 1;
      // 바깥 테두리는 벽이다. 출입구와 복도 끝만 통과할 수 있다.
      const walkable = zone !== null && (!border || zone === "entrance" || zone === "corridor");
      tiles.push({ x, y, zone, furniture: null, walkable });
    }
  }
  const map: OfficeMap = {
    width,
    height,
    tiles,
    zones,
    entrance: { x: 0, y: height - 1 },
    seats: [],
    partitions: [],
  };
  // 출입구에서 복도로 이어지는 통로.
  for (let y = deskHeight + 1; y < height; y += 1) {
    const tile = tileAt(map, { x: 0, y });
    if (tile) {
      tile.walkable = true;
      tile.zone = tile.zone ?? "corridor";
    }
  }

  const place = (point: Point, furniture: Furniture) => {
    const tile = tileAt(map, point);
    if (tile) {
      tile.furniture = furniture;
      tile.walkable = false;
    }
  };
  // 업무석: 3행마다 책상 줄, 그 아래 칸이 의자 자리다. 줄 사이 통로를 남긴다.
  for (let row = 0; row < deskRows; row += 1) {
    const y = 1 + row * 3;
    for (let x = 2; x < 15; x += 2) {
      place({ x, y }, "desk");
      map.seats.push({ x, y: y + 1 });
    }
  }
  const library = zones.library;
  for (let x = library.x; x < library.x + library.width; x += 1) {
    place({ x, y: library.y }, "shelf");
  }
  const internet = zones.internet;
  place({ x: internet.x + 1, y: internet.y }, "terminal");
  place({ x: internet.x + 3, y: internet.y }, "terminal");
  const testing = zones.testing;
  place({ x: testing.x + testing.width - 1, y: testing.y }, "rack");
  place({ x: testing.x + testing.width - 1, y: testing.y + 1 }, "rack");
  // 실험실(#235): 문(testing.x + 1) 옆과 뒤쪽 구석에 실험대. 가운데 칸은 비워 둔다.
  place({ x: testing.x, y: testing.y }, "labBench");
  place({ x: testing.x + testing.width - 1, y: testing.y + testing.height - 1 }, "labScope");
  // 회의실: 뒤쪽 화면과 그 앞 2칸짜리 테이블.
  place({ x: zones.meeting.x + 2, y: zones.meeting.y }, "whiteboard");
  place({ x: zones.meeting.x + 2, y: zones.meeting.y + 1 }, "table");
  place({ x: zones.meeting.x + 3, y: zones.meeting.y + 1 }, "table");
  // 휴게실: 소파, 뒤쪽 벽에 커피 바·냉장고·화분.
  place({ x: zones.lounge.x + 1, y: zones.lounge.y + 1 }, "sofa");
  place({ x: zones.lounge.x + zones.lounge.width - 3, y: zones.lounge.y }, "coffee");
  place({ x: zones.lounge.x + zones.lounge.width - 2, y: zones.lounge.y }, "fridge");
  place({ x: zones.lounge.x + zones.lounge.width - 1, y: zones.lounge.y }, "plant");
  addPartitions(map);
  return map;
}

/**
 * 구역 칸막이(#164). 업무석은 트인 사무실이고, 아래 방들은 벽으로 둘러 문으로만 드나든다.
 * - 도서관: 위(복도)는 벽(책장이 붙음), 왼쪽 복도 쪽에 문
 * - 인터넷·테스트 구역: 복도 쪽에 문
 * - 회의실·휴게실: 방 사이 빈 칸(x = 5, 10)을 좁은 통로로 열고 그 끝에 문
 * 방끼리 맞닿은 변과 통로 양옆은 벽이다.
 */
function addPartitions(map: OfficeMap): void {
  const { library, internet, testing, meeting, lounge, corridor } = map.zones;
  const top = library.y; // 위쪽 방 첫 줄
  const bottom = meeting.y; // 아래쪽 방 첫 줄
  const add = (a: Point, b: Point, door = false) => map.partitions.push({ a, b, door });
  const horizontal = (x0: number, x1: number, yAbove: number, doorX?: number) => {
    for (let x = x0; x <= x1; x += 1) {
      add({ x, y: yAbove }, { x, y: yAbove + 1 }, x === doorX);
    }
  };
  const vertical = (y0: number, y1: number, xLeft: number, doorY?: number) => {
    for (let y = y0; y <= y1; y += 1) {
      add({ x: xLeft, y }, { x: xLeft + 1, y }, y === doorY);
    }
  };
  const last = (r: Rect) => ({ x: r.x + r.width - 1, y: r.y + r.height - 1 });

  // 복도 ↔ 위쪽 방(통로 칸 x = 5, 10은 열어 둔다).
  horizontal(library.x, last(library).x, corridor.y);
  horizontal(internet.x, last(internet).x, corridor.y, internet.x + 2);
  horizontal(testing.x, last(testing).x, corridor.y, testing.x + 1);
  // 왼쪽 복도(x = 0) ↔ 도서관(문)·회의실(벽).
  vertical(library.y, last(library).y, 0, library.y + 1);
  vertical(meeting.y, last(meeting).y, 0);
  // 통로(x = 5, 10) 양옆 벽.
  for (const hall of [library.x + library.width, internet.x + internet.width]) {
    vertical(top, last(library).y, hall - 1);
    vertical(top, last(library).y, hall);
    for (let y = top; y <= last(library).y; y += 1) {
      const t = tileAt(map, { x: hall, y });
      if (t) {
        t.zone = "corridor";
        t.walkable = true;
      }
    }
  }
  // 위쪽 방 ↔ 아래쪽 방. 통로 끝(x = 5 → 회의실, x = 10 → 휴게실)은 문.
  horizontal(meeting.x, last(meeting).x, bottom - 1, library.x + library.width);
  horizontal(lounge.x, last(lounge).x, bottom - 1, internet.x + internet.width);
}

/** 구역 안의 걸을 수 있는 칸 목록. 활동 목적지를 고를 때 쓴다. */
export function walkableTiles(map: OfficeMap, zone: ZoneKind): Point[] {
  const rect = map.zones[zone];
  const result: Point[] = [];
  for (let y = rect.y; y < rect.y + rect.height; y += 1) {
    for (let x = rect.x; x < rect.x + rect.width; x += 1) {
      if (isWalkable(map, { x, y })) {
        result.push({ x, y });
      }
    }
  }
  return result;
}

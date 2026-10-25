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

export interface OfficeMap {
  width: number;
  height: number;
  tiles: Tile[];
  zones: Record<ZoneKind, Rect>;
  entrance: Point;
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
  const map: OfficeMap = { width, height, tiles, zones, entrance: { x: 0, y: height - 1 }, seats: [] };
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
  // 회의실: 뒤쪽 화면과 그 앞 2칸짜리 테이블.
  place({ x: zones.meeting.x + 2, y: zones.meeting.y }, "whiteboard");
  place({ x: zones.meeting.x + 2, y: zones.meeting.y + 1 }, "table");
  place({ x: zones.meeting.x + 3, y: zones.meeting.y + 1 }, "table");
  // 휴게실: 소파, 뒤쪽 벽에 커피 바·냉장고·화분.
  place({ x: zones.lounge.x + 1, y: zones.lounge.y + 1 }, "sofa");
  place({ x: zones.lounge.x + zones.lounge.width - 3, y: zones.lounge.y }, "coffee");
  place({ x: zones.lounge.x + zones.lounge.width - 2, y: zones.lounge.y }, "fridge");
  place({ x: zones.lounge.x + zones.lounge.width - 1, y: zones.lounge.y }, "plant");
  return map;
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

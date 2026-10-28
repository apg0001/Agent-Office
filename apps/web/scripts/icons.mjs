// 앱 아이콘 생성(#255). 사무실 리드 캐릭터(sprites.ts)를 32×32 문자 격자로 다시 그린 픽셀 그림을
// 외부 의존성 없이 PNG·SVG로 쓴다. 그림을 고치면 `node apps/web/scripts/icons.mjs`로 다시 만든다.

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync } from "node:zlib";

const OUT = join(dirname(fileURLToPath(import.meta.url)), "..", "public");

/** 한 글자가 픽셀 하나. 왼쪽 절반만 그리고 좌우 대칭으로 펼친다. `.`은 배경. */
const HALF = [
  "................",
  "................",
  "..........K....K",
  "..........KK..KK",
  "..........KKKKKK",
  "..........KKRKKB",
  "..........kkkkkk",
  "........hhhhhhhh",
  ".......hhhhhhhhh",
  "......hhhhhhhhhh",
  "......hhhsshhhss",
  "......hsssssssss",
  "......hsswesssss",
  "......hsseesssss",
  "......ssrrssssss",
  "......sssssssmss",
  ".......sssssssmm",
  "........ssssssss",
  "............SSSS",
  ".....ccccccccWWS",
  "....cccccccccWWb",
  "...ccccccccccCbb",
  "...ccccccccccCbb",
  "..cccccccccccCbb",
  "..ccccccccccccCb",
  "..cccccccccccccc",
  "DDDDDDDDDDDDDDDD",
  "dddddddddddddddd",
  "dddddddddddddddd",
  "dddddddddddddddd",
  "dddddddddddddddd",
  "dddddddddddddddd",
];

const PALETTE = {
  ".": [0x1b, 0x2a, 0x44],
  o: [0x26, 0x3a, 0x5c], // 머리 뒤 은은한 빛
  K: [0xf0, 0xd0, 0x40], // 왕관
  k: [0xc0, 0x98, 0x20],
  R: [0xe0, 0x50, 0x3a],
  B: [0x4f, 0xa8, 0xff],
  h: [0x4a, 0x30, 0x20], // 머리카락
  H: [0x6a, 0x48, 0x30],
  s: [0xf0, 0xc8, 0xa0], // 피부
  S: [0xd0, 0xa0, 0x78],
  e: [0x20, 0x20, 0x20],
  w: [0xff, 0xff, 0xff],
  r: [0xf0, 0x98, 0x88], // 볼
  m: [0x9a, 0x50, 0x40],
  c: [0x3f, 0x6f, 0xd0], // 옷(첫 번째 세션 색)
  C: [0x2f, 0x55, 0xa8],
  W: [0xf0, 0xf0, 0xf0], // 깃·말풍선
  b: [0xe0, 0x78, 0x3a], // 넥타이
  D: [0xc8, 0x8a, 0x52], // 책상
  d: [0x9a, 0x64, 0x38],
};

function grid() {
  const rows = HALF.map((left) => {
    if (left.length !== 16) throw new Error(`행 길이 ${left.length}: ${left}`);
    return Array.from(left + Array.from(left).reverse().join(""));
  });
  // 머리카락 왼쪽 위 윤기(비대칭).
  rows[8][10] = "H";
  rows[8][11] = "H";
  rows[9][9] = "H";
  // 오른쪽 눈 반사광도 왼쪽 눈과 같은 쪽(왼쪽 위)에.
  rows[12][21] = "w";
  rows[12][22] = "e";
  // 머리 위 상태 말풍선("…", 사무실 화면의 머리 위 상태와 같은 뜻).
  const bubble = [
    [1, ".WWWWW."],
    [2, "WWWWWWW"],
    [3, "WeWeWeW"],
    [4, "WWWWWWW"],
    [5, ".WWWWW."],
    [6, ".W....."],
  ];
  for (const [y, line] of bubble) {
    Array.from(line).forEach((c, i) => {
      if (c !== ".") rows[y][23 + i] = c;
    });
  }
  // 머리 뒤 빛: 배경 픽셀 가운데 원.
  rows.forEach((row, y) =>
    row.forEach((c, x) => {
      if (c === "." && (x - 15.5) ** 2 + (y - 13) ** 2 < 13.5 ** 2) row[x] = "o";
    }),
  );
  return rows;
}

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buf) {
  let c = 0xffffffff;
  for (const byte of buf) c = crcTable[(c ^ byte) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const head = Buffer.alloc(8);
  head.writeUInt32BE(data.length, 0);
  head.write(type, 4, "ascii");
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])), 0);
  return Buffer.concat([head, data, crc]);
}

/** RGBA 픽셀 함수로 PNG를 만든다. */
function png(size, pixel) {
  const raw = Buffer.alloc(size * (size * 4 + 1));
  for (let y = 0; y < size; y += 1) {
    raw[y * (size * 4 + 1)] = 0;
    for (let x = 0; x < size; x += 1) {
      raw.set(pixel(x, y), y * (size * 4 + 1) + 1 + x * 4);
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr.set([8, 6, 0, 0, 0], 8);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

/**
 * `art` 32칸을 `size` 픽셀 정사각형에 그린다. `inset`은 가장자리 여백 비율(maskable 안전 영역),
 * `radius`는 둥근 모서리 비율(0이면 꽉 찬 정사각형, 밖은 투명).
 */
function render(art, size, { inset = 0, radius = 0 } = {}) {
  const pad = Math.round(size * inset);
  const cell = (size - pad * 2) / 32;
  const r = size * radius;
  return png(size, (x, y) => {
    if (r > 0) {
      const cx = Math.min(Math.max(x + 0.5, r), size - r);
      const cy = Math.min(Math.max(y + 0.5, r), size - r);
      if ((x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2 > r * r) return [0, 0, 0, 0];
    }
    // 여백(maskable)은 가장자리 픽셀을 늘려 칠한다(배경·책상이 끊기지 않게).
    const rx = Math.floor((x - pad) / cell);
    const ry = Math.floor((y - pad) / cell);
    const c = art[Math.min(31, Math.max(0, ry))][Math.min(31, Math.max(0, rx))];
    const outside = rx < 0 || ry < 0 || rx > 31 || ry > 31;
    return [...PALETTE[outside && c === "o" ? "." : c], 255];
  });
}

function svg(art) {
  const rects = [];
  art.forEach((row, y) => {
    let x = 0;
    while (x < 32) {
      const c = row[x];
      let end = x + 1;
      while (end < 32 && row[end] === c) end += 1;
      const hex = PALETTE[c].map((v) => v.toString(16).padStart(2, "0")).join("");
      rects.push(`<rect x="${x}" y="${y}" width="${end - x}" height="1" fill="#${hex}"/>`);
      x = end;
    }
  });
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" shape-rendering="crispEdges">` +
    `<clipPath id="r"><rect width="32" height="32" rx="6"/></clipPath><g clip-path="url(#r)">${rects.join("")}</g></svg>\n`
  );
}

const art = grid();
const dir = join(OUT, "icons");
mkdirSync(dir, { recursive: true });
writeFileSync(join(dir, "icon-192.png"), render(art, 192, { radius: 0.18 }));
writeFileSync(join(dir, "icon-512.png"), render(art, 512, { radius: 0.18 }));
writeFileSync(join(dir, "icon-maskable-512.png"), render(art, 512, { inset: 0.1 }));
writeFileSync(join(dir, "apple-touch-icon.png"), render(art, 180));
writeFileSync(join(dir, "favicon-32.png"), render(art, 32, { radius: 0.18 }));
writeFileSync(join(OUT, "favicon.svg"), svg(art));

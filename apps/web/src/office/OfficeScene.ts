/**
 * Phaser 사무실 장면. 사실 상태(OfficeState)를 받아 목적지를 정하고, 캐릭터는 매 프레임
 * 그쪽으로 걸어간다. 이동은 순수 연출이며 사실 상태를 바꾸지 않는다. 캐릭터는 코드로 그리고,
 * 가구·벽은 CC0 그림(furnitureArt.ts)을 쓴다.
 */

import Phaser from "phaser";

import { assignSeats, targetTile } from "./behavior";
import {
  type CameraState,
  focusPoint,
  initialCamera,
  onManualControl,
  resumeAutoFocus,
  zoomBy,
} from "./camera";
import { findPath } from "./pathfinding";
import { BADGE_LABELS, idleCycle, presenceOf } from "./presence";
import { type DisplayedZone, nextDisplayedZone } from "./smoothing";
import {
  type Frame,
  labelFor,
  pixelColor,
  shirtIndex,
  spriteRows,
  textureKey,
  walkFrame,
} from "./sprites";
import { ART_BASE_URL, ART_SCALE, type ArtPiece, FURNITURE_ART, SEAT_ART, WALL_ART, artImages } from "./furnitureArt";
import type { OfficeMap, Point, ZoneKind } from "./grid";
import { ZONE_LABELS } from "./grid";
import { TILE_HEIGHT, TILE_WIDTH, depthOf, gridToScreen, tileDiamond } from "./iso";
import type { AgentState, OfficeState } from "./types";

const ZONE_COLORS: Record<ZoneKind, number> = {
  desk: 0xd8c8a8,
  library: 0xb8c8e0,
  internet: 0xb8e0d0,
  meeting: 0xe0c8e0,
  testing: 0xa7b0ba,
  lounge: 0xc8e0b0,
  corridor: 0xcfcfcf,
  entrance: 0xa0a0a0,
};
const WALK_TILES_PER_SECOND = 2.5;
/** 픽셀 하나를 화면 2px로 그린다. */
const PIXEL = 2;

interface Actor {
  container: Phaser.GameObjects.Container;
  sprite: Phaser.GameObjects.Image;
  shirt: number;
  label: Phaser.GameObjects.Text;
  marker: Phaser.GameObjects.Text;
  position: Point;
  target: Point;
  /** 목적지까지 남은 칸(가구·벽 회피). */
  path: Point[];
  agent: AgentState;
  displayed?: DisplayedZone;
  leaving: boolean;
  status: Phaser.GameObjects.Text;
}

const REPLAN_INTERVAL_MS = 250;

export class OfficeScene extends Phaser.Scene {
  private map: OfficeMap;
  private scenery: Phaser.GameObjects.GameObject[] = [];
  private readonly actors = new Map<string, Actor>();
  private state: OfficeState;
  private seats = new Map<string, Point>();
  private sinceReplan = 0;
  private camera: CameraState = initialCamera;
  /** 자동 초점 상태가 바뀌면 화면(React)에 알린다. */
  onCameraChange: (state: CameraState) => void = () => undefined;
  /** 캐릭터를 클릭하면 상세 패널(#51)을 연다. */
  onSelect: (agentId: string) => void = () => undefined;

  constructor(map: OfficeMap, initial: OfficeState) {
    super("office");
    this.map = map;
    this.state = initial;
  }

  preload(): void {
    for (const name of artImages()) {
      this.load.image(artKey(name), `${ART_BASE_URL}${name}.png`);
    }
  }

  create(): void {
    this.drawScenery();
    const center = gridToScreen({ x: this.map.width / 2, y: this.map.height / 2 });
    this.cameras.main.centerOn(center.sx, center.sy);
    this.cameras.main.setZoom(1);
    this.bindCameraInput();
    this.applyState(this.state);
  }

  /** 자동 초점을 다시 켠다(버튼·F 키). */
  resumeFocus(): void {
    this.setCamera(resumeAutoFocus(this.camera));
  }

  private setCamera(next: CameraState): void {
    const changed = next.autoFocus !== this.camera.autoFocus;
    this.camera = next;
    this.cameras.main.setZoom(next.zoom);
    if (changed) {
      this.onCameraChange(next);
    }
  }

  private bindCameraInput(): void {
    this.input.on("pointermove", (pointer: Phaser.Input.Pointer) => {
      if (!pointer.isDown) {
        return;
      }
      const camera = this.cameras.main;
      camera.scrollX -= (pointer.x - pointer.prevPosition.x) / camera.zoom;
      camera.scrollY -= (pointer.y - pointer.prevPosition.y) / camera.zoom;
      this.setCamera(onManualControl(this.camera));
    });
    this.input.on(
      "wheel",
      (_pointer: Phaser.Input.Pointer, _objects: unknown, _dx: number, deltaY: number) => {
        this.setCamera(zoomBy(this.camera, deltaY > 0 ? 0.9 : 1.1));
      },
    );
    this.input.keyboard?.on("keydown", (event: KeyboardEvent) => {
      const camera = this.cameras.main;
      const step = 32 / camera.zoom;
      const moves: Record<string, [number, number]> = {
        ArrowLeft: [-step, 0],
        ArrowRight: [step, 0],
        ArrowUp: [0, -step],
        ArrowDown: [0, step],
      };
      const move = moves[event.key];
      if (move) {
        camera.scrollX += move[0];
        camera.scrollY += move[1];
        this.setCamera(onManualControl(this.camera));
      } else if (event.key === "f" || event.key === "F") {
        this.resumeFocus();
      } else if (event.key === "+" || event.key === "-") {
        this.setCamera(zoomBy(this.camera, event.key === "+" ? 1.1 : 0.9));
      }
    });
  }

  /** 업무석이 늘어난 지도로 바닥만 다시 그린다. 캐릭터는 그대로 두고 새 목적지로 걷는다. */
  setMap(map: OfficeMap): void {
    this.map = map;
    if (!this.sys.isActive()) {
      return;
    }
    for (const object of this.scenery) {
      object.destroy();
    }
    this.scenery = [];
    this.drawScenery();
    this.applyState(this.state);
  }

  get rows(): number {
    return this.map.seats.length / 7;
  }

  private drawScenery(): void {
    this.drawFloor();
    this.drawWalls();
    this.drawFurniture();
    this.drawZoneLabels();
  }

  /** 새 사실 상태를 받는다. 목적지만 바꾸고, 현재 위치에서 자연스럽게 다시 걷는다. */
  applyState(state: OfficeState): void {
    this.state = state;
    if (!this.sys.isActive()) {
      return;
    }
    this.seats = assignSeats(this.map, state);
    const alive = new Set<string>();
    for (const agent of state.agents) {
      alive.add(agent.id);
      const actor = this.actors.get(agent.id) ?? this.spawn(agent);
      actor.agent = agent;
      const session = state.sessions.find((s) => s.id === agent.session_id);
      const name = labelFor(agent, state.devices.find((d) => d.id === session?.device_id));
      if (actor.label.text !== name) {
        actor.label.setText(name);
      }
      this.plan(actor, Date.now());
    }
    for (const [id, actor] of this.actors) {
      if (!alive.has(id) && !actor.leaving) {
        // 상태에서 빠진 캐릭터도 순간 사라지지 않고 퇴근한다.
        this.startLeaving(actor);
      }
    }
  }

  /** 폭주 병합을 거친 표시 구역으로 목적지를 정하고, 추정 상태를 구분해 표시한다. */
  private startLeaving(actor: Actor): void {
    actor.leaving = true;
    const here = { x: Math.round(actor.position.x), y: Math.round(actor.position.y) };
    actor.target = this.map.entrance;
    actor.path = findPath(this.map, here, this.map.entrance);
    actor.status.setVisible(false);
    actor.marker.setVisible(false);
  }

  private plan(actor: Actor, now: number): void {
    if (actor.leaving) {
      return;
    }
    const view = presenceOf(actor.agent, this.state);
    if (view.presence === "leaving") {
      this.startLeaving(actor);
      return;
    }
    actor.container.setAlpha(view.alpha);
    actor.status.setVisible(view.badge !== null);
    if (view.badge) {
      actor.status.setText(BADGE_LABELS[view.badge]);
    }
    actor.displayed = nextDisplayedZone(actor.displayed, actor.agent, now);
    // 유휴면 휴게실 안에서 가끔 자리를 옮긴다(생활 연출).
    const variant = actor.displayed.zone === "lounge" ? idleCycle(now, actor.agent.id) : 0;
    const target = targetTile(this.map, actor.agent, this.seats, actor.displayed.zone, variant);
    if (target.x !== actor.target.x || target.y !== actor.target.y || actor.path.length === 0) {
      // 이동 중에 목적지가 바뀌어도 지금 서 있는 칸에서 다시 경로를 찾는다.
      const here = { x: Math.round(actor.position.x), y: Math.round(actor.position.y) };
      actor.path = findPath(this.map, here, target);
      actor.target = target;
    }
    const inferred = actor.agent.confidence === "inferred";
    actor.marker.setVisible(inferred || actor.agent.turn_state === "WAITING_USER");
    actor.marker.setText(actor.agent.turn_state === "WAITING_USER" ? "!" : "?");
  }

  update(time: number, deltaMs: number): void {
    this.sinceReplan += deltaMs;
    if (this.sinceReplan >= REPLAN_INTERVAL_MS) {
      // 체류 시간 때문에 미뤄 둔 목적지 변경을 다시 확인한다.
      this.sinceReplan = 0;
      const now = Date.now();
      for (const actor of this.actors.values()) {
        this.plan(actor, now);
      }
    }
    const step = (WALK_TILES_PER_SECOND * deltaMs) / 1000;
    const active: Point[] = [];
    for (const actor of this.actors.values()) {
      let budget = step;
      while (budget > 0 && actor.path.length > 0) {
        const waypoint = actor.path[0]!;
        const dx = waypoint.x - actor.position.x;
        const dy = waypoint.y - actor.position.y;
        const distance = Math.hypot(dx, dy);
        if (distance <= budget) {
          actor.position = { ...waypoint };
          actor.path.shift();
          budget -= distance;
        } else {
          actor.position = {
            x: actor.position.x + (dx / distance) * budget,
            y: actor.position.y + (dy / distance) * budget,
          };
          budget = 0;
        }
      }
      if (actor.agent.turn_state !== "IDLE") {
        active.push(actor.position);
      }
      const key = this.texture(actor.agent.role, actor.shirt, walkFrame(actor.path.length > 0, time));
      if (actor.sprite.texture.key !== key) {
        actor.sprite.setTexture(key);
      }
      const screen = gridToScreen(actor.position);
      actor.container.setPosition(screen.sx, screen.sy - TILE_HEIGHT / 4);
      actor.container.setDepth(depthOf(actor.position, 5));
      if (actor.leaving && actor.path.length === 0) {
        // 출입구에 도착하면 퇴근 완료. 책상은 서버가 퇴근 연출 시간 후 반납한다.
        actor.container.destroy();
        this.actors.delete(actor.agent.id);
      }
    }
    if (this.camera.autoFocus) {
      // 활동 중인 캐릭터들의 중심을 부드럽게 따라간다.
      const focus = gridToScreen(
        focusPoint(active, { x: this.map.width / 2, y: this.map.height / 2 }),
      );
      const camera = this.cameras.main;
      const smoothing = Math.min(1, deltaMs / 400);
      camera.centerOn(
        camera.midPoint.x + (focus.sx - camera.midPoint.x) * smoothing,
        camera.midPoint.y + (focus.sy - camera.midPoint.y) * smoothing,
      );
    }
  }

  /** 역할·옷 색·프레임별 텍스처를 처음 필요할 때 한 번만 만든다. */
  private texture(role: AgentState["role"], shirt: number, frame: Frame): string {
    const key = textureKey(role, shirt, frame);
    if (!this.textures.exists(key)) {
      const rows = spriteRows(role, frame);
      const graphics = this.make.graphics({}, false);
      rows.forEach((row, y) => {
        Array.from(row).forEach((code, x) => {
          const color = pixelColor(code, shirt);
          if (color !== null) {
            graphics.fillStyle(color, 1).fillRect(x * PIXEL, y * PIXEL, PIXEL, PIXEL);
          }
        });
      });
      graphics.generateTexture(key, rows[0]!.length * PIXEL, rows.length * PIXEL);
      graphics.destroy();
    }
    return key;
  }

  private spawn(agent: AgentState): Actor {
    // 새 캐릭터는 출입구에서 출근한다.
    const start = { ...this.map.entrance };
    const container = this.add.container(0, 0);
    const session = this.state.sessions.find((s) => s.id === agent.session_id);
    const device = this.state.devices.find((d) => d.id === session?.device_id);
    const shirt = shirtIndex(session?.device_id ?? agent.id);
    const sprite = this.add.image(0, 0, this.texture(agent.role, shirt, 0)).setOrigin(0.5, 1);
    const top = -sprite.height;
    // "?"는 추정 상태, "!"는 관측된 승인 대기.
    const marker = this.add
      .text(0, top - 8, "?", { fontFamily: "monospace", fontSize: "12px", color: "#ffffff" })
      .setOrigin(0.5)
      .setVisible(false);
    const status = this.add
      .text(0, top - 20, "", {
        fontFamily: "sans-serif",
        fontSize: "10px",
        color: "#ffffff",
        backgroundColor: "#00000088",
      })
      .setOrigin(0.5)
      .setVisible(false);
    const label = this.add
      .text(0, 2, labelFor(agent, device), {
        fontFamily: "sans-serif",
        fontSize: "9px",
        color: "#ffffff",
        backgroundColor: "#00000066",
      })
      .setOrigin(0.5, 0);
    container.add([sprite, label, marker, status]);
    sprite.setInteractive({ useHandCursor: true });
    sprite.on("pointerup", (pointer: Phaser.Input.Pointer) => {
      // 드래그(카메라 이동)로 끝난 클릭은 선택으로 보지 않는다.
      if (pointer.getDistance() < 6) {
        this.onSelect(agent.id);
      }
    });
    const actor: Actor = {
      container,
      sprite,
      shirt,
      label,
      marker,
      status,
      position: start,
      target: start,
      path: [],
      agent,
      leaving: false,
    };
    this.actors.set(agent.id, actor);
    return actor;
  }

  private drawFloor(): void {
    const graphics = this.add.graphics();
    graphics.setDepth(-1);
    this.scenery.push(graphics);
    for (const tile of this.map.tiles) {
      const points = tileDiamond(tile).map((p) => new Phaser.Math.Vector2(p.sx, p.sy));
      const color = tile.zone ? ZONE_COLORS[tile.zone] : 0x6a6a6a;
      graphics.fillStyle(tile.walkable || tile.furniture ? color : 0x5a5a5a, 1);
      graphics.fillPoints(points, true);
      graphics.lineStyle(1, 0x000000, 0.08);
      graphics.strokePoints(points, true);
    }
  }

  /** 칸 중심 기준으로 그림 하나를 놓는다. 그림을 못 불러왔으면 건너뛴다(화면은 계속 동작). */
  private placeArt(tile: Point, piece: ArtPiece): void {
    const key = artKey(piece.image);
    if (!this.textures.exists(key)) {
      return;
    }
    const at = piece.cell ? { x: tile.x + piece.cell.x, y: tile.y + piece.cell.y } : tile;
    const screen = gridToScreen(at);
    const image = this.add
      .image(screen.sx + piece.dx, screen.sy + piece.dy, key)
      .setOrigin(0.5, 1)
      .setScale(piece.scale ?? ART_SCALE)
      .setDepth(depthOf(at, piece.layer));
    if (piece.tint !== undefined) {
      image.setTint(piece.tint);
    }
    this.scenery.push(image);
  }

  private drawFurniture(): void {
    for (const tile of this.map.tiles) {
      if (tile.furniture) {
        for (const piece of FURNITURE_ART[tile.furniture]) {
          this.placeArt(tile, piece);
        }
      }
    }
    for (const seat of this.map.seats) {
      this.placeArt(seat, SEAT_ART);
    }
  }

  /** 뒤쪽 두 변(x = 0, y = 0)에 벽을 세운다. 출입구·복도 끝은 비운다. 벽은 칸의 바깥 변에 붙인다. */
  private drawWalls(): void {
    const open = (point: Point) => {
      const tile = this.map.tiles[point.y * this.map.width + point.x];
      return tile?.zone === "entrance" || tile?.zone === "corridor";
    };
    const put = (point: Point, name: string, originX: number, offsetX: number) => {
      const key = artKey(name);
      if (!this.textures.exists(key)) {
        return;
      }
      const screen = gridToScreen(point);
      const wall = this.add
        .image(screen.sx + offsetX, screen.sy + 2, key)
        .setOrigin(originX, 1)
        .setScale(ART_SCALE)
        .setDepth(depthOf(point, -5));
      this.scenery.push(wall);
    };
    for (let y = 0; y < this.map.height; y += 1) {
      if (!open({ x: 0, y })) {
        put({ x: 0, y }, y % WALL_ART.windowEvery === 1 ? WALL_ART.left.window : WALL_ART.left.plain, 0, -TILE_WIDTH / 2);
      }
    }
    for (let x = 0; x < this.map.width; x += 1) {
      if (!open({ x, y: 0 })) {
        put({ x, y: 0 }, x % WALL_ART.windowEvery === 1 ? WALL_ART.right.window : WALL_ART.right.plain, 1, TILE_WIDTH / 2);
      }
    }
  }

  private drawZoneLabels(): void {
    for (const [zone, rect] of Object.entries(this.map.zones) as [ZoneKind, typeof this.map.zones.desk][]) {
      if (zone === "corridor" || zone === "entrance") {
        continue;
      }
      const screen = gridToScreen({ x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 });
      const label = this.add
        .text(screen.sx, screen.sy, ZONE_LABELS[zone], {
          fontFamily: "sans-serif",
          fontSize: "11px",
          color: "#333333",
        })
        .setOrigin(0.5)
        .setAlpha(0.6)
        .setDepth(1000);
      this.scenery.push(label);
    }
  }
}

function artKey(name: string): string {
  return `art-${name}`;
}

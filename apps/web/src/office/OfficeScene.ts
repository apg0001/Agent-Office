/**
 * Phaser 사무실 장면. 사실 상태(OfficeState)를 받아 목적지를 정하고, 캐릭터는 매 프레임
 * 그쪽으로 걸어간다. 이동은 순수 연출이며 사실 상태를 바꾸지 않는다. 캐릭터는 코드로 그리고,
 * 가구·벽은 CC0 그림(furnitureArt.ts)을 쓴다.
 */

import Phaser from "phaser";

import { assignSeats, spreadTargets, subagentIndexOf, targetTile } from "./behavior";
import {
  type CameraState,
  focusPoint,
  initialCamera,
  onManualControl,
  resumeAutoFocus,
  zoomBy,
} from "./camera";
import { findPath } from "./pathfinding";
import { headlineFor, idleHeadline } from "./headline";
import { BADGE_LABELS, idleCycle, presenceOf } from "./presence";
import { type DisplayedZone, nextDisplayedZone } from "./smoothing";
import {
  type Frame,
  labelFor,
  pixelColor,
  type Look,
  lookFor,
  shirtForSession,
  spriteRows,
  textureKey,
  walkFrame,
} from "./sprites";
import { ART_BASE_URL, ART_SCALE, type ArtPiece, FURNITURE_ART, SEAT_ART, artImages } from "./furnitureArt";
import type { OfficeMap, Point, ZoneKind } from "./grid";
import { ZONE_COLORS, ZONE_LABELS } from "./grid";
import { type ScreenPoint, TILE_HEIGHT, depthOf, gridToScreen, tileDiamond } from "./iso";
import {
  PARTITION_HEIGHT,
  WALL_HEIGHT,
  partitionFront,
  partitionQuad,
  wallBase,
  wallQuad,
  wallSegments,
} from "./walls";
import type { AgentState, OfficeState, SessionState } from "./types";
import { sessionName, shortWorkspace } from "./workspace";

const WALK_TILES_PER_SECOND = 2.5;
/**
 * 지도 렌더 배율. #166에서 버퍼를 절반으로 그려 2배로 키웠더니 가구·벽·글자가 깨져(소유자 피드백)
 * 다시 1배로 그린다. 지도 글자는 웹 화면과 같은 Galmuri11을 원래 크기(12px)로 써서 또렷하게 한다.
 */
export const RENDER_SCALE = 1;
const VIEW_SCALE = 1 / RENDER_SCALE;
const MAP_FONT = "Galmuri11, monospace";
/** 픽셀 하나를 화면 2px로 그린다. */
const PIXEL = 2;

interface Actor {
  container: Phaser.GameObjects.Container;
  sprite: Phaser.GameObjects.Image;
  shirt: number;
  look: Look;
  label: Phaser.GameObjects.Text;
  /** 이름표 아래 작은 세션 이름(#181). 서버가 주지 않으면 숨긴다. */
  workspace: Phaser.GameObjects.Text;
  marker: Phaser.GameObjects.Text;
  position: Point;
  target: Point;
  /** 이번 계획에서 원하는 칸(겹침 조정 전)과 그 칸이 고정 자리인지. */
  want?: Point;
  wantFixed?: boolean;
  /** 목적지까지 남은 칸(가구·벽 회피). */
  path: Point[];
  agent: AgentState;
  displayed?: DisplayedZone;
  leaving: boolean;
  status: Phaser.GameObjects.Text;
  /** 말풍선 기본 높이(엇갈림 전). */
  statusY: number;
}

const REPLAN_INTERVAL_MS = 250;
/** 옆 칸 말풍선과 겹치지 않게 번갈아 올리는 높이(px). */
const STATUS_LIFT = 16;

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
  /** 선택한 캐릭터. 자동 초점이 이 캐릭터를 따라가고 발밑에 강조 고리를 그린다. */
  private focused: string | null = null;
  private ring?: Phaser.GameObjects.Graphics;

  constructor(map: OfficeMap, initial: OfficeState) {
    super("office");
    this.map = map;
    this.state = initial;
  }

  preload(): void {
    for (const name of artImages()) {
      this.load.image(artKey(name), `${ART_BASE_URL}${name}.png`);
    }
    // pixelArt 모드는 모든 텍스처를 nearest로 줄여 0.3배로 줄인 가구 그림이 깨진다. 가구 그림만 선형 필터.
    this.load.on("filecomplete", (key: string) => {
      if (key.startsWith("art-")) {
        this.textures.get(key).setFilter(Phaser.Textures.FilterMode.LINEAR);
      }
    });
  }

  create(): void {
    this.drawScenery();
    const center = gridToScreen({ x: this.map.width / 2, y: this.map.height / 2 });
    this.cameras.main.centerOn(center.sx, center.sy);
    this.cameras.main.setZoom(VIEW_SCALE);
    this.bindCameraInput();
    this.ring = this.add.graphics().setVisible(false);
    this.ring.fillStyle(0xffcd4a, 0.4).fillEllipse(0, 0, 44, 22);
    this.ring.lineStyle(3, 0xffcd4a, 1).strokeEllipse(0, 0, 44, 22);
    this.tweens.add({ targets: this.ring, alpha: 0.55, duration: 600, yoyo: true, repeat: -1 });
    this.applyState(this.state);
  }

  /** 캐릭터를 선택해 따라가고 강조한다(세션 목록·캐릭터 클릭). null이면 선택을 푼다. */
  focusAgent(agentId: string | null): void {
    this.focused = agentId;
    for (const actor of this.actors.values()) {
      actor.label.setColor(actor.agent.id === agentId ? "#ffcd4a" : "#ffffff");
    }
    if (agentId) {
      this.setCamera(resumeAutoFocus(this.camera));
    }
  }

  /** 자동 초점을 다시 켠다(버튼·F 키). */
  resumeFocus(): void {
    this.setCamera(resumeAutoFocus(this.camera));
  }

  private setCamera(next: CameraState): void {
    const changed = next.autoFocus !== this.camera.autoFocus;
    this.camera = next;
    this.cameras.main.setZoom(next.zoom * VIEW_SCALE);
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
    this.drawPartitions();
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
      this.showWorkspace(actor, agent, session);
      this.plan(actor, Date.now());
    }
    this.assignTargets();
    for (const [id, actor] of this.actors) {
      if (!alive.has(id) && !actor.leaving) {
        // 상태에서 빠진 캐릭터도 순간 사라지지 않고 퇴근한다.
        this.startLeaving(actor);
      }
    }
  }

  /**
   * 대장 캐릭터 이름표 아래 세션 이름(폴더 이름, 팀 화면은 보이는 프로젝트 이름, #197).
   * 검증한 값만 줄여서 그린다(Phaser 글자라 HTML로 해석되지 않음).
   */
  private showWorkspace(actor: Actor, agent: AgentState, session: SessionState | undefined): void {
    const name = agent.role === "lead" && session ? sessionName(session) : null;
    const text = name ? shortWorkspace(name) : "";
    if (actor.workspace.text !== text) {
      actor.workspace.setText(text);
    }
    actor.workspace.setVisible(text !== "");
  }

  /** 폭주 병합을 거친 표시 구역으로 목적지를 정한다. */
  private startLeaving(actor: Actor): void {
    actor.leaving = true;
    const here = { x: Math.round(actor.position.x), y: Math.round(actor.position.y) };
    actor.target = this.map.entrance;
    actor.path = findPath(this.map, here, this.map.entrance);
    actor.status.setVisible(false);
    actor.marker.setVisible(false);
  }

  /** 쉬기 시작한 때: 세션의 마지막 활동 시각(없으면 모름). */
  private idleSince(actor: Actor): number | null {
    const session = this.state.sessions.find((s) => s.id === actor.agent.session_id);
    const at = session?.last_event_at ? Date.parse(session.last_event_at) : Number.NaN;
    return Number.isNaN(at) ? null : at;
  }

  /** 옆(2칸 안)에 다른 쉬는 캐릭터가 서 있으면 수다를 떤다. 걷는 중이면 아니다. */
  private hasIdleNeighbor(actor: Actor): boolean {
    if (actor.path.length > 0) {
      return false;
    }
    for (const other of this.actors.values()) {
      if (
        other !== actor &&
        !other.leaving &&
        other.agent.turn_state === "IDLE" &&
        other.path.length === 0 &&
        Math.hypot(other.position.x - actor.position.x, other.position.y - actor.position.y) <= 2
      ) {
        return true;
      }
    }
    return false;
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
    // 머리 위 짧은 상태: 연결 끊김 같은 존재 배지가 우선, 아니면 지금 활동(#162).
    // 쉬는 중이면 소일거리·수다·졸기·잠 연출(#233).
    const headline = view.badge
      ? BADGE_LABELS[view.badge]
      : actor.agent.turn_state === "IDLE"
        ? idleHeadline(actor.agent.id, now, this.idleSince(actor), this.hasIdleNeighbor(actor))
        : headlineFor(actor.agent);
    if (actor.status.text !== headline) {
      actor.status.setText(headline);
    }
    actor.status.setVisible(true);
    actor.displayed = nextDisplayedZone(actor.displayed, actor.agent, now);
    // 유휴면 휴게실 안에서 가끔 자리를 옮긴다(생활 연출).
    const variant = actor.displayed.zone === "lounge" ? idleCycle(now, actor.agent.id) : 0;
    const target = targetTile(
      this.map,
      actor.agent,
      this.seats,
      actor.displayed.zone,
      variant,
      subagentIndexOf(this.state, actor.agent),
    );
    // 실제 목적지는 모든 캐릭터의 원하는 칸을 모아 겹치지 않게 나눈 뒤 정한다(assignTargets).
    actor.want = target;
    actor.wantFixed = actor.agent.role === "lead" && actor.displayed.zone === "desk" && this.seats.has(actor.agent.session_id);
    // 머리 위 표시는 관측된 승인 대기("!")만. 추정 여부는 상세 패널에서 보여 준다.
    actor.marker.setVisible(actor.agent.turn_state === "WAITING_USER");
  }

  /** 원하는 칸들을 겹치지 않게 나누고, 바뀐 캐릭터만 지금 칸에서 다시 경로를 찾는다(#194). */
  private assignTargets(): void {
    const planning = [...this.actors.values()].filter((a) => !a.leaving && a.want);
    const targets = spreadTargets(
      this.map,
      planning.map((a) => ({ id: a.agent.id, point: a.want!, fixed: a.wantFixed ?? false })),
    );
    for (const actor of planning) {
      const target = targets.get(actor.agent.id) ?? actor.want!;
      if (target.x !== actor.target.x || target.y !== actor.target.y || actor.path.length === 0) {
        // 이동 중에 목적지가 바뀌어도 지금 서 있는 칸에서 다시 경로를 찾는다.
        const here = { x: Math.round(actor.position.x), y: Math.round(actor.position.y) };
        actor.path = findPath(this.map, here, target);
        actor.target = target;
      }
    }
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
      this.assignTargets();
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
      const key = this.texture(actor.agent.role, actor.shirt, walkFrame(actor.path.length > 0, time), actor.look);
      if (actor.sprite.texture.key !== key) {
        actor.sprite.setTexture(key);
      }
      const screen = gridToScreen(actor.position);
      actor.container.setPosition(screen.sx, screen.sy - TILE_HEIGHT / 4);
      // 말풍선은 칸보다 넓어 옆 칸 캐릭터와 겹친다. 칸 위치에 따라 높이를 번갈아 엇갈린다(#194).
      const lift = (Math.round(actor.position.x) + Math.round(actor.position.y)) % 2 === 0 ? 0 : STATUS_LIFT;
      actor.status.setY(actor.statusY - lift);
      actor.container.setDepth(depthOf(actor.position, 5));
      if (actor.leaving && actor.path.length === 0) {
        // 출입구에 도착하면 퇴근 완료. 책상은 서버가 퇴근 연출 시간 후 반납한다.
        actor.container.destroy();
        this.actors.delete(actor.agent.id);
      }
    }
    const focused = this.focused ? this.actors.get(this.focused) : undefined;
    if (this.ring) {
      this.ring.setVisible(focused !== undefined);
      if (focused) {
        const feet = gridToScreen(focused.position);
        this.ring.setPosition(feet.sx, feet.sy - TILE_HEIGHT / 4);
        this.ring.setDepth(focused.container.depth - 0.001);
      }
    }
    if (this.camera.autoFocus) {
      // 선택한 캐릭터가 있으면 그 캐릭터를, 없으면 활동 중인 캐릭터들의 중심을 부드럽게 따라간다.
      const focus = gridToScreen(
        focused ? focused.position : focusPoint(active, { x: this.map.width / 2, y: this.map.height / 2 }),
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
  private texture(role: AgentState["role"], shirt: number, frame: Frame, look: Look): string {
    const key = textureKey(role, shirt, frame, look);
    if (!this.textures.exists(key)) {
      const rows = spriteRows(role, frame, look);
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
    const shirt = shirtForSession(session, agent.id);
    const look = lookFor(session?.provider);
    const sprite = this.add.image(0, 0, this.texture(agent.role, shirt, 0, look)).setOrigin(0.5, 1);
    const top = -sprite.height;
    // "!"는 관측된 승인 대기.
    const marker = this.add
      .text(0, top - 8, "!", { fontFamily: MAP_FONT, fontSize: "12px", color: "#ffcd4a" })
      .setOrigin(0.5)
      .setVisible(false);
    const status = this.add
      .text(0, top - 22, "", {
        fontFamily: MAP_FONT,
        fontSize: "12px",
        color: "#ffffff",
        backgroundColor: "#000000aa",
        padding: { x: 3, y: 1 },
      })
      .setOrigin(0.5)
      .setVisible(false);
    const label = this.add
      .text(0, 2, labelFor(agent, device), {
        fontFamily: MAP_FONT,
        fontSize: "12px",
        color: "#ffffff",
        backgroundColor: "#00000066",
      })
      .setOrigin(0.5, 0);
    // 하위 에이전트는 작은 몸과 머리 위 상태로 구분한다. 이름표("하위")까지 달면 겹쳐 보인다.
    label.setVisible(agent.role !== "subagent");
    // 세션 이름은 이름표 바로 아래에 더 작고 흐리게. 길면 줄여 옆 캐릭터와 덜 겹치게 한다.
    const workspace = this.add
      .text(0, 17, "", {
        fontFamily: MAP_FONT,
        fontSize: "10px",
        color: "#c3c8de",
        backgroundColor: "#00000055",
      })
      .setOrigin(0.5, 0)
      .setVisible(false);
    for (const text of [label, workspace, marker, status]) {
      crisp(text);
    }
    container.add([sprite, label, workspace, marker, status]);
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
      look,
      label,
      workspace,
      marker,
      status,
      statusY: status.y,
      position: start,
      target: start,
      path: [],
      agent,
      leaving: false,
    };
    this.actors.set(agent.id, actor);
    this.showWorkspace(actor, agent, session);
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

  /** 뒤쪽 두 변에 바닥과 같은 2:1 투영으로 벽·창문을 그린다(walls.ts). 빛은 왼쪽 위에서 온다. */
  private drawWalls(): void {
    const graphics = this.add.graphics();
    graphics.setDepth(-0.5);
    this.scenery.push(graphics);
    const fill = (points: ScreenPoint[], color: number, alpha = 1) => {
      graphics.fillStyle(color, alpha);
      graphics.fillPoints(points.map((p) => new Phaser.Math.Vector2(p.sx, p.sy)), true);
    };
    for (const segment of wallSegments(this.map)) {
      const face = segment.side === "left" ? 0xf1e8d6 : 0xe0d4ba;
      if (segment.door) {
        // 출입문: 벽 위쪽만 남기고 문 구멍(어두운 바깥)·문틀을 그린다.
        fill(wallQuad(segment, 0, 0.12, 0, WALL_HEIGHT), face);
        fill(wallQuad(segment, 0.88, 1, 0, WALL_HEIGHT), face);
        fill(wallQuad(segment, 0, 1, 40, WALL_HEIGHT), face);
        fill(wallQuad(segment, 0.12, 0.88, 0, 40), 0x3a2e24);
        fill(wallQuad(segment, 0.1, 0.14, 0, 42), 0x8a5a32);
        fill(wallQuad(segment, 0.86, 0.9, 0, 42), 0x8a5a32);
        fill(wallQuad(segment, 0.1, 0.9, 40, 43), 0x8a5a32);
        fill(wallQuad(segment, 0.3, 0.7, 45, 52), 0x2f7d4f);
        fill(wallQuad(segment, 0, 1, WALL_HEIGHT - 4, WALL_HEIGHT), 0xd9a066);
        continue;
      }
      fill(wallQuad(segment, 0, 1, 0, WALL_HEIGHT), face);
      // 걸레받이와 위쪽 몰딩.
      fill(wallQuad(segment, 0, 1, 0, 5), 0xb98a5c);
      fill(wallQuad(segment, 0, 1, WALL_HEIGHT - 4, WALL_HEIGHT), 0xd9a066);
      if (segment.window) {
        fill(wallQuad(segment, 0.18, 0.82, 16, 44), 0x8a6a4a);
        fill(wallQuad(segment, 0.24, 0.76, 19, 41), segment.side === "left" ? 0xa9d3ef : 0x93c2e2);
        fill(wallQuad(segment, 0.49, 0.51, 19, 41), 0x8a6a4a);
      }
      // 이음매 선은 그리지 않는다. 위쪽 바깥 모서리만 살짝 어둡게.
      const [a, b] = wallBase(segment);
      graphics.lineStyle(1, 0x7a5a3a, 0.5);
      graphics.lineBetween(a.sx, a.sy - WALL_HEIGHT, b.sx, b.sy - WALL_HEIGHT);
    }
  }

  /** 구역 칸막이(#164). 앞쪽 칸 바로 뒤 깊이로 그려 뒤 칸 캐릭터는 가리고 앞 칸 캐릭터는 앞에 선다. */
  private drawPartitions(): void {
    for (const partition of this.map.partitions) {
      const front = partitionFront(partition);
      const graphics = this.add.graphics();
      graphics.setDepth(depthOf(front, -1));
      this.scenery.push(graphics);
      const fill = (points: ScreenPoint[], color: number, alpha = 1) => {
        graphics.fillStyle(color, alpha);
        graphics.fillPoints(points.map((p) => new Phaser.Math.Vector2(p.sx, p.sy)), true);
      };
      const horizontalEdge = partition.a.x !== partition.b.x;
      const face = horizontalEdge ? 0xe6dccb : 0xd6c9b1;
      if (partition.door) {
        // 문: 양쪽 문설주와 위 가로대만.
        fill(partitionQuad(partition, 0, 0.1, 0, PARTITION_HEIGHT + 6), 0x8a5a32);
        fill(partitionQuad(partition, 0.9, 1, 0, PARTITION_HEIGHT + 6), 0x8a5a32);
        fill(partitionQuad(partition, 0, 1, PARTITION_HEIGHT + 2, PARTITION_HEIGHT + 6), 0x8a5a32);
        continue;
      }
      fill(partitionQuad(partition, 0, 1, 0, PARTITION_HEIGHT), face);
      fill(partitionQuad(partition, 0, 1, 0, 4), 0xb98a5c);
      fill(partitionQuad(partition, 0, 1, PARTITION_HEIGHT - 3, PARTITION_HEIGHT), 0xc49060);
    }
    // 출입구 바닥 매트와 표지.
    const entrance = gridToScreen(this.map.entrance);
    const mat = this.add.graphics().setDepth(-0.9);
    mat.fillStyle(0x7a4a2a, 1);
    mat.fillPoints(
      tileDiamond(this.map.entrance).map(
        (p) => new Phaser.Math.Vector2(entrance.sx + (p.sx - entrance.sx) * 0.7, entrance.sy + (p.sy - entrance.sy) * 0.7),
      ),
      true,
    );
    this.scenery.push(mat);
    const sign = this.add
      .text(entrance.sx - 24, entrance.sy - 62, "🚪 출입구", {
        fontFamily: MAP_FONT,
        fontSize: "12px",
        color: "#ffffff",
        backgroundColor: "#2f7d4f",
        padding: { x: 3, y: 1 },
      })
      .setOrigin(0.5)
      .setDepth(1000);
    this.scenery.push(crisp(sign));
  }

  private drawZoneLabels(): void {
    for (const [zone, rect] of Object.entries(this.map.zones) as [ZoneKind, typeof this.map.zones.desk][]) {
      if (zone === "corridor" || zone === "entrance") {
        continue;
      }
      const screen = gridToScreen({ x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 });
      const label = this.add
        .text(screen.sx, screen.sy, ZONE_LABELS[zone], {
          fontFamily: MAP_FONT,
          fontSize: "12px",
          color: "#333333",
        })
        .setOrigin(0.5)
        .setAlpha(0.6)
        .setDepth(1000);
      this.scenery.push(crisp(label));
    }
  }
}

/** 지도 글자는 2배 해상도로 만들고 선형으로 줄여, 확대·축소해도 픽셀 모드(nearest)처럼 획이 빠지지 않게 한다. */
function crisp<T extends Phaser.GameObjects.Text>(text: T): T {
  text.setResolution(2);
  text.texture.setFilter(Phaser.Textures.FilterMode.LINEAR);
  return text;
}

function artKey(name: string): string {
  return `art-${name}`;
}

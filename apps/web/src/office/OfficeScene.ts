/**
 * Phaser 사무실 장면. 사실 상태(OfficeState)를 받아 목적지를 정하고, 캐릭터는 매 프레임
 * 그쪽으로 걸어간다. 이동은 순수 연출이며 사실 상태를 바꾸지 않는다. 외부 에셋 없이 그린다.
 */

import Phaser from "phaser";

import { assignSeats, targetTile } from "./behavior";
import type { Furniture, OfficeMap, Point, ZoneKind } from "./grid";
import { ZONE_LABELS } from "./grid";
import { TILE_HEIGHT, depthOf, gridToScreen, tileDiamond } from "./iso";
import type { AgentState, OfficeState } from "./types";

const ZONE_COLORS: Record<ZoneKind, number> = {
  desk: 0xd8c8a8,
  library: 0xb8c8e0,
  internet: 0xb8e0d0,
  meeting: 0xe0c8e0,
  testing: 0xe8d0b0,
  lounge: 0xc8e0b0,
  corridor: 0xcfcfcf,
  entrance: 0xa0a0a0,
};
const FURNITURE_COLORS: Record<Furniture, number> = {
  desk: 0x8b6b4a,
  shelf: 0x5a6e9a,
  terminal: 0x3a8a7a,
  whiteboard: 0xf2f2f2,
  rack: 0x7a5a3a,
  sofa: 0x6a9a5a,
  plant: 0x3f7f3f,
};
const WALK_TILES_PER_SECOND = 2.5;

interface Actor {
  container: Phaser.GameObjects.Container;
  position: Point;
  target: Point;
}

export class OfficeScene extends Phaser.Scene {
  private readonly map: OfficeMap;
  private readonly actors = new Map<string, Actor>();
  private state: OfficeState;

  constructor(map: OfficeMap, initial: OfficeState) {
    super("office");
    this.map = map;
    this.state = initial;
  }

  create(): void {
    this.drawFloor();
    this.drawFurniture();
    this.drawZoneLabels();
    const center = gridToScreen({ x: this.map.width / 2, y: this.map.height / 2 });
    this.cameras.main.centerOn(center.sx, center.sy);
    this.cameras.main.setZoom(1);
    this.applyState(this.state);
  }

  /** 새 사실 상태를 받는다. 목적지만 바꾸고, 현재 위치에서 자연스럽게 다시 걷는다. */
  applyState(state: OfficeState): void {
    this.state = state;
    if (!this.sys.isActive()) {
      return;
    }
    const seats = assignSeats(this.map, state);
    const alive = new Set<string>();
    for (const agent of state.agents) {
      alive.add(agent.id);
      const target = targetTile(this.map, agent, seats);
      const actor = this.actors.get(agent.id) ?? this.spawn(agent);
      actor.target = target;
    }
    for (const [id, actor] of this.actors) {
      if (!alive.has(id)) {
        actor.container.destroy();
        this.actors.delete(id);
      }
    }
  }

  update(_time: number, deltaMs: number): void {
    const step = (WALK_TILES_PER_SECOND * deltaMs) / 1000;
    for (const actor of this.actors.values()) {
      const dx = actor.target.x - actor.position.x;
      const dy = actor.target.y - actor.position.y;
      const distance = Math.hypot(dx, dy);
      if (distance > 0.001) {
        // 장애물 회피 경로는 #49. 지금은 목적지로 곧게 이동한다.
        const ratio = Math.min(1, step / distance);
        actor.position = { x: actor.position.x + dx * ratio, y: actor.position.y + dy * ratio };
      }
      const screen = gridToScreen(actor.position);
      actor.container.setPosition(screen.sx, screen.sy - TILE_HEIGHT / 4);
      actor.container.setDepth(depthOf(actor.position, 5));
    }
  }

  private spawn(agent: AgentState): Actor {
    // 새 캐릭터는 출입구에서 출근한다.
    const start = { ...this.map.entrance };
    const container = this.add.container(0, 0);
    const lead = agent.role === "lead";
    const body = this.add.rectangle(0, -10, 12, 16, lead ? 0x2f4f8f : 0x8f6f2f);
    const head = this.add.rectangle(0, -22, 10, 10, 0xf0c8a0);
    const badge = this.add.rectangle(0, -10, 4, 4, lead ? 0xf0d040 : 0xffffff);
    container.add([body, head, badge]);
    const actor = { container, position: start, target: start };
    this.actors.set(agent.id, actor);
    return actor;
  }

  private drawFloor(): void {
    const graphics = this.add.graphics();
    graphics.setDepth(-1);
    for (const tile of this.map.tiles) {
      const points = tileDiamond(tile).map((p) => new Phaser.Math.Vector2(p.sx, p.sy));
      const color = tile.zone ? ZONE_COLORS[tile.zone] : 0x6a6a6a;
      graphics.fillStyle(tile.walkable || tile.furniture ? color : 0x5a5a5a, 1);
      graphics.fillPoints(points, true);
      graphics.lineStyle(1, 0x000000, 0.08);
      graphics.strokePoints(points, true);
    }
  }

  private drawFurniture(): void {
    for (const tile of this.map.tiles) {
      if (!tile.furniture) {
        continue;
      }
      const screen = gridToScreen(tile);
      const block = this.add.rectangle(screen.sx, screen.sy - 8, 26, 16, FURNITURE_COLORS[tile.furniture]);
      block.setStrokeStyle(1, 0x000000, 0.3);
      block.setDepth(depthOf(tile, 2));
    }
  }

  private drawZoneLabels(): void {
    for (const [zone, rect] of Object.entries(this.map.zones) as [ZoneKind, typeof this.map.zones.desk][]) {
      if (zone === "corridor" || zone === "entrance") {
        continue;
      }
      const screen = gridToScreen({ x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 });
      this.add
        .text(screen.sx, screen.sy, ZONE_LABELS[zone], {
          fontFamily: "sans-serif",
          fontSize: "11px",
          color: "#333333",
        })
        .setOrigin(0.5)
        .setAlpha(0.6)
        .setDepth(1000);
    }
  }
}

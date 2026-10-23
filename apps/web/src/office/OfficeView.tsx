import { useEffect, useRef, useState } from "react";

import { createDefaultOffice } from "./grid";
import { connectLive, hasSession } from "./liveSource";
import { advanceSynthetic, createSyntheticOffice } from "./synthetic";
import type { DataSource } from "./types";

/**
 * 사무실 화면. 로그인 세션이 있으면 서버 실시간 상태를, 없으면 합성 데이터를 그린다.
 * Phaser는 브라우저에서만 지연 로드한다(테스트 환경에서 캔버스를 쓰지 않도록).
 */
export function OfficeView() {
  const host = useRef<HTMLDivElement>(null);
  const [source, setSource] = useState<DataSource>("synthetic");
  const [online, setOnline] = useState(false);

  useEffect(() => {
    const element = host.current;
    if (!element) {
      return undefined;
    }
    let disposed = false;
    let game: { destroy: (removeCanvas: boolean) => void } | undefined;
    let stop: (() => void) | undefined;
    void (async () => {
      const [{ default: Phaser }, { OfficeScene }, live] = await Promise.all([
        import("phaser"),
        import("./OfficeScene"),
        hasSession(),
      ]);
      if (disposed) {
        return;
      }
      const initial = createSyntheticOffice({ sessions: 4, subagentsPerSession: 1 });
      const scene = new OfficeScene(
        createDefaultOffice(2),
        live ? { source: "server", devices: [], sessions: [], agents: [] } : initial,
      );
      game = new Phaser.Game({
        type: Phaser.AUTO,
        parent: element,
        width: element.clientWidth || 960,
        height: 560,
        backgroundColor: "#2b2d31",
        pixelArt: true,
        scene,
      });
      if (live) {
        setSource("server");
        const handle = connectLive((state) => scene.applyState(state), setOnline);
        stop = handle.close;
        return;
      }
      let tick = 0;
      let current = initial;
      const timer = window.setInterval(() => {
        tick += 1;
        current = advanceSynthetic(current, tick);
        scene.applyState(current);
      }, 2500);
      stop = () => window.clearInterval(timer);
    })();
    return () => {
      disposed = true;
      stop?.();
      game?.destroy(true);
    };
  }, []);

  return (
    <section className="office" aria-label="사무실">
      {source === "synthetic" ? (
        <p className="synthetic-badge" role="status">
          합성 데이터(데모) — 실제 관측이 아닙니다
        </p>
      ) : (
        <p className="live-badge" role="status">
          {online ? "실시간 연결됨" : "서버 연결 끊김 — 재접속 중"}
        </p>
      )}
      <div ref={host} className="office-canvas" />
    </section>
  );
}

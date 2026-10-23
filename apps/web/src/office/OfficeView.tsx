import { useEffect, useRef, useState } from "react";

import { createDefaultOffice } from "./grid";
import { advanceSynthetic, createSyntheticOffice } from "./synthetic";
import type { OfficeState } from "./types";

/**
 * 사무실 화면. Phaser는 브라우저에서만 지연 로드한다(테스트 환경에서 캔버스를 쓰지 않도록).
 * 지금은 합성 데이터만 표시한다. 서버 실시간 연결은 #47에서 붙인다.
 */
export function OfficeView() {
  const host = useRef<HTMLDivElement>(null);
  const [state] = useState<OfficeState>(() =>
    createSyntheticOffice({ sessions: 4, subagentsPerSession: 1 }),
  );

  useEffect(() => {
    const element = host.current;
    if (!element) {
      return undefined;
    }
    let disposed = false;
    let game: { destroy: (removeCanvas: boolean) => void } | undefined;
    let timer: number | undefined;
    void (async () => {
      const [{ default: Phaser }, { OfficeScene }] = await Promise.all([
        import("phaser"),
        import("./OfficeScene"),
      ]);
      if (disposed) {
        return;
      }
      const map = createDefaultOffice(2);
      const scene = new OfficeScene(map, state);
      game = new Phaser.Game({
        type: Phaser.AUTO,
        parent: element,
        width: element.clientWidth || 960,
        height: 560,
        backgroundColor: "#2b2d31",
        pixelArt: true,
        scene,
      });
      let tick = 0;
      let current = state;
      timer = window.setInterval(() => {
        tick += 1;
        current = advanceSynthetic(current, tick);
        scene.applyState(current);
      }, 2500);
    })();
    return () => {
      disposed = true;
      if (timer !== undefined) {
        window.clearInterval(timer);
      }
      game?.destroy(true);
    };
  }, [state]);

  return (
    <section className="office" aria-label="사무실">
      {state.source === "synthetic" && (
        <p className="synthetic-badge" role="status">
          합성 데이터(데모) — 실제 관측이 아닙니다
        </p>
      )}
      <div ref={host} className="office-canvas" />
    </section>
  );
}

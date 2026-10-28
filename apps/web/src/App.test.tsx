import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("exports the application component", () => {
    expect(App).toBeTypeOf("function");
  });

  it("labels synthetic office data so it is never mistaken for observation", () => {
    const html = renderToString(<App />);
    expect(html).toContain("합성 데이터(데모)");
    expect(html).toContain("실제 관측이 아닙니다");
  });

  it("gives member pages the office top bar and a clear way back", () => {
    for (const path of ["/teams", "/devices", "/account", "/admin"]) {
      const html = renderToString(<App pathname={path} />);
      expect(html).toContain('class="topbar"');
      expect(html).toContain("사무실로 돌아가기");
    }
    // 로그인 전 화면은 상단 바 없이 그대로.
    expect(renderToString(<App pathname="/login" />)).not.toContain("사무실로 돌아가기");
  });
});

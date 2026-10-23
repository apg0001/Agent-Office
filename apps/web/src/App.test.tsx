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
});

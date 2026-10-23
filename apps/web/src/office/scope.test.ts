import { describe, expect, it } from "vitest";

import { detailPath } from "./detail";
import { liveUrl, scopeFromSearch } from "./liveSource";

const TEAM = "6f9c2b1e-4d0a-4c3b-9e7f-2a1d8c4b6e50";

describe("office scope", () => {
  it("reads only a team UUID from the query", () => {
    expect(scopeFromSearch(`?team=${TEAM}`)).toBe(TEAM);
    expect(scopeFromSearch("")).toBeNull();
    expect(scopeFromSearch("?team=../../etc")).toBeNull();
  });

  it("builds live and detail URLs per scope", () => {
    expect(liveUrl("https:", "office.example", null)).toBe("wss://office.example/api/v1/live");
    expect(liveUrl("http:", "localhost:5173", TEAM)).toBe(`ws://localhost:5173/api/v1/live?team=${TEAM}`);
    expect(detailPath("a1")).toBe("/api/v1/office/agents/a1");
    expect(detailPath("a1", TEAM)).toBe(`/api/v1/teams/${TEAM}/office/agents/a1`);
  });
});

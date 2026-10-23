import { describe, expect, it } from "vitest";

import { detailPath } from "./detail";
import { liveUrl, scopeFromSearch, searchFor } from "./liveSource";

const TEAM = "6f9c2b1e-4d0a-4c3b-9e7f-2a1d8c4b6e50";

describe("office scope", () => {
  it("reads only a team UUID from the query", () => {
    expect(scopeFromSearch(`?team=${TEAM}`)).toEqual({ kind: "team", teamId: TEAM });
    expect(scopeFromSearch("")).toEqual({ kind: "personal" });
    expect(scopeFromSearch("?team=../../etc")).toEqual({ kind: "personal" });
    expect(scopeFromSearch("?view=combined")).toEqual({ kind: "combined" });
    expect(scopeFromSearch("?view=side")).toEqual({ kind: "side" });
    expect(scopeFromSearch("?view=evil")).toEqual({ kind: "personal" });
  });

  it("builds live and detail URLs per scope", () => {
    const team = { kind: "team", teamId: TEAM } as const;
    expect(liveUrl("https:", "office.example")).toBe("wss://office.example/api/v1/live");
    expect(liveUrl("http:", "localhost:5173", team)).toBe(`ws://localhost:5173/api/v1/live?team=${TEAM}`);
    expect(liveUrl("http:", "h", { kind: "combined" })).toBe("ws://h/api/v1/live?view=combined");
    expect(detailPath("a1")).toBe("/api/v1/office/agents/a1");
    expect(detailPath("a1", team)).toBe(`/api/v1/teams/${TEAM}/office/agents/a1`);
    expect(detailPath("a1", { kind: "combined" })).toBe("/api/v1/office/combined/agents/a1");
  });

  it("round-trips scopes through the query string", () => {
    for (const scope of [
      { kind: "personal" },
      { kind: "combined" },
      { kind: "side" },
      { kind: "team", teamId: TEAM },
    ] as const) {
      expect(scopeFromSearch(searchFor(scope))).toEqual(scope);
    }
  });
});

import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { call, messageFor, routeFor, tokenFromHash } from "./api";
import { nextStep, startStep } from "./wizard";

describe("routing", () => {
  it("maps paths to pages and falls back to the office", () => {
    expect(routeFor("/login")).toBe("login");
    expect(routeFor("/login/")).toBe("login");
    expect(routeFor("/setup")).toBe("setup");
    expect(routeFor("/invite")).toBe("invite");
    expect(routeFor("/verify-email")).toBe("verify");
    expect(routeFor("/reset-password")).toBe("reset");
    expect(routeFor("/")).toBe("office");
    expect(routeFor("/anything")).toBe("office");
  });

  it("renders account pages without the office canvas", () => {
    const login = renderToString(<App pathname="/login" />);
    expect(login).toContain("로그인");
    expect(login).toContain('type="password"');
    expect(login).not.toContain("합성 데이터");
    expect(renderToString(<App pathname="/signup" />)).toContain("10자 이상");
  });
});

describe("tokenFromHash", () => {
  it("reads only a well-formed token from the fragment", () => {
    expect(tokenFromHash("#token=abc_DEF-123")).toBe("abc_DEF-123");
    expect(tokenFromHash("#x=1&token=abc")).toBe("abc");
    expect(tokenFromHash("#token=<script>")).toBeNull();
    expect(tokenFromHash("")).toBeNull();
  });
});

describe("call", () => {
  const respond = (status: number, body: string) =>
    (async () => new Response(body, { status })) as unknown as typeof fetch;

  it("returns data on success and the server error code on failure", async () => {
    expect((await call("GET", "/x", undefined, undefined, respond(200, '{"a":1}'))).data).toEqual({ a: 1 });
    const failed = await call("POST", "/x", {}, "t", respond(401, '{"detail":"invalid_credentials"}'));
    expect(failed).toMatchObject({ ok: false, status: 401, error: "invalid_credentials" });
    expect((await call("GET", "/x", undefined, undefined, respond(502, "<html>"))).error).toBe("http_502");
    const offline = (async () => {
      throw new Error("down");
    }) as unknown as typeof fetch;
    expect((await call("GET", "/x", undefined, undefined, offline)).error).toBe("network_error");
  });

  it("sends JSON and the CSRF header", async () => {
    let seen: RequestInit | undefined;
    const spy = (async (_url: string, init: RequestInit) => {
      seen = init;
      return new Response("", { status: 204 });
    }) as unknown as typeof fetch;
    await call("PUT", "/x", { a: 1 }, "csrf-1", spy);
    expect(seen?.headers).toEqual({ "Content-Type": "application/json", "X-CSRF-Token": "csrf-1" });
    expect(seen?.body).toBe('{"a":1}');
    expect(seen?.credentials).toBe("same-origin");
  });

  it("maps codes to Korean messages", () => {
    expect(messageFor("invalid_credentials")).toContain("비밀번호");
    expect(messageFor("unknown_code")).toContain("unknown_code");
    expect(messageFor(null)).toBe("");
  });
});

describe("setup wizard steps", () => {
  const status = (completed: boolean, admin: boolean) => ({
    setup_completed: completed,
    admin_exists: admin,
    bootstrap_pending: false,
  });

  it("starts at the right step", () => {
    expect(startStep(status(true, true), null)).toBe("completed");
    expect(startStep(status(false, false), null)).toBe("admin");
    expect(startStep(status(false, true), null)).toBe("login");
    expect(startStep(status(false, true), "USER")).toBe("login");
    expect(startStep(status(false, true), "SUPER_ADMIN")).toBe("site");
  });

  it("walks the settings steps in order and stops at finish", () => {
    expect(nextStep("site")).toBe("policy");
    expect(nextStep("policy")).toBe("mail");
    expect(nextStep("mail")).toBe("finish");
    expect(nextStep("finish")).toBe("finish");
  });
});

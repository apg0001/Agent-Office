import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { sortUsers, statusLabel } from "./AdminUsers";

const user = (id: string, status: "pending" | "active" | "disabled", verified: boolean, created: string) => ({
  id,
  email: `${id}@example.com`,
  display_name: id,
  instance_role: "USER" as const,
  status,
  email_verified: verified,
  created_at: created,
});

describe("admin users", () => {
  it("labels approval states", () => {
    expect(statusLabel(user("a", "pending", true, ""))).toBe("승인 대기");
    expect(statusLabel(user("a", "pending", false, ""))).toBe("이메일 확인 전");
    expect(statusLabel(user("a", "disabled", true, ""))).toBe("비활성");
  });

  it("puts users awaiting approval first, then newest", () => {
    const sorted = sortUsers([
      user("old", "active", true, "2026-10-01"),
      user("new", "active", true, "2026-10-20"),
      user("wait", "pending", true, "2026-09-01"),
      user("unverified", "pending", false, "2026-10-21"),
    ]);
    expect(sorted.map((u) => u.id)).toEqual(["wait", "unverified", "new", "old"]);
  });

  it("adds a hidden honeypot field to signup", () => {
    const html = renderToString(<App pathname="/signup" />);
    expect(html).toContain('name="website"');
    expect(html).toContain('aria-hidden="true"');
  });
});

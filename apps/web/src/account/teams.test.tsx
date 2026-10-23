import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { routeFor } from "./api";
import { canRemove, teamPermissions } from "./teams";

describe("team permissions", () => {
  it("matches the server role rules", () => {
    const owner = teamPermissions("OWNER");
    const manager = teamPermissions("MANAGER");
    const member = teamPermissions("MEMBER");

    expect(owner).toMatchObject({ changeRoles: true, transfer: true, deleteTeam: true, leave: false });
    expect(owner.inviteRoles).toEqual(["MEMBER", "MANAGER"]);
    expect(manager).toMatchObject({ edit: true, invite: true, changeRoles: false, deleteTeam: false, leave: true });
    expect(manager.inviteRoles).toEqual(["MEMBER"]);
    expect(member).toMatchObject({ invite: false, manageInvitations: false, leave: true });
  });

  it("only lets owners and managers remove lower roles", () => {
    expect(canRemove("OWNER", "MANAGER")).toBe(true);
    expect(canRemove("OWNER", "OWNER")).toBe(false);
    expect(canRemove("MANAGER", "MEMBER")).toBe(true);
    expect(canRemove("MANAGER", "MANAGER")).toBe(false);
    expect(canRemove("MEMBER", "MEMBER")).toBe(false);
  });

  it("routes /teams to the team page", () => {
    expect(routeFor("/teams")).toBe("teams");
    expect(renderToString(<App pathname="/teams" />)).toContain("팀");
  });
});

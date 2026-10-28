import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { SessionList } from "../office/SessionList";
import { createSyntheticOffice } from "../office/synthetic";
import { UNNAMED_SESSION, projectName, sessionName } from "../office/workspace";
import { ShareConsent } from "./ShareConsent";
import { SHARE_MODES, SHARE_MODE_LABELS, SHARE_VISIBLE_TEXT, shareModeFromConsent } from "./teams";

describe("team share modes (#197)", () => {
  it("sends 'all' when the pre-checked consent stays checked", () => {
    expect(shareModeFromConsent(true)).toBe("all");
    expect(shareModeFromConsent(false)).toBe("selected");
    expect(SHARE_MODES).toEqual(["all", "selected", "none"]);
    expect(SHARE_MODE_LABELS.all).toBe("모두 공유");
  });

  it("explains what teammates can and cannot see next to the checked box", () => {
    const html = renderToString(<ShareConsent checked onChange={() => undefined} />);
    expect(html).toContain('checked=""');
    expect(html).toContain("모두");
    expect(html).toContain("상태·활동 종류·표시 이름·프로젝트 이름");
    expect(html).toContain("대화·코드·경로·폴더 전체 경로는 보이지 않습니다");
    expect(SHARE_VISIBLE_TEXT).toContain("팀원에게 보이는 것");
    const off = renderToString(<ShareConsent checked={false} onChange={() => undefined} />);
    expect(off).not.toContain('checked=""');
    expect(off).toContain("선택한 프로젝트만");
  });
});

describe("session names on team views (#197)", () => {
  it("prefers the folder name, then the visible project name, then '(이름 없음)' on team views", () => {
    expect(sessionName({ workspace_name: "blog", project_name: "Blog" }, true)).toBe("blog");
    expect(sessionName({ workspace_name: null, project_name: "결제 서비스" }, true)).toBe("결제 서비스");
    expect(sessionName({ workspace_name: null, project_name: null }, true)).toBe(UNNAMED_SESSION);
    expect(sessionName({ workspace_name: null, project_name: null })).toBeNull();
    expect(sessionName({}, true)).toBe(UNNAMED_SESSION);
  });

  it("drops empty, overlong or control-character project names", () => {
    expect(projectName("  팀 프로젝트 ")).toBe("팀 프로젝트");
    for (const bad of [undefined, null, 1, "", "   ", "a\nb", "a\u202eb", "가".repeat(121)]) {
      expect(projectName(bad)).toBeNull();
    }
  });

  it("shows unnamed sessions only on team and combined lists", () => {
    const office = createSyntheticOffice({ sessions: 1, subagentsPerSession: 0 });
    office.sessions[0] = { ...office.sessions[0]!, workspace_name: null, project_name: null };
    const team = renderToString(<SessionList state={office} onSelect={() => undefined} unnamed />);
    expect(team).toContain(UNNAMED_SESSION);
    const personal = renderToString(<SessionList state={office} onSelect={() => undefined} />);
    expect(personal).not.toContain(UNNAMED_SESSION);

    office.sessions[0] = { ...office.sessions[0]!, project_name: "<b>p" };
    const named = renderToString(<SessionList state={office} onSelect={() => undefined} unnamed />);
    expect(named).toContain("&lt;b&gt;p");
    expect(named).not.toContain("<b>p");
  });
});

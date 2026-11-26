import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { type ApiResult, messageFor } from "./api";
import { type Act, DeviceNameShare, DeviceNameShareBox, type TeamSummary, patchShareDeviceName } from "./TeamsPage";
import {
  DEVICE_NAME_SHARE_HELP,
  DEVICE_NAME_SHARE_IDLE,
  DEVICE_NAME_SHARE_LABEL,
  DEVICE_NAME_SHARE_NONE_NOTE,
  type DeviceNameShareView,
  deviceNameShareShown,
  loadUnlessChanged,
  settleDeviceNameShare,
  supportsDeviceNameShare,
  toggleShareDeviceName,
} from "./teams";

const TEAM: TeamSummary = { id: "t1", name: "플랫폼", description: null, role: "MEMBER", share_mode: "all", share_device_name: false };
const passThrough: Act = async (action) => (await action()).ok;
const render = (team: TeamSummary) =>
  renderToStaticMarkup(<DeviceNameShare team={team} csrf="csrf" act={passThrough} onSaved={() => undefined} />);
const box = (team: TeamSummary, view: DeviceNameShareView) =>
  renderToStaticMarkup(
    <DeviceNameShareBox
      team={team}
      checked={deviceNameShareShown(team.share_device_name === true, view)}
      saving={view.pending !== null}
      onToggle={() => undefined}
    />,
  );
const result = (ok: boolean, error: string | null, data: unknown = null): ApiResult<unknown> => ({
  ok,
  status: ok ? 200 : 403,
  data,
  error,
});

describe("team 'show my PC name' toggle", () => {
  it("is off by default and explains what teammates see", () => {
    const html = render(TEAM);
    expect(html).toContain('type="checkbox"');
    expect(html).not.toContain('checked=""');
    expect(html).not.toContain('disabled=""');
    expect(html).toContain(DEVICE_NAME_SHARE_LABEL);
    expect(DEVICE_NAME_SHARE_LABEL).toBe("이 팀에 내 PC 이름 보이기");
    expect(DEVICE_NAME_SHARE_HELP).toContain("이름 · PC 이름");
    expect(DEVICE_NAME_SHARE_HELP).toContain("끄면 이름만");
    // 안내 문구는 체크 상자와 aria-describedby로 이어진다.
    expect(html).toContain('aria-describedby="device-name-share-help-t1"');
    expect(html).toContain('id="device-name-share-help-t1"');
    expect(html).not.toContain(DEVICE_NAME_SHARE_NONE_NOTE);
  });

  it("is checked when the server says it is on", () => {
    expect(render({ ...TEAM, share_device_name: true })).toContain('checked=""');
  });

  it("notes that nothing is visible while the share mode is 'none'", () => {
    // 마크업에서는 작은따옴표가 &#x27;로 이스케이프된다.
    expect(render({ ...TEAM, share_mode: "none" }).replaceAll("&#x27;", "'")).toContain(DEVICE_NAME_SHARE_NONE_NOTE);
  });

  it("is not drawn when an older server omits the field", () => {
    const old: TeamSummary = { id: "t1", name: "플랫폼", description: null, role: "MEMBER", share_mode: "all" };
    expect(supportsDeviceNameShare(old)).toBe(false);
    expect(supportsDeviceNameShare({ share_device_name: null })).toBe(false);
    expect(supportsDeviceNameShare(TEAM)).toBe(true);
    expect(render(old)).toBe("");
  });

  it("is disabled and shows the new value while saving", () => {
    const html = box(TEAM, { pending: true, saved: null });
    expect(html).toContain('checked=""');
    expect(html).toContain('disabled=""');
  });
});

describe("toggleShareDeviceName request", () => {
  it("sends only share_device_name to the share-mode endpoint", async () => {
    const calls: unknown[][] = [];
    const response = await toggleShareDeviceName({
      teamId: "t1",
      current: false,
      next: true,
      csrf: "csrf-token",
      setView: () => undefined,
      request: async (...args) => {
        calls.push(args);
        return result(true, null);
      },
    });
    expect(response.ok).toBe(true);
    expect(calls).toEqual([["PUT", "/api/v1/teams/t1/share-mode", { share_device_name: true }, "csrf-token"]]);
  });

  it("turns it off the same way", async () => {
    const bodies: unknown[] = [];
    await toggleShareDeviceName({
      teamId: "t1",
      current: true,
      next: false,
      csrf: "c",
      setView: () => undefined,
      request: async (_method, _path, body) => {
        bodies.push(body);
        return result(true, null);
      },
    });
    expect(bodies).toEqual([{ share_device_name: false }]);
  });
});

describe("checkbox value after saving", () => {
  /** 컴포넌트와 같은 순서로 상태를 굴린다: 저장 → (props가 올 때마다) settle → 표시 값. */
  const save = async (current: boolean, next: boolean, response: ApiResult<unknown>) => {
    const views: DeviceNameShareView[] = [];
    const saved: boolean[] = [];
    let view = DEVICE_NAME_SHARE_IDLE;
    const response_ = await toggleShareDeviceName({
      teamId: "t1",
      current,
      next,
      csrf: "c",
      setView: (value) => {
        view = value;
        views.push(value);
      },
      onSaved: (value) => saved.push(value),
      request: async () => response,
    });
    return { view, views, saved, response: response_ };
  };

  it("keeps the new value when the card is drawn again with the old props", async () => {
    const { view, views, saved } = await save(false, true, result(true, null, { share_device_name: true }));
    // 저장 중에는 새 값(낙관적), 끝나면 저장된 값.
    expect(views[0]).toEqual({ pending: true, saved: null });
    expect(view).toEqual({ pending: null, saved: { from: false, to: true } });
    expect(saved).toEqual([true]);
    // 목록이 아직 이전 응답(false)이어도 켜진 것으로 보인다. 저장 전 값으로 돌아가지 않는다.
    const stale = settleDeviceNameShare(false, view);
    expect(deviceNameShareShown(false, stale)).toBe(true);
    expect(box(TEAM, stale)).toContain('checked=""');
    expect(box(TEAM, stale)).not.toContain('disabled=""');
  });

  it("follows new props once they arrive", async () => {
    const { view } = await save(false, true, result(true, null, { share_device_name: true }));
    // 새 응답(true)이 오면 덮어쓰기를 버리고 props를 따른다.
    const fresh = settleDeviceNameShare(true, view);
    expect(fresh).toEqual(DEVICE_NAME_SHARE_IDLE);
    expect(deviceNameShareShown(true, fresh)).toBe(true);
    // 그 뒤 서버 값이 다시 꺼지면(다른 탭에서 끔) 그 값을 그대로 보여 준다.
    const later = settleDeviceNameShare(false, fresh);
    expect(deviceNameShareShown(false, later)).toBe(false);
    expect(box(TEAM, later)).not.toContain('checked=""');
  });

  it("uses the value the server returned, and falls back to the sent value", async () => {
    const refused = await save(false, true, result(true, null, { share_device_name: false }));
    expect(refused.saved).toEqual([false]);
    expect(deviceNameShareShown(false, refused.view)).toBe(false);
    const bare = await save(true, false, result(true, null, null));
    expect(bare.saved).toEqual([false]);
    expect(deviceNameShareShown(true, bare.view)).toBe(false);
  });

  it("patches only that team in the list so the card is not rebuilt from an old response", () => {
    const other: TeamSummary = { ...TEAM, id: "t2", share_device_name: false };
    const patched = patchShareDeviceName([TEAM, other], "t1", true);
    expect(patched.map((team) => team.share_device_name)).toEqual([true, false]);
    expect(patched[1]).toBe(other);
    expect(patched[0]).toMatchObject({ id: "t1", share_mode: "all", role: "MEMBER" });
    // 고친 목록으로 다시 그리면 덮어쓰기 없이도 켜져 있다.
    expect(render(patched[0]!)).toContain('checked=""');
  });
});

describe("team list responses that overlap a save", () => {
  it("uses a response when nothing was saved meanwhile", async () => {
    let loads = 0;
    const loaded = await loadUnlessChanged(
      () => 0,
      async () => {
        loads += 1;
        return "list";
      },
    );
    expect(loaded).toEqual({ fresh: true, value: "list" });
    expect(loads).toBe(1);
  });

  it("drops a response that started before a save finished and loads again", async () => {
    let epoch = 0;
    const responses = ["before-save", "after-save"];
    let loads = 0;
    const loaded = await loadUnlessChanged(
      () => epoch,
      async () => {
        const value = responses[loads]!;
        loads += 1;
        if (loads === 1) {
          // 첫 요청이 떠 있는 동안 PC 이름 공개 저장이 끝난다.
          epoch += 1;
        }
        return value;
      },
    );
    expect(loaded).toEqual({ fresh: true, value: "after-save" });
    expect(loads).toBe(2);
  });

  it("gives up after a few overlapping tries so the current list is kept", async () => {
    let epoch = 0;
    let loads = 0;
    const loaded = await loadUnlessChanged(
      () => epoch,
      async () => {
        loads += 1;
        epoch += 1;
        return "stale";
      },
    );
    expect(loaded).toEqual({ fresh: false });
    expect(loads).toBe(3);
  });
});

describe("checkbox value when saving fails", () => {
  const MESSAGES: [string, string][] = [
    ["forbidden", "권한이 없습니다."],
    ["network_error", "서버에 연결할 수 없습니다."],
    ["csrf_required", "세션이 바뀌었습니다. 새로고침 후 다시 시도하세요."],
    ["http_422", "요청을 처리하지 못했습니다(http_422)."],
  ];

  it("goes back to the previous value and shows the reason", async () => {
    for (const [code, message] of MESSAGES) {
      for (const current of [false, true]) {
        const views: DeviceNameShareView[] = [];
        const saved: boolean[] = [];
        let shownError: string | null = null;
        // TeamsPage의 act와 같은 처리: 실패하면 오류 문구를 화면 위 알림에 둔다.
        const act: Act = async (action) => {
          shownError = null;
          const outcome = await action();
          if (!outcome.ok) {
            shownError = messageFor(outcome.error);
          }
          return outcome.ok;
        };
        const ok = await act(() =>
          toggleShareDeviceName({
            teamId: "t1",
            current,
            next: !current,
            csrf: "c",
            setView: (value) => views.push(value),
            onSaved: (value) => saved.push(value),
            request: async () => result(false, code),
          }),
        );
        expect(ok).toBe(false);
        expect(shownError).toBe(message);
        // 먼저 새 값을 보였다가, 실패해서 덮어쓰기를 모두 지운다.
        expect(views).toEqual([{ pending: !current, saved: null }, DEVICE_NAME_SHARE_IDLE]);
        expect(saved).toEqual([]);
        const team = { ...TEAM, share_device_name: current };
        const html = box(team, views[views.length - 1]!);
        expect(html.includes('checked=""')).toBe(current);
        expect(html).not.toContain('disabled=""');
      }
    }
  });
});

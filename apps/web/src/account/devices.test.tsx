import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { messageFor, routeFor } from "./api";
import { type DeviceSummary, deviceStatus, remaining, sortDevices } from "./devices";

const device = (over: Partial<DeviceSummary>): DeviceSummary => ({
  id: "d",
  name: "PC",
  platform: "linux",
  collector_version: "0.1.0",
  created_at: "2026-10-24T00:00:00Z",
  last_seen_at: null,
  revoked_at: null,
  connection: "ONLINE",
  ...over,
});

describe("devices page (#41)", () => {
  it("routes /devices to the PC page", () => {
    expect(routeFor("/devices")).toBe("devices");
    expect(routeFor("/devices/")).toBe("devices");
    expect(renderToString(<App pathname="/devices" />)).toContain("내 PC");
  });

  it("shows revoked before connection state", () => {
    expect(deviceStatus(device({}))).toBe("연결됨");
    expect(deviceStatus(device({ connection: "DISCONNECTED" }))).toBe("연결 끊김");
    expect(deviceStatus(device({ revoked_at: "2026-10-24T01:00:00Z" }))).toBe("폐기됨");
  });

  it("counts down the one-time code and expires it", () => {
    const now = Date.parse("2026-10-24T00:00:00Z");
    expect(remaining("2026-10-24T00:10:00Z", now)).toBe("10:00");
    expect(remaining("2026-10-24T00:00:05Z", now)).toBe("0:05");
    expect(remaining("2026-10-24T00:00:00Z", now)).toBeNull();
    expect(remaining("not a date", now)).toBeNull();
  });

  it("lists active devices first, by name", () => {
    const sorted = sortDevices([
      device({ id: "1", name: "나", revoked_at: "2026-10-24T01:00:00Z" }),
      device({ id: "2", name: "다" }),
      device({ id: "3", name: "가" }),
    ]);
    expect(sorted.map((d) => d.id)).toEqual(["3", "2", "1"]);
  });

  it("explains the open-code limit", () => {
    expect(messageFor("too_many_open_codes")).toContain("최대 5개");
  });
});

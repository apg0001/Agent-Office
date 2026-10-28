import { describe, expect, it } from "vitest";

import { detectOs, pickDownloads } from "./CollectorDownloads";

const asset = (name: string) => ({ name, browser_download_url: `https://example.test/${name}` });

describe("collector downloads (#215)", () => {
  it("picks installers from the newest non-draft collector release", () => {
    const releases = [
      { tag_name: "server-v0.1.0-beta.3", html_url: "s", draft: false, assets: [] },
      { tag_name: "collector-v0.1.0-beta.3", html_url: "draft", draft: true, assets: [] },
      {
        tag_name: "collector-v0.1.0-beta.2",
        html_url: "page",
        draft: false,
        assets: [
          asset("agent-office-collector-setup-0.1.0-beta.2-windows-amd64.exe"),
          asset("agent-office-collector-0.1.0-beta.2-macos-universal.pkg"),
          asset("agent-office-collector_0.1.0.beta.2_amd64.deb"),
          asset("agent-office-collector-desktop_0.1.0.beta.2_amd64.deb"),
          asset("agent-office-collector_0.1.0.beta.2_arm64.deb"),
          asset("agent-office-collector-desktop_0.1.0.beta.2_arm64.deb"),
          asset("agent-office-collector-0.1.0-beta.2-windows-amd64.zip"),
        ],
      },
    ];
    const d = pickDownloads(releases)!;
    expect(d.version).toBe("0.1.0-beta.2");
    expect(d.windows?.name).toMatch(/setup.*\.exe$/);
    expect(d.macos?.name).toMatch(/\.pkg$/);
    expect(d.linux.map((l) => [l.arch, l.core?.name, l.desktop?.name])).toEqual([
      ["amd64", "agent-office-collector_0.1.0.beta.2_amd64.deb", "agent-office-collector-desktop_0.1.0.beta.2_amd64.deb"],
      ["arm64", "agent-office-collector_0.1.0.beta.2_arm64.deb", "agent-office-collector-desktop_0.1.0.beta.2_arm64.deb"],
    ]);
    expect(pickDownloads([])).toBeNull();
  });

  it("detects the visitor's OS for the default tab", () => {
    expect(detectOs("Mozilla/5.0 (Windows NT 10.0; Win64; x64)")).toBe("windows");
    expect(detectOs("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")).toBe("macos");
    expect(detectOs("Mozilla/5.0 (X11; Ubuntu; Linux x86_64)")).toBe("linux");
  });
});

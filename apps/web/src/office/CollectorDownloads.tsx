import { useEffect, useState } from "react";

/**
 * 메인 화면의 Collector 설치 안내(#215). 최신 Collector Release(태그 `collector-v…`, Beta 포함)의 설치 파일을
 * GitHub API로 찾아 OS별 링크와 설치 순서를 보여 준다. 실패하면 Release 목록 링크만 보인다.
 * 브라우저가 GitHub에 직접 요청한다(서버를 거치지 않음, 사용자 정보는 보내지 않음).
 */

const REPO = "apg0001/Agent-Office";
const RELEASES_URL = `https://github.com/${REPO}/releases`;
const API_URL = `https://api.github.com/repos/${REPO}/releases?per_page=20`;

export type OsTab = "windows" | "macos" | "linux";

interface Asset {
  name: string;
  browser_download_url: string;
}

interface Release {
  tag_name: string;
  html_url: string;
  draft: boolean;
  assets: Asset[];
}

export interface Downloads {
  version: string;
  page: string;
  windows?: Asset;
  macos?: Asset;
  linux: { arch: "amd64" | "arm64"; core?: Asset; desktop?: Asset }[];
}

/** Release 목록에서 가장 최근 Collector Release의 OS별 설치 파일을 고른다. */
export function pickDownloads(releases: Release[]): Downloads | null {
  const release = releases.find((r) => !r.draft && /^collector-v\d/.test(r.tag_name));
  if (!release) {
    return null;
  }
  const find = (re: RegExp) => release.assets.find((a) => re.test(a.name));
  return {
    version: release.tag_name.replace(/^collector-v/, ""),
    page: release.html_url,
    windows: find(/-setup-.*-windows-amd64\.exe$/),
    macos: find(/-macos-universal\.pkg$/),
    linux: (["amd64", "arm64"] as const).map((arch) => ({
      arch,
      core: find(new RegExp(`^agent-office-collector_[^_]+_${arch}\\.deb$`)),
      desktop: find(new RegExp(`^agent-office-collector-desktop_[^_]+_${arch}\\.deb$`)),
    })),
  };
}

export function detectOs(userAgent: string): OsTab {
  if (/Windows/i.test(userAgent)) return "windows";
  if (/Mac OS X|Macintosh/i.test(userAgent)) return "macos";
  return "linux";
}

const TABS: [OsTab, string][] = [
  ["windows", "Windows"],
  ["macos", "macOS"],
  ["linux", "Linux"],
];

function Link({ asset, label }: { asset?: Asset; label: string }) {
  if (!asset) {
    return <span className="hint">{label}: 이번 버전에 없음</span>;
  }
  return (
    <a className="download-link" href={asset.browser_download_url}>
      ⬇ {label} <span className="file">({asset.name})</span>
    </a>
  );
}

export function CollectorDownloads() {
  const [downloads, setDownloads] = useState<Downloads | null | undefined>(undefined);
  const [tab, setTab] = useState<OsTab>(() =>
    typeof navigator === "undefined" ? "windows" : detectOs(navigator.userAgent),
  );
  const [copied, setCopied] = useState(false);
  const server = typeof window === "undefined" ? "" : window.location.origin;

  useEffect(() => {
    let cancelled = false;
    fetch(API_URL, { headers: { Accept: "application/vnd.github+json" } })
      .then((response) => (response.ok ? response.json() : []))
      .then((releases: Release[]) => {
        if (!cancelled) setDownloads(pickDownloads(Array.isArray(releases) ? releases : []));
      })
      .catch(() => {
        if (!cancelled) setDownloads(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const copy = () => {
    void navigator.clipboard?.writeText(server).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  };

  const linux = downloads?.linux ?? [];
  return (
    <section className="collector-downloads" aria-label="Collector 설치">
      <h2>내 PC 연결하기(Collector 설치)</h2>
      <p>
        PC에서 Claude Code·Codex 활동을 이 사무실에 보이게 하려면 그 PC에 Collector를 설치합니다.
        {downloads ? ` 최신 버전 ${downloads.version}.` : ""}{" "}
        <a href={downloads?.page ?? RELEASES_URL}>전체 배포 파일</a>
      </p>
      <div className="os-tabs" role="tablist">
        {TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={tab === key ? "selected" : undefined}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </div>
      {downloads === undefined && <p className="hint">설치 파일을 찾는 중…</p>}
      {downloads === null && (
        <p className="hint">
          설치 파일 목록을 가져오지 못했습니다. <a href={RELEASES_URL}>배포 페이지</a>에서 받으세요.
        </p>
      )}
      {tab === "windows" && (
        <div className="os-guide">
          {downloads && <Link asset={downloads.windows} label="Windows 설치 프로그램(x64)" />}
          <ol>
            <li>받은 설치 프로그램을 실행합니다. 서명이 없어 SmartScreen이 막으면 <b>추가 정보 → 실행</b>을 누릅니다.</li>
            <li>설치가 끝나면 <b>처음 설정</b> 창이 뜹니다(관리자 권한 필요 없음).</li>
          </ol>
        </div>
      )}
      {tab === "macos" && (
        <div className="os-guide">
          {downloads && <Link asset={downloads.macos} label="macOS 설치 파일(Apple Silicon·Intel)" />}
          <ol>
            <li>
              받은 <code>.pkg</code>를 엽니다. "확인할 수 없음" 경고가 뜨면 <b>시스템 설정 → 개인정보 보호 및 보안</b> 맨
              아래에서 <b>그래도 열기</b>를 누릅니다.
            </li>
            <li>설치(관리자 암호 필요)가 끝나면 응용 프로그램의 <b>Agent Office Collector</b>를 열어 <b>처음 설정</b>을 합니다.</li>
          </ol>
        </div>
      )}
      {tab === "linux" && (
        <div className="os-guide">
          {linux.map(({ arch, core, desktop }) => (
            <p key={arch} className="download-pair">
              <b>{arch === "amd64" ? "일반 PC(amd64)" : "ARM(arm64)"}</b> <Link asset={core} label="Core" />{" "}
              <Link asset={desktop} label="데스크톱(GUI·트레이)" />
            </p>
          ))}
          <ol>
            <li>Ubuntu 22.04 이상(또는 Debian 12 이상)에서 <b>두 파일을 함께</b> 받습니다.</li>
            <li>
              받은 폴더에서 두 파일을 함께 설치합니다(<code>dpkg -i</code>는 필요한 라이브러리를 받지 않아 실패합니다).
              <pre className="command">
                {`sudo apt install ./agent-office-collector_*_${linux[0]?.arch ?? "amd64"}.deb ./agent-office-collector-desktop_*_${linux[0]?.arch ?? "amd64"}.deb`}
              </pre>
            </li>
            <li>앱 메뉴에서 <b>Agent Office Collector</b>를 열어 <b>처음 설정</b>을 합니다. 화면 없는 서버는 Core 파일만 설치합니다.</li>
          </ol>
        </div>
      )}
      <div className="os-guide common">
        <h3>처음 설정에서</h3>
        <ol>
          <li>
            서버 주소에 <code>{server}</code>{" "}
            <button type="button" className="link-button" onClick={copy}>
              {copied ? "복사됨" : "복사"}
            </button>
          </li>
          <li>
            브라우저 로그인으로 PC를 등록합니다(코드를 직접 넣는 경우 <a href="/devices">PC 화면</a>에서 등록 코드 발급).
          </li>
          <li>Claude Code·Codex Hook 설치와 자동 시작에 동의합니다. Codex는 그다음 Codex를 열어 "Hooks need review"에서 신뢰를 누릅니다.</li>
        </ol>
        <p className="hint">설치한 뒤에는 트레이 아이콘의 "업데이트 설치"로 새 버전을 받습니다.</p>
      </div>
    </section>
  );
}

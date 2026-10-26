# Collector 설치 패키지

관련 이슈: #42(headless Core 패키지), #159(GUI·트레이 설치 프로그램). GUI·트레이 설치 프로그램(Windows `.exe`, macOS `.pkg`, Linux desktop deb·rpm)의 설치·제거·서명 경고 우회는 [Collector GUI](collector-gui.md#설치-프로그램159)에 있습니다.

## 배포물

`collector/packaging/build.sh <버전>`(Docker만 필요, 호스트 Go 불필요)이 `dist/collector-<버전>/`에 만듭니다.

| OS | 아키텍처 | 형식 | 내용 |
|---|---|---|---|
| Linux | amd64·arm64 | `.deb`, `.rpm`, `.tar.gz` | `/usr/bin/agent-office-collector`, `/usr/bin/agent-office-collector-uninstall`, README |
| macOS | amd64·arm64 | `.tar.gz` | 바이너리, `agent-office-collector-uninstall`, README |
| Windows | amd64·arm64 | `.zip` | `agent-office-collector.exe`, `uninstall.ps1`, README |
| Windows(#159) | amd64 | `agent-office-collector-setup-<버전>-windows-amd64.exe` | Core·GUI·트레이 사용자별 설치 프로그램(`build-windows-installer.sh`, NSIS) |
| macOS(#159) | 유니버설 | `agent-office-collector-<버전>-macos-universal.pkg` | 앱 번들(GUI·Core·트레이)(`macos/build-pkg.sh`, macOS에서만) |
| Linux(#159) | amd64·arm64 | `agent-office-collector-desktop` `.deb`, `.rpm` | GUI·트레이·앱 메뉴 항목·아이콘, 같은 버전 Core 패키지에 의존(`build-desktop-linux.sh`, 아키텍처별 러너). #180부터 GUI를 `ubuntu:22.04`(glibc 2.35)에서 빌드해 Ubuntu 22.04 이상에서 설치·실행([지원 배포판](collector-gui.md#지원-linux-배포판데스크톱-패키지)) |

- 정적 바이너리(`CGO_ENABLED=0`, `-trimpath`), 버전은 `-ldflags -X main.version`.
- `SHA256SUMS`에 모든 배포물의 해시. 받은 쪽은 `sha256sum -c SHA256SUMS`(Windows `Get-FileHash`)로 확인합니다.
- deb·rpm은 nfpm 2.41.1로 만듭니다. 패키지 안의 버전은 `-beta.1`이 패키지 규칙상 `~beta.1`로 바뀝니다(정식 버전보다 낮게 정렬). GitHub Release가 파일 이름의 `~`를 `.`으로 바꾸므로 파일 이름은 처음부터 `.`으로 씁니다(0.1.0-beta.1에서 `SHA256SUMS`와 이름이 어긋나 수동 교체).

## 설치 뒤

패키지는 시스템 단위로 아무것도 켜지 않습니다. Hook·자동 시작은 사용자 계정 단위이고 사용자가 직접 승인해야 하기 때문입니다.

```sh
agent-office-collector register --server https://<서버> --code <웹 /devices에서 받은 코드> --name <PC 이름>
agent-office-collector hooks install --tool claude      # 미리보기 확인 후 --yes
agent-office-collector service install --activate
```

## 제거

| 범위 | 명령 | 지우는 것 |
|---|---|---|
| 기본 | `agent-office-collector-uninstall`(Windows `uninstall.ps1`) | 우리가 설치한 Hook과 자동 시작(소유 표시가 있는 것만, 트레이 자동 시작 `.desktop`·LaunchAgent·HKCU Run 값 포함 — #43, #159), 이 사용자의 트레이·GUI 프로세스 종료. 설정·장치 토큰·큐는 남김(재설치 시 그대로 이어감) |
| 완전 삭제 | `… --purge`(Windows `-Purge`) | 위 + OS 보안 저장소의 장치 토큰 항목(`logout --local-only`, 서버 폐기는 하지 않음, #41) + Core 설정·상태·큐 폴더(`~/.config/agent-office/collector` 등, Core 경로 규칙과 같음) + GUI 설정(`AgentOffice/CollectorGUI`) |
| macOS 앱 | `… --remove-app`(앱 안 `Contents/Resources/agent-office-collector-uninstall`) | 위 기본 정리 + `/Applications/Agent Office Collector.app`(번들 ID 확인 뒤, 권한이 없으면 sudo로 앱만) + `pkgutil --forget` |
| Windows 설치 프로그램 | 앱 제거(`uninstall.exe`) | `uninstall.ps1 -Yes` 기본 정리 + 설치 폴더·시작 메뉴·HKCU 제거 항목 |
| 패키지 | `dpkg -r` / `rpm -e` | 바이너리만. 다른 사용자 홈은 건드리지 않음 |

Claude·Codex의 원본 기록(`~/.claude/projects` 등)은 어떤 경우에도 지우지 않습니다. 장치를 서버에서도 끊으려면 웹 `/devices`에서 그 PC를 폐기합니다.

## 서명·공증(미구현·미검증)

| 대상 | 필요한 것 | 상태 |
|---|---|---|
| 해시 | `SHA256SUMS` | 구현 |
| 모든 배포물 서명 | GitHub OIDC keyless(cosign) 등 | 미구현(게시 워크플로에 `id-token: write`만 준비) |
| macOS | Developer ID 서명·notarization | 미구현(비용). `.pkg`는 #159에서 구현, 앱은 ad-hoc 서명만 |
| Windows | Authenticode 서명 | 미구현(비용). 설치 프로그램은 #159에서 NSIS `.exe`로 구현(MSI는 고르지 않음, ADR-0005) |

## 게시

`manifest.json`에는 Core 압축본(`artifacts`)과 설치 파일(`installers`, #180)이 함께 들어갑니다. 트레이의 승인형 업데이트는 채널 Release(`collector-beta`·`collector-stable`)의 manifest를 보므로, 버전 Release를 공개한 뒤 채널 manifest를 갱신해야 사용자에게 보입니다(`publish_channel: true` 또는 `collector/packaging/update-channel.sh`, [Collector 자체 업데이트](collector-update.md#키와-서명관리자)).

`.github/workflows/publish-collector.yml`(수동 실행, `collector-publish` Environment 승인)이 같은 스크립트로 빌드해 GitHub Release **초안**으로 올립니다. 관리자가 확인 후 공개합니다. #159부터 Windows 설치 프로그램(ubuntu-24.04, Docker), macOS `.pkg`(macos-latest), Linux 데스크톱 패키지(ubuntu-24.04·ubuntu-24.04-arm)를 각각 만든 뒤 Core 배포물과 합쳐 `SHA256SUMS`를 다시 계산합니다(파일 이름에 `~`가 있으면 실패). Core를 품는 작업은 업데이트 공개 키 때문에 같은 Environment 승인을 받습니다. #159의 변경 뒤로는 아직 실행하지 않았습니다(같은 스크립트를 `Cross-OS Collector`가 PR마다 실행).

## 검증(Pi 5 arm64, 2026-10-24)

- 6개 대상 빌드와 deb·rpm 4개 생성, `SHA256SUMS` 확인 통과.
- Debian 12 컨테이너: deb 설치 → `version` → 제거 도우미 기본(설정 유지)·`--purge`(설정 삭제, Claude 기록 유지) → `dpkg -r`로 바이너리 제거.
- Fedora 41 컨테이너: rpm 설치 → `version` → `rpm -e`로 제거.
- Linux tar.gz 실행, Windows zip 구성(PE 실행 파일) 확인.
- 미검증: amd64 Linux 패키지 실행(교차 빌드만), 서명.

## 검증(macOS·Windows 러너, 2026-10-25)

`.github/workflows/cross-os.yml`의 `tests/cross_os/collector_smoke.py`가 `macos-latest`(arm64)와 `windows-latest`(amd64) 러너에서 같은 저장소의 Core와 제거 도우미(`agent-office-collector-uninstall`, `uninstall.ps1`)로 다음을 확인합니다. 러너는 일회용이라 실제 사용자 위치를 씁니다. 서버는 스크립트 안의 가짜 API입니다.

등록 → OS 보안 저장소 보관 → Hook 설치(사용자 Hook 유지) → `run`·`hook claude` 수집·전송(프롬프트 sentinel 서버 미도달) → `service install --activate`(작업 스케줄러·launchd 등록, 서비스로 뜬 Core의 heartbeat) → `service uninstall --activate`(OS 등록 해제) → `logout`(서버 폐기) → 재등록 뒤 제거 도우미 `--purge`(설정·토큰 삭제, 우리 Hook만 제거, 사용자 Hook 유지).

이 검증에서 고친 문제:

- `uninstall.ps1`이 BOM 없는 UTF-8이라 Windows PowerShell 5.1에서 한글 문자열 구문 오류 → UTF-8 BOM 저장, `tests/test_repository_metadata.py`가 BOM을 검사.
- `$ErrorActionPreference = "Stop"`과 `2>$null`로 네이티브 명령 stderr가 종료 오류가 됨 → 네이티브 명령은 출력만 버리고 cmdlet에만 `-ErrorAction Stop`.
- 제거 도우미가 `service uninstall`을 `--activate` 없이 불러 정의 파일만 지우고 작업 스케줄러·launchd 등록과 실행 중인 Collector가 남음(Windows는 잠금 파일 때문에 `--purge` 실패) → `--activate`로 등록 해제·중지, Windows는 폴더 삭제를 잠시 재시도.

미실행: 서명하지 않은 실행 파일의 Gatekeeper·SmartScreen 경고, 사람이 쓰는 데스크톱에서 재로그인 후 자동 시작.

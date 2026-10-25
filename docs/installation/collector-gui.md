# Collector GUI(Wails)·브라우저 로그인 기기 등록·트레이·설치 프로그램

관련 이슈: #40 (`EP-06/ISS-01`, 요구사항 COL-001~002, USER-002), #41 (`EP-06/ISS-02`, DEV-001~005, COL-003), #43 (`EP-06/ISS-04`, COL-001~002, COL-010, DIST-007), #159(모든 OS 트레이·설치 프로그램, 명세 §10). 결정 근거: [ADR-0003](../decisions/0003-collector-gui-wails-v2.md), [ADR-0004](../decisions/0004-collector-keychain-tray.md), [ADR-0005](../decisions/0005-collector-tray-helper-installers.md).

`collector/gui`는 [Collector CLI](collector-cli.md)(`agent-office-collector`, 이하 Core)를 감싸는 데스크톱 창입니다. OS 알림과 업데이트 적용(#44)은 범위가 아닙니다. 트레이·자동 시작·GUI/Core 독립성은 #43에서, 모든 OS 트레이 도우미(`agent-office-collector-tray`)와 OS별 설치 프로그램은 #159에서 추가했습니다.

## 검증 범위

검증되지 않은 OS에서는 지원한다고 표시하지 않습니다.

| 대상 | 상태 | 근거 |
|---|---|---|
| Linux arm64 백엔드(Go, 화면 없음) | **검증**: `gofmt`, `go vet ./...`, `go test -race ./...` | Docker `golang:1.24-bookworm`, Raspberry Pi 5 호스트 |
| 실제 Core와의 계약(Linux arm64) | **검증**: 같은 저장소 Core를 빌드해 `AGENT_OFFICE_TEST_CORE_BIN`으로 지정한 통합 테스트 | `internal/app/integration_test.go` |
| Linux arm64 데스크톱 빌드·실행 | **검증(컨테이너)**: bookworm `libgtk-3-dev 3.24.38-2~deb12u3`·`libwebkit2gtk-4.0-dev 2.50.6-1~deb12u2`로 `-tags desktop,production` 빌드, Xvfb에서 창 표시·탭 전환·Go 바인딩 호출(설정 읽기, Core `status`·`hooks status`) 스크린샷 확인, GUI 종료 뒤 Core `status` 정상 | 아래 [Linux](#linux) |
| 실제 데스크톱 세션(Wayland·GNOME 등)·`BrowserOpenURL`로 브라우저 열기 | **미실행**: 컨테이너에 데스크톱·브라우저 없음. 브라우저 열기는 단위 테스트에서 호출 주소만 확인 | `app_test.go` |
| Linux amd64 | **미실행**(CI `Go GUI` 작업은 amd64에서 백엔드 테스트만 실행) | `.github/workflows/ci.yml` |
| Ubuntu 24.04 등 webkit2gtk 4.1만 있는 배포판 | **미검증**: Wails v2는 `-tags webkit2_41`이 필요할 수 있음 | — |
| macOS(WKWebView) | **검증(러너)**: GitHub Actions `macos-latest`(arm64, macOS 26.6)에서 백엔드 `go vet`·`go test`(실제 Core 계약 테스트 포함), `-tags desktop,production` 빌드, 실행 후 15초 유지. 창 화면·조작은 **미실행** | `.github/workflows/cross-os.yml`, [macOS·Windows](#macoswindowsgithub-actions-러너-검증) |
| Windows(WebView2) | **검증(러너)**: `windows-latest`(Windows Server 2025, amd64)에서 같은 항목, `-H windowsgui` 빌드 실행 후 15초 유지. 창 화면·조작과 Core 실행 시 콘솔 창 숨김(`CREATE_NO_WINDOW`) 육안 확인은 **미실행** | 같은 워크플로, `hidewindow_windows.go` |
| 실제 Agent Office 서버에서 웹 코드 발급 → GUI 등록 | **미실행**: 웹 화면은 Vitest·빌드, 등록은 `httptest` 서버로만 확인 | `devices.test.tsx`, `integration_test.go` |
| 장치 토큰 OS 보안 저장소(#41) — Linux Secret Service | **검증(컨테이너)**: Debian bookworm `gnome-keyring 42.1-1+b2`·`dbus 1.14.10`(`dbus-run-session`, 잠금 해제한 login 모음)에서 저장·읽기·삭제, `register` 뒤 `config.json`에 토큰 없음·`status` `os_keyring`, `logout` 뒤 항목 삭제, 파일 토큰을 `run`이 저장소로 이전 | `tests/poc/collector/desktop/keyring-secret-service.sh`, [Collector CLI](collector-cli.md#장치-토큰-보관41) |
| 장치 토큰 — 실제 데스크톱 세션(GNOME·KDE 로그인 키링 자동 잠금 해제, KWallet) | **미실행** | — |
| 장치 토큰 — macOS Keychain·Windows Credential Manager | **검증(러너)**: 두 러너에서 `AGENT_OFFICE_TEST_REAL_KEYRING=1` 실제 저장소 테스트 통과, 스모크에서 `register` 뒤 `token_storage` `os_keyring`·`config.json`에 토큰 없음, `logout` 뒤 서버 폐기·미등록. 사람이 쓰는 데스크톱의 키체인 잠금·접근 허용 대화상자는 **미실행** | `tests/cross_os/collector_smoke.py`, ADR-0004 |
| 대체 경로(보안 저장소 없음 → 0600 파일, `file_fallback`) | **검증**: 세션 D-Bus 없는 컨테이너·CI에서 `register`가 파일에 저장하고 상태에 `file_fallback` 표시 | `logout_test.go`, 아래 트레이 검증 로그 |
| 로그아웃(Core `logout`, GUI 버튼) | **검증**: 서버 `POST /api/v1/devices/revoke` 호출(장치 토큰 Bearer) → 로컬 토큰·`config.json` 삭제 → 실행 중인 Core가 전송·heartbeat 중지. 서버 오류·네트워크 오류면 아무것도 지우지 않음, `--local-only` | `logout_test.go`, `tray/independence_test.go` |
| 트레이 도우미 — Linux(StatusNotifierItem) | **검증(러너)**: GitHub `ubuntu-24.04`(amd64)·`ubuntu-24.04-arm`(arm64)에서 desktop deb 설치 → Xvfb·`dbus-run-session`·최소 `org.kde.StatusNotifierWatcher`에 등록, dbusmenu에 메뉴 7항목(사무실 열기·설정 열기·일시중지·재개·업데이트 확인·로그아웃·종료), D-Bus `Event` 클릭으로 일시중지·재개 → Core `paused` 반영, 셀프 테스트 8단계 통과(GUI 창 실행·종료 포함) | [설치 프로그램 실측](#설치-프로그램-실측cross-os-collector) |
| 트레이 도우미 — macOS 메뉴 막대 | **검증(러너, 처리 경로)**: `macos-latest`(arm64, macOS 26.6)에서 .pkg로 설치한 `LSUIElement` 앱이 15초 유지·단일 실행(종료 코드 3)·셀프 테스트 8단계 통과(`NSStatusItem`을 만든 뒤 `onReady`, 메뉴 항목 클릭 채널 → Core 명령·GUI 창 실행 → `--quit`으로 창 종료). 메뉴 막대에 그려진 아이콘을 사람이 누르는 것은 **미실행** | 같음 |
| 트레이 도우미 — Windows 알림 영역 | **검증(러너, 처리 경로)**: `windows-latest`(Windows Server 2025)에서 설치 프로그램으로 설치한 트레이가 15초 유지·단일 실행·셀프 테스트 8단계 통과(`Shell_NotifyIcon(NIM_ADD)` 성공 뒤에만 `onReady`가 불림, ICO 아이콘). 알림 영역 아이콘 클릭은 **미실행** | 같음 |
| 실제 패널(KDE Plasma, GNOME AppIndicator 확장, XFCE 등)에서 아이콘 표시·클릭 | **미실행**: 러너의 Watcher는 프로토콜 검증용 최소 구현 | — |
| 창 닫기 | 항상 GUI 창 프로세스 종료(트레이는 별도 프로세스로 남아 "설정 열기"로 다시 엶). #43의 "트레이로 숨기기"는 없앴습니다 | ADR-0005 |
| 트레이 자동 시작(XDG autostart·LaunchAgent·HKCU Run) | **검증(러너, 등록·제거)**: 세 OS 설치본에서 `agent-office-collector-tray autostart install` → 상태 `installed`·정의가 트레이 실행 파일을 가리킴 → 제거 프로그램(또는 제거 도우미)이 소유 항목만 삭제. 생성 `.desktop`은 `desktop-file-validate`, plist는 `plutil -lint` 통과(#43). 실제 로그인 시 실행은 **미실행** | `autostart_test.go`, `installer_smoke.py` |
| GUI/Core 독립성 | **검증**: 실제 Core `run`을 별도 프로세스로 띄우고 GUI 프로세스를 SIGKILL·트레이 종료로 끝내도 Core가 계속 Hook 이벤트를 전송, Core 강제 종료·재시작 시 트레이 상태 회복 | `tray/independence_test.go`(CI `Go GUI`) |
| 재부팅 후 자동 수집(Core 서비스 + 트레이 자동 시작) | **미실행**: 러너·컨테이너에서 다시 로그인할 수 없음 | [Collector CLI 자동 시작](collector-cli.md#자동-시작service) |
| 설치 프로그램 설치 → 실행 → 제거 | **검증(러너)**: Windows NSIS `.exe`(사용자별), macOS 유니버설 `.pkg`, Ubuntu 24.04 amd64·arm64 deb. 자세한 항목은 [설치 프로그램 실측](#설치-프로그램-실측cross-os-collector) | `.github/workflows/cross-os.yml` |
| Gatekeeper·SmartScreen 경고, 사람이 쓰는 데스크톱의 첫 실행 | **미실행**: 러너는 내려받은 파일 격리 속성(quarantine)·Mark of the Web 없이 설치 | [서명하지 않은 설치 프로그램](#서명하지-않은-설치-프로그램) |
| Linux rpm(Fedora 등) 데스크톱 패키지 설치 | **미실행**: 빌드만(의존 `gtk3`·`webkit2gtk4.1`) | — |

## 구조

```text
collector/gui
├ main.go                 Wails v2.12.0 창(빌드 태그 desktop,production에서만 실행)
├ frontend/assets/        정적 HTML·JS·CSS(빌드 도구·npm 의존성 없음, CSP 'self')
└ internal/
  ├ corecli/              Core 실행 파일 찾기·실행(시간 제한, 출력 상한)·JSON 해석·입력 검증
  ├ app/                  Wails에 바인딩되는 App(화면 없이 단위 테스트)
  ├ prefs/                GUI 설정 gui.json(서버 주소·PC 이름 초안만)
  ├ tray/                 트레이 상태·메뉴 동작(화면 없이 테스트), 상태별 아이콘(PNG·Windows ICO), GUI/Core 독립성 테스트
  ├ autostart/            트레이 자동 시작 정의(XDG .desktop·LaunchAgent·HKCU Run) 생성·설치·제거
  ├ launch/               GUI ↔ 트레이 프로세스 띄우기(부모·자식으로 묶지 않음), 기본 브라우저 열기(http(s)만)
  ├ single/               사용자별 단일 실행 잠금(flock·LockFileEx)
  ├ guictl/               GUI 단일 실행(gui.lock)과 두 번째 실행의 show/quit 요청(gui.request)
  └ fakecore/             테스트용 가짜 Core
cmd/agent-office-collector-tray/  트레이 도우미(fyne.io/systray v1.12.2가 프로세스 메인 스레드를 씀, ADR-0005)
```

### GUI와 Core의 분리

- GUI는 수집 루프를 품지 않습니다. 모든 동작은 Core 명령을 하위 프로세스로 실행해 처리합니다.

| 화면 | Core 명령 |
|---|---|
| 기기 등록 | `register --server URL --code-stdin --name PC [--force]` (코드는 표준 입력으로만) |
| 수집 상태(5초마다 갱신) | `status --json` |
| 일시중지·재개 | `pause`, `resume` |
| 진단 | `diagnose --json [--offline]` |
| 도구 탐지 | `hooks status --json` (`claude --version`·`codex --version`, 3초 제한) |
| Claude Hook 미리보기·설치·제거 | `hooks install --tool claude` → `hooks install --tool claude --yes`, `hooks uninstall --tool claude --yes` |
| Codex Hook | `hooks print --tool codex` (자동 설치 미지원) |
| 자동 시작 | `service print`, `service install [--activate]` |
| 로그아웃 | `logout [--local-only]` (서버 폐기·로컬 토큰 삭제는 Core가 함) |
| 트레이 상태(5초마다)·일시중지·재개·업데이트 확인·로그아웃 | `status --json`, `pause`, `resume`, `update check --channel stable\|beta`, `logout` |

- 창을 닫거나 GUI가 비정상 종료해도 자동 시작된 Core는 계속 수집합니다(`tray/independence_test.go`: GUI 프로세스 SIGKILL·트레이 종료 뒤에도 Core가 Hook 이벤트 전송). GUI는 `run`을 직접 띄우지 않으므로, 자동 시작을 설치·활성화하지 않았으면 수집 상태 화면에 "Core 프로세스: 실행 중 아님"이 보입니다.
- Core 위치: `$AGENT_OFFICE_COLLECTOR_BIN`(절대 경로만) → GUI·트레이 실행 파일과 같은 폴더의 `agent-office-collector[.exe]`(macOS 트레이 번들이면 바깥 앱 번들의 `Contents/MacOS`) → `PATH`. 못 찾으면 화면 위에 안내가 나옵니다.
- 명령 시간 제한은 20초, `service install`은 90초입니다. stdout은 1MiB, stderr는 8KiB까지만 보관합니다.

## 브라우저 로그인·기기 등록(#41)

```text
GUI: 서버 주소 저장 ─► "브라우저에서 로그인하고 등록 코드 받기"
                         │ 기본 브라우저로 <서버>/devices 열기
웹: 로그인(비밀번호·OAuth) ─► /devices "PC 등록 코드 발급"
                         │ POST /api/v1/devices/registration-codes (세션+CSRF)
                         │ XXXX-XXXX, 10분, 일회용, 열린 코드 최대 5개
GUI: 코드·PC 이름 입력 ─► Core register --code-stdin
                         │ POST /api/v1/devices/register
Core: 장치 토큰을 OS 보안 저장소에 저장(없으면 0600 설정 파일). GUI에는 결과 코드만 돌아옴
```

- GUI는 계정 이메일·비밀번호를 받지 않습니다. 로그인은 브라우저에서만 합니다.
- 서버 주소 규칙은 Core와 같습니다: https만, http는 `localhost`·`127.x`·`::1`만, 경로·쿼리·조각·사용자 정보 없음.
- 등록 코드는 서버 알파벳(`ABCDEFGHJKMNPQRSTVWXYZ23456789`) 8자만 받고, 공백·하이픈·소문자는 정리합니다. 형식이 틀리면 Core를 실행하지 않습니다. 입력란은 제출 직후 비웁니다.
- 이미 등록된 PC는 "새 장치로 다시 등록"을 선택해야 `--force`로 다시 등록합니다.
- 웹 `/devices`(로그인 필요, 상단 "PC" 링크)는 코드 발급·남은 시간 표시와 내 PC 목록(OS, Collector 버전, 연결 상태, 마지막 연결)·폐기를 제공합니다. 코드는 화면 상태에만 있고 저장하지 않습니다.

### 로그아웃(#41)

"기기 등록" 화면의 **로그아웃**은 확인 창을 거쳐 Core `logout`을 실행합니다. Core가 장치 토큰으로 `POST /api/v1/devices/revoke`를 호출해 서버에서 이 PC를 폐기하고(204, 이미 폐기돼 401·403이어도 진행), 보안 저장소 항목과 `config.json`을 지웁니다. 실행 중인 Core는 몇 초 안에 등록 정보가 사라진 것을 보고 전송·heartbeat를 멈춥니다(프로세스는 새 등록을 기다림). 서버에 닿지 않으면 아무것도 지우지 않고 실패하며, "로컬만 지우기"를 고르면 `--local-only`로 로컬만 지웁니다(웹 `/devices`에서 따로 폐기). 로컬 큐는 지우지 않습니다.

### Hook 연동 명시적 동의

1. "설치 미리보기"로 대상 파일(홈은 `~`), 이벤트별 변경, 추가할 핸들러를 보여 줍니다(파일 변경 없음).
2. 미리보기를 본 뒤에만 "내용을 확인했고 설치에 동의" 버튼이 켜지고, 누르면 확인 창을 한 번 더 띄웁니다.
3. 백엔드도 같은 실행에서 미리보기를 거치지 않았거나 승인 값이 없으면 `--yes`를 보내지 않습니다. 한 번의 승인은 한 번만 씁니다.
4. 백업·원자적 쓰기·소유 항목만 변경·제거는 Core 규칙([Hook 설치](collector-cli.md#hook-설치hooks-31))을 그대로 따릅니다.

## 트레이 도우미·자동 시작(#43, #159)

### 트레이 도우미(`agent-office-collector-tray`)

트레이는 Wails 창과 **다른 프로세스**입니다([ADR-0005](../decisions/0005-collector-tray-helper-installers.md)). `fyne.io/systray` v1.12.2가 그 프로세스의 메인 스레드·메시지 루프(macOS `NSApplication`, Windows `GetMessage`, Linux D-Bus)를 혼자 쓰므로 Wails 루프와 다투지 않습니다. GUI 창이 뜰 때 옆에 설치된 트레이를 함께 띄우고, 트레이는 사용자별 잠금(`AgentOffice/CollectorGUI/tray.lock`)으로 하나만 실행됩니다(두 번째는 종료 코드 3).

| 메뉴 | 동작 |
|---|---|
| 상태(비활성) | 수집 중 / 일시중지 / Core 실행 중 아님 / 기기 등록 필요 / 장치 폐기됨 / 보안 저장소 잠김 / Core 응답 없음 |
| 전송 대기(비활성) | Core 로컬 큐 건수("전송 대기 N건") |
| 사무실 열기 | GUI 설정의 서버 주소(`https://…/`)를 기본 브라우저로 엽니다(macOS `open`, Windows `rundll32 url.dll,FileProtocolHandler`, Linux `xdg-open`, http(s)만). 서버 주소가 없으면 설정 창 |
| 설정 열기 | GUI 창 실행 파일을 띄웁니다. 이미 떠 있으면 두 번째 실행이 첫 GUI에 "창 보이기"를 남기고 곧바로 끝납니다(아래 단일 실행) |
| 수집 일시중지·재개 | Core `pause`·`resume` 후 바로 상태 갱신(현재 상태에 맞는 항목만 활성) |
| 업데이트 확인 | Core `update check`(현재 버전이 `-beta.N` 등이면 beta 채널). 결과를 항목 제목에 표시("최신 버전"·"새 버전 …"·"확인 실패"). 적용은 하지 않습니다 |
| 로그아웃 ▸ 이 PC 로그아웃(서버에서 장치 폐기) | 하위 메뉴 항목을 한 번 더 골라야 Core `logout`(서버 폐기 + 로컬 토큰 삭제). 등록된 PC에서만 활성 |
| 종료(수집은 계속) | GUI 창 프로세스에 `--quit`을 보내고 트레이도 끝냅니다. Core `run`(OS 자동 시작)은 계속 수집합니다 |

- 상태는 5초마다 Core `status --json`으로 읽습니다. Core가 멈추면 "Core 실행 중 아님", 다시 시작하면 다음 주기에 "수집 중"으로 돌아옵니다. 트레이·GUI는 Core를 직접 띄우지 않습니다.
- 명세 §10의 "동기화"는 넣지 않았습니다. Core에 즉시 전송 명령이 없고 Core가 주기적으로 전송합니다(전송 대기 건수로 대신 표시).
- 아이콘은 상태별 색(수집 중 초록, 일시중지 주황, 오류 빨강, 미등록·Core 정지 회색)이고 Windows는 32×32 ICO, 그 밖은 22×22 PNG입니다. 툴팁·메뉴 문구는 enum에서 만든 고정 한국어와 Core가 낸 버전 문구뿐이고 서버 주소·경로·토큰을 넣지 않습니다.
- 창 닫기(X)는 항상 GUI 창 프로세스 종료입니다. 트레이는 남아 있으므로 "설정 열기"로 다시 엽니다. GUI는 한 번만 실행됩니다. Wails v2.12.0 단일 실행은 Windows에서 기존 창을 찾지 못하므로(ADR-0005) GUI 설정 폴더의 `gui.lock`(파일 잠금)과 `gui.request`(`show`/`quit` 한 단어, 0.3초마다 읽음)로 모든 OS에서 같게 처리합니다. 두 번째 실행은 Wails를 띄우지 않습니다.
- Linux에서 세션 버스에 `org.kde.StatusNotifierWatcher`가 없으면(GNOME 기본 셸 등) 아이콘이 보이지 않습니다. 이때도 트레이 프로세스는 떠 있고 창은 앱 메뉴의 "Agent Office Collector"로 엽니다(GNOME은 AppIndicator 확장 필요, 미검증).
- macOS 트레이는 `LSUIElement` 번들이라 Dock·앱 전환기에 보이지 않습니다.

```text
agent-office-collector-tray                         트레이 실행
agent-office-collector-tray autostart install|uninstall|status   로그인 시 트레이 자동 시작(소유 항목만 변경)
agent-office-collector-tray --self-test 결과.json   설치 검증용(아래), 일반 사용자는 쓰지 않음
agent-office-collector-tray version
```

### 트레이 자동 시작

"자동 시작" 화면 아래 **트레이 자동 시작**(또는 `agent-office-collector-tray autostart install`)은 Core 서비스(`service install`)와 별개입니다. 꺼도 수집은 계속되고, 켜면 로그인할 때 트레이 도우미를 `--autostart`로 띄웁니다. 트레이 도우미가 없는 개발 빌드에서는 GUI를 띄웁니다. 파일 이름·Label·값 이름과 소유 표시는 #43과 같아서 이전 GUI 자동 시작 항목을 그대로 갱신하며, 이전 항목으로 GUI가 `--autostart`로 뜨면 트레이만 띄우고 끝납니다.

| OS | 위치 | 소유 판정 |
|---|---|---|
| Linux | `${XDG_CONFIG_HOME:-~/.config}/autostart/agent-office-collector-gui.desktop` | 본문의 `agent-office-collector-gui:managed-autostart:v1`(주석·`X-AgentOffice-Managed`) |
| macOS | `~/Library/LaunchAgents/io.github.apg0001.agent-office.collector-gui.plist`(`RunAtLoad`, `LimitLoadToSessionType=Aqua`, `KeepAlive` 없음, 다음 로그인부터) | 같은 표시(XML 주석) |
| Windows | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 값 `AgentOfficeCollectorGUI` = `"<경로>\agent-office-collector-tray.exe" --autostart` | 값이 정확히 `"…\agent-office-collector-(tray\|gui).exe" --autostart` 형식일 때만 |

- 설치·제거 모두 확인 창을 거칩니다. 소유 표시가 없는 같은 이름 파일·값은 덮어쓰거나 지우지 않습니다. 파일은 같은 디렉터리 임시 파일 → fsync → rename으로 쓰고 심볼릭 링크는 따라가지 않습니다. 관리자 권한을 쓰지 않습니다.
- `go run`·`go test` 임시 실행 파일 경로는 거부합니다. 실행 파일을 옮겼으면 상태가 "이전 위치"로 보이고 다시 켜면 갱신합니다.

## 설치 프로그램(#159)

모든 설치 프로그램은 **코드 서명·공증을 하지 않습니다**(인증서 비용). 설치는 Hook·Core 자동 시작·트레이 자동 시작을 켜지 않습니다. 설치 뒤 "Agent Office Collector"를 열어 서버 주소 → 브라우저 로그인·등록 → Hook 미리보기·승인 → 자동 시작을 차례로 승인합니다(트레이 아이콘은 창과 함께 뜹니다).

| OS | 파일 | 설치 위치 | 관리자 권한 |
|---|---|---|---|
| Windows 10/11 x64 | `agent-office-collector-setup-<버전>-windows-amd64.exe`(NSIS 3) | `%LOCALAPPDATA%\Programs\Agent Office Collector`(Core·GUI·트레이·`uninstall.ps1`), 시작 메뉴 "Agent Office Collector"·"… 제거", HKCU 제거 항목 | 필요 없음(사용자별) |
| macOS 11+ (Apple Silicon·Intel) | `agent-office-collector-<버전>-macos-universal.pkg` | `/Applications/Agent Office Collector.app`(GUI·Core), 그 안 `Contents/Library/LoginItems/Agent Office Collector Tray.app`(메뉴 막대 트레이) | 필요(설치 프로그램이 관리자 암호를 물음) |
| Debian 12+·Ubuntu 24.04+ (amd64·arm64) | `agent-office-collector_<버전>_<arch>.deb` + `agent-office-collector-desktop_<버전>_<arch>.deb` | `/usr/bin/agent-office-collector{,-gui,-tray,-uninstall}`, 앱 메뉴 항목, 아이콘 | `sudo apt install ./…deb ./…deb` |
| Fedora 등(rpm) | `agent-office-collector-<버전>.<arch>.rpm` + `agent-office-collector-desktop-…rpm` | 같음(의존 `gtk3`·`webkit2gtk4.1`, **미검증**) | `sudo dnf install` |

- Windows는 WebView2 런타임이 필요합니다(Windows 11·최신 Windows 10에는 기본 포함). 없으면 GUI 창이 뜨지 않고 Microsoft의 Evergreen 런타임을 설치해야 합니다. 무인 설치는 `setup.exe /S`, 무인 제거는 `uninstall.exe /S`입니다. 업그레이드는 새 설치 프로그램을 실행합니다(실행 중인 트레이·GUI·Core 작업을 멈추고 바꾼 뒤 Core 로그온 작업이 있으면 다시 시작).
- Linux 데스크톱 패키지는 GTK3·webkit2gtk-4.1에 의존합니다(GUI는 `-tags webkit2_41`, Debian bookworm에서 빌드해 glibc 2.36 이상 필요). 헤드리스 서버는 Core 패키지만 설치합니다.
- 설치 프로그램으로 설치한 Core는 자체 업데이트(`update apply`)를 하지 않고 `managed_by_package`로 거절합니다. 새 설치 프로그램·패키지로 업데이트하세요(GUI·트레이와 버전이 어긋나지 않게).

### 제거

| OS | 방법 | 지우는 것 |
|---|---|---|
| Windows | 설정 → 앱 → "Agent Office Collector" 제거(또는 시작 메뉴 "… 제거", `uninstall.exe /S`) | `uninstall.ps1 -Yes`로 이 도구가 만든 Claude Hook·Core 로그온 작업(등록 해제·중지)·트레이 Run 값만 지운 뒤, 트레이·GUI를 끝내고 설치 폴더·바로 가기·제거 항목 삭제 |
| macOS | `"/Applications/Agent Office Collector.app/Contents/Resources/agent-office-collector-uninstall" --remove-app` | 같은 사용자별 정리 + 트레이·GUI 종료 + 앱 번들 삭제(권한이 없으면 sudo로 앱만) + `pkgutil --forget` |
| Linux | 각 사용자 `agent-office-collector-uninstall` → `sudo apt remove agent-office-collector-desktop agent-office-collector` | 사용자별 정리(위와 같음, LaunchAgent 대신 XDG autostart) → 패키지 파일 |

- 설정·장치 토큰·큐는 기본으로 남깁니다(다시 설치하면 이어 씀). 완전히 지우려면 제거 전에 `uninstall.ps1 -Purge`·`agent-office-collector-uninstall --purge`를 실행합니다(GUI 설정 `AgentOffice/CollectorGUI`까지 삭제, [설치 패키지](collector-packages.md#제거)).
- 다른 사용자나 다른 도구가 만든 Claude·Codex Hook·자동 시작 항목은 건드리지 않습니다. Claude·Codex 원본 기록은 어떤 경우에도 지우지 않습니다.

### 서명하지 않은 설치 프로그램

| OS | 나타나는 경고 | 여는 방법 |
|---|---|---|
| macOS | "확인되지 않은 개발자가 배포했기 때문에 열 수 없습니다" / "Apple은 악성 코드가 없음을 확인할 수 없습니다" | Finder에서 `.pkg`를 Control-클릭(우클릭) → 열기 → 열기. 막히면 시스템 설정 → 개인정보 보호 및 보안 → 아래쪽 "그래도 열기". 터미널: `xattr -d com.apple.quarantine <파일>.pkg` 뒤 다시 열기. 설치된 앱은 설치 프로그램이 풀어 쓴 파일이라 격리 속성이 없습니다(ad-hoc 서명) |
| Windows | SmartScreen "Windows의 PC 보호" | "추가 정보" → "실행". 브라우저가 내려받기를 막으면 "유지" 선택. PowerShell: `Unblock-File .\agent-office-collector-setup-…exe` |
| Linux | 없음(apt는 로컬 파일 설치) | `SHA256SUMS`로 무결성 확인: `sha256sum -c --ignore-missing SHA256SUMS` |

받은 파일은 항상 Release의 `SHA256SUMS`와 비교하세요(Windows `Get-FileHash`, macOS `shasum -a 256`). 경고를 우회하는 것은 출처와 해시를 확인한 파일에만 하세요. 위 대화상자는 러너에서 재현되지 않아 **실측하지 않았습니다**(문구는 OS 버전에 따라 다를 수 있음).

### 설치 프로그램 실측(Cross-OS Collector)

`.github/workflows/cross-os.yml`의 `Installer …` 작업이 `tests/cross_os/installer_smoke.py`로 일회용 러너에서 확인합니다. Windows 설치 프로그램과 Linux amd64 deb는 `Installer build` 작업이 Docker에서 만든 같은 파일을, macOS .pkg·Linux arm64 deb는 그 러너에서 만든 파일을 씁니다.

1. 설치(Windows `/S`, macOS `sudo installer -pkg`, Linux `sudo apt-get install ./…deb`) → 실행 파일·바로 가기(또는 `.desktop`·아이콘)·제거 항목(또는 pkg 영수증) 존재, Core·트레이 `version`. macOS는 트레이 `LSUIElement`와 `codesign --verify --deep --strict`.
2. 사용자 승인 동작: 남의 Hook이 든 `~/.claude/settings.json`에 Core `hooks install --yes`, Core `service install --activate`(Windows 작업 스케줄러·macOS launchd 등록 확인), `tray autostart install`(정의가 트레이를 가리킴).
3. 트레이 15초 유지, 두 번째 실행 종료 코드 3. Linux는 최소 Watcher 등록·dbusmenu 항목 7개·D-Bus 클릭으로 일시중지·재개.
4. `--self-test`: 실제 systray 메뉴 항목의 클릭 채널로 상태 → 일시중지 → 재개 → 업데이트 확인 → 사무실 열기(서버 없음 → 설정 창) → 설정 열기 → 로그아웃 → 종료. 그동안 GUI 창 프로세스가 실제로 뜨는지(초 단위 표본)와 "종료" 뒤 GUI·트레이가 모두 끝나는지 확인합니다. 브라우저는 열지 않고 요청만 셉니다. 업데이트 확인은 CI 빌드에 업데이트 공개 키가 없어 `update_check_failed`가 정상입니다(처리 경로만 확인).
5. 제거 → 설치 파일·바로 가기·제거 항목 삭제, 트레이 자동 시작 정의 삭제, Core 서비스 등록 해제, 트레이·GUI 프로세스 없음, 우리 Hook 삭제, 남의 Hook 유지.

결과(2026-10-26, PR #168): Cross-OS Collector 실행 40213759819에서 네 작업 모두 실패 항목 0개.

| 러너 | 설치 파일(빌드 환경) | 확인한 것 |
|---|---|---|
| `windows-latest`(Windows Server 2025 10.0.26100, amd64) | NSIS `.exe`(Docker `golang:1.24-bookworm` + `nsis 3.08-3+deb12u1`로 교차 빌드) | `/S` 설치 → 파일·시작 메뉴·HKCU 제거 항목 → Hook·작업 스케줄러 등록·Run 값(트레이) → 트레이 15초 유지·단일 실행 → 셀프 테스트 8단계(onReady 19ms, GUI 창 프로세스 10초 관측·최대 1개, 종료 뒤 GUI·트레이 없음) → `uninstall.exe /S` → 폴더·바로 가기·제거 항목·Run 값·작업 등록 삭제, 우리 Hook만 삭제·남의 Hook 유지 |
| `macos-latest`(macOS 26.6.2, arm64) | 유니버설 `.pkg`(같은 러너, Xcode 명령행 도구) | `sudo installer` → pkg 영수증·트레이 `LSUIElement`·`codesign --verify --deep --strict` → Hook·launchd 등록·LaunchAgent(트레이) → 트레이 15초 유지·단일 실행 → 셀프 테스트 8단계(onReady 296ms, GUI 10초 관측·최대 1개) → `--remove-app` → 앱·영수증·LaunchAgent·launchd 등록 삭제, 남의 Hook 유지 |
| `ubuntu-24.04`(glibc 2.39, amd64) | Core deb + desktop deb(Docker bookworm, `libgtk-3-dev 3.24.38-2~deb12u3`·`libwebkit2gtk-4.1-dev 2.50.6-1~deb12u2`), 의존 `agent-office-collector (= 0.0.0~ci.18), libgtk-3-0 \| libgtk-3-0t64, libwebkit2gtk-4.1-0` | `apt-get install` 의존성 해결 → 파일·`.desktop`·아이콘 → Hook·XDG autostart(트레이) → 트레이 15초 유지·단일 실행 → Watcher 등록·dbusmenu 7항목·D-Bus 클릭 일시중지·재개 → 셀프 테스트 8단계(GUI 최대 1개) → 제거 도우미 + `apt-get remove` → 파일·autostart 삭제, 남의 Hook 유지 |
| `ubuntu-24.04-arm`(glibc 2.39, arm64) | 같은 스크립트로 그 러너에서 빌드 | amd64와 같은 항목 |

업데이트 확인은 네 러너 모두 `update_check_failed`(CI 빌드에 업데이트 공개 키 없음, 정상), 로그아웃은 미등록 PC라 `logged_out`(Core "등록되어 있지 않습니다", 서버 요청 없음)입니다. 등록된 PC의 로그아웃·서버 폐기는 `tray/independence_test.go`와 `collector_smoke.py`가 확인합니다. Core 서비스 등록 확인은 Linux에서 하지 않았습니다(러너에 systemd 사용자 세션 없음).

### 이전 기록: GUI 안 트레이(#43, Linux 컨테이너)

#159 전에는 트레이가 GUI 프로세스 안(Linux만)에 있었고, Docker `golang:1.24-bookworm`(Raspberry Pi 5)에서 `tests/poc/collector/desktop/tray-e2e.sh`·`tray-close.sh`로 메뉴 클릭·창 닫기 숨기기·Core 재시작 회복·GUI 종료 뒤 수집 계속을 확인했습니다(2026-10-24). 이 두 스크립트는 기록용으로만 남깁니다.

## 프라이버시

- GUI 설정 `gui.json`에는 서버 주소와 PC 이름 초안만 저장합니다(디렉터리 0700, 파일 0600, 원자적 쓰기, 심볼릭 링크 거부). 같은 폴더의 `tray.lock`·`gui.lock`은 빈 잠금 파일, `gui.request`는 `show`/`quit` 한 단어뿐입니다. 위치: `$AGENT_OFFICE_COLLECTOR_HOME/gui/gui.json`, 없으면 OS 사용자 설정 폴더의 `AgentOffice/CollectorGUI/gui.json`.
- GUI는 Core 설정 파일(`config.json`)과 OS 보안 저장소를 읽지 않고 장치 토큰을 다루지 않습니다. 보관 위치는 Core `status`의 `token_storage`(`os_keyring`·`file_fallback`) 값으로만 표시합니다. 화면에 보내는 Core 출력에서 `aod_` 토큰 형식 문자열을 한 번 더 가립니다.
- 상태·진단 화면은 Core JSON의 enum·개수·시각·오류 코드만 씁니다. 모르는 enum은 `unknown`, 코드 형식이 아닌 오류·개수 키는 버립니다(경로·URL·OS 오류 문자열 차단).
- 등록 성공 시 Core 출력(장치 ID 포함)은 화면에 보이지 않습니다.
- 프런트엔드는 외부 스크립트·스타일·네트워크 요청을 쓰지 않습니다(테스트로 확인).
- 트레이 셀프 테스트 결과 JSON은 OS·항목·결과 코드·상태 enum만 담습니다(경로·주소·토큰·Core 출력 원문 없음). 등록된 PC에서는 로그아웃 단계를 건너뜁니다(`AGENT_OFFICE_TRAY_SELFTEST_LOGOUT=1`일 때만 실행).

## 빌드

공통: Go 1.24, Wails v2.12.0(`go.mod`에 고정, Wails CLI 불필요). Core를 먼저 빌드해 GUI와 같은 폴더에 둡니다. 트레이 도우미는 `go build ./cmd/agent-office-collector-tray`(Linux·Windows는 `CGO_ENABLED=0`, macOS는 cgo)로 같은 폴더에 둡니다.

설치 프로그램 빌드(서명 없음):

```bash
collector/packaging/build-windows-installer.sh <버전> dist/   # Linux + Docker: Windows amd64 교차 빌드 + NSIS
collector/packaging/build-desktop-linux.sh <버전> dist/       # Linux + Docker: 호스트 아키텍처의 desktop deb·rpm
collector/packaging/build.sh <버전> dist/                     # Core 압축본·deb·rpm(#42)
collector/packaging/macos/build-pkg.sh <버전> dist/           # macOS(Xcode 명령행 도구·Go 1.24): 유니버설 .pkg
```

### Linux

검증한 절차(Debian bookworm arm64 컨테이너):

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config   # 트레이 도우미는 C 라이브러리 없음(D-Bus 순수 Go)
cd collector/core && go build -o ../../dist/agent-office-collector ./cmd/agent-office-collector
cd ../gui && go build -tags desktop,production -ldflags "-s -w" -o ../../dist/agent-office-collector-gui .
../../dist/agent-office-collector-gui
```

배포 패키지는 `libwebkit2gtk-4.1-dev`와 `-tags desktop,production,webkit2_41`로 빌드합니다(bookworm `libwebkit2gtk-4.1-dev`로 빌드해 Ubuntu 24.04 amd64·arm64 러너에 설치·실행 확인, #159). webkit2gtk 4.0으로 빌드한 실행 파일은 Ubuntu 24.04에서 실행되지 않습니다(4.0 런타임 없음).

### macOS·Windows(GitHub Actions 러너 검증)

`.github/workflows/cross-os.yml`이 `macos-latest`(arm64, macOS 26.6, Xcode 26.6)와 `windows-latest`(Windows Server 2025, amd64) 러너에서 아래를 실행합니다. 러너는 실제 OS이지만 사람이 쓰는 데스크톱과는 다르므로, 화면을 눈으로 확인하는 항목(창 모양·브라우저 로그인 클릭)은 실장비 확인으로 남깁니다.

```bash
cd collector/gui
go build -tags desktop,production -ldflags "-s -w" -o agent-office-collector-gui .                # macOS
go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o agent-office-collector-gui.exe .  # Windows
```

- macOS: Wails v2.12.0이 `UTType`을 쓰지만 `UniformTypeIdentifiers`를 링크하지 않아 최신 SDK에서 링크가 실패했습니다. `link_darwin.go`(`desktop` 태그)에서 프레임워크를 추가해 고쳤습니다.
- 두 OS 모두 빌드한 GUI가 15초 동안 종료되지 않고 실행되는 것까지 확인합니다.

## 검증

Docker `golang:1.24-bookworm`(Raspberry Pi 5, Linux arm64):

```bash
cd collector/gui
test -z "$(gofmt -l .)"
go vet ./... && GOOS=windows go vet ./internal/... && GOOS=darwin go vet ./internal/...
(cd ../core && go build -o /tmp/ao/agent-office-collector ./cmd/agent-office-collector)
AGENT_OFFICE_TEST_CORE_BIN=/tmp/ao/agent-office-collector go test -race ./...
# GUI 창은 데스크톱 태그에서만 Wails를 링크한다. webkit·gtk 개발 패키지를 설치한 뒤:
go vet -tags desktop,production . && go build -tags desktop,production -o /tmp/ao/agent-office-collector-gui .
GOOS=windows CGO_ENABLED=0 go build -tags desktop,production -o /tmp/ao/gui.exe .   # Windows GUI 교차 빌드(cgo 없음)
```

`AGENT_OFFICE_TEST_CORE_BIN`이 없으면 실제 Core 계약·독립성 테스트는 사유를 남기고 건너뜁니다. CI `Go GUI` 작업은 Core를 빌드해 이 값을 넣고 실행합니다(데스크톱 태그 빌드·트레이 실행은 `Cross-OS Collector`의 설치 실측 작업이 맡음). 독립성 테스트는 실제 Core에 `AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`을 넣어 실행 환경의 키체인을 건드리지 않습니다.

| 테스트 | 내용 |
|---|---|
| `corecli_test.go` | Core 위치 탐색(환경 변수 절대 경로만·같은 폴더·PATH·없음), 종료 코드·시작 실패·시간 초과 시 프로세스 정지, 출력 상한, `status`·`diagnose`·`hooks status` 해석과 깨진 출력 거부, enum·오류 코드·개수 키 정리, 서버 주소·등록 코드·PC 이름 검증 표, 등록 코드가 인자가 아닌 표준 입력으로만 전달되고 잘못된 입력은 Core를 실행하지 않음 |
| `app_test.go` | 서버 저장·loopback 외 http 거부·`/devices` 주소로 브라우저 열기, 등록 성공·실패·이미 등록, Core가 토큰을 흘려도 화면 값에서 가림, `gui.json`에 코드·토큰 없음, 상태·일시중지·재개·진단 호출 인자, Core 없음 안내, Hook 미리보기→승인 순서와 일회성 승인, 자동 시작 승인·활성화 선택 |
| `integration_test.go` | 실제 Core: 미등록 상태 → `httptest` 서버 등록(서버가 받은 코드·이름 확인) → 재등록 거부 → 상태(http·loopback) → 일시중지·재개 → 진단 `reachable` → 가짜 `claude --version`으로 탐지(2.1.308) → Hook 미리보기는 파일 불변 → 설치 후 남의 Hook 보존·상태 `installed` → 제거 → Codex print → systemd unit 파일 설치·상태 `installed`. 화면 값·`gui.json`에 토큰·홈 경로 없음 |
| `prefs_test.go` | 위치 규칙, 0600·0700, 임시 파일 미잔류, 손상 파일·스키마 버전 불일치, 심볼릭 링크 읽기·쓰기 거부 |
| `main_test.go` | `app.js`가 부르는 메서드가 모두 `App`에 있음(바인딩 계약), 외부 리소스·인라인 스크립트 없음, CSP 존재, `device_token` 미참조 |
| `logout_autostart_test.go` | 로그아웃 승인 필수·인자(`logout`, `--local-only`)·서버 실패 안내·출력 토큰 가림, `token_storage` 표시, 자동 시작 상태(absent→installed→outdated)·승인 필수·임시 실행 파일 거부·남의 파일 보존·트레이가 옆에 있으면 트레이를 가리킴, 사무실 주소, 업데이트 확인(채널·최신·새 버전·실패) |
| `tray_test.go` | 상태 → 트레이 표시 표(수집 중·일시중지·Core 정지·폐기·토큰 잠김·미등록·응답 없음, 안전하지 않은 코드 가림), 전송 대기 건수, 메뉴 7항목 동작(서버 없을 때 사무실 열기 = 설정 창, 업데이트 결과 표시, 로그아웃은 서버 폐기 인자로만, 종료 = GUI `--quit` + 트레이 종료, GUI·브라우저 없을 때 코드), Core 응답 없음 → 회복, 상태별 아이콘 PNG·256px·Windows ICO 구조 |
| `siblings_test.go` | Linux·Windows 같은 폴더, macOS 앱 번들 ↔ 트레이 번들 위치(GUI → 트레이, 트레이 → Core·GUI) |
| `launch_test.go` | 브라우저 명령(OS별, http(s)만·사용자 정보·`file:`·`javascript:` 거부), 하위 프로세스 기다림·시간 초과 시 종료·실행 파일 없음 |
| `single_test.go` | 다른 프로세스가 같은 잠금을 못 얻음(ErrHeld), 해제 뒤 다시 얻음 |
| `guictl_test.go` | 두 번째 프로세스의 show·quit 요청이 첫 GUI에 전달, 이전 실행이 남긴 요청은 무시, 첫 GUI가 없으면 요청 파일을 남기지 않음, 알 수 없는 요청 거부 |
| `independence_test.go` | 실제 Core: 별도 프로세스 `run` → GUI 도우미 프로세스 SIGKILL·트레이 종료 뒤 Core 생존·Hook 이벤트 전송 → Core SIGKILL 시 `core_stopped` → 재시작 후 `running` → 트레이 일시중지·재개 → GUI 로그아웃(서버 폐기 1회) → 이후 이벤트 미전송·Core 프로세스 유지. 요청 본문·화면 값에 토큰·홈 경로 없음 |
| `autostart_test.go` | OS별 위치, Desktop Entry `Exec` 인용(공백·`"`·`$`·`%`·`\`·`` ` ``), plist 이스케이프·`KeepAlive` 없음, Windows Run 값 소유 판정 표, 설치·갱신·제거, 남의 파일·심볼릭 링크·남의 Run 값 거부, 임시 파일 미잔류 |
| Core `register_test.go`·`hooks_test.go` | `--code-stdin`(성공, `--code`와 동시 사용·빈 입력·긴 입력 거부), `hooks status --json` 필드와 경로·다른 Hook 명령 미노출, 읽기 실패 코드 |
| 웹 `devices.test.tsx` | `/devices` 경로, 상태 문구(폐기 우선), 남은 시간 계산·만료, 정렬, 열린 코드 상한 문구 |

데스크톱 확인(같은 컨테이너, Xvfb·xdotool): 위 Linux 빌드 → `gui.json`에 서버 주소를 미리 넣고 실행 → 10초 뒤 프로세스 생존, 계정·서버 화면에 저장된 주소가 표시(Go 바인딩 동작), "수집 상태"·"도구 탐지" 탭 클릭 시 Core 결과(미등록, 감지 안 됨 등) 표시 → GUI 종료 뒤 Core `status --json` 정상.

## 알려진 제한·후속

- 장치 토큰 OS 보안 저장소는 Linux Secret Service(컨테이너)와 macOS Keychain·Windows Credential Manager(러너)에서 확인했습니다. 사람이 쓰는 데스크톱의 키체인 잠금·접근 허용 대화상자는 실장비에서 확인해야 합니다.
- 토큰 회전 명령(`/devices/token/rotate`)은 아직 없습니다.
- 등록 후 자동으로 연결 확인(heartbeat 성공 대기)을 하지 않습니다. 수집 상태 화면의 "마지막 heartbeat"로 확인합니다.
- 트레이 아이콘이 실제 패널·메뉴 막대·알림 영역에 그려지는지와 사람의 클릭, Gatekeeper·SmartScreen 대화상자, 재로그인 뒤 자동 시작은 러너에서 자동화할 수 없어 실장비 확인이 남아 있습니다.
- 명세 §10 트레이 "동기화"(즉시 전송)는 Core 명령이 생긴 뒤 추가합니다. 새 버전 OS 알림과 주기적 업데이트 확인, 업데이트 적용 버튼은 업데이트(#44) 후속입니다.
- 코드 서명·공증(Apple Developer ID, Authenticode)은 비용 결정 뒤 별도 Issue입니다. Windows arm64 설치 프로그램과 Linux rpm 데스크톱 패키지 설치는 미검증입니다.

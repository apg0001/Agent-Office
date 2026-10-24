# Collector GUI(Wails)·브라우저 로그인 기기 등록·트레이

관련 이슈: #40 (`EP-06/ISS-01`, 요구사항 COL-001~002, USER-002), #41 (`EP-06/ISS-02`, DEV-001~005, COL-003), #43 (`EP-06/ISS-04`, COL-001~002, COL-010, DIST-007). 결정 근거: [ADR-0003](../decisions/0003-collector-gui-wails-v2.md), [ADR-0004](../decisions/0004-collector-keychain-tray.md).

`collector/gui`는 [Collector CLI](collector-cli.md)(`agent-office-collector`, 이하 Core)를 감싸는 데스크톱 창입니다. 설치 패키지·서명(#42), OS 알림, 업데이트(#44)는 범위가 아닙니다. 트레이·GUI 자동 시작·GUI/Core 독립성은 #43에서 추가했습니다(트레이는 Linux만).

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
| macOS(WKWebView) | **미검증**: 빌드·실행하지 않음. `GOOS=darwin go vet ./internal/...`만 수행 | — |
| Windows(WebView2) | **미검증**: 빌드·실행하지 않음. `GOOS=windows go vet ./internal/...`만 수행. Core 실행 시 콘솔 창 숨김(`CREATE_NO_WINDOW`)도 미확인 | `hidewindow_windows.go` |
| 실제 Agent Office 서버에서 웹 코드 발급 → GUI 등록 | **미실행**: 웹 화면은 Vitest·빌드, 등록은 `httptest` 서버로만 확인 | `devices.test.tsx`, `integration_test.go` |
| 장치 토큰 OS 보안 저장소(#41) — Linux Secret Service | **검증(컨테이너)**: Debian bookworm `gnome-keyring 42.1-1+b2`·`dbus 1.14.10`(`dbus-run-session`, 잠금 해제한 login 모음)에서 저장·읽기·삭제, `register` 뒤 `config.json`에 토큰 없음·`status` `os_keyring`, `logout` 뒤 항목 삭제, 파일 토큰을 `run`이 저장소로 이전 | `tests/poc/collector/desktop/keyring-secret-service.sh`, [Collector CLI](collector-cli.md#장치-토큰-보관41) |
| 장치 토큰 — 실제 데스크톱 세션(GNOME·KDE 로그인 키링 자동 잠금 해제, KWallet) | **미실행** | — |
| 장치 토큰 — macOS Keychain·Windows Credential Manager | **미검증**: `GOOS=darwin\|windows go vet`만. go-keyring 소스에서 macOS가 비밀 값을 표준 입력으로 넘기는 것만 확인 | ADR-0004 |
| 대체 경로(보안 저장소 없음 → 0600 파일, `file_fallback`) | **검증**: 세션 D-Bus 없는 컨테이너·CI에서 `register`가 파일에 저장하고 상태에 `file_fallback` 표시 | `logout_test.go`, 아래 트레이 검증 로그 |
| 로그아웃(Core `logout`, GUI 버튼) | **검증**: 서버 `POST /api/v1/devices/revoke` 호출(장치 토큰 Bearer) → 로컬 토큰·`config.json` 삭제 → 실행 중인 Core가 전송·heartbeat 중지. 서버 오류·네트워크 오류면 아무것도 지우지 않음, `--local-only` | `logout_test.go`, `tray/independence_test.go` |
| 트레이(Linux, StatusNotifierItem) | **검증(컨테이너)**: Xvfb·`dbus-run-session`·최소 `org.kde.StatusNotifierWatcher`(Python)에서 아이콘·툴팁·메뉴 7항목 등록, D-Bus `com.canonical.dbusmenu.Event`로 일시중지·재개·창 열기·GUI 종료 클릭 → Core 상태 반영, Core 강제 종료 시 "Core 실행 중 아님" → 재시작 후 "수집 중" | [트레이 검증](#트레이-검증linux-컨테이너) |
| 창 닫기 → 트레이로 숨기기(Linux) | **검증(컨테이너, openbox 3.6.1)**: Watcher가 있으면 `wmctrl -c`로 닫아도 GUI는 살아 있고 창만 숨김 → 트레이 "창 열기"로 다시 표시. Watcher가 없으면 닫기 = GUI 종료 | 같은 절차 |
| 실제 패널(KDE Plasma, GNOME AppIndicator 확장, XFCE 등)에서 아이콘 표시·클릭 | **미실행**: 컨테이너의 Watcher는 프로토콜 검증용 최소 구현 | — |
| 트레이 — macOS 메뉴 막대·Windows 알림 영역 | **미구현**: Wails v2에 트레이 API가 없고 systray와 메인 루프가 충돌할 수 있어 넣지 않음. 창 닫기 = GUI 종료(Core는 계속) | ADR-0004 |
| GUI 자동 시작(Linux XDG autostart) | **검증(파일)**: 생성 `.desktop`이 `desktop-file-validate`(desktop-file-utils 0.26) 통과, 소유 표시 없는 파일 교체·삭제 거부, 심볼릭 링크 거부. 실제 로그인 시 실행은 **미실행** | `autostart_test.go`, `autostart-validate.sh` |
| GUI 자동 시작(macOS LaunchAgent·Windows HKCU Run) | **미검증**: plist는 `xmllint` 형식만, Windows는 메모리 레지스트리로 소유 판정·교체 거부만 확인 | `autostart_test.go` |
| GUI/Core 독립성 | **검증**: 실제 Core `run`을 별도 프로세스로 띄우고 GUI 프로세스를 SIGKILL·트레이 종료로 끝내도 Core가 계속 Hook 이벤트를 전송, Core 강제 종료·재시작 시 트레이 상태 회복 | `tray/independence_test.go`(CI `Go GUI`) |
| 재부팅 후 자동 수집(Core 서비스 + GUI 자동 시작) | **미실행**: 컨테이너에 systemd 사용자 세션·로그인 세션 없음 | [Collector CLI 자동 시작](collector-cli.md#자동-시작service) |

## 구조

```text
collector/gui
├ main.go                 Wails v2.12.0 창(빌드 태그 desktop,production에서만 실행)
├ frontend/assets/        정적 HTML·JS·CSS(빌드 도구·npm 의존성 없음, CSP 'self')
└ internal/
  ├ corecli/              Core 실행 파일 찾기·실행(시간 제한, 출력 상한)·JSON 해석·입력 검증
  ├ app/                  Wails에 바인딩되는 App(화면 없이 단위 테스트)
  ├ prefs/                GUI 설정 gui.json(서버 주소·PC 이름 초안만)
  ├ tray/                 트레이 상태·메뉴 동작·창 닫기 판단(화면 없이 테스트), GUI/Core 독립성 테스트
  ├ autostart/            GUI 자동 시작 정의(XDG .desktop·LaunchAgent·HKCU Run) 생성·설치·제거
  └ fakecore/             테스트용 가짜 Core
tray_linux.go             fyne.io/systray v1.12.2 트레이(빌드 태그 desktop && linux)
tray_stub.go              그 밖의 빌드: 트레이 없음
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
| 트레이 상태(5초마다)·일시중지·재개 | `status --json`, `pause`, `resume` |

- 창을 닫거나 GUI가 비정상 종료해도 자동 시작된 Core는 계속 수집합니다(`tray/independence_test.go`: GUI 프로세스 SIGKILL·트레이 종료 뒤에도 Core가 Hook 이벤트 전송). GUI는 `run`을 직접 띄우지 않으므로, 자동 시작을 설치·활성화하지 않았으면 수집 상태 화면에 "Core 프로세스: 실행 중 아님"이 보입니다.
- Core 위치: `$AGENT_OFFICE_COLLECTOR_BIN`(절대 경로만) → GUI 실행 파일과 같은 폴더의 `agent-office-collector[.exe]` → `PATH`. 못 찾으면 화면 위에 안내가 나옵니다.
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

## 트레이·GUI 자동 시작(#43)

### 트레이(Linux)

| 항목 | 동작 |
|---|---|
| 아이콘·툴팁 | 상태별 색(수집 중 초록, 일시중지 주황, 오류 빨강, 미등록·Core 정지 회색)과 "Agent Office Collector — <상태>" |
| 상태(비활성 항목) | 수집 중 / 일시중지 / Core 실행 중 아님 / 기기 등록 필요 / 장치 폐기됨 / 보안 저장소 잠김 / Core 응답 없음 |
| 창 열기 | 숨긴 창을 보이고 앞으로 |
| 수집 일시중지·재개 | Core `pause`·`resume` 후 바로 상태 갱신(현재 상태에 맞는 항목만 활성) |
| GUI 종료(수집은 계속) | GUI 프로세스만 끝냅니다. Core `run`은 별도 프로세스라 계속 수집합니다 |

- 상태는 5초마다 Core `status --json`으로 읽습니다. Core가 멈추면 "Core 실행 중 아님"(실행 잠금 없음), 다시 시작하면 다음 주기에 "수집 중"으로 돌아옵니다. GUI는 Core를 직접 띄우지 않습니다.
- 세션 D-Bus 주소가 없으면 트레이를 시작하지 않습니다(D-Bus 자동 실행 방지). 세션 버스에 `org.kde.StatusNotifierWatcher`가 있을 때만 창 닫기(X)를 숨기기로 바꿉니다. GNOME 기본 셸처럼 Watcher가 없으면 아이콘이 보이지 않으므로 닫기는 GUI 종료입니다(AppIndicator 확장을 켜면 트레이 사용 가능, 미검증).
- GUI는 한 번만 실행됩니다(Wails `SingleInstanceLock`). 두 번째 실행은 곧바로 끝나고(종료 코드 1, 컨테이너 확인) 기존 창을 띄웁니다.
- 툴팁·메뉴 문구는 enum에서 만든 고정 한국어뿐이고 서버 주소·경로·토큰을 넣지 않습니다.
- macOS·Windows는 트레이가 없습니다(ADR-0004). 창 닫기는 GUI 종료이며 Core 자동 시작을 설치했다면 수집은 계속됩니다.

### GUI 자동 시작

"자동 시작" 화면 아래 **GUI(트레이) 자동 시작**은 Core 서비스(`service install`)와 별개입니다. 꺼도 수집은 계속되고, 켜면 로그인할 때 GUI를 `--autostart`로 띄웁니다. 트레이가 보이면 창 없이 시작하고, 아니면 창을 엽니다.

| OS | 위치 | 소유 판정 |
|---|---|---|
| Linux | `${XDG_CONFIG_HOME:-~/.config}/autostart/agent-office-collector-gui.desktop` | 본문의 `agent-office-collector-gui:managed-autostart:v1`(주석·`X-AgentOffice-Managed`) |
| macOS(미검증) | `~/Library/LaunchAgents/io.github.apg0001.agent-office.collector-gui.plist`(`RunAtLoad`, `LimitLoadToSessionType=Aqua`, `KeepAlive` 없음, 다음 로그인부터) | 같은 표시(XML 주석) |
| Windows(미검증) | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 값 `AgentOfficeCollectorGUI` = `"<경로>\agent-office-collector-gui.exe" --autostart` | 값이 정확히 이 형식(실행 파일 이름 + `--autostart`)일 때만 |

- 설치·제거 모두 확인 창을 거칩니다. 소유 표시가 없는 같은 이름 파일·값은 덮어쓰거나 지우지 않습니다. 파일은 같은 디렉터리 임시 파일 → fsync → rename으로 쓰고 심볼릭 링크는 따라가지 않습니다. 관리자 권한을 쓰지 않습니다.
- `go run`·`go test` 임시 실행 파일 경로는 거부합니다. GUI를 옮겼으면 상태가 "이전 위치"로 보이고 다시 켜면 갱신합니다.
- 화면에는 위치를 `~`로 줄여 보여 줍니다.

### 트레이 검증(Linux 컨테이너)

Docker `golang:1.24-bookworm`(Raspberry Pi 5, Linux arm64)에서 [빌드](#linux)한 두 실행 파일을 `tests/poc/collector/desktop/tray-e2e.sh`·`tray-close.sh`로 확인했습니다(`libwebkit2gtk-4.0-37 2.50.6-1~deb12u2`, `libgtk-3-0 3.24.38-2~deb12u3`, `xvfb 21.1.7-3+deb12u13`, `dbus 1.14.10-1~deb12u1`, `python3-dbus 1.3.2-4+b1`, `openbox 3.6.1-10`, `wmctrl 1.07-7+b1`).

```bash
# 저장소 루트. dist/에 agent-office-collector와 agent-office-collector-gui를 먼저 빌드
docker run --rm --tmpfs /w:exec,size=1g -v "$PWD/tests/poc/collector/desktop:/scripts:ro" -v "$PWD/dist:/bin-in:ro" golang:1.24-bookworm sh /scripts/tray-e2e.sh
docker run --rm --tmpfs /w:exec,size=1g -v "$PWD/tests/poc/collector/desktop:/scripts:ro" -v "$PWD/dist:/bin-in:ro" golang:1.24-bookworm sh /scripts/tray-close.sh
```

확인 결과(2026-10-24): 합성 서버(`fake_office.py`)로 등록(`file_fallback`) → `--autostart` 실행 시 보이는 창 없음 → Watcher에 `/StatusNotifierItem` 등록, 22×22 아이콘, 툴팁 "수집 중" → 메뉴 "수집 일시중지" 클릭 → Core `paused: true`, 메뉴 상태 "일시중지"·재개만 활성 → "수집 재개" → `paused: false` → "창 열기" → 창 표시 → 두 번째 실행 종료 코드 1·첫 GUI 유지 → Core SIGKILL 뒤 "Core 실행 중 아님", 재시작 뒤 "수집 중" → "GUI 종료" → GUI 종료, Core 계속 실행 → Hook 이벤트 1건 서버 수신. 닫기: Watcher 없음 → GUI 종료, Watcher 있음 → 숨김 → 트레이로 다시 표시 → 트레이 종료. GUI·Core 로그에 sentinel 토큰 0건.

## 프라이버시

- GUI 설정 `gui.json`에는 서버 주소와 PC 이름 초안만 저장합니다(디렉터리 0700, 파일 0600, 원자적 쓰기, 심볼릭 링크 거부). 위치: `$AGENT_OFFICE_COLLECTOR_HOME/gui/gui.json`, 없으면 OS 사용자 설정 폴더의 `AgentOffice/CollectorGUI/gui.json`.
- GUI는 Core 설정 파일(`config.json`)과 OS 보안 저장소를 읽지 않고 장치 토큰을 다루지 않습니다. 보관 위치는 Core `status`의 `token_storage`(`os_keyring`·`file_fallback`) 값으로만 표시합니다. 화면에 보내는 Core 출력에서 `aod_` 토큰 형식 문자열을 한 번 더 가립니다.
- 상태·진단 화면은 Core JSON의 enum·개수·시각·오류 코드만 씁니다. 모르는 enum은 `unknown`, 코드 형식이 아닌 오류·개수 키는 버립니다(경로·URL·OS 오류 문자열 차단).
- 등록 성공 시 Core 출력(장치 ID 포함)은 화면에 보이지 않습니다.
- 프런트엔드는 외부 스크립트·스타일·네트워크 요청을 쓰지 않습니다(테스트로 확인).

## 빌드

공통: Go 1.24, Wails v2.12.0(`go.mod`에 고정, Wails CLI 불필요). Core를 먼저 빌드해 GUI와 같은 폴더에 둡니다.

### Linux

검증한 절차(Debian bookworm arm64 컨테이너):

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config   # 트레이는 추가 C 라이브러리 없음(D-Bus 순수 Go)
cd collector/core && go build -o ../../dist/agent-office-collector ./cmd/agent-office-collector
cd ../gui && go build -tags desktop,production -ldflags "-s -w" -o ../../dist/agent-office-collector-gui .
../../dist/agent-office-collector-gui
```

webkit2gtk 4.1만 있는 배포판은 `libwebkit2gtk-4.1-dev`와 `-tags desktop,production,webkit2_41`을 시도하세요(미검증).

### macOS·Windows(미검증)

Wails v2 문서 기준으로 macOS는 Xcode 명령줄 도구, Windows는 WebView2 런타임이 필요합니다. 같은 `go build -tags desktop,production` 명령이 동작할 것으로 예상하지만 이 저장소에서 실행하지 않았습니다. Windows는 콘솔 창을 숨기려면 `-ldflags "-H windowsgui"`가 필요할 수 있습니다. 실장비 검증 전까지 지원으로 표시하지 않습니다.

## 검증

Docker `golang:1.24-bookworm`(Raspberry Pi 5, Linux arm64):

```bash
cd collector/gui
test -z "$(gofmt -l .)"
go vet ./... && GOOS=windows go vet ./internal/... && GOOS=darwin go vet ./internal/...
(cd ../core && go build -o /tmp/ao/agent-office-collector ./cmd/agent-office-collector)
AGENT_OFFICE_TEST_CORE_BIN=/tmp/ao/agent-office-collector go test -race ./...
# 트레이(tray_linux.go)는 데스크톱 태그에서만 컴파일된다. webkit·gtk 개발 패키지를 설치한 뒤:
go vet -tags desktop,production . && go build -tags desktop,production -o /tmp/ao/agent-office-collector-gui .
```

`AGENT_OFFICE_TEST_CORE_BIN`이 없으면 실제 Core 계약·독립성 테스트는 사유를 남기고 건너뜁니다. CI `Go GUI` 작업은 Core를 빌드해 이 값을 넣고 실행합니다(데스크톱 태그 빌드·트레이는 CI에서 실행하지 않음). 독립성 테스트는 실제 Core에 `AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`을 넣어 실행 환경의 키체인을 건드리지 않습니다.

| 테스트 | 내용 |
|---|---|
| `corecli_test.go` | Core 위치 탐색(환경 변수 절대 경로만·같은 폴더·PATH·없음), 종료 코드·시작 실패·시간 초과 시 프로세스 정지, 출력 상한, `status`·`diagnose`·`hooks status` 해석과 깨진 출력 거부, enum·오류 코드·개수 키 정리, 서버 주소·등록 코드·PC 이름 검증 표, 등록 코드가 인자가 아닌 표준 입력으로만 전달되고 잘못된 입력은 Core를 실행하지 않음 |
| `app_test.go` | 서버 저장·loopback 외 http 거부·`/devices` 주소로 브라우저 열기, 등록 성공·실패·이미 등록, Core가 토큰을 흘려도 화면 값에서 가림, `gui.json`에 코드·토큰 없음, 상태·일시중지·재개·진단 호출 인자, Core 없음 안내, Hook 미리보기→승인 순서와 일회성 승인, 자동 시작 승인·활성화 선택 |
| `integration_test.go` | 실제 Core: 미등록 상태 → `httptest` 서버 등록(서버가 받은 코드·이름 확인) → 재등록 거부 → 상태(http·loopback) → 일시중지·재개 → 진단 `reachable` → 가짜 `claude --version`으로 탐지(2.1.308) → Hook 미리보기는 파일 불변 → 설치 후 남의 Hook 보존·상태 `installed` → 제거 → Codex print → systemd unit 파일 설치·상태 `installed`. 화면 값·`gui.json`에 토큰·홈 경로 없음 |
| `prefs_test.go` | 위치 규칙, 0600·0700, 임시 파일 미잔류, 손상 파일·스키마 버전 불일치, 심볼릭 링크 읽기·쓰기 거부 |
| `main_test.go` | `app.js`가 부르는 메서드가 모두 `App`에 있음(바인딩 계약), 외부 리소스·인라인 스크립트 없음, CSP 존재, `device_token` 미참조 |
| `logout_autostart_test.go` | 로그아웃 승인 필수·인자(`logout`, `--local-only`)·서버 실패 안내·출력 토큰 가림, `token_storage` 표시, GUI 자동 시작 상태(absent→installed→outdated)·승인 필수·임시 실행 파일 거부·남의 파일 보존 |
| `tray_test.go` | 상태 → 트레이 표시 표(수집 중·일시중지·Core 정지·폐기·토큰 잠김·미등록·응답 없음, 안전하지 않은 코드 가림), 메뉴 동작, 창 닫기 = 숨기기(트레이 보일 때만)·트레이 종료는 숨기지 않음, `--autostart` 숨김 시작 조건, Core 응답 없음 → 회복, 상태별 아이콘 PNG |
| `independence_test.go` | 실제 Core: 별도 프로세스 `run` → GUI 도우미 프로세스 SIGKILL·트레이 종료 뒤 Core 생존·Hook 이벤트 전송 → Core SIGKILL 시 `core_stopped` → 재시작 후 `running` → 트레이 일시중지·재개 → GUI 로그아웃(서버 폐기 1회) → 이후 이벤트 미전송·Core 프로세스 유지. 요청 본문·화면 값에 토큰·홈 경로 없음 |
| `autostart_test.go` | OS별 위치, Desktop Entry `Exec` 인용(공백·`"`·`$`·`%`·`\`·`` ` ``), plist 이스케이프·`KeepAlive` 없음, Windows Run 값 소유 판정 표, 설치·갱신·제거, 남의 파일·심볼릭 링크·남의 Run 값 거부, 임시 파일 미잔류 |
| Core `register_test.go`·`hooks_test.go` | `--code-stdin`(성공, `--code`와 동시 사용·빈 입력·긴 입력 거부), `hooks status --json` 필드와 경로·다른 Hook 명령 미노출, 읽기 실패 코드 |
| 웹 `devices.test.tsx` | `/devices` 경로, 상태 문구(폐기 우선), 남은 시간 계산·만료, 정렬, 열린 코드 상한 문구 |

데스크톱 확인(같은 컨테이너, Xvfb·xdotool): 위 Linux 빌드 → `gui.json`에 서버 주소를 미리 넣고 실행 → 10초 뒤 프로세스 생존, 계정·서버 화면에 저장된 주소가 표시(Go 바인딩 동작), "수집 상태"·"도구 탐지" 탭 클릭 시 Core 결과(미등록, 감지 안 됨 등) 표시 → GUI 종료 뒤 Core `status --json` 정상.

## 알려진 제한·후속

- 장치 토큰 OS 보안 저장소는 Linux Secret Service만 컨테이너에서 확인했습니다. macOS Keychain(첫 접근 시 허용 프롬프트 등)·Windows Credential Manager는 실장비 검증 전까지 지원으로 표시하지 않습니다.
- 토큰 회전 명령(`/devices/token/rotate`)은 아직 없습니다.
- 등록 후 자동으로 연결 확인(heartbeat 성공 대기)을 하지 않습니다. 수집 상태 화면의 "마지막 heartbeat"로 확인합니다.
- 트레이는 Linux만 있습니다(macOS·Windows는 ADR-0004에 따라 후속 PoC). OS 알림, 설치 패키지(#42), 업데이트(#44)는 범위가 아닙니다.
- 재부팅 뒤 Core 서비스·GUI 자동 시작이 실제로 뜨는지는 실제 로그인 세션에서 확인해야 합니다.
- macOS·Windows와 실제 데스크톱 세션(브라우저 열기 포함)은 실장비에서 확인해야 합니다(#62 또는 별도 Issue).

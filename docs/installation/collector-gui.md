# Collector GUI(Wails)·브라우저 로그인 기기 등록

관련 이슈: #40 (`EP-06/ISS-01`, 요구사항 COL-001~002, USER-002), #41 (`EP-06/ISS-02`, DEV-001~005, COL-003). 결정 근거: [ADR-0003](../decisions/0003-collector-gui-wails-v2.md).

`collector/gui`는 [Collector CLI](collector-cli.md)(`agent-office-collector`, 이하 Core)를 감싸는 데스크톱 창입니다. 설치 패키지·서명(#42), 트레이·OS 알림(#43), 업데이트(#44)는 범위가 아닙니다.

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
| OS 보안 저장소(Keychain·DPAPI·Secret Service) 토큰 보관 | **미구현**: 장치 토큰은 여전히 Core 설정 파일(0600)에 있습니다. 후속 Issue 필요 | [제한](#알려진-제한후속) |

## 구조

```text
collector/gui
├ main.go                 Wails v2.12.0 창(빌드 태그 desktop,production에서만 실행)
├ frontend/assets/        정적 HTML·JS·CSS(빌드 도구·npm 의존성 없음, CSP 'self')
└ internal/
  ├ corecli/              Core 실행 파일 찾기·실행(시간 제한, 출력 상한)·JSON 해석·입력 검증
  ├ app/                  Wails에 바인딩되는 App(화면 없이 단위 테스트)
  ├ prefs/                GUI 설정 gui.json(서버 주소·PC 이름 초안만)
  └ fakecore/             테스트용 가짜 Core
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

- 창을 닫거나 GUI가 비정상 종료해도 자동 시작된 Core는 계속 수집합니다(Xvfb에서 GUI 종료 뒤 Core `status` 실행 확인). GUI는 `run`을 직접 띄우지 않으므로, 자동 시작을 설치·활성화하지 않았으면 수집 상태 화면에 "Core 프로세스: 실행 중 아님"이 보입니다.
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
Core: device_id·장치 토큰을 Core 설정 파일(0600)에 저장. GUI에는 결과 코드만 돌아옴
```

- GUI는 계정 이메일·비밀번호를 받지 않습니다. 로그인은 브라우저에서만 합니다.
- 서버 주소 규칙은 Core와 같습니다: https만, http는 `localhost`·`127.x`·`::1`만, 경로·쿼리·조각·사용자 정보 없음.
- 등록 코드는 서버 알파벳(`ABCDEFGHJKMNPQRSTVWXYZ23456789`) 8자만 받고, 공백·하이픈·소문자는 정리합니다. 형식이 틀리면 Core를 실행하지 않습니다. 입력란은 제출 직후 비웁니다.
- 이미 등록된 PC는 "새 장치로 다시 등록"을 선택해야 `--force`로 다시 등록합니다.
- 웹 `/devices`(로그인 필요, 상단 "PC" 링크)는 코드 발급·남은 시간 표시와 내 PC 목록(OS, Collector 버전, 연결 상태, 마지막 연결)·폐기를 제공합니다. 코드는 화면 상태에만 있고 저장하지 않습니다.

### Hook 연동 명시적 동의

1. "설치 미리보기"로 대상 파일(홈은 `~`), 이벤트별 변경, 추가할 핸들러를 보여 줍니다(파일 변경 없음).
2. 미리보기를 본 뒤에만 "내용을 확인했고 설치에 동의" 버튼이 켜지고, 누르면 확인 창을 한 번 더 띄웁니다.
3. 백엔드도 같은 실행에서 미리보기를 거치지 않았거나 승인 값이 없으면 `--yes`를 보내지 않습니다. 한 번의 승인은 한 번만 씁니다.
4. 백업·원자적 쓰기·소유 항목만 변경·제거는 Core 규칙([Hook 설치](collector-cli.md#hook-설치hooks-31))을 그대로 따릅니다.

## 프라이버시

- GUI 설정 `gui.json`에는 서버 주소와 PC 이름 초안만 저장합니다(디렉터리 0700, 파일 0600, 원자적 쓰기, 심볼릭 링크 거부). 위치: `$AGENT_OFFICE_COLLECTOR_HOME/gui/gui.json`, 없으면 OS 사용자 설정 폴더의 `AgentOffice/CollectorGUI/gui.json`.
- GUI는 Core 설정 파일(`config.json`)을 읽지 않고 장치 토큰을 다루지 않습니다. 화면에 보내는 Core 출력에서 `aod_` 토큰 형식 문자열을 한 번 더 가립니다.
- 상태·진단 화면은 Core JSON의 enum·개수·시각·오류 코드만 씁니다. 모르는 enum은 `unknown`, 코드 형식이 아닌 오류·개수 키는 버립니다(경로·URL·OS 오류 문자열 차단).
- 등록 성공 시 Core 출력(장치 ID 포함)은 화면에 보이지 않습니다.
- 프런트엔드는 외부 스크립트·스타일·네트워크 요청을 쓰지 않습니다(테스트로 확인).

## 빌드

공통: Go 1.24, Wails v2.12.0(`go.mod`에 고정, Wails CLI 불필요). Core를 먼저 빌드해 GUI와 같은 폴더에 둡니다.

### Linux

검증한 절차(Debian bookworm arm64 컨테이너):

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
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
```

`AGENT_OFFICE_TEST_CORE_BIN`이 없으면 실제 Core 계약 테스트는 사유를 남기고 건너뜁니다. CI `Go GUI` 작업은 Core를 빌드해 이 값을 넣고 실행합니다.

| 테스트 | 내용 |
|---|---|
| `corecli_test.go` | Core 위치 탐색(환경 변수 절대 경로만·같은 폴더·PATH·없음), 종료 코드·시작 실패·시간 초과 시 프로세스 정지, 출력 상한, `status`·`diagnose`·`hooks status` 해석과 깨진 출력 거부, enum·오류 코드·개수 키 정리, 서버 주소·등록 코드·PC 이름 검증 표, 등록 코드가 인자가 아닌 표준 입력으로만 전달되고 잘못된 입력은 Core를 실행하지 않음 |
| `app_test.go` | 서버 저장·loopback 외 http 거부·`/devices` 주소로 브라우저 열기, 등록 성공·실패·이미 등록, Core가 토큰을 흘려도 화면 값에서 가림, `gui.json`에 코드·토큰 없음, 상태·일시중지·재개·진단 호출 인자, Core 없음 안내, Hook 미리보기→승인 순서와 일회성 승인, 자동 시작 승인·활성화 선택 |
| `integration_test.go` | 실제 Core: 미등록 상태 → `httptest` 서버 등록(서버가 받은 코드·이름 확인) → 재등록 거부 → 상태(http·loopback) → 일시중지·재개 → 진단 `reachable` → 가짜 `claude --version`으로 탐지(2.1.308) → Hook 미리보기는 파일 불변 → 설치 후 남의 Hook 보존·상태 `installed` → 제거 → Codex print → systemd unit 파일 설치·상태 `installed`. 화면 값·`gui.json`에 토큰·홈 경로 없음 |
| `prefs_test.go` | 위치 규칙, 0600·0700, 임시 파일 미잔류, 손상 파일·스키마 버전 불일치, 심볼릭 링크 읽기·쓰기 거부 |
| `main_test.go` | `app.js`가 부르는 메서드가 모두 `App`에 있음(바인딩 계약), 외부 리소스·인라인 스크립트 없음, CSP 존재, `device_token` 미참조 |
| Core `register_test.go`·`hooks_test.go` | `--code-stdin`(성공, `--code`와 동시 사용·빈 입력·긴 입력 거부), `hooks status --json` 필드와 경로·다른 Hook 명령 미노출, 읽기 실패 코드 |
| 웹 `devices.test.tsx` | `/devices` 경로, 상태 문구(폐기 우선), 남은 시간 계산·만료, 정렬, 열린 코드 상한 문구 |

데스크톱 확인(같은 컨테이너, Xvfb·xdotool): 위 Linux 빌드 → `gui.json`에 서버 주소를 미리 넣고 실행 → 10초 뒤 프로세스 생존, 계정·서버 화면에 저장된 주소가 표시(Go 바인딩 동작), "수집 상태"·"도구 탐지" 탭 클릭 시 Core 결과(미등록, 감지 안 됨 등) 표시 → GUI 종료 뒤 Core `status --json` 정상.

## 알려진 제한·후속

- **OS 보안 저장소 미구현**: 사양은 민감 토큰을 OS 보안 저장소에 두라고 하지만 장치 토큰은 아직 Core 설정 파일(0600)에 있습니다. Keychain·DPAPI·Secret Service 연동과 기존 설정 이전은 별도 Issue로 남깁니다.
- 로그아웃(Core의 `/devices/revoke` 호출)과 토큰 회전 명령이 Core에 없어 GUI에도 없습니다. 웹 `/devices`에서 PC를 폐기할 수는 있습니다.
- 등록 후 자동으로 연결 확인(heartbeat 성공 대기)을 하지 않습니다. 수집 상태 화면의 "마지막 heartbeat"로 확인합니다.
- 트레이·OS 알림·창 닫기 시 트레이로 숨기기(#43), 설치 패키지(#42), 업데이트(#44)는 범위가 아닙니다.
- macOS·Windows와 실제 데스크톱 세션(브라우저 열기 포함)은 실장비에서 확인해야 합니다(#62 또는 별도 Issue).

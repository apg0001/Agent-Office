# ADR-0004: 장치 토큰은 go-keyring으로 OS 보안 저장소에, 트레이는 Linux에서만 fyne.io/systray로

- 상태: 제안됨(#41·#43 구현 근거, 소유자 검토 대기)
- 날짜: 2026-10-24
- 관련 이슈: #41, #43. [ADR-0003](0003-collector-gui-wails-v2.md) 결정 5를 구체화합니다.

## 맥락

- 사양은 장치 토큰을 OS 보안 저장소에 두라고 합니다. #34~#41까지 토큰은 Core 설정 파일(`config.json`, 0600)에 있었습니다.
- Core는 Go 1.24·cgo 없이 Linux·macOS·Windows로 교차 빌드합니다(`modernc.org/sqlite`). 보안 저장소 라이브러리도 cgo가 없어야 합니다.
- Wails v2.12.0에는 시스템 트레이 API가 없습니다(트레이는 v3 기능). Go 1.24에서 쓸 수 있는 Wails v2 최신이 v2.12.0입니다(ADR-0003).
- 개발·검증 환경은 Raspberry Pi 5(Linux arm64) Docker뿐이고 macOS·Windows 실장비가 없습니다.

## 결정

1. **보안 저장소 라이브러리**: `github.com/zalando/go-keyring` **v0.2.8**(2026-03, `go 1.18`, 의존성 `godbus/dbus/v5 v5.2.2`·`danieljoos/wincred v1.2.3`)을 정확한 버전으로 고정합니다.
   - Linux: Secret Service(D-Bus). macOS: `/usr/bin/security`(비밀 값은 명령 인자가 아니라 `security -i` 표준 입력으로 전달, 소스 확인). Windows: Credential Manager(`wincred`, 순수 Go syscall).
   - 대안: `99designs/keyring`(파일·pass·KWallet 등 백엔드가 많지만 의존성이 크고 macOS는 cgo 경로가 있음), 직접 D-Bus 구현(유지 비용). 의존성이 가장 작고 cgo가 없는 go-keyring을 고릅니다.
   - Core `golang.org/x/sys`(v0.37.0)·`modernc.org/sqlite`(v1.46.1)는 바뀌지 않았습니다.
2. **보관 규칙**: 항목 서비스 `io.github.apg0001.agent-office.collector`, 계정 = 장치 ID. 저장 뒤 다시 읽어 같을 때만 `config.json`에 `token_store: "keyring"`을 쓰고 토큰 필드를 비웁니다. 저장소를 쓸 수 없으면(세션 D-Bus 없음·잠김·응답 없음 10초) 0600 파일에 두고 `status`에 `token_storage: "file_fallback"`을 표시합니다. `AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`이면 저장소를 쓰지 않습니다. Linux에서 세션 버스 주소가 없으면 D-Bus 자동 실행(`dbus-launch`)을 피하려고 시도하지 않습니다.
3. **이전**: `run`이 시작(또는 등록 정보 변경으로 재시작)할 때 파일 토큰을 저장소로 옮기고 파일에서 지웁니다. 실패하면 파일 보관을 유지하고 다음 시작에서 다시 시도합니다.
4. **로그아웃**: Core `logout`은 기존 서버 API `POST /api/v1/devices/revoke`(장치 토큰 인증, #26)를 호출합니다. 서버에 새 엔드포인트를 추가하지 않습니다.
5. **트레이**: `fyne.io/systray` **v1.12.2**(`go 1.19`)를 **Linux 데스크톱 빌드에서만**(`//go:build desktop && linux`) 씁니다. Linux 구현은 StatusNotifierItem·dbusmenu를 D-Bus로만 내보내 cgo·GTK가 없고, Wails의 GTK 메인 루프와 다투지 않습니다(`RunWithExternalLoop`).
   - macOS: systray가 자체 NSApplication 델리게이트·메인 스레드 루프를 쓰므로 같은 프로세스의 Wails와 충돌할 수 있어 넣지 않습니다.
   - Windows: systray의 `RunWithExternalLoop`는 창을 만든 스레드와 다른 고루틴에서 메시지 루프를 돌려(v1.12.2 소스) 동작을 보장할 수 없어 넣지 않습니다.
   - 두 OS는 트레이 없음으로 동작합니다(창 닫기 = GUI 종료, Core는 계속 수집). 실장비 PoC 뒤 별도 도우미 프로세스 또는 Wails v3로 다시 결정합니다.
6. **창 닫기**: 트레이 아이콘이 실제로 보일 수 있을 때(세션 버스에 `org.kde.StatusNotifierWatcher`가 있을 때)만 창 닫기를 숨기기로 바꿉니다. GNOME 기본 셸처럼 Watcher가 없으면 닫기는 GUI 종료입니다(창을 다시 열 수단이 없어지므로).
7. **GUI 자동 시작**: Core 서비스와 별개로 XDG autostart `.desktop`(Linux), LaunchAgent plist(macOS, `KeepAlive` 없음), HKCU `Run` 값(Windows)을 씁니다. 소유 표시가 없는 항목은 바꾸거나 지우지 않습니다. `--autostart`로 실행되고 트레이가 보이면 창 없이 시작합니다.

## 결과

- 보안 저장소: Linux는 컨테이너의 gnome-keyring 42.1(Secret Service)로 저장·읽기·삭제·register→logout·파일 토큰 이전을 실제로 확인했습니다. macOS Keychain·Windows Credential Manager는 미검증입니다(`GOOS=darwin|windows go vet`만).
- 트레이: Linux arm64 컨테이너(Xvfb·openbox·dbus 1.14.10·최소 StatusNotifierWatcher)에서 D-Bus로 메뉴 클릭까지 확인했습니다. 실제 패널(KDE Plasma, GNOME AppIndicator 확장 등) 표시는 미검증입니다.
- GUI 실행 파일이 `fyne.io/systray`·`godbus/dbus/v5 v5.1.0`(Wails와 같은 버전)·`golang.org/x/sys v0.30.0`에 직접 의존합니다.
- 상태 JSON에 `token_storage`, 실행 상태 enum에 `token_unavailable`이 추가됐습니다(GUI 계약, `integration_test.go`).

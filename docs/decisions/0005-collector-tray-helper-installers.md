# ADR-0005: 트레이는 Wails 창과 다른 프로세스(agent-office-collector-tray)로, 설치 프로그램은 서명 없이 OS별 표준 형식으로

- 상태: 제안됨(#159 구현 근거, 소유자 검토 대기)
- 날짜: 2026-10-26
- 관련 이슈: #159. [ADR-0004](0004-collector-keychain-tray.md) 결정 5·6·7(트레이 Linux 전용, 창 닫기 숨기기, GUI 자동 시작)을 대체합니다.
- 보완: [ADR-0006](0006-collector-installer-auto-update.md)(#180)이 결정 2의 "업데이트 확인(적용하지 않음)"을 승인형 설치("업데이트 설치 (vX.Y.Z)" 항목)로, 결정 6의 "설치 프로그램은 아무것도 켜지 않음"을 첫 실행 마법사(한 화면 동의)로, 결정 7을 설치 파일 기반 업데이트로 보완합니다. Linux 데스크톱 GUI 빌드 기반은 bookworm에서 ubuntu:22.04로 바뀌었습니다.

## 맥락

- 마스터 명세 §10은 Windows·macOS·Linux GUI 설치 패키지, OS별 실제 설치·제거·트레이·자동 시작 검증, 트레이 메뉴(사무실 열기, 설정, 세션 상태, 일시중지/재개, 동기화, 업데이트 확인, 로그아웃, 종료)를 요구합니다.
- Wails v2.12.0(Go 1.24에서 쓸 수 있는 최신, ADR-0003)에는 트레이 API가 없습니다. ADR-0004는 `fyne.io/systray`를 같은 프로세스에 붙이면 macOS는 NSApplication 메인 루프를, Windows는 메시지 루프 스레드를 Wails와 다투므로 Linux에서만 쓰기로 했습니다.
- `fyne.io/systray` v1.12.2 소스(2026-10-26 확인): `init()`에서 `runtime.LockOSThread()`, `Run()`은 메인 스레드에서 macOS `[NSApp run]`, Windows `GetMessage` 루프, Linux D-Bus(StatusNotifierItem·dbusmenu, cgo 없음)를 돌립니다. Windows는 `Shell_NotifyIcon(NIM_ADD)`이 성공해야 `onReady`를 부르고, 아이콘은 `LoadImage(IMAGE_ICON)`로 읽어 ICO 형식이 필요합니다. macOS는 `[NSApp stop:]`으로 루프가 돌아옵니다.
- Wails v2.12.0 `SingleInstanceLock`은 macOS(잠금 파일 + 분산 알림)·Linux(D-Bus 이름)에서는 두 번째 실행의 인자를 첫 프로세스로 넘기지만, Windows는 첫 프로세스가 만든 메시지 전용 창(`HWND_MESSAGE`)을 `FindWindowW`로 찾아 찾지 못하고 두 번째 GUI가 그대로 뜹니다(소스 확인, `windows-latest` 러너에서 트레이 "종료" 뒤 GUI가 남는 것으로 실측). Linux는 `OnStartup`이 GTK 메인 루프보다 먼저 돌아 그 안의 `Quit`이 무시됩니다(`gtk_main_quit: main_loops != NULL`, 컨테이너 실측).
- 소유자는 Apple Developer ID·Authenticode 인증서 비용을 쓰지 않기로 했습니다.

## 결정

1. **별도 트레이 도우미 프로세스** `agent-office-collector-tray`(`collector/gui/cmd/agent-office-collector-tray`)를 만듭니다. 프로세스 전체를 `systray.Run`이 차지하므로 Wails와 루프를 나누지 않습니다. Linux도 같은 도우미로 옮겨 GUI 안의 트레이(`tray_linux.go`, `RunWithExternalLoop`)를 없앱니다.
   - 대안: Wails v3(트레이 API 있음)는 Go 1.25 이상·베타라 ADR-0003 범위를 벗어납니다. 같은 프로세스 `RunWithExternalLoop`는 macOS·Windows에서 검증할 방법이 없습니다.
2. **메뉴**(명세 §10): 상태(등록·수집·Core 실행 enum), 전송 대기 건수, 사무실 열기(기본 브라우저로 GUI 설정의 서버 주소, 없으면 설정 창), 설정 열기, 수집 일시중지/재개, 업데이트 확인(`update check`, 적용하지 않음), 로그아웃(하위 메뉴의 확인 항목을 한 번 더 골라야 `logout` 실행), 종료(트레이 + GUI 창 종료, Core는 계속). 트레이는 GUI처럼 Core 명령만 씁니다(`status --json`, `pause`, `resume`, `update check --channel`, `logout`).
   - **동기화**는 넣지 않습니다. Core에 즉시 전송 명령이 없고 Core가 주기적으로 전송합니다(전송 대기 건수로 대신 표시). 필요하면 Core에 `flush` 명령을 만든 뒤 추가합니다.
3. **창 ↔ 트레이**: GUI 단일 실행은 Wails `SingleInstanceLock` 대신 `internal/guictl`(GUI 설정 폴더의 파일 잠금 `gui.lock` + 요청 파일 `gui.request`, 300ms마다 읽음)로 모든 OS에서 같게 합니다. 두 번째 실행은 Wails를 띄우지 않고 `show`(인자 없음) 또는 `quit`(`--quit`)만 남기고 끝납니다. 트레이 "설정 열기"는 GUI를 인자 없이, "종료"는 `--quit`으로 띄우고 트레이도 끝납니다(떠 있는 GUI가 없으면 `--quit`은 아무것도 하지 않음). GUI는 뜰 때 트레이 도우미를 띄웁니다(사용자별 파일 잠금 `tray.lock`으로 두 번째 트레이는 곧바로 종료 코드 3). 창 닫기는 항상 GUI 종료입니다(트레이는 별도 프로세스로 남음).
4. **자동 시작**: 기존 GUI 자동 시작 정의(XDG `.desktop`, LaunchAgent, HKCU `Run`의 이름·소유 표시는 그대로)가 트레이 도우미를 `--autostart`로 띄웁니다. 예전 정의(GUI `--autostart`)로 GUI가 뜨면 트레이만 띄우고 끝납니다. Windows 소유 판정은 `agent-office-collector-(gui|tray).exe" --autostart` 형식만 인정합니다.
5. **설치 배치**: Core·GUI·트레이는 같은 폴더(Linux `/usr/bin`, Windows `%LOCALAPPDATA%\Programs\Agent Office Collector`)에 둡니다. macOS는 `/Applications/Agent Office Collector.app`(GUI가 주 실행 파일, Core도 `Contents/MacOS`)과 그 안의 `Contents/Library/LoginItems/Agent Office Collector Tray.app`(`LSUIElement`, Dock 없음)입니다. 서로의 위치는 `corecli.SiblingCandidates`로 찾습니다.
6. **설치 프로그램**(모두 서명·공증 없음):
   - Windows: NSIS 3.x(Debian `nsis`) 사용자별 설치 `.exe`. 관리자 권한 없음, 시작 메뉴 바로 가기, HKCU 제거 항목. Linux 컨테이너에서 Core·GUI(Wails Windows 프런트엔드는 cgo 없음)·트레이를 교차 빌드합니다. 제거 프로그램은 기존 `uninstall.ps1 -Yes`(이 도구 소유 Hook·Core 로그온 작업·트레이 Run 값만)를 실행한 뒤 파일을 지웁니다. MSI(WiX)는 러너 이미지 의존성과 사용자별 MSI의 제약 때문에 고르지 않았습니다.
   - macOS: arm64·x86_64를 lipo로 합친 유니버설 앱 번들, ad-hoc 서명(Apple Silicon은 서명 없는 코드를 실행하지 않음), `pkgbuild`(번들 이동 끔) + `productbuild` `.pkg`. macOS 러너에서만 빌드합니다(cgo·Cocoa). 제거는 앱 안의 `agent-office-collector-uninstall --remove-app`.
   - Linux: 기존 헤드리스 Core 패키지 `agent-office-collector`는 그대로 두고, `agent-office-collector-desktop` deb·rpm(GUI·트레이·`.desktop`·아이콘)이 같은 버전 Core와 GTK3·webkit2gtk-4.1에 의존합니다. GUI는 `-tags webkit2_41`로 빌드합니다(Ubuntu 24.04는 webkit2gtk-4.0 런타임이 없음). cgo라 아키텍처별 러너(amd64, `ubuntu-24.04-arm`)에서 Docker `golang:1.24-bookworm`으로 빌드합니다.
   - 어떤 설치 프로그램도 Hook·Core 서비스·트레이 자동 시작을 켜지 않습니다(사용자가 GUI에서 미리보기 후 승인).
7. **Core 자체 업데이트**: 앱 번들 안 또는 트레이 도우미와 같은 폴더의 Core는 `managed_by_package`로 자체 업데이트를 거절합니다(GUI·트레이와 버전이 어긋나지 않게 설치 프로그램으로 업데이트).
8. **검증 수단**: 트레이 `--self-test FILE`은 실제 systray 메뉴를 만든 뒤 각 항목의 클릭 채널에 클릭을 보내 실제 클릭과 같은 처리 경로를 탑니다(브라우저는 열지 않고 요청만 기록, 등록된 PC면 로그아웃 건너뜀). CI `Cross-OS Collector`가 Windows·macOS·Ubuntu 24.04 러너에서 설치 → 트레이 → 제거를 실측합니다(`tests/cross_os/installer_smoke.py`).

## 결과

- GUI 프로세스에 트레이 코드가 없어 Wails 메인 루프와 systray 루프가 같은 프로세스에서 만나지 않습니다.
- Linux에서 창 닫기가 더는 트레이 숨기기가 아닙니다(트레이가 별도 프로세스라 창을 다시 열 수 있음). ADR-0004 결정 6은 폐기됩니다.
- 실측 결과와 미검증 항목은 [Collector GUI](../installation/collector-gui.md#설치-프로그램159)와 PR #168에 기록합니다. OS 패널에 아이콘이 실제로 그려지는지, 사람의 메뉴 클릭, Gatekeeper·SmartScreen 대화상자는 러너에서 자동화할 수 없어 미검증입니다.
- 서명하지 않은 설치 프로그램은 macOS Gatekeeper·Windows SmartScreen 경고를 띄웁니다. 우회 방법을 문서로 안내하고, 서명·공증은 비용 결정 뒤 별도 Issue로 다룹니다.

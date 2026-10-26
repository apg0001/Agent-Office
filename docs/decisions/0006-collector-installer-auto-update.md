# ADR-0006: 설치 직후 첫 실행 마법사와 설치 파일 기반 승인형 자동 업데이트

- 상태: 제안됨(#180 구현 근거, 소유자 검토 대기)
- 날짜: 2026-10-26
- 관련 이슈: #180. [ADR-0005](0005-collector-tray-helper-installers.md) 결정 6의 "어떤 설치 프로그램도 … 켜지 않습니다"와 결정 7(설치 프로그램 배치의 Core는 자체 업데이트 거절)을 보완합니다. 마스터 명세 §10·§11.

## 맥락

- 소유자 요구(2026-10-26): 설치 파일로 설치하면 필요한 것은 자동으로 되고 사용자는 승인 버튼만 누른다. 업데이트도 자동으로 감지·알림하고 승인하면 설치·후처리까지 끝낸다(명세 §10: 기본은 알림 후 업데이트 버튼, 설정으로 자동 다운로드·자동 적용, 수동 확인 모드).
- #159 설치 프로그램(Windows NSIS 사용자별, macOS 유니버설 `.pkg`, Linux 데스크톱 deb·rpm)으로 설치한 Core는 GUI·트레이와 버전이 어긋나지 않게 `update apply`(Core 바이너리만 교체)를 `managed_by_package`로 거절합니다. 그래서 설치 프로그램 배치를 업데이트할 수단이 없었습니다.
- 소유자 PC에는 설치 프로그램 이전의 명령행 설치(macOS `~/.local/bin`, Windows `%LOCALAPPDATA%\AgentOffice\bin`, LaunchAgent·작업 스케줄러·Claude Hook이 그 경로를 가리킴)가 있습니다. Linux 데스크톱 PC 대부분은 Ubuntu 22.04입니다.
- Codex는 `hooks.json` 항목이 바뀌면 "Hooks need review"에서 사용자가 다시 신뢰해야 실행합니다. Collector는 Codex 신뢰 상태를 쓰거나 우회 플래그를 쓰지 않습니다.

## 결정

1. **첫 실행 마법사**(GUI "처음 설정"): 서버 주소 → 브라우저 로그인·PC 등록 → Claude·Codex 탐지 → **한 화면 동의**(탐지된 도구 Hook, Core 자동 시작, 트레이 자동 시작, 모두 기본 켬, 도구별 미리보기 제공) → 연결 확인(등록·Core 프로세스·서버 도달). 동의는 한 번의 확인으로 기존 동작(`hooks install --yes`·`service install --activate`·트레이 자동 시작)을 차례로 실행하며, 미리보기를 만든 도구만 설치합니다. Codex Hook을 설치하면 "Codex를 열어 Hooks need review에서 신뢰" 안내를 띄웁니다. 마법사 상태(`setup`: 없음·done·dismissed)는 GUI 설정(`gui.json`)에 둡니다.
2. **설치 직후 실행**: Windows NSIS 마침 화면 "열기"(기본 체크)가 GUI를 띄우고 GUI가 마법사를 엽니다. macOS `.pkg` postinstall은 대화형 설치(Installer.app)일 때만 콘솔 사용자 세션에서 트레이 앱을 엽니다(`launchctl asuser … open`). `installer(8)` 명령행 설치(업데이트·CI)는 `COMMAND_LINE_INSTALL`이 있어 아무것도 띄우지 않습니다. 트레이는 인자 없이 떴고 마법사가 끝나지 않았으면 설정 창을 엽니다. 로그인 자동 시작(`--autostart`)은 창을 띄우지 않습니다. Linux 패키지는 root로 설치되어 사용자 세션을 모르므로 앱 메뉴의 첫 실행에서 마법사가 뜹니다.
3. **manifest 확장(schema 1 유지)**: 채널 manifest에 `installers`(OS·아키텍처·종류 `nsis`/`pkg`/`deb`/`rpm`·패키지 이름·URL·SHA-256·크기)를 더합니다. 이전 Core는 모르는 키를 무시하고 `artifacts`(Core 압축본)로 자체 업데이트를 계속합니다. ed25519 서명은 manifest 바이트 전체에 대한 것이라 `installers`도 보호되며, 서명 검증은 그대로 필수입니다(키 없는 빌드는 확인하지 않음). macOS는 `universal` 하나, Linux는 Core·데스크톱 두 패키지를 함께 고릅니다.
4. **업데이트 흐름**: 트레이가 시작 45초(± 50%) 뒤와 6시간(± 10%)마다 Core `update check --installer --json`으로 확인합니다. 새 버전이 있으면 OS 알림(macOS `osascript`, Windows PowerShell 토스트, Linux `notify-send`; 실패는 무시)과 트레이 항목 "업데이트 설치 (vX.Y.Z)"를 보입니다. 그 항목을 누르는 것이 승인입니다. Core `update install --installer --yes`가 받기 → 크기·SHA-256 확인 → 계획 파일(상태 폴더 `updates/<버전>/plan.json`, 0600) → 자기 복사본 `agent-office-update-helper`를 띄우고 끝나며, 트레이·GUI 창도 끝납니다. 도우미는 실행 직전에 해시를 다시 확인하고 설치 프로그램을 실행합니다.
   - Windows: `setup.exe /S`(사용자별, 관리자 권한 없음, 설치 폴더는 HKCU `InstallLocation` 그대로). 설치 프로그램이 실행 파일 이름으로 Core·트레이·GUI를 끝내므로 도우미는 다른 이름입니다.
   - macOS: `osascript … do shell script "/usr/sbin/installer -pkg …" with administrator privileges` → OS 관리자 승인 창(경로는 argv·`quoted form`으로만 전달).
   - Linux: `pkexec /usr/bin/env DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get install -y …core.deb …desktop.deb`(rpm은 `dnf install -y`) → polkit 인증 창. 설치 종류는 `dpkg-query`/`rpm -q agent-office-collector-desktop`로 정합니다.
   - 끝나면(성공·실패·취소 모두) 결과(`from`·`to`·코드)를 GUI 설정 폴더 `update-result.json`에 쓰고 고정 위치의 트레이를 다시 띄웁니다. 성공은 설치된 Core의 `version`이 새 버전일 때만입니다.
5. **후처리**(`update post-install`): 트레이가 시작할 때 도우미 결과가 있거나 Core 버전이 `gui.json`의 `last_version`과 다르면 실행합니다. 이전에 설치돼 있던 것만 다시 적용합니다(멱등): 도구별 설정에 Agent Office 소유 Hook(명령 서명으로 식별)이 있으면 현재 Core 경로로 다시 설치(백업·원자적 쓰기, 다른 Hook 보존), 소유 표시가 있는 자동 시작 정의가 있으면 현재 경로로 다시 쓰고 활성화·재시작(Windows는 먼저 `schtasks /End`, Linux는 `systemctl --user restart`), 서비스가 있으면 새 버전이 상태 파일을 쓸 때까지 30초 확인합니다. 설치 위치가 고정이라 일반 업데이트에서는 Hook 명령 문자열이 바뀌지 않고 Codex 재신뢰가 필요 없습니다. 바뀌었으면(명령행 설치에서 옮겨 옴 등) 트레이가 "Codex에서 다시 신뢰" 알림을 띄웁니다.
6. **실패 처리**: 서명·해시·채널·버전 검증 실패나 설치 프로그램 실패·취소면 이전 버전이 그대로 설치돼 있습니다(설치 프로그램이 바꾸기 전 단계에서 멈춤). 도우미가 트레이를 다시 띄우고 트레이가 실패 코드와 "지금 버전을 계속 씁니다"를 알리며, 후처리로 서비스를 다시 시작합니다. Core 압축본 자체 업데이트의 `.bak` 되돌림은 그대로입니다. 설치 프로그램 내부 실패(파일 일부만 바뀜)는 되돌리지 못하므로 같은 설치 파일을 다시 실행하도록 안내합니다(아래 결과 참고).
7. **설정**(GUI "업데이트"): `notify`(기본, 알림 후 승인), `download`(받아서 확인해 두고 알림, 승인하면 설치), `auto`(받아서 바로 설치, macOS·Linux OS 승인 창은 그대로), `manual`(주기 확인 안 함, 메뉴 "업데이트 확인"만). 채널은 자동(Core 버전이 사전 배포면 beta)·stable·beta.
8. **명령행 설치 이전**: 설치 프로그램 설치 뒤 트레이 첫 실행의 후처리가 같은 설정·상태 폴더(등록·장치 토큰·큐·일시중지)를 그대로 쓰면서 소유 Hook·서비스 정의를 새 경로로 바꾸고 이전 Core를 멈춥니다(같은 서비스 이름·라벨이므로 하나만 돔). `legacy status|remove`는 알려진 위치의 이 도구 파일(실행 파일은 Go 빌드 정보의 메인 패키지 경로, 제거 도우미·README는 내용으로 식별)만 보여 주고 승인 시 지웁니다. 지금 Core가 설치 프로그램 배치일 때만, 서비스 정의·Hook이 더는 그 경로를 가리키지 않을 때만 지웁니다.
9. **Linux 22.04**: 데스크톱 GUI를 `ubuntu:22.04`(glibc 2.35, `libwebkit2gtk-4.1-dev` jammy-updates)에서 `-tags webkit2_41`로 빌드합니다. 빌드 스크립트가 GUI의 GLIBC 심볼 최고 버전 ≤ 2.35, 트레이 정적 링크를 확인합니다. Core·트레이는 `CGO_ENABLED=0` 정적 실행 파일입니다. deb 의존성에 jammy보다 새 하한을 두지 않습니다.
10. **게시**: `publish-collector.yml` manifest에 설치 파일이 들어갑니다. 입력 `publish_channel`(기본 끔)을 켜면 같은 승인 아래 버전 Release를 공개하고 채널 Release(`collector-beta`·`collector-stable`, 없으면 만듦, latest 아님)의 manifest·서명을 덮어씁니다(`collector/packaging/update-channel.sh`). 끄면 초안만 만들고 관리자가 확인 뒤 같은 스크립트를 실행합니다(manifest의 설치 파일 주소가 버전 Release 자산이라 초안 상태로는 받을 수 없음).
11. **테스트 전용 경로**: `AGENT_OFFICE_UPDATE_MANIFEST_URL`(manifest 주소), `AGENT_OFFICE_UPDATE_ALLOW_LOOPBACK_HTTP=1`(127.0.0.1 http 허용, 서명은 그대로 필수), `AGENT_OFFICE_UPDATE_ELEVATE=sudo-n`(OS 승인 창 대신 암호 없는 sudo, CI 러너용), 트레이 `--update-self-test FILE`(실제 메뉴 항목 클릭 채널로 승인). CI는 작업마다 일회용 테스트 서명 키를 만들고 서명 직후 비밀 키를 지웁니다(실제 배포 키·비밀 값 미사용).

## 대안

- **Core 자체 교체를 설치 프로그램 배치에도 허용**: GUI·트레이·앱 번들 서명이 어긋나고 macOS 번들·Linux 패키지 관리자 데이터베이스와 불일치해 고르지 않았습니다.
- **Sparkle·WinSparkle·OS 스토어**: 서명 인증서(Developer ID·Authenticode)와 별도 피드가 필요하고 소유자가 인증서 비용을 쓰지 않기로 했습니다.
- **트레이가 직접 설치 프로그램 실행**: Windows 설치 프로그램이 트레이를 끝내므로 결과 기록·재실행 주체가 사라집니다. 이름이 다른 도우미를 씁니다.
- **APT 저장소 배포(unattended-upgrades)**: 저장소 서명·호스팅이 필요합니다. 후속 후보로 남깁니다.

## 결과

- 사용자가 직접 할 일은 마법사·업데이트의 승인 버튼, macOS·Linux OS 승인 창의 암호 입력, Codex "Hooks need review" 신뢰뿐입니다. Gatekeeper·SmartScreen 경고 우회(서명 없음)는 처음 설치 파일을 열 때 그대로 남습니다.
- 실측 범위·미검증 항목은 [Collector GUI](../installation/collector-gui.md#설치-직후-자동-설정과-승인형-업데이트180)와 PR에 기록합니다. macOS 관리자 암호 대화상자·polkit 인증 창·OS 알림 표시·Installer.app 대화형 설치 뒤 postinstall 실행은 러너에서 자동화할 수 없어 미검증입니다.

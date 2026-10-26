# Collector 자체 업데이트

관련 이슈: #44(명령행 Core 자체 업데이트), #180(설치 프로그램 배치의 설치 파일 기반 승인형 업데이트, [ADR-0006](../decisions/0006-collector-installer-auto-update.md)). 사용자 흐름은 [Collector GUI](collector-gui.md#설치-직후-자동-설정과-승인형-업데이트180)에 있습니다.

## 사용

```sh
agent-office-collector update check [--channel stable|beta]          # 새 버전 알림만
agent-office-collector update apply [--channel stable|beta] --yes    # 받아서 적용
```

- 기본 채널은 `stable`. 업데이트는 **알린 뒤 사용자가 적용**합니다(자동 적용은 하지 않음. GUI가 알림을 보여 주고 같은 명령을 부릅니다).
- manifest 위치: `https://github.com/apg0001/Agent-Office/releases/download/collector-<채널>/manifest.json`(+ `.sig`). `--manifest-url`로 바꿀 수 있고 https만 받습니다.
- 패키지(deb·rpm, `/usr/bin`)나 GUI 설치 프로그램(macOS 앱 번들 안, 또는 트레이 도우미 `agent-office-collector-tray`가 같은 폴더에 있는 Windows 설치본, #159)으로 설치했다면 `update apply`는 `managed_by_package`로 거부합니다(GUI·트레이와 버전이 어긋나지 않게). 이 배치는 아래 [설치 파일 기반 업데이트](#설치-파일-기반-업데이트180)를 씁니다(트레이가 자동으로 확인·알림).

## 검증 순서

| 단계 | 내용 | 실패 코드 |
|---|---|---|
| 서명 | manifest 바이트에 대한 ed25519 서명을 빌드에 넣은 공개 키로 확인(키 여러 개 허용 = 키 교체) | `no_trusted_key`, `bad_signature` |
| 형식·채널 | schema 1, 요청한 채널과 같은지 | `bad_manifest`, `channel_mismatch` |
| 버전 | SemVer 2.0 우선순위로 현재보다 높을 때만 | 같거나 낮으면 "최신 버전" |
| 대상 | 현재 OS·아키텍처 배포물이 있는지, URL https | `no_artifact_for_platform`, `insecure_url` |
| 다운로드 | 크기 상한(200MB)·manifest 크기 일치·SHA-256 | `checksum_mismatch`, `too_large` |
| 압축 해제 | Collector 실행 파일 하나만, 경로 이동·링크 거부 | `binary_not_found`, `bad_archive` |
| 실행 확인 | 새 실행 파일의 `version`이 manifest 버전과 같은지 | `new_binary_invalid` |
| 교체 | 같은 디렉터리에서 원자적 이름 바꾸기, 이전 파일은 `.bak` | `not_writable` |
| 재기동 | 자동 시작 서비스가 있으면 재시작(systemd `restart`, launchd `kickstart -k`, 작업 스케줄러 End/Run), 30초 안에 새 버전이 `status.json`을 쓰는지 | 실패 시 이전 파일로 되돌리고 다시 시작 → `rolled_back` |

큐는 SQLite에 남아 있으므로 서비스가 SIGTERM으로 정상 종료한 뒤 새 버전이 체크포인트에서 이어 갑니다(#33·#34). 오류는 코드만 출력하고 URL·경로는 출력하지 않습니다.

## 설치 파일 기반 업데이트(#180)

```sh
agent-office-collector update check --installer [--channel stable|beta] [--json]     # 새 버전 설치 파일이 있는지
agent-office-collector update download --installer [--json]                          # 받아서 크기·SHA-256 확인(자동 다운로드 모드)
agent-office-collector update install --installer --yes [--relaunch <트레이>] [--result <결과.json>] [--json]
agent-office-collector update post-install [--json]                                  # 설치·업데이트 뒤 후처리
```

트레이가 이 명령들을 부릅니다. 사람이 직접 칠 필요는 없습니다.

| 단계 | 내용 | 실패 코드 |
|---|---|---|
| 배치 확인 | 설치 프로그램 배치(위 `managed_by_package` 조건)이고 설치 종류를 알 수 있어야 함: Windows `nsis`, macOS `pkg`, Linux `dpkg-query -W agent-office-collector-desktop` → `deb`, `rpm -q …` → `rpm` | `not_installer_install`(명령행 설치는 `update apply`) |
| manifest | 위 [검증 순서](#검증-순서)의 서명·형식·채널·버전 | 같음 |
| 대상 | `installers`에서 OS·아키텍처(macOS는 `universal`)·종류가 맞는 항목. Linux는 `agent-office-collector`·`agent-office-collector-desktop` 두 패키지가 모두 있어야 함. 크기 1B~200MB, SHA-256 64자, 안전한 파일 이름, https | `no_installer_for_platform`, `bad_manifest`, `insecure_url` |
| 다운로드 | 상태 폴더 `updates/<버전>/`(0700)에 임시 파일 → 크기·SHA-256 확인 → 이름 바꾸기(0600). 같은 파일이 이미 있고 해시가 맞으면 다시 받지 않음. 다른 버전 폴더는 지움 | `checksum_mismatch`, `too_large`, `http_<코드>` |
| 도우미 | 계획 `plan.json`(버전·종류·파일 경로·해시·트레이 위치)을 쓰고 Core를 `agent-office-update-helper`로 복사해 띄운 뒤 `installer_started`로 끝남 | `helper_start_failed` |
| 설치 | 도우미가 실행 직전에 해시를 다시 확인하고 실행: Windows `setup.exe /S`, macOS `osascript … installer -pkg … with administrator privileges`, Linux `pkexec /usr/bin/env DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get install -y -q --no-install-recommends <core.deb> <desktop.deb>`(rpm `dnf install -y`). 상한 30분 | `installer_cancelled`(macOS -128, pkexec 126), `installer_failed` |
| 확인 | 설치된 Core의 `version`이 새 버전인지 | `version_mismatch` |
| 결과 | 결과 파일(GUI 설정 폴더 `update-result.json`: `from`·`to`·코드·시각)을 쓰고 트레이를 다시 띄움. 성공하면 받은 설치 파일을 지움 | — |
| 후처리 | 트레이가 결과를 읽거나 버전 변화를 보면 `update post-install`: 우리 Hook이 있던 도구만 다시 설치, 우리 자동 시작 정의가 있으면 다시 쓰고 활성화·재시작(Windows `schtasks /End` 먼저, Linux `systemctl --user restart`), 30초 연결 확인, 이전 명령행 설치 파일 수 | 결과 JSON `status: attention`(Hook·서비스 실패, 연결 안 됨, Codex 재신뢰 필요) |

- 설치 프로그램 배치의 설치 위치는 업데이트해도 바뀌지 않습니다(Windows HKCU `InstallLocation`, macOS `/Applications`, Linux `/usr/bin`). 그래서 Hook 명령 문자열이 같고 Codex는 다시 신뢰할 필요가 없습니다.
- 출력·결과 파일에는 버전·enum·코드만 들어갑니다(주소·경로·설치 프로그램 출력 원문 없음). 설치 프로그램 출력 끝 4KiB는 취소 판정에만 쓰고 버립니다.
- 테스트·사설 미러용 환경 변수(서명 검증은 그대로 필수): `AGENT_OFFICE_UPDATE_MANIFEST_URL`(manifest 주소), `AGENT_OFFICE_UPDATE_ALLOW_LOOPBACK_HTTP=1`(127.0.0.1의 http 허용), `AGENT_OFFICE_UPDATE_ELEVATE=sudo-n`(OS 승인 창 대신 암호 없는 `sudo -n`, CI 러너용).

### manifest 형식

```json
{
  "schema": 1, "channel": "beta", "version": "0.2.0-beta.1", "published_at": "…",
  "artifacts": [{"os": "linux", "arch": "arm64", "url": "…tar.gz", "sha256": "…", "size": 0, "format": "tar.gz"}],
  "installers": [
    {"os": "windows", "arch": "amd64", "kind": "nsis", "url": "…setup-0.2.0-beta.1-windows-amd64.exe", "sha256": "…", "size": 0},
    {"os": "darwin", "arch": "universal", "kind": "pkg", "url": "…macos-universal.pkg", "sha256": "…", "size": 0},
    {"os": "linux", "arch": "amd64", "kind": "deb", "package": "agent-office-collector", "url": "…_amd64.deb", "sha256": "…", "size": 0},
    {"os": "linux", "arch": "amd64", "kind": "deb", "package": "agent-office-collector-desktop", "url": "…", "sha256": "…", "size": 0}
  ]
}
```

`installers`는 schema 1에 더한 선택 키입니다. 이전 Core는 모르는 키를 무시하고 `artifacts`로 자체 업데이트를 계속합니다. `agent-office-release manifest`가 `SHA256SUMS`의 Core 압축본과 설치 파일(`agent-office-collector-setup-<버전>-windows-<arch>.exe`, `agent-office-collector-<버전>-macos-universal.pkg`, `agent-office-collector[-desktop]_<버전>_<arch>.deb`, `agent-office-collector[-desktop]-<버전>-1.<x86_64|aarch64>.rpm`, deb·rpm 파일 이름의 버전은 첫 `-`가 `.`)을 모두 넣습니다.

## 키와 서명(관리자)

```sh
cd collector/core
go run ./cmd/agent-office-release keygen update.key          # 공개 키 출력, 비밀 키 0600
go run ./cmd/agent-office-release manifest ../../dist/collector-<버전> <버전> <채널> <배포물 기준 URL>
go run ./cmd/agent-office-release sign update.key ../../dist/collector-<버전>/manifest.json
```

- 공개 키는 빌드 때 넣습니다: `AGENT_OFFICE_UPDATE_KEYS=<공개 키> collector/packaging/build.sh <버전>`(내부적으로 `-ldflags -X …/update.TrustedKeys`). 키가 없는 빌드는 업데이트를 거부합니다.
- 게시 워크플로(`publish-collector.yml`)는 `collector-publish` Environment의 변수 `COLLECTOR_UPDATE_PUBLIC_KEYS`와 비밀 `COLLECTOR_UPDATE_SIGNING_KEY`로 빌드·서명해 Release 초안에 `manifest.json`·`.sig`를 함께 올립니다. 비밀 키는 저장소에 두지 않습니다.
- 채널 manifest 갱신(#180): 게시 워크플로를 `publish_channel: true`로 실행하면 같은 Environment 승인 아래 버전 Release 초안을 공개하고 채널 Release(사전 배포 → `collector-beta`, 아니면 `collector-stable`)가 없으면 만든 뒤(latest 표시 안 함) `manifest.json`·`.sig`를 덮어씁니다. 기본(`false`)은 초안만 만들고, 관리자가 초안을 확인한 뒤 `GH_TOKEN=… collector/packaging/update-channel.sh <버전> <manifest 폴더>`로 같은 일을 합니다. manifest의 설치 파일 주소가 버전 Release 자산이라 초안 상태로는 사용자가 받을 수 없습니다.

## 검증

- 단위·통합 테스트(Linux arm64 컨테이너, `go test -race`): SemVer 우선순위, 서명(다른 키·변조·키 없음), 채널·대상·http 거부, SHA-256 불일치, tar.gz·zip 추출·경로 이동 거부, 교체·`.bak`, 재기동 후 비정상 시 되돌림, 패키지 설치 거부, CLI `check`·`apply`(`--yes` 없으면 적용 안 함)·키 없는 빌드 거부.
- 릴리스 도구 `keygen`·`manifest`·`sign` 실행과 서명 검증.
- 실제 GitHub Release 실측(2026-10-25, Pi 5 linux/arm64): 같은 공개 키를 넣은 `0.1.0-beta.0` 시험 빌드가 `collector-v0.1.0-beta.1` Release의 `manifest.json`으로 새 버전을 찾고, `update apply --yes`로 받기·SHA-256 확인·교체·`.bak` 보존까지 성공.
- 이 실측에서 고친 문제: GitHub Release 다운로드는 저장소 주소로 302를 돌려주는데, 공통 HTTP 클라이언트가 장치 토큰 보호를 위해 리다이렉트를 막아 `http_302`로 실패했습니다. 업데이트 요청만 리다이렉트를 최대 5번 따라가고, 각 단계 주소가 https인지 다시 검사합니다(업데이트 요청에는 인증 헤더가 없음). **0.1.0-beta.1 배포 바이너리에는 이 수정이 없어 자체 업데이트가 실패하므로, 다음 버전으로는 한 번 수동 설치가 필요합니다.**
- 기본 채널 주소(`collector-beta`·`collector-stable` Release)는 관리자가 채널 Release를 만든 뒤에 동작합니다.
- 미검증: macOS·Windows에서 명령행 Core 교체(`update apply`)·서비스 재시작. 설치 파일 기반 업데이트의 실측 범위는 [Collector GUI](collector-gui.md#업데이트이전-실측cross-os-collector)에 있습니다.
- 설치 파일 기반 업데이트 단위·통합 테스트(#180, Linux arm64 컨테이너 `go test`): 설치 파일 선택(Windows·macOS universal·Linux 두 패키지, 누락·중복·잘못된 해시·크기·파일 이름 거부), 이전 형식 manifest 호환, 서명이 `installers`까지 덮음(변조 거부), 받아 둔 파일 재사용·변조 감지, OS별 설치 명령(경로는 argv로만), 계획 파일 검증, CLI `check`·`install`(승인 없으면 시작 안 함)·도우미(설치 → 버전 확인 → 결과 → 트레이 재실행, 실패·버전 불일치·계획 뒤 파일 교체 거부), 명령행 설치 거부, 후처리(이전 경로 Hook·서비스 정의를 새 경로로, 남의 Hook 보존, 멱등), `legacy` 정리(알아본 파일만, 참조 중이면 거부), 릴리스 도구 manifest의 설치 파일 항목.

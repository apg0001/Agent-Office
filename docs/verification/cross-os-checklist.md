# macOS·Windows 실장비 검증 체크리스트

Pi(Linux arm64)에서는 실행할 수 없는 검증 항목을 한곳에 모았습니다. 소유자나 Windows PC 에이전트가 위에서부터 차례로 실행하고, 결과를 표 형식 그대로 해당 Issue 댓글과 각 문서의 "검증 범위" 표에 옮깁니다. 실행하지 못한 항목은 **미실행**으로 남기고 지원한다고 표시하지 않습니다.

관련 Issue: #40(GUI), #41(장치 토큰 보안 저장소), #42(설치 패키지), #43(자동 시작·GUI/Core 독립성), #62(전체 사용 흐름), #87(Codex Desktop, Windows만).

## 기록 규칙

- 결과에는 OS 버전, CPU 아키텍처, 도구 버전(`claude --version`, `codex --version`, Go 버전), 실행 날짜, 성공 여부, 오류 코드만 적습니다.
- 프롬프트, 파일 본문, 전체 명령 출력, 사용자 홈 아래 경로, 장치 토큰, 등록 코드는 적지 않습니다. 스크린샷을 남긴다면 토큰·코드·경로가 보이지 않게 합니다.
- 테스트 서버는 Pi의 운영 스택이나 일회용 스택(`tests/e2e/claude_flow.sh`가 띄우는 것과 같은 Compose)을 씁니다. 실제 사용자 계정과 섞지 않으려면 테스트 전용 관리자 계정을 새로 만듭니다.

## 0. 준비(공통)

1. 서버 접속 주소를 정합니다(Tailscale 권장, [원격 접속](../installation/remote-access.md)).
2. 웹 `/devices`에서 PC 등록 코드를 발급받습니다(일회용, 짧게 유효).
3. 배포물을 받습니다. 아직 게시 전이면 Pi에서 `collector/packaging/build.sh 0.1.0-beta.1`로 만들고 `dist/collector-0.1.0-beta.1/`의 OS별 압축 파일과 `SHA256SUMS`를 옮깁니다.
4. 해시를 확인합니다. macOS는 `shasum -a 256 -c SHA256SUMS --ignore-missing`, Windows는 `Get-FileHash .\agent-office-collector-*.zip -Algorithm SHA256`의 결과를 `SHA256SUMS`와 비교합니다.

## 1. Core 설치·등록·토큰 보관(#42, #41)

| 단계 | macOS | Windows(PowerShell) | 기록할 것 |
|---|---|---|---|
| 압축 해제 | `tar -xzf agent-office-collector-*-darwin-*.tar.gz` | `Expand-Archive .\agent-office-collector-*-windows-*.zip` | 실행 파일이 실행되는지(`version`). macOS Gatekeeper 경고 여부(서명 전이라 예상됨) |
| 등록 | `printf '%s' '<코드>' \| ./agent-office-collector register --server <주소> --code-stdin --name <PC 이름>` | `'<코드>' \| .\agent-office-collector.exe register --server <주소> --code-stdin --name <PC 이름>` | 성공 여부, 웹 `/devices`에 PC가 보이는지 |
| 토큰 보관 | `./agent-office-collector status --json` | `.\agent-office-collector.exe status --json` | `token_storage`가 `os_keyring`인지. macOS는 키체인 접근 앱, Windows는 자격 증명 관리자에 `io.github.apg0001.agent-office.collector` 항목이 생겼는지 |
| 설정 파일 | `config.json`에 토큰 필드가 비어 있는지 | 같음 | 예/아니오만 |
| 로그아웃 | `./agent-office-collector logout` | `.\agent-office-collector.exe logout` | 저장소 항목 삭제, 웹 `/devices`에서 폐기됨 표시 |
| 대체 경로 | `AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`로 다시 등록 | `$env:AGENT_OFFICE_COLLECTOR_TOKEN_STORE='file'` | `token_storage`가 `file_fallback`인지 |

## 2. Hook·자동 시작·제거(#43, #42)

| 단계 | 명령(macOS 기준, Windows는 `.exe`) | 기록할 것 |
|---|---|---|
| Hook 미리보기 | `agent-office-collector hooks install --tool claude` | 바꾸는 파일과 항목만 보여 주고 아무것도 쓰지 않는지 |
| Hook 설치 | `… --yes` 후 `hooks status` | 기존 사용자 Hook이 그대로인지(설치 전후 `~/.claude/settings.json` SHA-256 비교, 해시만 기록), 백업 파일 생성 |
| Core 자동 시작 | `agent-office-collector service install --activate` | macOS: `launchctl print gui/$(id -u)/io.github.apg0001.agent-office.collector` 상태. Windows: `schtasks /Query /TN \AgentOffice\Collector`, 실행 시 콘솔 창이 보이는지(알려진 위험) |
| 재부팅 | 로그아웃·재로그인 또는 재부팅 | 로그인 후 `status`에서 `run`이 실행 중인지 |
| GUI 자동 시작 | GUI의 자동 시작 켜기 | macOS LaunchAgent plist, Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 값 생성, 재로그인 후 GUI가 뜨는지 |
| 제거 | `agent-office-collector-uninstall`(Windows `powershell -ExecutionPolicy Bypass -File uninstall.ps1`) | 우리 Hook·자동 시작만 지워지고 사용자 Hook은 남는지 |
| 완전 삭제 | `… --purge`(Windows `-Purge`) | 보안 저장소 항목과 설정 폴더 삭제, `~/.claude/projects` 등 원본 기록은 그대로인지 |

## 3. GUI 빌드·실행(#40, #43)

macOS는 Xcode 명령줄 도구, Windows는 WebView2 런타임과 Go 1.24가 필요합니다.

```sh
cd collector/core && go build -o ../../dist/agent-office-collector ./cmd/agent-office-collector
cd ../gui && go build -tags desktop,production -ldflags "-s -w" -o ../../dist/agent-office-collector-gui .
# Windows는 콘솔 창을 숨기려면 -ldflags "-s -w -H windowsgui"
```

| 확인 | 기록할 것 |
|---|---|
| 빌드 | 성공 여부, 실패하면 오류 첫 줄(경로 제거) |
| 창 표시·탭 전환 | 상태·기기·Hook 탭이 열리는지 |
| 브라우저 로그인 등록 | GUI에서 등록 → 기본 브라우저가 열리고 등록 완료까지 |
| GUI/Core 독립성 | 작업 관리자·`kill`로 GUI를 끝내도 Core `status`가 계속 수집 중인지, Core를 끝냈다 다시 켜면 GUI 상태가 회복되는지 |
| 창 닫기 | macOS·Windows는 트레이가 없으므로 닫기 = GUI 종료, Core는 계속 실행(ADR-0004) |

## 4. 실제 사용 흐름(#62)

macOS는 Linux와 같은 스크립트를 쓸 수 있습니다(Docker Desktop 필요).

```sh
tests/e2e/claude_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
SCENARIO=subagent tests/e2e/claude_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
```

Windows에는 이 스크립트를 옮기지 않았습니다. 대신 설치형 흐름으로 확인합니다: 1·2단계로 등록과 Hook 설치를 마친 뒤 Claude Code에서 짧은 작업(파일 하나 읽기, Bash 한 번, 하위 에이전트 한 번, TodoWrite 한 번)을 하고 웹 사무실 화면을 봅니다.

| 확인 | 기록할 것 |
|---|---|
| 세션 표시 | 사무실에 세션 시작 → 진행 → 종료가 보이는지 |
| 하위 에이전트 | lead 아래에 하위 에이전트가 보이는지 |
| 계획 단계 | 에이전트 상세에 계획 단계 상태가 보이는지. 이 런타임은 아직 미실측이라 `confidence`는 `inferred`가 정상 |
| 원문 미전송 | 작업에 넣은 고유 표식 문자열이 서버 DB 덤프에 없는지(`pg_dump … \| grep -c <표식>`이 0) |
| 대화형 승인 대기 | 권한 요청이 뜰 때 사무실에 승인 대기로 보이는지 |
| Codex CLI·Desktop | Windows는 #87 절차(`python tests/poc/codex/desktop_session.py prepare`부터)를 따릅니다 |

## 결과를 옮길 곳

- [Collector GUI 검증 범위](../installation/collector-gui.md#검증-범위), [Collector CLI 검증](../installation/collector-cli.md#검증), [설치 패키지](../installation/collector-packages.md), [Linux 흐름](linux-claude-flow.md)과 같은 형식의 OS별 결과 절
- 어댑터 지원 수준(`collector/core/internal/adapter/*/`의 `VerifiedTargets`)은 실제 관측한 Hook 이벤트만 추가하고 테스트를 함께 고칩니다.
- `docs/project-status.md` "다음 작업"

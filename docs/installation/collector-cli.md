# Collector CLI·백그라운드 실행·진단

관련 이슈: #34 (`EP-04/ISS-06`, 요구사항 COL-001~003, COL-010), #41(장치 토큰 OS 보안 저장소·`logout`, DEV-004~005). 선행: #29 어댑터, #30 tail, #32 정규화, #33 큐·전송.

GUI(#40~#44)와 분리된 headless 실행 파일 `agent-office-collector`입니다. GUI는 나중에 이 명령들을 감쌉니다. Hook 설치·제거는 #31에서 추가했습니다(아래 [Hook 설치](#hook-설치hooks-31)). 장치 토큰 OS 보안 저장소와 `logout`은 #41에서 추가했습니다(아래 [장치 토큰 보관](#장치-토큰-보관41)). 설치 패키지·서명(#42), 업데이트(#44)는 범위가 아닙니다.

## 검증 범위

| 대상 | 상태 | 근거 |
|---|---|---|
| Linux arm64(Docker `golang:1.24-bookworm`, Raspberry Pi 5 호스트) | **검증**: 단위·통합·종단 테스트(`go test -race`) | 아래 [검증](#검증) |
| Linux systemd 사용자 서비스 | **생성만 검증**: unit 파일 골든 테스트. 실제 `systemctl --user`로 등록·기동하지 않음(컨테이너에 systemd 없음) | `service/testdata/systemd.service.golden` |
| macOS launchd LaunchAgent | **미검증**: plist 생성 골든 테스트와 `GOOS=darwin go vet`·교차 빌드만 수행. 실제 macOS에서 실행하지 않음 | `launchd.plist.golden` |
| Windows 작업 스케줄러(로그온 작업) | **미검증**: 작업 XML 생성 골든 테스트와 `GOOS=windows go vet`·교차 빌드만 수행. `schtasks` 등록·UTF-16 XML 수용·콘솔 창 표시 여부를 실제 Windows에서 확인하지 않음 | `task.xml.golden` |
| 실제 Agent Office 서버(#35)와의 종단 실행 | **미실행**: `httptest` 가짜 서버로만 확인 | `e2e_test.go` |
| Claude Code 사용자 설정 Hook 설치·제거(`hooks`, #31) | **Linux arm64 컨테이너에서 파일 처리만 검증**: 임시 디렉터리의 합성 `settings.json`으로 백업·원자적 쓰기·소유 항목만 제거·거부 조건 테스트 | `hooks_test.go`, `cli/hooks_test.go` |
| Hook 명령 인용(linux·darwin·windows) | **생성만 검증**: 골든 테스트. macOS·Windows에서 실제 셸이 명령을 실행하는지 미확인 | `hooks/testdata/*.golden` |
| 실제 Claude Code·Codex가 설치된 `hook` 명령을 호출하는 경로 | **미실행**: 컨테이너에 인증된 Claude·Codex가 없어 실행하지 않음. 사용자 실제 `~/.claude`·`~/.codex`는 건드리지 않음 | — |
| 장치 토큰 Linux Secret Service(#41) | **검증(컨테이너)**: gnome-keyring 42.1·dbus 1.14.10, `AGENT_OFFICE_TEST_REAL_KEYRING=1` 테스트(저장·읽기·삭제, register→status→logout, 파일 토큰 이전) | `tests/poc/collector/desktop/keyring-secret-service.sh` |
| 장치 토큰 macOS Keychain·Windows Credential Manager | **미검증**: `GOOS=darwin\|windows go vet`만 | ADR-0004 |
| Codex 사용자 범위 Hook 설치·제거(`hooks --tool codex`, #170) | **Linux arm64 실측**: Codex CLI 0.163.0에서 `$CODEX_HOME/hooks.json` 설치 → TUI 신뢰 → `codex exec` → Collector → 서버 종단 확인. macOS·Windows·Desktop은 미검증(소스상 같은 위치) | [Codex 사용자 Hook 실측](../verification/codex-user-hooks.md), `tests/e2e/codex_flow.sh` |

검증되지 않은 OS에서는 지원한다고 표시하지 않습니다. 실장비 검증 전까지 macOS·Windows 서비스 정의는 미리보기·수동 등록용입니다.

## 명령

| 명령 | 동작 |
|---|---|
| `register --server URL --code XXXX-XXXX\|--code-stdin --name PC [--force]` | `--code-stdin`은 코드를 표준 입력 첫 줄(최대 64바이트)에서 읽습니다(GUI가 사용, 프로세스 목록에 코드가 보이지 않음). `POST /api/v1/devices/register`로 장치를 등록하고 장치 토큰을 OS 보안 저장소에 저장합니다(쓸 수 없으면 설정 파일 0600, [장치 토큰 보관](#장치-토큰-보관41)). 토큰은 출력하지 않습니다. `--force`로 다시 등록하면 이전 장치의 보안 저장소 항목을 지웁니다(서버 폐기는 하지 않음). 서버 주소는 https만 받고, http는 `localhost`·`127.x`·`::1`만 허용합니다. 이미 등록돼 있으면 `--force`가 필요합니다 |
| `logout [--local-only]` | 장치 토큰으로 `POST /api/v1/devices/revoke`를 호출해 서버에서 이 장치를 폐기한 뒤(204, 또는 이미 폐기돼 401·403) 보안 저장소 항목과 `config.json`을 지웁니다. 서버 오류·네트워크 오류·보안 저장소 잠김이면 아무것도 지우지 않고 1로 끝납니다. `--local-only`는 서버 요청 없이 로컬만 지웁니다(웹 `/devices`에서 따로 폐기). 미등록이면 0. 실행 중인 `run`은 다음 주기에 전송·heartbeat를 멈추고 새 등록을 기다립니다. 로컬 큐는 지우지 않습니다 |
| `run` | 포그라운드 장기 실행. SIGINT/SIGTERM(Windows는 Ctrl+C)으로 정상 종료(큐 보존, 종료 코드 0). 사용자당 1개만 실행(잠금 파일) |
| `hook claude\|codex [--product codex-desktop] [--tool-version V] [--mode M]` | Hook stdin JSON 한 건을 받아 실행 중인 `run`에 spool로 넘깁니다. **항상 종료 코드 0**, stdout·stderr 출력 없음 |
| `pause` / `resume` | 일시중지 상태를 설정 디렉터리 `pause.json`에 저장합니다. 실행 중인 `run`이 다음 주기(기본 2초)에 반영합니다 |
| `status [--json]` | 로컬 상태 요약 |
| `diagnose [--json] [--offline]` | `status` + Go 런타임 버전, 기호 위치, 서버 health 도달 여부 |
| `service install\|uninstall [--activate]` | 자동 시작 정의 설치·제거. `--activate`가 없으면 파일만 쓰고 실행할 명령을 안내합니다 |
| `service print [--os linux\|darwin\|windows] [--exe PATH]` | 정의 미리보기(파일을 쓰지 않음) |
| `hooks install\|uninstall [--tool claude\|codex] [--yes] [--scope user]` | 사용자 설정에 Hook 추가·제거(기본 `claude`). `--yes`가 없으면 미리보기만 출력합니다. Codex는 설치 뒤 Codex에서 직접 신뢰해야 실행됩니다 |
| `hooks status [--tool claude\|codex] [--json]` | Hook 설치 상태와 도구 버전 검증 수준. `--json`은 GUI용으로 도구별 감지 여부·버전·검증 수준·Hook 상태 enum·개수와 읽기 실패 코드만 출력합니다(설정 경로·다른 Hook 명령 원문 없음) |
| `hooks print [--tool claude\|codex] [--os linux\|darwin\|windows] [--exe PATH]` | Hook 항목 미리보기(파일을 쓰지 않음) |
| `version` | 버전·OS·아키텍처 |

종료 코드: 0 성공, 1 실패, 2 사용법 오류. `hook`은 예외 없이 0입니다.

## 실행 루프(`run`)

```text
수집 고루틴(주기 2초)                       전송 고루틴              heartbeat 고루틴(60초)
 ├ config.json 변경 감지 → 새 장치로 재시작   transport.Sender.Run     POST /devices/heartbeat
 ├ pause.json 반영                         (일시중지·폐기 시 정지)    (일시중지·폐기 시 정지)
 ├ spool/ Hook 후보 → Normalize → Enqueue → Notify
 ├ transcript 탐색(10초) → tail.Poll → Pipeline.Transcript → Enqueue(+체크포인트, 한 트랜잭션) → Notify
 ├ PruneSeen(now-35일), 24시간마다
 └ status.json 기록(15초 또는 상태 변화 시)
```

- [큐 문서](../architecture/collector-queue.md)의 #34 후속 항목대로 `queue.Open` → `SeenEventIDs` → `normalize.New(Sequences: q, SeenEventIDs: …)`를 씁니다.
- 등록 전이면 종료하지 않고 `not_registered` 상태로 기다리다가, `register`가 끝나면 자동으로 수집을 시작합니다(서비스 재시작 반복 방지).
- 서버가 401/403을 돌려주면(`ErrDeviceRevoked`) 전송과 heartbeat를 멈추고 `device_revoked` 상태와 "다시 등록" 안내를 남깁니다. 수집과 큐 기록은 계속하고 큐는 지우지 않습니다. `register --force`로 config가 바뀌면 몇 초 안에 새 장치로 다시 시작합니다. 이전 장치 `device_id`로 큐에 들어간 이벤트는 서버가 거부할 수 있고 dead-letter로 셉니다.
- 감시 루트는 `$CLAUDE_CONFIG_DIR/projects` 또는 `~/.claude/projects`입니다(설정 파일 `claude_projects_dir`로 바꿀 수 있음). ADR-0002에 따라 Codex transcript는 tail하지 않고 Codex는 Hook만 받습니다.
- 바뀌지 않은 transcript는 `stat`만 하고, 2분 넘게 바뀌지 않은 파일은 핸들을 닫아 열린 파일 수를 제한합니다.
- `hooks install`(#31)이 감지한 Claude 버전을 설정 파일 `claude_version`에 기록합니다. 실행 모드 `claude_mode`는 자동으로 채우지 않으므로, 비어 있으면 미검증 런타임으로 보고 confidence를 `inferred`로 낮춥니다.

### 최초 실행 위치

최초 실행(상태 디렉터리 `baseline.json` 없음)에서는 **이미 있는 transcript를 끝까지 건너뛰고** 체크포인트만 기록합니다. 오래된 줄에 `timestamp`가 없으면 관측 시각으로 찍혀 지금 일어난 일처럼 보이고, 설치 전 작업 내역을 서버로 올리지 않기 위해서입니다. 그 뒤 새로 생긴 transcript는 처음부터 읽고, Collector가 꺼져 있던 동안 기존 파일에 추가된 줄은 다음 실행에서 이어 읽습니다. 감시 루트가 아직 없으면(Claude 미설치) 기준선은 바로 완료로 표시합니다.

### 일시중지

일시중지 중에는 transcript tail, Hook spool 기록, 전송, heartbeat를 모두 멈춥니다(서버에는 120초 뒤 `DISCONNECTED`로 보임). `hook` 명령도 일시중지면 아무것도 쓰지 않습니다. `resume`은 `skip_backlog`를 켜고, `run`은 재개 직후 모든 transcript를 현재 끝으로 옮겨 **일시중지 동안 쓰인 줄을 수집하지 않습니다**. `pause.json`을 읽을 수 없으면 안전하게 일시중지로 처리합니다.

## Hook 전달(fail-open)

`hook` 프로세스는 SQLite를 열지 않고 네트워크도 쓰지 않습니다.

1. stdin을 최대 8MiB, 2초 안에 읽습니다. 넘거나 늦으면 버립니다.
2. 미등록·일시중지·알 수 없는 제공자·형식 오류면 버립니다.
3. #29 어댑터(`claude.ParseHook`·`codex.ParseHook`)로 화이트리스트 후보를 만들고, 후보(enum과 불투명 참조 ID)만 `spool/`에 파일 하나로 원자적으로 씁니다. prompt·tool_input·tool_response·cwd·transcript_path 같은 Hook 원문은 디스크에 쓰지 않습니다.
4. spool은 최대 2,000개입니다. `run`이 꺼져 있어 가득 차면 새 Hook은 버립니다.
5. panic을 포함한 어떤 오류도 종료 코드 0, 출력 없음으로 끝납니다. Claude Code는 일부 Hook의 stdout을 모델 문맥에 넣으므로 stdout에 쓰지 않습니다.

`run`은 spool 파일을 오래된 순서로 정규화·큐 기록 뒤 지웁니다. 형식이 틀린 파일은 지우고 `spool_invalid`로 셉니다.

## Hook 설치(`hooks`, #31)

### 대상과 이벤트

| 도구 | 설정 파일 | 등록 이벤트 | 지원 |
|---|---|---|---|
| Claude Code | `$CLAUDE_CONFIG_DIR/settings.json`, 없으면 `~/.claude/settings.json`의 `hooks` | #19에서 반복 관측한 9종: `SessionStart`, `UserPromptSubmit`, `PreToolUse`·`PostToolUse`·`PermissionRequest`(matcher `*`), `SubagentStart`, `SubagentStop`, `Stop`, `SessionEnd` | `install`·`uninstall`·`status`·`print` |
| Codex CLI·Desktop | `$CODEX_HOME/hooks.json`, 없으면 `~/.codex/hooks.json`(#170) | #17·#18·#170에서 관측한 9종(위와 같은 이름, matcher 없음). `SessionEnd`는 timeout 3초(Codex 상한) | `install`·`uninstall`·`status`·`print`. 설치 뒤 [Codex 신뢰](#codex-신뢰170)가 필요 |

계획 단계(#38)는 별도 Hook 없이 위 `PreToolUse`·`PostToolUse`(Claude `TodoWrite`, Codex `update_plan`)와 Claude transcript에서 상태만 읽어 `plan.updated`로 보냅니다. Claude `TodoWrite` 입력 형식은 Claude Code 2.1.308 Linux `-p`에서 실측해 `confidence`가 `observed`이고, 다른 런타임과 Codex `update_plan`은 `inferred`입니다([어댑터 문서](../architecture/collector-adapters.md#계획-단계planupdated-38)).

미관측 이벤트(`PostToolUseFailure`, `Notification`, `PreCompact` 등)는 등록하지 않습니다. `--scope`는 `user`만 받습니다(프로젝트 설정은 건드리지 않음).

### 핸들러

```json
{"type": "command", "command": "'/home/me/.local/bin/agent-office-collector' hook claude", "timeout": 5}
```

- 타임아웃 5초: Claude 기본값 600초는 도구 호출마다 지연을 더합니다(#19 실측 6.2초 → 21.8초). `hook` 명령은 stdin을 최대 2초만 기다리고 항상 0으로 끝납니다.
- Linux·macOS: 실행 파일 경로를 작은따옴표로 인용합니다(공백·`'` 포함 경로 허용).
- Windows: `"C:/Program Files/…/agent-office-collector.exe" hook claude`처럼 `/` 구분자와 큰따옴표를 씁니다. `"`·`%`·`!`·`$`·`` ` ``가 든 경로와 UNC 경로는 거부합니다. **실제 Windows Claude Code에서 실행하지 않았습니다.**
- 버전·모드 플래그는 넣지 않습니다. 도구를 업데이트해도 Hook 명령은 그대로입니다.

### 안전 규칙

1. **미리보기 후 승인**: `--yes` 없이 실행하면 대상 파일(홈은 `~`로 표시), 이벤트별 `+`(추가)·`~`(교체)·`-`(제거)와 추가할 핸들러만 출력하고 파일을 바꾸지 않습니다.
2. **보존**: 최상위 키 순서, 알 수 없는 키, 숫자·문자열 원문, 남의 Hook을 그대로 둡니다. 결과는 2칸 들여쓰기로 다시 씁니다.
3. **거부**: JSON 객체로 읽을 수 없는 파일(주석, 중복 키, 뒤따르는 값, 문법 오류), `hooks`나 이벤트 값의 형식이 다른 파일, 읽기 전용 파일, 심볼릭 링크, 4MiB 초과 파일은 백업·쓰기 없이 실패합니다.
4. **백업**: 파일을 바꾸기 직전에 원본 바이트를 `settings.json.agent-office-backup-<UTC 시각>`(0600, 기존 파일을 덮어쓰지 않음)으로 남깁니다. 백업은 자동으로 지우지 않습니다.
5. **원자적 쓰기**: 같은 디렉터리 임시 파일 → fsync → rename. 원래 파일 권한을 유지하고 새 파일은 0600입니다. 백업 뒤 파일을 다시 읽어 그 사이 다른 프로그램이 바꿨으면 쓰지 않습니다.
6. **소유 판정**: 핸들러 `command`의 첫 토큰(인용 해제) 파일 이름이 `agent-office-collector`(또는 `.exe`)이고 다음 인자가 `hook <tool>`이면 소유입니다. Claude 설정 검증이 모르는 키를 거부할 수 있어 별도 표시 키는 넣지 않습니다.
7. **멱등**: 이미 같은 항목이 있으면 아무것도 쓰지 않습니다. 실행 파일 위치가 바뀌었거나 수동으로 고친 소유 항목은 교체합니다.
8. **제거**: 모든 이벤트에서 소유 핸들러만 지웁니다. 소유 핸들러만 있던 그룹·이벤트 키·`hooks` 키는 비게 되면 지우고, 남의 핸들러가 함께 있던 그룹은 남깁니다. 설치 전 `"hooks": {}`처럼 원래 비어 있던 키는 제거 후 사라질 수 있습니다(의미 동일).

`status` 상태: `installed`(9종 모두 최신), `partial`(일부 누락), `modified`(소유 항목의 명령·타임아웃·matcher가 다름), `not_installed`, `unreadable`. 다른 Hook 개수만 표시하고 명령 원문은 출력하지 않습니다.

### 도구 버전 감지

`install`·`status`는 PATH의 `claude --version`·`codex --version`을 3초 제한으로 실행해 첫 줄의 버전 문자열만 꺼냅니다(나머지 출력·stderr는 버림). 어댑터 실측 표와 제품·버전·OS가 정확히 같을 때만 `verified`/`partial`이고 실측 모드를 함께 표시합니다. 예: Linux의 Claude Code 2.1.308은 `verified (실측 모드 print 한정)`, 그 밖은 `unverified`.

등록된 Collector에서 `hooks install`은 감지한 Claude 버전을 설정 파일 `claude_version`에 기록하고, `hook claude`는 `--tool-version`이 없으면 이 값을 씁니다. **실행 모드(`claude_mode`)는 기록하지 않습니다.** Hook payload로 `-p`와 대화형 TUI를 구분할 수 없고 대화형은 미실측이므로, 수동으로 `claude_mode`를 넣지 않는 한 confidence는 `inferred`로 남습니다. Claude를 업데이트하면 `hooks install --yes`를 다시 실행해 버전을 갱신하세요(Hook 항목은 변경 없음).

### 예시

```bash
agent-office-collector hooks status
agent-office-collector hooks install            # 미리보기
agent-office-collector hooks install --yes      # 백업 후 적용
agent-office-collector hooks uninstall --yes    # 소유 항목만 제거
agent-office-collector hooks install --tool codex        # Codex 미리보기
agent-office-collector hooks install --tool codex --yes  # 적용 후 Codex에서 신뢰
agent-office-collector hooks print --tool codex          # Codex hooks.json 형식 미리보기
```

실행 중인 Claude Code 세션은 설정 파일 변경을 바로 반영하지 않거나 `/hooks` 확인을 요구할 수 있습니다(미실측). 새 세션에서 확인하세요.

### Codex 신뢰(#170)

Codex는 사용자가 신뢰하지 않은 Hook을 **실행하지 않습니다**(관리 Hook 제외). Collector는 Codex 신뢰 상태를 쓰지 않으며 `--dangerously-bypass-hook-trust`도 쓰지 않습니다. Codex 0.163.0 Linux 실측 기준:

1. `agent-office-collector hooks install --tool codex`로 미리보기를 보고 `--yes`로 적용합니다. `~/.codex`(또는 `$CODEX_HOME`)가 없으면 만들지 않고 실패합니다(Codex를 한 번 실행한 뒤 다시).
2. 새 터미널에서 대화형 `codex`를 실행하면 "Hooks need review — N hooks are new or changed" 화면이 나옵니다. `1. Review hooks`로 명령을 확인하고 신뢰하거나 `2. Trust all and continue`를 고릅니다. 이미 실행 중인 세션에서는 `/hooks`에서 신뢰합니다. `codex exec`만 쓰는 경우에도 한 번은 대화형으로 신뢰해야 합니다(신뢰 전 `exec`는 경고 없이 Hook을 건너뜀).
3. Codex가 `config.toml`에 `[hooks.state."<hooks.json 경로>:<이벤트>:<그룹>:<핸들러>"] trusted_hash`를 기록합니다. 해시는 명령·타임아웃·matcher로 계산되므로 Collector 실행 파일을 옮겨 `install --yes`로 항목이 바뀌면 **다시 신뢰**해야 합니다. 키에 위치 번호가 들어가므로 같은 이벤트에서 앞쪽 다른 Hook을 지워도 다시 신뢰가 필요할 수 있습니다.

- `hooks status --tool codex`는 `hooks.json`의 항목만 확인하고 신뢰 여부는 보지 않습니다(Codex `/hooks`에서 확인).
- `uninstall`은 `hooks.json`의 소유 항목만 지우고 `config.toml`의 `hooks.state` 기록은 남깁니다(무해, Codex가 관리).
- `hooks.json` 최상위에는 `description`·`hooks`만 허용되므로(Codex가 파일 전체를 거부) 표시 키를 넣지 않고 명령 서명으로 소유를 판정합니다. `config.toml`의 `[hooks]` TOML 정의는 읽거나 바꾸지 않습니다(둘 다 있으면 Codex가 경고).
- 이전 `hooks print --tool codex` 출력을 손으로 넣었다면 `SessionEnd` timeout이 5라 `status`가 `modified`로 보이고 `install --yes`가 그 항목을 교체합니다(교체된 1개는 다시 신뢰).
- `hook codex`는 Codex 버전을 기록하지 않습니다. payload에 버전·실행 모드가 없어 수집 이벤트의 confidence는 `inferred`이고, CLI `PostToolUse`는 `unknown`으로 남습니다([어댑터 문서](../architecture/collector-adapters.md#버전-정책)).

### macOS에서 Codex 연결(소유자 안내, 미검증)

Claude Code가 이미 수집되는 Mac(Collector 등록·`run` 동작 중) 기준입니다. zsh에 그대로 붙여 넣을 수 있게 주석을 줄 끝에 두지 않았습니다.

확인:

```zsh
command -v agent-office-collector
agent-office-collector version
codex --version
codex features list | grep -w hooks
ls -l ~/.codex/hooks.json
```

`hooks`가 `false`면 `codex features enable hooks`를 실행합니다. Collector가 #170을 포함한 버전이면:

```zsh
agent-office-collector hooks install --tool codex
agent-office-collector hooks install --tool codex --yes
agent-office-collector hooks status --tool codex
```

이전 버전(0.1.0-beta.1)만 있고 `~/.codex/hooks.json`이 **없을 때만** 수동으로 만듭니다(있으면 덮어쓰지 말고 새 버전을 기다립니다).

```zsh
agent-office-collector hooks print --tool codex > ~/.codex/hooks.json
```

그다음 새 터미널에서 `codex`를 실행해 "Hooks need review" 화면에서 `2`(Trust all and continue)를 고르고 Enter, 아무 작업이나 한 번 시킨 뒤 웹 사무실에 Codex 세션이 보이는지 확인합니다. 보이지 않으면 `agent-office-collector status`의 `enqueued_hook`과 Codex `/hooks`의 신뢰 상태를 확인합니다.

## 파일 위치

| OS | 설정(config.json, pause.json) | 상태(queue.db, status.json, spool/, collector.lock, baseline.json) |
|---|---|---|
| Linux 등 | `${XDG_CONFIG_HOME:-~/.config}/agent-office/collector` | `${XDG_STATE_HOME:-~/.local/state}/agent-office/collector` |
| macOS | `~/Library/Application Support/AgentOffice/Collector` | 같은 위치의 `state/` |
| Windows | `%APPDATA%\AgentOffice\Collector` | `%LOCALAPPDATA%\AgentOffice\Collector` |
| 재정의 | `$AGENT_OFFICE_COLLECTOR_HOME/config` | `$AGENT_OFFICE_COLLECTOR_HOME/state` |

디렉터리는 0700, 파일은 0600으로 만듭니다(Windows는 사용자 프로필 ACL). 설정 파일은 임시 파일 → fsync → rename으로 원자적으로 쓰고, 심볼릭 링크 대상이면 쓰지 않습니다. 장치 토큰은 OS 보안 저장소에 두고 `config.json`에는 `token_store`만 남깁니다(대체 경로에서만 파일에 토큰).

## 장치 토큰 보관(#41)

| OS | 저장소 | 라이브러리 경로 |
|---|---|---|
| Linux | Secret Service(D-Bus: GNOME Keyring, KWallet 등) 기본 모음 | `github.com/zalando/go-keyring v0.2.8` → `godbus/dbus/v5` |
| macOS(미검증) | 로그인 키체인 일반 암호 | `/usr/bin/security`(비밀 값은 `-i` 표준 입력으로 전달) |
| Windows(미검증) | 자격 증명 관리자 일반 자격 증명 | `danieljoos/wincred`(순수 Go) |

- 항목: 서비스 `io.github.apg0001.agent-office.collector`, 계정 = 장치 ID(비밀 아님). 토큰은 출력·로그·진단에 넣지 않습니다.
- `register`는 저장소에 쓰고 다시 읽어 같을 때만 `config.json`에 `"token_store": "keyring"`을 쓰고 토큰 필드를 비웁니다. `config.json` 쓰기가 실패하면 방금 만든 항목을 지웁니다.
- **대체 경로**: 저장소를 쓸 수 없으면(세션 D-Bus 없음 — headless 서버·컨테이너·SSH, 저장소 잠김·거부, 10초 응답 없음) 0600 `config.json`에 `"token_store": "file"`로 둡니다. `status`·`diagnose`의 `token_storage`가 `file_fallback`, 텍스트 출력은 "장치 토큰 보관: 설정 파일(0600, 대체 경로…)"입니다. 정상은 `os_keyring`입니다.
- Linux에서 `DBUS_SESSION_BUS_ADDRESS`도 `/run/user/<uid>/bus`도 없으면 저장소를 시도하지 않습니다(D-Bus 자동 실행 `dbus-launch` 방지). systemd 사용자 서비스는 보통 세션 버스 주소를 물려받습니다(실제 세션 미검증).
- `AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`이면 저장소를 쓰지 않습니다(알 수 없는 값도 `file`로 취급). 비우거나 `auto`면 저장소를 먼저 시도합니다.
- **이전**: `run`이 시작하거나 등록 정보 변경으로 다시 시작할 때, 파일에 토큰이 있으면(#41 이전 형식 포함) 저장소에 쓰고 다시 읽어 확인한 뒤 `config.json`을 토큰 없이 원자적으로 다시 씁니다. 실패하면 파일 보관을 유지하고 다음 시작에서 다시 시도합니다. 디스크의 이전 블록까지 덮어쓰지는 않습니다.
- `token_store`가 `keyring`인데 저장소에서 읽지 못하면 `run`은 `token_unavailable` 상태로 10초마다 다시 시도하고 아무것도 보내지 않습니다.
- `hook` 명령은 토큰을 읽지 않습니다(등록 여부만 확인).

## 자동 시작(`service`)

| OS | 정의 위치 | `--activate` 명령 |
|---|---|---|
| Linux | `~/.config/systemd/user/agent-office-collector.service` (`Restart=on-failure`, `WantedBy=default.target`) | `systemctl --user daemon-reload`, `systemctl --user enable --now agent-office-collector.service` |
| macOS | `~/Library/LaunchAgents/io.github.apg0001.agent-office.collector.plist` (`RunAtLoad`, 비정상 종료 시 `KeepAlive`) | `launchctl bootout`(무시) → `launchctl bootstrap gui/<uid> <plist>` |
| Windows | `%LOCALAPPDATA%\AgentOffice\Collector\service\agent-office-collector-task.xml` (UTF-16LE, 로그온 트리거, `InteractiveToken`, 최소 권한, 실패 시 1분 뒤 재시작) | `schtasks /Create /TN \AgentOffice\Collector /XML <xml> /F`, `schtasks /Run` |

- 관리자 권한을 쓰지 않는 사용자 수준 정의만 만듭니다.
- 모든 정의에 소유 표시 `agent-office-collector:managed-service:v1`을 넣습니다. 설치는 같은 위치에 소유 표시 없는 파일이 있으면 **덮어쓰지 않고 실패**하고, 제거는 소유 표시가 있는 파일만 지웁니다. Windows는 같은 이름의 등록된 작업 XML도 조회해 소유 표시가 없으면 `/F` 교체·삭제를 하지 않습니다.
- `go run` 임시 바이너리로는 설치하지 않습니다. 빌드한 실행 파일을 고정 위치에 둔 뒤 설치하세요(정의에 실행 파일 절대 경로가 들어감).
- 알려진 미검증 위험: Windows `InteractiveToken` 작업이 콘솔 바이너리를 실행하면 콘솔 창이 보일 수 있습니다. GUI 패키지(#42) 또는 `-H windowsgui` 빌드에서 결정합니다.

## 진단(`status`·`diagnose`)

출력에는 버전, OS·아키텍처, 등록 여부, 장치 토큰 보관 위치(`token_storage`: `os_keyring`/`file_fallback`), 서버 scheme과 loopback 여부(호스트 이름은 표시하지 않음), 일시중지, run 프로세스 실행 여부, 마지막 실행 상태, 감시 루트 상태(`available`/`not_found`/`unavailable`), 개수(`acked`, `enqueued_transcript`, `normalize_*`, `baseline_skipped_files` 등), 오류 코드별 개수·마지막 시각, 큐 대기·drop-oldest·dead-letter 개수, 자동 시작 정의 상태만 들어갑니다. 위치는 `$XDG_STATE_HOME/agent-office/collector` 같은 기호 이름으로만 표시합니다.

넣지 않는 값: 장치 토큰, 등록 코드, 서버 호스트·URL, 장치 ID, 로컬 경로 원문, 세션·도구 ID, 이벤트 내용, OS·네트워크 오류 문자열(경로·URL 포함 가능).

오류 코드: `device_revoked`, `http_4xx`, `http_429`, `http_5xx`, `http_redirect`, `network`, `timeout`, `queue_error`, `watch_root_unavailable`, `tail_permission_denied`, `tail_unsupported`, `tail_error`, `spool_invalid`, `spool_error`, `pause_state_invalid`, `status_write_failed`.

`diagnose`의 서버 도달 확인은 인증 없이 `GET /api/v1/system/health`만 보냅니다(5초, 리다이렉트 미추종). 결과는 `reachable`, `http_<상태>`, `network`, `timeout`, `skipped`(`--offline`), `not_configured`입니다.

## 예시(Linux)

```bash
cd collector/core
go build -o ~/.local/bin/agent-office-collector ./cmd/agent-office-collector
agent-office-collector register --server https://office.example --code ABCD-EFGH --name "내 PC"
agent-office-collector run            # 포그라운드 확인 후 Ctrl+C
agent-office-collector service install --activate
agent-office-collector diagnose
```

## 검증

Linux arm64 Docker `golang:1.24-bookworm`에서 `gofmt -l`, `go vet ./...`(linux·`GOOS=windows`·`GOOS=darwin`), Windows·macOS 교차 빌드, `go test -race ./...`를 실행했습니다.

| 테스트 | 내용 |
|---|---|
| `config_test.go`, `fsutil_test.go` | 설정 0600·디렉터리 0700, 원자적 교체·임시 파일 미잔류, 심볼릭 링크 대상 거부, 서버 URL 규칙, 토큰 `fmt` 출력 가림, 손상된 pause 파일은 일시중지로 처리 |
| `paths_test.go`, `instance_test.go`, `spool_test.go` | OS별 위치 규칙, 단일 실행 잠금, spool 순서·상한·잘못된 파일·오래된 임시 파일 |
| `service_test.go` | OS별 골든(systemd·launchd·작업 XML), systemd 인용·XML 이스케이프·UTF-16LE BOM·CRLF, 상대 경로·개행 주입 거부, 소유 파일만 교체·제거, 남의 파일·작업은 명령 실행 없이 거부, 활성화 실패 보고 |
| `cli_test.go` | Hook fail-open 9가지 입력(인자 없음, 미지 제공자, 잘못된 플래그, 미등록, 깨진 JSON, 빈 입력, 세션 없음, 8MiB 초과, 일시중지)과 끝나지 않는 stdin·쓸 수 없는 spool, 실제 하위 프로세스 종료 코드 0·출력 없음, spool에 원문 sentinel 없음, pause/resume, 진단 출력에 토큰·경로·호스트·장치 ID sentinel 없음, service 명령, 두 번째 `run` 거부 |
| `hooks_test.go`, `cli/hooks_test.go` (#31) | Claude·Codex × linux·darwin·windows 골든, 위험 경로 거부, 소유 판정, 남의 Hook·알 수 없는 키 보존과 백업(0600)·권한 보존, 제거 후 의미상 동일 JSON, 두 번째 설치·제거 무변경, 깨진 JSON 7종 거부(쓰기·백업 없음), 읽기 전용 파일·심볼릭 링크·쓸 수 없는 디렉터리 거부, 실행 파일 이동 교체·`partial` 판정, `--yes` 없는 미리보기, CLI 출력에 토큰·홈 경로 없음, `claude_version` 기록, 버전 문자열 추출·검증 수준 판정. #170: Codex `$CODEX_HOME/hooks.json` 미리보기·설치·`status`(텍스트·JSON)·멱등·제거 후 원본과 동일, 최상위 키(`description`) 보존·표시 키 없음, 권한 유지·백업 0600, SessionEnd 3초와 이전 print 출력(5초) 교체, Codex 홈 없음·심볼릭 링크·깨진 파일 거부(쓰기·백업 없음), Claude 설정 미변경 |
| `secret_test.go`, `config/token_test.go` (#41) | 저장소 선택(강제 `file`·알 수 없는 값·세션 버스 없음·OS), 호출 시간 제한·OS 오류 문자열 버림, `token_store` 검증 규칙, 저장 후 재조회 검증·실패 시 항목 정리·파일 대체 경로, `config.json` 쓰기 실패 시 항목 롤백, 파일 토큰 이전(다른 필드 보존·두 번째 실행 무변경) |
| `cli/logout_test.go` (#41) | register가 `config.json`에 토큰을 쓰지 않음·`status` `os_keyring`, 저장소 없음 → `file_fallback`, `--force`가 이전 항목 삭제, logout 8가지(204·대체 경로·401·500·네트워크 오류·`--local-only`·저장소 잠김·잠김+`--local-only`)에서 서버 호출 Bearer·삭제 여부·출력에 토큰/URL 없음, 손상 설정, `run`의 legacy 토큰 이전과 logout 뒤 heartbeat 중지, 잠긴 저장소 → `token_unavailable` → 풀리면 전송. `AGENT_OFFICE_TEST_REAL_KEYRING=1`이면 실제 저장소로 register→logout→이전 |
| `e2e_test.go` | `httptest` 서버로 register → run → 기존 transcript 건너뜀 → 새 transcript(#20 합성 fixture `tests/fixtures/collector/claude/dual-source/transcript.jsonl`)와 Hook 이벤트 전송·heartbeat → 일시중지 동안의 줄·Hook 미수집 → 재개 후 새 줄 수집 → 정상 종료(`stopped`, 큐 0) → diagnose `reachable`. 서버 본문·상태 디렉터리에 원문·토큰 sentinel 없음, 같은 event_id 중복 전달 없음 |

## 알려진 제한·후속

- macOS·Windows 실장비에서 `run`·`service install --activate`·`hook`을 실행하지 않았습니다(#62 실장비 검증 또는 별도 Issue).
- 실제 systemd 사용자 세션에서 unit을 기동하지 않았습니다. 로그아웃 뒤에도 실행하려면 `loginctl enable-linger`가 필요하며 자동으로 켜지 않습니다.
- 큐 기록이 실패하면 그 주기에 정규화한 결정적 중복 제거 키가 메모리에 남아, 같은 Hook·transcript 이벤트가 이번 실행에서 다시 나오지 않을 수 있습니다(디스크 가득 참 등 드문 경우).
- 장치 토큰 회전(`/devices/token/rotate`) 명령은 아직 없습니다.
- macOS Keychain(첫 접근 허용 프롬프트, 서명 안 된 바이너리 교체 시 재허용)·Windows Credential Manager·실제 데스크톱 세션의 Secret Service 자동 잠금 해제는 실장비에서 확인해야 합니다.
- Hook 설치(#31): 실제 Claude Code가 설치된 Hook을 호출하는 종단 확인, macOS·Windows 명령 인용 실기 확인이 남았습니다. Codex 사용자 범위 위치는 #170에서 Linux로 실측했고 macOS·Windows·Desktop은 남았습니다.
- 제거 도우미(`agent-office-collector-uninstall`, `uninstall.ps1`)는 아직 Claude Hook만 제거합니다. Codex Hook은 `hooks uninstall --tool codex --yes`로 따로 제거합니다(#170 후속). 대화형 Claude 모드가 실측되기 전까지 `claude_mode`는 자동으로 채우지 않습니다.

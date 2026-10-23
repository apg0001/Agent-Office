# Collector CLI·백그라운드 실행·진단

관련 이슈: #34 (`EP-04/ISS-06`, 요구사항 COL-001~003, COL-010). 선행: #29 어댑터, #30 tail, #32 정규화, #33 큐·전송.

GUI(#40~#44)와 분리된 headless 실행 파일 `agent-office-collector`입니다. GUI는 나중에 이 명령들을 감쌉니다. Hook 자동 설치·제거(#31), OS 보안 저장소 토큰 보관(#41), 설치 패키지·서명(#42~#44)은 범위가 아닙니다.

## 검증 범위

| 대상 | 상태 | 근거 |
|---|---|---|
| Linux arm64(Docker `golang:1.24-bookworm`, Raspberry Pi 5 호스트) | **검증**: 단위·통합·종단 테스트(`go test -race`) | 아래 [검증](#검증) |
| Linux systemd 사용자 서비스 | **생성만 검증**: unit 파일 골든 테스트. 실제 `systemctl --user`로 등록·기동하지 않음(컨테이너에 systemd 없음) | `service/testdata/systemd.service.golden` |
| macOS launchd LaunchAgent | **미검증**: plist 생성 골든 테스트와 `GOOS=darwin go vet`·교차 빌드만 수행. 실제 macOS에서 실행하지 않음 | `launchd.plist.golden` |
| Windows 작업 스케줄러(로그온 작업) | **미검증**: 작업 XML 생성 골든 테스트와 `GOOS=windows go vet`·교차 빌드만 수행. `schtasks` 등록·UTF-16 XML 수용·콘솔 창 표시 여부를 실제 Windows에서 확인하지 않음 | `task.xml.golden` |
| 실제 Agent Office 서버(#35)와의 종단 실행 | **미실행**: `httptest` 가짜 서버로만 확인 | `e2e_test.go` |
| 실제 Claude Code·Codex가 `hook` 명령을 호출하는 경로 | **미실행**: Hook 설정 등록은 #31 범위 | — |

검증되지 않은 OS에서는 지원한다고 표시하지 않습니다. 실장비 검증 전까지 macOS·Windows 서비스 정의는 미리보기·수동 등록용입니다.

## 명령

| 명령 | 동작 |
|---|---|
| `register --server URL --code XXXX-XXXX --name PC [--force]` | `POST /api/v1/devices/register`로 장치를 등록하고 `device_id`·장치 토큰을 설정 파일(0600, 원자적 쓰기)에 저장합니다. 토큰은 출력하지 않습니다. 서버 주소는 https만 받고, http는 `localhost`·`127.x`·`::1`만 허용합니다. 이미 등록돼 있으면 `--force`가 필요합니다 |
| `run` | 포그라운드 장기 실행. SIGINT/SIGTERM(Windows는 Ctrl+C)으로 정상 종료(큐 보존, 종료 코드 0). 사용자당 1개만 실행(잠금 파일) |
| `hook claude\|codex [--product codex-desktop] [--tool-version V] [--mode M]` | Hook stdin JSON 한 건을 받아 실행 중인 `run`에 spool로 넘깁니다. **항상 종료 코드 0**, stdout·stderr 출력 없음 |
| `pause` / `resume` | 일시중지 상태를 설정 디렉터리 `pause.json`에 저장합니다. 실행 중인 `run`이 다음 주기(기본 2초)에 반영합니다 |
| `status [--json]` | 로컬 상태 요약 |
| `diagnose [--json] [--offline]` | `status` + Go 런타임 버전, 기호 위치, 서버 health 도달 여부 |
| `service install\|uninstall [--activate]` | 자동 시작 정의 설치·제거. `--activate`가 없으면 파일만 쓰고 실행할 명령을 안내합니다 |
| `service print [--os linux\|darwin\|windows] [--exe PATH]` | 정의 미리보기(파일을 쓰지 않음) |
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
- transcript 런타임 버전은 아직 자동 감지하지 않습니다(#31). 설정 파일 `claude_version`·`claude_mode`가 비어 있으면 미검증 런타임으로 보고 confidence를 `inferred`로 낮춥니다.

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

## 파일 위치

| OS | 설정(config.json, pause.json) | 상태(queue.db, status.json, spool/, collector.lock, baseline.json) |
|---|---|---|
| Linux 등 | `${XDG_CONFIG_HOME:-~/.config}/agent-office/collector` | `${XDG_STATE_HOME:-~/.local/state}/agent-office/collector` |
| macOS | `~/Library/Application Support/AgentOffice/Collector` | 같은 위치의 `state/` |
| Windows | `%APPDATA%\AgentOffice\Collector` | `%LOCALAPPDATA%\AgentOffice\Collector` |
| 재정의 | `$AGENT_OFFICE_COLLECTOR_HOME/config` | `$AGENT_OFFICE_COLLECTOR_HOME/state` |

디렉터리는 0700, 파일은 0600으로 만듭니다(Windows는 사용자 프로필 ACL). 설정 파일은 임시 파일 → fsync → rename으로 원자적으로 쓰고, 심볼릭 링크 대상이면 쓰지 않습니다. 장치 토큰은 지금 0600 설정 파일에 있으며 OS 보안 저장소 이전은 #41입니다.

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

출력에는 버전, OS·아키텍처, 등록 여부, 서버 scheme과 loopback 여부(호스트 이름은 표시하지 않음), 일시중지, run 프로세스 실행 여부, 마지막 실행 상태, 감시 루트 상태(`available`/`not_found`/`unavailable`), 개수(`acked`, `enqueued_transcript`, `normalize_*`, `baseline_skipped_files` 등), 오류 코드별 개수·마지막 시각, 큐 대기·drop-oldest·dead-letter 개수, 자동 시작 정의 상태만 들어갑니다. 위치는 `$XDG_STATE_HOME/agent-office/collector` 같은 기호 이름으로만 표시합니다.

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
| `e2e_test.go` | `httptest` 서버로 register → run → 기존 transcript 건너뜀 → 새 transcript(#20 합성 fixture `tests/fixtures/collector/claude/dual-source/transcript.jsonl`)와 Hook 이벤트 전송·heartbeat → 일시중지 동안의 줄·Hook 미수집 → 재개 후 새 줄 수집 → 정상 종료(`stopped`, 큐 0) → diagnose `reachable`. 서버 본문·상태 디렉터리에 원문·토큰 sentinel 없음, 같은 event_id 중복 전달 없음 |

## 알려진 제한·후속

- macOS·Windows 실장비에서 `run`·`service install --activate`·`hook`을 실행하지 않았습니다(#62 실장비 검증 또는 별도 Issue).
- 실제 systemd 사용자 세션에서 unit을 기동하지 않았습니다. 로그아웃 뒤에도 실행하려면 `loginctl enable-linger`가 필요하며 자동으로 켜지 않습니다.
- 큐 기록이 실패하면 그 주기에 정규화한 결정적 중복 제거 키가 메모리에 남아, 같은 Hook·transcript 이벤트가 이번 실행에서 다시 나오지 않을 수 있습니다(디스크 가득 참 등 드문 경우).
- 장치 토큰 회전(`/devices/token/rotate`), 로그아웃 시 `/devices/revoke` 호출, OS 보안 저장소는 #41입니다.
- Hook 설정 자동 설치(백업·소유 표시·원자적 쓰기)와 도구 버전 감지는 #31입니다.

# Codex app-server 관측 범위와 제한

관련 이슈: #21

실측일: 2026-10-23 (KST), Raspberry Pi 5 Linux arm64, `codex-cli 0.163.0`, 로그인하지 않은 상태, 사용자 상태와 분리한 임시 `CODEX_HOME`

## 결론

- app-server는 **자신이 시작했거나 같은 app-server 프로세스에 올라와 있는 thread만** 관측할 수 있습니다. 다른 프로세스에서 실행 중인 임의의 Desktop/CLI 세션에는 붙을 수 없습니다(4/4 재현).
- 같은 프로세스 안에서는 두 번째 클라이언트가 `thread/loaded/list`로 thread를 보고, 첫 턴이 저장된 뒤 `thread/resume`으로 **실행 중인 thread에 합류**해 `turn/*`·`item/*` 알림을 실시간으로 받았습니다(4/4). 첫 턴 전 합류는 거부(`-32600`)됐습니다.
- 합류 전에는 같은 프로세스의 다른 클라이언트도 `thread/started`, `thread/status/changed`, `thread/name/updated`만 받았습니다. 활동 알림(`turn/*`, `item/*`)은 thread를 구독한 클라이언트에게만 갑니다.
- CLI 도움말은 대화형 CLI가 "공유 백그라운드 서버(daemon)"를 쓸 수 있다고 설명합니다(`--no-daemon`, `codex agents`). 그러나 **실제 CLI·Desktop 세션이 이 daemon에 올라오는지는 검증하지 못했습니다.** Pi에는 Codex 로그인과 Desktop이 없고, 로그인 없는 `codex exec`는 인증 단계에서 끝나 daemon에 thread를 만들지 않았습니다.
- 따라서 **app-server를 Collector의 기본 수집 경로로 쓰지 않습니다.** Codex 기본 경로는 #17의 Hook(+보조 transcript)입니다. app-server는 아래 보안 조건을 만족하는 선택형 보조 경로로만 검토합니다.

## 프로토콜 표면

`codex app-server generate-json-schema`(로그인 불필요)로 생성한 안정판 스키마 기준입니다. `--experimental`을 붙이면 클라이언트 요청이 172개로 늘어납니다.

| 구분 | 개수 | Collector 관련 예 |
|---|---:|---|
| 클라이언트 요청 | 105 | `initialize`, `thread/list`, `thread/loaded/list`, `thread/read`, `thread/resume`, `thread/unsubscribe`, `thread/turns/list`, `hooks/list` |
| 서버 알림 | 85 | `thread/started`, `thread/status/changed`, `thread/closed`, `turn/started`, `turn/completed`, `turn/plan/updated`, `item/started`, `item/completed`, `hook/started`, `hook/completed`, `serverRequest/resolved` |
| 서버 요청 | 10 | `item/commandExecution/requestApproval`, `item/fileChange/requestApproval`, `item/permissions/requestApproval`, `item/tool/requestUserInput` |
| 클라이언트 알림 | 1 | `initialized` |

같은 요청 목록에 `fs/writeFile`, `fs/remove`, `command/exec`, `thread/shellCommand`(샌드박스 밖 실행), `config/value/write`, `turn/start`, `turn/interrupt`도 있습니다. 제어 소켓에 연결한 클라이언트는 관측만이 아니라 **실행과 파일 변경 권한**도 갖습니다.

`item/agentMessage/delta`, `item/commandExecution/outputDelta`, `item/reasoning/textDelta` 같은 알림은 모델 출력·명령 출력 원문을 담습니다. app-server를 쓰더라도 알림은 메서드 이름과 식별자만 남기고 본문은 즉시 폐기해야 합니다.

## 전송 방식

- `codex app-server`(기본 `stdio://`): 줄 단위 JSON-RPC
- `--listen unix://PATH`: **unix socket 위의 WebSocket**(HTTP `101 Switching Protocols` 확인). 줄 단위 JSON을 그대로 보내면 응답이 없습니다.
- `codex app-server daemon start`: `CODEX_HOME/app-server-control/` 아래 제어 소켓을 엽니다. 디렉터리 권한은 `drwx------`(소유자 전용)이었습니다. 소켓 경로는 `/tmp/codex-daemon-<uid>/<hash>`를 가리키는 심볼릭 링크였습니다.

## 실험

하네스: [`tests/poc/codex/app_server_probe.py`](../../tests/poc/codex/README.md). 모델 호출이 필요한 `turn/start`는 보내지 않고, 모델 없이 턴을 만드는 `thread/shellCommand`(`true`)와 `thread/name/set`만 사용했습니다.

| 단계 | 같은 서버(unix socket, 클라이언트 2) | 다른 서버(stdio 프로세스 2) |
|---|---|---|
| A가 `thread/start` | 성공(로그인 불필요) | 성공 |
| B의 `thread/loaded/list`에 A의 thread | 보임 | 안 보임(0개) |
| 첫 턴 전 B의 `thread/resume` | `-32600`(rollout 없음) | `-32600` |
| A의 첫 턴·이름 변경 중 B가 받은 알림 | `thread/status/changed` 2, `thread/name/updated` 1 | 없음 |
| 첫 턴 후 B의 `thread/resume` | 성공(실행 중인 thread에 합류) | `-32600` |
| A의 두 번째 턴 중 B가 받은 알림 | `turn/started`, `item/started`, `item/completed`, `turn/completed`, `thread/status/changed` 2 | 없음 |
| B의 `thread/unsubscribe` | 성공 | 성공 |

반복: 4회 동일(첫 시도 1회는 하네스가 서버 종료를 10초만 기다려 실패했고, 대기 후 강제 종료하도록 고친 뒤 재실행). 결과 JSON에는 원본 thread ID·경로·명령이 없고 실행별 HMAC만 있습니다.

daemon 실험: 격리 daemon을 띄우고 관찰 클라이언트를 붙인 채 `codex exec`를 실행했습니다. exec는 인증 오류로 종료(exit 1)했고, 그동안 `thread/loaded/list`와 `thread/list`는 계속 0개였습니다. exec가 daemon을 거치는지는 **판정 불가**입니다.

## Collector 설계 결정

1. Codex 기본 수집은 Hook(#17)입니다. app-server는 기본으로 켜지 않습니다.
2. 실행 중인 임의 세션에 app-server로 붙는다고 가정하지 않습니다. 같은 프로세스가 아니면 목록에도 보이지 않습니다.
3. 공유 daemon 경로를 쓰려면 먼저 다음을 실측해야 합니다: 실제 로그인한 대화형 CLI와 Desktop 세션이 daemon의 `thread/loaded/list`에 나타나는지, `thread/resume` 합류가 원래 클라이언트의 상태·승인 흐름에 부작용을 주지 않는지.
4. 그 경우에도 Collector는 메서드 허용 목록(`initialize`, `thread/loaded/list`, `thread/resume`, `thread/unsubscribe`)만 보내고, 실행·쓰기 요청(`turn/*`, `thread/shellCommand`, `command/exec`, `fs/*`, `config/*`)을 보내지 않는 클라이언트로 제한해야 합니다. 제어 소켓 접근 자체가 실행 권한이므로 사용자 명시 동의와 ADR이 필요합니다.
5. 서버 요청(`*/requestApproval`)에 Collector가 응답해서는 안 됩니다. 응답하면 사용자의 승인을 대신하게 됩니다.

## 남긴 상태

- 실험용 `CODEX_HOME`, 작업공간, 소켓은 모두 삭제했습니다. 실험 daemon의 보조 프로세스(`pid-update-loop`)가 `daemon stop` 뒤에도 남아 있어 경로를 확인한 뒤 종료했습니다.
- `/tmp/codex-daemon-<uid>/`에 실험용 홈을 가리키던 해시 이름 항목이 남았을 수 있습니다. 사용자의 실제 daemon 항목과 구분할 수 없어 삭제하지 않았습니다.
- 사용자의 실제 `~/.codex`는 열지 않았습니다.

## 미실행·후속 범위

- 로그인한 대화형 CLI·Desktop 세션의 daemon 공유 여부(로그인 필요, Desktop은 #18)
- 모델 턴(`turn/start`)의 `item/*` 종류별 알림, 승인 요청(`serverRequest`)과 `serverRequest/resolved`
- 하위 에이전트(`multiAgentMode`)·`hook/*` 알림과 Hook 이벤트의 중복 관계
- Windows·macOS의 제어 소켓 위치와 권한
- `--remote ws://` 원격 연결과 인증 토큰

# Linux 실제 사용 흐름 검증(#62 Linux 부분)

실제 Claude Code가 설치된 Hook을 부르고, Collector가 받아 서버로 보내고, 사무실에 나타나는 흐름을 처음부터 끝까지 확인합니다.

```sh
tests/e2e/claude_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
```

## 하는 일

1. 운영 Compose를 새 볼륨으로 띄우고 bootstrap으로 관리자 생성, 웹 API로 PC 등록 코드 발급
2. Collector를 임시 홈(`AGENT_OFFICE_COLLECTOR_HOME`)으로 `register --code-stdin` → `run`
3. 이 실행에만 쓰는 Hook 설정 파일(SessionStart·UserPromptSubmit·PreToolUse·PostToolUse·Stop·SessionEnd → `agent-office-collector hook claude`)
4. 빈 작업 폴더에서 `claude -p "… Bash로 echo <표식> …"` 한 번(`--setting-sources project --settings <임시> --allowedTools Bash`)
5. 서버 스냅샷·타임라인 확인, PostgreSQL 전체 덤프에서 표식 문자열 검색

## 안전장치

- 사용자 `~/.claude/settings.json`을 바꾸지 않습니다(프로젝트 설정만 쓰고 임시 `--settings`로 Hook 주입). 실행 전후 SHA-256을 비교합니다.
- Collector의 transcript 감시 위치를 빈 폴더로 돌려 사용자의 다른 Claude 세션을 수집하지 않습니다(Hook만 수집).
- 프롬프트에 넣은 표식 문자열이 서버 DB 어디에도 없어야 통과합니다.

## 결과(Pi 5 arm64, Ubuntu 24.04, Claude Code 2.1.308, 2026-10-24)

| 항목 | 결과 |
|---|---|
| 세션 | 1개, 시작→종료(`ENDED`) |
| 이벤트 종류 | `session.started`·`turn.started`·`tool.started`·`tool.completed`·`turn.completed`·`session.ended` |
| 표식 문자열(DB 전체 덤프) | 0건 |
| 사용자 설정 | 변경 없음 |
| 소요 | 약 48초(스택 생성 포함) |

## 하위 에이전트·계획 시나리오(`SCENARIO=subagent`)

```sh
SCENARIO=subagent tests/e2e/claude_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
```

`TodoWrite`로 할 일 2개를 만들고 `Task`로 하위 에이전트에게 Bash를 시킨 뒤 할 일을 완료로 바꾸게 합니다. Hook은 PostToolUseFailure·PermissionRequest·SubagentStart·SubagentStop까지 등록하고 `--allowedTools "ToolSearch Task Bash TodoWrite"`로 실행합니다(2.1.x에서 TodoWrite는 지연 로딩 도구라 ToolSearch가 필요).

| 항목 | 결과(2026-10-24, 같은 환경) |
|---|---|
| 에이전트 | 2개, 하위 에이전트가 lead 아래(`subagents_under_lead` 1) |
| 계획 단계 | `["completed","completed"]`(`plan.updated`) |
| 표식 문자열(DB 전체 덤프) | 0건 |
| 사용자 설정 | 변경 없음 |

이 실측에서 `hook` → spool → `run` 경로가 계획 상태를 잃는 문제 두 가지를 찾아 고쳤습니다(#38): spool 레코드에 계획 상태 enum이 없었고, `run`의 spool 처리가 Pipeline을 거치지 않아 `plan.updated`를 만들지 않았습니다. 할 일 문장은 spool·서버 어디에도 남지 않습니다. TodoWrite 입력 형식(`tool_input.todos[].status`)은 이 환경에서 실제로 관측했지만 어댑터의 계획 지원 수준은 아직 `unverified`(신뢰도 `inferred`)로 둡니다.

## 남은 범위(#62)

- macOS·Windows에서 같은 흐름, Codex CLI·Codex Desktop(#87, Windows PC)
- 대화형 Claude 세션(승인 대기 `Notification`), 실제 `hooks install`로 설치한 사용자 설정 경로
- GUI에서 등록부터 사무실 확인까지(#40)

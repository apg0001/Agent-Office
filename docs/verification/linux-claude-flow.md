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

## 남은 범위(#62)

- macOS·Windows에서 같은 흐름, Codex CLI·Codex Desktop(#87, Windows PC)
- 대화형 Claude 세션(승인 대기 `Notification`, 하위 에이전트), 실제 `hooks install`로 설치한 사용자 설정 경로
- GUI에서 등록부터 사무실 확인까지(#40)

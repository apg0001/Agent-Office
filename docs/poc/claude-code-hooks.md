# Claude Code Hook·JSONL 실측

관련 이슈: #19

실측일: 2026-10-23 (KST)

## 결론

Raspberry Pi 5(Ubuntu 24.04.5, aarch64)의 Claude Code `2.1.308`에서 합성 `claude -p` 세션을 10회 실행했습니다. 10회 모두 exit code 0이었고, 같은 시나리오의 Hook 이벤트 개수는 반복 실행마다 동일했습니다.

- `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`는 매회 관측했습니다.
- `SubagentStart`·`SubagentStop`은 병렬 하위 에이전트 2개에서 매회 2회씩 관측했습니다.
- `PermissionRequest`는 권한 거부 시나리오에서 매회 1회 관측했습니다.
- Hook의 `tool_use_id`는 `stream-json` 출력과 로컬 transcript의 `tool_use.id`와 모두 일치했습니다. Hook의 `agent_id`는 하위 에이전트 transcript의 `agentId`와 일치했습니다.

Codex(#17)와 달리 `PostToolUse`가 관측되므로, 이 버전·OS에서는 Hook만으로 `tool.started`와 `tool.completed`를 만들 수 있습니다. 다만 이 결과는 **Linux arm64, 비대화형 `-p` 모드, Claude Code 2.1.308**에 한정됩니다. Windows·macOS, 대화형 TUI, 다른 버전은 지원으로 표시하지 않습니다.

## 공식 기준

- [Claude Code Hooks 문서](https://code.claude.com/docs/en/hooks)는 Hook 이벤트, 입력 필드, 종료 코드 의미(0 성공, 2 차단, 그 밖은 비차단 오류)와 matcher를 정의합니다.
- 같은 문서는 `session_id`, `prompt_id`, `transcript_path`, `cwd`, `scratchpad_dir`, `permission_mode`, `effort`, `hook_event_name`을 공통 입력으로, `Stop`의 `background_tasks`·`session_crons`, `PostToolUse`의 `duration_ms`를 이벤트별 입력으로 정의합니다. `PermissionRequest`에는 `tool_use_id`가 없고, 프롬프트를 표시할 수 없는 세션에서는 Hook 결정이 없으면 도구 호출을 거부한다고 설명합니다.
- 대부분 이벤트의 Hook 기본 타임아웃은 600초(`UserPromptSubmit`은 30초)입니다. 종료 코드가 0도 2도 아니면 비차단 오류로 처리됩니다.
- `-p`의 도움말은 "설정 파일 검증에 실패하면 print 모드에서 조용히 무시된다"고 명시합니다. 이 하네스는 기록 0건으로 이 상황을 감지합니다.

## 안전한 실측 방법

하네스는 [`tests/poc/claude`](../../tests/poc/claude/README.md)에 있습니다.

- 임시 Git 작업공간과 합성 파일만 사용하고 종료 시 삭제
- `--setting-sources project` + 작업공간 밖 임시 `--settings` 파일로 프로브 Hook만 주입. `~/.claude/settings.json`은 10회 모두 실행 전후 SHA-256 동일
- `--strict-mcp-config`, `--disable-slash-commands`, `--tools`로 도구 집합 제한, `--permission-mode default`, `--permission-prompts none`(묻는 대신 자동 거부)
- 실행 중인 부모 Claude Code 세션의 `CLAUDE*` 환경 변수를 자식에서 제거
- Hook stdin, `stream-json` stdout, stderr 원문을 저장하지 않음. 결과 JSON 10개에서 합성 프롬프트·파일명·명령·임시 경로·홈 경로 패턴 일치 0건
- transcript는 Hook이 넘겨준 새 합성 세션 경로만 열고, `~/.claude` 밖 경로는 읽지 않음. 최대 4MiB·5,000줄

모델은 `sonnet` 별칭, 총 비용은 `stream-json` result 기준 약 $0.21입니다. 허용 필드 목록을 공식 문서에 맞춰 보정한 뒤 최종 프로브로 `basic`을 1회 더 실행해 같은 이벤트 개수와 미등록 필드 0개를 확인했습니다(결과 표에는 미포함). 실행마다 `~/.claude/projects/` 아래 합성 프로젝트 디렉터리가 하나씩, 총 11개 생성됐습니다. Claude Code가 관리하는 원본 기록이므로 삭제하지 않았으며 실제 사용자 내용은 없습니다.

## 반복 관측 결과

| 시나리오 | 횟수 | Hook 이벤트(매회) | 비고 |
|---|---:|---|---|
| `basic`(Read 1 + Bash 1) | 3 | Start 1, Prompt 1, Pre 2, Post 2, Stop 1, End 1 | 약 6.2초, $0.014 |
| `basic` + Hook 비정상 종료(exit 1) | 1 | 위와 동일 | Claude 정상 완료, 10.2초 |
| `basic` + Hook 타임아웃(2초) | 1 | 위와 동일 | Claude 정상 완료, 21.8초, stderr 1줄 |
| `subagents`(병렬 2) | 3 | Start 1, Prompt 3, Pre 4, Post 4, SubStart 2, SubStop 2, Stop 3, End 1 | 약 11.3초, $0.030~$0.039 |
| `denied`(Write 자동 거부) | 2 | Start 1, Prompt 1, Pre 1, PermissionRequest 1, Stop 1, End 1 | 1회는 미확인 이벤트 추가 등록 |

미관측: `Notification`, `PostToolUseFailure`, `PreCompact`는 등록했지만 전 시나리오에서 0회였습니다. 추가 등록한 `PermissionDenied`, `StopFailure`, `PostCompact`도 0회였습니다. 이 이름들을 등록해도 설정 전체가 무시되지는 않았습니다(다른 이벤트는 정상 기록).

`SessionStart.source`는 `startup`, `SessionEnd.reason`은 `-p` 정상 종료에서 `other`였습니다.

## 상관관계와 중복

| 키 | 관측 | 근거(10회) |
|---|---|---|
| `session_id` | 한 실행의 모든 Hook과 `stream-json`이 같은 값 | HMAC 1종, Hook·stream 일치 |
| `prompt_id` | 문서화된 공통 필드. 턴마다 새 값, 하위 에이전트 Hook은 부모 턴 값을 공유 | `SessionStart`에는 없고 첫 `UserPromptSubmit`부터 `SessionEnd`까지 존재 |
| `tool_use_id` | `PreToolUse`·`PostToolUse`가 같은 값. stream의 `tool_use.id`, transcript `tool_use.id`와 일치 | 거부 시나리오는 Post 없음 |
| `agent_id` | 하위 에이전트마다 고유. 하위 에이전트 내부 `PreToolUse`·`PostToolUse`에도 `agent_id`·`agent_type` 포함 | Hook·하위 transcript `agentId` 일치 |
| `PermissionRequest` | `tool_use_id`가 **없음**(문서와 일치) | `prompt_id`와 직전 `PreToolUse` 순서로만 연결 가능 |

Hook 내부 중복은 0이었습니다. 반면 transcript는 같은 `tool_use` 블록을 여러 줄에 다시 기록했습니다(`basic`: `tool_use` 블록 4개, 고유 ID 2개). JSONL을 보조 소스로 쓰면 `tool_use_id` 기준 중복 제거가 필수입니다. Hook·JSONL 교차 중복과 재시작은 #20에서 검증합니다.

## 하위 에이전트와 턴 경계

- `Agent` 도구의 `PostToolUse`는 하위 에이전트가 끝나기 **전**에 옵니다. 하위 에이전트가 백그라운드 작업으로 실행되기 때문입니다.
- 하위 에이전트의 완료 알림은 새 턴으로 주입되어 `UserPromptSubmit`이 추가로 발생합니다. 병렬 2개에서 사용자가 보낸 프롬프트는 1개지만 `UserPromptSubmit`과 `Stop`은 3회였습니다. `UserPromptSubmit`을 "사용자 입력"으로 해석하면 안 됩니다.
- 한 실행에서는 하위 에이전트가 아직 일하는 중에 메인 `Stop`이 먼저 왔습니다. `Stop`·`SubagentStop` 입력의 `background_tasks`는 아직 끝나지 않은 작업을 알려주는 근거입니다. 문서상 그 항목에는 `description`·`command`, `session_crons`에는 `prompt` 원문이 들어갈 수 있으므로 상태·개수 외에는 폐기해야 합니다.
- `stream-json`에서는 하위 에이전트 메시지가 `parent_tool_use_id`로 부모 `Agent` 호출에 연결됐고(매회 6줄), `task_started`·`task_progress`·`task_notification` subtype이 관측됐습니다.

## 로컬 transcript 구조

Hook이 넘겨준 경로는 사용자 Claude 상태 아래 다음 형태였습니다.

```text
projects/<redacted>/<redacted>.jsonl
projects/<redacted>/<redacted>/subagents/<redacted>.jsonl
```

- 메인 transcript는 `SessionStart`·첫 `UserPromptSubmit` 시점에는 **아직 없고**, 첫 `PreToolUse` 시점에 생깁니다.
- 하위 에이전트 기록은 메인 파일의 sidechain 줄이 아니라 별도 `subagents/` 파일에 기록됐습니다(메인 `isSidechain` 줄 0, 하위 파일 18줄 중 11줄).
- 줄 수는 실행 중 18→19→25→26→31→35처럼 증가했습니다. 깨진 줄은 0이었지만 tail은 partial write를 전제로 해야 합니다.
- 줄 `type`은 `user`, `assistant`, `system`, `attachment`, `queue-operation` 외에 `api-request`, `api-request-blob`, `api-request-shape`, `atis-latch`, `cost-state`, `last-prompt` 같은 내부 값이 관측됐습니다. transcript 형식은 Collector의 안정 API로 간주하지 않습니다.

## fail-open

프로브는 모든 입력에서 exit 0, stdout 없음입니다. 일부러 기록 후 exit 1을 내거나 2초 타임아웃을 넘겨도 Claude는 도구 2회를 실행하고 `success`로 끝났습니다. 다만 동기 Hook의 타임아웃은 이벤트마다 지연을 그대로 더했습니다(6.2초 → 21.8초). 기본 타임아웃이 600초이므로 Collector Hook은 `timeout`을 반드시 짧게 명시하고, 로컬 큐에 기록만 한 뒤 즉시 반환해야 합니다. 종료 코드 2는 차단 의미이므로 어떤 오류에도 쓰지 않습니다.

## 정규화 결정

| 관측 입력 | 이벤트 v1 후보 | 신뢰도/제약 |
|---|---|---|
| `SessionStart` | `session.started` | `observed`; source `hook` |
| `UserPromptSubmit` | `turn.started` | `observed`; `prompt` 즉시 폐기. 하위 에이전트 완료 알림 턴도 포함됨 |
| `PreToolUse` + 안전한 도구 분류 | `tool.started` | `observed`; `tool_input` 폐기 |
| `PostToolUse` | `tool.completed` | `observed`; `tool_response` 폐기. `Agent` 도구는 하위 에이전트 종료 의미가 아님 |
| `SubagentStart`/`SubagentStop` | `agent.started`/`agent.completed` | `observed`; `agent_id`로만 인원 생성 |
| 하위 에이전트 내부 도구 Hook | 해당 하위 `agent_id`의 `tool.*` | `agent_id` 존재 여부로 부모와 구분 |
| `PermissionRequest` | `approval.requested` 후보 | `-p` 자동 거부에서만 관측. 대화형 승인 대기와 `approval.resolved` 근거는 미검증이므로 아직 `waiting_user` 활동으로 쓰지 않음 |
| `Stop` | `turn.completed` 후보 | 세션 종료 아님. `background_tasks`가 남아 있으면 IDLE 근거가 아님 |
| `SessionEnd` | `session.ended` | `observed`; `-p` 정상 종료에서만 확인 |
| transcript/`stream-json` | 위 이벤트 보강 | Hook과 중복, `tool_use_id`로 중복 제거. #20 전 사용 금지 |

`prompt_id`, `effort`, `duration_ms`, `background_tasks`는 문서화돼 있지만 일부는 "Optional"이거나 조건부입니다. 문서 공통 필드인 `scratchpad_dir`는 10회 모두 관측되지 않았습니다. 어댑터는 이런 필드가 없어도 동작해야 합니다.

## 미실행·후속 범위

- 대화형 TUI 세션, 실제 승인 대기 대화상자, `Notification`(권한·유휴 알림), 사용자 중단(`is_interrupt`)은 미실행
- `PreCompact`/`PostCompact`, resume·fork·clear, `/hooks` 신뢰 흐름은 미실행
- 비정상 종료(SIGKILL)·네트워크 단절 시 `SessionEnd` 누락 여부는 미실행
- Windows·macOS와 다른 Claude Code 버전은 미실행
- 웹 검색·편집 도구, 계획 모드(`ExitPlanMode`)는 미실행
- Hook·JSONL 교차 중복, 로테이션, 재시작은 #20에서 검증

# Collector 입력 어댑터(Go)

관련 이슈: #29 (선행: #17, #18, #19, #20, ADR-0002)

Codex·Claude Hook stdin과 Claude transcript JSONL 한 줄을 제공자 중립 **후보(Candidate)**로 바꾸는 Go 패키지입니다. 파일 tail, Hook 설치, 큐·전송, 이벤트 v1 최종 정규화는 이 범위가 아닙니다.

## 패키지 구조

모두 `collector/core` 모듈(`github.com/apg0001/Agent-Office/collector/core`) 안의 표준 라이브러리 전용 코드입니다.

| 패키지 | 역할 |
|---|---|
| `internal/adapter` | `Candidate`·enum, 정확한 키 일치 JSON 객체 읽기, 식별자 검증, 버전 검증 수준(`Runtime`·`Target`·`Assess`), 중복 제거 키·UUIDv5 `event_id`·`Deduplicator` |
| `internal/adapter/claude` | `ParseHook`(Claude Code Hook), `ParseTranscriptLine`(transcript 한 줄), `VerifiedTargets` |
| `internal/adapter/codex` | `ParseHook`(Codex CLI·Desktop Hook), `VerifiedTargets`. Codex transcript는 읽지 않음(ADR-0002) |

## 화이트리스트

후보에는 아래 값만 들어갑니다.

| 필드 | 출처 | 비고 |
|---|---|---|
| `Provider`, `Source`, `EventType`, `Activity`, `Confidence` | 어댑터가 정한 이벤트 v1 enum | 원문 이벤트 이름은 복사하지 않음 |
| `SessionRef` | Claude `session_id`/`sessionId`, Codex `session_id` | 식별자 형식 검증 |
| `TurnRef` | Claude `prompt_id`/`promptId`, Codex `turn_id` | 식별자 형식 검증 |
| `ToolUseRef` | `tool_use_id`, transcript `tool_use.id`·`tool_result.tool_use_id` | 식별자 형식 검증 |
| `AgentRef` | `agent_id`/`agentId` | 하위 에이전트 소유자. `agent.*`에서는 해당 하위 에이전트 자신 |
| `ToolCategory` | `tool_name`의 분류 | 도구 이름 원문은 버림 |
| `AgentType` | `agent_type`의 분류 | 사용자 정의 이름은 `custom` |
| `Support` | 런타임·이벤트 검증 수준 | `verified`/`partial`/`unverified` |

- 식별자는 `[A-Za-z0-9._:-]{1,128}`만 통과합니다. 공백·경로 구분자·따옴표가 있으면 원문일 수 있으므로 식별자로 쓰지 않습니다.
- 최상위 키는 대소문자까지 정확히 일치할 때만 읽습니다(encoding/json 구조체 디코딩의 대소문자 무시 일치를 쓰지 않음).
- `prompt`, `tool_input`, `tool_response`, `error`, `last_assistant_message`, `cwd`, `transcript_path`, `agent_transcript_path`, `model`, `background_tasks`, `session_crons`, transcript `message.content` 본문은 어떤 후보 필드에도 들어가지 않습니다. 미등록 필드도 무시합니다.
- 오류는 고정 문자열 sentinel(`ErrTooLarge`, `ErrMalformed`, `ErrMissingSession`, `ErrMissingRef`)뿐입니다. encoding/json 오류(입력 문자를 포함할 수 있음)를 감싸지 않습니다.

## 이벤트 매핑

| Hook | Claude | Codex |
|---|---|---|
| `SessionStart` / `SessionEnd` | `session.started` / `session.ended` | 같음 |
| `UserPromptSubmit` | `turn.started`(키 `prompt_id`) | `turn.started`(키 `turn_id`) |
| `Stop` | `turn.completed`(키 없음) | 같음 |
| `PreToolUse` | `tool.started` | 같음 |
| `PostToolUse` | `tool.completed` | Desktop 런타임만 `tool.completed`. CLI·미상 런타임은 `unknown`(CLI exec 미관측, #17) |
| `PostToolUseFailure` | `tool.failed`(미관측이라 `unverified`) | 매핑 없음 → `unknown` |
| `PermissionRequest` | `approval.requested`(키 없음) | 같음 |
| `SubagentStart` / `SubagentStop` | `agent.started` / `agent.completed`(키 `agent_id`) | 같음 |
| 그 밖의 이름 | `unknown`(세션·턴 참조만, `unverified`) | 같음 |

transcript(Claude)는 `assistant`의 `tool_use` → `tool.started`, `user`의 `tool_result` → `tool.completed`/`tool.failed`(`is_error`가 JSON `true`일 때만), 프롬프트 줄(`promptId` 있음, `tool_result`·`isMeta`·`agentId` 없음) → `turn.started`입니다. 그 밖의 줄 유형(`api-request-blob`, `queue-operation`, `system` 등)은 후보 없이 `(nil, nil)`입니다.

## 활동(activity) 분류

보수적으로 이름이 명확한 도구만 분류합니다. Bash는 테스트·빌드·임의 명령을 구분할 수 없어 `unknown`입니다.

| 제공자 | 도구 이름 | 분류 / activity |
|---|---|---|
| Claude | `Read` | `read` / `reading` |
| Claude | `Grep`, `Glob`, `LS` | `search` / `reading` |
| Claude | `Edit`, `MultiEdit`, `Write`, `NotebookEdit` | `edit` / `coding` |
| Claude | `WebSearch`, `WebFetch` | `web` / `browsing` |
| Claude | `Agent`, `Task` | `delegate` / `delegating` |
| Claude | `Bash` | `shell` / `unknown` |
| Claude | `TodoWrite`, `ExitPlanMode`, `EnterPlanMode` | `plan` / `unknown`(계획 모드 미실측) |
| Codex | `Bash` | `shell` / `unknown` |
| Codex | `apply_patch` | `edit` / `coding`(이름 기반, 미관측) |
| Codex | `update_plan` | `plan` / `unknown` |
| 공통 | `mcp__*` | `mcp` / `unknown` |
| 공통 | 그 밖의 이름 | `other` / `unknown` |

Codex 하위 에이전트 관리 도구 이름은 #18에서 확정되지 않아 분류하지 않습니다. 도구 이벤트가 아닌 후보의 activity는 `unknown`이며, 상태 의미 부여는 #32 정규화·상태 엔진이 맡습니다.

## 버전 정책

Hook payload에는 도구 버전이 없으므로 호출자가 로컬에서 감지한 `Runtime{Product, Version, OS, Mode}`를 넘깁니다(감지는 #31). 런타임 값은 후보에 복사되지 않습니다.

| 대상 | 수준 | 관측 이벤트 |
|---|---|---|
| `claude-code` `2.1.308` `linux` `print`(-p) | `verified` | Session·Prompt·Pre/Post·Subagent·Permission·Stop·End, transcript |
| `codex-cli` `0.162.0-alpha.17.2` `windows` `exec` | `verified` | SessionStart, UserPromptSubmit, PreToolUse, Stop, SessionEnd |
| `codex-desktop` `26.1007.2314.0` `windows` `desktop` | `partial`(1회 실측) | UserPromptSubmit, Pre/PostToolUse, PermissionRequest, Subagent*, Stop, SessionEnd |

- 런타임이 목록과 정확히 일치하지 않거나 그 런타임에서 관측되지 않은 이벤트는 `Support=unverified`입니다. 파싱은 같은 규칙으로 안전하게 하지만 지원으로 주장하지 않습니다.
- `Confidence`는 `verified`만 `observed`, `partial`·`unverified`는 `inferred`입니다. 대화형 TUI, macOS·Windows Claude, 다른 버전은 모두 `inferred`가 됩니다.
- 새 버전을 지원하려면 실측 보고서를 남긴 뒤 `VerifiedTargets`에 추가하고 호환성 행렬을 갱신합니다.

## 중복 제거와 참조 구현 일치

[`dedup_reference.py`](../../tests/poc/collector/dedup_reference.py)를 그대로 이식했습니다.

- 키 `device|provider|session|kind|ref`, `tool.completed`/`tool.failed`는 kind `tool.finished`로 합침
- `event_id = UUIDv5(6f9c2b1e-4d0a-5c3b-9e7f-2a1d8c4b6e50, key)`, 키 없는 이벤트는 무작위 UUIDv4(큐 첫 기록 때 1회 생성·영속화, #33)
- `Deduplicator`는 키별 최초 관측만 통과, 영속 키 집합으로 재시작

`claude/parity_test.go`가 `tests/fixtures/collector/claude/dual-source/`를 읽어 `expected.json`의 통과 키 10개, Hook 우선 시 억제 8건, 완료 유형, 키 없는 Hook 이벤트 유형을 재현하고, 키별 `event_id`가 Hook·transcript에서 같으며 Python `uuid.uuid5` 값과 같음을 확인합니다.

참조 구현과 의도적으로 다른 점(통과 키 집합에는 영향 없음):

| 입력 | 참조 구현 | Go |
|---|---|---|
| 알 수 없는 Hook 이벤트 | 후보 없음 | `unknown` 후보(키 없음, `unverified`) |
| `agent_id` 없는 `SubagentStart`/`Stop` | 키 없는 `agent.*` 후보 | `ErrMissingRef` |
| 세션·식별자가 식별자 형식이 아님 | 그대로 사용 | 없는 값으로 취급 |
| `sessionId` 없는 transcript 활동 줄, 객체 아닌 `message` | 후보 없음 | 후보 없음 + `ErrMissingSession`/`ErrMalformed` |
| 제공자 | `claude` 고정 | 후보의 `Provider`(Codex 키는 Hook 단일 소스 멱등용, 교차 소스 미검증) |

## fail-open

- 상한: Hook payload 8MiB, transcript 줄 1MiB. 초과 시 `ErrTooLarge`.
- 깨진 JSON, 객체가 아닌 JSON, 깊은 중첩은 `ErrMalformed`. 내보낸 파서는 `recover`로 panic을 오류로 바꿉니다. 퍼즈 테스트(`FuzzParse`) 시드는 `go test`에서 매번 실행됩니다.
- 호출자는 오류가 나도 Hook을 종료 코드 0으로 끝내야 합니다(#31).

## 후속 범위

- #30: transcript 탐색·tail(체크포인트·로테이션)은 [Collector transcript 탐색·tail](collector-tail.md)의 `internal/tail`이 맡고, 이 패키지는 줄 단위 파서만 제공
- #31: Hook 설치·버전 감지로 `Runtime` 채우기
- #32: 후보 → 이벤트 v1 정규화(`device_id`·`agent_id` UUID 매핑, `parent_agent_id`, `occurred_at`, `sequence`, `unverified` 후보 송신 정책)
- #33: SQLite 큐에 키 집합·무작위 `event_id` 영속화

# Collector 이벤트 정규화(Go)

관련 이슈: #32 (선행: #15 이벤트 계약, #29 어댑터, #30 tail, ADR-0002)

어댑터 후보(`adapter.Candidate`)를 [이벤트 v1](../api/event-ingestion-v1.md) 객체로 바꾸는 Go 패키지입니다. Hook payload와 transcript tail 결과를 어댑터 → 정규화로 흘려 보내는 동기식 조립 단위(`Pipeline`)까지 포함합니다. SQLite 큐·전송(#33), polling 실행 루프·OS 서비스(#34), Hook 설치·버전 감지(#31)는 범위가 아닙니다.

## 패키지

`collector/core/internal/normalize`(표준 라이브러리 전용)

| API | 역할 |
|---|---|
| `Event` | v1 13개 필드만 가진 구조체. JSON 태그가 스키마 `required`·`properties`와 정확히 같음 |
| `New(Options) (*Normalizer, error)` | 장치 UUID, `SequenceStore`, 영속화된 중복 제거 키(`SeenKeys`)로 시작 |
| `Normalizer.Normalize(Input) (Record, Outcome)` | 후보 1건 → `accepted`/`duplicate`/`dropped_unknown`/`dropped_invalid` |
| `Normalizer.Counters()`, `SeenKeys()` | 개수(원문 없음), #33이 영속화할 중복 제거 키 |
| `NewPipeline(n)`, `Pipeline.Hook(provider, runtime, payload, observedAt)`, `Pipeline.Transcript(tail.Result, runtime, observedAt)` | 어댑터 + 정규화. 고루틴 없음 |
| `SequenceStore`, `MemorySequenceStore` | (장치, 세션)별 단조 증가 sequence 발급·영속화 인터페이스와 메모리 구현 |
| `SessionID`, `LeadAgentID`, `SubagentID`, `IdentityNamespace` | 원본 참조 → UUIDv5 매핑 |

`Record`는 `Event`와 `DeterministicID`(false면 무작위 `event_id`라 큐 첫 기록 값을 영속화해야 함)만 담습니다. 서버로는 `Event`만 보냅니다.

## 식별자 매핑

원본 제공자 세션·에이전트 ID는 PC 밖으로 나가지 않습니다. 고정 namespace `2d6a1f84-935e-5b27-a40c-7e31d59846bf`(`IdentityNamespace`, `event_id`용 `6f9c2b1e-…`와 다름)로 UUIDv5를 만듭니다.

| v1 필드 | 이름 | 비고 |
|---|---|---|
| `device_id` | 등록된 장치 UUID(입력) | UUID가 아니면 `ErrInvalidDeviceID`, 소문자로 정규화 |
| `session_id` | `session|<device>|<provider>|<session_ref>` | 재시작해도 같은 값 |
| `agent_id`(대장) | `lead|<device>|<provider>|<session_ref>` | `AgentRef`가 없는 후보 |
| `agent_id`(하위) | `subagent|<device>|<provider>|<session_ref>|<agent_ref>` | `AgentRef`가 있는 후보. `agent.*`는 해당 하위 에이전트 자신 |
| `parent_agent_id` | 하위 에이전트면 대장 `agent_id`, 대장이면 `null` | 같은 세션 안에서만 참조 |
| `event_id` | 키 있음: `UUIDv5(event-id namespace, device|provider|session|kind|ref)`, 키 없음: UUIDv4 | #29 `adapter.EventID`. 장치 범위에 장치 UUID 사용 |

UUIDv5는 추측 가능한 원본 ID에 대한 익명화를 보장하지 않습니다(같은 장치 UUID와 원본 ID를 아는 사람은 다시 계산 가능). 서버로 원문이 가지 않게 하는 것이 목적입니다.

## 필드 정책

| 필드 | 규칙 |
|---|---|
| `provider`, `event_type`, `activity` | v1 enum에 없으면 `unknown`으로 바꾸고 `confidence`를 `inferred`로 낮춤. 원문 값을 통과시키지 않음 |
| `source` | v1 enum에 없으면 버림(`dropped_invalid`). 대체할 안전한 값이 없음 |
| `confidence` | `observed`는 어댑터가 `observed`이고 검증 수준이 `verified`이고 `source`가 `inferred`가 아니고 enum 변환이 없을 때만. 그 밖은 모두 `inferred`(#29 `partial`·`unverified` 그대로 통과) |
| 세션 참조 | 식별자 형식이 아니면 `unknown` 이벤트는 `dropped_unknown`, 그 밖은 `dropped_invalid` |
| `occurred_at` | 원본 시각(transcript 줄 최상위 `timestamp`)이 시간대가 있는 RFC 3339이고 2000~9999년이며 관측 시각 + 5분 이내이면 그 값, 아니면 Collector 관측 시각. 항상 UTC `Z` 표기 |
| `sequence` | 중복 억제 뒤 `SequenceStore.Next(device, session_id)`. 세션마다 0부터, 중복은 소비하지 않음. 2^53-1을 넘으면 `dropped_invalid` |

- 이벤트를 새로 만들지 않습니다. Codex CLI·미상 런타임의 도구 완료는 어댑터가 이미 `unknown`으로 낮추고, `Pipeline.Hook`이 한 번 더 막습니다(`tool.completed`/`tool.failed` → `unknown`). `Stop` 누락(#20)도 보충하지 않으며 다음 `turn.started`·`session.ended`로 상태 엔진이 바로잡습니다.
- `TurnRef`, `ToolUseRef`, `ToolCategory`, `AgentType`, 런타임 값은 v1에 대응 필드가 없어 출력에 들어가지 않습니다(중복 제거 키 계산에만 사용).

## 중복 제거

- `Normalizer`는 장치 UUID로 `adapter.Deduplicator`를 만들고, enum 정리를 마친 후보로 키를 계산합니다. `unknown`으로 바뀐 후보는 키가 없습니다.
- Hook 우선이면 transcript 키 이벤트가 억제되고(`duplicate`), 순서가 바뀌어도 통과 키 집합과 `event_id`는 같습니다.
- `SeenKeys()`에는 원본 참조가 들어 있으므로 로컬 큐(#33)에만 저장하고 서버·로그로 보내지 않습니다.
- 처리 순서는 검증 → 중복 판정 → sequence 발급 → `event_id`입니다. #33은 중복 제거 키, sequence, 큐 기록을 한 트랜잭션으로 커밋해야 중간 실패로 이벤트를 잃지 않습니다.

## fail-open과 개수

`Pipeline`은 오류를 반환하지 않습니다. 어댑터 오류(`ErrTooLarge`, `ErrMalformed`, `ErrMissingSession`, `ErrMissingRef`)와 지원하지 않는 provider는 `DroppedInvalid`, 활동이 아닌 transcript 줄은 `IgnoredLines`로만 셉니다. `Counters`에는 숫자만 있습니다.

## 검증

| 테스트 | 내용 |
|---|---|
| `schema_test.go` | `Event` 직렬화 키 집합 = 스키마 `required` = `properties`, 패키지 enum = 스키마 enum, 외부 라이브러리 없는 검사기(필수·추가 필드·enum·const·uuid·date-time·pattern·길이·정수 범위·`source=inferred` 규칙)와 위반 사례 거부 |
| `normalize_test.go` | 장치 ID 검증, 재시작 결정성, 장치·제공자·세션 범위, namespace 분리, 하위 에이전트 부모, enum 변환·drop, confidence 정책, unknown 정책, `occurred_at` 선택, sequence 단조·영속·중복 미소비·상한, 키 유무별 `event_id` |
| `pipeline_test.go` | #20 dual-source fixture를 Hook + #30 `tail.PollOnce` → 어댑터 → 정규화로 양쪽 순서 처리: 모든 출력 스키마 통과, 키 이벤트 10개가 참조 구현 키와 같은 `event_id`, Hook 우선 시 13건·억제 8건, 세션 sequence 0..n-1, 영속 키로 재시작 시 재방출 0. transcript `timestamp` 검증, Codex CLI `tool.completed` 미생성, fail-open 개수 |
| `privacy_test.go` | Hook의 모든 ID·원문 필드, transcript의 ID·`timestamp`·본문·도구 이름·입력, 런타임 값, provider, 후보의 모든 문자열 필드에 sentinel을 넣어 출력 JSON·`Record`·`Counters`·오류 문자열에 sentinel과 `/home/`이 없음을 확인 |

Linux arm64(Docker `golang:1.24-bookworm`)에서 `gofmt`, `go vet`, `go test`, `go test -race`를 실행했습니다. Windows·macOS는 OS 의존 코드가 없지만 실행하지 않았습니다.

## 후속 범위

- #33: `SequenceStore`·`SeenKeys`·무작위 `event_id`·tail 체크포인트를 SQLite 한 트랜잭션으로 영속화, 배치 전송과 ACK
- #34: polling 루프(탐색 주기, 유휴 Tailer 닫기, `Pipeline` 호출), 첫 설치 시 기존 transcript를 처음부터 읽을지(타임스탬프 없는 줄은 관측 시각이 되어 과거 활동이 현재처럼 보일 수 있음) 끝에서 시작할지 정책
- #31: Hook에서 `Pipeline.Hook`으로 넘길 `Runtime` 감지
- Claude transcript 줄의 `timestamp` 값 형식은 #19·#20 PoC 보고서에 기록되지 않아 **미검증**입니다. 시간대가 있는 RFC 3339가 아니면 관측 시각으로 대체되므로 안전하지만, 실제 형식 확인은 다음 실측(#34 또는 호환성 갱신)에서 해야 합니다.

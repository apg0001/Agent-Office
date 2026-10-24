# Collector 이벤트 수집 계약 v1

관련 이슈: #15

계약 버전: `1.0`

이 문서는 `POST /api/v1/events/batch`의 전송 계약을 정의합니다. #15에서 스키마와 타입을 확정했고, #35에서 장치 인증·저장·응답을 구현했습니다. 장치 토큰은 [장치 등록·인증 API](devices-v1.md)로 받습니다.

## 단일 이벤트

정본은 [`packages/event-schema/schema/event-v1.schema.json`](../../packages/event-schema/schema/event-v1.schema.json)입니다.

| 필드 | 의미와 제약 |
|---|---|
| `schema_version` | 정확히 `1.0` |
| `event_id` | Collector가 최초 큐 기록 시 생성하고 재전송에서도 유지하는 UUID 멱등 키 |
| `device_id` | UUID. 서버가 장치 자격증명에서 판정한 값과 일치해야 함 |
| `session_id` | 제공자 세션을 정규화한 UUID |
| `agent_id` | 세션 안 에이전트 UUID |
| `parent_agent_id` | 같은 세션의 부모 에이전트 UUID, 루트이면 `null` |
| `provider` | `codex`, `claude`, `unknown` |
| `source` | `hook`, `jsonl`, `app_server`, `inferred` |
| `event_type` | v1 화이트리스트 enum. 미지원 관측은 `unknown` |
| `activity` | UI 사실 상태로 사용할 수 있는 v1 활동 enum |
| `confidence` | `observed` 또는 `inferred` |
| `occurred_at` | 시간대가 포함된 RFC 3339 `date-time` |
| `sequence` | 장치·세션 내 정렬 보조용 0 이상 JavaScript 안전 정수 |

`source`가 `inferred`이면 `confidence`도 반드시 `inferred`입니다. 반대로 JSONL 등 관측 소스에서 불완전한 증거로 추정한 경우에는 해당 소스를 유지하면서 `confidence: inferred`를 사용할 수 있습니다.

모든 필드는 필수이고 추가 필드는 거부됩니다. 따라서 프롬프트, 코드, 파일 본문·경로, 도구 입출력, 전체 명령, 사용자/소유자 식별자를 이 계약으로 전송할 수 없습니다. 이벤트에 메타데이터를 추가하려면 별도 개인정보·호환성 심사와 새 스키마 버전이 필요합니다.

### 계획 단계(`plan_steps`, #38)

선택 필드입니다. `event_type`이 `plan.updated`일 때만 허용하며, 계획 단계의 **상태만** 순서대로 담습니다(`pending`·`in_progress`·`completed`·`unknown`, 1~50개). 단계 문장·할 일 내용은 스키마상 넣을 수 없습니다.

```json
{"event_type": "plan.updated", "activity": "planning", "plan_steps": ["completed", "in_progress", "pending"], "...": "..."}
```

- 서버는 세션의 계획을 이 목록으로 통째로 바꿉니다. 이미 더 늦은 시각의 계획이 있으면(오프라인 큐에서 늦게 온 과거 이벤트) 무시합니다.
- 필드가 없는 이벤트의 `payload_digest`는 필드 추가 전과 같습니다(기존 이벤트 재전송이 계속 중복으로 처리됨).
- 이 필드를 모르는 이전 서버는 `additionalProperties: false` 때문에 이 이벤트를 거부합니다. 서버를 먼저 올린 뒤 Collector를 올립니다.

## 부모-자식 관계

- 루트 에이전트의 `parent_agent_id`는 `null`입니다.
- 하위 에이전트는 같은 `session_id`에 속한 부모 `agent_id`를 참조해야 합니다.
- JSON Schema는 참조 대상 존재 여부를 확인할 수 없으므로 서버 저장 계층이 동일 세션과 순환 참조를 검증합니다.
- 부모 이벤트가 늦게 도착할 수 있으므로 Collector 전송 순서만으로 참조 무결성을 단정하지 않습니다.

## 배치 요청과 ACK

요청 정본은 `collector-event-batch-request-v1.schema.json`이며 한 번에 1~500개 이벤트를 받습니다. 요청 본문 자체가 스키마에 맞지 않으면 전체 요청을 `422`로 거부하고 어떤 이벤트도 ACK하지 않습니다.

성공적으로 해석한 배치는 `collector-event-batch-response-v1.schema.json` 형식으로 결과를 나눕니다.

- `accepted_event_ids`: 영속화가 끝난 신규 이벤트
- `duplicate_event_ids`: 같은 `event_id`로 이미 영속화된 이벤트
- `rejected_events`: 코드와 재시도 가능 여부가 명시된 이벤트

Collector는 `accepted_event_ids`와 `duplicate_event_ids`만 로컬 큐에서 ACK 처리합니다(구현: [Collector 로컬 큐·배치 전송](../architecture/collector-queue.md), #33). HTTP 오류, 연결 끊김, 형식이 맞지 않는 응답에서는 ACK하지 않습니다. 서버는 DB 커밋이 끝난 뒤에만 응답하므로 두 목록에 있는 이벤트는 영속화가 끝난 것입니다.

### 거부 코드

| 코드 | 재시도 | 의미 |
|---|---|---|
| `unauthorized_device` | 아니오 | 이벤트의 `device_id`가 토큰으로 판정한 장치와 다름 |
| `unauthorized_scope` | 아니오 | 예약(현재 미사용) |
| `unsupported_schema_version` | 아니오 | 예약(현재는 요청 검증에서 `422`) |
| `retention_window_exceeded` | 아니오 | `occurred_at`이 서버 시각 기준 30일보다 오래됨(보존 기간 밖) |
| `event_id_conflict` | 아니오 | 같은 `event_id`가 다른 내용으로 이미 저장됨 |
| `parent_agent_unknown` | 예 | 부모 에이전트가 아직 없음. 부모 이벤트를 보낸 뒤 다시 보냄 |
| `parent_agent_conflict` | 아니오 | 이미 기록된 에이전트의 부모와 다름(부모는 처음 관측값으로 고정) |
| `occurred_at_out_of_range` | 아니오 | `occurred_at`이 서버 시각보다 1일 넘게 미래 |
| `internal_error` | 예 | 서버 저장 오류 |

`event_id_conflict`·`parent_agent_unknown`·`parent_agent_conflict`·`occurred_at_out_of_range`는 #35에서 v1 응답 enum에 추가했습니다. 당시 이 응답을 소비하는 Collector가 없었으므로 기존 소비자 호환성 영향은 없습니다. Collector(#33)는 알 수 없는 코드를 받으면 `retryable` 값만 따릅니다.

### 서버 처리 규칙(#35)

- 중복 판정: 허용 필드의 정규 직렬화 SHA-256(`payload_digest`)을 비교합니다. 같은 시각의 다른 표기(`Z`와 `+00:00`)는 같은 내용입니다.
- 세션: `(장치, provider, session_id)`마다 서버 세션 하나를 만듭니다. 다른 장치의 같은 `session_id`는 별개 세션입니다.
- 에이전트: 부모가 이미 있어야 하위 에이전트를 만듭니다. 같은 배치 안에서는 순서와 관계없이 부모를 먼저 처리합니다. 부모를 나중에 바꿀 수 없으므로 순환 계층이 생기지 않습니다.
- 이벤트마다 savepoint로 처리해 한 이벤트의 실패가 같은 배치의 다른 이벤트 저장을 막지 않습니다.
- 세션의 `last_event_at`과 장치의 `last_seen_at`을 갱신합니다. 세션 생명주기·현재 상태 계산은 #36 범위입니다.

### 제한

| 항목 | 값 | 초과 시 |
|---|---|---|
| 본문 크기 | 1MiB(그 밖의 API는 64KiB). `Content-Length`로 본문을 읽기 전에 검사 | `413 payload_too_large`. 길이 없는 chunked 요청은 `411 length_required` |
| 배치 크기 | 1~500 | `422` |
| 요청 빈도 | 장치당 60초에 120회(서버 프로세스 메모리 기준) | `429 rate_limited` + `Retry-After` |

사용자별 이벤트 총량 한도와 다중 인스턴스 공유 제한은 #28·#63에서 다룹니다.

### 검증 오류 응답

`422` 응답은 `{"detail": [{"loc": [...], "type": "..."}]}` 형식이며 입력 값과 메시지를 담지 않습니다. 위치(`loc`)에는 API가 정의한 필드 이름과 배열 인덱스만 남고, 정의되지 않은 키 이름은 `<field>`로 가립니다. FastAPI 기본 응답은 실패한 입력 값을 그대로 되돌려 보내므로, 스키마 밖 필드에 실린 원문이 응답·로그에 노출되지 않게 바꿨습니다. 이 정책은 모든 API에 적용됩니다.

자유 형식 오류 메시지는 이벤트 응답에 넣지 않습니다. 진단 정보가 원본 데이터나 내부 경로를 노출하지 않도록 서버 로그도 별도 검열합니다.

## 호환성 규칙

- v1에서 enum 값 추가나 필수 필드 변경은 기존 소비자를 깨뜨릴 수 있으므로 사전 호환성 테스트 없이 수행하지 않습니다.
- 알 수 없는 제공자·이벤트·활동은 원문 값을 통과시키지 않고 `unknown`으로 정규화합니다.
- 순수 UI 애니메이션 상태는 이벤트 계약에 기록하지 않습니다.
- `sequence`는 보조 정렬 값이며 전역 순서나 전달 1회만을 보장하지 않습니다. 최종 멱등 기준은 `event_id`입니다.

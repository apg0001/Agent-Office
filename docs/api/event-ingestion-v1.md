# Collector 이벤트 수집 계약 v1

관련 이슈: #15

계약 버전: `1.0`

이 문서는 구현 예정인 `POST /api/v1/collector/events`의 전송 계약을 정의합니다. #15에서는 스키마와 타입만 확정하며 인증·저장·응답 엔드포인트는 후속 이슈에서 구현합니다.

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

Collector는 `accepted_event_ids`와 `duplicate_event_ids`만 로컬 큐에서 ACK 처리합니다. HTTP 오류, 연결 끊김, 형식이 맞지 않는 응답에서는 ACK하지 않습니다. 같은 `event_id`에 다른 내용이 오면 정상 중복으로 취급하지 않고 서버가 충돌로 기록·거부해야 합니다. 이 저장·멱등성 동작은 #29 이후 구현에서 검증합니다.

자유 형식 오류 메시지는 이벤트 응답에 넣지 않습니다. 진단 정보가 원본 데이터나 내부 경로를 노출하지 않도록 서버 로그도 별도 검열합니다.

## 호환성 규칙

- v1에서 enum 값 추가나 필수 필드 변경은 기존 소비자를 깨뜨릴 수 있으므로 사전 호환성 테스트 없이 수행하지 않습니다.
- 알 수 없는 제공자·이벤트·활동은 원문 값을 통과시키지 않고 `unknown`으로 정규화합니다.
- 순수 UI 애니메이션 상태는 이벤트 계약에 기록하지 않습니다.
- `sequence`는 보조 정렬 값이며 전역 순서나 전달 1회만을 보장하지 않습니다. 최종 멱등 기준은 `event_id`입니다.

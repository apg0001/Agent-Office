# Event schema

Collector, 서버, 웹이 공유하는 이벤트 계약의 단일 소스입니다. JSON Schema Draft 2020-12를 기준으로 하며 TypeScript 타입은 같은 enum과 필드 구조를 제공합니다.

## v1 계약

- `schema/event-v1.schema.json`: 단일 정규화 이벤트
- `schema/collector-event-batch-request-v1.schema.json`: 최대 500개 이벤트의 업로드 요청
- `schema/collector-event-batch-response-v1.schema.json`: 수락·중복·거부 결과
- `src/index.ts`: 공통 TypeScript 타입과 enum 상수
- `fixtures`: 원문이 없는 익명 유효·무효 예시

이벤트 객체는 `additionalProperties: false`입니다. 원본 프롬프트, 코드, 파일 본문·경로, 도구 입출력, 전체 명령이나 사용자 식별 필드를 추가할 수 없습니다. 새로운 필드나 enum 값은 스키마 버전과 호환성 검토를 거쳐야 합니다.

`event_id`는 Collector가 로컬 큐에 처음 기록할 때 한 번 생성하는 UUID이며 재전송할 때 바꾸지 않습니다. `parent_agent_id`는 같은 세션의 부모 에이전트를 가리키고 루트 에이전트는 `null`입니다. 참조 존재 여부와 자격증명의 장치 ID 일치는 서버가 검증합니다.

```powershell
npm --workspace @agent-office/event-schema run typecheck
npm --workspace @agent-office/event-schema run test
npm --workspace @agent-office/event-schema run build
```

Python JSON Schema 계약 테스트는 저장소 루트의 `tests/test_event_contract.py`에 있습니다. CI 연결은 #16에서 진행합니다.

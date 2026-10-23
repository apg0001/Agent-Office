# 아키텍처 개요

## 데이터 흐름

1. 사용자 승인 후 로컬 Collector가 검증된 Hook 또는 로그 입력을 읽기 전용으로 관측한다.
2. 제공자 어댑터가 이벤트를 버전형 공통 계약으로 정규화하고 화이트리스트 밖의 필드를 제거한다.
3. 로컬 SQLite 큐가 이벤트 UUID와 전송 상태를 영속화한다.
4. Collector가 TLS와 장치 자격증명으로 서버에 배치 전송한다.
5. 서버가 두 번째 스키마·권한·멱등성 검사를 수행하고 PostgreSQL에 메타데이터만 저장한다.
6. 웹 클라이언트가 권한 범위의 스냅샷과 WebSocket 증분을 받아 사무실 상태를 복원한다.

현재는 버전형 이벤트 계약과 기반만 존재하며 수집·저장·실시간 경로는 구현 전입니다. 실제 지원 여부는 [호환성 행렬](../compatibility/matrix.md)에 기록합니다.

PostgreSQL 스키마·무결성 규칙·보존 인덱스·마이그레이션 전략은 [데이터 모델](data-model.md)에 있습니다(#22). 수집 API·상태 엔진은 아직 이 스키마를 사용하지 않습니다.

공통 계약 v1은 [`packages/event-schema`](../../packages/event-schema/README.md)에 있으며 필드가 닫힌 JSON Schema를 정본으로 사용합니다. 배치 수집 의미는 [이벤트 수집 API 계약](../api/event-ingestion-v1.md)에 정의합니다.

## 독립 상태축

- Collector: `ONLINE` / `DISCONNECTED`
- 세션 생명주기: `DISCOVERED` / `OPEN` / `ENDED` / `UNKNOWN`
- 활동: `WORKING` / `THINKING` / `WAITING_USER` / `IDLE` / `UNKNOWN`

세션이 조용하다는 이유만으로 종료시키지 않으며, `turn/completed`는 유휴 전환의 근거일 뿐 세션 종료가 아닙니다.

## 신뢰 경계

- 로컬 원본 로그와 프롬프트는 Collector 프로세스 밖으로 보내지 않습니다.
- 클라이언트가 보낸 owner/user 식별자를 신뢰하지 않습니다.
- 인스턴스 역할과 팀 역할은 독립 저장·검사합니다.
- UI 애니메이션은 사실 이벤트 저장과 분리합니다.

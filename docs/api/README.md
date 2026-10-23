# API 문서 상태

공통 이벤트 JSON Schema와 수집 payload 계약은 #15에서 확정했습니다. 상세 내용은 [Collector 이벤트 수집 계약 v1](event-ingestion-v1.md)을 참고하세요. 현재 실제 구현된 엔드포인트는 민감정보를 포함하지 않는 `GET /api/v1/system/health`뿐이며, 이벤트 수집 엔드포인트는 아직 구현 전입니다.

예정 범위:

- 장치 등록·heartbeat·폐기
- 이벤트 batch와 Collector 설정
- 사용자·장치·프로젝트·세션 조회
- 개인/팀 사무실 스냅샷
- 인증·스코프 기반 `WS /api/v1/live`

계약이 확정되기 전 필드나 상태를 지원된 것으로 간주하지 않습니다.

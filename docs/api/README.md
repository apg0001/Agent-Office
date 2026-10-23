# API 문서 상태

공통 이벤트 JSON Schema와 수집 payload 계약은 #15에서 확정했습니다. 상세 내용은 [Collector 이벤트 수집 계약 v1](event-ingestion-v1.md)을 참고하세요. 구현된 엔드포인트는 민감정보를 포함하지 않는 `GET /api/v1/system/health`와 [장치 등록·인증 API](devices-v1.md)(#26), [브라우저 세션·CSRF·로그아웃 API](auth-sessions-v1.md)(#25)입니다. 이벤트 배치 수집 `POST /api/v1/events/batch`(#35)도 구현됐습니다.

예정 범위:

- Collector 설정
- 사용자·장치·프로젝트·세션 조회
- 개인/팀 사무실 스냅샷
- 인증·스코프 기반 `WS /api/v1/live`

계약이 확정되기 전 필드나 상태를 지원된 것으로 간주하지 않습니다.

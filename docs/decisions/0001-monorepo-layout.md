# ADR-0001: 언어 경계를 보존하는 모노레포

- 상태: 승인됨(기준선)
- 날짜: 2026-10-22
- 관련 이슈: #13

## 맥락

FastAPI 서버, React/Phaser 웹, Go Collector, 공통 계약과 배포 파일을 함께 변경하면서도 런타임 수명주기와 개인정보 경계를 분리해야 합니다.

## 결정

하나의 저장소에서 `apps`, `collector`, `packages`, `deploy`, `tests`, `docs`를 최상위 경계로 사용합니다. 각 언어는 자체 패키지 설정을 가지며, 공통 이벤트 계약만 `packages/event-schema`에서 공유합니다.

## 결과

- 계약 변경을 한 PR에서 교차 검증할 수 있습니다.
- Go/Node/Python 도구가 모두 필요해 로컬 설정 비용이 증가합니다.
- 언어별 잠금 파일과 DB migration은 한 이슈의 명시적 소유자가 수정합니다.

# Agent Office 작업 지침

이 파일은 저장소에서 작업하는 모든 에이전트와 기여자가 따라야 하는 기준선이다. 상위 사용자 지시와 시스템 정책이 항상 우선한다.

## 시작 절차

1. `docs/project-status.md`를 먼저 읽어 현재 브랜치, 완료·미완료, 검증 결과와 다음 작업을 확인한다.
2. `git status --short --branch`, 원격, 현재 Issue/PR을 읽기 전용으로 확인한다.
3. 작업할 GitHub Issue가 없으면 먼저 한국어 Issue를 만들고 실제 번호를 얻는다.
4. `develop` 최신 상태에서 `feat/#번호-영문-설명`, `fix/#번호-영문-설명`, `docs/#번호-영문-설명` 형식의 브랜치를 만든다.
5. 한 브랜치는 원칙적으로 한 세부 Issue만 해결한다. 공통 스키마·마이그레이션·잠금 파일 충돌을 피한다.

## 아키텍처 경계

- `apps/server`: FastAPI REST/WebSocket, 인증·권한·상태 엔진. 원본 로그를 받지 않는다.
- `apps/web`: React 설정 UI와 Phaser 사무실 렌더러. 사실 상태와 연출 상태를 분리한다.
- `collector/core`: Go 백그라운드 수집·정규화·큐·전송. GUI 수명주기와 분리한다.
- `collector/adapters`: Codex/Claude 버전별 검증된 입력 어댑터. 미검증 입력은 `unknown`/`unsupported`다.
- `packages/event-schema`: 서버와 Collector가 공유하는 버전형 계약의 단일 소스다.
- `deploy`: 로컬·운영 Compose. 비밀 값을 커밋하지 않는다.
- `docs/project-status.md`: 세션 간 인수인계의 단일 기준 문서다.

## 안전·프라이버시

- 원본 프롬프트, 코드, 파일 본문, 도구 입출력, 전체 명령, 민감 로컬 경로를 서버·fixture·CI 로그·알림에 넣지 않는다.
- 이벤트는 명시적 화이트리스트로만 정규화한다. 장치와 사용자는 서버가 자격증명으로 판정한다.
- Hook은 fail-open, 짧은 타임아웃, 비차단을 기본으로 하며 기존 설정을 덮어쓰지 않는다.
- 원본 AI 도구 로그는 읽기 전용이다. 삭제·수정·권한 변경을 하지 않는다.
- 관측(`observed`), 추정(`inferred`), 순수 연출을 혼동하지 않는다.
- 프로덕션 배포, 외부 공개, 비가역 DB 변경, `main` 병합, 태그와 Stable 릴리스는 관리자 명시적 승인 전 실행하지 않는다.
- 라이선스는 소유자가 결정하기 전까지 만들거나 추정하지 않는다.

## 구현과 검증

- Python: Ruff, mypy strict, pytest.
- TypeScript: strict 타입 검사, Vitest, 프로덕션 빌드.
- Go: `gofmt`, `go vet`, `go test`와 race 테스트(지원 환경).
- 계약: JSON Schema/OpenAPI 유효성, 유효·무효 익명 fixture.
- 지원할 수 없는 OS·장비·외부 서비스 테스트는 조용히 건너뛰지 말고 `docs/project-status.md`에 미실행 사유를 남긴다.
- 테스트를 실행하지 않았으면 성공했다고 기록하지 않는다.

## 기록과 완료 조건

- Issue, PR, 커밋, 문서와 진행 보고는 한국어로 작성한다. 코드 식별자와 표준 프로토콜 필드는 영어를 사용할 수 있다.
- 중요한 판단은 ADR로 기록한다.
- 작업 중과 종료 직전에 `docs/project-status.md`에 변경 파일, 실제 실행한 검증, 실패·미실행, 남은 위험과 다음 명령을 갱신한다.
- 기능·예외 처리·테스트·프라이버시/권한 검토·문서·CI·리뷰가 완료되기 전 Issue를 닫지 않는다.
- `develop` 대상 PR을 만들고 리뷰/승인 후 병합한다. `main`은 릴리스 승인 전 건드리지 않는다.

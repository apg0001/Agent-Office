# 요구사항 추적 기준선

[마스터 개발 프롬프트 v0.8 원문](../specifications/agent-office-master-development-prompt-v0.8.md)의 요구사항 대분류와 실제 GitHub 이슈를 연결합니다. 원문은 세부 문맥과 완료 조건의 권위 있는 기준선이며, 이 표는 원문을 대체하지 않습니다. 세부 테스트 ID는 각 구현 PR에서 추가합니다.

원문 보존 정책과 SHA-256 무결성 값은 [기준 사양 보존 정책](../specifications/README.md)에 기록합니다. 기준선 보존 작업은 #80에서 추적합니다.

| 요구사항 | 주요 이슈 |
|---|---|
| AUTH-001~006 | #23~#25, #28, #64, #67, #74 |
| USER-001~003 | #26, #28, #39 |
| TEAM-001~006 | #27, #52~#55, #64 |
| DEV-001~006 | #26, #35, #41 |
| COL-001~010 | #17~#20, #29~#34, #40~#44, #62 |
| EVT-001~008 | #15~#21, #29~#39, #47, #50 |
| OFF-001~010 | #45~#51, #53~#54, #62~#63 |
| VIEW-001~004 | #38, #51 |
| SEC-001~006 | #15~#16, #23~#28, #31~#32, #35, #37~#39, #51~#55, #57~#61, #64, #66~#74 |
| SETUP-001~007 | #66~#69 |
| NOTIFY-001~005 | #70~#74 |
| DIST-001~012 | #13, #42~#44, #56, #65, #69 |
| OPS-001~018 | #28, #39, #56~#65, #68~#70 |
| DOC-001~004 | #13~#16, #57, #65, #69 |

전체 백로그는 #1~#12 Epic과 #13~#74 세부 이슈로 등록되어 있습니다.

## 이벤트 계약 v1 검증 매핑

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| CT-EVT-001 | EVT-001 스키마 버전·enum·길이/형식 제한 | Draft 2020-12 자체 검사, 유효·무효 fixture, TypeScript 상수 일치 테스트 |
| CT-EVT-002 | EVT-002 이벤트 멱등 키 | 필수 UUID `event_id`, 배치 중복 fixture, ACK 계약 문서 |
| CT-EVT-004 | EVT-004 부모-자식 관계 | nullable UUID `parent_agent_id`, 루트·하위 에이전트 fixture, 서버 참조 검증 책임 명시 |
| CT-EVT-007 | EVT-007 프라이버시 최소화 | `additionalProperties: false`, 정확한 허용 필드 집합 테스트, 자유 형식 오류 필드 거부 fixture |

## CI 기준선 검증 매핑

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| CI-EVT-001 | EVT-001 계약 자동 검증 | Python JSON Schema·OpenAPI 검사와 TypeScript 계약 테스트를 `CI Gate`에 연결 |
| CI-SEC-006 | SEC-006 서버 최소 데이터·비밀 보호 | 익명 fixture 화이트리스트 검사, 전체 Git 이력 Gitleaks, 최소 Actions 권한 |
| CI-DOC-004 | DOC-004 재현 가능한 기록 | 고정된 액션 SHA, 로컬 재현 명령, 단일 필수 상태 검사와 인수인계 문서 |

## Codex CLI PoC 검증 매핑

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| POC-CDX-001 | COL-003~006 Hook·로컬 기록 관측 | Codex CLI 0.162.0-alpha.17.2 합성 실행 4회와 구조 축약 보고서 |
| POC-CDX-002 | EVT-003 세션·턴 경계 | `SessionStart`/`UserPromptSubmit`/`Stop`/`SessionEnd` 상관관계와 종료 의미 기록 |
| POC-CDX-003 | EVT-004~005 도구·상태 진실성 | `PreToolUse` 관측, `PostToolUse` 반복 미관측을 지원 제한으로 명시 |
| POC-CDX-004 | EVT-007 프라이버시 | 원문 미저장, 허용 필드 타입·HMAC만 보존하는 프로브 회귀 테스트 |
| POC-CDX-005 | EVT-002·006·008 중복·시간·유휴 | 단일 실행 중복 0 확인; 교차 소스 중복·유휴는 #20 후속 범위로 명시 |

## Claude Code PoC 검증 매핑

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| POC-CLD-001 | COL-003~006 Hook·로컬 기록 관측 | Raspberry Pi 5 Linux arm64, Claude Code 2.1.308 합성 `-p` 실행 10회와 구조 축약 보고서 |
| POC-CLD-002 | EVT-003 세션·턴 경계 | `SessionStart`/`UserPromptSubmit`/`Stop`/`SessionEnd`, `prompt_id` 상관관계, 하위 에이전트 알림 턴 기록 |
| POC-CLD-003 | EVT-004 부모-자식 관계 | 병렬 하위 에이전트 2개 × 3회, Hook `agent_id`와 하위 transcript `agentId` 일치 |
| POC-CLD-004 | EVT-005 도구·상태 진실성 | `PreToolUse`·`PostToolUse` `tool_use_id` 일치, `PermissionRequest` 자동 거부, 대화형 승인 대기 미검증 명시 |
| POC-CLD-005 | EVT-007 프라이버시 | 원문 미저장·stdout 없음·`~/.claude` 밖 transcript 미열람 프로브 회귀 테스트 7개, 결과 원문 패턴 0건 |
| POC-CLD-006 | COL-004 비침습 Hook | 비정상 종료·타임아웃 장애 주입에서 Claude 정상 완료, 사용자 설정 SHA-256 무변경 |
| POC-CLD-007 | EVT-002·008 중복·유휴 | Hook·stream·transcript `tool_use_id` 일치, transcript 블록 중복 관측; `Stop` 단독 유휴 판정 금지. 교차 중복은 #20 |

## Docker 이미지 검증 매핑(#56)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| IMG-001 | DIST-004 멀티아키텍처 이미지 | CI `Images (amd64)`·`Images (arm64)` 네이티브 빌드·아키텍처 확인, Pi arm64 실측 |
| IMG-002 | DIST-005 digest 고정 | Dockerfile 베이스와 PostgreSQL 이미지 `태그@sha256` 고정 검사 테스트 |
| IMG-003 | DIST-006 이미지 설치·소스 빌드 경로 | 개발 Compose(소스 빌드)와 독립 운영 Compose(`태그@digest`) 렌더링·기동 |
| IMG-004 | OPS-001 health·재시작 | 이미지 `HEALTHCHECK`, `up --wait`, 프로세스 종료 후 자동 재시작 실측 |
| IMG-005 | SEC 최소 권한 | 비루트 사용자, 읽기 전용 루트, `cap_drop: ALL`, 루프백 바인딩 테스트·실측 |
| IMG-006 | OPS 배포 승인 | GHCR 게시 workflow가 `workflow_dispatch`·`image-publish` Environment로만 실행되는지 테스트 |

## Hook·JSONL 중복 제거와 tail 검증 매핑(#20)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DUP-001 | EVT-002 이중 수집 멱등 | 합성 fixture 처리 순서 무관 통과 키 집합, 키별 단일 UUIDv5 `event_id` |
| DUP-002 | EVT-002 재시작 중복 0 | 영속 키 집합으로 transcript 전체 재처리 시 통과 0 |
| DUP-003 | EVT-006 시간·순서 | 실측: Hook이 transcript보다 먼저 도착, resume append-only, `Stop` 누락 가능 |
| TAIL-001 | COL-007 partial write·인코딩 | partial line 보류, BOM·CRLF·빈 줄, 깨진 줄·과대 줄 건너뛰기 테스트 |
| TAIL-002 | COL-008 로테이션·truncate·재작성 | inode 변경, 크기 감소, 앞부분 지문 변경 시 처음부터 읽기 테스트 |
| TAIL-003 | COL-009 재시작·권한 | 체크포인트 직렬화 재개, 파일 없음·권한 거부 fail-open 테스트 |

## Codex app-server 검증 매핑(#21)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| APP-001 | COL-003 수집 경로 범위 | 같은 서버·다른 서버 2클라이언트 실험 4회, 프로토콜 스키마 105/85/10/1 |
| APP-002 | EVT-003~005 thread·turn·item 알림 | 합류 전 상태 알림만, 첫 턴 후 `thread/resume` 합류 시 `turn/*`·`item/*` 수신 |
| APP-003 | EVT-007 프라이버시 | 프로브가 메서드·키·오류 코드·HMAC만 출력, 단위 테스트 4개(WebSocket 프레이밍 포함) |
| APP-004 | SEC 최소 권한 | 제어 소켓의 실행·쓰기 요청 존재 확인, 기본 미사용 결정(ADR-0002) |

## 데이터 모델 검증 매핑(#22)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DB-001 | EVT-001·007 저장 최소화 | 자유 형식·원문 이름 컬럼 부재, 이벤트 컬럼 = v1 허용 필드 + 서버 전용 2개 |
| DB-002 | EVT-002 멱등성 | `event_id` PK, `ON CONFLICT DO NOTHING` 3회 재전송 1건 |
| DB-003 | DEV-004·SEC-002 장치 격리 | 다른 장치의 세션에 이벤트 삽입 거부(복합 FK) |
| DB-004 | EVT-004 부모·자식 | 다른 세션 부모 거부, lead의 부모 금지 |
| DB-005 | AUTH·USER 관리자 보호 | 마지막 활성 SUPER_ADMIN 삭제·강등·비활성화 거부 |
| DB-006 | TEAM-001~003 팀 역할 | OWNER 정확히 1명, 소유권 이전, OWNER 사용자 삭제 거부, 팀 삭제 연쇄 |
| DB-007 | OFF-003 책상 배정 | 세션의 레이아웃당 책상 1개 |
| DB-008 | OPS 마이그레이션 | head와 메타데이터 차이 0, downgrade·재-upgrade, 서버 이미지 안 `alembic upgrade head`(CI amd64·arm64) |

## 장치 인증 검증 매핑(#26)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DEVAPI-001 | DEV-001·002 등록·명명 | 코드 발급→등록→heartbeat→목록 흐름, 소유자 = 코드 발급자, 이름 중복 409와 코드 미소비 |
| DEVAPI-002 | DEV-004·SEC-005 자격증명 보호 | 토큰·코드는 SHA-256만 저장, 감사 로그에 원문 없음, 클라이언트 식별자 필드 422 |
| DEVAPI-003 | DEV-004 갱신 | 회전 후 새 토큰 유효, 이전 토큰 5분 유예 후 401, 유예 토큰 재회전 409 |
| DEVAPI-004 | DEV-005 폐기 | 자기 폐기·소유자 폐기 후 401, 타인 장치 404, CSRF 헤더 없으면 403 |
| DEVAPI-005 | SEC-001 계정 상태 | 비활성 사용자의 장치 토큰 401, 열린 코드 등록 400 |
| DEVAPI-006 | DEV-003 코드 남용 방지 | 일회용·만료·형식 오류·없음 구분 없는 400, 열린 코드 5개 초과 429 |

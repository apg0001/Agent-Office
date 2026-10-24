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

## 이벤트 수집 검증 매핑(#35)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| ING-001 | EVT-002 멱등성 | 배치 100회 재전송 시 DB 중복 0, 같은 `event_id`의 다른 내용은 `event_id_conflict`, 시각 표기 차이는 같은 digest |
| ING-002 | DEV-003·SEC-005 서버 판정 | 다른 `device_id` 거부, 장치 간 같은 세션 ID 분리, 무토큰·위조·폐기 토큰 401 |
| ING-003 | EVT-004 부모·자식 | 배치 내 순서 무관 해결, 부모 미도착 재시도 가능 거부 후 해결, 부모 변경 거부 |
| ING-004 | EVT-001·007 계약·프라이버시 | 유효·무효 fixture 계약 테스트, 거부 코드와 응답 스키마 일치, 응답 JSON Schema 검증, 422 응답에 입력 값 미포함 |
| ING-005 | SEC 전송 제한 | 본문 1MiB 413·chunked 411(본문 읽기 전), 배치 0·501개 422, 장치별 속도 제한 429 |
| ING-006 | EVT-006 시간 | 30일 초과 과거 `retention_window_exceeded`, 1일 초과 미래 `occurred_at_out_of_range`, 5분 미래 허용 |

## 브라우저 세션 검증 매핑(#25)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| SESS-001 | AUTH-005 쿠키 보호 | `HttpOnly`·`Secure`·`SameSite=Lax`·`Path=/` 속성, 로컬 HTTP에서만 Secure 해제 |
| SESS-002 | SEC-001 CSRF | 토큰 없음·위조·다른 세션 토큰 403, 같은 세션 토큰 통과, 토큰에 세션 원문 없음 |
| SESS-003 | AUTH-005 만료·폐기 | 절대 만료·유휴 만료 401, 마지막 사용 갱신 5분 간격, 비활성 사용자 401 |
| SESS-004 | AUTH-005 로그아웃 | 로그아웃 후 401과 쿠키 삭제, 전체 로그아웃은 장치 토큰 유지, 자기 세션만 목록·폐기(타인 404) |
| SESS-005 | AUTH-005 세션 고정·남용 | 로그인 시 이전 세션 폐기, 활성 세션 20개 상한, 해시만 저장 |

## 상태 엔진 검증 매핑(#36)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| STATE-001 | EVT-003 세션 생명주기 | 시작·활동으로 OPEN, 종료로 ENDED, 종료 뒤 더 늦은 활동은 재개, 종료보다 오래된 이벤트로 재개 안 함 |
| STATE-002 | EVT-005 활동 상태 | 턴·도구·승인 전이, 관측된 승인만 `WAITING_USER`, 턴 완료는 IDLE이지만 세션 종료 아님 |
| STATE-003 | EVT-006 순서 | 늦게 도착한 과거 이벤트는 저장만 하고 현재 상태·버전을 되돌리지 않음(시각 후 sequence 비교) |
| STATE-004 | EVT-004 하위 에이전트 | 하위 에이전트 독립 상태·시작/종료 시각, 대장 턴 완료 중에도 하위 WORKING 유지 |
| STATE-005 | 연결 상태 분리 | heartbeat 120초 기준 ONLINE/DISCONNECTED, 연결 끊김·30분 무활동 OPEN 세션만 읽을 때 UNKNOWN, ENDED로 바꾸지 않음 |

## Go Collector 어댑터 검증 매핑(#29)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| ADP-001 | COL-003~005 Hook 입력 매핑 | Claude·Codex Hook 이벤트 표 테스트, 미지 이벤트 `unknown`, Codex CLI `PostToolUse`로 `tool.completed` 미생성 |
| ADP-002 | EVT-002 이중 수집 멱등 | `claude/parity_test.go`: dual-source fixture로 `expected.json` 통과 키·억제 8건·완료 유형 재현, Hook·transcript `event_id` 동일, Python UUIDv5 값 일치, 재시작 재처리 0 |
| ADP-003 | COL-006 버전 감지·미검증 처리 | `VerifiedTargets` 정확 일치만 `verified`/`observed`, 다른 버전·OS·모드·미관측 이벤트 `unverified`/`inferred`, Codex Desktop `partial` |
| ADP-004 | EVT-007 프라이버시 | 민감 필드·도구 이름·하위 에이전트 유형·식별자 위치의 sentinel이 후보·오류 문자열에 0건, 정확한 키 일치, Codex 합성 fixture 원문 패턴 0건 |
| ADP-005 | EVT-001 enum 정합 | 후보 enum이 이벤트 v1 `provider`·`source`·`event_type`·`activity`·`confidence` 값의 부분집합 |
| ADP-006 | COL-004 fail-open | 크기 상한·깨진 JSON·깊은 중첩 typed 오류, `recover`, 퍼즈 시드 테스트(로컬 30초 퍼즈 약 96만 회 실패 0) |

## 사무실 실시간 검증 매핑(#37)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| LIVE-001 | OFF-010 스냅샷·복원 | 스냅샷이 자기 장치·세션·에이전트 상태만 담음, 재접속 시 끊긴 동안의 변경 복원 |
| LIVE-002 | 실시간 증분 | 이벤트 수집 후 delta 수신, `change_seq` 증가, 세션 종료 delta |
| LIVE-003 | SEC-001 인증·철회 | 쿠키 없음·위조 4401, 로그아웃 시 연결 즉시 종료 |
| LIVE-004 | SEC 교차 사이트 | 다른 Origin 4403, 같은 호스트 Origin 허용 |
| LIVE-005 | SEC-002 격리·프라이버시 | 다른 사용자의 변경 미전달, Collector 원본 ID·토큰 미노출 |

## 사무실 렌더러 검증 매핑(#45)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| OFFR-001 | OFF-001 맵·투영 분리 | 논리 그리드와 투영 모듈 분리, 투영 왕복·깊이 순서 테스트 |
| OFFR-002 | OFF-001 구역·장애물 | 구역별 걷기 가능 칸 존재, 가구·외벽 차단, 출입구 통행 |
| OFFR-003 | OFF-003·004 책상 | 자리 14개가 업무석 안 걷기 가능 칸, 업무석 확장 시 기존 자리 불변, 종료 세션 미배정 |
| OFFR-004 | OFF-007 행동 | 활동→구역 매핑, IDLE 휴게실, 하위 에이전트는 대장 옆 |
| OFFR-005 | 진실성 | 합성 데이터 배지 SSR 테스트, 합성 진행 함수의 서버 데이터 거부, 결정적 합성 |

## 실제 상태 연결 검증 매핑(#47)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| BEH-001 | OFF-007 관측·추정 구분 | 추정 이벤트가 만든 현재 상태 `confidence=inferred` 저장·스냅샷 노출, 화면 반투명·`?` 표시 |
| BEH-002 | OFF-007 폭주 병합 | 1.5초 안의 활동 변경은 표시 구역 유지, 이후 변경 반영, 승인 대기·유휴 즉시 |
| BEH-003 | OFF-010 실시간 적용 | 스냅샷 교체, `change_seq` 큰 갱신만 적용, 합성 상태와 혼합 금지, 메시지 형식 검증 |

## Go Collector tail 검증 매핑(#30)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| TAIL-001 | COL-005 JSONL 완결 줄 읽기 | `tail_test.go`: 참조 구현 tail 테스트 10종 이식(partial, BOM+CRLF, 깨진 줄 3건, 재시작, truncate, 같은 크기 재작성, 로테이션, 파일 없음, 권한 거부(root·Windows 건너뜀), 1MiB 초과 줄) |
| TAIL-002 | COL-008 재시작 복구 | 버전형 체크포인트 JSON 왕복 후 재개 시 재방출 0, 다른 버전 `ErrCheckpointVersion`, 퍼즈·무작위 200회 시나리오에서 조각 append·재시작·읽기 상한을 섞어도 중복·유실 0 |
| TAIL-003 | COL-005 로테이션 | `rotation_test.go`: 열린 핸들로 이전 파일 꼬리 drain 후 새 파일 처음부터, 이름 변경 후 재생성 전 `missing` 중 drain, 삭제 후 재생성 `rotated` |
| TAIL-004 | COL-003 탐색 | `discover_test.go`: `projects/<p>/<s>.jsonl`·`subagents/<a>.jsonl` 분류, 깊이 상한, 개수 상한, 심볼릭 링크 3종 미추적, 읽을 수 없는 디렉터리 건너뜀, 안정 `Rel`·`ID` |
| TAIL-005 | EVT-007 읽기 전용·프라이버시 | `readonly_test.go`: 탐색·tail 전후 감시 루트 항목 수·내용·mode·mtime 불변, 루트 오류에 경로 없음, 상태 enum·sentinel 오류만 |
| TAIL-006 | COL-005 어댑터 연동 | `fixture_test.go`: #20 합성 transcript를 Claude 배치로 놓고 탐색→tail→`ParseTranscriptLine` 후보가 직접 파싱 결과와 동일 |

## 책상 배정 검증 매핑(#48)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DESK-001 | OFF-003 세션 동안 유지 | 같은 세션의 후속 이벤트에도 책상 번호 불변, 스냅샷 `desk_slot` |
| DESK-002 | OFF-004 빈 자리 재사용 | 종료 후 30초 안에는 비우지 않고 새 번호, 이후 가장 낮은 번호 재사용 |
| DESK-003 | OFF-004 1~30 확장 | 30세션 번호 0~29 고유, 업무석 줄은 필요할 때만 증가·기존 자리 불변 |
| DESK-004 | 사용자 격리 | 사용자마다 별도 개인 레이아웃 |
| DESK-005 | 웹 배치 일치 | 서버 `slot_cell`과 웹 자리 좌표 규칙 일치, 반납 책상 미배정, 합성 ID 고유·형식 |

## 경로·카메라 검증 매핑(#49)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| NAV-001 | OFF-005 경로 탐색 | 출입구→모든 자리 경로가 걷기 가능 칸만 한 칸씩 이동, 가구 우회, 막힌 목적지는 가장 가까운 칸 |
| NAV-002 | OFF-005 목적지 갱신 | 목적지 변경 시 현재 칸에서 재탐색(장면), 이미 도착 시 빈 경로 |
| NAV-003 | OFF-006 카메라 | 수동 조작 시 자동 초점 일시정지·재활성화, 줌 범위 고정, 활동 캐릭터 중심 초점 |

## 생활·퇴근 연출 검증 매핑(#50)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| LIFE-001 | OFF-008 퇴근 | 세션 종료·하위 완료일 때만 퇴근, 그 밖에는 자리 유지 |
| LIFE-002 | 연결 상태 분리 | `DISCONNECTED`는 퇴근 없이 흐림·연결 끊김 표시 |
| LIFE-003 | 열림 미확인 | `UNKNOWN`은 옅게·마지막 활동 이후 대기 표시 |
| LIFE-004 | IDLE 생활 연출 | 주기적 자리 변경, 연속 ID도 시작 시점 분산 |

## Go Collector 정규화 검증 매핑(#32)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| NORM-001 | EVT-001 v1 계약 일치 | `schema_test.go`: `Event` 직렬화 키 = 스키마 `required` = `properties`, 패키지 enum = 스키마 enum, 의존성 없는 스키마 검사기로 모든 출력 검증 |
| NORM-002 | EVT-004·SEC-006 식별자 매핑 | `normalize_test.go`: 원본 세션·에이전트 참조 → 장치·제공자·세션 범위 UUIDv5(별도 namespace), 재시작 결정성, 하위 에이전트 `parent_agent_id` = 대장 |
| NORM-003 | COL-006 시간·순서 | 검증된 transcript `timestamp`만 사용(시간대 없음·미래·형식 오류는 관측 시각), UTC RFC 3339, 세션별 sequence 0부터 단조 증가·영속 저장소 재개·중복 미소비·상한 |
| NORM-004 | EVT-002 이중 수집 멱등 | `pipeline_test.go`: #20 dual-source fixture를 Hook + tail → 어댑터 → 정규화로 양쪽 순서 처리, 키 이벤트 10개·억제 8건, 영속 키 재시작 재방출 0 |
| NORM-005 | EVT-005 진실성 | 미검증·부분 지원은 `inferred`, enum 밖 값은 `unknown`+`inferred`, 세션 없는 `unknown`은 개수만 세고 버림, Codex CLI `tool.completed` 미생성 |
| NORM-006 | EVT-007·COL-007 프라이버시 | `privacy_test.go`: 모든 어댑터 입력 필드·런타임·후보 문자열 필드의 sentinel이 출력 JSON·카운터·오류에 없음, 어댑터 오류 fail-open 개수 |

## 상세 패널 검증 매핑(#51)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| VIEW-T001 | VIEW-001 상태·세션·PC·역할 | 서버 상세 응답의 장치·세션·역할, 패널 렌더링 |
| VIEW-T002 | VIEW-002 관계 | 리드↔하위 상위·하위 연결(서버·합성 모두) |
| VIEW-T003 | VIEW-003 안전한 타임라인 | 최신순, 허용 5개 필드만, 원본 세션·에이전트 ID 미노출 |
| VIEW-T004 | VIEW-004 원문 없는 계획 단계 | 단계 번호·상태만 표시. 수집 경로는 #38(현재 빈 목록) |
| VIEW-T005 | 권한 | 다른 사용자 에이전트·없는 ID 모두 404, 미로그인 401 |

## 픽셀 캐릭터 검증 매핑(#46)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| SPR-001 | OFF-002 역할 구분 | 리드 왕관·큰 몸, 하위 넥타이·작은 몸(모양 차이 테스트) |
| SPR-002 | OFF-002 PC 구분 | 장치 ID별 옷 색 고정·범위, 다양한 분포 |
| SPR-003 | EVT-004 안전한 라벨 | PC 이름만, 제어 문자 제거·10자 축약, 하위는 역할만 |
| SPR-004 | 연출 | 걷는 중에만 다리 프레임 교대 |

## 이메일 계정 검증 매핑(#23)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| ACC-001 | AUTH-001 가입·검증 | 가입→확인 메일→일회용 토큰 소비→로그인, 재사용·만료 토큰 거부 |
| ACC-002 | AUTH-002 로그인 | 세션 쿠키·CSRF 발급, 기존 세션 교체(고정 공격 방지) |
| ACC-003 | AUTH-003 복구 | 요청 응답 동일, 토큰 1회, 완료 시 모든 브라우저 세션 폐기 |
| ACC-004 | AUTH-006 가입 정책 | 기본 CLOSED, INVITE_ONLY 거부, ADMIN_APPROVAL 확인 후 대기 |
| ACC-005 | SEC-003 비밀번호 | Argon2id, 길이 정책, 해시에 원문 없음 |
| ACC-006 | SEC-004 열거·남용 방지 | 실패 응답 동일, 중복 가입 응답 동일, 이메일별 속도 제한 429 |
| ACC-007 | 교차 사이트 | 다른 Origin·폼 형식 403 |
| ACC-008 | SMTP 미설정 | 503, 계정 미생성 |

## Go Collector 큐·전송 검증 매핑(#33)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| QUEUE-001 | COL-007 로컬 영속 큐 | `queue_test.go`: SQLite WAL·`synchronous=FULL`·`busy_timeout`·스키마 버전(새 버전 거부), 파일 0600, 레코드·sequence·결정적 ID·체크포인트 한 트랜잭션, 잘못된 레코드 시 무커밋, 상한 초과 drop-oldest 개수 |
| QUEUE-002 | COL-009 재시작 복구 | 재시작 후 같은 `event_id`(무작위 포함)·순서, sequence 이어짐, 영속 결정적 ID로 키 이벤트 재방출 0, 미기록 sequence 비영속 |
| QUEUE-003 | EVT-002 멱등 재전송 | `transport_test.go`: 서버 저장 후 응답 전 연결 끊김 → 재시작 → 같은 본문·ID 재전송 → `duplicate` ACK 삭제, accepted·duplicate만 삭제, 잘못된 응답 7종 ACK 없음, 계약 fixture 응답 처리 |
| QUEUE-004 | COL-008 배치·재시도 | 500건·1MiB(설정 한도) 분할, 5xx·429 full jitter 지수 백오프와 `Retry-After` 하한, 타임아웃, 401·403 정지(큐 유지), 422 이분 탐색, retryable·알 수 없는 거부 코드, `max_attempts` dead-letter |
| QUEUE-005 | COL-007·SEC-006 프라이버시 | Hook 원문·ID 필드, 체크포인트 경로, 거부 코드 sentinel이 DB 본 파일·WAL·오류 문자열에 없음, 전송 오류에 본문·토큰·URL 없음 |

## 최초 관리자 bootstrap 검증 매핑(#66)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| BOOT-001 | SETUP-002 콘솔 일회성 토큰 | CLI 출력으로만 발급, DB에는 해시만, 재발급 시 이전 무효, 만료 거부 |
| BOOT-002 | SETUP-001 최초 SUPER_ADMIN | 토큰으로 활성 관리자·세션 생성, 이후 재사용·재발급 거부 |
| BOOT-003 | SEC-004 경합 | 같은 토큰 동시 4요청 중 1건만 성공, 관리자 1명 |
| BOOT-004 | 설치 완료 잠금 | `SUPER_ADMIN`만 완료 가능, 완료 후 상태 반영 |
| BOOT-005 | 오류 경계 | 입력 오류·중복 이메일은 토큰 미소비, 교차 사이트 403 |

## 관리자 설정 검증 매핑(#67 서버)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| CFG-001 | SETUP-003 사이트 주소 | 서비스명·기준 주소 저장, 위험 URL(경로·자격 증명·비 http) 거부 |
| CFG-002 | SETUP-004 가입 정책 | 네 정책 저장·반영, 그 밖 값 422 |
| CFG-003 | SETUP-005 SMTP | 저장·유지·삭제, 시험 발송(관리자 본인), 오류 코드만 반환 |
| CFG-004 | 비밀 평문 재표시 금지 | 응답·설정 행·암호문에 평문 없음, 설정 여부만 표시 |
| CFG-005 | 비밀 암호화 | AES-GCM 왕복, AAD 바인딩, 변조·다른 키 거부, 키 없으면 503 |
| CFG-006 | AUTH-004 OAuth 설정 | client ID·secret 모두 있어야 활성화, 공급자 제한 |
| CFG-007 | 권한 | 일반 사용자 403, 미로그인 401, CSRF 없으면 403 |

## 설치 마법사·계정 화면 검증 매핑(#67 웹)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| WEB-ACC-001 | SETUP-001 마법사 진입 | 설치 상태·역할별 시작 단계(완료·관리자 생성·로그인 필요·설정) |
| WEB-ACC-002 | SETUP-003~005 설정 단계 | 사이트→가입 정책→메일→완료 순서 |
| WEB-ACC-003 | AUTH-001~003 계정 화면 | 경로별 화면, 로그인 화면에 합성 사무실 없음 |
| WEB-ACC-004 | 토큰 취급 | URL 조각 토큰 형식 검사, 읽은 뒤 주소창에서 제거 |
| WEB-ACC-005 | API 호출 | JSON·CSRF 헤더·same-origin 쿠키, 오류 코드→한국어 문구, 네트워크 오류 |

## 팀·초대·프로젝트 공유 검증 매핑(#27)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| TEAM-T001 | TEAM-001 팀·OWNER 유일 | 생성자 OWNER, 역할 변경으로 OWNER 지정 불가, 이전 시 강등 후 승격 |
| TEAM-T002 | TEAM-002 역할 권한 | OWNER·MANAGER·MEMBER별 초대·수정·내보내기·삭제 허용/거부 |
| TEAM-T003 | TEAM-003 초대 | 일회용, 재초대 시 이전 무효, 취소·만료 거부, 다른 계정 수락 거부, 중복 구성원 409 |
| TEAM-T004 | TEAM-004 탈퇴·이전 | OWNER는 이전 후 탈퇴, 이전 후 원래 OWNER는 MANAGER |
| TEAM-T005 | TEAM-005 다중 팀 | 한 사용자가 팀마다 다른 역할 |
| TEAM-T006 | TEAM-006 공유 동의 | 소유자만, 소속 팀에만 공유, 해제 즉시 반영, 외부인 404 |
| TEAM-T007 | SEC-001 역할 독립 | SUPER_ADMIN도 팀 권한 없음 |
| TEAM-T008 | SEC-002 존재 숨김 | 남의 팀·없는 팀 같은 404, 구성원 이메일 미노출 |
| TEAM-T009 | AUTH-006 초대 가입 | INVITE_ONLY에서 초대 토큰·이메일 일치 시에만 가입 |

## 원격 접속·HTTPS 검증 매핑(#57)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| NET-001 | OPS-001 단일 진입점 | 운영 Compose 실기동: proxy만 호스트 포트, `/api`·웹·SPA 경로 분기 |
| NET-002 | OPS-002 실제 클라이언트 IP | 서버 로그가 클라이언트 IP 기록, 위조 `X-Forwarded-For` 무시 |
| NET-003 | OPS-003 보안 헤더 | `X-Frame-Options`·`Referrer-Policy`·`nosniff`, `Server` 제거 |
| NET-004 | DOC-001~003 가이드 | Tailscale·Cloudflare Tunnel·도메인 HTTPS·Nginx 절차와 위협 모델(외부 연동은 미검증 표시) |

## Go Collector 실행·진단 검증 매핑(#34)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| RUN-001 | COL-001 GUI와 분리된 Core 실행 | `e2e_test.go`: `httptest` 서버로 `register` → `run`(tail·정규화·큐·전송·heartbeat 고루틴) → 새 transcript·Hook 이벤트 수신 → 컨텍스트 취소(SIGINT/SIGTERM 경로)로 정상 종료(`stopped`, 큐 0). `cli_test.go`: 두 번째 `run` 거부(단일 실행 잠금) |
| RUN-002 | COL-002 OS별 자동 시작 | `service_test.go`: systemd 사용자 unit·launchd LaunchAgent·Windows 로그온 작업 XML 골든, 인용·이스케이프·UTF-16LE, 소유 표시 없는 파일·작업은 교체·삭제 거부. 실제 systemd·macOS·Windows 등록은 미실행 |
| RUN-003 | COL-003 탐지·최초 위치 | 최초 실행에서 기존 transcript 끝으로 건너뜀(`baseline_skipped_files`), 이후 새 파일은 처음부터 수집, 감시 루트 없음·접근 불가 상태 코드 |
| RUN-004 | COL-004·COL-005 Hook fail-open | `cli_test.go`: 잘못된 입력 9종·끝나지 않는 stdin·쓸 수 없는 spool·실제 하위 프로세스에서 종료 코드 0, 출력 없음, 제한 시간 안 종료. spool에는 어댑터 후보만 있고 prompt·경로 sentinel 없음 |
| RUN-005 | COL-010 일시중지·재개 | 일시중지 중 tail·Hook spool·전송·heartbeat 정지, 재개 후 일시중지 동안 쓰인 줄 미수집(`skip_backlog`), 손상된 pause 파일은 일시중지로 처리 |
| RUN-006 | COL-010·SEC-006 안전한 진단 | `status`·`diagnose` 출력(텍스트·JSON)에 토큰·서버 호스트·장치 ID·로컬 경로 sentinel 없음, 고정 오류 코드·개수만 표시, 설정 파일 0600·원자적 쓰기·토큰 `fmt` 가림 |

## 팀 사무실·철회 검증 매핑(#54, #55)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| TOFF-001 | TEAM-004 공유한 세션만 | 프로젝트 미지정·미공유 세션 제외, 공유 후 표시 |
| TOFF-002 | OFF-009 팀 배치 | 팀 화면용 책상 순번, PC 이름 대신 소유자 표시 이름 |
| TOFF-003 | TEAM-005 외부인 차단 | 비구성원·없는 팀 REST 404, WebSocket 4404, 상세 404 |
| TOFF-004 | TEAM-006 공유 해제·추방 | REST 즉시 반영, 추방 시 공유 자동 해제 |
| TOFF-005 | SEC-001 실시간 철회 | 공유 해제 시 새 스냅샷으로 제거, 추방 시 WebSocket 종료 |
| TOFF-006 | SEC-002 지정 권한 | 남의 세션·남의 프로젝트 지정 404 |
| TOFF-007 | 증분 계산 | 변경 없음·행 변경·행 제거 구분 |

## 팀 관리 화면 검증 매핑(#52)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| TUI-001 | TEAM-001 역할별 화면 | OWNER·MANAGER·MEMBER 권한 표가 서버 규칙과 일치 |
| TUI-002 | TEAM-002 내보내기 범위 | OWNER는 OWNER 외, MANAGER는 MEMBER만 |
| TUI-003 | TEAM-003 마지막 OWNER 보호 | 소유자 나가기 숨김·이전 안내 |
| TUI-004 | 경로 | `/teams` 화면 렌더링 |

## 개인·팀 통합 사무실 전환 검증 매핑(#53)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| VIEWS-001 | TEAM-005 통합 사무실 | 내 세션 + 팀 공유 세션, 두 팀에 공유된 세션도 한 번만 |
| VIEWS-002 | OFF-009 배치만 분리 | 범위별 책상 순번, 이벤트 단일 저장 |
| VIEWS-003 | 이름 노출 | 내 장치는 PC 이름, 남의 장치는 표시 이름, 비공유 세션·장치 이름 미노출 |
| VIEWS-004 | 철회 반영 | 통합 실시간에서 공유 해제 시 새 스냅샷으로 제거 |
| VIEWS-005 | 상세 권한 | 통합 상세는 보이는 팀 경유, 외부인 404 |
| VIEWS-006 | 웹 범위 | 주소↔범위 왕복, 범위별 실시간·상세 경로, 잘못된 값은 개인 |

## 보존·계정 삭제 검증 매핑(#39)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| RET-001 | OPS-015 30일 삭제 | 31일 지난 이벤트·종료 세션 삭제, 최근 데이터 유지 |
| RET-002 | OPS-016 활성 상태 예외 | 이벤트가 지워져도 열린 세션·현재 상태 유지 |
| RET-003 | 토큰·감사 로그 정리 | 만료 토큰 삭제, 감사 로그 기본 365일·최소 30일 |
| RET-004 | 동시 실행 | advisory lock으로 두 번째 실행은 건너뜀 |
| RET-005 | USER-003 계정 삭제 | 확인 입력, 데이터 연쇄 삭제, 마지막 관리자·공동 팀 소유자 거부 |
| RET-006 | 복원 후 재삭제 | 삭제 기록으로 되살아난 계정 재삭제 |
| RET-007 | 책상 배정 | 배정된 세션 삭제 시 제약 위반 없이 책상 해제(실측 발견) |

## 헬스·모니터링 검증 매핑(#59)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| MON-001 | OPS-009 익명 헬스 | 준비 상태 200/503, 값은 ok·fail·outdated만 |
| MON-002 | OPS-010 지표 | 호스트·DB·수집 지표, 관리자만, 식별자·PC 이름·URL 미포함 |
| MON-003 | OPS-011 외부 Heartbeat | 준비 정상일 때만 push, 성공·실패 기록, URL 미로그 |
| MON-004 | OPS-012 전원 차단 감지 | 외부 dead-man 방식 문서화(실측 미실행) |

## Go Collector Hook 설치 검증 매핑(#31)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| HOOK-001 | COL-004 비침습 설치 | `hooks_test.go`: 남의 Hook·알 수 없는 키(숫자 원문 `1.50`, `<&>` 포함)·최상위 키 순서 보존, 설치 전 원본 바이트 그대로의 0600 타임스탬프 백업, 파일 권한 보존, 임시 파일 + rename 원자적 쓰기 |
| HOOK-002 | COL-004 소유 항목만 해제 | 명령 서명(`agent-office-collector[.exe] hook <tool>`)으로만 소유 판정. 제거 후 원본과 의미상 같은 JSON, 남의 핸들러와 같은 그룹에 있던 소유 핸들러만 제거, 두 번째 설치·제거는 변경·백업 없음 |
| HOOK-003 | COL-004 거부 조건 | 주석·뒤따르는 값·중복 키·배열 최상위·깨진 JSON·`hooks` 형식 오류는 쓰기·백업 없이 거부, 읽기 전용 파일·심볼릭 링크·쓸 수 없는 디렉터리 거부, 읽은 뒤 파일이 바뀌면 덮어쓰지 않음 |
| HOOK-004 | COL-004 fail-open 명령 | linux·darwin·windows 골든: 타임아웃 5초, 공백·작은따옴표 경로 인용, Windows `"C:/…" hook <tool>`, 인용할 수 없는 경로 거부. 실제 Claude·Codex가 설치된 Hook을 호출하는 경로는 미실행 |
| HOOK-005 | COL-009 상태·복구 | `hooks status`의 `installed`/`partial`/`modified`/`not_installed`/`unreadable`, 이동한 실행 파일 항목 교체, 누락 이벤트만 추가. CLI 출력에 토큰·홈 경로 원문 없음 |
| HOOK-006 | COL-004 버전 감지 | `--version` 첫 줄에서 버전만 추출, 어댑터 실측 표와 제품·버전·OS가 정확히 같을 때만 verified/partial. 감지한 Claude 버전을 등록 설정 `claude_version`에 기록하고 실행 모드는 기록하지 않음 |

## 백업·복원 시험 검증 매핑(#58)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| BAK-001 | OPS-004 일관성 덤프 | PostgreSQL 17 `pg_dump -Fc` 실제 덤프, `pg_restore --list` 검사 |
| BAK-002 | OPS-005 암호화 | AES-256-GCM 청크, 평문 미기록, 변조·잘림·덧붙임·다른 키 거부 |
| BAK-003 | OPS-006 보존 | 로컬 7개, 외부 일간 7개 + 주간 4개 |
| BAK-004 | OPS-007 무결성 | 매니페스트 SHA-256, 손상 파일 `checksum_mismatch`, 외부 복사 체크섬 |
| BAK-005 | OPS-008 복원 시험 | 임시 DB 복원·마이그레이션·표 확인 후 삭제, 운영 이미지 실측 |
| BAK-006 | 비밀 | DB 암호는 환경 변수로만, 오류는 코드만 |

## 운영 설정·진단 화면 검증 매핑(#68)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| ADM-001 | SETUP-006 진단 | 준비·디스크·백업·복원 시험·Heartbeat·비밀 키·SMTP·접속 주소 경고 규칙 |
| ADM-002 | OPS-004~008 백업 조작 | 지금 백업·복원 시험 API, 키 없음 503, 결과 상태 기록, CSRF |
| ADM-003 | SETUP-007 설정 경계 | 인프라·인스턴스·개인 설정 위치 안내 |
| ADM-004 | 권한 | 관리자 외 접근 차단, 경로·렌더링 |

## 공개 가입 보호·계정 관리 검증 매핑(#28)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| ABU-001 | AUTH-001 관리자 승인 | 이메일 확인 전 승인 거부, 확인 후 승인·로그인 |
| ABU-002 | SEC-004 남용 방지 | 숨은 입력란 봇 무시, 차단 도메인(하위 포함), 인스턴스 전체 가입 한도 429 |
| ABU-003 | 마지막 SUPER_ADMIN 보호 | 강등 409, 다른 최고 관리자 생긴 뒤 허용, 자기 비활성화 금지 |
| ABU-004 | 비활성화 즉시 반영 | 세션 폐기, 재활성화 시 확인 여부에 따라 상태 |
| ABU-005 | 감사 로그 | 커서 페이지·접두어 필터·입력 형식 검사 |
| ABU-006 | USER-003·OPS-016 | 계정 삭제·30일 보존·복원 후 재삭제는 #39(RET-005~006) |

## 계획 진행도·타임라인 검증 매핑(#38)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| PLAN-001 | VIEW-003 원문 없는 계획 | 스키마상 상태 enum만, 단계 문장 fixture 거부 |
| PLAN-002 | EVT-007 계획 반영 | plan.updated로 단계 교체, 늦게 온 과거 계획 무시 |
| PLAN-003 | 계약 | plan.updated 외 이벤트의 plan_steps 422, 기존 이벤트 digest 불변 |
| PLAN-004 | VIEW-002 타임라인 페이지 | 최신순 커서 페이지, 허용 필드만, 잘못된 커서 400, 한도 초과 422 |
| PLAN-005 | 스코프 | 남의 에이전트 404, 공유 안 된 팀 타임라인 404 |

## 운영 알림 검증 매핑(#70~#74)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| NTF-001 | #70 공통 모델·템플릿 | 종류별 고정 문구, 원인 코드 형식 검사, 서비스 이름 정리 |
| NTF-002 | #70 비밀 필터 | URL·경로·토큰·이메일 지우기, Discord 멘션 비활성 |
| NTF-003 | #70 라우팅·억제 | 심각도·켜기 라우팅, 1시간 억제, 장애 뒤에만 복구 알림 |
| NTF-004 | #70 재시도·한도 | 재시도 가능 코드 3회, 불가 코드 1회, 채널 시간당 20건, 감사 로그 코드만 |
| NTF-005 | #71~#73 Webhook 보호 | 공급자 공식 도메인·https만, 자격 증명 주소 거부, 암호문 저장·비노출 |
| NTF-006 | #74 이메일 | 수신자 1~10명 정규화, SMTP 미설정 코드 |
| NTF-007 | 권한 | 관리자만, 비밀 키 없으면 503 |

## 관리 대시보드·보안 점검 검증 매핑(#61)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DASH-001 | 대시보드 지표 | 사용자·Collector·버전·이벤트·지연·자원·백업(#59·#68) + 배포 버전 |
| DASH-002 | 보안 점검 | 쿠키·HTTPS·Origin·비밀 키·백업·외부 복제·공개 가입·bootstrap 경고, 안전 설정에서 사라짐 |
| DASH-003 | 민감정보 | 경고에 주소·키 값 없음 |
| DASH-004 | 로그 보존 | 컨테이너 로그 회전·감사 로그·이벤트 보존 문서화 |

## Collector 계획 단계 검증 매핑(#38)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| CPLAN-001 | VIEW-003 원문 없는 계획 | `TodoWrite`·`update_plan` 문장 sentinel이 후보·이벤트·카운터·오류·큐 DB(WAL 포함)에 없음, 상태만 저장 |
| CPLAN-002 | EVT-007 계획 반영 | Claude Hook·transcript, Codex `update_plan`에서 `tool.started`와 `plan.updated`(`planning`) 두 기록, 스키마 유효 |
| CPLAN-003 | 상태 매핑 | 정확한 `pending`·`in_progress`·`completed`만 유지, 그 밖 `unknown`, 50개 상한, 빈 목록·실패 호출은 계획 없음, 퍼즈 |
| CPLAN-004 | 중복 제거 | 종류 `plan.updated`·`tool_use_id` 키로 Hook Pre/Post·transcript 순서 무관 1회, 다른 호출은 새 계획 |
| CPLAN-005 | 검증 수준 | 계획 입력 형식 미실측이라 항상 `inferred`, Codex CLI `PostToolUse`는 계획 없음 |

## 배포·롤백 검증 매핑(#60)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| DEP-001 | 배포 전 백업 | 새 이미지 백업 도구로 백업, 실패 시 중단 |
| DEP-002 | 마이그레이션 사전 실행 | 백업을 임시 DB에 복원해 새 마이그레이션 적용(통합 테스트·실측), 운영 DB 불변 |
| DEP-003 | health·smoke | 준비 상태·웹·SPA 경로 점검, 예전 이미지는 생존 확인 |
| DEP-004 | 수동 승인 | `deploy` 입력 없으면 변경 없음 |
| DEP-005 | 롤백 | 점검 실패 시 새 이미지로 DB 다운그레이드 후 이전 이미지 복귀(실측) |
| DEP-006 | 비역호환 복원 계획 | 다운그레이드 실패 시 백업 복원 절차 문서화 |

## OAuth 검증 매핑(#24)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| OAUTH-001 | AUTH-004 제공자 비활성 | 켜기·client ID·secret·공개 주소가 모두 있어야 목록에 나옴, 미설정 시작은 `provider_disabled`, 목록에 비밀 없음 |
| OAUTH-002 | SEC-003 state·로그인 CSRF | state 해시만 저장·10분 만료·일회용, 시작 브라우저 HttpOnly 쿠키 결속, 쿠키 없음·다른 쿠키·다른 제공자·만료 거부 |
| OAUTH-003 | SEC-003 PKCE | Google·GitHub 모두 S256 challenge(RFC 7636 예시 값 일치), 토큰 교환에 verifier·같은 redirect_uri |
| OAUTH-004 | AUTH-004 가입 정책 | CLOSED 거부, INVITE_ONLY는 초대 이메일 일치만, ADMIN_APPROVAL 대기, PUBLIC 활성, 차단 도메인, 미검증 이메일 거부 |
| OAUTH-005 | SEC-003 계정 탈취 방지 | 같은 이메일 기존 계정에 자동 연결하지 않음, 다른 사용자 신원 연결 거부, 연결 중 세션 교체 거부 |
| OAUTH-006 | AUTH-004 연결·해제 | 로그인 필요, 사용자당 제공자 하나, CSRF, 비밀번호 없는 계정의 마지막 수단 해제 거부 |
| OAUTH-007 | 계정 상태 | 비활성 사용자 로그인 거부, 승인 대기 사용자 세션 없음 |
| OAUTH-008 | 민감정보 | 액세스 토큰·client secret·이메일·인가 코드·프로필 문구가 서버 로그·감사 로그·리디렉션 URL에 없음 |
| OAUTH-009 | 실제 제공자 | 미실행: 실제 Google·GitHub OAuth 앱 왕복은 검증하지 않음 |

## 설치 방식 동일성 검증 매핑(#69)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| INST-001 | DIST-004 같은 구성 | 두 방식 모두 운영 Compose, 소스는 build 겹침 파일만 추가 |
| INST-002 | SETUP-007 같은 마법사 | 설치 상태 전·후, bootstrap 거부·성공, 설치 완료 동일 |
| INST-003 | DIST-005 같은 보안 기본값 | 가입 차단, 교차 사이트 거부, 쿠키 속성, 보안 헤더, CSRF, 보안 점검 코드 동일 |
| INST-004 | DIST-006 클린 설치 실측 | Pi arm64에서 새 볼륨 두 벌 설치·비교(`tests/install/compare.sh`) |
| INST-005 | DOC-001~004 | `docs/installation/install-methods.md` |

## 보안 시나리오 검증 매핑(#64)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| SEC-ROUTE-001 | SEC-001 경로 전수 | `app.routes` 펼친 목록 = OpenAPI 연산, 모든 경로 분류(새 경로는 분류 전까지 실패) |
| SEC-ROUTE-002 | AUTH-005 미인증 | 모든 사용자·관리 경로가 쿠키 없음·위조·과대 세션에 401 |
| SEC-TENANT-001 | SEC-002·TEAM-001 IDOR | 경로 변수 있는 모든 경로를 A의 ID로 B가 호출하면 403/404, 응답에 A의 값 없음, A 상태 불변 |
| SEC-TENANT-002 | TEAM-002 역할 독립 | ADMIN·SUPER_ADMIN도 남의 개인·팀 자원 404 |
| SEC-TENANT-003 | SEC-002 목록 격리 | 경로 변수 없는 모든 조회 경로에 남의 데이터 없음, 팀원은 공유 세션만(양성 대조) |
| SEC-TENANT-004 | TEAM-004 교차 범위 | 남의 프로젝트 공유·남의 팀에 공유·내 팀 범위로 남의 에이전트·가로챈 초대 토큰 거부 |
| SEC-TENANT-005~006 | SEC-002 WebSocket 범위 | 개인·통합 스냅샷 격리, 비구성원 `?team=` 4404, 내보낸 구성원 WS 4404·REST 404 |
| SEC-TENANT-007 | DEV-004 장치 판정 | 다른 `device_id` 거부, 같은 Collector 세션 ID는 장치별 분리 |
| SEC-ROLE-001~002 | AUTH-006 관리자 경계 | USER는 모든 관리 경로 403, ADMIN의 승격·강등·설치 잠금 403 |
| SEC-ROLE-003 | AUTH-006 상위 역할 보호 | (수정) ADMIN이 SUPER_ADMIN 비활성화·재활성화·승인 403, SUPER_ADMIN은 가능 |
| SEC-ROLE-004 | TEAM-003 팀 역할 상승 | MEMBER·MANAGER의 상위 동작 403, 인스턴스 SUPER_ADMIN인 MEMBER도 403, 수락 본문 역할 주입 422 |
| SEC-CSRF-001~002 | SEC-003 CSRF | 모든 쿠키 쓰기 경로: 토큰 없음·다른 사용자·같은 사용자 다른 세션·임의 값 403, 정적 의존성 확인 |
| SEC-CSRF-003~004 | SEC-003 로그인 전 쓰기 | 다른 Origin·`null`·폼·multipart 403, 허용 Origin 정확 일치 |
| SEC-CSRF-005~006 | SEC-003 WS·쿠키 | 교차 사이트 WS 4403(개인·팀·통합), 쿠키 HttpOnly·SameSite=Lax·Secure |
| SEC-TOKEN-001~004 | SEC-005 자격 증명 분리 | 장치 토큰으로 브라우저 경로·WS 불가, 세션으로 장치 경로 불가, 폐기·만료·비활성·삭제 계정 토큰 401 |
| SEC-MASS-001~002 | SEC-006 대량 할당 | 모든 요청 모델 `extra="forbid"`, 역할·ID 주입 422(값 미반사) |
| SEC-SESSION-001~005 | AUTH-005 세션 철회 | 로그아웃·개별 폐기·전체 로그아웃·비밀번호 재설정·비활성화 뒤 REST 401·WS 4401 즉시, 강등 즉시 반영 |
| SEC-LEAK-001 | SEC-004~006 비밀·원문 | 비밀번호·SMTP·OAuth·Webhook·Telegram·bootstrap·세션·재설정·장치·초대 토큰·원문 필드 sentinel이 응답·전체 DB·로그·메일에 없음(1회 발급 응답만 허용) |
| SEC-INPUT-001~002 | SEC-006 본문 남용 | 413/411/400, 3만 단계 중첩 JSON 4xx |
| SEC-INPUT-003 | SEC-006 문자 남용 | (수정) NUL 400 `invalid_input`, 제어·유니코드·HTML·SQL 모양 문자열은 데이터로 저장 |
| SEC-INPUT-004~006 | SEC-006 ID·퍼즈 | 경로 조작 ID 404/405/422, 이벤트 enum 퍼즈 150회 500 없음·미반사, 커서·필터 퍼즈 |
| SEC-INPUT-007 | SEC-004 SSRF | Webhook 내부 주소·다른 스킴·호스트 혼동·자격 증명 400 |
| SEC-GAP-001 | 미검증 | TLS·프록시 헤더·DNS 재바인딩·웹 XSS·실제 외부 서비스·DoS·타이밍은 `docs/security/threat-scenarios.md`에 미검증으로 기록 |

## Collector GUI 검증 매핑(#40, #41)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| GUI-001 | COL-001 GUI와 Core 분리 | GUI는 Core 명령만 하위 프로세스로 실행(`corecli`), 수집 루프 없음. Xvfb에서 GUI 종료 뒤 Core `status` 정상. ADR-0003 |
| GUI-002 | COL-001 Core 호출 안전성 | `corecli_test.go`: Core 위치 탐색(절대 경로만), 시간 초과 시 프로세스 정지, 출력 상한, 깨진 JSON 거부, 모르는 enum·코드 아닌 오류·개수 키 정리 |
| GUI-003 | COL-001·USER-002 상태 화면 | `app_test.go`·`integration_test.go`: 실제 Core `status`·`diagnose --json`(등록·서버 scheme·loopback·일시중지·큐·자동 시작·서버 도달), 일시중지·재개 |
| GUI-004 | COL-002 자동 시작 | `service print`·`service install [--activate]` 승인 필수, 실제 Core로 systemd unit 파일 설치·상태 `installed`(활성화는 미실행) |
| GUI-005 | DEV-001·002 브라우저 로그인·PC 이름 | 웹 `/devices` 코드 발급(`devices.test.tsx`), GUI는 `<서버>/devices` 열기만 하고 비밀번호를 받지 않음, PC 이름 1~80자·제어 문자 거부, 실제 Core 등록에서 서버가 받은 코드·이름 일치 |
| GUI-006 | DEV-003·004 일회용 코드·토큰 보호 | 코드는 표준 입력(`register --code-stdin`)으로만 전달·형식 오류는 Core 미실행, 화면 값·`gui.json`에 토큰·코드 없음, 흘린 토큰 가림. OS 보안 저장소는 미구현 |
| GUI-007 | DEV-005 폐기 | 웹 `/devices` 소유 PC 폐기 버튼(기존 `POST /devices/{id}/revoke`), Core 로그아웃 명령은 미구현 |
| GUI-008 | COL-003 도구 탐지·Hook 동의 | Core `hooks status --json`(감지·버전·검증 수준·Hook 상태), 미리보기 → 승인 순서·일회성 승인, 실제 Core로 남의 Hook 보존 설치·제거, Codex는 print 전용 |
| GUI-009 | 프런트엔드 계약 | `main_test.go`: `app.js` 호출 메서드가 모두 바인딩됨, 외부 리소스·인라인 스크립트 없음, CSP |
| GUI-GAP-001 | 미검증 | macOS·Windows 빌드·실행, 실제 데스크톱 세션의 브라우저 열기, webkit2gtk 4.1 배포판, 실제 서버 종단 등록은 미실행(`docs/installation/collector-gui.md`) |

## 부하·안정성·브라우저 실측 매핑(#63)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| PERF-001 | OFF-003 표시 지연 | 30세션·90에이전트 표시 P95 1.45초(정상), 1시간 경합 하 2.73초 |
| PERF-002 | EVT-002 수집 안정성 | 1시간 16만 이벤트 배치 실패 0·누락 0 |
| PERF-003 | OPS-009~012 재연결 | 서버 재시작 중 WebSocket 3.4초 회복, 배치 손실 0 |
| PERF-004 | OFF-010 브라우저 FPS | Chromium(SwiftShader) 평균 39.8·하위 5% 29.9FPS |
| PERF-005 | 화면 흐름 | 실제 브라우저 E2E 7단계 통과(CI `E2E (browser)`) |
| PERF-006 | 자원 | 서버 메모리 1시간 91→97MB(누수 징후 없음) |

## 릴리스 준비 검증 매핑(#65 준비)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| REL-001 | DIST-008 릴리스 노트 | 실제 검증·미검증·위험·호환성 초안 `docs/releases/0.1.0-beta.1.md` |
| REL-002 | DIST-009~012 승인 게이트 | 게시·main·태그는 관리자 승인 후(`docs/operations/release.md`), 게시 워크플로 `image-publish` Environment |
| REL-003 | DOC-001~004 | 설치·업그레이드·롤백·복원 문서 연결 |

## Collector 설치 패키지 검증 매핑(#42)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| PKG-001 | OS·아키텍처별 배포물 | 6개 대상 정적 빌드, Linux deb·rpm |
| PKG-002 | 해시 | SHA256SUMS 생성·확인 |
| PKG-003 | 설치·제거 | Debian deb·Fedora rpm 설치·실행·제거(실측) |
| PKG-004 | Hook·자격 증명 보존/삭제 선택 | 제거 도우미 기본 보존·`--purge` 삭제, Claude 기록 유지(실측) |
| PKG-005 | 서명·실장비 | 서명·공증·macOS·Windows는 미구현·미검증(문서화) |

## Collector 자체 업데이트 검증 매핑(#44)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| UPD-001 | 서명된 manifest | ed25519 서명 확인, 다른 키·변조·키 없음 거부, 키 여러 개 허용 |
| UPD-002 | 채널·OS·아키텍처 | 채널 불일치·대상 없음·http 거부, SemVer 우선순위 |
| UPD-003 | 배포물 검증 | 크기·SHA-256, 압축 해제 경로 이동 거부, 새 실행 파일 버전 확인 |
| UPD-004 | 알림 후 업데이트 | `check`는 알림만, `apply`는 `--yes`일 때만 |
| UPD-005 | 재기동 검증·롤백 | 원자적 교체·`.bak`, 비정상 재기동 시 되돌림 |
| UPD-006 | 패키지 설치 | `/usr/bin` 설치는 패키지 관리자로 안내 |

## Linux 실제 사용 흐름 검증 매핑(#62 Linux 부분)

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| FLOW-001 | 실제 Hook 수집 | Claude Code 2.1.308 `claude -p` → Hook → Collector → 서버, 세션·턴·도구 이벤트 |
| FLOW-002 | 원문 비전송 | 프롬프트 표식 문자열이 DB 전체 덤프에 0건 |
| FLOW-003 | 사용자 설정 보존 | `~/.claude/settings.json` 실행 전후 동일 |
| FLOW-004 | 등록 | 웹 API 등록 코드 → `register --code-stdin` |

## Collector 키체인·트레이 검증 매핑(#41, #43)

위 GUI-006의 "OS 보안 저장소는 미구현", GUI-007의 "Core 로그아웃 명령은 미구현"은 아래 KEY-001~004로 대체합니다.

| 테스트 ID | 요구사항 | 검증 근거 |
|---|---|---|
| KEY-001 | DEV-004·SEC-005 토큰 OS 보안 저장소 | `config/token_test.go`·`cli/logout_test.go`: register 뒤 `config.json`에 토큰 sentinel 없음(`token_store: keyring`), 저장 후 재조회 검증 실패·`config.json` 쓰기 실패 시 항목 정리, 출력·요청 본문에 토큰 없음. 실제 Linux Secret Service(gnome-keyring 42.1, 컨테이너) 저장·읽기·삭제. ADR-0004 |
| KEY-002 | DEV-004 대체 경로·상태 표시 | 세션 D-Bus 없음·저장소 실패·`AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file`이면 0600 파일, `status`·`diagnose` `token_storage: file_fallback`(정상 `os_keyring`), GUI 상태 화면 표시. 잠긴 저장소는 `token_unavailable` 대기 후 회복 |
| KEY-003 | DEV-004 기존 파일 토큰 이전 | `run` 시작 시 legacy `config.json` 토큰을 저장소로 옮기고 파일에서 삭제(다른 필드 보존, 두 번째 실행 무변경, 실패 시 파일 유지). 실제 Secret Service에서도 확인 |
| KEY-004 | DEV-005 로그아웃 | Core `logout`: 기존 `POST /api/v1/devices/revoke`(장치 토큰) → 로컬 항목·`config.json` 삭제, 실패 시 무변경, `--local-only`. 실행 중인 `run`의 heartbeat 중지. GUI 버튼은 승인 필수(`logout_autostart_test.go`), 실제 Core로 서버 폐기 1회·이후 이벤트 미전송(`independence_test.go`) |
| TRAY-001 | COL-001·USER-002 트레이 상태·메뉴 | `tray_test.go`: Core 상태 → 수집 중·일시중지·Core 정지·폐기·토큰 잠김·미등록·응답 없음 표시, 메뉴 동작, 아이콘. Linux 컨테이너(Xvfb·dbus·최소 StatusNotifierWatcher)에서 dbusmenu 클릭으로 일시중지·재개·창 열기·GUI 종료 확인 |
| TRAY-002 | COL-001 창 닫기와 Core 분리 | 트레이가 보일 때만 닫기 = 숨기기, 트레이 종료는 GUI만 종료. openbox `wmctrl -c`로 Watcher 있음(숨김)·없음(종료) 확인 |
| TRAY-003 | COL-001 GUI/Core 독립성 | `independence_test.go`(CI `Go GUI`, 실제 Core): GUI 프로세스 SIGKILL·트레이 종료 뒤 Core 생존·Hook 이벤트 전송, Core SIGKILL → `core_stopped` → 재시작 후 `running` 회복 |
| TRAY-004 | COL-002·DIST-007 GUI 자동 시작 | `autostart_test.go`: XDG `.desktop`·LaunchAgent·HKCU Run 생성, `Exec` 인용, 소유 표시 없는 파일·Run 값·심볼릭 링크 교체·삭제 거부, `--autostart` 숨김 시작 조건. `.desktop`은 `desktop-file-validate` 통과 |
| TRAY-GAP-001 | 미검증 | macOS·Windows 트레이(미구현), macOS Keychain·Windows Credential Manager, 실제 데스크톱 패널·로그인 키링, 실제 로그인·재부팅 뒤 GUI·Core 자동 시작은 미실행(`docs/installation/collector-gui.md`) |

# 보안 위협 시나리오와 검증(#64)

공격자 관점의 교차 시나리오를 통제 수단과 자동 테스트에 연결한다. 테스트는 모두
`apps/server/tests/security/`에 있고 PostgreSQL 통합 테스트로 실행된다(값은 모두 합성).

```sh
# 저장소 루트, PostgreSQL 테스트 DB가 있을 때
AGENT_OFFICE_TEST_DATABASE_URL=postgresql+psycopg://...  python -m pytest apps/server/tests/security
```

경로 검사는 `app.routes`를 직접 펼쳐(`include_router` 안쪽까지) 전수로 돈다. 펼친 목록은 OpenAPI
연산과 같아야 하고, 모든 경로는 공개·로그인 전·등록 코드·장치·WebSocket·사용자·관리자·
SUPER_ADMIN 중 하나로 분류되어야 한다(`secworld.py`). 새 경로가 생기면 분류 표를 고치기 전까지
`SEC-ROUTE-001`이 실패한다.

## 발견해 고친 문제

| 문제 | 영향 | 수정 | 테스트 |
|---|---|---|---|
| ADMIN이 SUPER_ADMIN 계정을 비활성화·재활성화·승인할 수 있었다(마지막 SUPER_ADMIN만 DB 트리거가 보호) | 하위 역할이 상위 관리자를 잠그거나(세션 폐기) SUPER_ADMIN이 비활성화한 계정을 되살림 | `admin_users._managed_user`: 대상이 SUPER_ADMIN이면 SUPER_ADMIN만 상태 변경, 그 밖 `403 forbidden` | SEC-ROLE-003 |
| 문자열 입력의 NUL 문자(`\u0000`)가 PostgreSQL 오류를 일으켜 `500`(팀·프로젝트·장치 이름, 로그인 이메일 등) | 처리되지 않은 오류, 서버 로그에 SQL·매개변수(사용자 입력) 기록 | `sqlalchemy.exc.DataError` 전역 처리기: `400 invalid_input`, 로그에는 예외 종류만 | SEC-INPUT-003 |

## 시나리오 → 통제 → 테스트

| 시나리오 | 통제 | 테스트 ID |
|---|---|---|
| 새 경로가 인증·분류 없이 추가됨 | 경로 전수 목록 = OpenAPI, 분류 표 강제 | SEC-ROUTE-001 |
| 미인증·위조·과대 세션 쿠키로 사용자/관리 경로 호출 | `current_user` 401 | SEC-ROUTE-002 |
| 사용자 B가 A의 장치·에이전트 세션·에이전트·타임라인·프로젝트·팀·구성원·초대·로그인 세션 ID로 모든 경로 호출(IDOR) | 소유·구성원 조건을 쿼리에 포함, 남의 자원은 404 | SEC-TENANT-001 |
| 인스턴스 ADMIN·SUPER_ADMIN이 남의 개인·팀 자원 접근 | 팀 권한은 팀 역할로만, 개인 자원은 소유자만 | SEC-TENANT-002 |
| 목록 경로(내 장치·사무실·통합·팀·프로젝트·세션·연결)로 남의 데이터 열람 | 사용자 범위 쿼리 | SEC-TENANT-003 |
| 내 자원과 남의 자원 섞기(남의 프로젝트를 내 팀에 공유, 내 팀 범위로 남의 에이전트, 내 세션을 남의 프로젝트에), 가로챈 초대 토큰 사용 | 양쪽 소유·구성원 동시 확인, 초대 이메일=계정 이메일 | SEC-TENANT-004 |
| WebSocket 개인·통합에서 남의 데이터, 비구성원의 `?team=` 구독 | 연결 시·매 주기 범위 재계산, 비구성원 4404 | SEC-TENANT-005 |
| 내보낸 구성원이 열린 WS·REST로 계속 열람 | 매 주기 구성원 재확인 | SEC-TENANT-006 |
| 장치 B가 A의 `device_id`·같은 Collector 세션 ID로 이벤트 주입 | 장치는 토큰으로 판정, 세션은 장치별 | SEC-TENANT-007 |
| USER가 관리 경로 호출 | `require_admin` 403 | SEC-ROLE-001 |
| ADMIN의 자기 승격·남 승격·SUPER_ADMIN 강등·설치 완료 잠금 | SUPER_ADMIN 전용 검사 | SEC-ROLE-002 |
| ADMIN이 SUPER_ADMIN 비활성화·재활성화·승인 | 대상 역할 검사(이번에 추가) | SEC-ROLE-003 |
| 팀 MEMBER→MANAGER/OWNER 동작, MANAGER→OWNER 동작, 인스턴스 SUPER_ADMIN인 MEMBER, 초대 수락 본문의 역할 주입 | 팀 역할 표(`docs/api/teams-v1.md`), 닫힌 본문 모델 | SEC-ROLE-004 |
| 쿠키 쓰기 요청 위조(CSRF): 토큰 없음·다른 사용자·같은 사용자의 다른 세션·임의 값 | 세션 유도 CSRF 토큰 헤더 | SEC-CSRF-001, SEC-CSRF-002(정적: 모든 쓰기 경로가 `require_csrf` 의존) |
| 로그인 전 쓰기(가입·로그인·복구·확인·최초 관리자) 위조: 다른 Origin·`null`·폼·multipart | Origin 동일·허용 목록 + JSON 본문만 | SEC-CSRF-003, SEC-CSRF-004 |
| 교차 사이트 WebSocket 하이재킹(개인·팀·통합) | Origin 검사 4403 | SEC-CSRF-005 |
| 쿠키 탈취·교차 사이트 전송 | HttpOnly·SameSite=Lax·Secure·Path=/ | SEC-CSRF-006 |
| 장치 토큰으로 브라우저 경로·WS 호출 | 쿠키 세션만 인정 | SEC-TOKEN-001 |
| 브라우저 세션(쿠키·세션 토큰 Bearer)으로 장치 경로 호출 | `aod_` 장치 토큰만 | SEC-TOKEN-002 |
| 폐기된 장치·회전 유예가 끝난 이전 토큰 사용 | 자격 증명 폐기·만료 조건 | SEC-TOKEN-003 |
| 비활성화·삭제된 계정의 장치 토큰 사용 | 소유자 `active` 조건, 삭제 시 장치 제거 | SEC-TOKEN-004 |
| 본문에 역할·소유자·사용자 ID 주입(대량 할당) | 모든 요청 모델 `extra="forbid"`(정적 전수) | SEC-MASS-001, SEC-MASS-002 |
| 로그아웃·개별 폐기·전체 로그아웃·비밀번호 재설정·관리자 비활성화 뒤 세션 계속 사용 | DB 세션 검증, WS 매 주기 재검증 4401 | SEC-SESSION-001~004 |
| 강등된 관리자가 기존 세션으로 관리 경로 사용 | 매 요청 DB 역할 확인 | SEC-SESSION-005 |
| 비밀·원문 유출: 관리자/사용자 비밀번호, SMTP 암호, OAuth client secret, Webhook URL, Telegram 봇 토큰, bootstrap 토큰, 세션 토큰, 재설정 토큰, 장치 토큰, 초대 토큰, 등록 코드, 원문 필드(`prompt`·`command`·`cwd`·`file_path`·`tool_output`) | 해시·AES-GCM 저장, 쓰기 전용 비밀, 닫힌 이벤트 스키마, 입력 미반사 422, 원인 코드만 남기는 외부 전송 오류 | SEC-LEAK-001(응답 본문·헤더, 모든 DB 테이블, 로그, 알림 메일 전수 검사) |
| 큰 본문·길이 없는 chunked·잘못된 길이 | `BodySizeLimitMiddleware` 413/411/400 | SEC-INPUT-001 |
| 깊은 중첩 JSON(최대 3만 단계, 임의 dict 칸 포함) | JSON 파서·검증 4xx | SEC-INPUT-002 |
| NUL·제어·양방향·폭 0·이모지·HTML·SQL 모양 이름 | 매개변수 바인딩, NUL 400(이번에 추가), JSON 응답 | SEC-INPUT-003 |
| 경로 조작·SQL 모양 경로 ID | UUID·Literal 경로 변수 | SEC-INPUT-004 |
| 이벤트 enum·타입·경계 값 퍼즈(고정 시드 150회) | 닫힌 이벤트 스키마, 미반사 422 | SEC-INPUT-005 |
| 타임라인 커서·페이지·감사 필터 퍼즈 | 커서 해석 400, 범위·패턴 422 | SEC-INPUT-006 |
| SSRF: 알림 Webhook을 내부 주소·다른 스킴·호스트 혼동 주소로 | 제공자별 HTTPS 호스트 허용 목록, 자격 증명 거부 | SEC-INPUT-007 |

## 기존 테스트가 이미 다루는 관련 시나리오

OAuth state·PKCE·로그인 CSRF·자동 연결 금지(`OAUTH-001~008`), bootstrap 토큰 일회성·동시성
(`BOOT-*`), 이벤트 화이트리스트·장치 판정(`ING-*`, `DEVAPI-*`), 백업 암호화·복원 시험(`#58`),
감사 로그 허용 키(`ABU-*`)는 각 기능 테스트에 있다. 이 문서의 테스트는 그 위에 경로 전수·교차
역할·교차 테넌트 관점을 더한다.

## 다루지 않은 것(미검증)

- 인프라 수준 공격: TLS 설정·인증서, 리버스 프록시 헤더 위조(`X-Forwarded-For`, 실제 IP 반영은
  #57), DNS 재바인딩, 호스트 OS·컨테이너 탈출, DB 직접 접근. 실제 HTTPS 환경에서 Secure 쿠키
  동작은 실측하지 않았다.
- 실제 외부 서비스: Discord·Telegram·Teams·SMTP·Google·GitHub 왕복(모두 대역으로 대체).
- 웹 UI XSS: 서버는 이름을 JSON 문자열로만 돌려준다. React 렌더링 이스케이프·CSP는 웹 테스트·
  브라우저 실측 범위이며 여기서 검증하지 않았다.
- 서비스 거부: 속도 제한 값의 적정성, 다중 프로세스 간 제한 공유(#63), 느린 요청(slowloris).
- 타이밍 공격: 로그인 존재 여부 시간 차는 같은 비용 검증으로 설계했지만 통계적으로 측정하지 않았다.
- 정책 판단으로 남긴 것: ADMIN은 다른 ADMIN을 비활성화할 수 있고, SMTP 호스트는 관리자가 임의
  주소(내부 주소 포함)로 정할 수 있다(관리자 신뢰). 운영 Heartbeat URL도 운영자 환경 변수다.
- 백업 파일 접근: API로 백업 파일을 내려받는 경로는 없다(경로 목록 검사로 확인). 백업 디렉터리의
  파일 시스템 권한은 운영 문서(`docs/operations/backup.md`) 범위다.

# 헬스·모니터링·외부 Heartbeat

관련 이슈: #59 (요구사항 OPS-009~012)

| 경로 | 인증 | 내용 |
|---|---|---|
| `GET /api/v1/system/health` | 없음 | 프로세스 생존만(`{"status":"ok"}`). 컨테이너 HEALTHCHECK·프록시 확인용 |
| `GET /api/v1/system/ready` | 없음 | DB 연결과 마이그레이션 버전. 정상 `200 {"ready":true,...}`, 아니면 `503`. 값은 `ok`·`fail`·`outdated`만 |
| `GET /api/v1/admin/metrics` | 관리자 세션 | 운영 지표(아래) |

## 운영 지표

숫자와 상태 코드만 담고 사용자·장치 식별자, 이메일, PC 이름, Heartbeat 주소는 담지 않습니다.

| 묶음 | 항목 |
|---|---|
| `host` | CPU 수, load average, 메모리 전체·가용, 디스크 전체·여유(`AGENT_OFFICE_DISK_PATH`, 기본 `/`) |
| `database` | `SELECT 1` 지연(ms), DB 크기, 마이그레이션 버전 |
| `collection` | 사용자·장치·온라인 장치 수, 열린 세션, 최근 1시간 이벤트 수, 수집 지연 95백분위(발생→서버 수신, 초), 최근 heartbeat의 최대 Collector 큐 깊이, 최근 하루 Collector 버전 분포 |
| `readiness` | 준비 상태와 같은 값 |
| `external_heartbeat` | 설정 여부, 마지막 시각·성공 여부 |
| `retention` | 보존 정리(#39) 마지막 실행 시각·성공 여부 |

컨테이너 안에서 본 디스크·메모리이므로, PostgreSQL 데이터를 다른 디스크(SSD)에 두었다면 그 디스크 여유는 호스트에서 따로 확인합니다.

## 외부 Heartbeat(전원 차단·단절 감지)

서버가 꺼지면 스스로 알릴 수 없으므로, 외부 서비스가 "신호가 끊겼다"를 감지하게 합니다.

1. healthchecks.io·Uptime Kuma(push 모니터)·Better Stack 등에서 push URL을 만듭니다.
2. `deploy/.env.production`에 `AGENT_OFFICE_HEARTBEAT_URL=<push URL>`(필요하면 `AGENT_OFFICE_HEARTBEAT_INTERVAL_SECONDS`, 기본 60, 최소 30)을 넣습니다.
3. 서버는 준비 상태가 정상일 때만 이 URL을 `GET`합니다. DB가 죽었거나 마이그레이션이 맞지 않아도 신호를 멈추므로 외부 서비스가 알림을 보냅니다.

URL은 비밀로 다루며 로그·지표에 남기지 않습니다. 실패는 "external heartbeat failed"만 로그에 남깁니다.

## 검증

- PostgreSQL 통합 테스트: 준비 상태 정상·마이그레이션 불일치 503, 지표 권한·식별자 미포함, 로컬 HTTP 서버로 Heartbeat 성공·실패.
- 실제 외부 서비스(healthchecks.io 등) 연동과 전원 차단 알림은 실측하지 않았습니다.

## 배포 상태·보안 점검(#61)

`GET /api/v1/admin/metrics`에 다음이 함께 나오고 관리 화면 "상태·진단"에 표시됩니다.

- `deployment`: 서버 이미지 버전·리비전(빌드 인자 `VERSION`·`REVISION`), 서버가 기대하는 마이그레이션 버전
- `security`: 설정만 보고 판단한 경고 목록(`code`·`level`·`text`, 값은 담지 않음)

| 코드 | 수준 | 의미 |
|---|---|---|
| `allowed_origins_wildcard` | 긴급 | 허용 Origin에 `*` |
| `cookie_not_secure` | 높음 | 세션 쿠키 Secure 꺼짐 |
| `public_url_http` | 높음 | 접속 주소가 HTTPS가 아님(localhost 제외) |
| `secret_key_missing` · `backup_disabled` · `backup_no_offsite` · `bootstrap_token_pending` | 주의 | 비밀 키 없음, 백업 꺼짐, 외부 복제 없음, 쓰지 않은 bootstrap 토큰 |
| `public_signup_open` · `setup_incomplete` | 정보 | 공개 가입 중(차단 도메인 없음), 설치 완료 전 |

긴급·높음은 관리 화면 맨 위 "확인할 문제"에도 올라갑니다.

## 로그 보존

| 로그 | 보존 |
|---|---|
| 컨테이너 표준 출력(서버·웹·proxy·DB) | Docker `json-file` 서비스마다 10MB × 3개 회전(운영 Compose) |
| 감사 로그(DB) | 기본 365일, 최소 30일(`AGENT_OFFICE_AUDIT_RETENTION_DAYS`, #39) |
| 활동 이벤트·종료 세션 | 30일(#39) |
| 백업 | 로컬 7개, 외부 일 7·주 4(#58) |

서버 로그에는 오류 종류·코드만 남기고 이메일·토큰·주소·경로·원문은 남기지 않습니다. 접근 로그에 메일 토큰이 남지 않도록 토큰은 URL 조각(#)으로 전달합니다.

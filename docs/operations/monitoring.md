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

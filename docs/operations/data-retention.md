# 데이터 보존·삭제

관련 이슈: #39 (요구사항 OPS-015~016, USER-003)

## 보존 기간

| 데이터 | 기준 시각 | 보존 |
|---|---|---|
| 활동 이벤트 | 서버 수신 시각 `received_at`(클라이언트 시각은 위조 가능) | 30일 |
| 종료 세션(에이전트·현재 상태·계획 단계 포함) | `ended_at` | 30일 |
| 열린 세션과 현재 상태 | — | 활동 종료까지 유지(이벤트가 지워져도 남음) |
| 장치 heartbeat | `received_at` | 30일 |
| 이메일 확인·비밀번호 재설정·장치 등록 코드 | 만료·사용 시각 | 1일 뒤 삭제 |
| 로그인 세션 | 만료·폐기 시각 | 7일 뒤 삭제 |
| 팀 초대 | 만료·수락·취소 시각 | 30일 |
| OAuth 시작 상태(#24) | 만료 시각(10분) | 즉시 |
| 감사 로그 | `occurred_at` | 기본 365일(`AGENT_OFFICE_AUDIT_RETENTION_DAYS`, 최소 30일) |

원본 로그(Claude·Codex transcript)는 각 도구의 정책을 따르며 Collector가 지우지 않습니다.

## 실행

- 서버가 기동 5분 뒤부터 한 시간마다 정리합니다. 서버 프로세스가 여러 개여도 PostgreSQL advisory lock으로 한 번에 하나만 실행합니다. 실패하면 오류 종류만 로그에 남기고 다음 주기에 다시 합니다.
- 즉시 실행: `docker compose -f deploy/compose.production.yaml exec server agent-office-retention`(표별 삭제 수만 출력).
- 세션을 지우기 전에 그 세션의 책상 배정을 먼저 비웁니다(배정 쌍 제약).

## 계정 삭제

`DELETE /api/v1/auth/account {confirm_email, password}`(세션 쿠키 + CSRF).

- 이메일을 다시 입력해야 하고, 비밀번호가 있는 계정은 비밀번호도 확인합니다(`400 confirm_email_mismatch`, `401 invalid_credentials`).
- 마지막 활성 `SUPER_ADMIN`은 지울 수 없습니다(`409 last_super_admin`). 다른 구성원이 있는 팀의 소유자는 먼저 소유권을 넘겨야 합니다(`409 owner_must_transfer`).
- 함께 지워지는 것: 장치·자격 증명·세션·이벤트·프로젝트·공유·팀 소속, 혼자인 팀. 감사 로그는 행위자 연결이 끊기고(사용자 ID만 대상에 남음) 보존 기간 뒤 지워집니다.
- 성공하면 세션 쿠키를 지웁니다.

## 백업 복원 후 재삭제

삭제한 계정의 ID만 `deleted_users`에 남깁니다(이메일·이름 없음). 백업을 복원해 삭제 전 계정이 되살아나도, 다음 보존 작업이 이 목록으로 다시 지웁니다. 복원 직후 바로 지우려면 `agent-office-retention`을 실행합니다. 백업 자체의 보존·암호화는 #58입니다.

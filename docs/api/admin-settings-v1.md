# 관리자 인스턴스 설정 API v1

관련 이슈: #67 (선행 #23 계정·#66 최초 관리자, 후속 #24 OAuth 로그인·웹 설정 화면)

`/api/v1/admin/settings` 아래 경로는 `SUPER_ADMIN`·`ADMIN` 세션만 쓸 수 있고, 쓰기 요청은 CSRF 토큰(`X-CSRF-Token`)이 필요합니다. 일반 사용자는 `403 forbidden`, 미로그인은 `401`입니다.

| 요청 | 내용 |
|---|---|
| `GET` | 전체 설정 보기(아래 모양) |
| `PUT /site {service_name, public_url}` | 서비스명과 기준 주소. 주소는 `http(s)://호스트[:포트]`만(경로·쿼리·자격 증명 거부, `400 invalid_public_url`) |
| `PUT /signup-policy {policy}` | `PUBLIC_VERIFIED_EMAIL`·`INVITE_ONLY`·`ADMIN_APPROVAL`·`CLOSED` |
| `PUT /office {hide_idle_after_minutes}` | 활동 없는 세션 숨기기(#245). 정수 0~10080분, 0이면 끔, 기본 60. 범위·형식이 틀리면 `422` |
| `PUT /smtp {host, port, username, sender, starttls, password}` | `password`: 생략·`null`이면 유지, `""`이면 삭제 |
| `DELETE /smtp` | 저장한 SMTP 설정과 암호 삭제(환경 변수 설정으로 돌아감) |
| `POST /smtp/test` | 요청한 관리자 본인 주소로 시험 메일. `{ok, error}`, 오류는 `auth_failed`·`connect_failed`·`timeout`·`tls_failed`·`starttls_unsupported`·`recipient_refused`·`smtp_error`·`not_configured` 코드만 |
| `PUT /oauth/{google\|github} {enabled, client_id, client_secret}` | 켜려면 client ID와 secret이 모두 있어야 함(`400 oauth_incomplete`). 로그인 흐름은 [OAuth API](auth-oauth-v1.md)(#24) |

`GET` 응답:

```json
{
  "site": {"service_name": "Agent Office", "public_url": "https://office.example.com"},
  "signup_policy": "CLOSED",
  "office": {"hide_idle_after_minutes": 60},
  "smtp": {"host": "…", "port": 587, "username": "…", "sender": "…", "starttls": true, "password_set": true},
  "smtp_source": "settings",
  "oauth": {"google": {"enabled": false, "client_id": "", "client_secret_set": false}, "github": {…}},
  "secret_storage_available": true
}
```

## 비밀 저장

- SMTP 암호와 OAuth client secret은 `instance_secrets`에 AES-256-GCM 암호문으로만 저장합니다. 응답에는 `password_set`·`client_secret_set`만 나오고 평문은 다시 보여 주지 않습니다.
- 키는 인프라 비밀 `AGENT_OFFICE_SECRET_KEY`(32바이트 URL-safe base64)입니다. DB와 따로 두므로 DB 백업만으로는 비밀을 읽을 수 없습니다. 생성 예: `python -c "import base64,os;print(base64.urlsafe_b64encode(os.urandom(32)).decode())"`.
- 키가 없거나 형식이 틀리면 비밀 저장 요청은 `503 secret_key_missing`이고 평문으로 저장하지 않습니다. 암호 없는 SMTP(내부 릴레이)는 키 없이 저장할 수 있습니다.
- 암호문은 비밀 이름을 AAD로 묶어 다른 칸으로 옮기면 복호화되지 않습니다. 키를 바꾸면 기존 비밀은 읽을 수 없으므로 다시 입력해야 합니다(키 교체 절차는 #70).

## 우선순위

메일 발송과 메일 링크는 관리 화면 설정을 먼저 쓰고, 없으면 환경 변수(`AGENT_OFFICE_SMTP_*`, `AGENT_OFFICE_PUBLIC_URL`, `docs/api/auth-accounts-v1.md`)를 씁니다. 저장된 SMTP 암호를 복호화할 수 없으면 인증 없이 보내지 않고 발송 불가로 처리합니다.

## 감사

모든 변경은 `settings.*` 감사 로그에 관리자 ID만 남깁니다. 가입 정책·OAuth 공급자 이름 외의 값(주소·호스트·비밀)은 남기지 않습니다.

## 웹 화면

- `/setup` 설치 마법사(`apps/web/src/account/SetupWizard.tsx`): bootstrap 토큰·최초 관리자 → 사이트 이름·접속 주소(기본값은 현재 주소) → 가입 정책 → SMTP 저장·시험 메일(건너뛰기 가능) → 완료 잠금. 관리자가 이미 있으면 로그인 후 이어서 진행하고, 완료 후에는 "설치 완료됨"만 보여 줍니다.
- `/login`·`/signup`·`/verify-email`·`/reset-password` 계정 화면. 메일 링크의 `#token=`은 읽은 즉시 주소창에서 지웁니다.
- 서버 오류 코드는 한국어 문구로 바꿔 보여 주고, SMTP 시험 실패는 원인 코드만 표시합니다. 암호 입력란은 저장 후 비웁니다.
- 운영 웹 이미지 nginx는 화면 경로를 `index.html`로 돌려줍니다(`apps/web/nginx.conf`). `/api`는 리버스 프록시(#57)가 서버로 보냅니다.

## 사용자 관리·감사 로그(#28)

| 요청 | 설명 |
|---|---|
| `GET /api/v1/admin/users?status=&limit=&offset=` | 사용자 목록(이메일 포함, 관리자만), `total`, 이메일 확인 후 승인 대기 수 `pending_approval` |
| `POST /api/v1/admin/users/{id}/approve` | 이메일 확인을 마친 대기 사용자만 승인(`409 not_awaiting_approval`) |
| `POST /api/v1/admin/users/{id}/disable` | 비활성화 + 모든 브라우저 세션 폐기. 자기 계정은 `409 cannot_modify_self` |
| `POST /api/v1/admin/users/{id}/enable` | 이메일 확인됐으면 `active`, 아니면 `pending` |
| `PUT /api/v1/admin/users/{id}/role {instance_role}` | SUPER_ADMIN만. 마지막 활성 SUPER_ADMIN 강등은 DB 트리거가 막아 `409 last_super_admin` |
| (승인·비활성화·재활성화 공통) | 대상이 `SUPER_ADMIN`이면 SUPER_ADMIN만 할 수 있습니다. `ADMIN`은 `403 forbidden`(#64) |
| `PUT /api/v1/admin/settings/blocked-domains {domains}` | 가입 차단 도메인(최대 500개, 형식 검사) |
| `GET /api/v1/admin/audit?limit=&before=&action=` | 최신순 감사 로그, `before` 커서, `action` 접두어 필터. 행위자는 표시 이름만 |

웹 `/admin`에 사용자(승인 대기 맨 위)·감사 로그·차단 도메인 영역이 있습니다.

## 백업 실행(#68)

| 요청 | 설명 |
|---|---|
| `POST /api/v1/admin/backup/run` | 지금 백업. `{ok, error, name, size_bytes}` |
| `POST /api/v1/admin/backup/restore-test` | 최신 백업으로 임시 DB 복원 시험. `{ok, error, name}` |

백업 키가 없으면 `503 backup_key_missing`. 결과는 `backup.status`와 감사 로그(`backup.run`·`backup.restore_test`)에 코드만 남습니다.

## 관리 화면 `/admin`(#68)

관리자(SUPER_ADMIN·ADMIN)만 들어갈 수 있고 사무실 상단에 "관리" 링크가 보입니다.

- **상태·진단**: 확인할 문제 목록(긴급·주의. 예: 준비 안 됨, 디스크 10% 미만, 백업 실패·2일 넘게 없음, 복원 시험 31일 넘게 없음, Heartbeat 없음·실패, 비밀 키 없음, 이메일 확인 정책인데 SMTP 없음, 접속 주소 없음), 호스트·DB·수집 지표, Heartbeat·보존 정리·백업·복원 시험 결과, "지금 백업"·"복원 시험" 버튼
- **인스턴스 설정**: 사이트, 사무실 화면(활동 없는 세션 숨기기, 아래), 가입 정책, SMTP(저장·시험 메일, 암호는 다시 표시하지 않음), OAuth client
- **설정 경계 안내**: 인프라(`.env.production`, 재시작 필요) / 인스턴스(이 화면) / 개인(`/teams`·계정·Collector)

## 활동 없는 세션 숨기기(#245)

세션을 닫지 않고 오래 쉬게 두면 사무실이 붐비므로, 마지막 활동(`last_event_at`) 후 정한 분이 지난 세션은 사무실 지도와 "활성 세션" 목록에서 숨깁니다.

- 저장 위치: `instance_settings`의 `office.hide_idle_after_minutes`(정수). 값이 없거나 잘못되면 기본 60분입니다. 마이그레이션은 없습니다.
- 전달: 개인·팀·통합 사무실 스냅샷에 `settings: {"hide_idle_after_minutes": N}`이 실립니다([사무실 실시간 API](office-live-v1.md)). 증분에는 없습니다.
- 화면: 숨긴 세션의 캐릭터는 퇴근처럼 출입구로 걸어 나가고, 새 활동이 오면 다시 자리로 돌아옵니다. 승인 대기(`WAITING_USER`) 세션은 숨기지 않습니다. 세션을 종료하거나 기록·책상 배정을 바꾸지 않습니다(화면 표시만).
- 반영 시점: 팀·통합 화면은 다음 주기(1초 안)에 새 스냅샷으로, 개인 사무실은 다음 재접속·새로고침 때 반영됩니다.
- 감사: `settings.office`(관리자 ID와 분 값만).

## 알려진 제한

- 알림 채널은 `docs/operations/notifications.md`(#70~#74)를 따릅니다.
- 실제 브라우저에서 화면 조작은 확인하지 않았습니다(헤드리스 브라우저 없음, #63).
- 실제 브라우저에서 화면 흐름은 확인하지 않았습니다(헤드리스 브라우저 없음, #63). 서버 렌더링·단위 테스트만 했습니다.
- 실제 외부 SMTP 서버 발송은 검증하지 않았습니다(테스트는 가짜 SMTP).

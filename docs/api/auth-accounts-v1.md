# 이메일 계정 API v1

관련 이슈: #23 (선행 #25 세션, 후속 #24 OAuth·#27 초대 가입·#66 최초 관리자·#68 메일 설정 화면)

모든 경로는 `/api/v1/auth` 아래에 있습니다. 로그인 전 요청이라 CSRF 토큰이 없으므로 아래 조건으로 교차 사이트 요청을 막습니다.

- `Content-Type: application/json`만 받음(HTML 폼은 이 형식을 보낼 수 없음)
- 브라우저 `Origin`은 요청 `Host`와 같거나 `AGENT_OFFICE_ALLOWED_ORIGINS`에 있어야 함
- 어기면 `403 {"detail": "cross_site_rejected"}`

## 엔드포인트

| 요청 | 성공 | 실패 |
|---|---|---|
| `POST /signup {email, password, display_name}` | `202 {status: "verification_sent"}` | `403 signup_closed`·`invite_required`, `400 invalid_email`·`password_too_short`·`password_too_long`, `503 mail_not_configured`, `429` |
| `POST /verify-email {token}` | `204` | `400 invalid_token`, `429` |
| `POST /login {email, password}` | `200 {user, csrf_token, expires_at}` + 세션 쿠키 | `401 invalid_credentials`, `403 email_not_verified`·`approval_pending`, `429` |
| `POST /password-reset/request {email}` | `202 {status: "reset_sent_if_exists"}` | `503 mail_not_configured`, `429` |
| `POST /password-reset/confirm {token, password}` | `204`, 모든 브라우저 세션 폐기 | `400 invalid_token`·비밀번호 정책 코드, `429` |

## 계정 존재 여부 숨김

- 이미 있는 주소로 가입하면 응답은 같고, 그 주소로만 토큰 없는 안내 메일을 보냅니다. 비밀번호 해시는 두 경우 모두 계산합니다.
- 로그인 실패는 없는 계정·틀린 비밀번호·비활성 계정 모두 `401 invalid_credentials`입니다. 없는 계정도 더미 해시로 같은 비용을 들여 검증합니다.
- `email_not_verified`·`approval_pending`은 비밀번호가 맞았을 때만 나옵니다.
- 복구 요청은 계정 유무와 관계없이 같은 응답입니다.

## 가입 정책

`instance_settings`의 `auth.signup_policy` 값입니다. 설치 마법사(#67)가 정하기 전 기본값은 `CLOSED`입니다.

| 정책 | 동작 |
|---|---|
| `PUBLIC_VERIFIED_EMAIL` | 가입 → 이메일 확인 → 활성 |
| `ADMIN_APPROVAL` | 가입 → 이메일 확인 → 관리자 승인 전까지 `pending`(승인 화면은 EP-11) |
| `INVITE_ONLY` | 공개 가입 거부. 초대 수락 가입은 #27 |
| `CLOSED` | 가입 거부 |

## 비밀번호와 토큰

- Argon2id(19MiB·2회·병렬 1, OWASP 최소 권장). 로그인 시 매개변수가 바뀌었으면 다시 해시합니다.
- 길이 10~128자만 검사합니다(NIST SP 800-63B, 조합 규칙 없음).
- 이메일 확인 토큰 24시간, 복구 토큰 1시간. DB에는 SHA-256 해시만 두고 소비는 원자적 `UPDATE … RETURNING`으로 한 번만 성공합니다. 새 토큰을 발급하면 같은 사용자의 이전 미사용 토큰은 지웁니다.
- 메일 링크는 `{AGENT_OFFICE_PUBLIC_URL}/verify-email#token=…` 형식입니다. 토큰을 URL 조각에 두어 서버 접근 로그·Referer에 남지 않게 합니다.
- 복구 완료 시 비밀번호를 바꾸고 이메일 확인 처리(주소 소유 증명)를 하며 모든 브라우저 세션을 폐기합니다. 장치 토큰은 유지합니다.

## 메일 설정(SMTP)

관리 화면 설정(`docs/api/admin-settings-v1.md`)이 있으면 그것을 먼저 쓰고, 없으면 아래 환경 변수를 씁니다.

| 환경 변수 | 기본 | 설명 |
|---|---|---|
| `AGENT_OFFICE_PUBLIC_URL` | 없음 | 메일 링크의 기준 주소 |
| `AGENT_OFFICE_SMTP_HOST`·`AGENT_OFFICE_SMTP_FROM` | 없음 | 둘 다 있어야 발송 |
| `AGENT_OFFICE_SMTP_PORT` | `587` | |
| `AGENT_OFFICE_SMTP_USERNAME`·`AGENT_OFFICE_SMTP_PASSWORD` | 없음 | 비밀번호는 저장소·로그에 두지 않음 |
| `AGENT_OFFICE_SMTP_STARTTLS` | `true` | |

SMTP나 기준 주소가 없으면 가입·복구 요청은 `503 mail_not_configured`이고 계정을 만들지 않습니다(확인할 수 없는 계정 방지). 발송 실패는 오류 종류만 로그에 남기고 주소·토큰은 남기지 않습니다.

## 속도 제한

메모리 고정 창, 단일 서버 프로세스 기준입니다.

| 대상 | 한도 |
|---|---|
| 가입 | IP당 시간당 5회 |
| 이메일 확인·복구 확정 | IP당 시간당 20회 |
| 로그인 | IP당 10분 30회, 이메일당 15분 10회 |
| 복구 요청 | IP당 시간당 10회, 이메일당 시간당 3회 |

## 알려진 제한

- 리버스 프록시 뒤에서는 모든 요청이 같은 IP로 보입니다. 신뢰 프록시의 `X-Forwarded-For` 반영은 #57에서 합니다. 이메일별 한도는 그와 관계없이 동작합니다.
- 실제 SMTP 서버 발송은 검증하지 않았습니다(테스트는 가짜 발송기). 웹 로그인·가입 화면은 별도 이슈입니다.
- 감사 로그에는 사용자 ID와 결과만 남고 이메일·비밀번호·토큰은 남지 않습니다.

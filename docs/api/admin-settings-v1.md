# 관리자 인스턴스 설정 API v1

관련 이슈: #67 (선행 #23 계정·#66 최초 관리자, 후속 #24 OAuth 로그인·웹 설정 화면)

`/api/v1/admin/settings` 아래 경로는 `SUPER_ADMIN`·`ADMIN` 세션만 쓸 수 있고, 쓰기 요청은 CSRF 토큰(`X-CSRF-Token`)이 필요합니다. 일반 사용자는 `403 forbidden`, 미로그인은 `401`입니다.

| 요청 | 내용 |
|---|---|
| `GET` | 전체 설정 보기(아래 모양) |
| `PUT /site {service_name, public_url}` | 서비스명과 기준 주소. 주소는 `http(s)://호스트[:포트]`만(경로·쿼리·자격 증명 거부, `400 invalid_public_url`) |
| `PUT /signup-policy {policy}` | `PUBLIC_VERIFIED_EMAIL`·`INVITE_ONLY`·`ADMIN_APPROVAL`·`CLOSED` |
| `PUT /smtp {host, port, username, sender, starttls, password}` | `password`: 생략·`null`이면 유지, `""`이면 삭제 |
| `DELETE /smtp` | 저장한 SMTP 설정과 암호 삭제(환경 변수 설정으로 돌아감) |
| `POST /smtp/test` | 요청한 관리자 본인 주소로 시험 메일. `{ok, error}`, 오류는 `auth_failed`·`connect_failed`·`timeout`·`tls_failed`·`starttls_unsupported`·`recipient_refused`·`smtp_error`·`not_configured` 코드만 |
| `PUT /oauth/{google\|github} {enabled, client_id, client_secret}` | 켜려면 client ID와 secret이 모두 있어야 함(`400 oauth_incomplete`). 로그인 흐름은 #24 |

`GET` 응답:

```json
{
  "site": {"service_name": "Agent Office", "public_url": "https://office.example.com"},
  "signup_policy": "CLOSED",
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

## 알려진 제한

- 웹 설정 화면(설치 마법사)은 후속 PR입니다.
- 실제 외부 SMTP 서버 발송은 검증하지 않았습니다(테스트는 가짜 SMTP).

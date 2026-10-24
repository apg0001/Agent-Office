# Google/GitHub OAuth API v1

관련 이슈: #24 (선행 #23 이메일 계정, #25 세션, #27 초대, #67 관리자 OAuth 설정)

OAuth는 선택 기능입니다. 관리자가 제공자를 켜고 client ID·client secret을 모두 저장했고 공개 주소(`site.public_url` 또는 `AGENT_OFFICE_PUBLIC_URL`)가 있을 때만 버튼과 시작 경로가 동작합니다. 설정이 없으면 웹에 버튼이 보이지 않고 시작 경로는 `#oauth=provider_disabled`로 돌려보냅니다.

검증 상태: 가짜 제공자(HTTP 함수 대체)로 자동 테스트만 했습니다. **실제 Google·GitHub OAuth 앱으로 끝까지 로그인해 본 검증은 아직 하지 않았습니다.** 실제 앱 검증 전에는 지원으로 표시하지 않습니다.

## 엔드포인트

모든 경로는 `/api/v1/auth` 아래에 있습니다.

| 요청 | 결과 |
|---|---|
| `GET /oauth/providers` | `200 {providers: ["google", "github"]}` — 설정이 끝난 제공자 이름만. client ID·secret은 내보내지 않음 |
| `GET /oauth/{provider}/start?intent=login\|link[&invite=<초대 토큰>]` | `302` 제공자 인가 화면 + `ao_oauth` 쿠키. 실패 시 `302` 웹 주소 `#oauth=<코드>` |
| `GET /oauth/{provider}/callback?code&state` | `302` 웹 주소 `#oauth=<코드>`. 로그인·가입 성공이면 세션 쿠키 발급 |
| `GET /identities` | `200 {password_set, identities: [{provider, linked_at}]}` (로그인 필요) |
| `DELETE /identities/{provider}` | `204`. `403 csrf_required`, `404 not_found`, `409 last_login_method` (로그인·CSRF 필요) |

`provider`는 `google` 또는 `github`입니다.

## 보안 설계

- **state**: 32바이트 무작위 값. DB(`oauth_states`, 마이그레이션 `0007`)에는 SHA-256 해시만 두고 10분 뒤 만료, 콜백에서 한 번 쓰면 바로 지웁니다. 만료 행은 보존 작업(#39)이 지웁니다.
- **로그인 CSRF 방지(브라우저 결속)**: 시작한 브라우저에만 `ao_oauth` 쿠키(HttpOnly, `Path=/api/v1/auth/oauth`, SameSite=Lax, 기본 Secure, 10분)를 줍니다. 쿠키 값은 PKCE `code_verifier`이고 DB에는 그 해시만 둡니다. 콜백은 state 행과 쿠키 해시가 맞아야 진행합니다. 공격자가 자기 인가 코드·state를 피해자 브라우저로 보내도 쿠키가 없어 `state_invalid`입니다.
- **PKCE(S256)**: Google·GitHub 모두 `code_challenge`를 보내고 토큰 교환 때 `code_verifier`를 보냅니다.
- **요청 범위**: Google `openid email`, GitHub `user:email`. 저장소 권한은 요청하지 않습니다.
- **저장하는 값**: `auth_identities`에 제공자와 불변 subject(Google `sub`, GitHub 숫자 `id`)만 둡니다. 액세스 토큰은 신원 확인 직후 버리고 저장·기록하지 않습니다. 프로필 이름·사진·로그인 이름은 저장하지 않습니다. 신규 가입이면 제공자가 검증한 이메일을 계정 이메일로 쓰고, 표시 이름은 이메일 앞부분으로 시작합니다.
- **이메일 검증**: Google은 `userinfo`의 `email_verified=true`, GitHub는 `/user/emails`의 `primary`이면서 `verified`인 주소만 씁니다. 검증된 주소가 없으면 신규 가입을 거부합니다(`email_unverified`).
- **자동 연결 금지**: 같은 이메일의 기존 계정이 있어도 자동으로 연결하지 않습니다(`account_exists_link_required`). 제공자 쪽 이메일 검증만 믿고 연결하면 계정 탈취 위험이 있으므로, 기존 방법으로 로그인한 뒤 계정 화면(`/account`)에서 연결합니다. 이 코드는 해당 이메일을 검증한 제공자 계정 소유자에게만 보입니다.
- **결과 전달**: 웹 주소 조각(`#oauth=<코드>`)으로만 알립니다. 토큰·이메일은 URL에 넣지 않고 응답에 `Cache-Control: no-store`, `Referrer-Policy: no-referrer`를 붙입니다.
- **로그·감사**: 제공자 응답 본문, 토큰, client secret, 이메일을 로그에 남기지 않습니다(상태 코드·오류 종류만). 감사 로그 `auth.oauth`(실패, `reason=<제공자>:<코드>`), `auth.oauth.login`·`auth.oauth.signup`·`auth.oauth.linked`·`auth.oauth.unlinked`(성공, `reason=<제공자>`).
- **속도 제한**: 시작·콜백 각각 IP당 10분 30회.
- 남은 위험: 콜백 URL의 `code`·`state`는 제공자가 쿼리로 보내므로 리버스 프록시 접근 로그에 남을 수 있습니다. 코드는 일회용·짧은 만료이고 PKCE verifier(쿠키) 없이는 교환할 수 없습니다. 초대 토큰을 `invite`로 넘기면 같은 이유로 접근 로그에 남을 수 있으나, 초대 이메일과 제공자 검증 이메일이 같아야만 가입됩니다.

## 콜백 규칙

1. state·쿠키 검증 실패 → `state_invalid`. 사용자가 취소 → `denied`. 토큰 교환 실패 → `exchange_failed`, 신원 조회 실패 → `identity_failed`, 연결 실패 → `provider_unreachable`.
2. **이미 연결된 신원**: 그 사용자로 로그인합니다. `disabled` → `login_failed`, 승인 대기 → `approval_pending`, 활성 → 세션 발급 후 `/#oauth=signed_in`.
3. **계정 연결(`intent=link`)**: 시작할 때 로그인한 사용자와 콜백 때 로그인한 사용자가 같아야 합니다(아니면 `login_required`). 이미 본인 신원 → `already_linked`, 다른 사용자의 신원 → `identity_in_use`, 같은 제공자의 다른 신원이 이미 있음 → `provider_already_linked`, 성공 → `/account#oauth=linked`. 로그인한 본인이 연결하므로 제공자 이메일이 계정 이메일과 달라도 됩니다.
4. **신규 계정(`intent=login`)**: 같은 이메일 계정이 있으면 `account_exists_link_required`. 그 밖에는 가입 정책(#23)을 따릅니다.
   - `CLOSED` → `signup_closed`
   - `INVITE_ONLY` → 시작 때 `invite`로 넘긴 열린 초대가 있고 초대 이메일이 제공자 검증 이메일과 같아야 함, 아니면 `invite_required`(잘못된 초대 토큰은 시작 단계에서 `invalid_invite`). 팀 합류는 기존처럼 로그인 후 초대 수락에서 합니다.
   - `ADMIN_APPROVAL` → 대기(`pending`) 계정 생성, 세션 없음, `approval_pending`
   - `PUBLIC_VERIFIED_EMAIL` → 이메일이 제공자에서 검증됐으므로 바로 활성, `/#oauth=signed_up`
   - 차단 도메인(#28) → `email_domain_blocked`
5. 실패 결과는 `/login#oauth=<코드>`(로그인) 또는 `/account#oauth=<코드>`(연결)로 돌아갑니다.

## 연결 해제

`DELETE /identities/{provider}`는 CSRF 헤더가 필요합니다. 비밀번호가 없는 계정(OAuth로만 가입)의 마지막 로그인 수단은 지울 수 없습니다(`409 last_login_method`). 동시 해제를 막기 위해 사용자 행을 잠그고 확인합니다. OAuth로 가입한 사용자가 비밀번호를 만들려면 비밀번호 재설정(#23)을 쓰면 됩니다.

## 설정 방법

먼저 관리 화면의 사이트 설정에서 공개 주소(예: `https://office.example.com`)를 저장하고, 서버에 `AGENT_OFFICE_SECRET_KEY`가 있어야 client secret을 저장할 수 있습니다(#67). 콜백 주소는 다음 형식입니다.

```text
{공개 주소}/api/v1/auth/oauth/google/callback
{공개 주소}/api/v1/auth/oauth/github/callback
```

### Google Cloud Console

1. [Google Cloud Console](https://console.cloud.google.com/)에서 프로젝트를 고르거나 만듭니다.
2. **API 및 서비스 → OAuth 동의 화면**(Google Auth Platform)에서 앱 이름·지원 이메일을 정하고 범위는 `openid`, `email`만 추가합니다. 외부 사용자에게 열 때는 게시 상태와 검토 요건을 확인합니다.
3. **사용자 인증 정보 → 사용자 인증 정보 만들기 → OAuth 클라이언트 ID**, 애플리케이션 유형 **웹 애플리케이션**을 고릅니다.
4. **승인된 리디렉션 URI**에 `{공개 주소}/api/v1/auth/oauth/google/callback`을 정확히 넣습니다(https, 끝 슬래시 없음).
5. 발급된 클라이언트 ID와 클라이언트 보안 비밀을 Agent Office 관리 화면 OAuth 설정의 Google 항목에 넣고 켭니다.

### GitHub OAuth App

1. GitHub **Settings → Developer settings → OAuth Apps → New OAuth App**(조직이면 조직 설정의 같은 메뉴)으로 갑니다.
2. **Homepage URL**에 공개 주소, **Authorization callback URL**에 `{공개 주소}/api/v1/auth/oauth/github/callback`을 넣습니다. Device Flow는 켜지 않습니다.
3. 앱을 만든 뒤 **Client ID**를 복사하고 **Generate a new client secret**으로 비밀을 만듭니다.
4. 두 값을 관리 화면 OAuth 설정의 GitHub 항목에 넣고 켭니다.

GitHub App이 아니라 OAuth App 기준입니다. GitHub 이메일이 비공개여도 `user:email` 범위로 검증된 기본 주소를 읽습니다.

## 웹

- 로그인·가입 화면: 설정된 제공자 버튼(`Google(으)로 로그인` 등). 초대 링크로 들어온 탭이면 초대 토큰을 시작 주소에 붙입니다.
- `/account`: 비밀번호 설정 여부, 제공자별 연결·해제 버튼과 결과 문구.
- 결과 코드는 화면에서 읽은 뒤 주소창에서 지웁니다.

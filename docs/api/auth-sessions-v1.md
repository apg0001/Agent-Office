# 브라우저 세션·CSRF·로그아웃 API v1

관련 이슈: #25

브라우저 로그인 세션의 유지·보호·종료를 다룹니다. 세션 **발급**은 로그인 수단이 `agent_office.auth.sessions.issue_session`으로 합니다(#23 이메일·비밀번호, #24 OAuth). 아직 로그인 엔드포인트는 없습니다.

## 세션 쿠키

| 속성 | 값 |
|---|---|
| 이름 | `ao_session` |
| 값 | 32바이트 무작위 토큰(URL-safe). 서버는 SHA-256 해시만 저장 |
| `HttpOnly` | 항상 |
| `Secure` | 기본 켬. 로컬 HTTP 개발에서만 `AGENT_OFFICE_COOKIE_SECURE=false` |
| `SameSite` | `Lax`(외부 링크로 들어올 때 세션 유지, 교차 사이트 쓰기 요청에는 쿠키 미전송) |
| `Path` | `/` |
| `Max-Age` | 30일(절대 만료와 같음) |

## 수명과 폐기

- 절대 만료 30일, 유휴 만료 7일. 마지막 사용 시각은 5분 이상 지났을 때만 갱신해 요청마다 DB에 쓰지 않습니다.
- 로그인할 때 이전 쿠키 세션을 폐기합니다(세션 고정 방지).
- 사용자당 활성 세션은 20개까지이며, 넘으면 가장 오래 쓰지 않은 세션부터 폐기합니다.
- 사용자가 `active`가 아니면 모든 세션이 즉시 무효입니다.
- 로그아웃·전체 로그아웃은 브라우저 세션만 끝냅니다. Collector 장치 토큰은 [장치 API](devices-v1.md)로 따로 폐기합니다.

## CSRF

쿠키로 인증하는 쓰기 요청(`POST` 등)은 `X-CSRF-Token` 헤더에 CSRF 토큰을 보내야 합니다. 없거나 틀리면 `403 csrf_required`입니다.

- 토큰은 `GET /api/v1/auth/session` 응답의 `csrf_token`입니다.
- 토큰은 세션 토큰에서 SHA-256으로 유도합니다(`agent-office-csrf:` 접두사). 세션 쿠키가 HttpOnly라 다른 사이트나 스크립트가 세션 토큰을 읽을 수 없으므로 토큰을 위조할 수 없습니다. 서버는 별도 저장 없이 쿠키로 다시 계산해 상수 시간으로 비교합니다.
- 다른 세션의 CSRF 토큰은 통하지 않습니다. 서버는 CORS를 열지 않습니다.
- #26에서 임시로 쓰던 `X-Agent-Office-Request: 1` 헤더는 이 토큰으로 대체됐습니다.

## 엔드포인트

| 메서드·경로 | 인증 | 응답 |
|---|---|---|
| `GET /api/v1/auth/session` | 세션 | `200 {user: {id, display_name, instance_role}, csrf_token, expires_at}` |
| `POST /api/v1/auth/logout` | 세션 + CSRF | `204`. 현재 세션 폐기, 쿠키 삭제(`Max-Age=0`) |
| `POST /api/v1/auth/logout-all` | 세션 + CSRF | `204`. 자기 모든 브라우저 세션 폐기, 쿠키 삭제 |
| `GET /api/v1/auth/sessions` | 세션 | `200 [{id, created_at, last_seen_at, expires_at, current}]`. 자기 활성 세션만 |
| `POST /api/v1/auth/sessions/{id}/revoke` | 세션 + CSRF | `204`. 다른 사용자의 세션은 `404 not_found`. 현재 세션이면 쿠키도 삭제 |

감사 로그: `auth.session.created`, `auth.logout`, `auth.logout_all`, `auth.session.revoked`. 토큰 원문은 기록하지 않습니다.

## 알려진 제한

- 세션 목록에 브라우저·IP 정보를 남기지 않습니다(개인정보 최소화). 기기 구분이 필요하면 별도 심사 후 추가합니다.
- 로그인 시도 속도 제한과 계정 잠금은 #23·#28에서 다룹니다.

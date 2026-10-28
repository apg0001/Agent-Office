# 원격 접속과 HTTPS

관련 이슈: #57 (선행 #56 이미지·Compose)

운영 Compose(`deploy/compose.production.yaml`)는 `proxy`(Caddy) 하나만 호스트 포트를 엽니다. 브라우저는 이 주소 하나로 웹 화면과 `/api`(HTTP·WebSocket)를 같은 출처로 씁니다. 서버·웹·DB 컨테이너는 호스트 포트가 없습니다.

```text
브라우저 ─▶ (Tailscale / Cloudflare Tunnel / 도메인 HTTPS / 루프백) ─▶ proxy:8080 ─┬─▶ /api/* → server:8000
                                                                                   └─▶ 그 밖   → web:8080
```

## 지원 수준

| 방식 | 권장 | 검증 상태 |
|---|---|---|
| 루프백(같은 PC) | 개발·점검 | **검증**(Pi Linux arm64, 로컬 빌드 이미지): proxy 경유 health 200·웹 200·`/setup` SPA 경로, 보안 헤더, 서버가 실제 클라이언트 IP를 보고 위조 `X-Forwarded-For`는 무시, WebSocket 업그레이드 전달, 운영 컨테이너 안 `agent-office-bootstrap` 실행 |
| Tailscale(사설망) | **기본 권장** | 미검증(절차 문서만) |
| Cloudflare Tunnel(공개) | 선택 | 미검증(절차 문서만) |
| 도메인 + Caddy 자동 HTTPS | 선택 | 미검증(공인 IP·포트 포워딩 필요) |
| LAN HTTP 직접 | 비권장 | 미검증 |

## 공통 설정(`deploy/.env.production`)

| 변수 | 설명 |
|---|---|
| `AGENT_OFFICE_BIND_ADDRESS` | proxy 바인딩 주소. 기본 `127.0.0.1` |
| `AGENT_OFFICE_HTTP_PORT`·`AGENT_OFFICE_HTTPS_PORT` | 호스트 포트. 기본 8080·8443 |
| `AGENT_OFFICE_SITE_ADDRESS` | 도메인 자동 HTTPS를 쓸 때만 도메인. 기본 `:8080`(HTTP) |
| `AGENT_OFFICE_PUBLIC_URL` | 사용자가 실제로 여는 주소(메일 링크). 설치 마법사에서 저장한 값이 우선 |
| `AGENT_OFFICE_ALLOWED_ORIGINS` | 접속 주소와 Host가 다른 특수한 앞단에서만 |
| `AGENT_OFFICE_COOKIE_SECURE` | 기본 `true`. HTTPS 없는 LAN 직접 접속에서만 `false` |

설치 후 첫 접속은 `/setup`이고, 토큰은 `docker compose -f deploy/compose.production.yaml exec server agent-office-bootstrap`으로 받습니다(`docs/api/setup-v1.md`).

## Tailscale(권장)

사설 tailnet 안에서만 접속하고 HTTPS 인증서는 Tailscale이 발급합니다. 공개 인터넷에 포트를 열지 않습니다.

```sh
# 호스트에서(Compose 기본값 그대로 루프백 8080)
sudo tailscale serve --bg --https=443 http://127.0.0.1:8080
# 접속 주소: https://<호스트 이름>.<tailnet>.ts.net
```

`AGENT_OFFICE_PUBLIC_URL`(또는 설치 마법사의 접속 주소)을 위 주소로 둡니다. Tailscale serve는 원래 Host를 넘기므로 Origin 검사가 그대로 동작합니다. 팀원을 tailnet에 초대하거나 노드 공유로 접근 범위를 정합니다.

### Tailscale Funnel(공개, Tailscale 없이 접속)

팀원·팀원 PC가 Tailscale을 쓰지 않고 접속하게 하려면 같은 주소를 인터넷에 공개합니다(2026-10-28 Pi 실측: 공개 DNS로 health 200).

```sh
sudo tailscale funnel --bg http://127.0.0.1:8080
# 끄기: sudo tailscale funnel --https=443 off
```

- 처음이면 tailnet 관리 화면에서 Funnel 허용 링크가 뜹니다. 주소는 Serve와 같아서 이미 등록한 Collector는 그대로 동작합니다.
- 공개하기 전에 관리 화면의 가입 정책을 **"초대만"** 또는 **"관리자 승인"** 으로 두세요.
- **실제 클라이언트 IP:** Tailscale은 실제 IP를 `X-Forwarded-For`로 넘깁니다. Caddy는 기본으로 루프백에서 온 값만 믿으므로, 같은 호스트의 Tailscale이 Docker 게이트웨이를 거쳐 들어오는 이 구성에서는 `deploy/.env.production`에 아래를 더하고 proxy를 다시 시작합니다. 그러지 않으면 모든 외부 사용자가 같은 IP로 보여 로그인 시도 제한을 함께 쓰게 됩니다(#209).

  ```sh
  AGENT_OFFICE_TRUSTED_PROXIES=127.0.0.1/32 172.16.0.0/12
  ```

  `AGENT_OFFICE_BIND_ADDRESS`가 `127.0.0.1`일 때만 넣습니다. LAN에 직접 열었으면(0.0.0.0) LAN 기기도 Docker 게이트웨이로 보이므로 넣지 마세요(위조 IP를 믿게 됨).

## Cloudflare Tunnel(선택, 공개)

인바운드 포트 없이 공개 도메인으로 접속합니다. 인터넷에 공개되므로 가입 정책은 `INVITE_ONLY`·`ADMIN_APPROVAL`·`CLOSED` 중에서 고르고, 가능하면 Cloudflare Access로 앞단 인증을 겁니다.

```sh
cloudflared tunnel create agent-office
cloudflared tunnel route dns agent-office office.example.com
# config.yml의 ingress: - hostname: office.example.com  service: http://127.0.0.1:8080
cloudflared tunnel run agent-office
```

- WebSocket은 Cloudflare Tunnel이 그대로 전달합니다.
- 이 구성에서 Caddy가 보는 클라이언트는 cloudflared(루프백)입니다. 실제 IP로 속도 제한을 하려면 Caddyfile 전역 옵션에 `servers { trusted_proxies static 127.0.0.1/32 ::1  client_ip_headers CF-Connecting-IP }`를 추가해야 합니다. 이 설정은 아직 검증하지 않았습니다. 설정하지 않으면 모든 요청이 한 IP로 보이므로 IP별 한도가 함께 소진되고, 이메일별 한도만 개별로 동작합니다.

## 도메인 + Caddy 자동 HTTPS(선택)

공인 IP와 라우터 포트 포워딩(80→8080, 443→8443)이 있을 때만 씁니다.

```sh
AGENT_OFFICE_SITE_ADDRESS=office.example.com
AGENT_OFFICE_BIND_ADDRESS=0.0.0.0
```

Caddy가 Let's Encrypt 인증서를 받아 `caddy-data` 볼륨에 저장하고 HTTP를 HTTPS로 넘깁니다. 인증서 발급은 외부에서 80번 포트로 접근 가능해야 합니다.

## Nginx를 직접 쓸 때

같은 경로 분기만 지키면 됩니다. `/api/`는 서버로, WebSocket은 `proxy_http_version 1.1`과 `Upgrade`·`Connection` 헤더를 넘기고, `Host`·`X-Forwarded-For`·`X-Forwarded-Proto`를 설정합니다. 이 경우 서버 컨테이너를 Nginx만 접근할 수 있는 네트워크에 두어야 `FORWARDED_ALLOW_IPS="*"`가 안전합니다.

## 위협 모델

| 위협 | 대응 |
|---|---|
| 첫 접속자가 관리자를 차지 | 콘솔 일회용 bootstrap 토큰(#66) |
| 교차 사이트 요청·WebSocket 하이재킹 | SameSite=Lax·Secure·HttpOnly 쿠키, CSRF 토큰, 로그인 전 요청과 WebSocket의 Origin 검사 |
| 클라이언트 IP 위조로 속도 제한 우회 | Caddy가 신뢰하지 않는 클라이언트의 `X-Forwarded-For`를 덮어씀, 서버는 호스트 포트가 없어 proxy만 접근 |
| 메일 링크 토큰 유출 | 토큰은 URL 조각(#)으로 전달되어 접근 로그·Referer에 남지 않음, `Referrer-Policy: no-referrer` |
| 클릭재킹 | `X-Frame-Options: DENY` |
| 공개 인터넷 노출 | 기본 루프백 바인딩, 사설망(Tailscale) 권장, 공개 시 가입 정책 제한·Cloudflare Access |
| 인증서·평문 HTTP | HTTPS는 앞단(Tailscale·Cloudflare·Caddy)이 담당, Secure 쿠키 기본 |

## 알려진 제한

- Caddy 컨테이너는 이미지 기본 사용자(root)로 실행하되 `NET_BIND_SERVICE` 외 모든 capability를 버리고 읽기 전용 루트 파일시스템으로 둡니다. 이미지의 실행 파일에 이 파일 권한이 붙어 있어 빼면 실행이 거부됩니다(실측). 컨테이너 안에서는 1024 이상 포트(8080·8443)만 씁니다.
- 운영 Compose는 마이그레이션을 자동 실행하지 않습니다. 첫 기동 후 `docker compose -f deploy/compose.production.yaml exec server alembic -c /app/alembic.ini upgrade head`를 실행합니다.
- Tailscale·Cloudflare·도메인 HTTPS 실측은 하지 않았습니다. 외부 계정과 공인 IP가 필요합니다.

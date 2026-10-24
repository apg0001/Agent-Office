# 설치 방식: 공식 이미지와 소스 빌드

관련 이슈: #69

두 방식 모두 같은 운영 Compose(`deploy/compose.production.yaml`)를 씁니다. 그래서 프록시(Caddy)·보안 설정(읽기 전용 루트, capability 제거, 루프백 바인딩)·볼륨·환경 변수·설치 마법사가 같습니다. 다른 것은 이미지를 어디서 가져오느냐뿐입니다.

| | 공식 이미지 | 소스 빌드 |
|---|---|---|
| 이미지 | `deploy/.env.production`의 `AGENT_OFFICE_SERVER_IMAGE`·`AGENT_OFFICE_WEB_IMAGE`(`tag@digest`) | 이 저장소의 Dockerfile로 빌드(`deploy/compose.build.yaml`) |
| 명령 | `docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml up -d --wait` | `docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml -f deploy/compose.build.yaml up -d --build --wait` |
| 업그레이드 | `deploy/upgrade.sh`(#60) | `git pull` 후 같은 빌드 명령, 또는 빌드한 이미지 이름으로 `deploy/upgrade.sh` |

소스 빌드에서는 `AGENT_OFFICE_SERVER_IMAGE=agent-office-server:source`처럼 빌드 결과 이름을 env 파일에 적습니다. 공통 후속 단계:

```sh
C="docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml"
$C exec server alembic -c /app/alembic.ini upgrade head
$C exec server agent-office-bootstrap      # 일회용 토큰 → 브라우저에서 /setup
```

`deploy/compose.yaml`(개발용)은 Vite·직접 포트를 쓰는 **개발 전용** 구성입니다. 프록시·보안 설정이 없으므로 서비스 운영에 쓰지 않습니다.

## 동일성 검증

`tests/install/compare.sh <서버 이미지> <웹 이미지>`가 두 방식을 같은 호스트에 각각 새 볼륨으로 설치하고, `tests/install/probe.py`로 바깥에서 본 동작을 비교합니다(토큰·쿠키 값은 출력하지 않음).

비교 항목: 준비 상태, 웹 `/`, SPA 경로(`/setup`·`/login`·`/admin`), 보안 헤더와 `Server` 헤더 제거, 설치 상태(전·후), 기본 가입 차단(`signup_closed`), 교차 사이트 로그인 거부, 잘못된 bootstrap 토큰 거부, 토큰으로 최초 SUPER_ADMIN 생성, 세션 쿠키 속성(HttpOnly·Secure·SameSite=Lax·Path=/), 관리자 지표·보안 점검 코드, CSRF 없는 쓰기 거부, 설치 완료.

**실측(Pi 5 arm64, USB SSD, 2026-10-24)**: 같은 Dockerfile로 만든 "공식 이미지 역할" 이미지와 `compose.build.yaml` 소스 빌드가 모든 항목에서 같았습니다(약 70초).

## 한계

- GHCR에 실제로 게시된 이미지로는 아직 비교하지 않았습니다(게시는 관리자 승인 후). 게시되면 `compare.sh ghcr.io/...@sha256:... ghcr.io/...@sha256:...`로 같은 검사를 합니다.
- amd64 클린 VM, NAS 등 다른 호스트는 미검증입니다.
- Docker 없이 Python·Node를 직접 설치하는 방식은 개발용(`docs/installation/development.md`)이며 운영 지원 대상이 아닙니다.

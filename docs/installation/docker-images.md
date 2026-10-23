# Docker 이미지와 Compose

관련 이슈: #56

## 이미지 구성

| 이미지 | Dockerfile / 컨텍스트 | 베이스(멀티아키텍처 digest 고정) | 실행 사용자 | 포트 | health |
|---|---|---|---|---|---|
| `agent-office-server` | `apps/server/Dockerfile` / `apps/server` | `python:3.13.17-slim` | `65532:65532` | 8000 | `GET /api/v1/system/health` |
| `agent-office-web` | `apps/web/Dockerfile` / 저장소 루트 | `node:24.22.1-alpine`(빌드), `nginxinc/nginx-unprivileged:1.30.6-alpine`(런타임) | `101` | 8080 | `GET /` |
| PostgreSQL | 공식 이미지 | `postgres:17.11-alpine` | 이미지 기본값 | 내부 5432 | `pg_isready` |

- 서버는 빌드 단계에서 wheel을 만들고 런타임 단계는 wheel만 설치합니다.
- 웹은 루트 `package-lock.json`으로 `npm ci`를 실행해 의존성 버전을 재현합니다. 빌드 컨텍스트 범위는 `apps/web/Dockerfile.dockerignore`가 제한합니다.
- 베이스 이미지는 `태그@sha256:<index digest>`로 고정합니다. index digest는 `linux/amd64`와 `linux/arm64`를 모두 포함합니다.
- 두 이미지 모두 OCI label(`source`, `version`, `revision`)과 `HEALTHCHECK`를 가집니다.

## 설치 방식

| 방식 | 파일 | 용도 |
|---|---|---|
| 소스 빌드(B) | `deploy/compose.yaml` | 기여자·로컬 개발. `agent-office-server:dev`, `agent-office-web:dev`를 로컬에서 빌드 |
| 사전 빌드 이미지(A) | `deploy/compose.production.yaml` | 운영. 빌드 없이 `태그@digest` 이미지만 실행 |

### 소스 빌드

```bash
cp deploy/.env.example deploy/.env   # 로컬 암호를 바꾼다
docker compose --env-file deploy/.env -f deploy/compose.yaml up -d --build --wait
curl -fsS http://127.0.0.1:8000/api/v1/system/health
docker compose --env-file deploy/.env -f deploy/compose.yaml down
```

### 사전 빌드 이미지

```bash
cp deploy/.env.production.example deploy/.env.production   # 암호와 이미지 tag@digest를 바꾼다
docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml config --quiet
docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml up -d --wait
```

운영 구성의 기본값은 다음과 같습니다.

- server·web은 `127.0.0.1`에만 바인딩합니다. 외부 접속은 리버스 프록시나 Tunnel(#57)을 통해서만 엽니다.
- PostgreSQL은 호스트 포트를 열지 않습니다. `AGENT_OFFICE_POSTGRES_DATA`로 SSD 경로를 지정할 수 있습니다.
- server·web은 `read_only` 루트 파일시스템, `/tmp` tmpfs, `cap_drop: ALL`, `no-new-privileges`로 실행합니다.
- 모든 서비스는 `restart: always`이고, json-file 로그는 10MB × 3개로 회전합니다.

`deploy/.env.production.example`의 digest는 형식만 보여주는 자리표시자입니다. 아직 GHCR에 게시된 이미지가 없으므로 그대로는 실행되지 않습니다.

## 이미지 게시(관리자 승인 필요)

`.github/workflows/publish-images.yml`은 `workflow_dispatch`로만 실행되며 `image-publish` Environment를 사용합니다. 게시 전에 저장소 관리자가 다음을 직접 설정해야 합니다.

1. GitHub 저장소 Settings → Environments에서 `image-publish`를 만들고 필수 리뷰어를 지정합니다.
2. Actions에서 `Publish Images`를 수동 실행하고 SemVer `version`을 입력합니다.
3. 실행 요약에 기록된 `이미지:버전@sha256:...`을 운영 `.env.production`에 그대로 사용합니다.

게시된 이미지의 digest는 다음 명령으로 다시 확인할 수 있습니다.

```bash
docker buildx imagetools inspect ghcr.io/<owner>/agent-office-server:<version>
```

## 베이스 이미지 갱신

1. 새 버전 태그의 index digest를 확인합니다: `docker buildx imagetools inspect python:<version>-slim`
2. 출력의 Manifests에 `linux/amd64`와 `linux/arm64`가 모두 있는지 확인합니다.
3. Dockerfile의 `ARG *_IMAGE=` 또는 Compose의 PostgreSQL 참조를 `태그@digest`로 바꿉니다.
4. 별도 Issue/PR에서 CI `Images (amd64)`·`Images (arm64)`를 통과시킵니다.

PostgreSQL 메이저 버전 변경은 데이터 디렉터리 호환이 깨지므로 이미지 교체만으로 하지 않습니다. 백업·복원 절차(#58)를 거칩니다.

## Raspberry Pi 5 실측

2026-10-23, Raspberry Pi 5 8GB, Ubuntu 24.04.5 aarch64, Docker 29.9.2 / Compose v5.6.1 / BuildKit v0.34.0, 루트 SD 카드.

| 항목 | 기존 Dockerfile | #56 Dockerfile | 비고 |
|---|---:|---:|---|
| 콜드 빌드(server+web 병렬) | 18분 27초 | 11분 43초 | 베이스 이미지 다운로드 포함. 캐시 상태가 달라 단순 비교 불가 |
| 가장 느린 단계 | `pip install` 532초, `npm install` 350초 | `npm ci` 432초 | 네트워크·SD 카드 I/O가 주 원인 |
| server 재빌드(wheel 캐시) | - | 1분 58초 | 런타임 단계만 다시 실행 |
| server 이미지 | 260MB | 258MB | wheel을 bind mount로 설치해 레이어에 남기지 않음 |
| web 이미지 | 76.2MB | 90.6MB | 비루트 `nginx-unprivileged` 1.30.6 베이스 |
| 첫 `up --wait`(PostgreSQL 이미지 pull 포함) | - | 5분 59초 | |
| 재기동 `up --wait` | - | 20.9초 | |
| 유휴 메모리 | - | server 36MiB, PostgreSQL 22MiB, web 4.6MiB | `docker stats` 1회 |

운영 Compose(로컬 `:dev` 이미지로 대체)에서 확인한 사항:

- health JSON과 웹 HTTP 200을 확인했고, LAN 주소에서는 server 포트에 접근할 수 없었습니다(루프백 바인딩).
- 루트 파일시스템 쓰기는 거부되고 `/tmp`만 쓸 수 있었습니다. `CapDrop=[ALL]`, `no-new-privileges`, 로그 회전 10MB×3이 적용됐습니다.
- 컨테이너 안에서 PID 1에 SIGTERM을 보내자 13초 만에 재시작해 healthy로 돌아왔습니다(RestartCount 1).
- `docker kill`은 Docker가 수동 중지로 취급하므로 `restart: always`가 적용되지 않았습니다. 이것은 장애 시험 방법의 한계이지 정책 오류가 아닙니다.

미실행:

- 호스트 재부팅 후 자동 기동(#59·#63에서 수행)
- QEMU 교차 빌드(`linux/amd64` on Pi): binfmt 등록이 호스트 커널 설정을 바꾸므로 하지 않음. amd64는 CI 네이티브 러너에서 검증
- GHCR 게시와 게시 이미지 pull: 관리자 승인 전 실행하지 않음
- SSD에 PostgreSQL 데이터를 둔 구성: Pi에 SSD가 없음

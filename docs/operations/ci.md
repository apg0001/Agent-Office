# 지속적 통합 기준

관련 이슈: #16

`.github/workflows/ci.yml`은 `main`·`develop` 대상 PR, `develop` push와 수동 실행에서 동작합니다. 권한은 저장소 내용 읽기로 제한하고, 같은 PR의 이전 실행은 새 실행이 시작되면 취소합니다.

## 검사 구성

| 상태 검사 | 범위 |
|---|---|
| Python | Python 3.13, Ruff lint/format, mypy strict, pytest, pip 일관성, JSON Schema와 OpenAPI 검증. PostgreSQL 17.11 서비스로 스키마·마이그레이션 테스트 필수 실행(#22) |
| Node | Node 24, `npm ci`, TypeScript, Vitest, production build, production dependency audit |
| Go | Go 1.24, gofmt, vet, race test |
| Compose | actionlint workflow 문법, 개발 구성과 독립 운영 구성(`compose.production.yaml`) 렌더링 |
| Images (amd64·arm64) | GitHub 네이티브 `ubuntu-24.04`·`ubuntu-24.04-arm` 러너에서 server·web 이미지 빌드, 개발 Compose `up --wait`, 서버 이미지 안 `alembic upgrade head`, 아키텍처·비루트 사용자·health HTTP 확인 후 볼륨까지 정리 (#56, #22) |
| Secret Scan | 전체 Git 이력을 Gitleaks로 검사하며 탐지 내용은 출력·artifact·PR 댓글로 재노출하지 않음 |
| CI Gate | 위 검사가 모두 `success`일 때만 성공하는 보호 규칙용 단일 상태 검사 |

GitHub Action은 릴리스 태그 이름이 아니라 40자리 commit SHA로 고정하고 주석에 확인한 버전을 남깁니다. 업데이트할 때는 공식 릴리스와 commit SHA를 확인하고 별도 Issue/PR에서 전체 CI를 다시 실행합니다.

`main`과 `develop` 보호 규칙은 최신 기준 브랜치와 동기화된 `CI Gate` 성공을 필수로 요구합니다. 하위 job 이름은 진단용이며 보호 규칙 context는 `CI Gate` 하나로 고정합니다.

## 로컬 재현

```powershell
.\.venv\Scripts\python -m pip install -r requirements-dev.txt
.\.venv\Scripts\ruff check apps/server tests
.\.venv\Scripts\ruff format --check apps/server tests
.\.venv\Scripts\mypy apps/server/src
.\.venv\Scripts\python -m pytest apps/server/tests tests
.\.venv\Scripts\python -m pip check

npm ci
npm run typecheck
npm run test
npm run build
npm audit --omit=dev --audit-level=high

Push-Location collector/core
gofmt -l .
go vet ./...
go test -race ./...
Pop-Location

docker compose --env-file deploy/.env.example -f deploy/compose.yaml config --quiet
docker compose --env-file deploy/.env.production.example -f deploy/compose.production.yaml config --quiet
docker compose --env-file deploy/.env.example -f deploy/compose.yaml -p agent-office-ci up -d --build --wait
docker compose --env-file deploy/.env.example -f deploy/compose.yaml -p agent-office-ci down --volumes
docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:1.7.12
docker run --rm -v "${PWD}:/workspace" ghcr.io/gitleaks/gitleaks:v8.30.1 detect --source=/workspace --no-banner --redact
```

호스트 Go가 없으면 프로젝트 상태 문서에 기록된 공식 `golang:1.24-bookworm` 컨테이너로 동일 명령을 실행합니다.

## Fixture와 출력 안전

- fixture는 예약된 예시 UUID와 합성 상태만 사용하며 실제 사용자 로그를 변형해 만들지 않습니다.
- 실패 메시지에는 fixture 이름과 스키마 경로만 사용하고 이벤트 본문 전체를 출력하지 않습니다.
- Gitleaks 발견 시 로그를 복사해 Issue에 붙이지 않습니다. 노출된 비밀을 먼저 폐기·교체하고 보안 경로로 처리합니다.
- CI가 실행되지 않았거나 GitHub 장애로 결과를 확인하지 못한 경우 성공으로 기록하지 않습니다.

## 이미지 게시

`.github/workflows/publish-images.yml`은 GHCR에 `linux/amd64`·`linux/arm64` 이미지를 게시합니다. 외부 공개이므로 `workflow_dispatch`로만 실행되며, `image-publish` Environment의 필수 리뷰어 승인을 거치도록 설정해야 합니다. 절차는 [Docker 이미지 문서](../installation/docker-images.md)를 따릅니다.

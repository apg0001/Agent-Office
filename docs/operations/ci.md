# 지속적 통합 기준

관련 이슈: #16

`.github/workflows/ci.yml`은 `main`·`develop` 대상 PR, `develop` push와 수동 실행에서 동작합니다. 권한은 저장소 내용 읽기로 제한하고, 같은 PR의 이전 실행은 새 실행이 시작되면 취소합니다.

## 검사 구성

| 상태 검사 | 범위 |
|---|---|
| Python | Python 3.13, Ruff lint/format, mypy strict, pytest, pip 일관성, JSON Schema와 OpenAPI 검증 |
| Node | Node 24, `npm ci`, TypeScript, Vitest, production build, production dependency audit |
| Go | Go 1.24, gofmt, vet, race test |
| Compose | actionlint workflow 문법, 개발 구성과 base+production overlay 렌더링 |
| Secret Scan | 전체 Git 이력을 Gitleaks로 검사하며 탐지 내용은 출력·artifact·PR 댓글로 재노출하지 않음 |
| CI Gate | 위 다섯 검사가 모두 `success`일 때만 성공하는 보호 규칙용 단일 상태 검사 |

GitHub Action은 릴리스 태그 이름이 아니라 40자리 commit SHA로 고정하고 주석에 확인한 버전을 남깁니다. 업데이트할 때는 공식 릴리스와 commit SHA를 확인하고 별도 Issue/PR에서 전체 CI를 다시 실행합니다.

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
docker compose --env-file deploy/.env.example -f deploy/compose.yaml -f deploy/compose.production.yaml config --quiet
docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:1.7.12
docker run --rm -v "${PWD}:/workspace" ghcr.io/gitleaks/gitleaks:v8.30.1 detect --source=/workspace --no-banner --redact
```

호스트 Go가 없으면 프로젝트 상태 문서에 기록된 공식 `golang:1.24-bookworm` 컨테이너로 동일 명령을 실행합니다.

## Fixture와 출력 안전

- fixture는 예약된 예시 UUID와 합성 상태만 사용하며 실제 사용자 로그를 변형해 만들지 않습니다.
- 실패 메시지에는 fixture 이름과 스키마 경로만 사용하고 이벤트 본문 전체를 출력하지 않습니다.
- Gitleaks 발견 시 로그를 복사해 Issue에 붙이지 않습니다. 노출된 비밀을 먼저 폐기·교체하고 보안 경로로 처리합니다.
- CI가 실행되지 않았거나 GitHub 장애로 결과를 확인하지 못한 경우 성공으로 기록하지 않습니다.

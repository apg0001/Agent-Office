# 개발 환경 설치

## 확인된 기준선

- Windows 11 계열 환경(#13~#17)
- Raspberry Pi 5, Ubuntu 24.04 aarch64(#19, #56)
- Python 3.13+
- Node.js 24+, npm 11+
- Go 1.24+
- Docker Engine과 Compose

## 서버

```powershell
python -m venv .venv
.\.venv\Scripts\python -m pip install --upgrade pip
.\.venv\Scripts\python -m pip install -e ".\apps\server[dev]"
.\.venv\Scripts\python -m pytest apps/server/tests
.\.venv\Scripts\python -m ruff check apps/server
.\.venv\Scripts\python -m mypy apps/server/src
.\.venv\Scripts\python -m uvicorn agent_office.main:app --app-dir apps/server/src --reload
```

DB 스키마 테스트는 PostgreSQL이 필요합니다. 관리 권한 URL을 주면 테스트마다 임시 DB를 만들고 지웁니다.

```bash
docker run -d --name ao-pg-test -e POSTGRES_PASSWORD=<로컬 전용 암호> -p 127.0.0.1:55432:5432 postgres:17.11-alpine
AGENT_OFFICE_TEST_DATABASE_URL="postgresql+psycopg://postgres:<로컬 전용 암호>@127.0.0.1:55432/postgres" python -m pytest apps/server/tests/test_db_schema.py
```

URL이 없으면 DB 테스트는 사유를 표시하고 건너뜁니다. 건너뛴 결과를 통과로 기록하지 않습니다.

## 웹

```powershell
npm install
npm run typecheck
npm run test
npm run build
npm run dev:web
```

## Collector

```powershell
go test ./collector/...
go run ./collector/core/cmd/agent-office-collector
```

Go가 없으면 해당 검사를 성공으로 기록하지 말고 [프로젝트 상태](../project-status.md)에 미실행으로 남깁니다.

## Compose

```powershell
Copy-Item deploy/.env.example deploy/.env
# deploy/.env의 로컬 암호를 변경한다.
docker compose --env-file deploy/.env -f deploy/compose.yaml config
docker compose --env-file deploy/.env -f deploy/compose.yaml up --build
```

웹은 컨테이너 안 8080 포트(비루트 nginx)에서 실행되며 호스트 `AGENT_OFFICE_WEB_PORT`(기본 5173)로 연결됩니다.

운영에는 `.env.example` 값을 사용하지 않습니다. 운영 구성과 이미지 규칙은 [Docker 이미지와 Compose](docker-images.md)를 따릅니다. 문서가 있다고 프로덕션 배포가 승인된 것은 아닙니다.

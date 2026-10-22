# 개발 환경 설치

## 확인된 기준선

- Windows 11 계열 환경
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

운영에는 `.env.example` 값을 사용하지 않습니다. 현재 Compose는 개발 기준선이며 프로덕션 배포 승인을 의미하지 않습니다.

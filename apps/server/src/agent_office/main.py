import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager, suppress
from typing import Literal

from fastapi import FastAPI
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from agent_office import monitoring, retention
from agent_office.api import accounts, admin_settings, auth, devices, events, office, setup, teams
from agent_office.db.engine import get_engine
from agent_office.http_limits import BodySizeLimitMiddleware
from agent_office.validation import validation_error_handler


class HealthResponse(BaseModel):
    status: Literal["ok"]
    service: Literal["agent-office-server"]


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
    # 보존 기간 정리(#39). 첫 실행은 기동 5분 뒤라 짧은 실행(테스트·헬스 확인)에는 영향이 없다.
    # 외부 Heartbeat(#59)는 URL을 설정했을 때만 돈다.
    tasks = [
        asyncio.create_task(retention.run_periodically()),
        asyncio.create_task(monitoring.send_heartbeats()),
    ]
    try:
        yield
    finally:
        for task in tasks:
            task.cancel()
            with suppress(asyncio.CancelledError):
                await task


app = FastAPI(
    lifespan=lifespan,
    title="Agent Office API",
    version="0.0.0",
    docs_url="/api/docs",
    openapi_url="/api/openapi.json",
)


@app.get("/api/v1/system/health", response_model=HealthResponse, tags=["system"])
async def health() -> HealthResponse:
    """민감정보 없이 프로세스 생존 여부만 반환한다."""
    return HealthResponse(status="ok", service="agent-office-server")


@app.get("/api/v1/system/ready", tags=["system"])
def ready() -> JSONResponse:
    """준비 상태(#59): DB 연결과 마이그레이션 버전. 비정상이면 503. 세부 값은 ok/fail/outdated만."""
    try:
        with get_engine().begin() as connection:
            body = monitoring.readiness(connection)
    except Exception:  # noqa: BLE001 - DB 접속 실패도 같은 모양으로 알린다.
        body = {"ready": False, "checks": {"database": "fail", "migrations": "fail"}}
    return JSONResponse(body, status_code=200 if body["ready"] else 503)


app.add_middleware(BodySizeLimitMiddleware)
app.add_exception_handler(RequestValidationError, validation_error_handler)
app.include_router(auth.router)
app.include_router(accounts.router)
app.include_router(setup.router)
app.include_router(admin_settings.router)
app.include_router(admin_settings.metrics_router)
app.include_router(teams.router)
app.include_router(devices.router)
app.include_router(events.router)
app.include_router(office.router)

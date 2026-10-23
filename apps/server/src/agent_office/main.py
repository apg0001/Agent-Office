import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager, suppress
from typing import Literal

from fastapi import FastAPI
from fastapi.exceptions import RequestValidationError
from pydantic import BaseModel

from agent_office import retention
from agent_office.api import accounts, admin_settings, auth, devices, events, office, setup, teams
from agent_office.http_limits import BodySizeLimitMiddleware
from agent_office.validation import validation_error_handler


class HealthResponse(BaseModel):
    status: Literal["ok"]
    service: Literal["agent-office-server"]


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
    # 보존 기간 정리(#39). 첫 실행은 기동 5분 뒤라 짧은 실행(테스트·헬스 확인)에는 영향이 없다.
    task = asyncio.create_task(retention.run_periodically())
    try:
        yield
    finally:
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


app.add_middleware(BodySizeLimitMiddleware)
app.add_exception_handler(RequestValidationError, validation_error_handler)
app.include_router(auth.router)
app.include_router(accounts.router)
app.include_router(setup.router)
app.include_router(admin_settings.router)
app.include_router(teams.router)
app.include_router(devices.router)
app.include_router(events.router)
app.include_router(office.router)

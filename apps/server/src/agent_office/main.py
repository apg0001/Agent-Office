from typing import Literal

from fastapi import FastAPI
from pydantic import BaseModel

from agent_office.api import devices


class HealthResponse(BaseModel):
    status: Literal["ok"]
    service: Literal["agent-office-server"]


app = FastAPI(
    title="Agent Office API",
    version="0.0.0",
    docs_url="/api/docs",
    openapi_url="/api/openapi.json",
)


@app.get("/api/v1/system/health", response_model=HealthResponse, tags=["system"])
async def health() -> HealthResponse:
    """민감정보 없이 프로세스 생존 여부만 반환한다."""
    return HealthResponse(status="ok", service="agent-office-server")


app.include_router(devices.router)

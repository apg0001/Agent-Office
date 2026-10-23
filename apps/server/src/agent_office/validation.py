"""요청 검증 오류 응답. 입력 값과 미등록 키 이름을 되돌려 보내지 않는다."""

from __future__ import annotations

import re

from fastapi import Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

MAX_ERRORS = 20
# 위치에는 API가 정의한 필드 이름 형태만 남긴다. 그 밖(원문이 담길 수 있는 임의 키)은 가린다.
FIELD_NAME = re.compile(r"[a-z][a-z0-9_]{0,63}")
SAFE_LOCATIONS = {"body", "query", "path", "header", "cookie"}


def _safe_loc(loc: tuple[object, ...], allowed: set[str]) -> list[object]:
    result: list[object] = []
    for part in loc:
        if (
            isinstance(part, int)
            or isinstance(part, str)
            and (part in SAFE_LOCATIONS or part in allowed)
        ):
            result.append(part)
        else:
            result.append("<field>")
    return result


async def validation_error_handler(request: Request, exc: Exception) -> JSONResponse:
    assert isinstance(exc, RequestValidationError)
    allowed = {
        name for route in request.app.routes for name in getattr(route, "_agent_office_fields", ())
    } | _model_field_names(request)
    errors = [
        {"loc": _safe_loc(tuple(error.get("loc", ())), allowed), "type": str(error.get("type"))}
        for error in exc.errors()[:MAX_ERRORS]
    ]
    return JSONResponse(status_code=422, content={"detail": errors})


def _model_field_names(request: Request) -> set[str]:
    """요청 모델에 선언된 필드 이름(중첩 포함)만 허용 목록으로 쓴다."""
    route = request.scope.get("route")
    body_field = getattr(route, "body_field", None)
    names: set[str] = set()
    stack = [getattr(getattr(body_field, "field_info", None), "annotation", None)]
    seen: set[int] = set()
    while stack:
        model = stack.pop()
        if model is None or id(model) in seen:
            continue
        seen.add(id(model))
        fields = getattr(model, "model_fields", None)
        if fields:
            for name, info in fields.items():
                if FIELD_NAME.fullmatch(name):
                    names.add(name)
                stack.append(info.annotation)
        for arg in getattr(model, "__args__", ()) or ():
            stack.append(arg)
    return names

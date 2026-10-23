# 베이스 이미지는 멀티아키텍처 index digest로 고정한다(linux/amd64, linux/arm64).
# 갱신 절차는 docs/installation/docker-images.md를 따른다.
ARG PYTHON_IMAGE=python:3.13.17-slim@sha256:fe14fd57a4c0361110eaa0c284112b189750bcb43fb04bb5d1ee7b7744027ccf

FROM ${PYTHON_IMAGE} AS build

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /src
COPY pyproject.toml ./
COPY src ./src
# 서버 패키지와 런타임 의존성을 wheel로 만들어 런타임 단계에는 빌드 도구를 남기지 않는다.
RUN python -m pip wheel --wheel-dir /wheels .

FROM ${PYTHON_IMAGE} AS runtime

ARG VERSION=0.0.0-dev
ARG REVISION=unknown

LABEL org.opencontainers.image.title="agent-office-server" \
      org.opencontainers.image.description="Agent Office FastAPI server" \
      org.opencontainers.image.source="https://github.com/apg0001/Agent-Office" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

# wheel은 바인드 마운트로만 읽어 런타임 이미지 레이어에 남기지 않는다.
RUN --mount=type=bind,from=build,source=/wheels,target=/tmp/wheels \
    python -m pip install --no-index --find-links /tmp/wheels agent-office-server

WORKDIR /app
# 마이그레이션 실행용: `alembic -c /app/alembic.ini upgrade head`
COPY alembic.ini ./alembic.ini

USER 65532:65532
EXPOSE 8000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/api/v1/system/health', timeout=2)"]
CMD ["uvicorn", "agent_office.main:app", "--host", "0.0.0.0", "--port", "8000"]

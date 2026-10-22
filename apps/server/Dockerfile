FROM python:3.13-slim AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app
COPY pyproject.toml ./
COPY src ./src
RUN python -m pip install --no-cache-dir .

USER 65532:65532
EXPOSE 8000
CMD ["uvicorn", "agent_office.main:app", "--host", "0.0.0.0", "--port", "8000"]

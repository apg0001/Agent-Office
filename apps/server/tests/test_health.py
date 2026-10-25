from fastapi.testclient import TestClient

from agent_office.main import app


def test_health_is_anonymous_and_minimal() -> None:
    response = TestClient(app).get("/api/v1/system/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "service": "agent-office-server"}

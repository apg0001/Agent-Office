from agent_office.main import app
from openapi_spec_validator import validate


def test_generated_openapi_is_valid() -> None:
    document = app.openapi()

    validate(document)
    assert document["openapi"].startswith("3.")
    assert "/api/v1/system/health" in document["paths"]

import json
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource

ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIRECTORY = ROOT / "packages" / "event-schema" / "schema"
FIXTURE_DIRECTORY = ROOT / "packages" / "event-schema" / "fixtures"


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


EVENT_SCHEMA = load_json(SCHEMA_DIRECTORY / "event-v1.schema.json")
REQUEST_SCHEMA = load_json(
    SCHEMA_DIRECTORY / "collector-event-batch-request-v1.schema.json"
)
RESPONSE_SCHEMA = load_json(
    SCHEMA_DIRECTORY / "collector-event-batch-response-v1.schema.json"
)
SCHEMAS = (EVENT_SCHEMA, REQUEST_SCHEMA, RESPONSE_SCHEMA)
REGISTRY = Registry().with_resources(
    (schema["$id"], Resource.from_contents(schema)) for schema in SCHEMAS
)


def schema_for_fixture(path: Path) -> dict[str, Any]:
    if path.name.startswith("event-"):
        return EVENT_SCHEMA
    if path.name.startswith("batch-request"):
        return REQUEST_SCHEMA
    if path.name.startswith("batch-response"):
        return RESPONSE_SCHEMA
    raise AssertionError(f"스키마 매핑이 없는 fixture 이름: {path.name}")


def validation_errors(path: Path) -> list[Any]:
    validator = Draft202012Validator(
        schema_for_fixture(path),
        registry=REGISTRY,
        format_checker=FormatChecker(),
    )
    return list(validator.iter_errors(load_json(path)))


def fixture_paths(kind: str) -> list[Path]:
    return sorted((FIXTURE_DIRECTORY / kind).glob("*.json"))


def test_schemas_are_valid_draft_2020_12() -> None:
    for schema in SCHEMAS:
        Draft202012Validator.check_schema(schema)


@pytest.mark.parametrize("fixture", fixture_paths("valid"), ids=lambda path: path.name)
def test_valid_fixtures_pass(fixture: Path) -> None:
    assert validation_errors(fixture) == []


@pytest.mark.parametrize(
    "fixture", fixture_paths("invalid"), ids=lambda path: path.name
)
def test_invalid_fixtures_fail(fixture: Path) -> None:
    assert validation_errors(fixture), f"무효 fixture가 통과함: {fixture.name}"


def test_event_contract_has_only_reviewed_fields() -> None:
    assert set(EVENT_SCHEMA["properties"]) == {
        "schema_version",
        "event_id",
        "device_id",
        "session_id",
        "agent_id",
        "parent_agent_id",
        "provider",
        "source",
        "event_type",
        "activity",
        "confidence",
        "occurred_at",
        "sequence",
    }
    assert EVENT_SCHEMA["additionalProperties"] is False

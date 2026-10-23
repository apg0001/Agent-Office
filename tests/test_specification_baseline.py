import hashlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC_DIR = ROOT / "docs" / "specifications"
MASTER_PROMPT = SPEC_DIR / "agent-office-master-development-prompt-v0.8.md"
SPEC_INDEX = SPEC_DIR / "README.md"

EXPECTED_SHA256 = "76bf1febe43afa60072e11f232885709e24f4a4fb619067935426adfafd4c17b"
EXPECTED_SIZE = 44_535
EXPECTED_REQUIREMENT_RANGES = (
    "AUTH-001~006",
    "USER-001~003",
    "TEAM-001~006",
    "DEV-001~006",
    "COL-001~010",
    "EVT-001~008",
    "OFF-001~010",
    "VIEW-001~004",
    "SEC-001~006",
    "SETUP-001~007",
    "NOTIFY-001~005",
    "DIST-001~012",
    "OPS-001~018",
    "DOC-001~004",
)


def test_master_prompt_is_an_immutable_exact_snapshot() -> None:
    contents = MASTER_PROMPT.read_bytes()

    assert len(contents) == EXPECTED_SIZE
    assert hashlib.sha256(contents).hexdigest() == EXPECTED_SHA256


def test_master_prompt_contains_every_indexed_requirement_range() -> None:
    prompt = MASTER_PROMPT.read_text(encoding="utf-8")
    traceability = (ROOT / "docs" / "requirements" / "traceability.md").read_text(
        encoding="utf-8"
    )

    for requirement_range in EXPECTED_REQUIREMENT_RANGES:
        assert requirement_range in prompt
        assert requirement_range in traceability


def test_specification_indexes_link_the_authoritative_snapshot() -> None:
    expected_link = "specifications/agent-office-master-development-prompt-v0.8.md"
    docs_index = (ROOT / "docs" / "README.md").read_text(encoding="utf-8")
    traceability = (ROOT / "docs" / "requirements" / "traceability.md").read_text(
        encoding="utf-8"
    )
    spec_index = SPEC_INDEX.read_text(encoding="utf-8")

    assert expected_link in docs_index
    assert "../specifications/agent-office-master-development-prompt-v0.8.md" in (
        traceability
    )
    assert "76bf1febe43afa60072e11f232885709e24f4a4fb619067935426adfafd4c17b" in (
        spec_index
    )

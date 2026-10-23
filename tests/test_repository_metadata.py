import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
ISSUE_TEMPLATE_DIR = ROOT / ".github" / "ISSUE_TEMPLATE"
CI_WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"


def test_issue_forms_are_valid_and_traceable() -> None:
    form_paths = sorted(ISSUE_TEMPLATE_DIR.glob("*.yml"))
    form_paths.remove(ISSUE_TEMPLATE_DIR / "config.yml")

    assert {path.name for path in form_paths} == {
        "bug.yml",
        "documentation.yml",
        "feature.yml",
        "poc.yml",
    }

    for path in form_paths:
        form = yaml.safe_load(path.read_text(encoding="utf-8"))
        assert form["name"]
        assert form["description"]
        assert form["title"]
        assert form["body"]
        assert any(item.get("validations", {}).get("required") for item in form["body"])


def test_blank_issues_are_disabled_and_security_link_is_private_route() -> None:
    config = yaml.safe_load(
        (ISSUE_TEMPLATE_DIR / "config.yml").read_text(encoding="utf-8")
    )

    assert config["blank_issues_enabled"] is False
    assert config["contact_links"][0]["url"].endswith("/security/policy")


def test_pull_request_template_requires_evidence_and_handoff() -> None:
    template = (ROOT / ".github" / "PULL_REQUEST_TEMPLATE.md").read_text(
        encoding="utf-8"
    )

    for heading in (
        "## 실제 실행한 검증",
        "## 미실행 검증",
        "## 보안·개인정보·권한 검토",
        "## 문서와 인수인계",
    ):
        assert heading in template

    assert "docs/project-status.md" in template
    assert "Closes #" in template


def test_ci_workflow_has_a_pinned_complete_gate() -> None:
    workflow_text = CI_WORKFLOW.read_text(encoding="utf-8")
    workflow = yaml.load(workflow_text, Loader=yaml.BaseLoader)

    assert workflow["permissions"] == {"contents": "read"}
    assert set(workflow["on"]["pull_request"]["branches"]) == {"main", "develop"}
    assert workflow["concurrency"]["cancel-in-progress"] == "true"
    assert set(workflow["jobs"]["ci-gate"]["needs"]) == {
        "python",
        "node",
        "go",
        "compose",
        "secret-scan",
    }
    assert workflow["jobs"]["ci-gate"]["name"] == "CI Gate"

    action_references = re.findall(r"uses:\s+[^@\s]+@([^\s#]+)", workflow_text)
    assert action_references
    assert all(
        re.fullmatch(r"[0-9a-f]{40}", reference) for reference in action_references
    )
    assert "rhysd/actionlint@sha256:" in workflow_text
    assert 'GITLEAKS_ENABLE_UPLOAD_ARTIFACT: "false"' in workflow_text

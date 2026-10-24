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
        "go-gui",
        "compose",
        "images",
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


def test_image_publish_workflow_is_manual_and_gated() -> None:
    workflow_path = ROOT / ".github" / "workflows" / "publish-images.yml"
    workflow_text = workflow_path.read_text(encoding="utf-8")
    workflow = yaml.load(workflow_text, Loader=yaml.BaseLoader)

    # GHCR 게시는 외부 공개다. PR·push로는 절대 실행되지 않아야 한다.
    assert set(workflow["on"]) == {"workflow_dispatch"}
    assert workflow["permissions"] == {"contents": "read"}
    publish = workflow["jobs"]["publish"]
    assert publish["environment"] == "image-publish"
    assert publish["permissions"] == {"contents": "read", "packages": "write"}

    action_references = re.findall(r"uses:\s+[^@\s]+@([^\s#]+)", workflow_text)
    assert action_references
    assert all(
        re.fullmatch(r"[0-9a-f]{40}", reference) for reference in action_references
    )
    assert "platforms: linux/amd64,linux/arm64" in workflow_text


def test_dockerfiles_pin_multi_arch_base_images() -> None:
    for dockerfile in (
        ROOT / "apps" / "server" / "Dockerfile",
        ROOT / "apps" / "web" / "Dockerfile",
    ):
        text = dockerfile.read_text(encoding="utf-8")
        base_images = re.findall(r"^ARG \w+_IMAGE=(\S+)$", text, flags=re.MULTILINE)
        assert base_images, dockerfile
        for image in base_images:
            assert re.fullmatch(r"[\w./-]+:[\w.-]+@sha256:[0-9a-f]{64}", image), image
        assert "HEALTHCHECK" in text
        assert re.search(r"^USER \d+:\d+$", text, flags=re.MULTILINE) or (
            "nginx-unprivileged" in text
        )


def test_production_compose_uses_pinned_images_without_builds() -> None:
    compose = yaml.safe_load(
        (ROOT / "deploy" / "compose.production.yaml").read_text(encoding="utf-8")
    )

    for name, service in compose["services"].items():
        assert "build" not in service, name
        assert service["restart"] == "always", name
        assert "no-new-privileges:true" in service["security_opt"], name
    # 호스트 포트는 proxy(#57)만 연다. 서버는 proxy가 넘긴 클라이언트 IP를 믿으므로 직접 노출되면 안 된다.
    for name in ("postgres", "server", "web"):
        assert "ports" not in compose["services"][name], name
    for name in ("server", "web", "proxy"):
        service = compose["services"][name]
        assert service["read_only"] is True, name
        assert service["cap_drop"] == ["ALL"], name
    proxy = compose["services"]["proxy"]
    assert proxy.get("cap_add", []) == ["NET_BIND_SERVICE"]
    assert re.search(r"@sha256:[0-9a-f]{64}\}$", proxy["image"]), proxy["image"]
    assert proxy["ports"] and all(
        port.startswith("${AGENT_OFFICE_BIND_ADDRESS:-127.0.0.1}:")
        for port in proxy["ports"]
    )

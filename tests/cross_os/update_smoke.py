"""승인형 자동 업데이트 실측(#180): 이전 빌드 설치 → 로컬 서명 manifest·새 설치 파일 → 트레이 감지 → 승인 → 설치 →
후처리 → Hook·서비스 보존·버전 상승.

일회용 환경(CI 러너) 전용(AGENT_OFFICE_SMOKE_DISPOSABLE=1). 서명 키는 CI가 이 실행을 위해 만든 테스트 키다(실제
배포 키 아님). manifest·설치 파일은 127.0.0.1의 http 테스트 서버로 내놓으며, Core는 환경 변수
AGENT_OFFICE_UPDATE_ALLOW_LOOPBACK_HTTP=1일 때만 루프백 http를 받는다(서명 검증은 그대로 필수).

    python tests/cross_os/update_smoke.py --old <이전 버전> --new <새 버전> --serve <manifest·새 설치 파일 폴더> \
        --install <이전 설치 파일> [--install ...]

자동화하지 못하는 것: macOS 관리자 암호 대화상자, Linux polkit(pkexec) 인증 창. 러너에는 사람이 없으므로
AGENT_OFFICE_UPDATE_ELEVATE=sudo-n(암호 없는 sudo)으로 같은 설치 명령(installer(8), apt-get)을 실행한다.
OS 알림이 실제로 화면에 뜨는지도 보지 않는다(트레이 셀프 테스트가 알림 요청 횟수만 센다).
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import traceback
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from smoke_common import (
    USER_HOOK,
    Checks,
    core_service_query,
    count,
    gui_prefs_path,
    install,
    layout,
    run,
    running,
    serve,
    uninstall,
    wait,
)

PORT = 8765


def main() -> int:
    if os.environ.get("AGENT_OFFICE_SMOKE_DISPOSABLE") != "1":
        print(
            "일회용 환경에서만 실행합니다(AGENT_OFFICE_SMOKE_DISPOSABLE=1)",
            file=sys.stderr,
        )
        return 2
    ap = argparse.ArgumentParser()
    ap.add_argument("--old", required=True)
    ap.add_argument("--new", required=True)
    ap.add_argument("--serve", required=True)
    ap.add_argument("--install", action="append", required=True)
    a = ap.parse_args()
    c = Checks()
    paths = layout()
    core, tray = str(paths["core"]), str(paths["tray"])
    claude = Path.home() / ".claude" / "settings.json"
    codex_dir = Path.home() / ".codex"
    prefs = gui_prefs_path()
    srv = None
    try:
        install([str(Path(p).resolve()) for p in a.install])
        c.record("old_installed", a.old in run([core, "version"]).stdout)

        # 이전 버전에서 사용자가 승인해 둔 상태: Claude·Codex Hook, Core 자동 시작, 마법사 끝남.
        claude.parent.mkdir(parents=True, exist_ok=True)
        claude.write_text(
            json.dumps({"hooks": {"Stop": [{"matcher": "", "hooks": [USER_HOOK]}]}}),
            encoding="utf-8",
        )
        codex_dir.mkdir(parents=True, exist_ok=True)
        run([core, "hooks", "install", "--tool", "claude", "--yes"])
        run([core, "hooks", "install", "--tool", "codex", "--yes"])
        codex_before = (codex_dir / "hooks.json").read_bytes()
        q = core_service_query()
        if q:
            run([core, "service", "install", "--activate"], timeout=120)
            c.record("old_service_registered", run(q, check=False).returncode == 0)
        prefs.parent.mkdir(parents=True, exist_ok=True)
        prefs.write_text(
            json.dumps(
                {"schema_version": 1, "setup": "dismissed", "last_version": a.old}
            ),
            encoding="utf-8",
        )

        srv = serve(str(Path(a.serve).resolve()), PORT)
        env = dict(os.environ)
        env["AGENT_OFFICE_UPDATE_MANIFEST_URL"] = (
            f"http://127.0.0.1:{PORT}/manifest.json"
        )
        env["AGENT_OFFICE_UPDATE_ALLOW_LOOPBACK_HTTP"] = "1"
        if sys.platform != "win32":
            env["AGENT_OFFICE_UPDATE_ELEVATE"] = "sudo-n"

        # 트레이: 감지 → "업데이트 설치" 항목 클릭(승인) → 설치 시작 → 트레이 종료.
        report_path = Path(tempfile.mkdtemp()) / "update-self-test.json"
        st = subprocess.Popen(
            [tray, "--update-self-test", str(report_path)],
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        try:
            rc = st.wait(timeout=420)
        except subprocess.TimeoutExpired:
            st.kill()
            rc = -1
        report = (
            json.loads(report_path.read_text(encoding="utf-8"))
            if report_path.exists()
            else {}
        )
        c.note(
            "tray_update_self_test",
            {
                k: report.get(k)
                for k in ("check", "install_click", "notifications", "after_start")
            },
        )
        c.record("tray_detected_update", report.get("check") == "update_available")
        c.record(
            "tray_offered_install_item",
            f"v{a.new}" in str(report.get("offered_label", "")),
        )
        c.record(
            "tray_install_approved", report.get("install_click") == "update_installing"
        )
        c.record("tray_self_test_exit_zero", rc == 0)

        # 도우미: 설치 프로그램 실행 → 새 트레이 실행 → 후처리(last_version 기록).
        def upgraded() -> bool:
            try:
                p = json.loads(prefs.read_text(encoding="utf-8"))
            except (OSError, ValueError):
                return False
            return p.get("last_version") == a.new

        c.record(
            "version_bumped",
            wait(lambda: a.new in run([core, "version"], check=False).stdout, 900),
        )
        c.record("post_install_done", wait(upgraded, 300))
        c.record("result_consumed", not (prefs.parent / "update-result.json").exists())
        c.record(
            "new_tray_running",
            wait(lambda: count("agent-office-collector-tray") == 1, 60),
        )
        c.record("tray_version_bumped", a.new in run([tray, "version"]).stdout)

        settings = json.loads(claude.read_text(encoding="utf-8"))
        stop_hooks = [
            h
            for g in settings.get("hooks", {}).get("Stop", [])
            for h in g.get("hooks", [])
        ]
        c.record("claude_hooks_kept", "PreToolUse" in settings.get("hooks", {}))
        c.record("user_hook_kept", USER_HOOK in stop_hooks)
        # 설치 위치가 그대로라 Codex Hook 명령이 바뀌지 않는다(Codex 재신뢰 불필요).
        c.record(
            "codex_hooks_unchanged",
            (codex_dir / "hooks.json").read_bytes() == codex_before,
        )
        if q:
            c.record("service_kept", run(q, check=False).returncode == 0)

            def core_new() -> bool:
                s = json.loads(
                    run([core, "status", "--json"], check=False).stdout or "{}"
                )
                return s.get("process_running") is True

            c.record("core_running_after_update", wait(core_new, 60))
    except Exception as exc:  # noqa: BLE001
        c.failed.append("exception")
        c.note("exception", str(exc)[:200])
        # 경로 원문 없이 실패한 줄 번호만 남긴다.
        c.note("exception_line", traceback.extract_tb(exc.__traceback__)[-1].lineno)
    finally:
        if srv is not None:
            srv.shutdown()
        if running("agent-office-collector-tray"):
            if sys.platform == "win32":
                subprocess.run(
                    ["taskkill", "/F", "/IM", "agent-office-collector-tray.exe"],
                    check=False,
                    capture_output=True,
                )
            else:
                subprocess.run(
                    ["pkill", "-f", "agent-office-collector-tray"], check=False
                )
        time.sleep(2)
        with contextlib.suppress(Exception):
            uninstall()
        shutil.rmtree(claude.parent, ignore_errors=True)
        shutil.rmtree(codex_dir, ignore_errors=True)
    return c.finish()


if __name__ == "__main__":
    sys.exit(main())

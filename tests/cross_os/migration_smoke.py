"""명령행 설치 → 설치 프로그램 이전 실측(#180).

이전 명령행 설치(macOS·Linux `~/.local/bin/agent-office-collector`, Windows `%LOCALAPPDATA%\\AgentOffice\\bin`)에서
Claude·Codex Hook과 Core 자동 시작을 켜 둔 상태로 설치 프로그램을 설치하고 트레이를 처음 띄운다. 기대:
설정·상태 폴더(등록·장치 토큰·큐·일시중지)는 그대로, 소유 Hook·서비스 정의는 새 설치 위치를 가리키고 다른 Hook은
그대로, 이전 Core 프로세스는 멈추고 새 Core 하나만 돈다. 이어서 승인형 정리(`legacy remove --yes`)가 알아본
이전 파일만 지운다.

일회용 환경(CI 러너) 전용(AGENT_OFFICE_SMOKE_DISPOSABLE=1). 결과는 단계별 통과 여부·개수만 출력한다.

    python tests/cross_os/migration_smoke.py --version <버전> --cli-core <명령행 Core 실행 파일> --install <설치 파일>

장치 등록은 하지 않는다(서버 없음). 같은 설정 폴더를 쓰는지는 이전 Core로 켠 일시중지 상태가 새 Core에서도
보이는지로 확인한다.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import shutil
import subprocess
import sys
import time
import traceback
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from smoke_common import (
    USER_HOOK,
    Checks,
    count,
    gui_prefs_path,
    install,
    layout,
    legacy_dir,
    run,
    running,
    uninstall,
    wait,
)

ROOT = Path(__file__).resolve().parents[2]


def service_definition() -> Path:
    if sys.platform == "win32":
        return (
            Path(os.environ["LOCALAPPDATA"])
            / "AgentOffice"
            / "Collector"
            / "service"
            / "agent-office-collector-task.xml"
        )
    return (
        Path.home()
        / "Library"
        / "LaunchAgents"
        / "io.github.apg0001.agent-office.collector.plist"
    )


def read_text_any(p: Path) -> str:
    data = p.read_bytes()
    if data[:2] == b"\xff\xfe":
        return data.decode("utf-16")
    return data.decode("utf-8", errors="replace")


def hook_form(p: str) -> str:
    """Hook 명령 안의 실행 파일 표기(Windows는 슬래시, 대소문자 무시 비교)."""
    if sys.platform == "win32":
        return p.replace("\\", "/").lower()
    return p


def hook_text(s: str) -> str:
    """JSON 문자열을 풀어 비교한다(Windows는 소문자)."""
    return s.lower() if sys.platform == "win32" else s


def core_process_paths() -> list[str]:
    """실행 중인 Core 프로세스의 실행 파일 경로(비교에만 쓰고 출력하지 않음)."""
    if sys.platform == "win32":
        p = subprocess.run(
            [
                "powershell",
                "-NoProfile",
                "-Command",
                "(Get-Process -Name agent-office-collector -ErrorAction SilentlyContinue).Path",
            ],
            check=False,
            capture_output=True,
            text=True,
            errors="replace",
        )
        return [line.strip() for line in p.stdout.splitlines() if line.strip()]
    p = subprocess.run(
        ["ps", "-axo", "comm="],
        check=False,
        capture_output=True,
        text=True,
        errors="replace",
    )
    return [
        line.strip()
        for line in p.stdout.splitlines()
        if line.strip().endswith("/agent-office-collector")
    ]


def main() -> int:
    if os.environ.get("AGENT_OFFICE_SMOKE_DISPOSABLE") != "1":
        print(
            "일회용 환경에서만 실행합니다(AGENT_OFFICE_SMOKE_DISPOSABLE=1)",
            file=sys.stderr,
        )
        return 2
    if sys.platform not in ("win32", "darwin"):
        print("Windows·macOS 러너용입니다", file=sys.stderr)
        return 2
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", required=True)
    ap.add_argument("--cli-core", required=True)
    ap.add_argument("--install", required=True)
    a = ap.parse_args()
    c = Checks()
    paths = layout()
    new_core, tray = str(paths["core"]), str(paths["tray"])
    legacy = legacy_dir()
    win = sys.platform == "win32"
    old_core = legacy / (
        "agent-office-collector.exe" if win else "agent-office-collector"
    )
    claude = Path.home() / ".claude" / "settings.json"
    codex_dir = Path.home() / ".codex"
    prefs = gui_prefs_path()
    tray_proc = None
    try:
        # --- 이전 명령행 설치 ---------------------------------------------------------
        legacy.mkdir(parents=True, exist_ok=True)
        shutil.copy2(a.cli_core, old_core)
        os.chmod(old_core, 0o755)
        shutil.copy2(
            ROOT / "collector" / "packaging" / "README.txt", legacy / "README.txt"
        )
        if win:
            shutil.copy2(
                ROOT / "collector" / "packaging" / "uninstall.ps1",
                legacy / "uninstall.ps1",
            )
        else:
            shutil.copy2(
                ROOT / "collector" / "packaging" / "uninstall.sh",
                legacy / "agent-office-collector-uninstall",
            )
        claude.parent.mkdir(parents=True, exist_ok=True)
        claude.write_text(
            json.dumps({"hooks": {"Stop": [{"matcher": "", "hooks": [USER_HOOK]}]}}),
            encoding="utf-8",
        )
        codex_dir.mkdir(parents=True, exist_ok=True)
        old = str(old_core)
        run([old, "hooks", "install", "--tool", "claude", "--yes"])
        run([old, "hooks", "install", "--tool", "codex", "--yes"])
        run([old, "service", "install", "--activate"], timeout=120)
        run([old, "pause"])
        c.record(
            "legacy_core_running",
            wait(
                lambda: any(p.lower() == old.lower() for p in core_process_paths()), 30
            ),
        )
        codex_before = (codex_dir / "hooks.json").read_bytes()

        # --- 설치 프로그램 → 트레이 첫 실행(후처리) -----------------------------------------
        install([str(Path(a.install).resolve())])
        prefs.parent.mkdir(parents=True, exist_ok=True)
        prefs.write_text(
            json.dumps({"schema_version": 1, "setup": "dismissed"}), encoding="utf-8"
        )
        tray_proc = subprocess.Popen(
            [tray], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
        )

        def migrated() -> bool:
            try:
                return (
                    json.loads(prefs.read_text(encoding="utf-8")).get("last_version")
                    == a.version
                )
            except (OSError, ValueError):
                return False

        c.record("post_install_done", wait(migrated, 240))

        s = claude.read_text(encoding="utf-8")
        c.record("claude_hooks_point_to_new_core", hook_form(new_core) in hook_text(s))
        c.record("claude_hooks_no_legacy_path", hook_form(old) not in hook_text(s))
        stop = [
            h
            for g in json.loads(s)["hooks"].get("Stop", [])
            for h in g.get("hooks", [])
        ]
        c.record("user_hook_kept", USER_HOOK in stop)
        cx = (codex_dir / "hooks.json").read_text(encoding="utf-8")
        c.record(
            "codex_hooks_point_to_new_core",
            hook_form(new_core) in hook_text(cx) and hook_form(old) not in hook_text(cx),
        )
        # 명령 경로가 바뀌었으므로 Codex는 한 번 다시 신뢰해야 한다(트레이가 알림으로 안내).
        c.record(
            "codex_hooks_rewritten_needs_trust",
            (codex_dir / "hooks.json").read_bytes() != codex_before,
        )
        svc = read_text_any(service_definition())
        c.record(
            "service_points_to_new_core",
            new_core.lower() in svc.lower() and old.lower() not in svc.lower(),
        )

        def only_new_core() -> bool:
            procs = [p.lower() for p in core_process_paths()]
            return len(procs) == 1 and procs[0] == new_core.lower()

        c.record("single_new_core_process", wait(only_new_core, 90))
        c.note("core_processes", len(core_process_paths()))
        st = json.loads(run([new_core, "status", "--json"]).stdout)
        c.record("config_kept_paused_state", st.get("paused") is True)

        # --- 승인형 정리 ---------------------------------------------------------------
        ls = json.loads(run([new_core, "legacy", "status", "--json"]).stdout)
        c.record(
            "legacy_listed",
            ls.get("eligible") is True and len(ls.get("files", [])) == 3,
        )
        rm = json.loads(
            run([new_core, "legacy", "remove", "--yes", "--json"], check=False).stdout
        )
        c.note("legacy_removed", rm.get("removed"))
        c.record("legacy_removed_all", rm.get("removed") == 3 and rm.get("failed") == 0)
        c.record(
            "legacy_files_gone",
            not old_core.exists() and not (legacy / "README.txt").exists(),
        )
        run([new_core, "resume"], check=False)
    except Exception as exc:  # noqa: BLE001
        c.failed.append("exception")
        c.note("exception", str(exc)[:200])
        # 경로 원문 없이 실패한 줄 번호만 남긴다.
        c.note("exception_line", traceback.extract_tb(exc.__traceback__)[-1].lineno)
    finally:
        if tray_proc is not None and tray_proc.poll() is None:
            tray_proc.kill()
        if running("agent-office-collector-tray"):
            if win:
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
        shutil.rmtree(legacy, ignore_errors=True)
        shutil.rmtree(claude.parent, ignore_errors=True)
        shutil.rmtree(codex_dir, ignore_errors=True)
    c.note("core_count_after", count("agent-office-collector"))
    return c.finish()


if __name__ == "__main__":
    sys.exit(main())

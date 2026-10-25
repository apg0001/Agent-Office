"""Collector 설치 프로그램 실측(#159): 설치 → 파일·버전 → Hook·자동 시작 → 트레이 실행·메뉴 → 제거.

일회용 환경(CI 러너·컨테이너) 전용이다. 실제 사용자 홈의 Claude 설정·자동 시작·Core 서비스와
시스템 위치(/Applications, /usr/bin, %LOCALAPPDATA%\\Programs)를 쓰고 지우므로
AGENT_OFFICE_SMOKE_DISPOSABLE=1 이 없으면 실행하지 않는다. 결과는 단계별 통과 여부·결과 코드만
JSON으로 출력한다(경로·출력 원문·토큰 없음).

    python tests/cross_os/installer_smoke.py <버전> <설치 파일> [<설치 파일> ...]

- Windows: agent-office-collector-setup-<버전>-windows-amd64.exe (/S 무인 설치·제거)
- macOS: agent-office-collector-<버전>-macos-universal.pkg (sudo installer)
- Linux: Core deb + desktop deb (sudo apt-get install ./...). 트레이 D-Bus 메뉴 클릭은 환경 변수
  AO_SNI_ITEMS(최소 StatusNotifierWatcher 기록 파일)와 AO_SNI_CLIENT(sni_client.py)가 있으면 확인한다.

자동화할 수 없는 것: OS 패널에 아이콘이 실제로 그려지는지, 사람의 클릭, Gatekeeper·SmartScreen 대화상자.
"""

from __future__ import annotations

import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

USER_HOOK = {"type": "command", "command": "echo installer-smoke-user-hook"}
APP = Path("/Applications/Agent Office Collector.app")
PKG_ID = "io.github.apg0001.agent-office.collector"
WIN_UNINSTALL_KEY = (
    r"Software\Microsoft\Windows\CurrentVersion\Uninstall\AgentOfficeCollector"
)


def layout() -> dict[str, Path]:
    if sys.platform == "win32":
        base = Path(os.environ["LOCALAPPDATA"]) / "Programs" / "Agent Office Collector"
        return {
            "dir": base,
            "core": base / "agent-office-collector.exe",
            "gui": base / "agent-office-collector-gui.exe",
            "tray": base / "agent-office-collector-tray.exe",
            "uninstall": base / "uninstall.exe",
            "menu": Path(os.environ["APPDATA"])
            / "Microsoft"
            / "Windows"
            / "Start Menu"
            / "Programs"
            / "Agent Office Collector"
            / "Agent Office Collector.lnk",
        }
    if sys.platform == "darwin":
        c = APP / "Contents"
        tray_app = c / "Library" / "LoginItems" / "Agent Office Collector Tray.app"
        return {
            "dir": APP,
            "core": c / "MacOS" / "agent-office-collector",
            "gui": c / "MacOS" / "agent-office-collector-gui",
            "tray": tray_app / "Contents" / "MacOS" / "agent-office-collector-tray",
            "tray_plist": tray_app / "Contents" / "Info.plist",
            "uninstall": c / "Resources" / "agent-office-collector-uninstall",
        }
    b = Path("/usr/bin")
    return {
        "core": b / "agent-office-collector",
        "gui": b / "agent-office-collector-gui",
        "tray": b / "agent-office-collector-tray",
        "uninstall": b / "agent-office-collector-uninstall",
        "menu": Path(
            "/usr/share/applications/io.github.apg0001.agent-office.collector.desktop"
        ),
        "icon": Path(
            "/usr/share/icons/hicolor/256x256/apps/agent-office-collector.png"
        ),
    }


def autostart_location() -> Path | None:
    if sys.platform == "darwin":
        return (
            Path.home()
            / "Library"
            / "LaunchAgents"
            / "io.github.apg0001.agent-office.collector-gui.plist"
        )
    if sys.platform.startswith("linux"):
        base = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")
        return base / "autostart" / "agent-office-collector-gui.desktop"
    return None


def win_run_value() -> str | None:
    import winreg

    try:
        with winreg.OpenKey(
            winreg.HKEY_CURRENT_USER, r"Software\Microsoft\Windows\CurrentVersion\Run"
        ) as k:
            return str(winreg.QueryValueEx(k, "AgentOfficeCollectorGUI")[0])
    except OSError:
        return None


def win_key_exists(path: str) -> bool:
    import winreg

    try:
        winreg.OpenKey(winreg.HKEY_CURRENT_USER, path).Close()
        return True
    except OSError:
        return False


def running(name: str) -> bool:
    return count(name) > 0


def count(name: str) -> int:
    """이 이름의 실행 중인 프로세스 수(명령줄 원문은 보지 않고 개수만)."""
    if sys.platform == "win32":
        # tasklist 출력은 콘솔 코드 페이지(한국어 등)라 바이트로 받아 ASCII 이름만 찾는다.
        p = subprocess.run(
            ["tasklist", "/FI", f"IMAGENAME eq {name}.exe", "/NH", "/FO", "CSV"],
            check=False,
            capture_output=True,
        )
        if p.returncode != 0:
            raise RuntimeError(f"tasklist exit {p.returncode}")
        return p.stdout.lower().count(f'"{name}.exe"'.lower().encode())
    p = subprocess.run(
        ["pgrep", "-f", f"(^|/){name}( |$)"], check=False, capture_output=True
    )
    return len(p.stdout.split())


def wait(cond, timeout=30.0) -> bool:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if cond():
            return True
        time.sleep(0.5)
    return False


def core_service_query() -> list[str]:
    if sys.platform == "win32":
        return ["schtasks", "/Query", "/TN", r"\AgentOffice\Collector"]
    if sys.platform == "darwin":
        return [
            "launchctl",
            "print",
            f"gui/{os.getuid()}/io.github.apg0001.agent-office.collector",
        ]
    return []


def main() -> int:
    if os.environ.get("AGENT_OFFICE_SMOKE_DISPOSABLE") != "1":
        print(
            "일회용 환경에서만 실행합니다(AGENT_OFFICE_SMOKE_DISPOSABLE=1)",
            file=sys.stderr,
        )
        return 2
    version, installers = sys.argv[1], [str(Path(p).resolve()) for p in sys.argv[2:]]
    results: dict[str, object] = {"os": platform.platform(), "arch": platform.machine()}
    failed: list[str] = []
    paths = layout()

    def record(name: str, ok: object) -> None:
        results[name] = bool(ok)
        if not ok:
            failed.append(name)

    def run(
        argv: list[str], timeout: float = 600, check: bool = True, **kw
    ) -> subprocess.CompletedProcess:
        # Core·도구 출력은 UTF-8(한국어)이다. 콘솔 코드 페이지로 해석하지 않는다.
        p = subprocess.run(
            argv,
            check=False,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            **kw,
        )
        if check and p.returncode != 0:
            # 출력에는 경로가 들어갈 수 있어 단계 이름·종료 코드만 남긴다.
            raise RuntimeError(
                f"{Path(argv[0]).name} {argv[1] if len(argv) > 1 else ''} exit {p.returncode}"
            )
        return p

    core, tray = str(paths["core"]), str(paths["tray"])
    claude_settings = Path.home() / ".claude" / "settings.json"
    claude_settings.parent.mkdir(parents=True, exist_ok=True)
    claude_settings.write_text(
        json.dumps({"hooks": {"Stop": [{"matcher": "", "hooks": [USER_HOOK]}]}}),
        encoding="utf-8",
    )
    tray_proc = None
    try:
        # --- 설치 ---------------------------------------------------------------
        if sys.platform == "win32":
            run([installers[0], "/S"])
        elif sys.platform == "darwin":
            run(["sudo", "installer", "-pkg", installers[0], "-target", "/"])
            record(
                "pkg_receipt",
                run(["pkgutil", "--pkg-info", PKG_ID], check=False).returncode == 0,
            )
        else:
            run(["sudo", "apt-get", "install", "-y", "-q", *installers], timeout=900)
        for key in ("core", "gui", "tray", "uninstall", "menu", "icon"):
            if key in paths:
                record(f"installed_{key}", paths[key].is_file())
        if sys.platform == "win32":
            record("uninstall_registered", win_key_exists(WIN_UNINSTALL_KEY))
        if sys.platform == "darwin":
            plist = run(
                ["plutil", "-extract", "LSUIElement", "raw", str(paths["tray_plist"])],
                check=False,
            )
            record("tray_is_menu_bar_only", plist.stdout.strip() == "true")
            record(
                "codesign_verify",
                run(
                    ["codesign", "--verify", "--deep", "--strict", str(APP)],
                    check=False,
                ).returncode
                == 0,
            )
        record("core_version", version in run([core, "version"]).stdout)
        record("tray_version", version in run([tray, "version"]).stdout)

        # --- 사용자 승인 동작(설치 프로그램은 켜지 않음): Hook·Core 서비스·트레이 자동 시작 ----------
        run([core, "hooks", "install", "--tool", "claude", "--yes"])
        settings = json.loads(claude_settings.read_text(encoding="utf-8"))
        record("hooks_installed", "PreToolUse" in settings.get("hooks", {}))
        q = core_service_query()
        if q:
            run([core, "service", "install", "--activate"], timeout=120)
            record("core_service_registered", run(q, check=False).returncode == 0)
        run([tray, "autostart", "install"])
        record(
            "tray_autostart_status",
            run([tray, "autostart", "status"]).stdout.strip() == "installed",
        )
        if sys.platform == "win32":
            v = win_run_value() or ""
            record(
                "tray_autostart_points_to_tray",
                v.lower().endswith('agent-office-collector-tray.exe" --autostart'),
            )
        else:
            loc = autostart_location()
            record(
                "tray_autostart_points_to_tray",
                loc is not None and tray in loc.read_text(encoding="utf-8"),
            )

        # --- 트레이: 실행 유지·단일 실행 ------------------------------------------------
        tray_proc = subprocess.Popen(
            [tray, "--autostart"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
        )
        time.sleep(15)
        record("tray_stays_up_15s", tray_proc.poll() is None)
        second = run([tray], check=False, timeout=60)
        record("tray_single_instance", second.returncode == 3)
        items, client = os.environ.get("AO_SNI_ITEMS"), os.environ.get("AO_SNI_CLIENT")
        if items and client:
            # Linux: 최소 StatusNotifierWatcher에 등록됐는지와 D-Bus 메뉴 클릭(dbusmenu Event)을 확인한다.
            record(
                "sni_registered",
                wait(
                    lambda: (
                        Path(items).exists() and Path(items).read_text().strip() != ""
                    ),
                    20,
                ),
            )
            sender = Path(items).read_text().split()[0]
            layout_out = run(
                [sys.executable, client, sender, "layout"], check=False
            ).stdout
            for label in (
                "사무실 열기",
                "설정 열기",
                "수집 일시중지",
                "수집 재개",
                "업데이트 확인",
                "로그아웃",
                "종료",
            ):
                record(f"sni_menu_has_{label}", label in layout_out)
            run([sys.executable, client, sender, "click", "수집 일시중지"], check=False)
            record(
                "sni_click_pause",
                wait(
                    lambda: (
                        json.loads(run([core, "status", "--json"]).stdout).get("paused")
                        is True
                    ),
                    15,
                ),
            )
            run([sys.executable, client, sender, "click", "수집 재개"], check=False)
            record(
                "sni_click_resume",
                wait(
                    lambda: (
                        json.loads(run([core, "status", "--json"]).stdout).get("paused")
                        is False
                    ),
                    15,
                ),
            )
        tray_proc.terminate()
        try:
            tray_proc.wait(timeout=20)
        except subprocess.TimeoutExpired:
            tray_proc.kill()
        tray_proc = None
        wait(lambda: not running("agent-office-collector-tray"), 20)

        # --- 트레이 셀프 테스트: 실제 systray 메뉴 항목의 클릭 경로로 모든 메뉴 동작 ------------------
        record("no_gui_before_self_test", not running("agent-office-collector-gui"))
        report_path = Path(tempfile.mkdtemp()) / "tray-self-test.json"
        self_test = subprocess.Popen(
            [tray, "--self-test", str(report_path)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        # 셀프 테스트의 "사무실 열기"(서버 없음 → 설정 창)·"설정 열기"가 GUI 창 프로세스를 실제로 띄우는지 본다.
        gui_seen, gui_max, deadline = 0, 0, time.monotonic() + 900
        while self_test.poll() is None and time.monotonic() < deadline:
            n = count("agent-office-collector-gui")
            gui_seen += 1 if n else 0
            gui_max = max(gui_max, n)
            time.sleep(1)
        if self_test.poll() is None:
            self_test.kill()
        st = subprocess.CompletedProcess(self_test.args, self_test.wait())
        results["gui_seen_seconds_during_self_test"] = gui_seen
        record("gui_started_from_tray", gui_seen >= 3)
        # "사무실 열기"와 "설정 열기"가 GUI를 두 번 띄워도 창 프로세스는 하나여야 한다(단일 실행).
        results["gui_max_processes_during_self_test"] = gui_max
        record("gui_single_instance", gui_max == 1)
        report = (
            json.loads(report_path.read_text(encoding="utf-8"))
            if report_path.exists()
            else {}
        )
        results["tray_self_test_steps"] = {
            s["item"]: s["code"] for s in report.get("steps", [])
        }
        results["tray_ready_ms"] = report.get("ready_ms")
        record("tray_self_test_ok", st.returncode == 0 and report.get("ok") is True)
        record(
            "gui_quit_from_tray",
            wait(lambda: not running("agent-office-collector-gui"), 30),
        )
        if sys.platform != "win32" and running("agent-office-collector-gui"):
            # 진단: 남은 GUI가 종료 요청(--quit)으로 뜬 프로세스인지(명령줄 원문은 남기지 않음).
            p = subprocess.run(
                ["pgrep", "-f", "agent-office-collector-gui --quit"],
                check=False,
                capture_output=True,
            )
            results["gui_left_is_quit_request"] = p.returncode == 0
        record(
            "tray_exited_after_quit",
            wait(lambda: not running("agent-office-collector-tray"), 15),
        )

        # --- 제거 -------------------------------------------------------------------
        if sys.platform == "win32":
            run([str(paths["uninstall"]), "/S"])
            record("files_removed", wait(lambda: not paths["dir"].exists(), 90))
            record("start_menu_removed", wait(lambda: not paths["menu"].exists(), 30))
            record("uninstall_unregistered", not win_key_exists(WIN_UNINSTALL_KEY))
            record("tray_autostart_removed", win_run_value() is None)
        else:
            u = run(
                [str(paths["uninstall"]), "--yes"]
                + (["--remove-app"] if sys.platform == "darwin" else []),
                check=False,
            )
            record("uninstall_helper_exit_zero", u.returncode == 0)
            if sys.platform == "darwin":
                record("files_removed", not APP.exists())
                record(
                    "pkg_receipt_forgotten",
                    run(["pkgutil", "--pkg-info", PKG_ID], check=False).returncode != 0,
                )
            else:
                run(
                    [
                        "sudo",
                        "apt-get",
                        "remove",
                        "-y",
                        "-q",
                        "agent-office-collector-desktop",
                        "agent-office-collector",
                    ]
                )
                record(
                    "files_removed",
                    not any(paths[k].exists() for k in ("core", "gui", "tray", "menu")),
                )
            loc = autostart_location()
            record("tray_autostart_removed", loc is not None and not loc.exists())
        if q:
            record("core_service_unregistered", run(q, check=False).returncode != 0)
        record(
            "no_tray_or_gui_left",
            not running("agent-office-collector-tray")
            and not running("agent-office-collector-gui"),
        )
        settings = json.loads(claude_settings.read_text(encoding="utf-8"))
        stop_hooks = [
            h
            for g in settings.get("hooks", {}).get("Stop", [])
            for h in g.get("hooks", [])
        ]
        record("our_hooks_removed", "PreToolUse" not in settings.get("hooks", {}))
        record("user_hook_kept", USER_HOOK in stop_hooks)
    except Exception as exc:  # noqa: BLE001
        failed.append("exception")
        results["exception"] = str(exc)[:200]
    finally:
        if tray_proc is not None and tray_proc.poll() is None:
            tray_proc.kill()
        shutil.rmtree(claude_settings.parent, ignore_errors=True)
    results["failed"] = failed
    print(json.dumps(results, ensure_ascii=False, indent=2))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

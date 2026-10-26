"""Collector 설치 실측 공통 도우미(#180). installer_smoke.py의 배치·프로세스 함수를 다시 쓰고 설치·제거를 더한다.

일회용 환경(CI 러너) 전용이다. 결과에는 단계별 통과 여부·결과 코드·개수만 남긴다(경로·출력 원문·토큰 없음).
"""

from __future__ import annotations

import functools
import http.server
import json
import os
import subprocess
import sys
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from installer_smoke import (
    APP,
    USER_HOOK,
    core_service_query,
    count,
    layout,
    running,
    wait,
)

__all__ = [
    "APP",
    "USER_HOOK",
    "Checks",
    "core_service_query",
    "count",
    "gui_prefs_path",
    "install",
    "json_escaped",
    "layout",
    "legacy_dir",
    "run",
    "running",
    "serve",
    "uninstall",
    "wait",
]


def run(
    argv: list[str], timeout: float = 600, check: bool = True, **kw
) -> subprocess.CompletedProcess:
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


class Checks:
    def __init__(self) -> None:
        self.results: dict[str, object] = {}
        self.failed: list[str] = []

    def record(self, name: str, ok: object) -> None:
        self.results[name] = bool(ok)
        if not ok:
            self.failed.append(name)

    def note(self, name: str, value: object) -> None:
        self.results[name] = value

    def finish(self) -> int:
        self.results["failed"] = self.failed
        print(json.dumps(self.results, ensure_ascii=False, indent=2))
        return 1 if self.failed else 0


def gui_prefs_path() -> Path:
    """GUI 설정(gui.json) 위치(os.UserConfigDir 기준, collector/gui/internal/prefs)."""
    if sys.platform == "win32":
        base = Path(os.environ["APPDATA"])
    elif sys.platform == "darwin":
        base = Path.home() / "Library" / "Application Support"
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")
    return base / "AgentOffice" / "CollectorGUI" / "gui.json"


def legacy_dir() -> Path:
    """이전 명령행 설치 위치(docs/installation/collector-cli.md)."""
    if sys.platform == "win32":
        return Path(os.environ["LOCALAPPDATA"]) / "AgentOffice" / "bin"
    return Path.home() / ".local" / "bin"


def json_escaped(p: str) -> str:
    """JSON 문자열 안에 쓰인 경로 표기(Windows 역슬래시 이스케이프)."""
    return json.dumps(p)[1:-1]


def install(installers: list[str]) -> None:
    if sys.platform == "win32":
        run([installers[0], "/S"])
    elif sys.platform == "darwin":
        run(["sudo", "installer", "-pkg", installers[0], "-target", "/"])
    else:
        run(["sudo", "apt-get", "install", "-y", "-q", *installers], timeout=900)


def uninstall() -> None:
    paths = layout()
    if sys.platform == "win32":
        run([str(paths["uninstall"]), "/S"], check=False)
        wait(lambda: not paths["dir"].exists(), 90)
        return
    run(
        [str(paths["uninstall"]), "--yes"]
        + (["--remove-app"] if sys.platform == "darwin" else []),
        check=False,
    )
    if sys.platform.startswith("linux"):
        run(
            [
                "sudo",
                "apt-get",
                "remove",
                "-y",
                "-q",
                "agent-office-collector-desktop",
                "agent-office-collector",
            ],
            check=False,
        )


class _QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args) -> None:  # 요청 경로를 로그에 남기지 않는다
        pass


def serve(directory: str, port: int) -> http.server.ThreadingHTTPServer:
    """127.0.0.1에서 directory를 내놓는 테스트용 업데이트 서버."""
    handler = functools.partial(_QuietHandler, directory=directory)
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv

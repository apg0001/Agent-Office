"""Collector 실제 OS 스모크(#40~#43): 등록 → 토큰 보관 → Hook 설치 → 수집·전송 → 자동 시작 → 로그아웃 → 제거.

일회용 환경(CI 러너·VM) 전용이다. 실제 사용자 홈의 Claude 설정·OS 보안 저장소·자동 시작을 쓰므로
AGENT_OFFICE_SMOKE_DISPOSABLE=1 이 없으면 실행하지 않는다. 서버는 이 스크립트 안의 가짜 API이며
토큰과 프롬프트는 합성 sentinel이다. 결과는 단계별 통과 여부만 JSON으로 출력한다.

    python tests/cross_os/collector_smoke.py <agent-office-collector 실행 파일> <제거 스크립트>
"""

from __future__ import annotations

import hashlib
import json
import os
import platform
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

TOKEN = "aod_SMOKE_SENTINEL_TOKEN_DO_NOT_SHOW"
PROMPT_SENTINEL = "SMOKE_PROMPT_SENTINEL_7f3a"
DEVICE_ID = "0d6b2a10-7a4c-4b8e-9f00-1234567890ab"
SESSION = "00000000-0000-4000-8000-000000000401"
COUNTS = {"heartbeat": 0, "events": 0, "revoke": 0, "leaks": 0}
USER_HOOK = {"type": "command", "command": "echo smoke-user-hook"}


class Office(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, body=None):
        data = json.dumps(body).encode() if body is not None else b""
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n) if n else b""
        if PROMPT_SENTINEL.encode() in body:
            COUNTS["leaks"] += 1
        if self.path == "/api/v1/devices/register":
            return self._send(201, {"device_id": DEVICE_ID, "device_token": TOKEN})
        if self.headers.get("Authorization") != "Bearer " + TOKEN:
            return self._send(401)
        if self.path == "/api/v1/devices/heartbeat":
            COUNTS["heartbeat"] += 1
            return self._send(204)
        if self.path == "/api/v1/devices/revoke":
            COUNTS["revoke"] += 1
            return self._send(204)
        if self.path == "/api/v1/events/batch":
            ids = [e["event_id"] for e in json.loads(body).get("events", [])]
            COUNTS["events"] += len(ids)
            return self._send(
                200,
                {
                    "schema_version": "1.0",
                    "accepted_event_ids": ids,
                    "duplicate_event_ids": [],
                    "rejected_events": [],
                },
            )
        self._send(404)


def config_dir() -> Path:
    if sys.platform == "win32":
        return Path(os.environ["APPDATA"]) / "AgentOffice" / "Collector"
    if sys.platform == "darwin":
        return (
            Path.home()
            / "Library"
            / "Application Support"
            / "AgentOffice"
            / "Collector"
        )
    base = os.environ.get("XDG_CONFIG_HOME") or str(Path.home() / ".config")
    return Path(base) / "agent-office" / "collector"


def wait(cond, timeout=30.0) -> bool:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if cond():
            return True
        time.sleep(0.5)
    return False


def main() -> int:
    if os.environ.get("AGENT_OFFICE_SMOKE_DISPOSABLE") != "1":
        print(
            "일회용 환경에서만 실행합니다(AGENT_OFFICE_SMOKE_DISPOSABLE=1)",
            file=sys.stderr,
        )
        return 2
    exe, uninstall = sys.argv[1], sys.argv[2]
    server = ThreadingHTTPServer(("127.0.0.1", 0), Office)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_address[1]}"
    results: dict[str, object] = {"os": platform.platform(), "arch": platform.machine()}
    failed: list[str] = []

    def run(*args, stdin="", check=True):
        p = subprocess.run(
            [exe, *args],
            check=False,
            input=stdin,
            capture_output=True,
            text=True,
            timeout=120,
        )
        if check and p.returncode != 0:
            # 출력에는 경로가 들어갈 수 있어 종료 코드만 남긴다.
            raise RuntimeError(
                f"{args[0]} {args[1] if len(args) > 1 else ''} exit {p.returncode}"
            )
        return p

    def status():
        return json.loads(run("status", "--json").stdout)

    def record(name, ok):
        results[name] = bool(ok)
        if not ok:
            failed.append(name)

    claude_settings = Path.home() / ".claude" / "settings.json"
    claude_settings.parent.mkdir(parents=True, exist_ok=True)
    claude_settings.write_text(
        json.dumps({"hooks": {"Stop": [{"matcher": "", "hooks": [USER_HOOK]}]}}),
        encoding="utf-8",
    )
    original = hashlib.sha256(claude_settings.read_bytes()).hexdigest()
    runner = None
    try:
        results["version"] = run("version").stdout.strip()
        run(
            "register",
            "--server",
            url,
            "--code-stdin",
            "--name",
            "SmokePC",
            stdin="ABCD-EFGH\n",
        )
        st = status()
        results["token_storage"] = st.get("token_storage")
        record("registered", st.get("registration") == "registered")
        record("token_in_os_store", st.get("token_storage") == "os_keyring")
        cfg = (config_dir() / "config.json").read_text(encoding="utf-8")
        record("token_absent_from_config", TOKEN not in cfg)

        run("hooks", "install", "--tool", "claude", "--yes")
        settings = json.loads(claude_settings.read_text(encoding="utf-8"))
        stop_hooks = [
            h for g in settings["hooks"].get("Stop", []) for h in g.get("hooks", [])
        ]
        record("hooks_installed", "PreToolUse" in settings["hooks"])
        record("user_hook_kept_on_install", USER_HOOK in stop_hooks)

        runner = subprocess.Popen(
            [exe, "run"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
        )
        record("heartbeat", wait(lambda: COUNTS["heartbeat"] > 0))
        for payload in (
            {
                "hook_event_name": "SessionStart",
                "session_id": SESSION,
                "source": "startup",
            },
            {
                "hook_event_name": "UserPromptSubmit",
                "session_id": SESSION,
                "prompt_id": "00000000-0000-4000-8000-000000000402",
                "prompt": PROMPT_SENTINEL,
                "cwd": str(Path.home() / PROMPT_SENTINEL),
            },
        ):
            p = run("hook", "claude", stdin=json.dumps(payload), check=False)
            record(f"hook_exit_zero_{payload['hook_event_name']}", p.returncode == 0)
        record("events_delivered", wait(lambda: COUNTS["events"] >= 2))
        runner.terminate()
        runner.wait(timeout=30)
        runner = None

        run("service", "install", "--activate")
        record("service_installed", status().get("service") == "installed")
        os_query: list[str] = []
        if sys.platform == "win32":
            os_query = ["schtasks", "/Query", "/TN", r"\AgentOffice\Collector"]
        elif sys.platform == "darwin":
            label = "io.github.apg0001.agent-office.collector"
            os_query = ["launchctl", "print", f"gui/{os.getuid()}/{label}"]
        if os_query:
            q = subprocess.run(os_query, check=False, capture_output=True)
            record("service_registered_in_os", q.returncode == 0)
        before = COUNTS["heartbeat"]
        results["service_heartbeat"] = wait(lambda: COUNTS["heartbeat"] > before, 60)
        run("service", "uninstall", "--activate")
        record("service_removed", status().get("service") != "installed")
        if os_query:
            q = subprocess.run(os_query, check=False, capture_output=True)
            record("service_unregistered_in_os", q.returncode != 0)

        run("logout")
        record("revoked_on_server", COUNTS["revoke"] == 1)
        record(
            "unregistered_after_logout",
            status().get("registration") == "not_registered",
        )
        # 재등록 후 제거 스크립트 --purge로 Hook·토큰·설정을 지운다.
        run(
            "register",
            "--server",
            url,
            "--code-stdin",
            "--name",
            "SmokePC",
            stdin="ABCD-EFGH\n",
        )
        run("hooks", "install", "--tool", "claude", "--yes")
        if sys.platform == "win32":
            cmd = [
                "powershell",
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                uninstall,
                "-Purge",
                "-Yes",
            ]
        else:
            cmd = ["sh", uninstall, "--purge", "--yes"]
        u = subprocess.run(
            cmd, check=False, capture_output=True, text=True, timeout=120
        )
        record("uninstall_exit_zero", u.returncode == 0)
        if u.returncode != 0:
            # 일회용 러너 진단용: 제거 스크립트 오류의 마지막 몇 줄만 남긴다.
            results["uninstall_error_tail"] = (
                (u.stdout + u.stderr).strip().splitlines()[-6:]
            )
        record("config_purged", not config_dir().exists())
        settings = json.loads(claude_settings.read_text(encoding="utf-8"))
        stop_hooks = [
            h
            for g in settings.get("hooks", {}).get("Stop", [])
            for h in g.get("hooks", [])
        ]
        record("our_hooks_removed", "PreToolUse" not in settings.get("hooks", {}))
        record("user_hook_kept_on_uninstall", USER_HOOK in stop_hooks)
        results["settings_byte_identical_after_uninstall"] = (
            hashlib.sha256(claude_settings.read_bytes()).hexdigest() == original
        )
    except Exception as e:  # noqa: BLE001 - 단계 실패를 결과로 남긴다
        failed.append(f"error: {e}")
    finally:
        if runner is not None:
            runner.kill()
        server.shutdown()
    record("no_prompt_leak", COUNTS["leaks"] == 0)
    results["server_counts"] = dict(COUNTS)
    results["failed"] = failed
    print(json.dumps(results, indent=1, ensure_ascii=False))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

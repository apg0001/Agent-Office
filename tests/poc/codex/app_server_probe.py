"""Codex app-server 부착 범위를 원문 없이 관측하는 PoC 하네스(#21).

사용자 상태와 분리한 임시 CODEX_HOME에서만 서버를 띄운다. 모델 호출이 필요한 turn은
보내지 않으므로 로그인 없이 실행된다. 출력에는 메서드 이름, 응답 키, 오류 코드,
실행별 salt HMAC으로 만든 thread 지문만 남긴다.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import queue
import secrets
import socket
import struct
import subprocess
import tempfile
import threading
import time
from collections import Counter
from pathlib import Path
from typing import Any

METHOD_PATTERN_CHARS = set(
    "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/_"
)
SHELL_COMMAND = "true"


def safe_method(value: object) -> str | None:
    if (
        isinstance(value, str)
        and 0 < len(value) <= 80
        and set(value) <= METHOD_PATTERN_CHARS
    ):
        return value
    return None


class Client:
    """JSON-RPC 클라이언트 공통부. 수신 메시지는 메모리에만 두고 요약만 내보낸다."""

    def __init__(self) -> None:
        self.messages: queue.Queue[dict[str, Any]] = queue.Queue()
        self.received: list[dict[str, Any]] = []
        self.next_id = 1

    def _start_reader(self) -> None:
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self) -> None:
        raise NotImplementedError

    def _write_text(self, text: str) -> None:
        raise NotImplementedError

    def _accept(self, text: str | bytes) -> None:
        try:
            item = json.loads(text)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return
        if isinstance(item, dict):
            self.messages.put(item)

    def _drain(self, timeout: float) -> None:
        end = time.monotonic() + timeout
        while True:
            remaining = end - time.monotonic()
            if remaining <= 0:
                return
            try:
                self.received.append(self.messages.get(timeout=remaining))
            except queue.Empty:
                return

    def request(
        self, method: str, params: dict[str, Any], timeout: float = 15
    ) -> dict[str, Any]:
        request_id = self.next_id
        self.next_id += 1
        self._send(
            {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
        )
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            for item in self.received:
                if item.get("id") == request_id and (
                    "result" in item or "error" in item
                ):
                    return item
            self._drain(0.2)
        return {"id": request_id, "timeout": True}

    def notify(self, method: str) -> None:
        self._send({"jsonrpc": "2.0", "method": method})

    def _send(self, item: dict[str, Any]) -> None:
        self._write_text(json.dumps(item))

    def settle(self, seconds: float) -> None:
        self._drain(seconds)

    def notifications(self) -> list[dict[str, Any]]:
        return [m for m in self.received if "method" in m and "id" not in m]

    def close(self) -> None:
        raise NotImplementedError


class StdioClient(Client):
    """`codex app-server` stdio 전송: 줄 단위 JSON."""

    def __init__(self, command: list[str], env: dict[str, str], cwd: Path) -> None:
        super().__init__()
        self.process = subprocess.Popen(
            command,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env=env,
            cwd=cwd,
        )
        self._start_reader()

    def _read(self) -> None:
        assert self.process.stdout is not None
        for line in self.process.stdout:
            self._accept(line)

    def _write_text(self, text: str) -> None:
        assert self.process.stdin is not None
        self.process.stdin.write((text + "\n").encode())
        self.process.stdin.flush()

    def close(self) -> None:
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()


class UnixWebSocketClient(Client):
    """`--listen unix://` 제어 소켓 전송: unix socket 위의 WebSocket 텍스트 프레임."""

    def __init__(self, path: Path) -> None:
        super().__init__()
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(str(path))
        key = base64.b64encode(os.urandom(16)).decode()
        self.sock.sendall(
            (
                "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n"
                "Connection: Upgrade\r\n"
                f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
            ).encode()
        )
        header = b""
        while b"\r\n\r\n" not in header:
            chunk = self.sock.recv(1)
            if not chunk:
                raise ConnectionError("websocket handshake closed")
            header += chunk
        if not header.startswith(b"HTTP/1.1 101"):
            raise ConnectionError("websocket upgrade refused")
        self.lock = threading.Lock()
        self._start_reader()

    def _recv_exact(self, size: int) -> bytes:
        data = b""
        while len(data) < size:
            chunk = self.sock.recv(size - len(data))
            if not chunk:
                raise ConnectionError("socket closed")
            data += chunk
        return data

    def _read(self) -> None:
        fragments = b""
        try:
            while True:
                first, second = self._recv_exact(2)
                opcode = first & 0x0F
                length = second & 0x7F
                if length == 126:
                    length = struct.unpack("!H", self._recv_exact(2))[0]
                elif length == 127:
                    length = struct.unpack("!Q", self._recv_exact(8))[0]
                mask = self._recv_exact(4) if second & 0x80 else b""
                payload = self._recv_exact(length)
                if mask:
                    payload = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
                if opcode == 0x8:
                    return
                if opcode == 0x9:
                    self._frame(0xA, payload)
                    continue
                if opcode in {0x1, 0x0}:
                    fragments += payload
                    if first & 0x80:
                        self._accept(fragments)
                        fragments = b""
        except (ConnectionError, OSError):
            return

    def _frame(self, opcode: int, payload: bytes) -> None:
        mask = os.urandom(4)
        length = len(payload)
        if length < 126:
            header = struct.pack("!BB", 0x80 | opcode, 0x80 | length)
        elif length < 65536:
            header = struct.pack("!BBH", 0x80 | opcode, 0x80 | 126, length)
        else:
            header = struct.pack("!BBQ", 0x80 | opcode, 0x80 | 127, length)
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
        with self.lock:
            self.sock.sendall(header + mask + masked)

    def _write_text(self, text: str) -> None:
        self._frame(0x1, text.encode())

    def close(self) -> None:
        try:
            self._frame(0x8, b"")
        except OSError:
            pass
        self.sock.close()


def fingerprint(value: object, salt: str) -> str | None:
    if not isinstance(value, str) or not value:
        return None
    return hmac.new(salt.encode(), value.encode(), hashlib.sha256).hexdigest()[:16]


def thread_ids(value: object) -> set[str]:
    """응답·알림 안의 thread 식별자 후보만 모은다(값은 지문으로만 내보낸다)."""
    found: set[str] = set()
    if isinstance(value, dict):
        for key, item in value.items():
            if key in {"threadId", "id"} and isinstance(item, str) and len(item) <= 64:
                found.add(item)
            elif key == "thread" and isinstance(item, dict):
                found |= thread_ids({"id": item.get("id")})
            elif isinstance(item, dict | list):
                found |= thread_ids(item)
    elif isinstance(value, list):
        for item in value:
            found |= thread_ids(item)
    return found


def summarize_response(response: dict[str, Any]) -> dict[str, object]:
    if response.get("timeout"):
        return {"status": "timeout"}
    if "error" in response:
        error = response["error"] if isinstance(response["error"], dict) else {}
        code = error.get("code")
        return {"status": "error", "code": code if isinstance(code, int) else None}
    result = response.get("result")
    keys = sorted(result) if isinstance(result, dict) else []
    return {"status": "ok", "result_keys": keys}


def initialize(client: Client) -> dict[str, object]:
    response = client.request(
        "initialize", {"clientInfo": {"name": "agent-office-poc", "version": "0.0.0"}}
    )
    client.notify("initialized")
    return summarize_response(response)


def loaded_contains(client: Client, thread: str) -> dict[str, object]:
    response = client.request("thread/loaded/list", {})
    summary = summarize_response(response)
    result = response.get("result")
    data = result.get("data") if isinstance(result, dict) else None
    summary["loaded_count"] = len(data) if isinstance(data, list) else None
    summary["contains_target"] = thread in thread_ids(data) or thread in (
        data if isinstance(data, list) else []
    )
    return summary


def method_counts(messages: list[dict[str, Any]]) -> dict[str, int]:
    return dict(
        sorted(
            Counter(
                safe_method(m.get("method")) or "other"
                for m in messages
                if "method" in m
            ).items()
        )
    )


def shell_turn(client: Client, thread: str) -> dict[str, object]:
    return summarize_response(
        client.request(
            "thread/shellCommand",
            {"threadId": thread, "command": SHELL_COMMAND, "timeoutMs": 10_000},
        )
    )


def two_client_experiment(
    first: Client, second: Client, workspace: Path, salt: str
) -> dict[str, object]:
    """first가 연 thread를 second가 볼 수 있는지, 합류하면 활동 알림을 받는지 본다."""
    report: dict[str, object] = {
        "initialize": [initialize(first), initialize(second)],
    }
    started = first.request("thread/start", {"cwd": str(workspace), "ephemeral": False})
    report["thread_start"] = summarize_response(started)
    result = started.get("result")
    thread = (
        (result.get("thread") or {}).get("id") if isinstance(result, dict) else None
    )
    if not isinstance(thread, str):
        return report
    report["thread_fingerprint"] = fingerprint(thread, salt)
    first.settle(1)
    second.settle(1)
    report["second_loaded"] = loaded_contains(second, thread)
    report["second_resume_before_first_turn"] = summarize_response(
        second.request("thread/resume", {"threadId": thread})
    )

    # 1단계: 합류 전 활동. second가 무엇을 받는지 본다.
    marker = len(second.received)
    report["first_turn"] = shell_turn(first, thread)
    report["first_name_set"] = summarize_response(
        first.request(
            "thread/name/set", {"threadId": thread, "name": "agent-office-poc"}
        )
    )
    first.settle(4)
    second.settle(4)
    report["second_notifications_before_join"] = method_counts(second.received[marker:])

    # 2단계: rollout이 생긴 뒤 합류하고 다시 활동한다.
    report["second_resume_after_first_turn"] = summarize_response(
        second.request("thread/resume", {"threadId": thread})
    )
    second.settle(1)
    marker = len(second.received)
    first_marker = len(first.received)
    report["second_turn_by_first"] = shell_turn(first, thread)
    first.settle(4)
    second.settle(4)
    report["notifications_after_join"] = {
        "first": method_counts(first.received[first_marker:]),
        "second": method_counts(second.received[marker:]),
    }
    report["second_unsubscribe"] = summarize_response(
        second.request("thread/unsubscribe", {"threadId": thread})
    )
    return report


def stop(process: subprocess.Popen[bytes]) -> None:
    """SIGTERM 후 종료를 기다리고, 늦으면 강제 종료한다(1회 관측: 10초 내 미종료)."""
    process.terminate()
    try:
        process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=10)


def wait_for_socket(path: Path, timeout: float = 20) -> bool:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if path.exists():
            return True
        time.sleep(0.2)
    return False


def run(codex: str) -> dict[str, object]:
    salt = secrets.token_hex(32)
    with tempfile.TemporaryDirectory(prefix="agent-office-appserver-poc-") as temp:
        root = Path(temp)
        home = root / "codex-home"
        workspace = root / "workspace"
        home.mkdir()
        workspace.mkdir()
        subprocess.run(["git", "init", "--quiet"], cwd=workspace, check=True)
        env = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith("CODEX")
        }
        env["CODEX_HOME"] = str(home)
        version = subprocess.run(
            [codex, "--version"], capture_output=True, text=True, env=env, check=False
        ).stdout.strip()
        report: dict[str, object] = {
            "poc_schema_version": "1.0",
            "codex_version": version,
        }

        # A. 같은 서버 프로세스(unix socket)에 두 클라이언트가 붙는다.
        socket_path = root / "app-server.sock"
        server = subprocess.Popen(
            [codex, "app-server", "--listen", f"unix://{socket_path}"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            env=env,
            cwd=workspace,
        )
        try:
            if wait_for_socket(socket_path):
                first: Client = UnixWebSocketClient(socket_path)
                second: Client = UnixWebSocketClient(socket_path)
                try:
                    report["same_server"] = two_client_experiment(
                        first, second, workspace, salt
                    )
                finally:
                    first.close()
                    second.close()
            else:
                report["same_server"] = {"status": "socket_not_created"}
        finally:
            stop(server)

        # B. 서로 다른 서버 프로세스(stdio)에 각각 붙는다.
        first_stdio: Client = StdioClient([codex, "app-server"], env, workspace)
        second_stdio: Client = StdioClient([codex, "app-server"], env, workspace)
        try:
            report["separate_servers"] = two_client_experiment(
                first_stdio, second_stdio, workspace, salt
            )
        finally:
            first_stdio.close()
            second_stdio.close()
        return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--codex", default="codex")
    parser.add_argument("--confirm-synthetic-run", action="store_true")
    arguments = parser.parse_args()
    if not arguments.confirm_synthetic_run:
        parser.error("--confirm-synthetic-run is required")
    print(json.dumps(run(arguments.codex), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

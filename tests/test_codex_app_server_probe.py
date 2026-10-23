import base64
import hashlib
import json
import socket
import struct
import sys
import tempfile
import threading
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests" / "poc" / "codex"))

import app_server_probe as probe

SENTINEL = "SENSITIVE_SYNTHETIC_VALUE_MUST_NOT_APPEAR"
WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"


def test_response_summary_keeps_only_keys_and_codes() -> None:
    ok = probe.summarize_response(
        {
            "id": 1,
            "result": {"thread": {"id": SENTINEL, "cwd": SENTINEL}, "model": SENTINEL},
        }
    )
    error = probe.summarize_response(
        {"id": 2, "error": {"code": -32600, "message": f"no rollout for {SENTINEL}"}}
    )

    assert ok == {"status": "ok", "result_keys": ["model", "thread"]}
    assert error == {"status": "error", "code": -32600}
    assert probe.summarize_response({"timeout": True}) == {"status": "timeout"}
    assert SENTINEL not in json.dumps([ok, error])


def test_method_counts_reject_unexpected_method_text() -> None:
    counts = probe.method_counts(
        [
            {"method": "turn/started", "params": {"threadId": SENTINEL}},
            {"method": f"x {SENTINEL} y"},
            {"method": "a" * 81},
            {"id": 3, "result": {}},
        ]
    )

    assert counts == {"other": 2, "turn/started": 1}


def test_thread_ids_are_found_but_only_exported_as_fingerprints() -> None:
    params = {
        "thread": {"id": "thread-1", "name": SENTINEL},
        "item": {"threadId": "thread-2"},
    }

    found = probe.thread_ids(params)
    exported = probe.fingerprint("thread-1", "salt")

    assert {"thread-1", "thread-2"} <= found
    assert exported is not None and "thread-1" not in exported
    assert len(exported) == 16


def _fake_websocket_server(
    path: Path, outgoing: dict[str, object], received: list[str]
) -> None:
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    server.bind(str(path))
    server.listen(1)
    connection, _ = server.accept()
    request = b""
    while b"\r\n\r\n" not in request:
        request += connection.recv(1)
    key = next(
        line.split(":", 1)[1].strip()
        for line in request.decode().split("\r\n")
        if line.lower().startswith("sec-websocket-key")
    )
    accept = base64.b64encode(hashlib.sha1((key + WS_GUID).encode()).digest()).decode()
    connection.sendall(
        (
            "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n"
            f"Connection: Upgrade\r\nSec-WebSocket-Accept: {accept}\r\n\r\n"
        ).encode()
    )
    payload = json.dumps(outgoing).encode()
    # 126 이상이면 16비트 확장 길이를 쓴다. 서버 프레임은 마스킹하지 않는다.
    connection.sendall(struct.pack("!BBH", 0x81, 126, len(payload)) + payload)
    first, second = connection.recv(2)
    length = second & 0x7F
    mask = connection.recv(4)
    data = b""
    while len(data) < length:
        data += connection.recv(length - len(data))
    received.append(bytes(b ^ mask[i % 4] for i, b in enumerate(data)).decode())
    assert first == 0x81 and second & 0x80
    connection.close()
    server.close()


@pytest.mark.skipif(
    not hasattr(socket, "AF_UNIX"),
    reason="이 Python에는 socket.AF_UNIX가 없다(Windows). 제어 소켓 실측은 Linux 기준(#21, #88)",
)
def test_unix_websocket_client_frames_round_trip() -> None:
    outgoing = {
        "jsonrpc": "2.0",
        "method": "turn/started",
        "params": {"pad": "x" * 200},
    }
    received: list[str] = []
    with tempfile.TemporaryDirectory(prefix="ao-ws-") as temp:
        path = Path(temp) / "s.sock"
        server = threading.Thread(
            target=_fake_websocket_server, args=(path, outgoing, received), daemon=True
        )
        server.start()
        assert probe.wait_for_socket(path, timeout=5)
        client = probe.UnixWebSocketClient(path)
        client.notify("initialized")
        client.settle(1)
        server.join(timeout=5)
        client.close()

    assert client.notifications() == [outgoing]
    assert json.loads(received[0]) == {"jsonrpc": "2.0", "method": "initialized"}

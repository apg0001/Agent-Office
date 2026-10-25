#!/usr/bin/env python3
"""합성 OpenAI Responses API(SSE) 서버. 자격 증명 없이 `codex exec` 한 턴을 끝까지 돌린다(#170).

    python3 -I mock_responses.py <port> <log>

- 첫 요청: `exec_command`로 `true`를 한 번 실행하라는 function_call
- function_call_output이 있는 요청: 어시스턴트 메시지 "done"
- WebSocket 업그레이드(GET)는 404로 거절해 Codex가 HTTPS 전송으로 내려가게 한다.

로그에는 경로·메서드·function_call_output 존재 여부만 남긴다(요청 본문·프롬프트 없음).
"""

import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = sys.argv[2]


def log(rec):
    with open(LOG, "a", encoding="utf-8") as f:
        f.write(json.dumps(rec) + "\n")


def sse(events):
    return "".join(
        f"event: {ev['type']}\ndata: {json.dumps(ev)}\n\n" for ev in events
    ).encode()


def completed(rid):
    usage = {
        "input_tokens": 0,
        "input_tokens_details": None,
        "output_tokens": 0,
        "output_tokens_details": None,
        "total_tokens": 0,
    }
    return {"type": "response.completed", "response": {"id": rid, "usage": usage}}


COUNT = {"n": 0}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _empty(self, code):
        self.send_response(code)
        self.send_header("content-length", "0")
        self.end_headers()

    def do_GET(self):
        log({"method": "GET", "path": self.path.split("?")[0]})
        self._empty(404)

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("content-length") or 0))
        path = self.path.split("?")[0]
        if not path.endswith("/responses"):
            log({"method": "POST", "path": path})
            self._empty(404)
            return
        try:
            req = json.loads(body)
        except ValueError:
            req = {}
        items = req.get("input", []) if isinstance(req, dict) else []
        has_output = any(
            isinstance(i, dict) and i.get("type") == "function_call_output"
            for i in items
        )
        COUNT["n"] += 1
        rid = f"resp_{COUNT['n']}"
        log({"method": "POST", "path": path, "function_call_output": has_output})
        if has_output:
            item = {
                "type": "message",
                "role": "assistant",
                "id": rid,
                "content": [{"type": "output_text", "text": "done"}],
            }
        else:
            item = {
                "type": "function_call",
                "call_id": f"call_{COUNT['n']}",
                "name": "exec_command",
                "arguments": json.dumps({"cmd": "true"}),
            }
        data = sse(
            [
                {"type": "response.created", "response": {"id": rid}},
                {"type": "response.output_item.done", "item": item},
                completed(rid),
            ]
        )
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()

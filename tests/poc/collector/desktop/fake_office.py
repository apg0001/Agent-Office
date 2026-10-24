"""트레이 검증용 최소 Agent Office API(등록·heartbeat·배치·폐기). 토큰은 합성 sentinel."""

import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

TOKEN = "aod_SENTINEL_TOKEN_tray_e2e_DO_NOT_SHOW"
COUNTS = {"heartbeat": 0, "events": 0, "revoke": 0}


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, body=None):
        data = json.dumps(body).encode() if body is not None else b""
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/counts":
            return self._send(200, COUNTS)
        if self.path == "/api/v1/system/health":
            return self._send(200, {"status": "ok"})
        self._send(404)

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n) if n else b""
        if self.path == "/api/v1/devices/register":
            return self._send(
                201,
                {
                    "device_id": "0d6b2a10-7a4c-4b8e-9f00-1234567890ab",
                    "device_token": TOKEN,
                },
            )
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


HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()

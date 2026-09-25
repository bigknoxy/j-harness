#!/usr/bin/env python3
"""Minimal OpenAI-compatible stub for scheduled container E2E.

This is a test fixture, not part of the harness. It answers
POST /v1/chat/completions with a deterministic triage classification so the
nightly job can drive a real container over real HTTP without a model server.

Usage:
    python3 scripts/stub_llm.py [port]

Env:
    STUB_PORT   port to listen on (default 11500)
    STUB_HOST   interface to bind (default 127.0.0.1; use 0.0.0.0 so a
                container can reach it through the Docker host gateway)
"""

import json
import os
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def classify(text: str) -> dict:
    low = text.lower()
    if re.search(r"charge|invoice|refund|billing|bill", low):
        return {"category": "billing", "priority": "high"}
    if re.search(r"crash|error|bug|technical|stack trace|outage", low):
        return {"category": "technical", "priority": "normal"}
    return {"category": "other", "priority": "low"}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):  # keep CI logs quiet
        pass

    def _send(self, code: int, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):  # noqa: N802 (http.server API)
        if not self.path.endswith("/chat/completions"):
            self._send(404, {"error": {"message": "not found"}})
            return
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            self._send(400, {"error": {"message": "bad json"}})
            return

        messages = req.get("messages") or []
        text = ""
        for m in messages:
            if m.get("role") == "user":
                text = m.get("content") or ""
        content = json.dumps(classify(text))

        self._send(
            200,
            {
                "id": "chatcmpl-stub",
                "object": "chat.completion",
                "model": req.get("model", "stub"),
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": content},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
            },
        )

    def do_GET(self):  # noqa: N802
        if self.path == "/healthz":
            self._send(200, {"status": "ok"})
            return
        self._send(404, {"error": {"message": "not found"}})


def main() -> int:
    port = int(os.environ.get("STUB_PORT") or (sys.argv[1] if len(sys.argv) > 1 else 11500))
    host = os.environ.get("STUB_HOST") or "127.0.0.1"
    server = ThreadingHTTPServer((host, port), Handler)
    print(f"stub llm listening on {host}:{port}", flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

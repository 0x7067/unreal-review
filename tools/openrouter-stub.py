#!/usr/bin/env python3
"""Local OpenRouter stand-in for the offline canary.

Binds 127.0.0.1 and answers every request with HTTP 401 and a stable
Unauthorized body. Prints the origin on stdout, then serves until killed.
The request log path is the first argument.
"""

import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = "127.0.0.1"
BODY = b'{"error":{"message":"Unauthorized","code":401}}\n'


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self._reply()

    def do_POST(self):
        self._reply()

    def _reply(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        host = self.headers.get("Host", "")
        with open(sys.argv[1], "a", encoding="utf-8") as log:
            log.write(f"{self.command} {self.path} host={host}\n")
        self.close_connection = True
        self.send_response(401)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, fmt, *args):
        return


def main():
    if len(sys.argv) != 2:
        print("usage: openrouter-stub.py <request-log>", file=sys.stderr)
        return 2
    server = ThreadingHTTPServer((HOST, 0), Handler)
    print(f"http://{HOST}:{server.server_address[1]}", flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

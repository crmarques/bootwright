"""Environment-labelled HTTP peer for the isolated SNO acceptance case."""

from __future__ import annotations

import http.server
import json
import os


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({
            "environment": os.environ["ENVIRONMENT"],
            "source": self.client_address[0],
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    if os.environ.get("ENVIRONMENT") not in {"staging", "production"}:
        raise SystemExit("ENVIRONMENT must be staging or production")
    http.server.ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()

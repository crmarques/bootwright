"""Sample server of the lab-5-nodes-ceph-networks case.

It answers HTTP on the user network only and stores each request body in one
of three Ceph stores reached over the Ceph public network:

    PUT|GET|DELETE /block/<name>    a file on XFS over a kernel-mapped RBD image
    PUT|GET|DELETE /file/<name>     a file on a kernel-mounted CephFS subvolume
    PUT|GET|DELETE /object/<name>   an S3 object behind the RGW ingress VIP
    GET /healthz                    store readiness and the source address
                                    this host uses towards each Ceph endpoint

Run by app-setup.sh as `python3 -I sample-server.py /etc/e2e-sample/config.json`
on RHEL 9 (Python 3.9, standard library only).
"""

from __future__ import annotations

import datetime
import hashlib
import hmac
import http.client
import http.server
import json
import os
import re
import socket
import ssl
import sys
import urllib.parse

MAX_BODY = 16 * 1024 * 1024
NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")


def load_config(path):
    with open(path, encoding="utf-8") as handle:
        config = json.load(handle)
    with open(config["s3"]["credentialsFile"], encoding="utf-8") as handle:
        config["s3"]["credentials"] = json.load(handle)
    return config


def source_address(destination):
    probe = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        probe.connect((destination, 9))
        return probe.getsockname()[0]
    finally:
        probe.close()


class PinnedHTTPSConnection(http.client.HTTPSConnection):
    """Dial the ingress VIP while verifying and naming the endpoint FQDN."""

    def __init__(self, host, address, context, timeout):
        super().__init__(host, 443, timeout=timeout, context=context)
        self.address = address
        self.tls = context

    def connect(self):
        raw = socket.create_connection((self.address, 443), self.timeout)
        self.sock = self.tls.wrap_socket(raw, server_hostname=self.host)


class ObjectStore:
    def __init__(self, s3):
        self.host = s3["host"]
        self.address = s3["vip"]
        self.bucket = s3["bucket"]
        self.region = s3["region"]
        self.access = s3["credentials"]["accessKey"]
        self.secret = s3["credentials"]["secretKey"]
        self.context = ssl.create_default_context(cafile=s3["caFile"])

    def _key(self, datestamp):
        key = ("AWS4" + self.secret).encode()
        for part in (datestamp, self.region, "s3", "aws4_request"):
            key = hmac.new(key, part.encode(), hashlib.sha256).digest()
        return key

    def authorization(self, method, path, headers, payload, amz_date):
        """The SigV4 Authorization value over every header given, empty query."""
        datestamp = amz_date[:8]
        names = sorted(header.lower() for header in headers)
        values = {header.lower(): str(value).strip() for header, value in headers.items()}
        signed = ";".join(names)
        canonical = "\n".join(
            [method, path, "", "".join(f"{name}:{values[name]}\n" for name in names), signed, payload]
        )
        scope = f"{datestamp}/{self.region}/s3/aws4_request"
        to_sign = "\n".join(
            ["AWS4-HMAC-SHA256", amz_date, scope, hashlib.sha256(canonical.encode()).hexdigest()]
        )
        signature = hmac.new(self._key(datestamp), to_sign.encode(), hashlib.sha256).hexdigest()
        return (
            f"AWS4-HMAC-SHA256 Credential={self.access}/{scope}, "
            f"SignedHeaders={signed}, Signature={signature}"
        )

    def request(self, method, name, body=b""):
        now = datetime.datetime.now(datetime.timezone.utc)
        amz_date = now.strftime("%Y%m%dT%H%M%SZ")
        payload = hashlib.sha256(body).hexdigest()
        path = "/" + urllib.parse.quote(self.bucket)
        if name:
            path += "/" + urllib.parse.quote(name, safe="-_.~")
        headers = {"host": self.host, "x-amz-content-sha256": payload, "x-amz-date": amz_date}
        headers["authorization"] = self.authorization(method, path, headers, payload, amz_date)
        connection = PinnedHTTPSConnection(self.host, self.address, self.context, 30)
        try:
            connection.request(method, path, body=body or None, headers=headers)
            response = connection.getresponse()
            return response.status, response.read()
        except http.client.HTTPException as error:
            raise OSError(f"S3 {method} failed: {error!r}") from error
        finally:
            connection.close()

    def ready(self):
        status, _ = self.request("HEAD", "")
        return status == 200

    def put(self, name, body):
        status, answer = self.request("PUT", name, body)
        if status != 200:
            raise OSError(f"S3 PUT answered {status}: {answer[:200]!r}")

    def get(self, name):
        status, answer = self.request("GET", name)
        if status == 404:
            raise FileNotFoundError(name)
        if status != 200:
            raise OSError(f"S3 GET answered {status}")
        return answer

    def delete(self, name):
        status, _ = self.request("DELETE", name)
        if status not in (200, 204):
            raise OSError(f"S3 DELETE answered {status}")


class DirectoryStore:
    def __init__(self, root):
        self.root = root

    def ready(self):
        return os.path.ismount(self.root)

    def put(self, name, body):
        target = os.path.join(self.root, name)
        staging = target + ".partial"
        with open(staging, "wb") as handle:
            handle.write(body)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(staging, target)
        directory = os.open(self.root, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def get(self, name):
        with open(os.path.join(self.root, name), "rb") as handle:
            return handle.read()

    def delete(self, name):
        os.unlink(os.path.join(self.root, name))


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "e2e-sample/1"
    stores = {}
    config = {}

    def _reply(self, status, document=None, body=None):
        payload = body if body is not None else json.dumps(document, sort_keys=True).encode() + b"\n"
        self.send_response(status)
        self.send_header("Content-Type", "application/octet-stream" if body is not None else "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def _target(self):
        parts = self.path.split("?", 1)[0].strip("/").split("/")
        if len(parts) != 2 or parts[0] not in self.stores or not NAME.match(parts[1]):
            self._reply(404, {"error": "use /block/<name>, /file/<name> or /object/<name>"})
            return None, None
        return parts[0], parts[1]

    def do_GET(self):
        if self.path == "/healthz":
            self._health()
            return
        store, name = self._target()
        if store is None:
            return
        try:
            body = self.stores[store].get(name)
        except FileNotFoundError:
            self._reply(404, {"error": f"{store}/{name} not found"})
            return
        except OSError as error:
            self._reply(502, {"error": f"{store} read failed: {error}"})
            return
        self._reply(200, body=body)

    def do_PUT(self):
        store, name = self._target()
        if store is None:
            return
        try:
            length = int(self.headers.get("Content-Length", "-1"))
        except ValueError:
            length = -1
        if length < 0 or length > MAX_BODY:
            self._reply(413, {"error": f"send a Content-Length from 0 to {MAX_BODY}"})
            return
        body = self.rfile.read(length)
        try:
            self.stores[store].put(name, body)
        except OSError as error:
            self._reply(502, {"error": f"{store} write failed: {error}"})
            return
        self._reply(201, {"store": store, "name": name, "bytes": len(body),
                          "sha256": hashlib.sha256(body).hexdigest()})

    def do_DELETE(self):
        store, name = self._target()
        if store is None:
            return
        try:
            self.stores[store].delete(name)
        except FileNotFoundError:
            self._reply(404, {"error": f"{store}/{name} not found"})
            return
        except OSError as error:
            self._reply(502, {"error": f"{store} delete failed: {error}"})
            return
        self._reply(200, {"store": store, "name": name, "deleted": True})

    def _health(self):
        ready = {}
        for store, backend in sorted(self.stores.items()):
            try:
                ready[store] = bool(backend.ready())
            except OSError as error:
                ready[store] = f"error: {error}"
        sources = {"mon": {}, "objectVIP": {}}
        for monitor in self.config["monitors"]:
            sources["mon"][monitor] = source_address(monitor)
        vip = self.config["s3"]["vip"]
        sources["objectVIP"][vip] = source_address(vip)
        healthy = all(value is True for value in ready.values())
        self._reply(200 if healthy else 503, {
            "ready": ready,
            "listen": f"{self.config['listen']}:{self.config['port']}",
            "sourceAddress": sources,
        })

    def log_message(self, format, *args):
        sys.stderr.write("%s %s\n" % (self.address_string(), format % args))


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: sample-server.py CONFIG")
    config = load_config(sys.argv[1])
    Handler.config = config
    Handler.stores = {
        "block": DirectoryStore(config["blockDir"]),
        "file": DirectoryStore(config["fileDir"]),
        "object": ObjectStore(config["s3"]),
    }
    server = http.server.ThreadingHTTPServer((config["listen"], config["port"]), Handler)
    sys.stderr.write(f"listening on {config['listen']}:{config['port']}\n")
    server.serve_forever()


if __name__ == "__main__":
    main()

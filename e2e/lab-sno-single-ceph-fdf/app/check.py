"""Exercise external Ceph through FDF without printing object credentials."""

import datetime
import hashlib
import hmac
import os
import pathlib
import time
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, target):
        raise RuntimeError("the RGW endpoint redirected a signed request")


def s3_request(method, content=b""):
    host = os.environ["BUCKET_HOST"]
    port = int(os.environ["BUCKET_PORT"])
    authority = f"{host}:{port}"
    path = "/" + urllib.parse.quote(os.environ["BUCKET_NAME"], safe="") + "/fdf-check"
    now = datetime.datetime.now(datetime.timezone.utc)
    stamp, day = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
    payload = hashlib.sha256(content).hexdigest()
    headers = {"host": authority, "x-amz-content-sha256": payload, "x-amz-date": stamp}
    names = ";".join(sorted(headers))
    canonical = "\n".join([method, path, "", "".join(f"{key}:{headers[key]}\n" for key in sorted(headers)), names, payload])
    scope = day + "/us-east-1/s3/aws4_request"
    sign = "\n".join(["AWS4-HMAC-SHA256", stamp, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    key = ("AWS4" + os.environ["AWS_SECRET_ACCESS_KEY"]).encode()
    for part in (day, "us-east-1", "s3", "aws4_request"):
        key = hmac.new(key, part.encode(), hashlib.sha256).digest()
    signature = hmac.new(key, sign.encode(), hashlib.sha256).hexdigest()
    headers["Authorization"] = (f"AWS4-HMAC-SHA256 Credential={os.environ['AWS_ACCESS_KEY_ID']}/{scope}, "
                                f"SignedHeaders={names}, Signature={signature}")
    request = urllib.request.Request(f"http://{authority}{path}", data=content if method == "PUT" else None, headers=headers, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(request, timeout=30) as response:
        return response.read(1024)


def check():
    for volume in ("block", "file"):
        marker = pathlib.Path("/" + volume + "/fdf-check")
        wanted = ("persisted-" + volume + "\n").encode()
        if os.environ["CHECK_MODE"] == "write":
            with marker.open("wb") as handle:
                handle.write(wanted)
                handle.flush()
                os.fsync(handle.fileno())
        if marker.read_bytes() != wanted:
            raise RuntimeError(volume + " persistence failed")
    if os.environ["CHECK_OBJECT"] == "true":
        wanted = b"persisted-object\n"
        if os.environ["CHECK_MODE"] == "write":
            s3_request("PUT", wanted)
        if s3_request("GET") != wanted:
            raise RuntimeError("object persistence failed")
    pathlib.Path("/tmp/passed").touch()
    print("RBD and CephFS persistence passed; object=" + os.environ["CHECK_OBJECT"], flush=True)
    while True:
        time.sleep(60)


if __name__ == "__main__":
    check()

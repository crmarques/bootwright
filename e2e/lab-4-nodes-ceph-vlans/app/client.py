"""Client of the lab-4-nodes-ceph-vlans sample server.

Runs in a container on the controller host, on the host network, and talks to
the sample server over the user network only. Standard library only:

    podman run --rm -i --network host IMAGE python3 - URL MODE [EXPECTED_SOURCE] < client.py

MODE is one of
    roundtrip     for each store: put 4 KiB, 1 MiB and 8 MiB of random bytes,
                  read each back and compare, overwrite one, delete them and
                  require a 404 afterwards
    write-canary  put a deterministic canary in every store and keep it
    check-canary  read the canaries back and compare
    clean-canary  delete the canaries

EXPECTED_SOURCE, when given, is the address the server must use towards every
MON and the object VIP: its Ceph public address.
"""

import hashlib
import json
import os
import sys
import urllib.error
import urllib.request

STORES = ("block", "file", "object")
SIZES = (4096, 1024 * 1024, 8 * 1024 * 1024)
CANARY = "canary-lab-4-nodes-ceph-vlans"


def call(base, method, path, body=None):
    request = urllib.request.Request(base + path, data=body, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(request, timeout=120) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()
    except (urllib.error.URLError, OSError) as error:
        return 0, str(error).encode()


def canary_bytes(store):
    seed = hashlib.sha256(f"{CANARY}/{store}".encode()).digest()
    return b"".join(hashlib.sha256(seed + i.to_bytes(4, "big")).digest() for i in range(8192))


def check(results, label, condition, detail=""):
    results.append((label, condition))
    print(f"{'PASS' if condition else 'FAIL'}  {label}{'  ' + detail if detail else ''}")


def health(base, expected_source, results):
    status, body = call(base, "GET", "/healthz")
    print(body.decode(errors="replace").strip())
    check(results, "healthz answers 200 with every store ready", status == 200, f"HTTP {status}")
    if expected_source and status in (200, 503):
        document = json.loads(body)
        for kind, sources in sorted(document["sourceAddress"].items()):
            for target, source in sorted(sources.items()):
                check(results, f"source towards {kind} {target} is {expected_source}",
                      source == expected_source, f"got {source}")


def roundtrip(base, results):
    for store in STORES:
        names = []
        for size in SIZES:
            name = f"rt-{size}-{os.urandom(4).hex()}"
            names.append(name)
            data = os.urandom(size)
            status, body = call(base, "PUT", f"/{store}/{name}", data)
            digest = hashlib.sha256(data).hexdigest()
            stored = status == 201 and json.loads(body).get("sha256") == digest
            check(results, f"{store} put {size} B", stored, f"HTTP {status}")
            status, body = call(base, "GET", f"/{store}/{name}")
            check(results, f"{store} get {size} B matches", status == 200 and body == data, f"HTTP {status}")
        replacement = os.urandom(65536)
        call(base, "PUT", f"/{store}/{names[0]}", replacement)
        status, body = call(base, "GET", f"/{store}/{names[0]}")
        check(results, f"{store} overwrite reads the new bytes", status == 200 and body == replacement)
        for name in names:
            status, _ = call(base, "DELETE", f"/{store}/{name}")
            check(results, f"{store} delete {name}", status == 200, f"HTTP {status}")
            status, _ = call(base, "GET", f"/{store}/{name}")
            check(results, f"{store} {name} is gone", status == 404, f"HTTP {status}")


def canary(base, mode, results):
    for store in STORES:
        data = canary_bytes(store)
        path = f"/{store}/{CANARY}"
        if mode == "write-canary":
            status, _ = call(base, "PUT", path, data)
            check(results, f"{store} canary written", status == 201, f"HTTP {status}")
        elif mode == "check-canary":
            status, body = call(base, "GET", path)
            check(results, f"{store} canary intact", status == 200 and body == data, f"HTTP {status}")
        else:
            status, _ = call(base, "DELETE", path)
            check(results, f"{store} canary deleted", status == 200, f"HTTP {status}")


def main():
    if len(sys.argv) not in (3, 4):
        sys.exit("usage: client.py URL roundtrip|write-canary|check-canary|clean-canary [EXPECTED_SOURCE]")
    base, mode = sys.argv[1].rstrip("/"), sys.argv[2]
    expected_source = sys.argv[3] if len(sys.argv) == 4 else ""
    if mode not in ("roundtrip", "write-canary", "check-canary", "clean-canary"):
        sys.exit(f"unknown mode {mode}")
    results = []
    health(base, expected_source, results)
    if mode == "roundtrip":
        roundtrip(base, results)
    else:
        canary(base, mode, results)
    failed = [label for label, passed in results if not passed]
    print(f"\n{len(results) - len(failed)} passed, {len(failed)} failed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()

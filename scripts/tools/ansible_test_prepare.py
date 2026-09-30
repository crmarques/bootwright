"""Acquire the reviewed Ansible sanity artifacts into a development cache.

The artifacts include the managed-host floor interpreter, whose archive the
sanity gate verifies and extracts on each run to import the collection's
modules under the oldest Python a managed host may run.
"""

import hashlib
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener


def cache_root(root: Path) -> Path:
    """Return the shared check cache that scripts/cache-dir names."""
    completed = subprocess.run(
        [str(root / "scripts/cache-dir")], check=True, capture_output=True, text=True
    )
    return Path(completed.stdout.strip())


def asset_host(hostname) -> bool:
    """Return whether a host is GitHub's release asset content host."""
    return hostname is not None and hostname.endswith(".githubusercontent.com")


class PublisherRedirect(HTTPRedirectHandler):
    """Keep the artifact acquisition within its declared HTTPS publisher.

    A GitHub release asset is served from its content host, so a release may
    also move to a githubusercontent.com host; no other publisher may.
    """

    def redirect_request(self, request, response, code, message, headers, new_url):
        original, target = urlsplit(request.full_url), urlsplit(new_url)
        if (
            target.scheme != "https"
            or not (
                target.hostname == original.hostname
                or (
                    (original.hostname == "github.com" or asset_host(original.hostname))
                    and asset_host(target.hostname)
                )
            )
            or target.username is not None
            or target.password is not None
        ):
            raise ValueError("Unexpected artifact publisher redirect")
        return super().redirect_request(
            request, response, code, message, headers, new_url
        )


def main() -> None:
    if len(sys.argv) > 2:
        raise SystemExit("Usage: ansible_test_prepare.py [cache-directory]")
    root = Path(__file__).resolve().parents[2]
    destination = (
        Path(sys.argv[1])
        if len(sys.argv) == 2
        else cache_root(root) / "ansible-test-artifacts"
    )
    destination.mkdir(parents=True, exist_ok=True)
    artifacts = json.loads(
        (root / "scripts/tools/ansible-test-artifacts.json").read_text()
    )
    artifacts.append(
        json.loads(
            (root / "scripts/tools/ansible-check-floor-interpreter.json").read_text()
        )
    )
    opener = build_opener(PublisherRedirect())
    for artifact in artifacts:
        target = destination / artifact["filename"]
        if target.exists():
            data = target.read_bytes()
        else:
            with opener.open(artifact["url"], timeout=60) as response:
                data = response.read(artifact["bytes"] + 1)
        if (
            len(data) != artifact["bytes"]
            or hashlib.sha256(data).hexdigest() != artifact["sha256"]
        ):
            raise SystemExit("Ansible test artifact does not match its lock")
        if not target.exists():
            with target.open("xb") as stream:
                stream.write(data)
    print(f"Verified {len(artifacts)} Ansible test artifacts")


if __name__ == "__main__":
    main()

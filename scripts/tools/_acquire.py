"""Acquire one pinned artifact for a development gate.

The bootstrap and the Ansible artifact preparation each fetch a file by URL,
hold it to the size and SHA-256 its lock pins and keep it only on a match. This
owns that rule and the one publisher redirect policy, so a hardening lands once.
It uses the standard library alone because the bootstrap runs on a bare host.
"""

import hashlib
from pathlib import Path
import subprocess
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
    """Keep an acquisition within its declared HTTPS publisher.

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


def acquire(pin, target: Path, label: str, timeout: int, opener=None) -> None:
    """Verify the file at target against its pin, fetching it first if absent.

    The pin names the url, bytes and sha256. A fetched file is written only
    after it matches, so a refused download leaves nothing behind.
    """
    if target.exists():
        data = target.read_bytes()
    else:
        opener = opener or build_opener(PublisherRedirect())
        with opener.open(pin["url"], timeout=timeout) as response:
            data = response.read(pin["bytes"] + 1)
    if len(data) != pin["bytes"] or hashlib.sha256(data).hexdigest() != pin["sha256"]:
        raise SystemExit(f"{label} does not match its lock")
    if not target.exists():
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("xb") as stream:
            stream.write(data)

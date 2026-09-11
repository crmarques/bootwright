"""Materialize the pinned Ansible check interpreter and its locked tools.

The gate needs an exact CPython release that a development host is not required
to provide. This acquires the reviewed interpreter, verifies it against its
lock, and builds an isolated environment holding only the reviewed tool closure.
Any host Python new enough to run this script can perform that bootstrap.
"""

import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, build_opener


class PublisherRedirect(HTTPRedirectHandler):
    """Keep the interpreter acquisition within its declared HTTPS publisher."""

    def redirect_request(self, request, response, code, message, headers, new_url):
        original, target = urlsplit(request.full_url), urlsplit(new_url)
        if (
            target.scheme != "https"
            or target.username is not None
            or target.password is not None
            or not (
                target.hostname == original.hostname
                or target.hostname.endswith(".githubusercontent.com")
            )
        ):
            raise ValueError("Unexpected interpreter publisher redirect")
        return super().redirect_request(
            request, response, code, message, headers, new_url
        )


def acquire(lock, archive):
    if archive.exists():
        data = archive.read_bytes()
    else:
        opener = build_opener(PublisherRedirect())
        with opener.open(lock["url"], timeout=300) as response:
            data = response.read(lock["bytes"] + 1)
    if (
        len(data) != lock["bytes"]
        or hashlib.sha256(data).hexdigest() != lock["sha256"]
    ):
        raise SystemExit("Ansible check interpreter does not match its lock")
    if not archive.exists():
        archive.parent.mkdir(parents=True, exist_ok=True)
        with archive.open("xb") as stream:
            stream.write(data)


def main() -> int:
    if len(sys.argv) != 1:
        raise SystemExit("scripts/tools/ansible_check_bootstrap.py takes no arguments")
    if sys.version_info < (3, 12):
        raise SystemExit("The bootstrap requires CPython 3.12 or newer to extract safely")

    root = Path(__file__).resolve().parents[2]
    tools = root / "scripts/tools"
    area = root / ".cache/ansible-check"
    interpreter = area / "venv/bin/python"
    lock = json.loads((tools / "ansible-check-interpreter.json").read_text())

    if not interpreter.exists():
        archive = area / "interpreter.tar.gz"
        acquire(lock, archive)
        runtime = area / "runtime"
        if not (runtime / lock["member"]).exists():
            with tarfile.open(archive) as bundle:
                # "data" refuses absolute paths, parent traversal, links that
                # escape the destination, and device or setuid members.
                bundle.extractall(runtime, filter="data")
        subprocess.run(
            [str(runtime / lock["member"]), "-I", "-m", "venv", str(area / "venv")],
            check=True,
        )

    version = subprocess.run(
        [str(interpreter), "-I", "-c", "import sys; print('.'.join(map(str, sys.version_info[:3])))"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if version != lock["version"]:
        raise SystemExit(
            f"Ansible check interpreter is {version}, not the pinned {lock['version']}"
        )

    subprocess.run(
        [
            str(interpreter), "-I", "-m", "pip", "install", "--quiet",
            "--require-hashes", "--only-binary=:all:",
            "-r", str(tools / "ansible-requirements.txt"),
        ],
        check=True,
    )
    subprocess.run(
        [sys.executable, str(tools / "ansible_test_prepare.py")], check=True
    )
    print(f"Prepared the Ansible check interpreter at {interpreter}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(failure.returncode) from None

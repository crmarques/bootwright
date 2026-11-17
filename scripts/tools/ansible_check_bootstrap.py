"""Materialize the pinned Ansible check interpreter and its locked tools.

The gate needs an exact CPython release that a development host is not required
to provide. This acquires the reviewed interpreter, verifies it against its
lock, and builds an isolated environment holding only the reviewed tool closure.
Any host Python new enough to run this script can perform that bootstrap.
"""

import json
from pathlib import Path
import subprocess
import sys
import tarfile

from _acquire import acquire, cache_root


def main() -> int:
    if len(sys.argv) != 1:
        raise SystemExit("scripts/tools/ansible_check_bootstrap.py takes no arguments")
    if sys.version_info < (3, 12):
        raise SystemExit("The bootstrap requires CPython 3.12 or newer to extract safely")

    root = Path(__file__).resolve().parents[2]
    tools = root / "scripts/tools"
    area = cache_root(root) / "ansible-check"
    interpreter = area / "venv/bin/python"
    lock = json.loads((tools / "ansible-check-interpreter.json").read_text())

    if not interpreter.exists():
        archive = area / "interpreter.tar.gz"
        acquire(lock, archive, "Ansible check interpreter", 300)
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

"""Materialize the pinned Ansible check interpreter and its locked tools.

The gate needs an exact CPython release that a development host is not required
to provide. This acquires the reviewed interpreter, verifies it against its
lock, and builds an isolated environment holding only the reviewed tool closure.
Any host Python new enough to run this script can perform that bootstrap.
"""

import fcntl
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile

from _acquire import acquire, cache_root


LOCK_FILES = (
    "ansible-check-interpreter.json",
    "ansible-requirements.txt",
    "ansible-check-floor-interpreter.json",
    "ansible-test-artifacts.json",
)


def environment_area(tools: Path, cache: Path) -> Path:
    key = hashlib.sha256()
    for name in LOCK_FILES:
        key.update(hashlib.sha256((tools / name).read_bytes()).digest())
    return cache / "ansible-check" / "environments" / key.hexdigest()


def prepare(tools: Path, area: Path, interpreter: Path) -> None:
    lock = json.loads((tools / LOCK_FILES[0]).read_text())

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
        [sys.executable, str(tools / "ansible_test_prepare.py")],
        check=True,
        stdout=sys.stderr,
    )


def resolve(tools: Path, cache: Path) -> Path:
    area = environment_area(tools, cache)
    interpreter = area / "venv/bin/python"
    ready = area / "ready"
    area.mkdir(parents=True, exist_ok=True)
    # The artifacts are shared, so serialize materialization across lock sets.
    with (cache / "ansible-check" / "bootstrap.lock").open("a") as guard:
        fcntl.flock(guard, fcntl.LOCK_EX)
        if not (ready.is_file() and interpreter.is_file()):
            prepare(tools, area, interpreter)
            ready.touch()
    return interpreter


def main() -> int:
    args = sys.argv[1:]
    if args not in ([], ["--print-python"]):
        raise SystemExit("Usage: ansible_check_bootstrap.py [--print-python]")
    if sys.version_info < (3, 12):
        raise SystemExit("The bootstrap requires CPython 3.12 or newer to extract safely")
    root = Path(__file__).resolve().parents[2]
    interpreter = resolve(root / "scripts/tools", cache_root(root))
    if args:
        print(interpreter)
    else:
        print(f"Prepared the Ansible check interpreter at {interpreter}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(failure.returncode) from None

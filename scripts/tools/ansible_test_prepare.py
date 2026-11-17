"""Acquire the reviewed Ansible sanity artifacts into a development cache.

The artifacts include the managed-host floor interpreter, whose archive the
sanity and units gates verify and extract on each run to import the
collection's modules and run their unit tests under the oldest Python a
managed host may run.
"""

import json
from pathlib import Path
import sys

from _acquire import acquire, cache_root


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
    for artifact in artifacts:
        acquire(
            artifact, destination / artifact["filename"], "Ansible test artifact", 60
        )
    print(f"Verified {len(artifacts)} Ansible test artifacts")


if __name__ == "__main__":
    main()

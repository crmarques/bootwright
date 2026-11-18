"""Run the shipped controller playbook over the real runner protocol.

This drives `playbooks/controller/setup.yml` and the `controller_prerequisites`
role exactly as Go does: one frozen `controller-prerequisites-v1` request, the
result channel on descriptor 3 and the authorization channel on descriptor 4.
The request selects one synthetic client whose source is already retained in a
private temporary tool area, so the run acquires nothing over the network,
changes no host package and never executes the published file. A replay of the
same request must verify the published bytes and report no change.
"""

import hashlib
import os
from pathlib import Path
import stat
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "setup_entry_point"))
import entry_point  # noqa: E402  pylint: disable=wrong-import-position

CONTENT = b"synthetic client fixture"
SOURCE = "tool-kubectl-" + "a" * 32 + "-v1.36.1"
MEMBER = "tools/kubectl/v1.36.1/kubectl"
IDENTITY = "a" * 64


def request(area_root):
    identity = os.stat(area_root)
    return {
        "bootwright_controller_request": {
            "version": "controller-prerequisites-v1",
            "identity": IDENTITY,
            "publicationBundle": {
                "path": str(area_root),
                "device": identity.st_dev,
                "inode": identity.st_ino,
                "writable": True,
                "sealed": False,
            },
            "tools": [
                {
                    "kind": "kubectl",
                    "version": "v1.36.1",
                    "compatibility": "",
                    "archive": "binary",
                    "source": {
                        "id": SOURCE,
                        "url": "https://downloads.example.test/exact",
                        "sha256": hashlib.sha256(CONTENT).hexdigest(),
                        "bytes": len(CONTENT),
                    },
                    "files": [{"member": "kubectl", "path": MEMBER}],
                }
            ],
            "acquisition": [{"source": SOURCE, "seconds": 121}],
            "egress": {"httpProxy": "", "httpsProxy": "", "noProxy": []},
            "trust": {"host": "", "file": ""},
        }
    }


def check(record):
    """Require the preparation and completion evidence of the one tool."""
    if record["preparation"] != {"addedSources": [SOURCE]}:
        raise entry_point.Failure("the preparation did not announce exactly the request's source")
    evidence = record["evidence"]
    if evidence["request"] != IDENTITY or evidence["postcondition"] is not True:
        raise entry_point.Failure("completion evidence is not attributable to the request")
    if len(evidence["tools"]) != 1:
        raise entry_point.Failure("completion evidence does not name the one tool")
    return evidence["tools"]


class Setup(entry_point.EntryPoint):
    """The controller stage, whose preparation the completed record carries."""

    def run(self, variables):
        messages, changed = super().run(variables)
        phases = [message["phase"] for message in messages]
        if phases != ["loaded", "prepared", "continue", "completed"]:
            raise entry_point.Failure(f"unexpected protocol sequence: {phases}")
        messages[-1] = dict(messages[-1], preparation=messages[1]["preparation"])
        return messages, changed


with tempfile.TemporaryDirectory(prefix="bootwright-prerequisites-") as work:
    root = Path(work, "area")
    root.mkdir(mode=0o700)
    Path(root, "sources").mkdir(mode=0o700)
    retained = Path(root, "sources", SOURCE)
    retained.write_bytes(CONTENT)
    retained.chmod(0o600)

    entry = Setup(work, "controller/setup.yml", group="bootwright_controller", host="controller")
    entry_point.replay(entry, request(root), check)
    published = Path(root, MEMBER)
    if published.read_bytes() != CONTENT or stat.S_IMODE(published.stat().st_mode) != 0o700:
        raise SystemExit("the published client does not hold the retained bytes")
print("PASS controller playbook published one tool over the v6 runner protocol")

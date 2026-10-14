"""Run the shipped controller playbook over the real runner protocol.

This drives `playbooks/controller/setup.yml` and the `controller_prerequisites`
role exactly as Go does: one frozen `controller-prerequisites-v6` request, the
result channel on descriptor 3 and the authorization channel on descriptor 4.
The request selects one synthetic client whose source is already retained in a
private temporary tool area, so the run acquires nothing over the network,
changes no host package and never executes the published file. A replay of the
same request must verify the published bytes and report no change.
"""

import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile

candidates = [
    Path(value) / "ansible_collections/bootwright/core"
    for value in os.environ.get("ANSIBLE_COLLECTIONS_PATH", "").split(os.pathsep)
    if value
]
candidates.append(Path(__file__).resolve().parents[4])
collection = next(
    path
    for path in candidates
    if (path / "playbooks/controller/setup.yml").is_file()
)
interpreter = os.environ.get("ANSIBLE_TEST_PYTHON_INTERPRETER", sys.executable)

CONTENT = b"synthetic client fixture"
SOURCE = "tool-kubectl-" + "a" * 32 + "-v1.36.1"
MEMBER = "tools/kubectl/v1.36.1/kubectl"
IDENTITY = "a" * 64


def run(area, area_root):
    """Run the playbook once and return the protocol records it wrote."""
    identity = os.stat(area_root)
    request = {
        "version": "controller-prerequisites-v6",
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
    }
    inventory = {
        "all": {
            "children": {
                "bootwright_controller": {
                    "hosts": {
                        "controller": {
                            "ansible_connection": "local",
                            "ansible_python_interpreter": interpreter,
                            "ansible_host": "localhost",
                        }
                    }
                }
            }
        }
    }
    Path(area, "inventory.json").write_text(json.dumps(inventory), encoding="utf-8")
    Path(area, "request.json").write_text(
        json.dumps({"bootwright_controller_request": request}), encoding="utf-8"
    )

    result_read, result_write = os.pipe()
    authorization_read, authorization_write = os.pipe()

    def wire():
        os.dup2(result_write, 3)
        os.dup2(authorization_read, 4)

    child = subprocess.Popen(  # pylint: disable=consider-using-with
        [
            interpreter,
            "-I",
            "-m",
            "ansible.cli.playbook",
            "-i",
            str(Path(area, "inventory.json")),
            "--extra-vars",
            "@" + str(Path(area, "request.json")),
            str(collection / "playbooks/controller/setup.yml"),
        ],
        cwd=area,
        env={
            "PATH": "/usr/bin:/usr/sbin",
            "HOME": area,
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "TMPDIR": area,
            "ANSIBLE_COLLECTIONS_PATH": os.environ.get("ANSIBLE_COLLECTIONS_PATH", ""),
            "ANSIBLE_NOCOLOR": "1",
            "ANSIBLE_LOCAL_TEMP": str(Path(area, "local")),
            "ANSIBLE_REMOTE_TEMP": str(Path(area, "remote")),
        },
        preexec_fn=wire,  # pylint: disable=subprocess-popen-preexec-fn
        # The close pass runs before preexec_fn, so the destination descriptors
        # must be kept explicitly or the remap lands on closed numbers.
        pass_fds=(3, 4, result_write, authorization_read),
    )
    os.close(result_write)
    os.close(authorization_read)

    messages = []
    with os.fdopen(result_read, "rb") as stream:
        for line in stream:
            message = json.loads(line)
            messages.append(message)
            if message["phase"] in ("loaded", "prepared", "continue"):
                os.write(authorization_write, b"proceed\n")
    os.close(authorization_write)
    if child.wait() != 0:
        raise SystemExit(f"controller playbook exited {child.returncode}")
    return messages


def check(messages, outcome):
    """Require the v6 sequence, its preparation and completion evidence."""
    phases = [message["phase"] for message in messages]
    if phases != ["loaded", "prepared", "continue", "completed"]:
        raise SystemExit(f"unexpected protocol sequence: {phases}")
    if messages[1]["preparation"] != {"addedSources": [SOURCE]}:
        raise SystemExit("the preparation did not announce exactly the request's source")
    completion = messages[-1]
    evidence = completion["evidence"]
    if completion["outcome"] != outcome:
        raise SystemExit(f"the run reported {completion['outcome']}, want {outcome}")
    if evidence["request"] != IDENTITY or evidence["postcondition"] is not True:
        raise SystemExit("completion evidence is not attributable to the request")
    if len(evidence["tools"]) != 1:
        raise SystemExit("completion evidence does not name the one tool")
    return evidence["tools"]


with tempfile.TemporaryDirectory(prefix="bootwright-prerequisites-") as work:
    root = Path(work, "area")
    root.mkdir(mode=0o700)
    Path(root, "sources").mkdir(mode=0o700)
    retained = Path(root, "sources", SOURCE)
    retained.write_bytes(CONTENT)
    retained.chmod(0o600)

    first = check(run(work, root), "changed")
    published = Path(root, MEMBER)
    if published.read_bytes() != CONTENT or stat.S_IMODE(published.stat().st_mode) != 0o700:
        raise SystemExit("the published client does not hold the retained bytes")
    second = check(run(work, root), "unchanged")
    if first != second:
        raise SystemExit("a replay changed the tool evidence")
print("PASS controller playbook published one tool over the v6 runner protocol")

"""Run the shipped controller setup entrypoint over the real runner protocol.

This drives `playbooks/controller/setup.yml` and the `controller_prerequisites`
role exactly as Go does: one frozen request, the result channel on descriptor 3
and the authorization channel on descriptor 4. The request selects no native
transition and no target client, so the run observes the host inventory and
completes without changing a single package.
"""

import json
import os
from pathlib import Path
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

release = ""
for line in Path("/etc/os-release").read_text(encoding="utf-8").splitlines():
    if line.startswith("VERSION_ID="):
        release = line.split("=", 1)[1].strip().strip('"')
    if line.startswith("ID="):
        distribution = line.split("=", 1)[1].strip().strip('"')
if distribution not in ("fedora", "rhel"):
    raise SystemExit("current OS does not provide a qualified native backend")

with tempfile.TemporaryDirectory(prefix="bootwright-prerequisites-") as area:
    bundle = Path(area, "bundle")
    bundle.mkdir(mode=0o700)
    identity = os.stat(bundle)
    request = {
        "version": "controller-prerequisites-v4",
        "operation": "setup",
        "identity": "a" * 64,
        "platform": {
            "os": distribution,
            "release": release,
            "architecture": "amd64",
        },
        "bundle": {
            "path": str(bundle),
            "device": identity.st_dev,
            "inode": identity.st_ino,
            "writable": True,
            "sealed": False,
        },
        "publicationBundle": {
            "path": str(bundle),
            "device": identity.st_dev,
            "inode": identity.st_ino,
            "writable": True,
            "sealed": False,
        },
        "packages": [],
        "native": None,
        "tools": [],
        "acquisition": [],
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
        raise SystemExit(f"controller setup playbook exited {child.returncode}")

phases = [message["phase"] for message in messages]
if phases != ["loaded", "prepared", "completed"]:
    raise SystemExit(f"unexpected protocol sequence: {phases}")

completion = messages[-1]
evidence = completion["evidence"]
if completion["outcome"] != "unchanged":
    raise SystemExit(f"a no-transition request reported {completion['outcome']}")
if evidence["request"] != "a" * 64 or evidence["postcondition"] is not True:
    raise SystemExit("completion evidence is not attributable to the request")
if evidence["added"] != [] or evidence["tools"] != [] or evidence["planDigest"] != "":
    raise SystemExit("a no-transition request claimed dependency effects")
if evidence["before"] != evidence["after"]:
    raise SystemExit("the observed inventory changed without an approved action")
print("PASS controller setup entrypoint completed over the runner protocol")

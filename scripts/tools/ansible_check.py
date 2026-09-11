"""Run repository Ansible checks with the exact reviewed Python tool closure."""

from importlib.metadata import PackageNotFoundError, version
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main() -> int:
    if sys.version_info[:3] != (3, 13, 15):
        raise SystemExit(
            "Ansible checks require the pinned CPython 3.13.15 interpreter."
        )
    if len(sys.argv) != 1:
        raise SystemExit(
            "scripts/ansible-check does not accept partial check controls."
        )

    root = Path(__file__).resolve().parents[2]
    requirements = root / "scripts/tools/ansible-requirements.txt"
    for line in requirements.read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("#"):
            continue
        requirement = line.split(" ", 1)[0]
        name, expected = requirement.split("==", 1)
        try:
            actual = version(name)
        except PackageNotFoundError:
            actual = None
        if actual != expected:
            raise SystemExit(
                "Ansible check dependencies differ from the reviewed lock. "
                "Install scripts/tools/ansible-requirements.txt into an isolated "
                "CPython 3.13.15 environment with pip --require-hashes, then set "
                "BOOTWRIGHT_ANSIBLE_CHECK_PYTHON to that interpreter."
            )

    executable = Path(sys.executable)
    automation = root / "ansible"
    collection = automation / "collections/ansible_collections/bootwright/core"
    fixtures = Path(
        os.environ.get(
            "BOOTWRIGHT_ANSIBLE_TEST_ARTIFACTS",
            str(root / ".cache/ansible-test-artifacts"),
        )
    )
    with tempfile.TemporaryDirectory(prefix="bootwright-ansible-check-") as temporary:
        area = Path(temporary)
        inventory = area / "inventory.json"
        inventory.write_text(
            json.dumps(
                {
                    "all": {
                        "hosts": {
                            "bootwright_controller": {
                                "ansible_connection": "local",
                                "ansible_python_interpreter": str(executable),
                            }
                        }
                    }
                }
            ),
            encoding="utf-8",
        )
        environment = {
            "PATH": os.pathsep.join([str(executable.parent), "/usr/bin", "/bin"]),
            "HOME": str(area),
            "XDG_CACHE_HOME": str(area / "cache"),
            "TMPDIR": str(area),
            "LC_ALL": "C.UTF-8",
            "LANG": "C.UTF-8",
            "ANSIBLE_CONFIG": str(automation / "ansible.cfg"),
            "ANSIBLE_COLLECTIONS_PATH": str(automation / "collections"),
            "ANSIBLE_ROLES_PATH": str(collection / "roles"),
            "ANSIBLE_INVENTORY": str(inventory),
            "ANSIBLE_LOCAL_TEMP": str(area / "ansible-local"),
            "ANSIBLE_REMOTE_TEMP": str(area / "ansible-remote"),
            "ANSIBLE_NOCOLOR": "1",
        }
        playbooks = sorted((collection / "playbooks").rglob("*.yml"))
        if not playbooks:
            raise SystemExit(
                "No shipped Ansible playbooks were found for syntax checking."
            )
        for playbook in playbooks:
            subprocess.run(
                [
                    str(executable),
                    "-I",
                    "-m",
                    "ansible.cli.playbook",
                    "--syntax-check",
                    "-i",
                    str(inventory),
                    str(playbook),
                ],
                cwd=root,
                env=environment,
                check=True,
            )
        subprocess.run(
            [
                str(executable),
                "-I",
                "-m",
                "ansiblelint",
                "--offline",
                "--project-dir",
                str(automation),
                str(automation),
            ],
            cwd=root,
            env=environment,
            check=True,
        )
        test_collection = area / "collections/ansible_collections/bootwright/core"
        shutil.copytree(
            collection,
            test_collection,
            ignore=shutil.ignore_patterns("__pycache__", ".pytest_cache", "output"),
        )
        wheelhouse = area / "wheels"
        wheelhouse.mkdir()
        artifacts = json.loads(
            (root / "scripts/tools/ansible-test-artifacts.json").read_text()
        )
        for artifact in artifacts:
            try:
                data = (fixtures / artifact["filename"]).read_bytes()
            except OSError as failure:
                raise SystemExit(
                    "Pinned Ansible test artifacts are missing; run "
                    "scripts/tools/ansible_test_prepare.py first."
                ) from failure
            if (
                len(data) != artifact["bytes"]
                or hashlib.sha256(data).hexdigest() != artifact["sha256"]
            ):
                raise SystemExit("Ansible test artifacts differ from their lock")
            (wheelhouse / artifact["filename"]).write_bytes(data)
        pip_cache = area / ".ansible/test/cache"
        pip_cache.mkdir(parents=True)
        shutil.copyfile(wheelhouse / "get_pip_25_2.py", pip_cache / "get_pip_25_2.py")
        pip_configuration = area / ".config/pip/pip.conf"
        pip_configuration.parent.mkdir(parents=True)
        pip_configuration.write_text(
            "[global]\nno-index = true\nfind-links = " + str(wheelhouse) + "\n"
        )
        environment.update(
            PIP_NO_INDEX="1",
            PIP_FIND_LINKS=str(wheelhouse),
            ANSIBLE_COLLECTIONS_PATH=str(area / "collections"),
            ANSIBLE_TEST_CONTENT_ROOT=str(test_collection),
        )
        ansible_test = [str(executable), "-I", str(executable.parent / "ansible-test")]
        suites = [
            ["sanity"],
            ["units", "--num-workers", "0"],
            ["integration", "controller_tool"],
            ["integration", "controller_supervisor"],
        ]
        # controller_native builds an RPM and drives the running host's package
        # manager, so it is an operator-run harness rather than part of the
        # unitary gate.
        if os.environ.get("BOOTWRIGHT_ANSIBLE_NATIVE_TARGET") == "1":
            suites.append(["integration", "controller_native"])
            suites.append(["integration", "controller_prerequisites"])
        for arguments in suites:
            subprocess.run(
                ansible_test
                + arguments
                + ["--local", "--python", "3.13", "--color", "no"],
                cwd=test_collection,
                env=environment,
                check=True,
            )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(failure.returncode) from None

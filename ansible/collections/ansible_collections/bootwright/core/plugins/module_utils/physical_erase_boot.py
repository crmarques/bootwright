"""Dedicated erase-only Anaconda entrypoint; it never returns to installation."""

from __future__ import annotations

import base64
import inspect
import json
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_runtime
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity

RAM = "/run/bootwright-erase"


def kickstart(network, request, private_key, authorized_key):
    files = {
        "physical_erase_worker.py": inspect.getsource(physical_erase_worker),
        "bounded_process.py": inspect.getsource(bounded_process),
        "physical_machine_identity.py": inspect.getsource(physical_machine_identity),
        "runtime.py": inspect.getsource(physical_erase_runtime),
        "request.json": json.dumps(request),
        "host-key": private_key,
        "authorized_keys": "restrict " + authorized_key.strip() + "\n",
    }
    lines = [
        network.rstrip(),
        "%pre --erroronfail --interpreter=/bin/bash",
        "umask 077",
        "fail_closed() { trap '' HUP INT TERM; while :; do sleep 60; done; }",
        "trap fail_closed EXIT HUP INT TERM",
        "set -euo pipefail",
        "test ! -e /run/bootwright-erase",
        "mkdir -m 0700 /run/bootwright-erase",
    ]
    for name, data in files.items():
        encoded = base64.b64encode(data.encode()).decode()
        lines.append("base64 --decode > " + RAM + "/" + name + " <<'BOOTWRIGHT_ERASE_DATA'")
        lines.extend((encoded, "BOOTWRIGHT_ERASE_DATA"))
    lines.extend(
        (
            "cat > /run/systemd/system/bootwright-erase-sshd.service <<'BOOTWRIGHT_ERASE_SSH_UNIT'",
            "[Unit]",
            "Description=Bootwright RAM receipt transport",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/sbin/sshd -D -f /run/bootwright-erase/sshd_config",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_SSH_UNIT",
            "cat > /run/systemd/system/bootwright-erase.service <<'BOOTWRIGHT_ERASE_UNIT'",
            "[Unit]",
            "Description=Bootwright authorized offline logical overwrite",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/bin/python3 /run/bootwright-erase/runtime.py",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_UNIT",
            "systemctl daemon-reload",
            "systemctl start --no-block bootwright-erase.service",
            "fail_closed",
            "%end",
            "",
        )
    )
    return "\n".join(lines)

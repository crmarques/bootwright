"""Bounded bridge to the supplied platform's maintained DNF implementation."""

from __future__ import annotations

import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile


def unique_object(pairs):
    result = {}
    folded = set()
    for key, value in pairs:
        if key.lower() in folded:
            raise ValueError("duplicate native result key")
        folded.add(key.lower())
        result[key] = value
    return result


def invoke(request, scratch, mutation=False):
    """DNF is a provided OS interface; no private Python ABI imports leak to it."""
    platform = request["platform"]
    if platform["architecture"] != "amd64" or platform["os"] not in (
        "fedora",
        "rhel",
    ):
        raise ValueError("native platform")
    executable = "/usr/bin/python3"
    if platform["os"] == "rhel":
        executable = "/usr/bin/python3.9"
    helper = Path(__file__).with_name("native_resolution.py")
    data = json.dumps(request, separators=(",", ":"), ensure_ascii=True).encode()
    if len(data) > 4 << 20:
        raise ValueError("native request limit")
    info = os.stat(scratch, follow_symlinks=False)
    if (
        not stat.S_ISDIR(info.st_mode)
        or info.st_uid != os.geteuid()
        or stat.S_IMODE(info.st_mode) != 0o700
    ):
        raise ValueError("native scratch authority")
    with tempfile.TemporaryFile(dir=scratch) as output:
        # This controller-side bridge must retain child lifetime after native
        # authorization; there is no remote AnsibleModule/run_command instance.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(
            [executable, "-I", "-B", str(helper), scratch],
            stdin=subprocess.PIPE,
            stdout=output,
            stderr=subprocess.DEVNULL,
            cwd=scratch,
            env={"LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "PATH": "/usr/bin:/usr/sbin"},
            close_fds=True,
        )
        try:
            # Once native effects are authorized, vendor package hooks must finish
            # before the invocation supervisor releases the workspace authority.
            process.communicate(data, timeout=None if mutation else 120)
        except subprocess.TimeoutExpired as exc:
            process.kill()
            process.wait()
            raise ValueError("native inspection deadline") from exc
        if process.returncode != 0:
            raise ValueError("native operation refused")
        output.seek(0)
        result = output.read((16 << 20) + 1)
        if len(result) > 16 << 20:
            raise ValueError("native result limit")
    value = json.loads(result, object_pairs_hook=unique_object)
    if not isinstance(value, dict):
        raise ValueError("native result")
    return value


def inspection(platform, plan=None):
    with tempfile.TemporaryDirectory(prefix="bootwright-dnf-inspect-") as scratch:
        return invoke(
            {"operation": "inspect", "platform": platform, "plan": plan}, scratch
        )

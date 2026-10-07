"""Bounded bridge to the supplied platform's maintained DNF implementation."""

from __future__ import annotations

import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile


# The closed classes the native helper names its refusal by.
NATIVE_CLASSES = frozenset(
    (
        "solver-conflict",
        "missing-candidate",
        "signature",
        "database",
        "transaction",
        "postcondition",
        "foundation",
        "timeout",
        "internal",
    )
)
MAX_DIAGNOSTIC = 4096
MAX_DETAIL = 200


class NativeRefused(ValueError):
    """The native helper refused, with its closed class and one bounded line
    of its exception for the private run output only."""

    def __init__(self, reason, detail=""):
        super().__init__("native operation refused")
        self.reason = reason
        self.detail = detail


def helper_refusal(data):
    """The refusal the helper's stderr names: 'refused <class>' on its first
    line, or internal, and its second line bounded as the detail."""
    lines = data[:MAX_DIAGNOSTIC].split(b"\n")
    first = lines[0].decode("ascii", "replace")
    reason = first[len("refused "):] if first.startswith("refused ") else ""
    if reason not in NATIVE_CLASSES:
        reason = "internal"
    text = lines[1].decode("utf-8", "replace") if len(lines) > 1 else ""
    text = "".join(
        character
        for character in text
        if not (ord(character) < 32 or 127 <= ord(character) < 160)
    )
    return NativeRefused(
        reason, text.encode("utf-8")[:MAX_DETAIL].decode("utf-8", "ignore")
    )


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
    with tempfile.TemporaryFile(dir=scratch) as output, tempfile.TemporaryFile(
        dir=scratch
    ) as diagnostic:
        # This controller-side bridge must retain child lifetime after native
        # authorization; there is no remote AnsibleModule/run_command instance.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(
            [executable, "-I", "-B", str(helper), scratch],
            stdin=subprocess.PIPE,
            stdout=output,
            stderr=diagnostic,
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
            raise NativeRefused("timeout") from exc
        if process.returncode != 0:
            diagnostic.seek(0)
            raise helper_refusal(diagnostic.read(MAX_DIAGNOSTIC))
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

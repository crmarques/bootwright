"""Apply one frozen native solver transaction through the supplied DNF API."""

from __future__ import annotations

import os
from pathlib import Path
import tempfile
import time

from ansible.plugins.action import ActionBase
from ansible.utils.display import Display
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    MAX_DEADLINE,
    AcquisitionRefused,
    Bundle,
    Routes,
    download,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_native import (
    invoke,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_refusal import (
    name,
)


def staging_bound(packages, acquisition, staging):
    """Each package's frozen acquisition entry, in the packages' order, and the
    staging bound in seconds, which is zero exactly when there is nothing to
    stage."""
    if [entry["source"] for entry in acquisition[: len(packages)]] != [
        package["source"]["id"] for package in packages
    ]:
        raise ValueError("native acquisition")
    if (
        not isinstance(staging, int)
        or isinstance(staging, bool)
        or not 0 <= staging <= MAX_DEADLINE
        or (staging == 0) != (not packages)
    ):
        raise ValueError("native staging")
    return acquisition[: len(packages)]


def stage_payloads(packages, acquisition, staging, egress, scratch):
    """Stream each approved package into its own scratch file under its own
    frozen acquisition deadline, all of them under the staging bound, so no
    package is ever held whole in memory, and name each file by its source.
    One pool serves each route the packages take."""
    entries = staging_bound(packages, acquisition, staging)
    payloads, total = {}, 0
    deadline = time.monotonic() + staging

    def expired(source):
        if time.monotonic() > deadline:
            raise AcquisitionRefused(
                "native acquisition deadline", reason="timeout", source=source["id"]
            )

    with Routes() as routes:
        for package, entry in zip(packages, entries):
            source = package["source"]
            expired(source)
            total += source["bytes"]
            if (
                not 0 < source["bytes"] <= 256 << 20
                or total > 4 << 30
                or source["id"] in payloads
            ):
                raise ValueError("native source limit")
            destination = Path(scratch) / (str(len(payloads)) + ".rpm")
            descriptor = os.open(
                destination,
                os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                0o600,
            )
            try:
                download(source, egress, descriptor, entry["seconds"], routes)
                os.fsync(descriptor)
            except AcquisitionRefused as refusal:
                refusal.source = source["id"]
                raise
            finally:
                os.close(descriptor)
            payloads[source["id"]] = str(destination)
        if packages:
            expired(packages[-1]["source"])
    return payloads


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        bundle = None
        try:
            if set(self._task.args) != {"request"}:
                raise ValueError("request")
            request = self._task.args["request"]
            plan = request["native"]
            if (
                request["operation"] not in ("setup", "recover")
                or plan is None
                or not 0 < len(plan["actions"]) <= 512
            ):
                raise ValueError("native authority")
            bundle = Bundle(request["publicationBundle"])
            bundle.writable()
            with tempfile.TemporaryDirectory(prefix="bootwright-dnf-apply-") as scratch:
                payloads = stage_payloads(
                    plan["packages"],
                    request["acquisition"],
                    request["nativeStaging"],
                    request["egress"],
                    scratch,
                )
                bundle.writable()
                emit({"phase": "native"}, acknowledge=True)
                proof = invoke(
                    {
                        "operation": "apply",
                        "platform": request["platform"],
                        "requirements": plan["requirements"],
                        "versions": plan["requests"],
                        "egress": request["egress"],
                        "plan": plan,
                        "payloads": payloads,
                    },
                    scratch,
                    mutation=True,
                )
                if proof != {
                    "changed": True,
                    "planDigest": plan["digest"],
                    "beforeSHA256": plan["beforeSHA256"],
                    "afterSHA256": plan["afterSHA256"],
                }:
                    raise ValueError("native completion")
                bundle.verify()
                return dict(result, changed=True, evidence=proof)
        except (KeyError, TypeError, ValueError, OSError) as error:
            # The record names a class and a source, never exception text.
            name(error, warn=Display().warning)
            return dict(
                result,
                failed=True,
                msg="Native dependency transaction or its postcondition was refused.",
            )
        finally:
            if bundle is not None:
                bundle.close()

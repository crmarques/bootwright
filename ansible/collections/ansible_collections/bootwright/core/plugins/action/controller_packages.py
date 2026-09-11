"""Apply one frozen native solver transaction through the supplied DNF API."""

import os
from pathlib import Path
import tempfile
import time

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    Bundle,
    download,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_native import (
    invoke,
)


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
            bundle = Bundle(request["bundle"])
            bundle.writable()
            with tempfile.TemporaryDirectory(prefix="bootwright-dnf-apply-") as scratch:
                payloads, total = {}, 0
                deadline = time.monotonic() + 300
                for package in plan["packages"]:
                    if time.monotonic() > deadline:
                        raise ValueError("native acquisition deadline")
                    source = package["source"]
                    total += source["bytes"]
                    if (
                        not 0 < source["bytes"] <= 256 << 20
                        or total > 4 << 30
                        or source["id"] in payloads
                    ):
                        raise ValueError("native source limit")
                    data = download(source, request["egress"])
                    destination = Path(scratch) / (str(len(payloads)) + ".rpm")
                    descriptor = os.open(
                        destination,
                        os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                        0o600,
                    )
                    with os.fdopen(descriptor, "wb") as stream:
                        stream.write(data)
                        stream.flush()
                        os.fsync(stream.fileno())
                    payloads[source["id"]] = str(destination)
                if time.monotonic() > deadline:
                    raise ValueError("native acquisition deadline")
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
        except (KeyError, TypeError, ValueError, OSError):
            return dict(
                result,
                failed=True,
                msg="Native dependency transaction or its postcondition was refused.",
            )
        finally:
            if bundle is not None:
                bundle.close()

"""Execute an approved local-only transaction using the maintained DNF API."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import stat

from ansible_collections.bootwright.core.plugins.module_utils.native_signatures import (
    file_identity,
)


def verified_payloads(plan, payloads, scratch):
    if not isinstance(payloads, dict) or len(payloads) != len(plan["packages"]):
        raise ValueError("native payload closure")
    root = Path(scratch).resolve(strict=True)
    total = 0
    for package in plan["packages"]:
        source = package["source"]
        path = Path(payloads[source["id"]])
        if path.parent != root or path.name.startswith("."):
            raise ValueError("native payload authority")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(descriptor)
            total += info.st_size
            if (
                not stat.S_ISREG(info.st_mode)
                or info.st_uid != os.geteuid()
                or info.st_nlink != 1
                or stat.S_IMODE(info.st_mode) != 0o600
                or info.st_size != source["bytes"]
                or not 0 < info.st_size <= 256 << 20
                or total > 4 << 30
            ):
                raise ValueError("native payload identity")
            content = hashlib.sha256()
            while True:
                block = os.read(descriptor, 1 << 20)
                if not block:
                    break
                content.update(block)
            if content.hexdigest() != source["sha256"] or file_identity(
                os.fstat(descriptor)
            ) != file_identity(info):
                raise ValueError("native payload changed")
        finally:
            os.close(descriptor)


def transitions(actions):
    # Solver reason labels are metadata; the effect grant is these exact identities.
    return [
        {
            key: action[key]
            for key in ("kind", "before", "after", "sourceID")
            if key in action
        }
        for action in actions
    ]


def same_transaction(expected, actual):
    if (
        actual["solver"] != expected["solver"]
        or actual["solverVersion"] != expected["solverVersion"]
        or actual["beforeSHA256"] != expected["beforeSHA256"]
        or actual["afterSHA256"] != expected["afterSHA256"]
        or transitions(actual["actions"]) != transitions(expected["actions"])
    ):
        raise ValueError("native transaction changed")


def run4(base):
    import dnf.callback

    class Progress(dnf.callback.TransactionProgress):
        def __init__(self):
            super().__init__()
            self.failed = False

        def error(self, message):
            self.failed = True

    # Every path is local and checks are explicitly enabled. Missing publisher
    # trust refuses; only the Ceph grant imports its pinned signer keys first.
    base.conf.gpgcheck = True
    base.conf.localpkg_gpgcheck = True
    for package in base.transaction.install_set:
        if base.package_signature_check(package)[0] != 0:
            raise ValueError("native package signature")
    progress = Progress()
    base.do_transaction(progress)
    if progress.failed:
        raise ValueError("native vendor hook failed")


def apply(request, scratch):
    # This import is resolved only from the same shipped automation directory by
    # the fixed platform helper entrypoint, never from an ambient Python path.
    from ansible_collections.bootwright.core.plugins.module_utils import (
        native_resolution as native,
    )
    from ansible_collections.bootwright.core.plugins.module_utils import (
        native_signatures,
    )

    plan = request["plan"]
    native.validate_plan(plan)
    if (
        request["operation"] != "apply"
        or request["platform"] != plan["platform"]
        or request["requirements"] != plan["requirements"]
        or request["versions"] != plan["requests"]
        or not 0 < len(plan["actions"]) <= 512
        or os.geteuid() != 0
    ):
        raise ValueError("native transaction authority")
    verified_payloads(plan, request["payloads"], scratch)
    native_signatures.verify(request, scratch, request["payloads"])
    resolved, base, _transaction = native.solve4(
        request, scratch, local_sources=request["payloads"], return_transaction=True
    )
    try:
        same_transaction(plan, resolved)
        before = native.inspect(request, scratch)
        if before["inventorySHA256"] != plan["beforeSHA256"]:
            raise ValueError("native inventory changed before transaction")
        if request.get("policy") == "ceph-packages-v2":
            native_signatures.import_ceph_keys(request)
        # DNF owns its transaction lock, RPM checks, vendor hooks and history.
        # Concurrent direct RPM/operator writes are diagnosed by exact inventory
        # comparison; they are outside this supported coordination boundary.
        run4(base)
        # One snapshot serves both proofs, so the closure is read back once.
        # Integrity is this transaction's own postcondition and covers exactly
        # the identities it installed; readiness after it is presence alone.
        snapshot = native.snapshot_database(request["platform"], scratch)
        after = native.inspect(dict(request, snapshot=snapshot), scratch)
        installed = [action["after"] for action in plan["actions"]]
        if (
            after["inventorySHA256"] != plan["afterSHA256"]
            or after["rootsReady"] is not True
            or not native.verified_files(
                request["platform"], snapshot, installed, scratch
            )
        ):
            raise ValueError("native transaction postcondition")
        return {
            "changed": True,
            "planDigest": plan["digest"],
            "beforeSHA256": plan["beforeSHA256"],
            "afterSHA256": plan["afterSHA256"],
        }
    finally:
        base.close()

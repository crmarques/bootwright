"""Update the owned dashboard account and its original retained password recoverably."""

from __future__ import annotations

import hashlib
import importlib
import json
import os
import stat
import secrets

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native as native

PHASES = ("admin_prepare", "admin_update")


def prepare(worker):
    worker.dashboard_retained()
    store = worker.dashboard_store()
    scope = worker.dashboard_scope()
    unused, record = store.records(scope)
    user = worker.ceph("dashboard", "ac-user-show", native.DASHBOARD_USER)
    password = native.dashboard_password()
    hasher = importlib.import_module("crypt")
    hashed = hasher.crypt(password, hasher.mksalt(hasher.METHOD_BLOWFISH, rounds=1 << 10))
    native.require(isinstance(hashed, str) and hashed.startswith("$2"), "dashboard-verify")
    candidate = {"guard": {"scope": scope, "record": record, "hash": user["password"]}, "password": password, "hash": hashed}
    return {"private": json.dumps(candidate)}


def update(worker, candidate):
    worker.verify_tool()
    scope = worker.dashboard_scope()
    store = worker.dashboard_store()
    guard, password = candidate["guard"], candidate["password"]
    native.require(native.dashboard_policy(password) and guard["scope"] == scope, "dashboard-identity")
    reserved, ready = store.records(scope)
    native.require(reserved is not None and ready is not None, "dashboard-identity")
    binding = store.binding(scope, "ceph-dashboard-user-v1")
    store.prove_binding(binding)
    journal = {"scope": scope, "source": guard, "target": hashlib.sha256(password.encode("ascii")).hexdigest()}
    marker = "dashboard-credential-update.json"
    retained = worker.markers.read(marker)
    if retained != journal:
        native.require(ready == guard["record"], "dashboard-identity")
        worker.dashboard_verify(store.material(scope))
        native.require(worker.ceph("dashboard", "ac-user-show", native.DASHBOARD_USER)["password"] == guard["hash"], "dashboard-drift")
        replace_record(store, marker, retained, journal)
    user = worker.ceph("dashboard", "ac-user-show", native.DASHBOARD_USER)
    if user["password"] == guard["hash"]:
        code = worker.ceph_status("dashboard", "ac-user-set-password", native.DASHBOARD_USER, "-i", "-", data=password.encode("ascii"))
        native.require(code == 0, "dashboard-policy")
    worker.dashboard_verify(password)
    try:
        if store.material(scope) == password:
            return {"proved": True}
    except native.Refusal:
        pass
    with store.directory() as directory:
        descriptor = os.open(store.name("material"), os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(descriptor, "r+b") as handle:
            info = os.fstat(handle.fileno())
            native.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and store.metadata(info) == reserved["metadata"], "dashboard-identity")
            native.require(worker.markers.read(marker) == journal, "dashboard-identity")
            handle.seek(0)
            handle.write(password.encode("ascii"))
            handle.truncate()
            handle.flush()
            os.fsync(handle.fileno())
            record = dict(ready, metadata=store.metadata(os.fstat(handle.fileno()), complete=True))
        replace_record(store, store.name("id"), ready, record)
    native.require(store.material(scope) == password, "dashboard-identity")
    worker.dashboard_verify(password)
    return {"proved": True}


def dispatch(envelope):
    worker = native.Worker(envelope["request"], envelope["host"])
    native.require(worker.request["action"] == "cluster", "request")
    private = dict(envelope["private"])
    incarnation = private.pop("incarnation", None)
    worker.verify_tool()
    worker.dashboard_scope()
    native.require(incarnation is not None and worker.monmap_incarnation()[0] == incarnation, "ownership")
    if envelope["phase"] == "admin_prepare":
        native.require(private == {}, "material")
        return prepare(worker)
    native.require(envelope["phase"] == "admin_update" and set(private) == {"update"}, "material")
    return update(worker, private["update"])


def replace_record(store, name, expected, replacement):
    if expected is None:
        store.markers.write(name, replacement)
        return
    with store.directory() as directory:
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(descriptor, "rb") as handle:
            before = os.fstat(handle.fileno())
            native.require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == before.st_gid == 0
                           and stat.S_IMODE(before.st_mode) == 0o600 and before.st_size <= 16384, "dashboard-identity")
            native.require(json.loads(handle.read(16385)) == expected, "dashboard-identity")
        stage = ".credential-" + secrets.token_hex(16)
        descriptor = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
        try:
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(native.canonical(replacement))
                handle.flush()
                os.fsync(handle.fileno())
            now = os.stat(name, dir_fd=directory, follow_symlinks=False)
            native.require(store.metadata(now, complete=True) == store.metadata(before, complete=True) and now.st_nlink == 1, "dashboard-identity")
            os.replace(stage, name, src_dir_fd=directory, dst_dir_fd=directory)
            os.fsync(directory)
        finally:
            try:
                os.unlink(stage, dir_fd=directory)
            except FileNotFoundError:
                pass

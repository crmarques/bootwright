"""Ceph-owned roots through the shared immutable, publisher-verified DNF closure."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import fcntl
import hashlib
import os
import re
import shlex
import stat
import tempfile

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_crypto_policy, ceph_native, ceph_package_sources, native_apply, native_resolution, native_signatures,
)

ROOT = "/var/lib/bootwright/ceph-packages"
LEGACY_DIRECTORIES = {"bootstrap-mds", "bootstrap-mgr", "bootstrap-osd", "bootstrap-rbd", "bootstrap-rgw", "crash", "mds", "mgr", "mon", "osd", "tmp", "stat"}


def actual_platform():
    """The current installed OS may have drifted since managed installation."""
    path = "/etc/os-release"
    if os.path.islink(path):
        ceph_native.require(os.readlink(path) in ("../usr/lib/os-release", "/usr/lib/os-release"), "package-platform")
        path = "/usr/lib/os-release"
    data = ceph_native.read_regular(path, maximum=64 << 10).decode("utf-8")
    values = {}
    for line in data.splitlines():
        key, delimiter, value = line.partition("=")
        if key not in ("ID", "VERSION_ID"):
            continue
        ceph_native.require(delimiter and key not in values, "package-platform")
        words = shlex.split(value, comments=True, posix=True)
        ceph_native.require(len(words) == 1, "package-platform")
        values[key] = words[0]
    ceph_native.require(set(values) == {"ID", "VERSION_ID"} and all(values.values()) and os.uname().machine == "x86_64", "package-platform")
    return {"os": values["ID"], "release": values["VERSION_ID"], "architecture": "amd64"}


def ceph_root():
    """Open the fixed native data root without following any ancestor link."""
    current = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for component in ("var", "lib", "ceph"):
            try:
                following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
            except FileNotFoundError:
                ceph_native.require(component == "ceph", "package-host")
                return None
            info = os.fstat(following)
            # The exact ceph-common candidate creates its host Ceph UID167.
            # This is host RPM ownership, independent of container UIDs.
            allowed = (0, 167) if component == "ceph" else (0,)
            try:
                ceph_native.require(info.st_uid in allowed and info.st_mode & 0o022 == 0, "package-host")
            except Exception:
                os.close(following)
                raise
            os.close(current)
            current = following
        result, current = current, None
        return result
    finally:
        if current is not None:
            os.close(current)


def ceph_directory(directory, request, owned):
    """Unknown/foreign data never authorizes upgrading its host packages."""
    with os.scandir(directory) as entries:
        count = 0
        for entry in entries:
            count += 1
            ceph_native.require(count <= 128 and entry.is_dir(follow_symlinks=False), "package-host")
            child = os.open(entry.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            try:
                info = os.fstat(child)
                ceph_native.require(info.st_mode & 0o022 == 0, "package-host")
                if entry.name == request["fsid"]:
                    ceph_native.require(owned and info.st_uid == 0, "foreign-cluster")
                elif entry.name in LEGACY_DIRECTORIES:
                    ceph_native.require(info.st_uid in (0, 167), "package-host")
                    with os.scandir(child) as contents:
                        ceph_native.require(next(contents, None) is None, "foreign-cluster")
                else:
                    raise ceph_native.Refusal("foreign-cluster")
            finally:
                os.close(child)


def host_admission(request):
    actual_platform()
    directory = ceph_root()
    if directory is None:
        return
    try:
        ceph_directory(directory, request, ceph_native.Markers(request).owned())
    finally:
        os.close(directory)


def transaction_request(request):
    versions = {"ceph": request["cephadm"]["packageVersion"], "ansible": request["ansiblePackageVersion"],
                "license": "latest", "inheritedPolicy": "latest"}
    if "nfsGaneshaPackageVersion" in request:
        versions["nfsGanesha"] = request["nfsGaneshaPackageVersion"]
    return {"policy": "ceph-packages-v2", "platform": actual_platform(),
            "requirements": {"cephPackages": True}, "versions": versions}


class Journal:
    """Retain package decisions independently of Ceph disk/cluster ownership.

    Destroy does not uninstall provider prerequisites or remove publisher keys.
    Planned decisions survive a killed transaction and can only be resumed at
    their exact before/after inventory. No fresh resolution replaces a plan.
    """

    def __init__(self, request, root=ROOT):
        self.markers = ceph_native.Markers(request, root=root)
        self.root = root
        self.lock = None

    def __enter__(self):
        self.markers.create()
        self.lock = os.open(os.path.join(self.root, ".package-lock"), os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            info = os.fstat(self.lock)
            ceph_native.require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1 and info.st_mode & 0o077 == 0,
                                "package-journal")
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except Exception:
            os.close(self.lock)
            self.lock = None
            raise
        return self

    def __exit__(self, *args):
        if self.lock is not None:
            os.close(self.lock)
            self.lock = None

    def read(self, name):
        return self.markers.read(name)

    def write(self, name, value):
        self.markers.write(name, value)


@contextlib.contextmanager
def phase(reason, coordinate="Ceph package transaction"):
    """A native helper's failure is named by the package phase it failed in, at the site it failed."""
    try:
        yield
    except ceph_native.Refusal:
        raise
    except Exception as error:
        raise ceph_native.NativeFailure(reason, coordinate, str(error)).with_traceback(error.__traceback__) from None


@contextlib.contextmanager
def accepted_license(accepted):
    ceph_native.require(accepted is True, "package-license")
    previous = os.environ.get("ACCEPT_EULA")
    os.environ["ACCEPT_EULA"] = "yes"
    try:
        yield
    finally:
        if previous is None:
            os.environ.pop("ACCEPT_EULA", None)
        else:
            os.environ["ACCEPT_EULA"] = previous


def validate_decision(record, binding, grant):
    ceph_native.require(isinstance(record, dict) and set(record) == {"version", "binding", "plan"}, "package-journal")
    ceph_native.require(record["version"] == "ceph-package-decision-v1" and record["binding"] == binding, "package-journal")
    plan = native_resolution.validate_plan(record["plan"])
    ceph_native.require(plan["platform"] == grant["platform"] and plan["requirements"] == grant["requirements"] and plan["requests"] == grant["versions"]
                        and plan["solver"] == "dnf4", "package-journal")
    roots = sorted((r["key"], r["package"]["name"], r["requested"]) for r in plan["roots"])
    ceph_native.require(roots == sorted(native_resolution.selected_roots(grant)), "package-journal")
    selected = {native_resolution.canonical(root["package"]) for root in plan["roots"] if root["key"] in ("ceph", "ansible", "nfsGanesha")}
    ceph_native.require(all(action["kind"] in ("install", "upgrade") or action["kind"] == "downgrade"
                            and native_resolution.canonical(action["after"]) in selected for action in plan["actions"]), "package-journal")
    return plan


def integrity(request, scratch, plan):
    # Verify exactly the frozen installed closure, excluding config/runtime files.
    identities = [{key: p[key] for key in ("name", "epoch", "version", "release", "architecture")} for p in plan["packages"]]
    snapshot = native_resolution.snapshot_database(request["platform"], scratch)
    return native_resolution.verified_files(request["platform"], snapshot, identities, scratch)


def header_digest(arguments):
    code, output = ceph_native.bounded_command(["/usr/bin/rpm", *arguments], timeout=30, limit=1024)
    ceph_native.require(code == 0 and re.fullmatch(rb"[0-9a-f]{64}\n", output) is not None, "package-artifact")
    return output.decode().strip()


def rpm_query(arguments):
    code, output = ceph_native.bounded_command(["/usr/bin/rpm", *arguments], timeout=30, limit=256 << 10)
    ceph_native.require(code == 0, "package-artifact")
    return output


def prove_inherited_policy(authentication, plan, request, host, source, binding):
    ceph_crypto_policy.validate(authentication.get("policy"), plan, authentication["headers"], ceph_crypto_policy.scope(request, host, source), binding)
    ceph_crypto_policy.installed(authentication["policy"], rpm_query)


def trusted_headers(plan, payloads):
    return {p["source"]["id"]: header_digest(["-qp", "--nosignature", "--qf", "%{SHA256HEADER}\\n", "--",
                                              payloads[p["source"]["id"]]]) for p in plan["packages"]}


def installed_headers(plan, expected):
    if not isinstance(expected, dict) or set(expected) != {p["source"]["id"] for p in plan["packages"]}:
        return False
    for package in plan["packages"]:
        nevra = "{name}-{epoch}:{version}-{release}.{architecture}".format(**package)
        actual = header_digest(["-q", "--qf", "%{SHA256HEADER}\\n", "--", nevra])
        if actual != expected[package["source"]["id"]]:
            return False
    return True


def prepare(request, host, certificate, *, acquisition=None, journal=None):
    source = ceph_package_sources.source_for(request, host)
    ceph_native.require(request["action"] == "cluster" and ceph_native.supported_transport(request), "unsupported-release")
    ceph_native.require(os.geteuid() == 0, "packages")
    ceph_native.require(isinstance(certificate, str), "material")
    host_admission(request)
    binding = native_resolution.digest({"request": request, "host": host, "source": source,
                                        "certificateSHA256": hashlib.sha256(certificate.encode()).hexdigest()})
    acquire = acquisition or ceph_package_sources.Acquisition(source, certificate, repository=request["packageRepository"])
    with (journal or Journal(request)) as durable, tempfile.TemporaryDirectory(prefix="bootwright-ceph-packages-") as scratch:
        grant = transaction_request(request)
        record = durable.read("decision.json")
        if record is None:
            with phase("package-resolution", str(grant["versions"])):
                grant["repositories"] = acquire.repositories(scratch)
                plan = native_resolution.solve4(grant, scratch)
            record = {"version": "ceph-package-decision-v1", "binding": binding, "plan": plan}
            validate_decision(record, binding, grant)
            durable.write("decision.json", record)
        plan = validate_decision(record, binding, grant)
        grant["plan"] = plan
        complete = durable.read("complete.json")
        completion = {"version": "ceph-package-complete-v1", "binding": binding, "planDigest": plan["digest"]}
        before = native_resolution.inspect(grant, scratch)
        verified = durable.read("verified.json")
        if complete is not None:
            ceph_native.require(verified is not None and verified.get("binding") == binding and verified.get("planDigest") == plan["digest"], "package-journal")
            ceph_native.require(complete == completion and before["inventorySHA256"] == plan["afterSHA256"] and before["rootsReady"] is True
                                and installed_headers(plan, verified["headers"]) and integrity(grant, scratch, plan), "package-drift")
            prove_inherited_policy(verified, plan, request, host, source, binding)
            return {"changed": False, "planDigest": plan["digest"], "ready": True}
        with phase("package-signature"):
            grant["publisherKeys"] = acquire.publisher_key(scratch)
            grant["payloads"] = acquire.payloads(scratch, plan)
            native_apply.verified_payloads(plan, grant["payloads"], scratch)
            native_signatures.verify(grant, scratch, grant["payloads"])
        authentication = {"version": "ceph-package-artifacts-v1", "binding": binding, "planDigest": plan["digest"],
                          "headers": trusted_headers(plan, grant["payloads"])}
        authentication["policy"] = ceph_crypto_policy.capture(plan, grant["payloads"], authentication["headers"],
                                                              ceph_crypto_policy.scope(request, host, source), binding, rpm_query)
        ceph_native.require(verified is None or verified == authentication, "package-journal")
        durable.write("verified.json", authentication)
        # A killed transaction is completed only by exact after-inventory and
        # verified closure. Other partial/foreign inventories remain unresolved.
        if before["inventorySHA256"] == plan["afterSHA256"] and before["rootsReady"] is True:
            ceph_native.require(installed_headers(plan, authentication["headers"]) and integrity(grant, scratch, plan), "package-drift")
            prove_inherited_policy(authentication, plan, request, host, source, binding)
            durable.write("complete.json", completion)
            return {"changed": bool(plan["actions"]), "planDigest": plan["digest"], "ready": True}
        ceph_native.require(before["inventorySHA256"] == plan["beforeSHA256"] and bool(plan["actions"]), "package-drift")
        grant["operation"] = "apply"
        host_admission(request)
        with accepted_license(request["packages"]["licenseAccepted"]), phase("package-transaction"):
            result = native_apply.apply(grant, scratch)
        ceph_native.require(result["planDigest"] == plan["digest"] and result["afterSHA256"] == plan["afterSHA256"], "packages")
        ceph_native.require(installed_headers(plan, authentication["headers"]), "package-artifact")
        prove_inherited_policy(authentication, plan, request, host, source, binding)
        durable.write("complete.json", completion)
        return {"changed": result["changed"], "planDigest": plan["digest"], "ready": True}


def entrypoint(payload):
    ceph_native.require(isinstance(payload, dict) and set(payload) == {"request", "host", "private"}, "packages")
    ceph_native.require(isinstance(payload["private"], dict) and set(payload["private"]) == {"certificate"}, "material")
    return prepare(payload["request"], payload["host"], payload["private"]["certificate"])

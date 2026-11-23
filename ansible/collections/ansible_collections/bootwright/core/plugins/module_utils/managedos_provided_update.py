"""Reconcile one owner's release-bounded update on a provided RHEL host."""

from __future__ import annotations

import contextlib
import fcntl
import json
import os
import re
import secrets
import stat

from ansible_collections.bootwright.core.plugins.module_utils import managedos_update_worker as shared
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

ROOT, DIRECTORY = os.path.split("/etc/bootwright/provided-updates")
BOOT_ID = "/proc/sys/kernel/random/boot_id"
FIPS = "/proc/sys/crypto/fips_enabled"
BOOT = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}")
RELEASE_TAG = re.compile(r"(?:^|[.+])el([0-9]+)(?:[_.]([0-9]+))?(?:$|[.+_])")
COMMENT = "bootwright-provided-update:"
STATES = ("absent", "pending", "upgraded", "complete")
POLICIES = ("withinRelease", "toRelease")
IDENTITY = ("redhat-release", "redhat-release-eula")
REFUSALS = (
    "update-release-mismatch", "update-release-unavailable", "update-release-drift", "update-no-repository",
    "update-foreign", "update-unavailable", "update-failed", "update-reboot",
)
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def read_value(path, pattern, limit, reason):
    with open(path, "rb") as stream:
        value = stream.read(limit + 1).strip().decode("ascii", "replace")
    if not pattern.fullmatch(value):
        raise shared.Refused(reason)
    return value


def earlier_minor(found, release):
    if not shared.RELEASE.fullmatch(found):
        return False
    have, want = (tuple(int(part) for part in value.split(".")) for value in (found, release))
    return have[0] == want[0] and have[1] < want[1]


def prove_release(release, path=shared.OS_RELEASE, earlier=False):
    """Require RHEL at the release, or before a move to it an earlier minor of its major; return VERSION_ID."""
    with open(path, "rb") as stream:
        data = stream.read(shared.MAX_OS_RELEASE + 1)
    if len(data) > shared.MAX_OS_RELEASE:
        raise shared.Refused("update-release-mismatch", detail="unknown")
    values = dict(line.split("=", 1) for line in data.decode("utf-8", "replace").splitlines()
                  if line.startswith(("ID=", "VERSION_ID=")))
    found = values.get("VERSION_ID", "").strip().strip("\"'")
    if values.get("ID", "").strip().strip("\"'") != "rhel" or found != release and not (earlier and earlier_minor(found, release)):
        raise shared.Refused("update-release-mismatch", detail=shared.token(found))
    return found


def boot_id():
    return read_value(BOOT_ID, BOOT, 64, "update-reboot")


def fips_mode():
    return read_value(FIPS, re.compile("[01]"), 4, "update-failed")


def sync_parent(path, parent):
    if parent is not None:
        os.fsync(parent)
        return
    descriptor = os.open(os.path.dirname(path), DIRECTORY_FLAGS)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def safe_directory(path, *, parent=None, create=False, mode=0o700):
    if create:
        with contextlib.suppress(FileExistsError):
            os.mkdir(path, mode, dir_fd=parent)
        # Another apply may just have created the same entry. Synchronize
        # either way before a transaction can depend on the directory.
        sync_parent(path, parent)
    descriptor = os.open(path, DIRECTORY_FLAGS, dir_fd=parent)
    info = os.fstat(descriptor)
    if info.st_uid != 0 or info.st_mode & (0o022 if parent is None else 0o077):
        os.close(descriptor)
        raise shared.Refused("update-foreign")
    return descriptor


@contextlib.contextmanager
def directory(root, apply):
    """All owners on a host hold one lock while reading or changing receipts and DNF."""
    with contextlib.ExitStack() as stack:
        try:
            parent = safe_directory(root, create=apply)
            stack.callback(os.close, parent)
            descriptor = safe_directory(DIRECTORY, parent=parent, create=apply)
            stack.callback(os.close, descriptor)
        except FileNotFoundError:
            if apply:
                raise
            yield None
            return
        except OSError:
            raise shared.Refused("update-foreign") from None
        try:
            fcntl.flock(descriptor, (fcntl.LOCK_EX if apply else fcntl.LOCK_SH) | fcntl.LOCK_NB)
        except BlockingIOError:
            raise shared.Refused("update-unavailable", "busy") from None
        yield descriptor


def valid_receipt(value, digest):
    return (isinstance(value, dict) and set(value) == {"request", "boot", "fips", "state", "packages", "transaction"}
            and value["request"] == digest and isinstance(value["boot"], str) and bool(BOOT.fullmatch(value["boot"]))
            and value["fips"] in ("0", "1") and value["state"] in STATES[1:]
            and all(type(value[key]) is int and 0 <= value[key] <= 2147483647  # pylint: disable=unidiomatic-typecheck
                    for key in ("packages", "transaction"))
            and bool(value["packages"]) == bool(value["transaction"])
            and (value["state"] != "pending" or value["transaction"] == 0)
            and (value["state"] != "upgraded" or value["transaction"] > 0))


class Receipt:
    """A root-only receipt belonging to one original apply, beneath an opened directory."""

    def __init__(self, descriptor, owner, digest):
        self.descriptor = descriptor
        self.name = owner + ".json"
        self.digest = digest

    def read(self):
        if self.descriptor is None:
            return None
        try:
            descriptor = os.open(self.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=self.descriptor)
        except FileNotFoundError:
            return None
        except OSError:
            raise shared.Refused("update-foreign") from None
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or info.st_mode & 0o077:
                raise shared.Refused("update-foreign")
            data = shared.read_bounded(descriptor, shared.MAX_RECORD)
            value = json.loads(data) if len(data) <= shared.MAX_RECORD else None
        except (ValueError, UnicodeError, OSError):
            raise shared.Refused("update-foreign") from None
        finally:
            os.close(descriptor)
        if not valid_receipt(value, self.digest):
            raise shared.Refused("update-foreign")
        return value

    def write(self, record):
        temporary = "." + secrets.token_hex(16)
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC, 0o600, dir_fd=self.descriptor)
        try:
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(canonical(record))
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, self.name, src_dir_fd=self.descriptor, dst_dir_fd=self.descriptor)
            os.fsync(self.descriptor)
        finally:
            with contextlib.suppress(FileNotFoundError):
                os.unlink(temporary, dir_fd=self.descriptor)


def history(owner, dnf):
    """Only DNF's completed transaction is recovery evidence."""
    try:
        with dnf.Base() as base:
            base.conf.read()
            entries = [entry for entry in base.history.old() if entry.comment == COMMENT + owner]
            if not entries:
                return 0, 0
            if len(entries) != 1 or entries[0].return_code != 0:
                raise shared.Refused("update-failed", "history")
            entry = entries[0]
            count = sum(1 for item in entry.packages() if item.is_package() and shared.forward(item, dnf))
            if entry.tid <= 0 or count <= 0:
                raise shared.Refused("update-failed", "history")
            return entry.tid, count
    except shared.Refused:
        raise
    except Exception:  # pylint: disable=broad-exception-caught
        raise shared.Refused("update-failed", "history") from None


def release_swap(package, version):
    repository = getattr(package, "reponame", "")
    if not isinstance(repository, str) or not shared.REPOSITORY.fullmatch(repository):
        repository = "unknown"
    raise shared.Refused("update-release-drift", detail=shared.token(version) + "@" + repository)


def check_release_transaction(transaction, release, installed):
    """Reject an identity change other than one to the release, and a visible later RHEL content tag.

    A host on an earlier minor moves only when the transaction installs the release's own identity.
    """
    installing = list(transaction.install_set)
    target = tuple(int(part) for part in release.split("."))
    for package in installing:
        if package.name in IDENTITY and package.version != release:
            release_swap(package, package.version)
        tag = RELEASE_TAG.search(package.release)
        if tag and (int(tag[1]) != target[0] or tag[2] is not None and int(tag[2]) > target[1]):
            release_swap(package, ".".join(part for part in tag.groups() if part is not None))
    replacements = {package.name for package in installing if package.version == release}
    for package in transaction.remove_set:
        if package.name in IDENTITY and package.name not in replacements:
            release_swap(package, "removed")
    if installed != release and "redhat-release" not in replacements:
        raise shared.Refused("update-release-unavailable", detail=shared.token(installed))
    return installing


def pin_transaction(conf, release):
    major, minor = release.split(".")
    conf.substitutions["releasever"] = release
    conf.substitutions["releasever_major"] = major
    conf.substitutions["releasever_minor"] = minor


def configure(base, owner, release):
    base.conf.read()
    base.conf.substitutions.update_from_etc(base.conf.installroot, varsdir=base.conf.varsdir)
    pin_transaction(base.conf, release)
    base.conf.history_record = True
    base.conf.comment = COMMENT + owner
    base.conf.exit_on_lock = True
    base.conf.assumeno = True
    base.init_plugins()
    base.pre_configure_plugins()
    pin_transaction(base.conf, release)
    base.read_all_repos()
    base.configure_plugins()
    repositories = list(base.repos.iter_enabled())
    if not repositories:
        raise shared.Refused("update-no-repository")
    for repo in repositories:
        repo.metadata_expire = 0
        repo.skip_if_unavailable = False


def check_signatures(base, packages):
    # Match DNF's convention: the active RPM policy can require package
    # signatures even when a repository's own option is disabled.
    import rpm  # pylint: disable=import-outside-toplevel

    if rpm.expandMacro("%_pkgverify_level") in ("signature", "all"):
        for repo in base.repos.iter_enabled():
            repo.gpgcheck = True
    for package in packages:
        if base.package_signature_check(package)[0] != 0:
            raise shared.Refused("update-failed", "signature")


def upgrade(owner, release, installed, dnf):
    """Commit the inspected solver result once; do not let a second solve select different RPMs."""
    from dnf.callback import TransactionProgress  # pylint: disable=import-outside-toplevel

    class Progress(TransactionProgress):
        failed = False

        def error(self, message):
            del message
            self.failed = True

    with dnf.Base() as base:
        configure(base, owner, release)
        try:
            base.fill_sack(load_system_repo=True)
        except dnf.exceptions.Error:
            raise shared.Refused("update-unavailable", "metadata") from None
        base.upgrade_all()
        try:
            base.resolve()
        except dnf.exceptions.Error:
            raise shared.Refused("update-failed", "resolution") from None
        packages = check_release_transaction(base.transaction, release, installed)
        if not packages:
            return False
        try:
            base.download_packages(packages)
        except dnf.exceptions.Error:
            raise shared.Refused("update-unavailable", "download") from None
        check_signatures(base, packages)
        prove_release(installed)
        progress = Progress()
        base.do_transaction(progress)
        if progress.failed:
            raise shared.Refused("update-failed", "transaction")
    return True


def result(record, changed=False):
    return {"changed": changed, "state": record["state"] if record else "absent",
            "packages": record["packages"] if record else 0,
            "transaction": record["transaction"] if record else 0,
            "rebooted": bool(record and record["state"] == "complete" and record["transaction"])}


class Update:
    """Coordinate the receipt, exact transaction and boot proof."""

    def __init__(self, receipt, owner, release, policy, dnf=shared.load_dnf):
        self.receipt = receipt
        self.owner = owner
        self.release = release
        self.policy = policy
        self.dnf = dnf

    def starting_release(self):
        """Before its transaction a move to the release also admits an earlier minor."""
        return prove_release(self.release, earlier=self.policy == "toRelease")

    def finish(self, record, changed):
        prove_release(self.release)
        if fips_mode() != record["fips"]:
            raise shared.Refused("update-reboot", "fips")
        self.prove_transaction(record)
        if record["state"] == "upgraded" and boot_id() != record["boot"]:
            record = dict(record, state="complete")
            self.receipt.write(record)
        return result(record, changed)

    def apply(self, record):
        if record is not None and record["state"] != "pending":
            return self.finish(record, record["state"] == "upgraded")
        installed = self.starting_release()
        if record is None:
            record = {"request": self.receipt.digest, "state": "pending", "boot": boot_id(),
                      "fips": fips_mode(), "transaction": 0, "packages": 0}
            self.receipt.write(record)
        if fips_mode() != record["fips"]:
            raise shared.Refused("update-reboot", "fips")
        dnf = self.dnf()
        transaction, packages = history(self.owner, dnf)
        if not transaction:
            record = dict(record, boot=boot_id())
            self.receipt.write(record)
            if upgrade(self.owner, self.release, installed, dnf):
                transaction, packages = history(self.owner, dnf)
                if not transaction:
                    raise shared.Refused("update-failed", "history")
        record = dict(record, state="upgraded" if transaction else "complete", transaction=transaction, packages=packages)
        self.receipt.write(record)
        return self.finish(record, True)

    def prove_transaction(self, record):
        if record["transaction"] and history(self.owner, self.dnf()) != (record["transaction"], record["packages"]):
            raise shared.Refused("update-failed", "history")

    def observe(self, record):
        if record is None:
            return result(None)
        if record["state"] == "pending":
            self.starting_release()
        else:
            prove_release(self.release)
        if record["state"] == "complete" and (fips_mode() != record["fips"] or record["transaction"] and boot_id() == record["boot"]):
            raise shared.Refused("update-reboot", "boot-proof")
        return result(record)


def execute(operation, owner, digest, release, policy, root=ROOT):
    if (operation not in ("apply", "complete", "observe") or policy not in POLICIES
            or not all(isinstance(value, str) and shared.DIGEST.fullmatch(value) for value in (owner, digest))
            or not isinstance(release, str) or len(release) > 64 or not shared.RELEASE.fullmatch(release)
            or os.geteuid() != 0):
        raise shared.Refused("update-failed", "authority")
    with directory(root, operation != "observe") as descriptor:
        receipt = Receipt(descriptor, owner, digest)
        current = receipt.read()
        update = Update(receipt, owner, release, policy)
        if operation == "observe":
            return update.observe(current)
        if operation == "apply":
            return update.apply(current)
        if current is None or current["state"] == "pending":
            raise shared.Refused("update-reboot", "boot-proof")
        outcome = update.finish(current, current["state"] == "upgraded")
        if outcome["state"] != "complete":
            raise shared.Refused("update-reboot", "boot-proof")
        return outcome

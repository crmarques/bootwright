"""The fixed root worker that updates an installed machine within its release.

It runs on the installed machine as root, under the system's isolated Python,
and reads one JSON request on standard input: the operation, the installation
request's digest and the release. It proves that /etc/os-release names that
release, resolves with the guest's own dnf what `dnf upgrade` would do, refusing
a transaction that moves redhat-release off the release, then runs `dnf
upgrade --refresh --assumeyes --comment=bootwright:<digest>`, only with at least
UPGRADE_FLOOR seconds of its session left; it never passes
--nogpgcheck, --allowerasing or --repofrompath and never changes a repository.

Ownership is a root-only record, /etc/bootwright/update.json, keyed by the
request digest: `pending` before the upgrade, `upgraded` once dnf succeeded,
`complete` once the caller rebooted the machine and the release was proved
again. A record of another digest is foreign and nothing is changed. The dnf
history transaction is the one carrying this request's comment, so a reply
lost after dnf ran is found by the next run rather than counted as nothing.

The answer is one line of JSON carrying closed reasons, states, counts and the
running kernel; a refusal's detail is only a version or a repository ID of a
closed character set. Only the standard library is imported at load; dnf is
imported when the guest needs it.
"""

from __future__ import annotations

import json
import os
import re
import stat
import subprocess
import sys
import time

RECORD_DIRECTORY = "/etc/bootwright"
RECORD = "/etc/bootwright/update.json"
OS_RELEASE = "/etc/os-release"
DNF = "/usr/bin/dnf"
SESSION_CEILING = 1740
UPGRADE_FLOOR = 900
MAX_INPUT = 4096
MAX_RECORD = 4096
MAX_OS_RELEASE = 65536
REASONS = ("update-release-mismatch", "update-release-drift", "update-no-repository", "update-foreign",
           "update-unavailable", "update-failed", "transport")
CAUSES = {"update-unavailable": ("metadata", "download", "timeout"),
          "update-failed": ("resolution", "transaction", "timeout", "history", "tooling")}
DETAILED = ("update-release-mismatch", "update-release-drift")
STATES = {"apply": ("pending", "upgraded", "complete"),
          "complete": ("complete",),
          "inspect": ("absent", "pending", "upgraded", "complete", "foreign")}
RECORD_STATES = ("pending", "upgraded", "complete")
KEYS = frozenset(("operation", "request", "release"))
DIGEST = re.compile(r"[0-9a-f]{64}")
RELEASE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
TOKEN = re.compile(r"[A-Za-z0-9._+~-]{1,64}")
REPOSITORY = re.compile(r"[A-Za-z0-9._:-]{1,90}")
KERNEL = re.compile(r"[A-Za-z0-9._+~-]{1,128}")
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
UNREADABLE = "unreadable"
COMMENT = "bootwright:"


class Refused(Exception):
    """A closed refusal; detail is a version or repository token of a closed character set, or empty."""

    def __init__(self, reason, cause=None, detail=""):
        super().__init__(reason)
        self.reason = reason
        self.cause = cause
        self.detail = detail


def read_bounded(descriptor, limit):
    """At most limit + 1 bytes from an open descriptor."""
    chunks = []
    size = 0
    while size <= limit:
        chunk = os.read(descriptor, limit + 1 - size)
        if not chunk:
            break
        chunks.append(chunk)
        size += len(chunk)
    return b"".join(chunks)


def token(value):
    """A version as a refusal may name it, or 'unknown'."""
    return value if isinstance(value, str) and TOKEN.fullmatch(value) else "unknown"


def version_id(path=OS_RELEASE):
    """VERSION_ID of os-release(5), unquoted, or ''."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK)
    except OSError:
        return ""
    try:
        data = read_bounded(descriptor, MAX_OS_RELEASE)
    finally:
        os.close(descriptor)
    if len(data) > MAX_OS_RELEASE:
        return ""
    for line in data.decode("utf-8", "replace").splitlines():
        name, separator, value = line.strip().partition("=")
        if separator and name == "VERSION_ID":
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            return value
    return ""


def load_dnf():
    """The system's dnf, imported only on the guest."""
    try:
        import dnf  # pylint: disable=import-outside-toplevel
        import dnf.exceptions  # pylint: disable=import-outside-toplevel
        import dnf.transaction  # pylint: disable=import-outside-toplevel
    except ImportError:
        raise Refused("update-failed", "tooling") from None
    return dnf


def forward(item, dnf):
    """Whether a history item installs a package rather than records one it replaced."""
    return getattr(item, "action", None) in getattr(dnf.transaction, "FORWARD_ACTIONS", ())


def preview(release, dnf=None):
    """Resolve and download what dnf upgrade would do, applying nothing; the packages it would change."""
    dnf = dnf or load_dnf()
    base = dnf.Base()
    try:
        base.conf.read()
        base.conf.substitutions.update_from_etc(base.conf.installroot)
        base.read_all_repos()
        enabled = list(base.repos.iter_enabled())
        if not enabled:
            raise Refused("update-no-repository")
        for repository in enabled:
            repository.metadata_expire = 0
        try:
            base.fill_sack(load_system_repo=True)
        except dnf.exceptions.Error:
            raise Refused("update-unavailable", "metadata") from None
        base.upgrade_all()
        try:
            base.resolve()
        except dnf.exceptions.Error:
            raise Refused("update-failed", "resolution") from None
        install = list(base.transaction.install_set)
        for package in install:
            if package.name == "redhat-release" and package.version != release:
                repository = package.reponame if isinstance(package.reponame, str) and REPOSITORY.fullmatch(package.reponame) else "unknown"
                raise Refused("update-release-drift", detail=token(package.version) + "@" + repository)
        try:
            base.download_packages(install)
        except dnf.exceptions.Error:
            raise Refused("update-unavailable", "download") from None
        return len(install)
    finally:
        base.close()


def history(request, dnf=None):
    """(transaction ID, packages it installed) of the newest dnf history transaction carrying this request's comment, or (0, 0)."""
    dnf = dnf or load_dnf()
    base = dnf.Base()
    try:
        comment = COMMENT + request
        mine = [entry for entry in base.history.old() if getattr(entry, "comment", None) == comment]
        if not mine:
            return 0, 0
        entry = max(mine, key=lambda candidate: candidate.tid)
        return int(entry.tid), sum(1 for item in entry.packages() if forward(item, dnf))
    except Refused:
        raise
    except Exception:  # pylint: disable=broad-exception-caught
        raise Refused("update-failed", "history") from None
    finally:
        base.close()


def upgrade(request, timeout):
    """Run dnf upgrade once; its output is discarded and only its exit code is read."""
    argv = [DNF, "upgrade", "--refresh", "--assumeyes", "--comment=" + COMMENT + request]
    try:
        # A fixed argv on the guest; no AnsibleModule runs there.
        # pylint: disable-next=ansible-bad-function
        completed = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   env=dict(ENVIRONMENT), cwd="/", timeout=timeout, check=False)
    except subprocess.TimeoutExpired:
        raise Refused("update-failed", "timeout") from None
    except FileNotFoundError:
        raise Refused("update-failed", "tooling") from None
    if completed.returncode != 0:
        raise Refused("update-failed", "transaction")


class Guest:
    """The installed machine's update state; every dependency is injectable."""

    def __init__(self, record=RECORD, directory=RECORD_DIRECTORY, owner=0, os_release=OS_RELEASE,
                 resolve=preview, transactions=history, run=upgrade, kernel=None, clock=time.monotonic):
        self.record = record
        self.directory = directory
        self.owner = owner
        self.os_release = os_release
        self.resolve = resolve
        self.transactions = transactions
        self.run = run
        self.kernel = kernel or (lambda: os.uname().release)
        self.clock = clock
        self.started = clock()

    def read_record(self):
        """The ownership record, None when absent, or UNREADABLE."""
        try:
            descriptor = os.open(self.record, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        except FileNotFoundError:
            return None
        except OSError:
            return UNREADABLE
        try:
            status = os.fstat(descriptor)
            if (not stat.S_ISREG(status.st_mode) or status.st_uid != self.owner
                    or status.st_mode & 0o077 or status.st_size > MAX_RECORD):
                return UNREADABLE
            data = read_bounded(descriptor, MAX_RECORD)
        finally:
            os.close(descriptor)
        try:
            value = json.loads(data.decode("utf-8")) if len(data) <= MAX_RECORD else None
        except (ValueError, UnicodeError):
            return UNREADABLE
        if not isinstance(value, dict) or set(value) != {"request", "state", "transaction"}:
            return UNREADABLE
        if not isinstance(value["request"], str) or not DIGEST.fullmatch(value["request"]):
            return UNREADABLE
        transaction = value["transaction"]
        if value["state"] not in RECORD_STATES or type(transaction) is not int or transaction < 0:  # pylint: disable=unidiomatic-typecheck
            return UNREADABLE
        return value

    def owned(self, request):
        """This request's record or None; any other record refuses update-foreign."""
        record = self.read_record()
        if record is None:
            return None
        if record == UNREADABLE or record["request"] != request:
            raise Refused("update-foreign")
        return record

    def sync_directory(self):
        descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def write(self, request, state, transaction):
        """Publish the record atomically and durably as root-only canonical JSON."""
        os.makedirs(self.directory, 0o700, exist_ok=True)
        data = json.dumps({"request": request, "state": state, "transaction": transaction},
                          sort_keys=True, separators=(",", ":")).encode("ascii")
        temporary = os.path.join(self.directory, ".update.json.tmp")
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            os.fchmod(descriptor, 0o600)
            view = memoryview(data)
            while view:
                view = view[os.write(descriptor, view):]
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        os.replace(temporary, self.record)
        self.sync_directory()

    def prove_release(self, release):
        found = version_id(self.os_release)
        if found != release:
            raise Refused("update-release-mismatch", detail=token(found))

    def remaining(self):
        return max(1, int(SESSION_CEILING - (self.clock() - self.started)))


def result(guest, changed, state, release, transaction, packages):
    kernel = guest.kernel()
    return {"changed": changed, "kernel": kernel if isinstance(kernel, str) and KERNEL.fullmatch(kernel) else "unknown",
            "packages": packages, "rebooted": state == "complete" and transaction != 0, "release": release,
            "state": state, "transaction": transaction}


def apply_operation(guest, payload):
    request, release = payload["request"], payload["release"]
    record = guest.owned(request)
    guest.prove_release(release)
    if record is not None and record["state"] != "pending":
        transaction = record["transaction"]
        packages = guest.transactions(request)[1] if transaction else 0
        return result(guest, record["state"] != "complete", record["state"], release, transaction, packages)
    counted = guest.resolve(release)
    if guest.remaining() < UPGRADE_FLOOR:
        raise Refused("update-unavailable", "timeout")
    guest.write(request, "pending", 0)
    guest.run(request, guest.remaining())
    transaction, packages = guest.transactions(request)
    if counted and transaction == 0:
        raise Refused("update-failed", "history")
    guest.write(request, "upgraded", transaction)
    return result(guest, True, "upgraded", release, transaction, packages)


def complete_operation(guest, payload):
    request, release = payload["request"], payload["release"]
    record = guest.owned(request)
    if record is None or record["state"] == "pending":
        raise Refused("transport")
    guest.prove_release(release)
    transaction = record["transaction"]
    packages = guest.transactions(request)[1] if transaction else 0
    if record["state"] != "complete":
        guest.write(request, "complete", transaction)
    return result(guest, record["state"] != "complete", "complete", release, transaction, packages)


def inspect_operation(guest, payload):
    request, release = payload["request"], payload["release"]
    try:
        record = guest.owned(request)
    except Refused:
        return result(guest, False, "foreign", "", 0, 0)
    if record is None:
        return result(guest, False, "absent", "", 0, 0)
    state, transaction = record["state"], record["transaction"]
    found, packages = guest.transactions(request) if transaction else (0, 0)
    if state == "complete" and (version_id(guest.os_release) != release or found != transaction):
        state = "upgraded"
    return result(guest, False, state, release, transaction, packages)


OPERATIONS = {"apply": apply_operation, "complete": complete_operation, "inspect": inspect_operation}


def parse_request(data):
    """The validated request; every value is checked before any guest state is read."""
    if not isinstance(data, bytes) or len(data) > MAX_INPUT:
        raise Refused("transport")
    try:
        payload = json.loads(data.decode("utf-8"))
    except (ValueError, UnicodeError):
        raise Refused("transport") from None
    if not isinstance(payload, dict) or set(payload) != KEYS or payload["operation"] not in OPERATIONS:
        raise Refused("transport")
    if not isinstance(payload["request"], str) or not DIGEST.fullmatch(payload["request"]):
        raise Refused("transport")
    if not isinstance(payload["release"], str) or not RELEASE.fullmatch(payload["release"]):
        raise Refused("transport")
    return payload


def answer(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def refusal(refused):
    reply = {"ok": False, "reason": refused.reason if refused.reason in REASONS else "transport"}
    if refused.cause is not None and refused.cause in CAUSES.get(reply["reason"], ()):
        reply["cause"] = refused.cause
    if reply["reason"] in DETAILED and refused.detail:
        reply["detail"] = refused.detail
    return reply


def main(stdin=None, stdout=None, guest=None):
    """Answer one request with exactly one line; never a traceback or any other output."""
    try:
        source = stdin if stdin is not None else sys.stdin.buffer
        payload = parse_request(source.read(MAX_INPUT + 1))
        reply = {"ok": True, "result": OPERATIONS[payload["operation"]](guest if guest is not None else Guest(), payload)}
    except Refused as refused:
        reply = refusal(refused)
    except BaseException:  # pylint: disable=broad-exception-caught
        reply = {"ok": False, "reason": "transport"}
    target = stdout if stdout is not None else sys.stdout
    target.write(answer(reply) + "\n")
    target.flush()
    return 0

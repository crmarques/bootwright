"""Converge chrony on one provided RHEL host onto its declared time servers and prove its clock synchronized."""

from __future__ import annotations

import base64
import binascii
import json
import os
import re
import stat
import time

from ansible_collections.bootwright.core.plugins.module_utils import atomic_publish, bounded_process
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

CONF = "/etc/chrony.conf"
RECORD_DIRECTORY = "/etc/bootwright"
RECORD_NAME = "provided-time.json"
SYSTEMCTL = "/usr/bin/systemctl"
RPM = "/usr/bin/rpm"
DNF = "/usr/bin/dnf"
TIMEDATECTL = "/usr/bin/timedatectl"
CHRONYC = "/usr/bin/chronyc"
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
COMMAND_TIMEOUT = 60
PACKAGE_TIMEOUT = 300
PROOF_SECONDS = 180
POLL_SECONDS = 5
MAX_CONF = 64 * 1024
MAX_OUTPUT = 64 * 1024
MAX_RECORD = 1 << 20
MAX_DETAIL = 160
MAX_SERVERS = 256
DEFAULT_MODE = 0o644
DIGEST = re.compile(r"[0-9a-f]{64}")
SERVER = re.compile(r"[A-Za-z0-9:][A-Za-z0-9.:-]{0,252}")
SOURCE = re.compile(r"[\^=#][*+\-?x~] +(\S+) +\d+ +\d+ +([0-7]+) ")
OPERATIONS = ("apply", "observe")
STATES = ("absent", "partial", "complete")
REFUSALS = ("time-service-conflict", "time-package", "time-config", "time-unproved", "time-unavailable")
CONFLICTS = ("systemd-timesyncd.service", "ntpd.service", "ntpsec.service", "openntpd.service", "timemaster.service")
SOURCE_DIRECTIVES = (b"server", b"pool", b"peer")
ENABLED = ("enabled", "enabled-runtime")
ACTIVE = ("active", "activating", "reloading")
MODES = {"^": "s", "=": "p", "#": "l"}
MARKS = {"*": "S", "+": "C", "-": "N", "?": "U", "x": "F", "~": "V"}
RECORD_KEYS = frozenset(("request", "file", "mode", "uid", "gid", "label", "enabled", "active"))


class Refused(Exception):
    """A closed refusal; the detail is a closed cause or unit name and never a value from the host."""

    def __init__(self, reason, detail=""):
        super().__init__(reason)
        self.reason = reason
        self.detail = detail


def valid_servers(servers):
    return (isinstance(servers, list) and 0 < len(servers) <= MAX_SERVERS and len(set(servers)) == len(servers)
            and all(isinstance(server, str) and SERVER.fullmatch(server) for server in servers))


def rewritten(current, servers):
    """The chrony configuration with its server, pool and peer lines replaced by the declared servers.

    The declared lines stand at the first removed line, or at the start when there was none; every other line stays as it is."""
    lines = current.splitlines(keepends=True)
    declared = [b"server " + server.encode("ascii") + b" iburst\n" for server in servers]
    kept, position = [], None
    for line in lines:
        words = line.split(None, 1)
        if words and words[0].lower() in SOURCE_DIRECTIVES:
            if position is None:
                position = len(kept)
            continue
        kept.append(line)
    if position is None:
        position = 0
    return b"".join(kept[:position] + declared + kept[position:])


def sources_detail(output):
    """The sources chronyc lists as address@mode, state and reach letters and digits, joined by underscores within MAX_DETAIL."""
    entries = []
    for line in output.splitlines():
        found = SOURCE.match(line)
        if not found or line[1] not in MARKS or not re.fullmatch(r"[0-9A-Za-z.:-]{1,253}", found.group(1)):
            continue
        entries.append("%s@%s%s%s" % (found.group(1), MODES[line[0]], MARKS[line[1]], found.group(2)[:3]))
    if not entries:
        return "none"
    detail = ""
    for entry in entries:
        joined = detail + ("_" if detail else "") + entry
        if len(joined) > MAX_DETAIL:
            break
        detail = joined
    return detail or "none"


class Host:
    """One provided host's chrony files and commands; every dependency is injectable."""

    def __init__(self, run=None, conf=CONF, directory=RECORD_DIRECTORY, owner=0, monotonic=time.monotonic, sleep=time.sleep):
        self.run = run if run is not None else self.bounded
        self.conf = conf
        self.directory = directory
        self.record = os.path.join(directory, RECORD_NAME)
        self.owner = owner
        self.monotonic = monotonic
        self.sleep = sleep

    @staticmethod
    def bounded(argv, timeout):
        """The exit code and standard output of one command, or None when it could not start or outran its time."""
        try:
            result = bounded_process.run(argv, timeout=timeout, limit=MAX_OUTPUT, environment=dict(ENVIRONMENT), cwd="/")
        except bounded_process.Failure:
            return None
        return result.code, result.stdout.decode("utf-8", "replace").strip()

    def command(self, argv, timeout=COMMAND_TIMEOUT):
        answer = self.run(argv, timeout)
        if answer is None:
            raise Refused("time-unavailable", "command")
        return answer

    def succeed(self, argv, timeout=COMMAND_TIMEOUT):
        if self.command(argv, timeout)[0] != 0:
            raise Refused("time-unavailable", "command")

    def unit_enabled(self, unit):
        return self.command([SYSTEMCTL, "is-enabled", unit])[1] in ENABLED

    def unit_active(self, unit):
        return self.command([SYSTEMCTL, "is-active", unit])[1] in ACTIVE

    def reject_conflicts(self):
        for unit in CONFLICTS:
            if self.unit_enabled(unit) or self.unit_active(unit):
                raise Refused("time-service-conflict", unit)

    def chronyd(self):
        return self.unit_enabled("chronyd"), self.unit_active("chronyd")

    def installed(self):
        return self.command([RPM, "-q", "chrony"])[0] == 0

    def install(self):
        answer = self.run([DNF, "-y", "install", "chrony"], PACKAGE_TIMEOUT)
        if answer is None or answer[0] != 0:
            raise Refused("time-package")

    def read_conf(self):
        """The file's bytes, mode, owner, group and SELinux label; every part None for an absent file.

        A link, a file that is not regular or one larger than MAX_CONF refuses before any change."""
        try:
            descriptor = os.open(self.conf, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        except FileNotFoundError:
            return {"data": None, "mode": DEFAULT_MODE, "uid": self.owner, "gid": self.owner, "label": None}
        except OSError as failure:
            raise Refused("time-config" if os.path.islink(self.conf) else "time-unavailable", "file") from failure
        try:
            status = os.fstat(descriptor)
            if not stat.S_ISREG(status.st_mode) or status.st_size > MAX_CONF:
                raise Refused("time-config")
            data = os.read(descriptor, MAX_CONF + 1)
            if len(data) > MAX_CONF:
                raise Refused("time-config")
            try:
                label = os.getxattr(descriptor, "security.selinux")
            except OSError:
                label = None
        except OSError as failure:
            raise Refused("time-unavailable", "file") from failure
        finally:
            os.close(descriptor)
        return {"data": data, "mode": stat.S_IMODE(status.st_mode), "uid": status.st_uid, "gid": status.st_gid, "label": label}

    def write_conf(self, data, mode, uid, gid, label):
        try:
            atomic_publish.publish(os.path.dirname(self.conf), os.path.basename(self.conf), data, mode,
                                   prefix=".provided-time-", owner=(uid, gid), label=label)
        except (OSError, ValueError) as failure:
            raise Refused("time-unavailable", "file") from failure

    def remove_conf(self):
        try:
            os.unlink(self.conf)
            descriptor = os.open(os.path.dirname(self.conf), os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
        except FileNotFoundError:
            pass
        except OSError as failure:
            raise Refused("time-unavailable", "file") from failure

    def read_record(self):
        """The rollback record an unfinished apply left, None when there is none; an unreadable one refuses."""
        unreadable = Refused("time-unavailable", "record")
        try:
            descriptor = os.open(self.record, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        except FileNotFoundError:
            return None
        except OSError:
            raise unreadable from None
        try:
            status = os.fstat(descriptor)
            if not stat.S_ISREG(status.st_mode) or status.st_uid != self.owner or status.st_mode & 0o077 or status.st_size > MAX_RECORD:
                raise unreadable
            data = os.read(descriptor, MAX_RECORD + 1)
        except OSError:
            raise unreadable from None
        finally:
            os.close(descriptor)
        try:
            value = json.loads(data.decode("utf-8")) if len(data) <= MAX_RECORD else None
        except (ValueError, UnicodeError):
            raise unreadable from None
        return self.checked(value, unreadable)

    @staticmethod
    def checked(value, unreadable):
        if not isinstance(value, dict) or set(value) != RECORD_KEYS or not isinstance(value["request"], str) or not DIGEST.fullmatch(value["request"]):
            raise unreadable
        for key in ("mode", "uid", "gid"):
            if type(value[key]) is not int or value[key] < 0 or key == "mode" and value[key] > 0o7777:  # pylint: disable=unidiomatic-typecheck
                raise unreadable
        if type(value["enabled"]) is not bool or type(value["active"]) is not bool:  # pylint: disable=unidiomatic-typecheck
            raise unreadable
        data, label = value["file"], value["label"]
        if data is not None:
            try:
                data = base64.b64decode(data, validate=True) if isinstance(data, str) else None
            except (binascii.Error, ValueError):
                data = None
            if data is None or len(data) > MAX_CONF:
                raise unreadable
        if label is not None:
            if not isinstance(label, str) or not label or len(label) > 4096 or "\0" in label:
                raise unreadable
            label = label.encode("utf-8") + b"\0"
        return dict(value, file=data, label=label)

    def write_record(self, current, enabled, active, digest):
        """Publish the rollback record atomically and durably as root-only canonical JSON before any change."""
        data = canonical({
            "request": digest, "file": None if current["data"] is None else base64.b64encode(current["data"]).decode("ascii"),
            "mode": current["mode"], "uid": current["uid"], "gid": current["gid"],
            "label": None if current["label"] is None else current["label"].rstrip(b"\0").decode("utf-8", "replace"),
            "enabled": enabled, "active": active,
        })
        try:
            os.makedirs(self.directory, 0o700, exist_ok=True)
            atomic_publish.publish(self.directory, RECORD_NAME, data, 0o600, prefix=".provided-time-")
        except (OSError, ValueError) as failure:
            raise Refused("time-unavailable", "record") from failure
        return {"request": digest, "file": current["data"], "mode": current["mode"], "uid": current["uid"], "gid": current["gid"],
                "label": current["label"], "enabled": enabled, "active": active}

    def remove_record(self):
        try:
            os.unlink(self.record)
            descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
        except OSError as failure:
            raise Refused("time-unavailable", "record") from failure

    def synchronized(self):
        code, output = self.command([TIMEDATECTL, "show", "--property=NTPSynchronized", "--value"])
        if code != 0:
            raise Refused("time-unavailable", "command")
        return output == "yes"

    def await_sync(self):
        """Whether the clock reports synchronized within PROOF_SECONDS, read every POLL_SECONDS."""
        deadline = self.monotonic() + PROOF_SECONDS
        while True:
            if self.synchronized():
                return True
            if self.monotonic() + POLL_SECONDS > deadline:
                return False
            self.sleep(POLL_SECONDS)

    def sources(self):
        answer = self.run([CHRONYC, "-n", "sources"], COMMAND_TIMEOUT)
        if answer is None or answer[0] != 0:
            return "unavailable"
        return sources_detail(answer[1])

    def restore(self, record):
        """Return the file and chronyd to the recorded state; any step that fails keeps the record."""
        current = self.read_conf()
        if record["file"] is None:
            self.remove_conf()
        elif current["data"] != record["file"]:
            self.write_conf(record["file"], record["mode"], record["uid"], record["gid"], record["label"])
        self.succeed([SYSTEMCTL, "enable" if record["enabled"] else "disable", "chronyd"])
        self.succeed([SYSTEMCTL, "restart" if record["active"] else "stop", "chronyd"])

    def rollback(self, record):
        try:
            self.restore(record)
        except Refused:
            raise Refused("time-unavailable", "restore") from None
        self.remove_record()

    def observe(self, servers):
        try:
            outstanding = self.read_record() is not None
        except Refused:
            outstanding = True
        try:
            current = self.read_conf()
        except Refused:
            return {"changed": False, "state": "partial" if outstanding else "absent", "postcondition": False}
        if current["data"] != rewritten(current["data"] or b"", servers):
            return {"changed": False, "state": "partial" if outstanding else "absent", "postcondition": False}
        try:
            enabled, active = self.chronyd()
            holds = enabled and active and self.synchronized()
        except Refused:
            holds = False
        holds = holds and not outstanding
        return {"changed": False, "state": "complete" if holds else "partial", "postcondition": holds}

    def apply(self, servers, digest):
        self.reject_conflicts()
        record = self.read_record()
        self.read_conf()
        if not self.installed():
            self.install()
        current = self.read_conf()
        wanted = rewritten(current["data"] or b"", servers)
        enabled, active = self.chronyd()
        differs = wanted != (current["data"] or b"")
        outstanding = record is not None
        changed = differs or not (enabled and active) or outstanding
        if changed:
            if record is None:
                record = self.write_record(current, enabled, active, digest)
            if differs:
                self.write_conf(wanted, current["mode"], current["uid"], current["gid"], current["label"])
            self.succeed([SYSTEMCTL, "enable", "chronyd"])
            self.succeed([SYSTEMCTL, "restart", "chronyd"])
        if not self.await_sync():
            detail = self.sources()
            if record is not None:
                self.rollback(record)
            raise Refused("time-unproved", detail)
        if record is not None:
            self.remove_record()
        return {"changed": changed, "state": "complete", "postcondition": True}


def execute(operation, digest, servers, host=None):
    if operation not in OPERATIONS or not isinstance(digest, str) or not DIGEST.fullmatch(digest) or os.geteuid() != 0:
        raise Refused("time-unavailable", "authority")
    if not valid_servers(servers):
        raise Refused("time-unavailable", "input")
    host = host if host is not None else Host()
    result = host.observe(servers) if operation == "observe" else host.apply(servers, digest)
    return dict(result, servers=list(servers))

"""Converge one provided RHEL host's existing registration onto its declared Satellite."""

from __future__ import annotations

import base64
import binascii
import configparser
import json
import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, managedos_rhsm_worker as rhsm
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

RHSM_CONF = "/etc/rhsm/rhsm.conf"
REDHAT_REPO = "/etc/yum.repos.d/redhat.repo"
RECORD = "/etc/bootwright/provided-registration.json"
RECORD_KEYS = frozenset(("request", "settings", "ca"))
IDENTITY_CEILING = 60
REFRESH_CEILING = 90
MAX_FILE = 1 << 22
MAX_OUTPUT = 1 << 20
MAX_RECORD = 1 << 23
OPERATIONS = ("apply", "observe")
STATES = ("converged", "unchanged", "diverged", "absent")
REFUSALS = ("registration-absent", "registration-foreign", "registration-trust", "registration-unavailable", "registration-content-root")
SETTINGS = ("server.hostname", "server.port", "server.prefix", "server.insecure", "rhsm.baseurl", "rhsm.repo_ca_cert")
TRUST_TOKENS = tuple(token.lower() for token in rhsm.TRUST_TEXT + tuple(rhsm.TRUST_ERRORS)) + ("unable to verify server's identity",)
FOREIGN = re.compile(r"\b(?:401|404|410)\b|could not be found|has been deleted|unauthorized|invalid credentials", re.IGNORECASE)
IDENTITY_LINE = "system identity"
URLS = re.compile(r"[\s,]+")


def merged(argv, timeout):
    """The exit code and the command's output, errors included, within MAX_OUTPUT bytes, or None on timeout."""
    try:
        result = bounded_process.run(argv, timeout=timeout, limit=MAX_OUTPUT, environment=dict(rhsm.ENVIRONMENT), cwd="/")
    except bounded_process.Failure as failure:
        if failure.kind == "timeout":
            return None
        raise rhsm.Refused("registration-unavailable", "tooling" if failure.kind == "command" else "service") from None
    return result.code, (result.stdout + b"\n" + result.stderr).decode("utf-8", "replace")


def read_text(path):
    """A regular file's text within MAX_FILE bytes, or None when absent."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK)
    except FileNotFoundError:
        return None
    except OSError:
        raise rhsm.Refused("registration-unavailable", "tooling") from None
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise rhsm.Refused("registration-unavailable", "tooling")
        data = rhsm.read_bounded(descriptor, MAX_FILE)
    finally:
        os.close(descriptor)
    if len(data) > MAX_FILE:
        raise rhsm.Refused("registration-unavailable", "tooling")
    return data.decode("utf-8", "replace")


def parsed(text):
    parser = configparser.RawConfigParser(interpolation=None, strict=False, allow_no_value=True)
    try:
        parser.read_string(text or "")
    except configparser.Error:
        raise rhsm.Refused("registration-unavailable", "tooling") from None
    return parser


def read_settings(path):
    """The six settings a registration changes as rhsm.conf holds them; None for one it does not set."""
    parser = parsed(read_text(path))
    settings = {}
    for name in SETTINGS:
        section, key = name.split(".")
        settings[name] = parser.get(section, key, fallback=None)
    return settings


def declared(satellite, ca):
    return {"server.hostname": satellite["hostname"], "server.port": "443", "server.prefix": "/rhsm", "server.insecure": "0",
            "rhsm.baseurl": satellite["contentBaseURL"], "rhsm.repo_ca_cert": ca}


def restore(guest, recorded):
    """The one subscription-manager config call that returns rhsm.conf to the recorded settings."""
    options = ["--" + name + "=" + recorded[name] for name in SETTINGS if recorded[name] is not None]
    removals = ["--remove=" + name for name in SETTINGS if recorded[name] is None]
    code = guest.run([rhsm.SUBSCRIPTION_MANAGER, "config"] + options + removals, rhsm.CONFIG_CEILING)
    return code == 0


def prior_ca(guest, bundle):
    """The CA path's raw bytes or None when absent, and whether it already holds the bundle as trust publishes it.

    A link, a file that is not regular or one larger than a bundle refuses before any change."""
    parent = guest.ca_parent()
    try:
        try:
            descriptor = os.open(os.path.basename(guest.ca), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK, dir_fd=parent)
        except FileNotFoundError:
            return None, False
        except OSError:
            raise rhsm.Refused("registration-unavailable", "tooling") from None
        try:
            status = os.fstat(descriptor)
            if not stat.S_ISREG(status.st_mode):
                raise rhsm.Refused("registration-unavailable", "tooling")
            data = rhsm.read_bounded(descriptor, rhsm.MAX_BUNDLE)
        except OSError:
            raise rhsm.Refused("registration-unavailable", "tooling") from None
        finally:
            os.close(descriptor)
    finally:
        os.close(parent)
    if len(data) > rhsm.MAX_BUNDLE:
        raise rhsm.Refused("registration-unavailable", "tooling")
    published = status.st_uid == guest.owner and status.st_gid == guest.group and stat.S_IMODE(status.st_mode) == 0o644
    return data, published and data == bundle


def restore_ca(guest, previous):
    """Return the CA path to the recorded bytes, unlinking a file that was not there before; True when it holds them."""
    parent = guest.ca_parent()
    try:
        if previous is None:
            try:
                os.unlink(os.path.basename(guest.ca), dir_fd=parent)
            except FileNotFoundError:
                pass
            os.fsync(parent)
        elif guest.present_ca(parent) != previous:
            guest.publish_ca(parent, previous)
        return True
    except (rhsm.Refused, OSError):
        return False
    finally:
        os.close(parent)


def current_ca(guest):
    """The bytes at the CA path, None when absent or not safely readable."""
    try:
        parent = guest.ca_parent()
    except rhsm.Refused:
        return None
    try:
        return guest.present_ca(parent)
    except (rhsm.Refused, OSError):
        return None
    finally:
        os.close(parent)


def classify(answer):
    """The refusal for a failed identity proof, read from the command's own words and never returned."""
    if answer is None:
        return rhsm.Refused("registration-unavailable", "timeout")
    text = answer[1].lower()
    if any(token in text for token in TRUST_TOKENS):
        return rhsm.Refused("registration-trust")
    if FOREIGN.search(text):
        return rhsm.Refused("registration-foreign")
    return rhsm.Refused("registration-unavailable", "service")


def identity_of(output):
    for line in output.splitlines():
        key, separator, value = line.partition(":")
        if separator and key.strip() == IDENTITY_LINE:
            return value.strip()
    return None


def prove_consumer(capture, consumer):
    answer = capture([rhsm.SUBSCRIPTION_MANAGER, "identity"], IDENTITY_CEILING)
    if answer is None or answer[0] != 0 or identity_of(answer[1]) != consumer:
        raise classify(answer)


def content_root_holds(repo, root):
    """Whether redhat.repo holds a repository and every base URL of every one begins with the content root and '/'."""
    parser = parsed(read_text(repo))
    prefix = root.rstrip("/") + "/"
    sections = parser.sections()
    for section in sections:
        urls = [url for url in URLS.split(parser.get(section, "baseurl", fallback=None) or "") if url]
        if not urls or not all(url.startswith(prefix) for url in urls):
            return False
    return bool(sections)


def regenerate(capture, repo, root):
    for argv, ceiling in (([rhsm.SUBSCRIPTION_MANAGER, "refresh"], REFRESH_CEILING),
                          ([rhsm.SUBSCRIPTION_MANAGER, "repos", "--list-enabled"], rhsm.REPOSITORIES_CEILING)):
        answer = capture(argv, ceiling)
        if answer is None:
            raise rhsm.Refused("registration-unavailable", "timeout")
        if answer[0] != 0:
            raise rhsm.Refused("registration-unavailable", "service")
    if not content_root_holds(repo, root):
        raise rhsm.Refused("registration-content-root")


class Host:
    """One provided host's registration files and commands; every dependency is injectable."""

    def __init__(self, guest=None, capture=merged, conf=RHSM_CONF, repo=REDHAT_REPO, record=RECORD, directory=rhsm.RECORD_DIRECTORY):
        self.guest = guest if guest is not None else rhsm.Guest()
        self.capture = capture
        self.conf = conf
        self.repo = repo
        self.record = record
        self.directory = directory

    def read_record(self):
        """The rollback record an unfinished apply left, None when there is none; an unreadable one refuses."""
        unreadable = rhsm.Refused("registration-unavailable", "tooling", detail="record")
        try:
            descriptor = os.open(self.record, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        except FileNotFoundError:
            return None
        except OSError:
            raise unreadable from None
        try:
            status = os.fstat(descriptor)
            if not stat.S_ISREG(status.st_mode) or status.st_uid != self.guest.owner or status.st_mode & 0o077 or status.st_size > MAX_RECORD:
                raise unreadable
            data = rhsm.read_bounded(descriptor, MAX_RECORD)
        except OSError:
            raise unreadable from None
        finally:
            os.close(descriptor)
        try:
            value = json.loads(data.decode("utf-8")) if len(data) <= MAX_RECORD else None
        except (ValueError, UnicodeError):
            raise unreadable from None
        if not isinstance(value, dict) or set(value) != RECORD_KEYS or not isinstance(value["request"], str) or not rhsm.DIGEST.fullmatch(value["request"]):
            raise unreadable
        settings, ca = value["settings"], value["ca"]
        if not isinstance(settings, dict) or set(settings) != set(SETTINGS):
            raise unreadable
        if any(item is not None and (not isinstance(item, str) or "\0" in item) for item in settings.values()):
            raise unreadable
        if ca is not None:
            try:
                ca = base64.b64decode(ca, validate=True) if isinstance(ca, str) else None
            except (binascii.Error, ValueError):
                ca = None
            if ca is None or len(ca) > rhsm.MAX_BUNDLE:
                raise unreadable
        return {"request": value["request"], "settings": settings, "ca": ca}

    def write_record(self, record):
        """Publish the rollback record atomically and durably as root-only canonical JSON before any change."""
        ca = None if record["ca"] is None else base64.b64encode(record["ca"]).decode("ascii")
        data = canonical({"request": record["request"], "settings": record["settings"], "ca": ca})
        if len(data) > MAX_RECORD:
            raise rhsm.Refused("registration-unavailable", "tooling", detail="record")
        temporary = os.path.join(self.directory, ".provided-registration.json.tmp")
        try:
            os.makedirs(self.directory, 0o700, exist_ok=True)
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
        except OSError:
            rhsm.quietly(lambda: os.unlink(temporary))
            raise rhsm.Refused("registration-unavailable", "tooling", detail="record") from None

    def remove_record(self):
        try:
            os.unlink(self.record)
            self.sync_directory()
        except OSError:
            raise rhsm.Refused("registration-unavailable", "tooling", detail="record") from None

    def sync_directory(self):
        descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def converged(self, satellite):
        return (read_settings(self.conf) == declared(satellite, self.guest.ca) and current_ca(self.guest) == satellite["caBundle"]
                and content_root_holds(self.repo, satellite["contentBaseURL"]))

    def observe(self, satellite):
        present, consumer = self.guest.certificate()
        try:
            outstanding = self.read_record() is not None
        except rhsm.Refused:
            outstanding = True
        if outstanding:
            return {"changed": False, "consumer": consumer or "", "state": "diverged", "postcondition": False}
        if not present:
            return {"changed": False, "consumer": "", "state": "absent", "postcondition": False}
        try:
            converged = consumer is not None and self.converged(satellite)
        except rhsm.Refused:
            converged = False
        return {"changed": False, "consumer": consumer or "", "state": "converged" if converged else "diverged", "postcondition": converged}

    def apply(self, satellite, digest):
        present, consumer = self.guest.certificate()
        if not present:
            raise rhsm.Refused("registration-absent")
        if consumer is None:
            raise rhsm.Refused("registration-foreign")
        record = self.read_record()
        settings = read_settings(self.conf)
        previous, published = prior_ca(self.guest, satellite["caBundle"])
        configured = not published or settings != declared(satellite, self.guest.ca)
        outstanding = record is not None
        if configured and not outstanding:
            record = {"request": digest, "settings": settings, "ca": previous}
            self.write_record(record)
        try:
            if configured:
                self.guest.trust(satellite["caBundle"])
                self.guest.configure(satellite)
            prove_consumer(self.capture, consumer)
        except rhsm.Refused as refused:
            if record is not None:
                self.rollback(record, refused)
            raise
        regenerate(self.capture, self.repo, satellite["contentBaseURL"])
        if record is not None:
            self.remove_record()
        changed = configured or outstanding
        return {"changed": changed, "consumer": consumer, "state": "converged" if changed else "unchanged", "postcondition": True}

    def rollback(self, record, refused):
        """Return the host to the recorded settings and CA; the record goes only once both are back."""
        restored = restore(self.guest, record["settings"])
        restored = restore_ca(self.guest, record["ca"]) and restored
        if restored:
            try:
                self.remove_record()
            except rhsm.Refused:
                restored = False
        if not restored and not refused.detail:
            refused.detail = "settings-not-restored"


def satellite_of(hostname, content_base_url, ca_bundle):
    try:
        rhsm.ca_bundle(ca_bundle)
    except rhsm.Refused:
        raise rhsm.Refused("registration-trust", detail="bundle") from None
    try:
        return rhsm.satellite_of({"hostname": hostname, "contentBaseURL": content_base_url, "caBundle": ca_bundle})
    except rhsm.Refused:
        raise rhsm.Refused("registration-unavailable", detail="input") from None


def execute(operation, digest, hostname, content_base_url, ca_bundle, host=None):
    if operation not in OPERATIONS or not isinstance(digest, str) or not rhsm.DIGEST.fullmatch(digest) or os.geteuid() != 0:
        raise rhsm.Refused("registration-unavailable", "authority")
    satellite = satellite_of(hostname, content_base_url, ca_bundle)
    host = host if host is not None else Host()
    return host.observe(satellite) if operation == "observe" else host.apply(satellite, digest)

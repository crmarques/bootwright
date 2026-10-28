"""The fixed root worker that registers an installed machine with its subscription service.

It runs on the installed machine as root, under the system's isolated Python,
and reads one JSON request on standard input: the organization and the
activation key reach it only there. It registers through the RHSM D-Bus API's
private registration bus with empty connection options, so the hosted service
the machine's own configuration names is the only one it reaches, and it never
runs subscription-manager register, clean or --force.

A registration this installation made is proved by the consumer certificate's
CN together with a root-only record keyed by the installation request's digest.
The record is written pending before the service is asked, so a reply lost
between the service and the record is adopted by the next run instead of
looking foreign. Anything else is foreign and left registered.

A request updating within its release names that release: before the
repositories the worker proves `subscription-manager release --list` offers
it and sets it with `subscription-manager release --set`, on every run.

The answer is one line of JSON carrying only closed reasons, states and the
consumer UUID; no service text, no secret and no traceback leaves the worker.
Only the standard library is imported at load; dbus and rhsm.certificate are
imported when the guest needs them.
"""

from __future__ import annotations

import json
import os
import re
import stat
import subprocess
import sys

CONSUMER_CERT = "/etc/pki/consumer/cert.pem"
RECORD_DIRECTORY = "/etc/bootwright"
RECORD = "/etc/bootwright/registration.json"
SUBSCRIPTION_MANAGER = "/usr/sbin/subscription-manager"
BUS_NAME = "com.redhat.RHSM1"
REGISTER_SERVER_PATH = "/com/redhat/RHSM1/RegisterServer"
REGISTER_SERVER_INTERFACE = "com.redhat.RHSM1.RegisterServer"
REGISTER_PATH = "/com/redhat/RHSM1/Register"
REGISTER_INTERFACE = "com.redhat.RHSM1.Register"
REGISTER_CEILING = 300
REPOSITORIES_CEILING = 300
UNREGISTER_CEILING = 300
RELEASE_CEILING = 60
MAX_RELEASES = 65536
MAX_INPUT = 65536
MAX_RECORD = 4096
MAX_REPLY = 1 << 20
MAX_CERTIFICATE = 65536
MAX_REPOSITORIES = 256
REASONS = ("registration-foreign", "registration-refused", "registration-identity-collision",
           "registration-input", "registration-unavailable", "registration-repositories",
           "update-release-unavailable", "transport")
CAUSES = {"registration-unavailable": ("service", "timeout", "tooling", "unregister")}
STATES = {"register": ("registered", "adopted", "unchanged"),
          "unregister": ("unregistered", "absent", "retained"),
          "inspect": ("owned", "pending", "absent", "foreign")}
OWNED_STATES = frozenset(("registered", "adopted", "unchanged", "owned"))
KEYS = {"register": frozenset(("operation", "request", "organization", "activationKey", "enable", "disable", "release")),
        "unregister": frozenset(("operation", "request")),
        "inspect": frozenset(("operation", "request"))}
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
DIGEST = re.compile(r"[0-9a-f]{64}")
RELEASE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
SECRET = re.compile(rb"[\x21-\x7e]{1,255}")
REPOSITORY = re.compile(r"[^\s\"'/\\#,%][^\s\"'/\\#,]{0,254}")
PRINTABLE = re.compile(r"[\x21-\x7e]+")
COLLISION = re.compile(r"The DMI UUID of this host \(.*\) matches other registered hosts")
ALREADY_REGISTERED = "This system is already registered."
UNAVAILABLE = frozenset(("ConnectionException", "ConnectionSetupException",
                         "ConnectionOSErrorException", "ProxyException",
                         "RemoteServerException", "RateLimitExceededException"))
TOOLING_ERRORS = frozenset(("org.freedesktop.DBus.Error.ServiceUnknown",
                            "org.freedesktop.DBus.Error.NameHasNoOwner",
                            "org.freedesktop.DBus.Error.Spawn.ServiceNotFound"))
TIMEOUT_ERRORS = frozenset(("org.freedesktop.DBus.Error.NoReply",
                            "org.freedesktop.DBus.Error.Timeout",
                            "org.freedesktop.DBus.Error.TimedOut"))
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
UNREADABLE = "unreadable"


class Refused(Exception):
    """A closed refusal; detail is fixed text for the controller and never part of the answer."""

    def __init__(self, reason, cause=None, detail=""):
        super().__init__(reason)
        self.reason = reason
        self.cause = cause
        self.detail = detail


def secret_value(raw, name):
    """One line of printable ASCII without whitespace, allowing one final LF; the value is never echoed."""
    value = raw[:-1] if isinstance(raw, bytes) and raw.endswith(b"\n") else raw
    if not isinstance(value, bytes) or not SECRET.fullmatch(value):
        raise Refused("registration-input", detail="the " + name + " is not one line of printable ASCII without whitespace")
    return value.decode("ascii")


def repository_list(values, star):
    """At most MAX_REPOSITORIES unique subscription repository IDs."""
    if not isinstance(values, list) or len(values) > MAX_REPOSITORIES:
        return False
    if not all(isinstance(value, str) for value in values) or len(set(values)) != len(values):
        return False
    return all(star if value == "*" else bool(REPOSITORY.fullmatch(value) and PRINTABLE.fullmatch(value)) for value in values)


def repositories(enable, disable):
    """The validated enable and disable lists; only disable may purge with '*', and they are disjoint."""
    if not repository_list(enable, False) or not repository_list(disable, True) or set(enable) & set(disable):
        raise Refused("registration-input", detail="the subscription repository list is not valid")
    return list(enable), list(disable)


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


def load_dbus():
    """The system's python3-dbus, imported only on the guest."""
    try:
        import dbus  # pylint: disable=import-outside-toplevel
        import dbus.connection  # pylint: disable=import-outside-toplevel
        import dbus.exceptions  # pylint: disable=import-outside-toplevel
    except ImportError:
        raise Refused("registration-unavailable", "tooling") from None
    return dbus


def consumer_identity(path):
    """The consumer UUID in the certificate's subject CN, or None; the organization is never read."""
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        data = read_bounded(descriptor, MAX_CERTIFICATE)
    finally:
        os.close(descriptor)
    if len(data) > MAX_CERTIFICATE:
        return None
    try:
        from rhsm.certificate import create_from_pem  # pylint: disable=import-outside-toplevel
    except ImportError:
        raise Refused("registration-unavailable", "tooling") from None
    try:
        common = create_from_pem(data.decode("ascii")).subject.get("CN")
    except Exception:  # pylint: disable=broad-exception-caught
        return None
    return common if isinstance(common, str) and UUID.fullmatch(common) else None


def bounded_run(argv, timeout):
    """The exit code of a fixed native command whose output is discarded, or None on timeout."""
    try:
        # A fixed argv on the guest; no AnsibleModule runs there.
        # pylint: disable-next=ansible-bad-function
        completed = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   env=dict(ENVIRONMENT), cwd="/", timeout=timeout, check=False)
    except subprocess.TimeoutExpired:
        return None
    except FileNotFoundError:
        raise Refused("registration-unavailable", "tooling") from None
    return completed.returncode


def bounded_capture(argv, timeout):
    """The exit code and at most MAX_RELEASES + 1 bytes of a fixed native command's output, or None on timeout."""
    try:
        # A fixed argv on the guest; no AnsibleModule runs there.
        # pylint: disable-next=ansible-bad-function
        completed = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   env=dict(ENVIRONMENT), cwd="/", timeout=timeout, check=False)
    except subprocess.TimeoutExpired:
        return None
    except FileNotFoundError:
        raise Refused("registration-unavailable", "tooling") from None
    return completed.returncode, completed.stdout[:MAX_RELEASES + 1]


def offered(output):
    """The releases a release --list output names, one per line; its table frame and title are not releases."""
    if len(output) > MAX_RELEASES:
        return frozenset()
    lines = output.decode("ascii", "replace").splitlines()
    return frozenset(line.strip() for line in lines if RELEASE.fullmatch(line.strip()))


def classify(error):
    """A closed refusal for a D-Bus error; the service's text is matched, never carried."""
    name = error.get_dbus_name()
    if name in TOOLING_ERRORS:
        return Refused("registration-unavailable", "tooling")
    if name in TIMEOUT_ERRORS:
        return Refused("registration-unavailable", "timeout")
    message = error.get_dbus_message()
    text, exception = (message if isinstance(message, str) else ""), ""
    try:
        parsed = json.loads(text)
    except ValueError:
        parsed = None
    if isinstance(parsed, dict):
        text = parsed.get("message") if isinstance(parsed.get("message"), str) else ""
        exception = parsed.get("exception") if isinstance(parsed.get("exception"), str) else ""
    if COLLISION.search(text):
        return Refused("registration-identity-collision")
    if ALREADY_REGISTERED in text:
        return Refused("registration-foreign")
    if exception in UNAVAILABLE:
        return Refused("registration-unavailable", "service")
    return Refused("registration-refused")


def consumer_of(reply):
    """The consumer UUID of RegisterWithActivationKeys' JSON reply."""
    if not isinstance(reply, str) or len(reply.encode("utf-8", "surrogatepass")) > MAX_REPLY:
        raise Refused("transport")
    try:
        consumer = json.loads(reply)
    except ValueError:
        raise Refused("transport") from None
    uuid = consumer.get("uuid") if isinstance(consumer, dict) else None
    if not isinstance(uuid, str) or not UUID.fullmatch(uuid):
        raise Refused("transport")
    return uuid


def quietly(action):
    """Run a cleanup step whose failure changes nothing the worker reports."""
    try:
        action()
    except Exception:  # pylint: disable=broad-exception-caught
        pass


class Guest:
    """The installed machine's registration state; every dependency is injectable."""

    def __init__(self, cert=CONSUMER_CERT, record=RECORD, directory=RECORD_DIRECTORY, owner=0,
                 run=bounded_run, bus=load_dbus, identity=consumer_identity, capture=bounded_capture):
        self.cert = cert
        self.record = record
        self.directory = directory
        self.owner = owner
        self.run = run
        self.bus = bus
        self.identity = identity
        self.capture = capture

    def certificate(self):
        """Whether a consumer certificate is present, and its consumer UUID when it names one."""
        try:
            status = os.lstat(self.cert)
        except FileNotFoundError:
            return False, None
        if not stat.S_ISREG(status.st_mode):
            return True, None
        return True, self.identity(self.cert)

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
        if not isinstance(value, dict) or set(value) != {"consumer", "request"}:
            return UNREADABLE
        consumer, request = value["consumer"], value["request"]
        if not isinstance(consumer, str) or not (consumer == "" or UUID.fullmatch(consumer)):
            return UNREADABLE
        if not isinstance(request, str) or not DIGEST.fullmatch(request):
            return UNREADABLE
        return value

    def state(self, request):
        """(kind, certificate present, consumer UUID, record): kind is owned, pending, absent or foreign."""
        present, common = self.certificate()
        record = self.read_record()
        own = isinstance(record, dict) and record["request"] == request
        if present:
            if common and own and record["consumer"] == common:
                return "owned", present, common, record
            if common and own and record["consumer"] == "":
                return "pending", present, common, record
            return "foreign", present, common, record
        if record is None or own:
            return "absent", present, None, record
        return "foreign", present, None, record

    def sync_directory(self):
        descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def write(self, consumer, request):
        """Publish the record atomically and durably as root-only canonical JSON."""
        os.makedirs(self.directory, 0o700, exist_ok=True)
        data = json.dumps({"consumer": consumer, "request": request}, sort_keys=True, separators=(",", ":")).encode("ascii")
        temporary = os.path.join(self.directory, ".registration.json.tmp")
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

    def remove(self):
        os.unlink(self.record)
        self.sync_directory()

    def register(self, organization, key):
        """One RegisterWithActivationKeys call on the private bus with empty options; the consumer UUID."""
        dbus = self.bus()
        try:
            system = dbus.SystemBus()
            server = dbus.Interface(system.get_object(BUS_NAME, REGISTER_SERVER_PATH), REGISTER_SERVER_INTERFACE)
            address = server.Start("", timeout=REGISTER_CEILING)
        except dbus.exceptions.DBusException as error:
            raise classify(error) from None
        private = None
        try:
            private = dbus.connection.Connection(address)
            register = dbus.Interface(private.get_object(None, REGISTER_PATH, introspect=False), REGISTER_INTERFACE)
            reply = register.RegisterWithActivationKeys(
                organization, dbus.Array([key], signature="s"), dbus.Dictionary({}, signature="sv"),
                dbus.Dictionary({}, signature="sv"), "", timeout=REGISTER_CEILING)
        except dbus.exceptions.DBusException as error:
            raise classify(error) from None
        finally:
            if private is not None:
                quietly(private.close)
            quietly(lambda: server.Stop("", timeout=REGISTER_CEILING))
        return consumer_of(reply)

    def repositories(self, enable, disable):
        """One subscription-manager repos call: every disable, then every enable, in order."""
        if not enable and not disable:
            return
        argv = [SUBSCRIPTION_MANAGER, "repos"] + ["--disable=" + item for item in disable] + ["--enable=" + item for item in enable]
        code = self.run(argv, REPOSITORIES_CEILING)
        if code is None:
            raise Refused("registration-unavailable", "timeout")
        if code != 0:
            raise Refused("registration-repositories")

    def release(self, release):
        """Prove the subscription service offers the release, then make it the sticky minor release."""
        listed = self.capture([SUBSCRIPTION_MANAGER, "release", "--list"], RELEASE_CEILING)
        if listed is None:
            raise Refused("registration-unavailable", "timeout")
        if listed[0] != 0:
            raise Refused("registration-unavailable", "service")
        if release not in offered(listed[1]):
            raise Refused("update-release-unavailable")
        code = self.run([SUBSCRIPTION_MANAGER, "release", "--set=" + release], RELEASE_CEILING)
        if code is None:
            raise Refused("registration-unavailable", "timeout")
        if code != 0:
            raise Refused("registration-unavailable", "service")


def result(changed, consumer, state, enable=(), disable=()):
    return {"changed": changed, "consumer": consumer or "", "owned": state in OWNED_STATES,
            "repositories": {"disable": list(disable), "enable": list(enable)}, "state": state}


def register_operation(guest, payload):
    request, enable, disable = payload["request"], payload["enable"], payload["disable"]
    kind, common = guest.state(request)[::2]
    if kind == "foreign":
        raise Refused("registration-foreign")
    if kind == "owned":
        state = "unchanged"
    elif kind == "pending":
        guest.write(common, request)
        state = "adopted"
    else:
        guest.write("", request)
        consumer = guest.register(payload["organization"], payload["activationKey"])
        common = guest.certificate()[1]
        if common != consumer:
            raise Refused("transport")
        guest.write(common, request)
        state = "registered"
    if payload["release"]:
        guest.release(payload["release"])
    guest.repositories(enable, disable)
    return result(state != "unchanged", common, state, enable, disable)


def unregister_operation(guest, payload):
    kind, present, common, record = guest.state(payload["request"])
    if kind in ("owned", "pending"):
        code = guest.run([SUBSCRIPTION_MANAGER, "unregister"], UNREGISTER_CEILING)
        if code != 0 or guest.certificate()[0]:
            raise Refused("registration-unavailable", "unregister")
        guest.remove()
        return result(True, common, "unregistered")
    if kind == "foreign" and present:
        return result(False, "", "retained")
    if kind == "absent" and record is not None:
        guest.remove()
        return result(True, "", "absent")
    return result(False, "", "absent")


def inspect_operation(guest, payload):
    kind, common = guest.state(payload["request"])[::2]
    return result(False, common if kind in ("owned", "pending") else "", kind)


OPERATIONS = {"register": register_operation, "unregister": unregister_operation, "inspect": inspect_operation}


def text_secret(value, name):
    if not isinstance(value, str):
        raise Refused("registration-input", detail="the " + name + " is not one line of printable ASCII without whitespace")
    try:
        raw = value.encode("ascii")
    except UnicodeError:
        raise Refused("registration-input", detail="the " + name + " is not one line of printable ASCII without whitespace") from None
    return secret_value(raw, name)


def parse_request(data):
    """The validated request; every value is checked before any guest state is read."""
    if not isinstance(data, bytes) or len(data) > MAX_INPUT:
        raise Refused("registration-input")
    try:
        payload = json.loads(data.decode("utf-8"))
    except (ValueError, UnicodeError):
        raise Refused("registration-input") from None
    operation = payload.get("operation") if isinstance(payload, dict) else None
    if operation not in KEYS or set(payload) != KEYS[operation]:
        raise Refused("registration-input")
    if not isinstance(payload["request"], str) or not DIGEST.fullmatch(payload["request"]):
        raise Refused("registration-input")
    if operation == "register":
        payload["organization"] = text_secret(payload["organization"], "organization")
        payload["activationKey"] = text_secret(payload["activationKey"], "activation key")
        payload["enable"], payload["disable"] = repositories(payload["enable"], payload["disable"])
        if not isinstance(payload["release"], str) or not (payload["release"] == "" or RELEASE.fullmatch(payload["release"])):
            raise Refused("registration-input", detail="the release is not a <major>.<minor> release")
    return payload


def answer(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def main(stdin=None, stdout=None, guest=None):
    """Answer one request with exactly one line; never a traceback or any other output."""
    try:
        source = stdin if stdin is not None else sys.stdin.buffer
        payload = parse_request(source.read(MAX_INPUT + 1))
        reply = {"ok": True, "result": OPERATIONS[payload["operation"]](guest if guest is not None else Guest(), payload)}
    except Refused as refused:
        reply = {"ok": False, "reason": refused.reason if refused.reason in REASONS else "transport"}
        if refused.cause is not None and refused.cause in CAUSES.get(reply["reason"], ()):
            reply["cause"] = refused.cause
    except BaseException:  # pylint: disable=broad-exception-caught
        reply = {"ok": False, "reason": "transport"}
    target = stdout if stdout is not None else sys.stdout
    target.write(answer(reply) + "\n")
    target.flush()
    return 0

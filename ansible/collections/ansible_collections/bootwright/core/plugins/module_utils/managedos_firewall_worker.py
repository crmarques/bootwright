"""The fixed root worker that realizes host firewalld services on an installed machine.

It runs on the installed machine as root, under the system's isolated Python,
and reads one JSON request on standard input: the operation, the ownership mark
and the services, each with its name, the CIDR networks it serves and the exact
bytes of its firewalld service file. It never parses XML: a service file is
compared byte for byte, and ownership is the mark inside its <description>.

A service file is owned when it is a regular root-owned file under
/etc/firewalld/services whose bytes hold <description>MARK</description>, absent
when no entry exists and foreign for anything else, which nothing here ever
changes. A service is a member of the zone of every interface whose
network contains its first network's address (the virtual address), or, when none
does, whose own network is one of its later networks, in the permanent configuration and at
runtime. apply refuses a foreign file or a service without an interface before
it changes anything, writes each missing or different file atomically, reloads
firewalld when it wrote a file or the permanent configuration does not know a
service yet, adds the permanent membership and reloads again. remove takes every
permanent membership of an owned service back, deletes its file and reloads, and
reloads too while any zone still lists a service at runtime, so a removal that
ended between its delete and its reload completes on retry; a service without
its file keeps any permanent membership, which is not provably ours. A
membership is any zone listing a service permanently or at runtime, and a
service is covered only when every selected zone lists it in both. inspect reads
and changes nothing.

Every command is a fixed argv run without a shell, and every token read from its
output is checked against a closed character set. The answer is one line of JSON
carrying closed reasons and the evidence of each service, never command output.
Only the standard library is imported.
"""

from __future__ import annotations

import hashlib
import ipaddress
import json
import os
import re
import stat
import subprocess
import sys

SERVICES_DIRECTORY = "/etc/firewalld/services"
FIREWALL_CMD = "/usr/bin/firewall-cmd"
IP = "/usr/sbin/ip"
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
MAX_INPUT = 1 << 21
MAX_SERVICE_FILE = 65536
MAX_CONTENT = 16384
MAX_COMMAND_OUTPUT = 1 << 20
COMMAND_TIMEOUT = 120
MAX_SERVICES = 64
MAX_NETWORKS = 16
MAX_ZONES = 64
MAX_MEMBERSHIPS = 4096
MAX_MARK = 256
OPERATIONS = ("apply", "inspect", "remove")
STATES = ("owned", "absent", "foreign")
REASONS = ("host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive", "host-firewall-failed", "transport")
CAUSES = {"host-firewall-failed": ("tooling", "reload", "zone", "write", "address")}
DETAILED = ("host-firewall-foreign", "host-firewall-no-interface")
KEYS = frozenset(("operation", "mark", "services"))
SERVICE_KEYS = frozenset(("name", "networks", "content"))
ENTRY_KEYS = frozenset(("covered", "digest", "memberships", "name", "state"))
NAME = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?")
INTERFACE = re.compile(r"[A-Za-z0-9_.:@-]{1,15}")
ZONE = re.compile(r"[A-Za-z0-9_-]{1,17}")
DIGEST = re.compile(r"[0-9a-f]{64}")
MARK_FORBIDDEN = frozenset("<>&\"'")
NOT_LISTED = (1, 101)


class Refused(Exception):
    """A closed refusal; detail is a service name of a closed character set, or empty."""

    def __init__(self, reason, cause=None, detail=""):
        super().__init__(reason)
        self.reason = reason
        self.cause = cause
        self.detail = detail


def failed(cause):
    return Refused("host-firewall-failed", cause)


def valid_mark(value):
    """A mark as an XML description and a shell-free token carry it: printable ASCII without markup characters."""
    return (isinstance(value, str) and 0 < len(value) <= MAX_MARK and all(" " <= character <= "~" for character in value)
            and not MARK_FORBIDDEN & set(value))


def valid_name(value):
    return isinstance(value, str) and bool(NAME.fullmatch(value))


def valid_network(value):
    if not isinstance(value, str):
        return False
    try:
        ipaddress.ip_network(value, strict=True)
    except ValueError:
        return False
    return True


def single_host(value):
    """The virtual address as its own single-host network."""
    network = ipaddress.ip_network(value, strict=True)
    return network.prefixlen == network.max_prefixlen


def valid_networks(value):
    return (isinstance(value, list) and 0 < len(value) <= MAX_NETWORKS and all(valid_network(network) for network in value)
            and single_host(value[0]))


def valid_content(value):
    return (isinstance(value, str) and value.startswith("<?xml") and len(value) <= MAX_CONTENT
            and all(character < "\x80" for character in value))


def valid_service(value):
    return (isinstance(value, dict) and set(value) == SERVICE_KEYS and valid_name(value["name"])
            and valid_networks(value["networks"]) and valid_content(value["content"]))


def valid_services(value):
    return isinstance(value, list) and 0 < len(value) <= MAX_SERVICES and all(valid_service(item) for item in value)


def valid_membership(value):
    if not isinstance(value, dict) or set(value) != {"interface", "zone"}:
        return False
    interface, zone = value["interface"], value["zone"]
    return (isinstance(interface, str) and (interface == "" or bool(INTERFACE.fullmatch(interface)))
            and isinstance(zone, str) and bool(ZONE.fullmatch(zone)))


def valid_memberships(value):
    if not isinstance(value, list) or len(value) > MAX_MEMBERSHIPS or not all(valid_membership(item) for item in value):
        return False
    keys = [(item["zone"], item["interface"]) for item in value]
    return keys == sorted(set(keys))


def valid_entry(value):
    """One service's evidence as the worker builds it."""
    if not isinstance(value, dict) or set(value) != ENTRY_KEYS or value["state"] not in STATES or not valid_name(value["name"]):
        return False
    digest = value["digest"]
    if not isinstance(digest, str) or not (digest == "" or DIGEST.fullmatch(digest)):
        return False
    if (value["state"] == "owned" and not digest) or (value["state"] == "absent" and digest):
        return False
    return type(value["covered"]) is bool and valid_memberships(value["memberships"])  # pylint: disable=unidiomatic-typecheck


def valid_evidence(value, names=None):
    """The evidence list of a result: one valid entry per service, in request order when the names are given."""
    if not isinstance(value, list) or not 0 < len(value) <= MAX_SERVICES or not all(valid_entry(item) for item in value):
        return False
    return names is None or [item["name"] for item in value] == list(names)


def run_command(argv):
    """(exit code, standard output) of a fixed argv run without a shell; output beyond its bound is a tooling failure."""
    try:
        # A fixed argv on the guest; no AnsibleModule runs there.
        # pylint: disable-next=ansible-bad-function
        completed = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   env=dict(ENVIRONMENT), cwd="/", timeout=COMMAND_TIMEOUT, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise failed("tooling") from None
    if len(completed.stdout) > MAX_COMMAND_OUTPUT:
        raise failed("tooling")
    return completed.returncode, completed.stdout


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


def token(data, pattern):
    """One output token of the closed character set, or a tooling failure."""
    try:
        text = data.decode("ascii").strip()
    except UnicodeError:
        raise failed("tooling") from None
    if not pattern.fullmatch(text):
        raise failed("tooling")
    return text


class Guest:
    """The installed machine's firewall; every dependency is injectable."""

    def __init__(self, run=run_command, directory=SERVICES_DIRECTORY, owner=0):
        self.run = run
        self.directory = directory
        self.owner = owner

    def command(self, *arguments):
        return self.run([FIREWALL_CMD] + list(arguments))

    def require_running(self):
        code, output = self.command("--state")
        if code != 0 or output.strip() != b"running":
            raise Refused("host-firewall-inactive")

    def addresses(self):
        """Each (interface name, interface address) the machine carries, from ip -json."""
        code, output = self.run([IP, "-json", "address", "show"])
        if code != 0:
            raise failed("tooling")
        try:
            listing = json.loads(output.decode("utf-8"))
        except (ValueError, UnicodeError):
            raise failed("tooling") from None
        if not isinstance(listing, list):
            raise failed("tooling")
        found = []
        for item in listing:
            if not isinstance(item, dict) or not isinstance(item.get("addr_info", []), list):
                raise failed("tooling")
            for info in item.get("addr_info", []):
                if isinstance(info, dict) and "local" not in info:
                    continue
                found.append((item.get("ifname"), self.local(info)))
        return found

    @staticmethod
    def local(info):
        try:
            return ipaddress.ip_interface("%s/%d" % (info["local"], info["prefixlen"]))
        except (KeyError, TypeError, ValueError):
            raise failed("address") from None

    def interfaces(self, networks, found):
        """(candidate interfaces, whether there is one) for a service.

        The first network is the virtual address as a single-host prefix. The candidates are the interfaces whose
        own network contains it; only when none does, those whose own network is exactly one of the later networks."""
        address = ipaddress.ip_network(networks[0], strict=True).network_address
        names = {name for name, interface in found if interface.version == address.version and address in interface.network}
        if not names:
            later = {str(ipaddress.ip_network(text, strict=True)) for text in networks[1:]}
            names = {name for name, interface in found if str(interface.network) in later}
        for name in names:
            if not isinstance(name, str) or not INTERFACE.fullmatch(name):
                raise failed("tooling")
        return sorted(names), bool(names)

    def zone_of(self, interface):
        code, output = self.command("--get-zone-of-interface=" + interface)
        if code != 0:
            code, output = self.command("--get-default-zone")
            if code != 0:
                raise failed("tooling")
        return token(output, ZONE)

    def zones(self, permanent=True):
        code, output = self.command(*((["--permanent"] if permanent else []) + ["--get-zones"]))
        if code != 0:
            raise failed("tooling")
        names = output.split()
        if len(names) > MAX_ZONES:
            raise failed("tooling")
        return sorted({token(name, ZONE) for name in names})

    def known(self, names):
        """Whether the permanent configuration has loaded every named service; a name is only compared, never echoed."""
        code, output = self.command("--permanent", "--get-services")
        if code != 0:
            raise failed("tooling")
        loaded = set(output.split())
        return all(name.encode("ascii") in loaded for name in names)

    def lists(self, zone, name, permanent):
        arguments = (["--permanent"] if permanent else []) + ["--zone=" + zone, "--query-service=" + name]
        code, _output = self.command(*arguments)
        if code == 0:
            return True
        if code in NOT_LISTED:
            return False
        raise failed("tooling")

    def path(self, name):
        return os.path.join(self.directory, name + ".xml")

    def ownership(self, name, mark):
        """(state, file bytes or None) of a service file; anything but a root-owned regular file holding the mark is foreign."""
        path = self.path(name)
        try:
            status = os.lstat(path)
        except FileNotFoundError:
            return "absent", None
        except OSError:
            return "foreign", None
        if not stat.S_ISREG(status.st_mode):
            return "foreign", None
        try:
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        except OSError:
            return "foreign", None
        try:
            held = os.fstat(descriptor)
            if not stat.S_ISREG(held.st_mode) or held.st_uid != self.owner or held.st_size > MAX_SERVICE_FILE:
                return "foreign", None
            data = read_bounded(descriptor, MAX_SERVICE_FILE)
        finally:
            os.close(descriptor)
        if len(data) > MAX_SERVICE_FILE:
            return "foreign", None
        marked = b"<description>" + mark.encode("ascii") + b"</description>" in data
        return ("owned" if marked else "foreign"), data

    def sync_directory(self):
        descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def write(self, name, content):
        """Publish a service file atomically and durably."""
        temporary = os.path.join(self.directory, "." + name + ".xml.tmp")
        data = content.encode("ascii")
        try:
            os.makedirs(self.directory, 0o755, exist_ok=True)
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass
            descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644)
            try:
                os.fchmod(descriptor, 0o644)
                view = memoryview(data)
                while view:
                    view = view[os.write(descriptor, view):]
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
            os.replace(temporary, self.path(name))
            self.sync_directory()
        except OSError:
            raise failed("write") from None

    def delete(self, name):
        try:
            os.unlink(self.path(name))
            self.sync_directory()
        except FileNotFoundError:
            pass
        except OSError:
            raise failed("write") from None

    def reload(self):
        code, _output = self.command("--reload")
        if code != 0:
            raise failed("reload")

    def alter(self, zone, name, verb):
        code, _output = self.command("--permanent", "--zone=" + zone, "--" + verb + "-service=" + name)
        if code != 0:
            raise failed("zone")

    def entry(self, service, mark, found, zones, live):
        """One service's inspect evidence: a zone that lists it permanently or at runtime is a membership."""
        name = service["name"]
        state, data = self.ownership(name, mark)
        interfaces, covered = self.interfaces(service["networks"], found)
        memberships, held = [], set()
        for interface in interfaces:
            zone = self.zone_of(interface)
            held.add(zone)
            permanent = self.lists(zone, name, True)
            runtime = self.lists(zone, name, False)
            if permanent or runtime:
                memberships.append((zone, interface))
            if not (permanent and runtime):
                covered = False
        for zone in sorted(set(zones) | set(live)):
            if zone not in held and ((zone in zones and self.lists(zone, name, True)) or (zone in live and self.lists(zone, name, False))):
                memberships.append((zone, ""))
        return {"covered": covered, "digest": "" if data is None else hashlib.sha256(data).hexdigest(),
                "memberships": [{"interface": interface, "zone": zone} for zone, interface in sorted(memberships)],
                "name": name, "state": state}

    def evidence(self, payload):
        found = self.addresses()
        zones, live = self.zones(), self.zones(permanent=False)
        return [self.entry(service, payload["mark"], found, zones, live) for service in payload["services"]]

    def targets(self, payload, found):
        """Every service's (state, bytes, [(interface, zone)]); a foreign file or a network without an interface refuses."""
        planned = []
        for service in payload["services"]:
            name = service["name"]
            state, data = self.ownership(name, payload["mark"])
            if state == "foreign":
                raise Refused("host-firewall-foreign", detail=name)
            interfaces, complete = self.interfaces(service["networks"], found)
            if not complete:
                raise Refused("host-firewall-no-interface", detail=name)
            planned.append((state, data, sorted({self.zone_of(interface) for interface in interfaces})))
        return planned

    def missing_at_runtime(self, payload, planned):
        return any(not self.lists(zone, service["name"], False)
                   for service, (_state, _data, zones) in zip(payload["services"], planned) for zone in zones)

    def listed_at_runtime(self, payload):
        live = self.zones(permanent=False)
        return any(self.lists(zone, service["name"], False) for service in payload["services"] for zone in live)


def apply_operation(guest, payload):
    guest.require_running()
    planned = guest.targets(payload, guest.addresses())
    changed = False
    for service, (state, data, _zones) in zip(payload["services"], planned):
        if state == "absent" or data != service["content"].encode("ascii"):
            guest.write(service["name"], service["content"])
            changed = True
    if changed or not guest.known([service["name"] for service in payload["services"]]):
        guest.reload()
        changed = True
    added = False
    for service, (_state, _data, zones) in zip(payload["services"], planned):
        for zone in zones:
            if not guest.lists(zone, service["name"], True):
                guest.alter(zone, service["name"], "add")
                added = True
    reloaded = False
    if added or guest.missing_at_runtime(payload, planned):
        guest.reload()
        reloaded = True
    return {"changed": changed or added or reloaded, "services": guest.evidence(payload)}


def remove_operation(guest, payload):
    guest.require_running()
    states = []
    for service in payload["services"]:
        state, _data = guest.ownership(service["name"], payload["mark"])
        if state == "foreign":
            raise Refused("host-firewall-foreign", detail=service["name"])
        states.append(state)
    zones = guest.zones()
    changed = False
    for service, state in zip(payload["services"], states):
        if state != "owned":
            continue
        for zone in zones:
            if guest.lists(zone, service["name"], True):
                guest.alter(zone, service["name"], "remove")
                changed = True
        guest.delete(service["name"])
        changed = True
    if changed or guest.listed_at_runtime(payload):
        guest.reload()
        changed = True
    return {"changed": changed, "services": guest.evidence(payload)}


def inspect_operation(guest, payload):
    guest.require_running()
    return {"changed": False, "services": guest.evidence(payload)}


HANDLERS = {"apply": apply_operation, "inspect": inspect_operation, "remove": remove_operation}


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
    if not valid_mark(payload["mark"]) or not valid_services(payload["services"]):
        raise Refused("transport")
    if len({service["name"] for service in payload["services"]}) != len(payload["services"]):
        raise Refused("transport")
    return payload


def answer(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def refusal(refused):
    reply = {"ok": False, "reason": refused.reason if refused.reason in REASONS else "transport"}
    if refused.cause is not None and refused.cause in CAUSES.get(reply["reason"], ()):
        reply["cause"] = refused.cause
    if reply["reason"] in DETAILED and valid_name(refused.detail):
        reply["detail"] = refused.detail
    return reply


def main(stdin=None, stdout=None, guest=None):
    """Answer one request with exactly one line; never a traceback or any other output."""
    try:
        source = stdin if stdin is not None else sys.stdin.buffer
        payload = parse_request(source.read(MAX_INPUT + 1))
        reply = {"ok": True, "result": HANDLERS[payload["operation"]](guest if guest is not None else Guest(), payload)}
    except Refused as refused:
        reply = refusal(refused)
    except BaseException:  # pylint: disable=broad-exception-caught
        reply = {"ok": False, "reason": "transport"}
    target = stdout if stdout is not None else sys.stdout
    target.write(answer(reply) + "\n")
    target.flush()
    return 0

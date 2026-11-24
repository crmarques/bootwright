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

import fcntl
import hashlib
import ipaddress
import json
import os
import re
import stat
import subprocess
import sys
import tempfile

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
MAX_ADDRESSES = 64
MAX_ZONES = 64
MAX_MEMBERSHIPS = 4096
MAX_MARK = 256
OPERATIONS = ("apply", "inspect", "remove", "confirm")
STATES = ("owned", "absent", "foreign")
REASONS = ("host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive", "host-firewall-failed", "transport")
CAUSES = {"host-firewall-failed": ("tooling", "reload", "zone", "write", "address", "runtime-drift", "enable", "ssh-admission")}
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


# The system Python and firewalld's own client library read every native
# registry in one bounded process, avoiding hundreds of firewall-cmd launches.
REGISTRY_COMPARISON = r"""
def registries_equal(client, owned):
    config = client.config()
    descriptors = (
        ("listServices", "getServiceSettings", "getServiceNames", "getServiceByName"),
        ("listIcmpTypes", "getIcmpTypeSettings", "getIcmpTypeNames", "getIcmpTypeByName"),
        ("getIPSets", "getIPSetSettings", "getIPSetNames", "getIPSetByName"),
        ("getHelpers", "getHelperSettings", "getHelperNames", "getHelperByName"),
    )
    for listing, setting, names, by_name in descriptors:
        runtime = set(getattr(client, listing)())
        permanent = set(getattr(config, names)())
        excluded = set(owned) if listing == "listServices" else set()
        runtime, permanent = runtime - excluded, permanent - excluded
        if len(runtime | permanent) > 4096 or runtime != permanent:
            return False
        for name in sorted(runtime):
            live = getattr(client, setting)(name).settings
            saved = getattr(config, by_name)(name).getSettings().settings
            if live != saved:
                return False
    return True
"""


class Refused(Exception):
    """A closed refusal; detail is a service name of a closed character set, or empty."""

    def __init__(self, reason, cause=None, detail=""):
        super().__init__(reason)
        self.reason = reason
        self.cause = cause
        self.detail = detail


class Oversized(Refused):
    """A tooling failure whose cause is output beyond its bound, which a caller may answer another way."""


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


def valid_addresses(value):
    return (isinstance(value, list) and 0 < len(value) <= MAX_ADDRESSES
            and all(isinstance(item, str) and 0 < len(item) <= 253
                    and re.fullmatch(r"[A-Za-z0-9_.:-]+", item) for item in value))


def valid_service(value):
    if not isinstance(value, dict) or not SERVICE_KEYS <= set(value) <= SERVICE_KEYS | {"addresses"}:
        return False
    selectors = (valid_addresses(value["addresses"]) and not value["networks"] if "addresses" in value
                 else valid_networks(value["networks"]))
    return valid_name(value["name"]) and selectors and valid_content(value["content"])


def valid_services(value):
    return isinstance(value, list) and 0 <= len(value) <= MAX_SERVICES and all(valid_service(item) for item in value)


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
    if not isinstance(value, list) or not 0 <= len(value) <= MAX_SERVICES or not all(valid_entry(item) for item in value):
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
        raise Oversized("host-firewall-failed", "tooling")
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

    def __init__(self, run=run_command, directory=SERVICES_DIRECTORY, owner=0, preserve_runtime=False):
        self.preserve_runtime = preserve_runtime
        self.owned_names = set()
        self.snapshots = {True: None, False: None}
        self.per_zone = {True: False, False: False}
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

    def service_interfaces(self, service, found):
        if "addresses" not in service:
            return self.interfaces(service["networks"], found)
        names = set()
        for address in service["addresses"]:
            resolved = self.resolve(address)
            matches = {name for name, interface in found if interface.ip == resolved}
            if len(matches) != 1:
                return [], False
            names.update(matches)
        if not all(isinstance(name, str) and INTERFACE.fullmatch(name) for name in names):
            raise failed("address")
        return sorted(names), bool(names)

    def resolve(self, address):
        try:
            return ipaddress.ip_address(address)
        except ValueError:
            code, output = self.run(["/usr/bin/getent", "ahosts", address])
        if code != 0:
            raise failed("address")
        try:
            addresses = {ipaddress.ip_address(line.split()[0].decode("ascii")) for line in output.splitlines()}
        except (ValueError, IndexError, UnicodeError):
            raise failed("address") from None
        if len(addresses) != 1:
            raise failed("address")
        return addresses.pop()

    def zone_of(self, interface):
        code, output = self.command("--get-zone-of-interface=" + interface)
        if code == 2:
            code, output = self.command("--get-default-zone")
            if code != 0:
                raise failed("tooling")
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

    def snapshot(self, permanent):
        """Every zone's listed services on one side, read once until a change of that side forgets it.

        None when the one listing cannot be used: it exceeds the output bound or is not ASCII, as a host with many
        foreign rich rules or a UTF-8 log prefix gives. That side is then answered zone by zone, and stays so."""
        if self.per_zone[permanent]:
            return None
        if self.snapshots[permanent] is None:
            try:
                code, output = self.command(*((["--permanent"] if permanent else []) + ["--list-all-zones"]))
            except Oversized:
                self.per_zone[permanent] = True
                return None
            if code != 0:
                raise failed("tooling")
            if len(output) > MAX_COMMAND_OUTPUT or not output.isascii():
                self.per_zone[permanent] = True
                return None
            self.snapshots[permanent] = zone_services(output)
        return self.snapshots[permanent]

    def lists(self, zone, name, permanent):
        services = self.snapshot(permanent)
        if services is None:
            return self.queries(zone, name, permanent)
        if zone not in services:
            raise failed("tooling")
        return name in services[zone]

    def queries(self, zone, name, permanent):
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
        if self.preserve_runtime:
            self.require_reload_safe()
        code, _output = self.command("--reload")
        self.snapshots = {True: None, False: None}
        if code != 0:
            raise failed("reload")

    def require_reload_safe(self):
        snapshots = []
        for permanent in (False, True):
            arguments = (["--permanent"] if permanent else []) + ["--list-all-zones"]
            code, output = self.command(*arguments)
            if code != 0:
                raise failed("tooling")
            snapshots.append(zone_snapshot(output, self.owned_names))
        if snapshots[0] != snapshots[1]:
            raise failed("runtime-drift")
        for arguments in (["--direct", "--get-all-rules"], ["--direct", "--get-all-passthroughs"], ["--direct", "--get-all-chains"], ["--list-all-policies"]):
            values = []
            for permanent in (False, True):
                code, output = self.command(*((["--permanent"] if permanent else []) + arguments))
                if code != 0:
                    raise failed("tooling")
                values.append(zone_snapshot(output, set()))
            if values[0] != values[1]:
                raise failed("runtime-drift")
        self.require_registries_safe()

    def require_registries_safe(self):
        source = REGISTRY_COMPARISON + ("\nimport sys\nfrom firewall.client import FirewallClient\n"
                                        "print('same' if registries_equal(FirewallClient(), sys.argv[1:]) else 'different')\n")
        code, output = self.run(["/usr/bin/python3", "-I", "-c", source] + sorted(self.owned_names))
        if code != 0:
            raise failed("tooling")
        if output != b"same\n":
            raise failed("runtime-drift")

    def alter(self, zone, name, verb):
        code, _output = self.command("--permanent", "--zone=" + zone, "--" + verb + "-service=" + name)
        self.snapshots[True] = None
        if code != 0:
            raise failed("zone")

    def entry(self, service, mark, found, zones, live):
        """One service's inspect evidence: a zone that lists it permanently or at runtime is a membership."""
        name = service["name"]
        state, data = self.ownership(name, mark)
        interfaces, covered = self.service_interfaces(service, found)
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
            if state == "foreign" or state == "absent" and self.known([name]):
                raise Refused("host-firewall-foreign", detail=name)
            interfaces, complete = self.service_interfaces(service, found)
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


ZONE_HEADER = re.compile(r"([A-Za-z0-9_-]{1,17})(?: \((?:default|active|disabled)(?:, (?:default|active|disabled))*\))?")


def zone_services(output):
    """{zone: services it lists} from one --list-all-zones; a shape outside the closed one is a tooling failure."""
    try:
        text = output.decode("ascii")
    except UnicodeError:
        raise failed("tooling") from None
    zones = {}
    current = None
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        if not line.startswith((" ", "\t")):
            header = ZONE_HEADER.fullmatch(stripped)
            if not header or header[1] in zones or len(zones) >= MAX_ZONES:
                raise failed("tooling")
            current = header[1]
            zones[current] = None
        elif current is None:
            raise failed("tooling")
        elif line.startswith("  services:") and not line.startswith("   "):
            if zones[current] is not None:
                raise failed("tooling")
            zones[current] = frozenset(stripped.split()[1:])
    if any(services is None for services in zones.values()):
        raise failed("tooling")
    return zones


def zone_snapshot(output, owned):
    """Reload preserves interface bindings; all other foreign zone intent must match."""
    try:
        text = output.decode("ascii")
    except UnicodeError:
        raise failed("tooling") from None
    result = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("interfaces:"):
            continue
        if stripped.startswith("services:"):
            stripped = "services: " + " ".join(sorted(set(stripped.split()[1:]) - owned))
        elif not line.startswith(" "):
            header = re.fullmatch(r"([A-Za-z0-9_-]+) \((?:default|active|disabled)(?:, (?:default|active|disabled))*\)", stripped)
            if header:
                stripped = header[1]
        result.append(stripped)
    return result


NFT = "/usr/sbin/nft"
SYSTEMCTL = "/usr/bin/systemctl"
SSHD = "/usr/sbin/sshd"
SSH_TABLE = "bw_ssh_ingress"
SSH_UNIT = "bw-ssh-ingress.service"
SSH_CONFIG = "/etc/" + SSH_TABLE + ".nft"
SSH_UNIT_PATH = "/etc/systemd/system/" + SSH_UNIT
SSH_LOCK = "/run/" + SSH_TABLE + ".lock"
SSH_CANDIDATES = "/run"
SSH_STATES = ("absent", "drifted", "ready", "pending")
SYSTEMD_RUN = "/usr/bin/systemd-run"
WITHDRAW_UNIT = "bw-ssh-ingress-withdraw"
WITHDRAW_TIMER = WITHDRAW_UNIT + ".timer"
WITHDRAW_SERVICE = WITHDRAW_UNIT + ".service"
WITHDRAW_SECONDS = 90
WITHDRAW_MARGIN = 15


def valid_ssh_sources(sources):
    return (isinstance(sources, list) and 0 < len(sources) <= MAX_ADDRESSES
            and all(valid_network(source) and "%" not in source and str(ipaddress.ip_network(source)) == source
                    and getattr(ipaddress.ip_network(source).network_address, "ipv4_mapped", None) is None
                    for source in sources)
            and sources == sorted(set(sources)))


def ssh_rules(sources, ports, mark):
    def rule(left, right, verdict, operator="==", comment=None):
        structure = {"family": "inet", "table": SSH_TABLE, "chain": "input",
                     "expr": [{"match": {"op": operator, "left": left, "right": right}}, {verdict: None}]}
        if comment:
            structure["comment"] = comment
        return {"rule": structure}

    lines = ['iifname "lo" return comment "' + mark + '"', "ct state established return"]
    structures = [
        {"table": {"family": "inet", "name": SSH_TABLE}},
        {"chain": {"family": "inet", "table": SSH_TABLE, "name": "input", "type": "filter",
                   "hook": "input", "prio": 0, "policy": "accept"}},
        rule({"meta": {"key": "iifname"}}, "lo", "return", comment=mark),
        rule({"ct": {"key": "state"}}, 2, "return", operator="in"),
    ]
    for source in sources:
        network = ipaddress.ip_network(source)
        family = "ip" if network.version == 4 else "ip6"
        address = str(network.network_address)
        # A zero-length prefix matches its entire family. nft lowers it differently
        # across releases, so express it as a family test directly.
        if network.prefixlen == 0:
            protocol = "ipv4" if network.version == 4 else "ipv6"
            lines.append("meta nfproto " + protocol + " return")
            structures.append(rule({"meta": {"key": "nfproto"}}, 2 if network.version == 4 else 10, "return"))
        else:
            host = network.prefixlen == network.max_prefixlen
            lines.append(f"{family} saddr {address if host else source} return")
            right = address if host else {"prefix": {"addr": address, "len": network.prefixlen}}
            structures.append(rule({"payload": {"protocol": family, "field": "saddr"}}, right, "return"))
    for port in ports:
        lines.append(f"tcp dport {port} drop")
        structures.append(rule({"payload": {"protocol": "tcp", "field": "dport"}}, port, "drop"))
    identity = "inet " + SSH_TABLE
    text = f"# {mark}\nadd table {identity}\ndelete table {identity}\ntable {identity} {{\n" + (
        "  chain input {\n    type filter hook input priority filter; policy accept;\n    "
        + "\n    ".join(lines) + "\n  }\n}\n")
    return text.encode("ascii"), structures


def ssh_ports(guest):
    code, data = guest.run([SSHD, "-T"])
    ports = {int(match) for match in re.findall(rb"^port ([0-9]{1,5})$", data, flags=re.MULTILINE)}
    if code or not ports or len(ports) > 16 or min(ports) < 1 or max(ports) > 65535:
        raise failed("ssh-admission")
    # The supported sshd listener owns its sockets. A socket unit can listen
    # on ports unrelated to sshd_config.
    for unit in ("sshd.socket", "ssh.socket"):
        active, _output = guest.run([SYSTEMCTL, "is-active", unit])
        enabled, output = guest.run([SYSTEMCTL, "is-enabled", unit])
        if active == 0 or enabled == 0 and output.strip() in (b"enabled", b"enabled-runtime"):
            raise failed("ssh-admission")
    ss = "/usr/sbin/ss" if os.path.exists("/usr/sbin/ss") else "/usr/bin/ss"
    code, data = guest.run([ss, "-H", "-ltnp"])
    try:
        live = {int(line.split()[3].rsplit(b":", 1)[1])
                for line in data.splitlines() if b'("sshd",pid=' in line}
    except (IndexError, ValueError):
        raise failed("ssh-admission") from None
    if code or live - ports:
        raise failed("ssh-admission")
    return sorted(ports)


def ssh_peer():
    """Read the peer recorded by sshd in the invoking process ancestry."""
    pid = os.getpid()
    for _depth in range(32):
        try:
            with open(f"/proc/{pid}/environ", "rb") as file:
                values = file.read(MAX_COMMAND_OUTPUT).split(b"\0")
            peer = next((entry.split(b"=", 1)[1] for entry in values if entry.startswith(b"SSH_CONNECTION=")), None)
            if peer is not None:
                source, _source_port, _destination, destination_port = peer.decode("ascii").split()
                return ipaddress.ip_address(source), int(destination_port)
            with open(f"/proc/{pid}/stat", "rb") as file:
                pid = int(file.read(8192).rsplit(b")", 1)[1].split()[1])
        except (FileNotFoundError, ProcessLookupError):
            return None
        except (OSError, ValueError, IndexError):
            raise failed("ssh-admission") from None
        if pid <= 1:
            return None
    raise failed("ssh-admission")


def require_ssh_peer(sources, ports, remote):
    peer = ssh_peer()
    if peer is None:
        if remote:
            raise failed("ssh-admission")
        return
    address, port = peer
    address = getattr(address, "ipv4_mapped", None) or address
    networks = [ipaddress.ip_network(source) for source in sources]
    if port not in ports or not any(address in network for network in networks):
        raise failed("ssh-admission")


def ssh_unit(mark):
    return (f"# {mark}\n[Unit]\nDescription=Restrict inbound SSH sources\n"
            "Before=ssh.service sshd.service\n"
            "[Service]\nType=oneshot\nRemainAfterExit=yes\n"
            f"ExecStart={NFT} -f {SSH_CONFIG}\nExecReload={NFT} -f {SSH_CONFIG}\n"
            "[Install]\nWantedBy=multi-user.target\nRequiredBy=sshd.service\n").encode("ascii")


def ssh_file(path, mark):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        return None
    except OSError:
        raise failed("ssh-admission") from None
    try:
        status = os.fstat(descriptor)
        if not stat.S_ISREG(status.st_mode) or status.st_uid != 0 or status.st_mode & 0o022 or status.st_nlink != 1:
            raise failed("ssh-admission")
        data = read_bounded(descriptor, MAX_SERVICE_FILE)
        if len(data) > MAX_SERVICE_FILE or data.split(b"\n", 1)[0] != b"# " + mark.encode("ascii"):
            raise failed("ssh-admission")
        return data
    finally:
        os.close(descriptor)


def publish_ssh_file(path, data):
    directory = os.path.dirname(path)
    temporary = None
    try:
        if os.path.realpath(directory) != directory:
            raise failed("write")
        if data is None:
            os.unlink(path)
        else:
            with tempfile.NamedTemporaryFile(dir=directory, delete=False) as file:
                temporary = file.name
                os.fchmod(file.fileno(), 0o644)
                file.write(data)
                file.flush()
                os.fsync(file.fileno())
            os.replace(temporary, path)
            temporary = None
        descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    except OSError:
        raise failed("write") from None
    finally:
        if temporary is not None:
            os.unlink(temporary)


def nft_listing(guest, *args):
    code, data = guest.run([NFT, "-j", "-s", "-nn", "list", *args])
    if code:
        return None
    try:
        entries = json.loads(data)["nftables"]
        return [{kind: {key: value for key, value in row.items() if key != "handle"}}
                for entry in entries for kind, row in entry.items() if kind != "metainfo"]
    except (ValueError, TypeError, KeyError, AttributeError):
        raise failed("ssh-admission") from None


def ssh_runtime(guest, mark):
    entries = nft_listing(guest, "table", "inet", SSH_TABLE)
    if entries is None:
        tables = nft_listing(guest, "tables")
        if tables is None or {"table": {"family": "inet", "name": SSH_TABLE}} in tables:
            raise failed("ssh-admission")
        return None
    if not any(entry.get("rule", {}).get("comment") == mark for entry in entries):
        raise failed("ssh-admission")
    return entries


def ssh_command(guest, *arguments):
    if guest.run(list(arguments))[0]:
        raise failed("ssh-admission")


def ssh_restore_path():
    return os.path.join(SSH_CANDIDATES, SSH_TABLE + ".restore")


def ssh_restore_argv():
    """Load the kernel chain this apply found, as staged before any change: the chain it listed, or none."""
    return [NFT, "-f", ssh_restore_path()]


def ssh_stage_restore(guest, runtime):
    """Stage, in a private run file, the chain the kernel holds before anything changes it; an absent chain stages its absence."""
    identity = "inet " + SSH_TABLE
    data = ("add table " + identity + "\ndelete table " + identity + "\n").encode("ascii")
    if runtime is not None:
        code, listing = guest.run([NFT, "list", "table", "inet", SSH_TABLE])
        if code or not listing:
            raise failed("ssh-admission")
        data += listing
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=SSH_CANDIDATES, delete=False) as file:
            temporary = file.name
            os.fchmod(file.fileno(), 0o600)
            file.write(data)
            file.flush()
            os.fsync(file.fileno())
        os.replace(temporary, ssh_restore_path())
        temporary = None
    except OSError:
        raise failed("ssh-admission") from None
    finally:
        if temporary is not None:
            os.unlink(temporary)
    # A restore that would not reload never guards a candidate.
    ssh_command(guest, NFT, "-c", "-f", ssh_restore_path())


def ssh_attempt(step):
    try:
        step()
        return True
    except Refused:
        return False


def ssh_withdraw(guest, mark, prior):
    """Put the filter, its unit, its enablement and its chain back as this apply found them.

    Every step runs even when an earlier one fails, so the most of the earlier
    access returns; the caller refuses ssh-admission either way. The chain is
    the staged one, never a file: when an earlier file cannot be put back the
    unit is left disabled, so no boot loads the candidate either."""
    config, unit, enabled = prior
    ssh_attempt(lambda: ssh_command(guest, SYSTEMCTL, "disable", SSH_UNIT)
                if guest.run([SYSTEMCTL, "is-enabled", SSH_UNIT])[1] == b"enabled\n" and not enabled else None)
    if unit is None:
        guest.run([SYSTEMCTL, "stop", SSH_UNIT])
    restored = all([
        ssh_attempt(lambda: publish_ssh_file(SSH_CONFIG, config) if ssh_file(SSH_CONFIG, mark) != config else None),
        ssh_attempt(lambda: publish_ssh_file(SSH_UNIT_PATH, unit) if ssh_file(SSH_UNIT_PATH, mark) != unit else None),
    ])
    ssh_attempt(lambda: ssh_command(guest, SYSTEMCTL, "daemon-reload"))
    if enabled and restored:
        ssh_attempt(lambda: ssh_command(guest, SYSTEMCTL, "enable", SSH_UNIT))
    elif enabled:
        ssh_attempt(lambda: ssh_command(guest, SYSTEMCTL, "disable", SSH_UNIT))
    ssh_attempt(lambda: ssh_command(guest, *ssh_restore_argv()))


def ssh_publish(guest, mark, prior, wanted_config, wanted_unit, wanted_runtime):
    """Publish the filter and its unit and prove the chain they load; the caller withdraws them on any refusal."""
    config, unit, _enabled = prior
    if config != wanted_config:
        publish_ssh_file(SSH_CONFIG, wanted_config)
    if unit != wanted_unit:
        publish_ssh_file(SSH_UNIT_PATH, wanted_unit)
    ssh_command(guest, SYSTEMCTL, "daemon-reload")
    code, properties = guest.run([SYSTEMCTL, "show", "--property=FragmentPath,DropInPaths", SSH_UNIT])
    expected = {("FragmentPath=" + SSH_UNIT_PATH).encode("ascii"), b"DropInPaths="}
    if code or set(properties.splitlines()) != expected:
        raise failed("ssh-admission")
    ssh_command(guest, SYSTEMCTL, "enable", SSH_UNIT)
    ssh_command(guest, SYSTEMCTL, "reload-or-restart", SSH_UNIT)
    if ssh_runtime(guest, mark) != wanted_runtime:
        raise failed("ssh-admission")


def ssh_candidate(guest, wanted_config, *argv):
    """Run the nft argv on the candidate configuration staged in a private run file."""
    with tempfile.NamedTemporaryFile(dir=SSH_CANDIDATES) as candidate:
        candidate.write(wanted_config)
        candidate.flush()
        ssh_command(guest, *argv, candidate.name)


def ssh_arm(guest, mark, wanted_config, wanted_runtime):
    """Load only the candidate chain, after a timer that puts the earlier chain back unless a fresh session confirms it."""
    if guest.run([SYSTEMCTL, "is-active", WITHDRAW_TIMER])[0] == 0:
        # An earlier arm no session confirmed: its staged chain is the one this apply found.
        ssh_command(guest, *ssh_restore_argv())
    guest.run([SYSTEMCTL, "stop", WITHDRAW_TIMER, WITHDRAW_SERVICE])
    guest.run([SYSTEMCTL, "reset-failed", WITHDRAW_TIMER, WITHDRAW_SERVICE])
    ssh_stage_restore(guest, ssh_runtime(guest, mark))
    try:
        ssh_command(guest, SYSTEMD_RUN, "--unit=" + WITHDRAW_UNIT, "--collect", "--on-active=" + str(WITHDRAW_SECONDS),
                    "--timer-property=AccuracySec=1s", *ssh_restore_argv())
        ssh_candidate(guest, wanted_config, NFT, "-f")
        if ssh_runtime(guest, mark) != wanted_runtime:
            raise failed("ssh-admission")
    except Refused:
        # The timer stays armed unless this restore proved the earlier chain back.
        if guest.run(ssh_restore_argv())[0] == 0:
            guest.run([SYSTEMCTL, "stop", WITHDRAW_TIMER])
        raise failed("ssh-admission") from None


def ssh_confirm(guest, payload, config, unit, wanted_unit):
    """A fresh session's confirmation: stop the withdrawal, prove it never started and that the chain the apply loaded is the one this
    session came through, then publish its file and unit; any refusal after the stop withdraws everything first."""
    mark = payload["mark"]
    prior = (config, unit, guest.run([SYSTEMCTL, "is-enabled", SSH_UNIT])[1] == b"enabled\n")
    guest.run([SYSTEMCTL, "stop", WITHDRAW_TIMER, WITHDRAW_SERVICE])
    try:
        if guest.run([SYSTEMCTL, "is-active", WITHDRAW_TIMER])[0] == 0:
            raise failed("ssh-admission")
        code, started = guest.run([SYSTEMCTL, "show", "--property=ExecMainStartTimestampMonotonic", WITHDRAW_SERVICE])
        if code or started.strip() != b"ExecMainStartTimestampMonotonic=0":
            raise failed("ssh-admission")
        wanted_config, wanted_runtime = ssh_rules(payload["sshSources"], ssh_ports(guest), mark)
        if ssh_runtime(guest, mark) != wanted_runtime:
            raise failed("ssh-admission")
        ssh_publish(guest, mark, prior, wanted_config, wanted_unit, wanted_runtime)
    except Refused:
        ssh_withdraw(guest, mark, prior)
        raise failed("ssh-admission") from None
    return "ready", True


def ssh_policy(guest, payload):
    sources = payload["sshSources"]
    mark = payload["mark"]
    operation = payload["operation"]
    config, unit = ssh_file(SSH_CONFIG, mark), ssh_file(SSH_UNIT_PATH, mark)
    wanted_unit = ssh_unit(mark)
    if unit is not None and unit != wanted_unit:
        raise failed("ssh-admission")
    if operation == "confirm":
        return ssh_confirm(guest, payload, config, unit, wanted_unit)
    runtime = ssh_runtime(guest, mark)
    if operation == "remove":
        if unit is not None:
            ssh_command(guest, SYSTEMCTL, "disable", SSH_UNIT)
            ssh_command(guest, SYSTEMCTL, "daemon-reload")
            ssh_command(guest, SYSTEMCTL, "stop", SSH_UNIT)
        if runtime is not None:
            ssh_command(guest, NFT, "delete", "table", "inet", SSH_TABLE)
        if config is not None:
            publish_ssh_file(SSH_CONFIG, None)
        if unit is not None:
            publish_ssh_file(SSH_UNIT_PATH, None)
            ssh_command(guest, SYSTEMCTL, "daemon-reload")
        if ssh_runtime(guest, mark) is not None:
            raise failed("ssh-admission")
        return "absent", any(part is not None for part in (config, unit, runtime))
    if config is None and unit is None and runtime is None and operation == "inspect":
        return "absent", False
    ports = ssh_ports(guest)
    wanted_config, wanted_runtime = ssh_rules(sources, ports, mark)
    enabled = guest.run([SYSTEMCTL, "is-enabled", SSH_UNIT])[1] == b"enabled\n"
    if (config, unit, runtime, enabled) == (wanted_config, wanted_unit, wanted_runtime, True):
        return "ready", False
    if operation == "inspect":
        return "drifted", False
    # Test the full atomic replacement before writing anything.
    ssh_candidate(guest, wanted_config, NFT, "-c", "-f")
    if payload.get("confirm"):
        ssh_arm(guest, mark, wanted_config, wanted_runtime)
        return "pending", True
    prior = (config, unit, enabled)
    ssh_stage_restore(guest, runtime)
    try:
        ssh_publish(guest, mark, prior, wanted_config, wanted_unit, wanted_runtime)
    except Refused:
        ssh_withdraw(guest, mark, prior)
        raise failed("ssh-admission") from None
    return "ready", True


def converge(guest, payload, remote=True):
    if "sshSources" not in payload:
        return HANDLERS[payload["operation"]](guest, payload)
    operation = payload["operation"]
    if operation in ("apply", "confirm"):
        require_ssh_peer(payload["sshSources"], ssh_ports(guest), remote or operation == "confirm")
    if operation == "inspect":
        return converge_ssh(guest, payload)
    try:
        descriptor = os.open(SSH_LOCK, os.O_CREAT | os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    except OSError:
        raise failed("ssh-admission") from None
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != 0 or info.st_mode & 0o077:
            raise OSError("invalid SSH filter lock")
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        os.close(descriptor)
        raise failed("ssh-admission") from None
    try:
        return converge_ssh(guest, payload)
    finally:
        os.close(descriptor)


def converge_ssh(guest, payload):
    result = HANDLERS[payload["operation"]](guest, payload) if payload["services"] else {"services": [], "changed": False}
    state, changed = ssh_policy(guest, payload)
    return dict(result, sshPolicy=state, changed=result["changed"] or changed)


def apply_operation(guest, payload):
    guest.require_running()
    guest.owned_names = {service["name"] for service in payload["services"]}
    planned = guest.targets(payload, guest.addresses())
    changed = False
    for service, (state, data, _zones) in zip(payload["services"], planned):
        if state == "absent" or data != service["content"].encode("ascii"):
            guest.write(service["name"], service["content"])
            changed = True
    if changed or not guest.known([service["name"] for service in payload["services"]]):
        guest.reload()
        changed = True
    missing = [(zone, service["name"]) for service, (_state, _data, zones) in zip(payload["services"], planned)
               for zone in zones if not guest.lists(zone, service["name"], True)]
    for zone, name in missing:
        guest.alter(zone, name, "add")
    added = bool(missing)
    reloaded = False
    if added or guest.missing_at_runtime(payload, planned):
        guest.reload()
        reloaded = True
    return {"changed": changed or added or reloaded, "services": guest.evidence(payload)}


def remove_operation(guest, payload):
    guest.require_running()
    guest.owned_names = {service["name"] for service in payload["services"]}
    states = []
    for service in payload["services"]:
        state, _data = guest.ownership(service["name"], payload["mark"])
        if state == "foreign":
            raise Refused("host-firewall-foreign", detail=service["name"])
        states.append(state)
    zones = guest.zones()
    listed = {service["name"]: [zone for zone in zones if guest.lists(zone, service["name"], True)]
              for service, state in zip(payload["services"], states) if state == "owned"}
    changed = False
    for service, state in zip(payload["services"], states):
        if state != "owned":
            continue
        for zone in listed[service["name"]]:
            guest.alter(zone, service["name"], "remove")
        guest.delete(service["name"])
        changed = True
    if changed or guest.listed_at_runtime(payload):
        guest.reload()
        changed = True
    return {"changed": changed, "services": guest.evidence(payload)}


def inspect_operation(guest, payload):
    guest.require_running()
    return {"changed": False, "services": guest.evidence(payload)}


HANDLERS = {"apply": apply_operation, "inspect": inspect_operation, "remove": remove_operation, "confirm": inspect_operation}


def parse_request(data):
    """The validated request; every value is checked before any guest state is read."""
    if not isinstance(data, bytes) or len(data) > MAX_INPUT:
        raise Refused("transport")
    try:
        payload = json.loads(data.decode("utf-8"))
    except (ValueError, UnicodeError):
        raise Refused("transport") from None
    if not isinstance(payload, dict) or not KEYS <= set(payload) <= KEYS | {"sshSources", "confirm"} or payload["operation"] not in OPERATIONS:
        raise Refused("transport")
    # Only an installed host's apply arms a withdrawal, and only its fresh session confirms it.
    if "confirm" in payload and (payload["confirm"] is not True or payload["operation"] != "apply" or "sshSources" not in payload):
        raise Refused("transport")
    if payload["operation"] == "confirm" and "sshSources" not in payload:
        raise Refused("transport")
    if not valid_mark(payload["mark"]) or not valid_services(payload["services"]):
        raise Refused("transport")
    if "sshSources" in payload and not valid_ssh_sources(payload["sshSources"]):
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
        reply = {"ok": True, "result": converge(guest if guest is not None else Guest(), payload)}
    except Refused as refused:
        reply = refusal(refused)
    except BaseException:  # pylint: disable=broad-exception-caught
        reply = {"ok": False, "reason": "transport"}
    target = stdout if stdout is not None else sys.stdout
    target.write(answer(reply) + "\n")
    target.flush()
    return 0

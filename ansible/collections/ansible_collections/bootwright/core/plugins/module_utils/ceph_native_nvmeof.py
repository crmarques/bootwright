"""Bounded NVMe-oF manager operations with original-attempt resource ownership.

The native command and readback shapes are dashboard/controllers/nvmeof.py and
model/nvmeof.py. Gateway mTLS is the native client's verified secure channel;
this worker never takes a verification bypass or an arbitrary command.
"""

from __future__ import annotations

# Exact types reject booleans where native integer identities are required.
# pylint: disable=unidiomatic-typecheck

import hashlib
import ipaddress
import re
import socket
import uuid

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import Refusal, canonical, named, require

PHASES = frozenset(("nvmeof", "nvmeof_cluster", "nvmeof_observe"))
READ_ONLY_PHASES = PHASES - {"nvmeof"}
DOMAIN = r"[A-Za-z](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
NQN = re.compile(r"(?:nqn\.2014-08\.org\.nvmexpress:uuid:[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}"
                 r"|nqn\.[0-9]{4}-(?:0[1-9]|1[0-2])\." + DOMAIN + r"(?:\." + DOMAIN + r")*:[!-~]+)\Z")
MEMBERS = frozenset(("name", "service", "group", "pool", "mtls", "nqn", "hosts", "gateways", "listeners", "namespaces"))
TLS_FIELDS = ("root_ca_cert", "server_cert", "server_key", "client_cert", "client_key")
COMMANDS = frozenset((("gateway", "info"), ("gateway", "listener_info"), ("subsystem", "list"), ("subsystem", "add"),
                      ("listener", "list"), ("listener", "add"), ("namespace", "list"), ("namespace", "add"), ("host", "list"), ("host", "add")))


def phase_budget(request, phase):
    """60 seconds per CLI command, 30 per held image probe, plus local work.

    Scope: 5+G+3N+D commands. First apply: 10+N+L+H more;
    replay is shorter. Each add holds its cache image (one 30-second probe),
    and the first transport hold probes the manager image once. Resource
    observation adds four snapshot reads, G ANA reads and L two-second TCP
    probes. Manager retries belong to the separate 640-second cluster phase.
    """
    if phase == "nvmeof_unit_observe":
        require(request.get("action") == "service" and request["service"]["type"] == "nvmeof")
        # Preserve the shared observer's 640-second bound and add every gateway read.
        return 640 + 60 * len(request["service"]["documents"][0]["placement"]["hosts"]) + 60
    frozen = request["nvmeof"]
    gateways, namespaces = len(frozen["gateways"]), len(frozen["namespaces"])
    listeners, hosts = len(frozen["listeners"]), len(frozen["hosts"])
    data_pools = sum(bool(n["image"]["dataPool"]) for n in frozen["namespaces"])
    scope = 5 + gateways + 3 * namespaces + data_pools
    if phase == "nvmeof":
        return 60 * (scope + 10 + namespaces + listeners + hosts) + 30 * (2 + namespaces + listeners + hosts) + 60
    require(phase == "nvmeof_observe")
    return 60 * (scope + 4 + gateways) + 2 * listeners + 60


def execution_budget(request, operation):
    """The full controller bound includes a complete observation and a retry window."""
    observation = 660 + phase_budget(request, "nvmeof_observe") + 20
    if operation == "apply":
        return 660 + phase_budget(request, "nvmeof") + 20 + observation + 900 + 60
    return observation + 60


def validate(request, hosts):
    frozen = request.get("nvmeof")
    require(isinstance(frozen, dict) and set(frozen) == MEMBERS)
    require(all(named(frozen[key]) for key in ("name", "service", "group", "pool")))
    require(frozen["mtls"] in ("enable_auth", "enable_mtls"))
    require(isinstance(frozen["nqn"], str) and 11 <= len(frozen["nqn"]) <= 223 and NQN.fullmatch(frozen["nqn"]) is not None)
    allowed = frozen["hosts"]
    require(isinstance(allowed, list) and 1 <= len(allowed) <= 128
            and all(isinstance(nqn, str) and 11 <= len(nqn) <= 223 and NQN.fullmatch(nqn) is not None for nqn in allowed))
    require(allowed == sorted(set(allowed)))
    names = {host["name"] for host in hosts}
    for key in ("gateways", "listeners"):
        entries = frozen[key]
        require(isinstance(entries, list) and 1 <= len(entries) <= 16)
        for entry in entries:
            require(isinstance(entry, dict) and set(entry) == {"host", "address", "port"} and entry["host"] in names)
            require(type(entry["port"]) is int and 1 <= entry["port"] <= 65535)
            try:
                require(isinstance(entry["address"], str) and str(ipaddress.ip_address(entry["address"])) == entry["address"])
            except ValueError:
                raise Refusal("request") from None
        require(len({(e["host"], e["address"], e["port"]) for e in entries}) == len(entries))
    gateways = frozen["gateways"]
    require(len({g["host"] for g in gateways}) == len(gateways))
    require({x["host"] for x in frozen["listeners"]} == {g["host"] for g in gateways})
    delegated = [d for d in request.get("delegated", []) if d["name"] == "nvmeof." + frozen["service"]]
    require(len(delegated) == 1 and set(delegated[0]["hosts"]) == {g["host"] for g in gateways})
    namespaces = frozen["namespaces"]
    require(isinstance(namespaces, list) and 1 <= len(namespaces) <= 128)
    for namespace in namespaces:
        require(isinstance(namespace, dict) and set(namespace) == {"id", "image"})
        require(type(namespace["id"]) is int and 1 <= namespace["id"] <= 4294967294)
        ceph_native_resources.validate_rbd(namespace["image"])
    require(len({n["id"] for n in namespaces}) == len(namespaces)
            and len({(n["image"]["pool"], n["image"]["name"]) for n in namespaces}) == len(namespaces))

    require(execution_budget(request, "apply") <= 21600, "nvmeof-budget")


class NVMeOFWorker(ceph_native_resources.ResourceWorker):
    def nvme_identity(self):
        frozen = self.request["nvmeof"]
        seed = canonical({"owner": self.request["owner"], "fsid": self.request["fsid"], "name": frozen["name"], "nqn": frozen["nqn"]})
        return {"serial": "bw" + hashlib.sha256(seed).hexdigest()[:18], "namespaces": {
            str(n["id"]): str(uuid.uuid5(uuid.UUID(self.request["fsid"]), seed.decode() + ":" + str(n["id"]))) for n in frozen["namespaces"]}}

    def recovery_scope(self):
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "nvmeof"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def nvme_records(self):
        name = self.request["nvmeof"]["name"]
        return "nvmeof-attempt." + name + ".json", "nvmeof-complete." + name + ".json"

    def nvme(self, family, verb, *arguments, gateway=None):
        require((family, verb) in COMMANDS, "request")
        args = ("nvmeof", family, verb, "--gw_group", self.request["nvmeof"]["group"]) + arguments
        if gateway is not None:
            args += ("--server_address", gateway["address"])
        try:
            value = (self.tunnel_effect if verb == "add" else self.ceph)(*args)
        except Refusal:
            raise Refusal("nvmeof-native") from None
        require(isinstance(value, dict) and type(value.get("status")) is int and value["status"] == 0, "nvmeof-native")
        return value

    def nvme_rows(self, family, member):
        result = self.nvme(family, "list", "--nqn", self.request["nvmeof"]["nqn"])
        rows = result.get(member)
        require(isinstance(rows, list) and len(rows) <= 256 and all(isinstance(row, dict) for row in rows), "native-output")
        return result, rows

    def prove_nvme_service(self):
        frozen = self.request["nvmeof"]
        name = "nvmeof." + frozen["service"]
        attempt = self.markers.read("service-" + name + "-attempt.json")
        complete = self.markers.read("service-" + name + "-complete.json")
        require(isinstance(attempt, dict) and complete == attempt and isinstance(attempt.get("scope"), dict), "ownership")
        require(attempt["scope"].get("owner") == self.request["owner"] and attempt["scope"].get("fsid") == self.request["fsid"], "ownership")
        stored = self.service_specs(limit=1024).get(name)
        require(isinstance(stored, dict) and isinstance(stored.get("spec"), dict), "nvmeof-identity")
        spec = stored["spec"]
        require(spec.get("pool") == frozen["pool"] and spec.get("group") == frozen["group"] and spec.get(frozen["mtls"]) is True, "nvmeof-identity")
        require(all(spec.get(key, True) is True for key in ("verify_nqns", "verify_keys", "verify_listener_ip")), "nvmeof-identity")
        if frozen["mtls"] == "enable_auth":
            require(all(isinstance(spec.get(key), str) and spec[key] for key in TLS_FIELDS), "nvmeof-identity")
        retained = attempt.get("service")
        require(isinstance(retained, dict) and isinstance(retained.get("spec"), dict), "ownership")
        scrubbed = dict(stored, spec={k: v for k, v in spec.items() if k not in TLS_FIELDS})
        expected = dict(retained, spec={k: v for k, v in retained["spec"].items() if k not in TLS_FIELDS})
        require(scrubbed == expected, "nvmeof-identity")
        placement = stored.get("placement", {})
        require(set(placement.get("hosts", [])) == {g["host"] for g in frozen["gateways"]}, "nvmeof-identity")
        self.owned_pool(frozen["pool"])

    def prove_nvme_images(self):
        identities = {}
        for namespace in self.request["nvmeof"]["namespaces"]:
            image = namespace["image"]
            mark, attempt_name, identity_name = self.rbd_marks(image)
            self.owned_pool(image["pool"])
            if image["dataPool"]:
                self.owned_pool(image["dataPool"])
            require(self.markers.read(mark) == image and image["name"] in self.rbd_names(image["pool"]), "nvmeof-identity")
            require(self.markers.read(attempt_name) == {"version": ceph_native_resources.RBD_ATTEMPT, "owner": self.request["owner"],
                    "fsid": self.request["fsid"], "rbd": image}, "nvmeof-identity")
            info = self.rbd_info(image)
            require(self.markers.read(identity_name) == {"id": info["id"]} and self.image_matches(info, image), "nvmeof-identity")
            identities[str(namespace["id"])] = info["id"]
        return identities

    def prove_nvme_gateways(self):
        frozen = self.request["nvmeof"]
        config = self.ceph("dashboard", "nvmeof-gateway-list")
        require(isinstance(config, dict) and isinstance(config.get("gateways"), dict), "native-output")
        entries = config["gateways"].get("nvmeof." + frozen["service"])
        require(isinstance(entries, list) and len(entries) == len(frozen["gateways"]), "nvmeof-identity")
        identities = []
        all_entries = [entry for group in config["gateways"].values() if isinstance(group, list) for entry in group]
        require([e for e in all_entries if isinstance(e, dict) and e.get("group") == frozen["group"]] == entries, "nvmeof-identity")
        for gateway in frozen["gateways"]:
            address = gateway["address"]
            url = ("[" + address + "]" if ":" in address else address) + ":" + str(gateway["port"])
            # The native client's server-address selector uses substring matching across groups.
            # Refuse ambiguous routing before asking it to connect or making any resource effect.
            routes = [e for e in all_entries if isinstance(e, dict) and isinstance(e.get("service_url"), str) and address in e["service_url"]]
            require(len(routes) == 1 and routes[0].get("service_url") == url and routes[0] in entries, "nvmeof-identity")
            matches = [e for e in entries if isinstance(e, dict) and e.get("service_url") == url and e.get("group") == frozen["group"]]
            require(len(matches) == 1 and isinstance(matches[0].get("daemon_name"), str), "nvmeof-identity")
            info = self.nvme("gateway", "info", gateway=gateway)
            require(info.get("hostname") == gateway["host"] and info.get("group") == frozen["group"]
                    and info.get("addr") == address and info.get("port") == gateway["port"]
                    and info.get("name") == "client." + matches[0]["daemon_name"]
                    and info.get("gateway_initialization_over") is True, "nvmeof-identity")
            require(type(info.get("load_balancing_group")) is int and info["load_balancing_group"] > 0, "nvmeof-identity")
            identities.append({key: info[key] for key in ("hostname", "name", "addr", "port", "group", "load_balancing_group")})
        require(len({g["load_balancing_group"] for g in identities}) == len(identities), "nvmeof-identity")
        return identities

    def qualify_nvme_commands(self):
        qualify_commands(self)

    def nvme_scope(self):
        require(self.markers.owned() and self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.prove_nvme_service()
        self.qualify_nvme_commands()
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "request": self.request["nvmeof"],
                "identity": self.nvme_identity(), "images": self.prove_nvme_images(), "gateways": self.prove_nvme_gateways()}

    def nvme_snapshot(self, scope):
        frozen, identity = scope["request"], scope["identity"]
        unused, subsystems = self.nvme_rows("subsystem", "subsystems")
        require(len(subsystems) <= 1, "nvmeof-identity")
        if not subsystems:
            return None
        subsystem = subsystems[0]
        require(subsystem.get("nqn") == frozen["nqn"] and subsystem.get("serial_number") == identity["serial"]
                and subsystem.get("enable_ha") is True and subsystem.get("allow_any_host") is False
                and subsystem.get("has_dhchap_key") is False
                and subsystem.get("max_namespaces") == len(frozen["namespaces"]), "nvmeof-identity")
        hosts_info, hosts = self.nvme_rows("host", "hosts")
        require(hosts_info.get("allow_any_host") is False and hosts_info.get("subsystem_nqn") == frozen["nqn"], "nvmeof-identity")
        allowed = [h.get("nqn") for h in hosts]
        require(all(isinstance(n, str) and n in frozen["hosts"] for n in allowed) and len(set(allowed)) == len(allowed), "nvmeof-identity")
        require(all(not h.get("use_psk") and not h.get("use_dhchap") for h in hosts), "nvmeof-identity")
        unused, listeners = self.nvme_rows("listener", "listeners")
        actual_listeners = []
        for listener in listeners:
            value = {"host": listener.get("host_name"), "address": listener.get("traddr"), "port": listener.get("trsvcid")}
            require(value in frozen["listeners"] and listener.get("trtype") == "TCP" and not listener.get("secure"), "nvmeof-identity")
            require(listener.get("adrfam") == (1 if ":" in value["address"] else 0), "nvmeof-identity")
            actual_listeners.append(value)
        require(len({canonical(v) for v in actual_listeners}) == len(actual_listeners), "nvmeof-identity")
        unused, namespaces = self.nvme_rows("namespace", "namespaces")
        actual_ids = []
        for namespace in namespaces:
            wanted = [n for n in frozen["namespaces"] if n["id"] == namespace.get("nsid")]
            require(len(wanted) == 1, "nvmeof-identity")
            target = wanted[0]
            image = target["image"]
            require(namespace.get("uuid") == identity["namespaces"][str(target["id"])]
                    and namespace.get("rbd_pool_name") == image["pool"] and namespace.get("rbd_image_name") == image["name"]
                    and not namespace.get("rados_namespace_name") and native_size(namespace.get("rbd_image_size")) == image["sizeBytes"]
                    and namespace.get("ns_subsystem_nqn") == frozen["nqn"] and namespace.get("block_size") == 512
                    and namespace.get("auto_visible") is True and not namespace.get("trash_image")
                    and namespace.get("load_balancing_group") == self.nvme_lb_group(scope, target), "nvmeof-identity")
            actual_ids.append(target["id"])
        require(len(set(actual_ids)) == len(actual_ids), "nvmeof-identity")
        return {"hosts": allowed, "listeners": actual_listeners, "namespaces": actual_ids}

    @staticmethod
    def nvme_lb_group(scope, namespace):
        index = sorted(n["id"] for n in scope["request"]["namespaces"]).index(namespace["id"])
        return scope["gateways"][index % len(scope["gateways"])]["load_balancing_group"]

    @staticmethod
    def nvme_complete(scope, snapshot):
        frozen = scope["request"]
        return (snapshot is not None and set(snapshot["hosts"]) == set(frozen["hosts"])
                and len(snapshot["listeners"]) == len(frozen["listeners"]) and len(snapshot["namespaces"]) == len(frozen["namespaces"]))

    def nvmeof(self):
        scope = self.nvme_scope()
        attempt_name, complete_name = self.nvme_records()
        attempt, complete = self.markers.read(attempt_name), self.markers.read(complete_name)
        require(attempt is None or attempt == scope, "ownership")
        require(complete is None or complete == scope and attempt == scope, "ownership")
        snapshot = self.nvme_snapshot(scope)
        require(snapshot is None or attempt == scope, "nvmeof-foreign")
        if complete is not None:
            require(self.nvme_complete(scope, snapshot), "nvmeof-identity")
            return {"changed": False}
        if attempt is None:
            self.markers.write(attempt_name, scope)
        frozen = scope["request"]
        nqn = ("--nqn", frozen["nqn"])
        if snapshot is None:
            self.nvme("subsystem", "add", *nqn, "--no_group_append=true", "--serial_number", scope["identity"]["serial"],
                      "--max_namespaces", str(len(frozen["namespaces"])))
            snapshot = self.nvme_snapshot(scope)
            require(snapshot is not None, "nvmeof-identity")
        for namespace in frozen["namespaces"]:
            if namespace["id"] not in snapshot["namespaces"]:
                image = namespace["image"]
                self.nvme("namespace", "add", *nqn, "--rbd_pool", image["pool"], "--rbd_image_name", image["name"],
                          "--nsid", str(namespace["id"]), "--uuid", scope["identity"]["namespaces"][str(namespace["id"])],
                          "--create_image=false", "--trash_image=false", "--block_size", "512", "--force=false",
                          "--load_balancing_group", str(self.nvme_lb_group(scope, namespace)))
        for listener in frozen["listeners"]:
            if listener not in snapshot["listeners"]:
                gateway = next(g for g in frozen["gateways"] if g["host"] == listener["host"])
                self.nvme("listener", "add", *nqn, "--host_name", listener["host"], "--traddr", listener["address"],
                          "--trsvcid", str(listener["port"]), "--adrfam", "1" if ":" in listener["address"] else "0",
                          "--verify_host_name=true", "--force=false", gateway=gateway)
        for host in frozen["hosts"]:
            if host not in snapshot["hosts"]:
                self.nvme("host", "add", *nqn, "--host_nqn", host)
        require(self.nvme_complete(scope, self.nvme_snapshot(scope)), "nvmeof-identity")
        self.markers.write(complete_name, scope)
        return {"changed": True}

    def prove_nvme_ana(self, scope):
        frozen = scope["request"]
        groups = {self.nvme_lb_group(scope, n) for n in frozen["namespaces"]}
        optimized = set()
        ready = True
        for gateway in frozen["gateways"]:
            result = self.nvme("gateway", "listener_info", "--nqn", frozen["nqn"], gateway=gateway)
            rows = result.get("gw_listeners")
            require(isinstance(rows, list) and len(rows) <= 16, "native-output")
            wanted = [l for l in frozen["listeners"] if l["host"] == gateway["host"]]
            seen = []
            for row in rows:
                require(isinstance(row, dict) and isinstance(row.get("listener"), dict), "native-output")
                listener = row["listener"]
                identity = {"host": listener.get("host_name"), "address": listener.get("traddr"), "port": listener.get("trsvcid")}
                require(identity in wanted and identity not in seen, "nvmeof-identity")
                seen.append(identity)
                states = row.get("lb_states")
                require(isinstance(states, list) and all(isinstance(v, dict) for v in states), "native-output")
                require(len({v.get("grp_id") for v in states}) == len(states), "native-output")
                ready = ready and listener.get("active") is True
                for group in groups:
                    values = [v.get("state") for v in states if v.get("grp_id") == group]
                    ready = ready and len(values) == 1 and values[0] in ("OPTIMIZED", "NON_OPTIMIZED")
                    if values == ["OPTIMIZED"]:
                        optimized.add(group)
            ready = ready and len(seen) == len(wanted)
        return ready and optimized == groups and all(listener_reachable(l) for l in frozen["listeners"])

    def nvmeof_cluster(self):
        self.cluster_only = True
        result = self.observe()
        return dict(result, blocking=list(self.blocking))

    def nvmeof_observe(self):
        evidence = self.empty_evidence()
        evidence["ready"] = self.observe_resource(evidence, True)
        evidence["partial"] = evidence["exists"] and not evidence["ready"]
        evidence["removed"] = evidence["owned"] and not evidence["exists"]
        return {key: evidence[key] for key in ("owned", "exists", "ready", "partial", "removed", "resource")}

    def observe_resource(self, evidence, ready):
        if getattr(self, "cluster_only", False):
            return ready
        scope = self.nvme_scope()
        attempt_name, complete_name = self.nvme_records()
        attempt, complete = self.markers.read(attempt_name), self.markers.read(complete_name)
        require(attempt is None or attempt == scope, "ownership")
        require(complete is None or complete == scope and attempt == scope, "ownership")
        snapshot = self.nvme_snapshot(scope)
        require(snapshot is None or attempt == scope, "nvmeof-foreign")
        require(complete is None or self.nvme_complete(scope, snapshot), "nvmeof-identity")
        evidence["owned"], evidence["exists"] = attempt is not None, snapshot is not None
        evidence["resource"] = {"id": scope["identity"]["serial"] if snapshot is not None else "", "path": ""}
        return ready and complete == scope and self.nvme_complete(scope, snapshot) and self.prove_nvme_ana(scope)


def validate_unit(unit, request, hosts):
    del hosts
    require(named(unit["id"]) and len(unit["documents"]) == 1)
    document = unit["documents"][0]
    require(document["service_type"] == "nvmeof" and document["service_id"] == unit["id"])
    spec = document.get("spec")
    require(isinstance(spec, dict) and {"pool", "group", "port"} <= set(spec)
            <= {"pool", "group", "port", "enable_auth", "enable_mtls", "discovery_port", "transports"})
    require(spec.get("transports", "tcp") == "tcp" and type(spec.get("discovery_port", 8009)) is int and 1 <= spec.get("discovery_port", 8009) <= 65535)
    require(named(spec["pool"]) and named(spec["group"]) and type(spec["port"]) is int and 1 <= spec["port"] <= 65535)
    require(all(type(spec.get(k, False)) is bool for k in ("enable_auth", "enable_mtls")))
    require(spec.get("enable_auth", False) != spec.get("enable_mtls", False))
    placement = document["placement"]
    require(1 <= len(placement["hosts"]) <= 16 and placement.get("count", len(placement["hosts"])) == len(placement["hosts"]))
    paths = {b["path"] for b in unit.get("bindings", [])}
    require(paths == ({"/spec/" + field for field in TLS_FIELDS} if spec.get("enable_auth") else set()))
    require("nvmeof" in {entry["name"] for entry in request.get("auxiliary", [])})


def check_unit(worker, unit):
    worker.owned_pool(unit["documents"][0]["spec"]["pool"])
    qualify_commands(worker)
    require("dashboard" in worker.manager_modules(), "nvmeof-native")
    unused, held = worker.image_settings()
    require(worker.image_key("nvmeof") in held, "foreign-image")


def expected_unit(unit):
    return {("nvmeof." + unit["id"], "nvmeof"): len(unit["documents"][0]["placement"]["hosts"])}


def readiness_unit(worker, unit, blocking):
    del blocking
    spec = unit["documents"][0]["spec"]
    ready = True
    for host in unit["documents"][0]["placement"]["hosts"]:
        address = next(h["address"] for h in worker.request["hosts"] if h["name"] == host)
        value = worker.ceph("nvmeof", "gateway", "info", "--gw_group", spec["group"], "--server_address", address)
        require(isinstance(value, dict) and value.get("hostname") == host and value.get("group") == spec["group"], "nvmeof-identity")
        ready = ready and value.get("status") == 0 and value.get("gateway_initialization_over") is True
    return ready


def listener_reachable(listener):
    try:
        with socket.create_connection((listener["address"], listener["port"]), timeout=2):
            return True
    except OSError:
        return False


def qualify_commands(worker):
    output = worker.ceph("--help", json_result=False)
    require(isinstance(output, bytes) and 0 < len(output) <= ceph_native.MAX_OUTPUT, "native-output")
    for family, verb in COMMANDS:
        prefix = ("nvmeof " + family + " " + verb).encode("ascii")
        require(re.search(rb"(?m)^\s*" + re.escape(prefix) + rb"(?:\s|$)", output) is not None, "nvmeof-native")


def native_size(value):
    # Protobuf JSON emits uint64 as decimal text; some native wrappers emit a JSON integer.
    if isinstance(value, str) and re.fullmatch(r"(?:0|[1-9][0-9]{0,19})", value):
        value = int(value)
    require(type(value) is int and 0 <= value <= (1 << 64) - 1, "native-output")
    return value

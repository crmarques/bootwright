"""Closed staging plans for the unqualified IBM verified-tunnel candidate.

These helpers never read files, run commands or promote an artifact. The caller
owns verified host access, exact native-file descriptors, durable intent and
current runtime proof. The initial public ``cephadm deploy`` route is deprecated
in the inspected provider source; its behavior still needs lab qualification.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import ipaddress
import json
import os
import re
import shlex
import uuid

ROOT = "/var/lib/bootwright/ceph"
CONTAINER_ROOT = "/run/bootwright-ceph-ssh"
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
IMAGE = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}\Z")
SCRATCH = re.compile(r"\.(?:bootstrap|tunnel-stage)-[a-zA-Z0-9_-]{1,64}\Z")
MAX_METADATA = 4096
MAX_SSHD_CONFIG = 256 * 1024
RECOVERY_STAGES = frozenset(("unclaimed", "claimed", "prepared", "seed", "configured", "mounted", "complete"))


class StageRefusal(ValueError):
    """A fixed failure reason, never native output or protected material."""


def require(condition):
    if not condition:
        raise StageRefusal("native-tunnel-stage-unproved")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def named(value):
    return isinstance(value, str) and NAME.fullmatch(value) is not None


def bindings(request):
    """Validate the staging subset without inferring ownership from native names."""
    require(isinstance(request, dict))
    require(request.get("version") == "ceph-core-v4" and request.get("clusterSSHTransport") == "verified-tunnel")
    require(named(request.get("owner")) and named(request.get("bootstrap")))
    fsid = request.get("fsid")
    try:
        require(str(uuid.UUID(fsid)) == fsid)
    except (ValueError, TypeError, AttributeError):
        raise StageRefusal("native-tunnel-stage-unproved") from None
    require(isinstance(request.get("image"), str) and IMAGE.fullmatch(request["image"]) is not None)
    tool = request.get("cephadm")
    require(isinstance(tool, dict) and set(tool) == {"path", "version", "packageVersion"})
    require(tool["path"] in ("/usr/sbin/cephadm", "/usr/bin/cephadm"))
    require(tool["version"] == "20.2.1" and tool["packageVersion"] == "20.2.1-405.el9cp")
    hosts = request.get("hosts")
    require(isinstance(hosts, list) and 1 <= len(hosts) <= 256)
    result, names, machines, addresses, stems = [], set(), set(), set(), set()
    for host in hosts:
        require(isinstance(host, dict) and named(host.get("name")) and named(host.get("machine")))
        address = host.get("address")
        try:
            require(isinstance(address, str) and str(ipaddress.ip_address(address)) == address)
        except (ValueError, TypeError):
            raise StageRefusal("native-tunnel-stage-unproved") from None
        require(type(host.get("port")) is int and 1 <= host["port"] <= 65535)
        stem = host["name"].split(".", 1)[0]
        require(stem and len(stem) <= 120)
        require(host["name"] not in names and host["machine"] not in machines and address not in addresses and stem not in stems)
        names.add(host["name"])
        machines.add(host["machine"])
        addresses.add(address)
        stems.add(stem)
        result.append({key: host[key] for key in ("name", "machine", "address", "port")})
    require(request["bootstrap"] in names)
    bootstrap = next(host for host in result if host["name"] == request["bootstrap"])
    require(request.get("monIP") == bootstrap["address"])
    return sorted(result, key=lambda host: host["name"])


def stage_intent(request):
    """Freeze deterministic provider-shaped IDs before the first native effect."""
    hosts = bindings(request)
    identity = {"version": "ceph-tunnel-stage-v1", "owner": request["owner"], "fsid": request["fsid"],
                "image": request["image"], "bootstrap": request["bootstrap"], "hosts": hosts}
    digest = hashlib.sha256(canonical(identity)).digest()
    # Native generated MGR IDs use a short hostname plus six lowercase letters.
    # The full bindings above, not this shortened suffix, carry ownership proof.
    stem = request["bootstrap"].split(".", 1)[0]
    identity["monID"] = stem
    identity["mgrID"] = stem + "." + "".join(chr(ord("a") + part % 26) for part in digest[:6])
    return identity


def recovery_scope(request):
    """Bind all cluster authoring, without consumer actions or private material."""
    bindings(request)
    excluded = {"action", "pool", "client", "packages"}
    return {"version": "ceph-bootstrap-recovery-v1",
            "cluster": {name: value for name, value in request.items() if name not in excluded}}


def recovery_record(request, host, stage):
    require(host in {item["name"] for item in bindings(request)} and stage in RECOVERY_STAGES)
    return {"version": "ceph-bootstrap-state-v1", "scope": recovery_scope(request), "host": host, "stage": stage}


def prove_recovery_record(request, host, value, evidence=False):
    fields = {"version", "scope", "host", "stage"} | ({"evidence"} if evidence else set())
    require(isinstance(value, dict) and set(value) == fields and value.get("stage") in RECOVERY_STAGES)
    require(canonical({name: value[name] for name in fields - {"evidence"}}) ==
            canonical(recovery_record(request, host, value["stage"])))
    return value["stage"]


def seed_daemon(request, daemon, observing):
    """An action needs the seed daemon running and enabled. An observation also admits it between cephadm's --rm
    containers, when it names no image only if it has no container."""
    require(daemon.get("fsid") == request["fsid"] and daemon.get("style") == "cephadm:v1")
    images = (request["image"], "") if observing and daemon.get("container_id") == "" else (request["image"],)
    require(daemon.get("container_image_name") in images)
    require(observing or daemon.get("state") == "running" and daemon.get("enabled") is True)


def prove_seed_daemons(request, daemons, observing=False):
    """Metadata-only proof admits capture, never consumption of native secrets."""
    intent = stage_intent(request)
    require(isinstance(daemons, list) and len(daemons) == 2)
    names = {"mon." + intent["monID"], "mgr." + intent["mgrID"]}
    observed = set()
    for daemon in daemons:
        require(isinstance(daemon, dict) and daemon.get("name") in names and daemon["name"] not in observed)
        seed_daemon(request, daemon, observing)
        observed.add(daemon["name"])
    require(observed == names)
    return intent


def marker_path(request):
    bindings(request)
    return os.path.join(ROOT, request["fsid"])


def scratch_path(request, scratch):
    parent = marker_path(request)
    require(isinstance(scratch, str) and os.path.dirname(scratch) == parent)
    require(SCRATCH.fullmatch(os.path.basename(scratch)) is not None)
    return scratch


def bootstrap_argv(request, scratch):
    """Stage zero-host bootstrap without the provider's unverified SSH checks."""
    intent = stage_intent(request)
    scratch_path(request, scratch)
    require(request.get("callHome") == "enabled")
    argv = [request["cephadm"]["path"], "--image", request["image"], "bootstrap",
            "--fsid", request["fsid"], "--mon-id", intent["monID"], "--mgr-id", intent["mgrID"],
            "--mon-ip", request["monIP"], "--output-dir", marker_path(request),
            "--skip-ssh", "--skip-monitoring-stack", "--skip-dashboard",
            "--skip-admin-label", "--allow-fqdn-hostname", "--no-cleanup-on-failure",
            "--config", os.path.join(scratch, "ceph.conf"), "--automatically-accept-license"]
    networks = request.get("clusterNetworks")
    require(networks is None or isinstance(networks, list) and len(networks) <= 8)
    for network in networks or []:
        try:
            require(isinstance(network, str) and str(ipaddress.ip_network(network, strict=True)) == network)
        except (ValueError, TypeError):
            raise StageRefusal("native-tunnel-stage-unproved") from None
    if networks:
        argv.extend(["--cluster-network", ",".join(networks)])
    return argv


def initial_files(request):
    """Derive exact paths; the caller must open all ancestors without links."""
    intent = stage_intent(request)
    result = {}
    for kind, identifier in (("mon", intent["monID"]), ("mgr", intent["mgrID"])):
        directory = os.path.join("/var/lib/ceph", request["fsid"], kind + "." + identifier)
        result[kind] = {name: os.path.join(directory, name) for name in ("config", "keyring", "unit.meta")}
    return result


def unique_object(pairs):
    result = {}
    for name, value in pairs:
        require(name not in result)
        result[name] = value
    return result


def parse_metadata(raw, kind):
    """Accept only the inspected minimal bootstrap's public metadata shape."""
    require(kind in ("mon", "mgr") and isinstance(raw, bytes) and 0 < len(raw) <= MAX_METADATA)
    try:
        value = json.loads(raw, object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise StageRefusal("native-tunnel-stage-unproved") from None
    require(isinstance(value, dict) and set(value) == {"service_name", "ports", "memory_request", "memory_limit"})
    require(value["service_name"] == kind and value["memory_request"] is None and value["memory_limit"] is None)
    ports = value["ports"]
    require(isinstance(ports, list) and all(type(port) is int and 1 <= port <= 65535 for port in ports))
    require(len(set(ports)) == len(ports))
    # IBM create_mon passes no endpoints; create_mgr passes these two when the
    # monitoring/dashboard bootstrap options are disabled. Preserve their order.
    require(not ports if kind == "mon" else len(ports) == 2 and set(ports) == {9283, 8765})
    return value


def initial_redeploy_plan(request, intent, daemon_list, host_inventory, mon_metadata, mgr_metadata, observing=False):
    """Prove the exact first MON/MGR pair before planning any local redeploy."""
    expected = stage_intent(request)
    require(intent == expected)
    require(isinstance(host_inventory, list) and host_inventory == [])
    require(isinstance(daemon_list, list) and len(daemon_list) == 2)
    names = {"mon." + expected["monID"], "mgr." + expected["mgrID"]}
    observed = set()
    for daemon in daemon_list:
        require(isinstance(daemon, dict) and daemon.get("name") in names and daemon["name"] not in observed)
        seed_daemon(request, daemon, observing)
        observed.add(daemon["name"])
    require(observed == names)
    mon = parse_metadata(mon_metadata, "mon")
    mgr = parse_metadata(mgr_metadata, "mgr")
    return {"version": "ceph-tunnel-redeploy-v1", "intent": expected, "monMetadata": mon, "mgrMetadata": mgr}


def redeploy_argv(request, plan, scratch):
    """Append the protected mount while preserving frozen daemon metadata/ports."""
    intent = stage_intent(request)
    require(isinstance(plan, dict) and set(plan) == {"version", "intent", "monMetadata", "mgrMetadata"})
    require(plan["version"] == "ceph-tunnel-redeploy-v1" and plan["intent"] == intent)
    parse_metadata(canonical(plan["monMetadata"]), "mon")
    metadata = parse_metadata(canonical(plan["mgrMetadata"]), "mgr")
    scratch_path(request, scratch)
    bundle = os.path.join(marker_path(request), "transport")
    return [request["cephadm"]["path"], "--image", request["image"], "deploy", "--fsid", request["fsid"],
            "--name", "mgr." + intent["mgrID"], "--config", os.path.join(scratch, "mgr.config"),
            "--keyring", os.path.join(scratch, "mgr.keyring"), "--meta-json", canonical(metadata).decode("ascii"),
            "--tcp-ports", " ".join(str(port) for port in metadata["ports"]),
            "--extra-container-args=-v", "--extra-container-args=" + bundle + ":" + CONTAINER_ROOT + ":ro,z"]


def sshd_includes(raw, owned_policy=None):
    """Expose a closed include set and forbid unproved conditional policies.

    The caller proves descriptors, ancestor/file ownership, permissions, links,
    bounded recursion and the exact owned-policy filename. This function never
    resolves paths or accepts ownership based on matching content alone.
    """
    require(isinstance(raw, bytes) and 0 < len(raw) <= MAX_SSHD_CONFIG)
    if owned_policy is not None:
        require(isinstance(owned_policy, bytes) and raw == owned_policy)
        return []
    try:
        text = raw.decode("ascii")
    except UnicodeError:
        raise StageRefusal("native-tunnel-stage-unproved") from None
    require(all(character in "\t\r\n" or " " <= character <= "~" for character in text))
    includes = []
    for line in text.splitlines():
        try:
            lexer = shlex.shlex(line, posix=True)
            lexer.whitespace_split = True
            lexer.commenters = "#"
            words = list(lexer)
        except ValueError:
            raise StageRefusal("native-tunnel-stage-unproved") from None
        if not words:
            continue
        first = words.pop(0)
        if "=" in first:
            keyword, first_argument = first.split("=", 1)
            if first_argument:
                words.insert(0, first_argument)
        else:
            keyword = first
            if words and words[0] == "=":
                words.pop(0)
        require(keyword.lower() != "match")
        if keyword.lower() != "include":
            continue
        require(words)
        for pattern in words:
            allowed = pattern == "/etc/ssh/sshd_config.d/*.conf"
            allowed = allowed or re.fullmatch(r"/etc/ssh/sshd_config\.d/[A-Za-z0-9_-][A-Za-z0-9_.-]{0,127}\.conf", pattern) is not None
            allowed = allowed or pattern == "/etc/crypto-policies/back-ends/opensshserver.config"
            require(allowed)
            includes.append(pattern)
    require(len(includes) <= 64)
    return includes

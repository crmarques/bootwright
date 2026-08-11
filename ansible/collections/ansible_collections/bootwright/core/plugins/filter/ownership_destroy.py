from __future__ import annotations

import hashlib
import json
import posixpath
import re
import xml.etree.ElementTree as ET


_LIBVIRT_METADATA_NAMESPACE = "https://bootwright.io/libvirt/metadata/1.0"


def bootwright_libvirt_explicit_absence(value, resource_kind):
    if not isinstance(value, str) or resource_kind not in {
        "libvirt-domain",
        "libvirt-network",
        "libvirt-pool",
    }:
        return False
    nouns = {
        "libvirt-domain": "domain",
        "libvirt-network": "network",
        "libvirt-pool": r"(?:storage\s+)?pool",
    }
    noun = nouns[resource_kind]
    patterns = [
        rf"(?im)^\s*(?:error:\s*)?{noun}\s+not\s+found(?:\s*:|\s*$)",
        rf"(?i)no\s+{noun}\s+with\s+matching\s+name",
    ]
    return any(re.search(pattern, value) is not None for pattern in patterns)


def bootwright_podman_container_identity(value, expected):
    if not isinstance(value, str) or not isinstance(expected, dict):
        return False
    if not expected or any(
        not isinstance(key, str)
        or not key
        or not isinstance(item, str)
        or not item
        for key, item in expected.items()
    ):
        return False
    try:
        document = json.loads(value)
    except (TypeError, ValueError):
        return False
    if not isinstance(document, list) or len(document) != 1:
        return False
    container = document[0]
    if not isinstance(container, dict):
        return False
    config = container.get("Config")
    if not isinstance(config, dict):
        return False
    labels = config.get("Labels")
    if not isinstance(labels, dict):
        return False
    return all(labels.get(key) == item for key, item in expected.items())


def bootwright_podman_container_context(value):
    try:
        if not isinstance(value, str):
            return {}
        document = json.loads(value)
        if not isinstance(document, list) or len(document) != 1:
            return {}
        config = document[0].get("Config")
        labels = config.get("Labels") if isinstance(config, dict) else None
        if not isinstance(labels, dict):
            return {}
        context = labels.get("bootwright.context", "")
        if not isinstance(context, str):
            return {}
        if context and re.fullmatch(r"[A-Za-z0-9_.-]+", context) is None:
            return {}
        return {"valid": True, "context": context}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_podman_explicit_absence(value):
    if not isinstance(value, str):
        return False
    patterns = [
        r"(?im)^\s*(?:error:\s*)?no such container(?:\s*:|\s+['\"]?[A-Za-z0-9_.-]+['\"]?\s*$)",
        r"(?im)^\s*(?:error:\s*)?no container with name or id(?:\s*:|\s+).+$",
        r"(?im)^\s*(?:error:\s*)?no such object(?:\s*:|\s+).+$",
    ]
    return any(re.search(pattern, value) is not None for pattern in patterns)


def _bootwright_bmc_pool_parts(value):
    if not isinstance(value, str) or not value.strip():
        return None
    try:
        root = ET.fromstring(value)
    except (ET.ParseError, TypeError, ValueError):
        return None
    if root.tag != "pool" or root.attrib != {"type": "dir"}:
        return None
    names = [child for child in list(root) if child.tag == "name"]
    targets = [child for child in list(root) if child.tag == "target"]
    metadata = [child for child in list(root) if child.tag == "metadata"]
    paths = targets[0].findall("./path") if len(targets) == 1 else []
    uuids = [child for child in list(root) if child.tag == "uuid"]
    if (
        len(names) != 1
        or names[0].attrib
        or list(names[0])
        or len(root.findall(".//name")) != 1
        or len(targets) != 1
        or len(paths) != 1
        or paths[0].attrib
        or list(paths[0])
        or len(metadata) != 1
        or metadata[0].attrib
        or len(uuids) > 1
    ):
        return None
    name = names[0].text.strip() if isinstance(names[0].text, str) else ""
    target_path = paths[0].text.strip() if isinstance(paths[0].text, str) else ""
    if not name or not target_path.startswith("/"):
        return None
    uuid = ""
    if uuids:
        uuid = uuids[0].text.strip() if isinstance(uuids[0].text, str) else ""
        if not re.fullmatch(
            r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
            uuid,
        ):
            return None
    resources = root.findall(
        f"./metadata/{{{_LIBVIRT_METADATA_NAMESPACE}}}bmcService"
    )
    if len(resources) != 1 or resources[0].attrib or list(metadata[0]) != resources:
        return None
    allowed = {
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}context": "context",
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}provider": "provider",
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}host": "host",
    }
    identity = {}
    for child in list(resources[0]):
        key = allowed.get(child.tag)
        if key is None or key in identity or child.attrib or list(child):
            return None
        text = child.text.strip() if isinstance(child.text, str) else ""
        if not text:
            return None
        identity[key] = text
    if set(identity) != set(allowed.values()):
        return None
    return identity, name, target_path, uuid


def bootwright_bmc_pool_identity(value):
    parts = _bootwright_bmc_pool_parts(value)
    if parts is None:
        return {}
    identity, name, target_path, _ = parts
    return {**identity, "type": "dir", "name": name, "targetPath": target_path}


def bootwright_bmc_pool_uuid(value):
    parts = _bootwright_bmc_pool_parts(value)
    return "" if parts is None else parts[3]


def _bootwright_systemd_unit_parts(value):
    if not isinstance(value, str) or not value.strip():
        return None
    allowed = {
        "Unit": {"Description", "After", "Wants"},
        "Service": {
            "Type",
            "Environment",
            "ExecStart",
            "Restart",
            "RestartSec",
            "StandardOutput",
            "StandardError",
        },
        "Install": {"WantedBy"},
    }
    sections = {}
    section = None
    for raw_line in value.splitlines():
        line = raw_line.strip()
        if not line:
            continue
        header = re.fullmatch(r"\[([A-Za-z]+)\]", line)
        if header is not None:
            section = header.group(1)
            if section not in allowed or section in sections:
                return None
            sections[section] = []
            continue
        if section is None or "=" not in line:
            return None
        key, entry_value = line.split("=", 1)
        if key not in allowed[section] or not entry_value or key.strip() != key:
            return None
        sections[section].append((key, entry_value))
    if set(sections) != set(allowed):
        return None
    for name, required in allowed.items():
        keys = [key for key, _ in sections[name]]
        if name == "Service":
            if set(keys) != required or any(
                keys.count(key) != 1 for key in required - {"Environment"}
            ):
                return None
        elif set(keys) != required or len(keys) != len(required):
            return None
    unit = dict(sections["Unit"])
    service = {
        key: entry_value
        for key, entry_value in sections["Service"]
        if key != "Environment"
    }
    install = dict(sections["Install"])
    if (
        service["Type"] != "simple"
        or service["Restart"] != "on-failure"
        or service["RestartSec"] != "5"
        or service["StandardOutput"] != service["StandardError"]
        or re.fullmatch(r"append:\S+/(?:sushy|vmedia)[.]log", service["StandardOutput"])
        is None
        or install["WantedBy"] != "multi-user.target"
    ):
        return None
    environment_keys = {
        "BOOTWRIGHT_CONTEXT": "context",
        "BOOTWRIGHT_PROVIDER": "provider",
        "BOOTWRIGHT_HOST": "host",
    }
    allowed_environment = set(environment_keys) | {"TMPDIR", "TMP", "TEMP"}
    environment = {}
    for key, line in sections["Service"]:
        if key != "Environment":
            continue
        match = re.fullmatch(r'(?:"([A-Z_]+)=([^"\s]+)"|([A-Z_]+)=(\S+))', line)
        if match is None:
            return None
        environment_key = match.group(1) or match.group(3)
        environment_value = match.group(2) or match.group(4)
        if environment_key not in allowed_environment or environment_key in environment:
            return None
        environment[environment_key] = environment_value
    base_environment = set(environment_keys)
    temp_environment = {"TMPDIR", "TMP", "TEMP"}
    if set(environment) not in [base_environment, base_environment | temp_environment]:
        return None
    if temp_environment <= set(environment) and len({environment[key] for key in temp_environment}) != 1:
        return None
    identity = {}
    for environment_key, key in environment_keys.items():
        identity[key] = environment[environment_key]
    if unit["Description"] == f"Bootwright sushy-emulator ({identity['provider']})":
        kind = "sushy"
        if unit["After"] != "libvirtd.service network-online.target" or unit["Wants"] != "libvirtd.service network-online.target" or set(environment) != base_environment | temp_environment:
            return None
    elif unit["Description"] == f"Bootwright sushy vmedia HTTP ({identity['provider']})":
        kind = "vmedia"
        if unit["After"] != "network-online.target" or unit["Wants"] != "network-online.target" or set(environment) != base_environment:
            return None
    else:
        return None
    return identity, service, environment, kind


def bootwright_systemd_unit_identity(value):
    parts = _bootwright_systemd_unit_parts(value)
    return {} if parts is None else parts[0]


def bootwright_bmc_sushy_config_identity(value):
    if not isinstance(value, str) or not value.strip():
        return {}
    assignments = {}
    for raw_line in value.splitlines():
        line = raw_line.strip()
        if not line:
            continue
        match = re.fullmatch(r"([A-Z][A-Z0-9_]*)\s*=\s*(.+)", line)
        if match is None or match.group(1) in assignments:
            return {}
        assignments[match.group(1)] = match.group(2)
    required = {
        "SUSHY_EMULATOR_LIBVIRT_URI",
        "SUSHY_EMULATOR_FEATURE_SET",
        "SUSHY_EMULATOR_STATE_DIR",
        "SUSHY_EMULATOR_STORAGE_POOL",
        "SUSHY_EMULATOR_LISTEN_IP",
        "SUSHY_EMULATOR_LISTEN_PORT",
    }
    optional = {"SUSHY_EMULATOR_AUTH_FILE"}
    if set(assignments) not in [required, required | optional]:
        return {}

    def unicode_string(key):
        match = re.fullmatch(r"u'([^'\r\n]+)'", assignments[key])
        return "" if match is None else match.group(1)

    libvirt_uri = unicode_string("SUSHY_EMULATOR_LIBVIRT_URI")
    feature_set = unicode_string("SUSHY_EMULATOR_FEATURE_SET")
    state_dir = unicode_string("SUSHY_EMULATOR_STATE_DIR")
    pool = unicode_string("SUSHY_EMULATOR_STORAGE_POOL")
    bind_address = unicode_string("SUSHY_EMULATOR_LISTEN_IP")
    port_match = re.fullmatch(
        r"([1-9][0-9]{0,4})", assignments["SUSHY_EMULATOR_LISTEN_PORT"]
    )
    if (
        not libvirt_uri
        or feature_set != "vmedia"
        or not state_dir.startswith("/")
        or not state_dir.endswith("/state")
        or state_dir == "/state"
        or not pool
        or not bind_address
        or port_match is None
        or int(port_match.group(1)) > 65535
    ):
        return {}
    state_root = state_dir[: -len("/state")]
    auth_path = ""
    if "SUSHY_EMULATOR_AUTH_FILE" in assignments:
        auth_path = unicode_string("SUSHY_EMULATOR_AUTH_FILE")
        if auth_path != state_root + "/htpasswd":
            return {}
    return {
        "libvirtURI": libvirt_uri,
        "stateRoot": state_root,
        "pool": pool,
        "bindAddress": bind_address,
        "redfishPort": port_match.group(1),
        "authPath": auth_path,
    }


def bootwright_bmc_vmedia_unit_runtime(value):
    parts = _bootwright_systemd_unit_parts(value)
    if parts is None or parts[3] != "vmedia":
        return {}
    identity, service, _, _ = parts
    match = re.fullmatch(
        r"/usr/bin/python3\s+-m\s+http[.]server\s+([0-9]+)\s+--bind\s+127[.]0[.]0[.]1\s+--directory\s+(\S+)",
        service["ExecStart"],
    )
    if match is None:
        return {}
    port, root = match.groups()
    return {**identity, "vMediaPort": port, "vMediaRoot": root}


def bootwright_bmc_sushy_unit_runtime(value):
    parts = _bootwright_systemd_unit_parts(value)
    if parts is None or parts[3] != "sushy":
        return {}
    identity, service, environment, _ = parts
    match = re.fullmatch(
        r"(\S+)/venv/bin/sushy-emulator\s+--config\s+(\S+)/sushy[.]conf",
        service["ExecStart"],
    )
    if (
        match is None
        or match.group(1) != match.group(2)
        or any(environment[key] != match.group(1) + "/tmp" for key in ["TMPDIR", "TMP", "TEMP"])
        or service["StandardOutput"] != "append:" + match.group(1) + "/sushy.log"
    ):
        return {}
    return {**identity, "stateRoot": match.group(1)}


def bootwright_bmc_claim_identity(
    value, expected_context, expected_provider, expected_host, provider_state_dir
):
    if not all(
        isinstance(item, str)
        and re.fullmatch(r"[A-Za-z0-9_.-]+", item or "") is not None
        for item in [expected_context, expected_provider, expected_host]
    ):
        return {}
    if (
        not isinstance(provider_state_dir, str)
        or not provider_state_dir.startswith("/")
        or provider_state_dir == "/"
        or posixpath.normpath(provider_state_dir) != provider_state_dir
    ):
        return {}
    if not isinstance(value, str) or not value.strip():
        return {}
    try:
        document = json.loads(value)
    except (TypeError, ValueError):
        return {}
    if not isinstance(document, dict) or set(document) != {
        "apiVersion",
        "kind",
        "name",
        "owner",
        "context",
        "host",
        "provider",
        "paths",
        "attributes",
    }:
        return {}
    state_root = f"{provider_state_dir}/bmc/{expected_provider}"
    vmedia_root = f"/var/lib/libvirt/images/bootwright/{expected_context}/bmc/{expected_provider}/vmedia"
    claim_path = f"/var/lib/bootwright/shared-services/bmc-emulator/{expected_provider}"
    redfish_unit = f"bootwright-sushy-{expected_provider}.service"
    vmedia_unit = f"bootwright-vmedia-{expected_provider}.service"
    if any(
        [
            document.get("apiVersion")
            != "bootwright.io/bmc-service-claim/v1alpha1",
            document.get("kind") != "bmc-emulator",
            document.get("name") != expected_provider,
            document.get("owner") != "bootwright",
            document.get("context") != expected_context,
            document.get("host") != expected_host,
            document.get("provider") != expected_provider,
            document.get("paths")
            != [
                state_root,
                vmedia_root,
                f"/etc/systemd/system/{redfish_unit}",
                f"/etc/systemd/system/{vmedia_unit}",
                claim_path,
            ],
        ]
    ):
        return {}
    attrs = document.get("attributes")
    if not isinstance(attrs, dict) or set(attrs) != {
        "redfishUnit",
        "vMediaUnit",
        "redfishPort",
        "vMediaPort",
        "libvirtURI",
        "bindAddress",
        "authPath",
        "pool",
        "claimPath",
        "stateRoot",
        "vMediaRoot",
        "firewallManaged",
    }:
        return {}
    if any(not isinstance(item, str) for item in attrs.values()):
        return {}
    redfish_port = _bootwright_bmc_port(attrs.get("redfishPort"))
    vmedia_port = _bootwright_bmc_port(attrs.get("vMediaPort"))
    if (
        attrs.get("redfishUnit") != redfish_unit
        or attrs.get("vMediaUnit") != vmedia_unit
        or not redfish_port
        or not vmedia_port
        or redfish_port == vmedia_port
        or not attrs.get("libvirtURI")
        or attrs["libvirtURI"].strip() != attrs["libvirtURI"]
        or not attrs.get("bindAddress")
        or attrs["bindAddress"].strip() != attrs["bindAddress"]
        or attrs.get("authPath") not in ["", state_root + "/htpasswd"]
        or attrs.get("pool") != f"bootwright-{expected_provider}-vmedia"
        or attrs.get("claimPath") != claim_path
        or attrs.get("stateRoot") != state_root
        or attrs.get("vMediaRoot") != vmedia_root
        or attrs.get("firewallManaged") not in ["true", "false"]
    ):
        return {}
    return document


def bootwright_bmc_transition_identity(
    value, expected_context, expected_provider, expected_host, provider_state_dir
):
    if not isinstance(value, str) or not value.strip():
        return {}
    try:
        document = json.loads(value)
    except (TypeError, ValueError):
        return {}
    if not isinstance(document, dict) or set(document) != {
        "apiVersion",
        "kind",
        "name",
        "owner",
        "context",
        "host",
        "provider",
        "active",
        "pending",
    }:
        return {}
    expected = {
        "apiVersion": "bootwright.io/bmc-service-transition/v1alpha1",
        "kind": "bmc-emulator-transition",
        "name": expected_provider,
        "owner": "bootwright",
        "context": expected_context,
        "host": expected_host,
        "provider": expected_provider,
    }
    if any(document.get(key) != item for key, item in expected.items()):
        return {}
    active = bootwright_bmc_claim_identity(
        json.dumps(document.get("active")),
        expected_context,
        expected_provider,
        expected_host,
        provider_state_dir,
    )
    pending = bootwright_bmc_claim_identity(
        json.dumps(document.get("pending")),
        expected_context,
        expected_provider,
        expected_host,
        provider_state_dir,
    )
    if not active or not pending or active == pending:
        return {}
    if document["active"] != active or document["pending"] != pending:
        return {}
    return document


def bootwright_bmc_claim_digest(value):
    if not isinstance(value, (dict, list)) or not value:
        return ""
    try:
        payload = json.dumps(
            value,
            ensure_ascii=True,
            separators=(",", ":"),
            sort_keys=True,
        ).encode("utf-8")
    except (TypeError, ValueError):
        return ""
    return "sha256:" + hashlib.sha256(payload).hexdigest()


def bootwright_bmc_consequence_digest(value):
    if (
        not isinstance(value, dict)
        or value.get("kind") != "bmc-emulator"
        or not isinstance(value.get("attributes"), dict)
    ):
        return ""
    attributes = {
        key: item
        for key, item in value["attributes"].items()
        if key != "firewallManaged"
    }
    consequence = {
        "apiVersion": "bootwright.io/bmc-host-consequence/v1alpha1",
        "kind": "bmc-emulator",
        "name": value.get("name"),
        "context": value.get("context"),
        "host": value.get("host"),
        "provider": value.get("provider"),
        "paths": value.get("paths"),
        "attributes": attributes,
    }
    if (
        any(
            not isinstance(consequence.get(key), str)
            or not consequence[key]
            for key in ["name", "context", "host", "provider"]
        )
        or not isinstance(consequence["paths"], list)
        or not consequence["paths"]
        or any(not isinstance(item, str) or not item for item in consequence["paths"])
        or not attributes
        or any(not isinstance(item, str) for item in attributes.values())
    ):
        return ""
    return bootwright_bmc_claim_digest(consequence)


def _bootwright_host_mutation_lease(value):
    if not isinstance(value, dict) or set(value) != {
        "apiVersion",
        "kind",
        "token",
        "runId",
        "command",
        "controller",
        "pid",
        "processStart",
        "startedAt",
    }:
        return {}
    if (
        value.get("apiVersion")
        != "bootwright.io/host-mutation-lease/v1alpha1"
        or value.get("kind") != "host-mutation-lease"
        or re.fullmatch(r"sha256:[a-f0-9]{64}", value.get("token", "")) is None
        or value.get("command") not in ["apply", "destroy"]
        or isinstance(value.get("pid"), bool)
        or not isinstance(value.get("pid"), int)
        or value["pid"] <= 0
        or not isinstance(value.get("processStart"), str)
        or re.fullmatch(
            r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[.][0-9]{1,9})?Z",
            value.get("startedAt", ""),
        )
        is None
    ):
        return {}
    for key in ["runId", "controller"]:
        item = value.get(key)
        if (
            not isinstance(item, str)
            or not item
            or item.strip() != item
            or "\n" in item
        ):
            return {}
    return value


def bootwright_host_operation_guard_any(value):
    try:
        if isinstance(value, str):
            value = json.loads(value)
        if not isinstance(value, dict) or set(value) != {
            "apiVersion",
            "kind",
            "owner",
            "context",
            "host",
            "operation",
            "invocation",
            "selectionDigest",
            "selection",
            "lease",
        }:
            return {}
        selection = value.get("selection")
        if (
            value.get("apiVersion")
            != "bootwright.io/host-shared-service-operation/v1alpha1"
            or value.get("kind") != "host-shared-service-operation"
            or value.get("owner") != "bootwright"
            or value.get("operation") not in ["apply", "destroy"]
            or re.fullmatch(
                r"sha256:[a-f0-9]{64}", value.get("selectionDigest", "")
            )
            is None
            or not isinstance(selection, dict)
            or set(selection)
            != {"apiVersion", "kind", "context", "command", "host", "consequences"}
            or selection.get("apiVersion")
            != "bootwright.io/host-shared-service-selection/v1alpha1"
            or selection.get("kind") != "host-shared-service-selection"
            or selection.get("context") != value.get("context")
            or selection.get("command") != value.get("operation")
            or selection.get("host") != value.get("host")
            or value.get("selectionDigest") != bootwright_bmc_claim_digest(selection)
            or value.get("operation") != value.get("lease", {}).get("command")
            or not _bootwright_host_mutation_lease(value.get("lease"))
        ):
            return {}
        for item in [value.get("context"), value.get("host")]:
            if not isinstance(item, str) or re.fullmatch(r"[A-Za-z0-9_.-]+", item) is None:
                return {}
        consequences = selection.get("consequences")
        if not isinstance(consequences, list) or not consequences:
            return {}
        keys = []
        for consequence in consequences:
            if not isinstance(consequence, dict) or set(consequence) not in [
                {"kind", "name", "selectionDigests"},
                {"kind", "name", "selectionDigests", "claimDigests"},
            ]:
                return {}
            key = (consequence.get("kind"), consequence.get("name"))
            if any(
                not isinstance(item, str)
                or re.fullmatch(r"[A-Za-z0-9_.-]+", item) is None
                for item in key
            ):
                return {}
            digests = consequence.get("selectionDigests")
            if (
                not isinstance(digests, list)
                or not digests
                or digests != sorted(set(digests))
                or any(
                    re.fullmatch(r"sha256:[a-f0-9]{64}", item or "") is None
                    for item in digests
                )
            ):
                return {}
            claim_digests = consequence.get("claimDigests")
            if claim_digests is not None and (
                not isinstance(claim_digests, list)
                or not claim_digests
                or claim_digests != sorted(set(claim_digests))
                or any(
                    re.fullmatch(r"sha256:[a-f0-9]{64}", item or "") is None
                    for item in claim_digests
                )
            ):
                return {}
            keys.append(key)
        if keys != sorted(set(keys)):
            return {}
        invocation = value.get("invocation")
        if (
            not isinstance(invocation, str)
            or not invocation
            or invocation.strip() != invocation
            or "\n" in invocation
        ):
            return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_operation_guard(
    lease,
    selection,
    invocation,
):
    if not isinstance(selection, dict):
        return {}
    document = {
        "apiVersion": "bootwright.io/host-shared-service-operation/v1alpha1",
        "kind": "host-shared-service-operation",
        "owner": "bootwright",
        "context": selection.get("context"),
        "host": selection.get("host"),
        "operation": selection.get("command"),
        "invocation": invocation,
        "selectionDigest": bootwright_bmc_claim_digest(selection),
        "selection": selection,
        "lease": lease,
    }
    normalized = bootwright_host_operation_guard_any(document)
    return document if normalized == document else {}


def bootwright_host_endpoint_claim_any(value):
    try:
        if isinstance(value, str):
            value = json.loads(value)
        if not isinstance(value, dict) or set(value) != {
            "apiVersion",
            "kind",
            "protocol",
            "port",
            "owner",
            "claimDigest",
        }:
            return {}
        owner = value.get("owner")
        if (
            value.get("apiVersion")
            != "bootwright.io/host-endpoint-claim/v1alpha1"
            or value.get("kind") != "host-endpoint"
            or value.get("protocol") not in ["tcp", "udp"]
            or isinstance(value.get("port"), bool)
            or not isinstance(value.get("port"), int)
            or value["port"] < 1
            or value["port"] > 65535
            or re.fullmatch(
                r"sha256:[a-f0-9]{64}", value.get("claimDigest", "")
            )
            is None
            or not isinstance(owner, dict)
            or set(owner) != {"manager", "kind", "name", "context", "host"}
            or owner.get("manager") != "bootwright"
        ):
            return {}
        for key in ["kind", "name", "context", "host"]:
            item = owner.get(key)
            if not isinstance(item, str) or re.fullmatch(r"[A-Za-z0-9_.-]+", item) is None:
                return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_endpoint_claim(
    protocol, port, logical_kind, logical_name, context, host, claim_digest
):
    document = {
        "apiVersion": "bootwright.io/host-endpoint-claim/v1alpha1",
        "kind": "host-endpoint",
        "protocol": protocol,
        "port": port,
        "owner": {
            "manager": "bootwright",
            "kind": logical_kind,
            "name": logical_name,
            "context": context,
            "host": host,
        },
        "claimDigest": claim_digest,
    }
    normalized = bootwright_host_endpoint_claim_any(document)
    return document if normalized == document else {}


def bootwright_host_endpoint_claim_set(value):
    try:
        if not isinstance(value, list):
            return {}
        claims = [bootwright_host_endpoint_claim_any(item) for item in value]
        if any(not item for item in claims):
            return {}
        keys = [(item["protocol"], item["port"]) for item in claims]
        if len(keys) != len(set(keys)) or keys != sorted(keys):
            return {}
        return {"valid": True, "claims": claims}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_endpoint_claim_groups(value):
    try:
        if not isinstance(value, list):
            return {}
        claims = [bootwright_host_endpoint_claim_any(item) for item in value]
        if any(not item for item in claims):
            return {}
        groups = {}
        for claim in claims:
            key = (claim["protocol"], claim["port"])
            groups.setdefault(key, [])
            if claim not in groups[key]:
                groups[key].append(claim)
        return {
            "valid": True,
            "groups": [
                {"protocol": key[0], "port": key[1], "claims": groups[key]}
                for key in sorted(groups)
            ],
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_bmc_endpoint_transition(active, pending):
    try:
        if not isinstance(active, dict) or not active:
            active = pending
        if not isinstance(pending, dict) or not pending:
            pending = active
        if not active or not pending:
            return {}
        for claim in [active, pending]:
            if (
                claim.get("kind") != "bmc-emulator"
                or claim.get("owner") != "bootwright"
                or not isinstance(claim.get("attributes"), dict)
            ):
                return {}

        def endpoint_claims(claim):
            digest = bootwright_bmc_claim_digest(claim)
            owner = {
                "kind": "bmc-emulator",
                "name": claim["name"],
                "context": claim["context"],
                "host": claim["host"],
            }
            return [
                bootwright_host_endpoint_claim(
                    "tcp",
                    int(claim["attributes"][attribute]),
                    owner["kind"],
                    owner["name"],
                    owner["context"],
                    owner["host"],
                    digest,
                )
                for attribute in ["redfishPort", "vMediaPort"]
            ]

        active_claims = endpoint_claims(active)
        pending_claims = endpoint_claims(pending)
        if any(not item for item in active_claims + pending_claims):
            return {}
        pending_by_key = {
            (item["protocol"], item["port"]): item for item in pending_claims
        }
        active_by_key = {
            (item["protocol"], item["port"]): item for item in active_claims
        }
        retained = dict(active_by_key)
        retained.update(pending_by_key)
        old_only = [
            item
            for key, item in active_by_key.items()
            if key not in pending_by_key
        ]
        return {
            "valid": True,
            "desiredClaims": sorted(
                pending_claims, key=lambda item: (item["protocol"], item["port"])
            ),
            "retainedClaims": [retained[key] for key in sorted(retained)],
            "oldOnlyCandidates": sorted(
                old_only, key=lambda item: (item["protocol"], item["port"])
            ),
            "allCandidates": active_claims + pending_claims,
            "allowedDigests": sorted(
                {
                    bootwright_bmc_claim_digest(active),
                    bootwright_bmc_claim_digest(pending),
                }
            ),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_endpoint_registry_any(value):
    try:
        if isinstance(value, str):
            value = json.loads(value)
        if not isinstance(value, dict) or set(value) != {
            "apiVersion",
            "kind",
            "owner",
            "claims",
        }:
            return {}
        if (
            value.get("apiVersion")
            != "bootwright.io/host-endpoint-registry/v1alpha1"
            or value.get("kind") != "host-endpoint-registry"
            or value.get("owner") != "bootwright"
            or not isinstance(value.get("claims"), list)
        ):
            return {}
        claims = [bootwright_host_endpoint_claim_any(item) for item in value["claims"]]
        keys = [(item.get("protocol"), item.get("port")) for item in claims]
        if (
            any(not item for item in claims)
            or len(keys) != len(set(keys))
            or keys != sorted(keys)
            or claims != value["claims"]
        ):
            return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def _bootwright_endpoint_owner_key(claim):
    owner = claim["owner"]
    return owner["kind"], owner["name"], owner["context"], owner["host"]


def bootwright_host_endpoint_registry_next(current, desired, allowed_digests):
    try:
        if current:
            registry = bootwright_host_endpoint_registry_any(current)
            if not registry:
                return {}
            existing = registry["claims"]
        else:
            existing = []
        desired_set = bootwright_host_endpoint_claim_set(desired)
        if (
            not desired_set
            or not desired_set["claims"]
            or not isinstance(allowed_digests, list)
            or not allowed_digests
            or any(
                re.fullmatch(r"sha256:[a-f0-9]{64}", item or "") is None
                for item in allowed_digests
            )
        ):
            return {}
        desired_claims = desired_set["claims"]
        owner_key = _bootwright_endpoint_owner_key(desired_claims[0])
        if any(_bootwright_endpoint_owner_key(item) != owner_key for item in desired_claims):
            return {}
        retained = []
        for claim in existing:
            if _bootwright_endpoint_owner_key(claim) == owner_key:
                if claim["claimDigest"] not in allowed_digests:
                    return {}
                continue
            retained.append(claim)
        retained_by_key = {
            (item["protocol"], item["port"]): item for item in retained
        }
        conflicts = []
        for claim in desired_claims:
            key = (claim["protocol"], claim["port"])
            if key in retained_by_key:
                conflicts.append(
                    {
                        "protocol": key[0],
                        "port": key[1],
                        "owner": retained_by_key[key]["owner"],
                    }
                )
        claims = sorted(
            retained + desired_claims,
            key=lambda item: (item["protocol"], item["port"]),
        )
        document = {
            "apiVersion": "bootwright.io/host-endpoint-registry/v1alpha1",
            "kind": "host-endpoint-registry",
            "owner": "bootwright",
            "claims": claims,
        }
        return {
            "valid": True,
            "conflicts": conflicts,
            "registry": document if not conflicts else {},
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_endpoint_registry_remove(current, candidates):
    try:
        if not current:
            return {"valid": True, "registry": {}}
        registry = bootwright_host_endpoint_registry_any(current)
        groups = bootwright_host_endpoint_claim_groups(candidates)
        if not registry or not groups:
            return {}
        candidate_by_key = {
            (item["protocol"], item["port"]): item["claims"]
            for item in groups["groups"]
        }
        retained = []
        for claim in registry["claims"]:
            key = (claim["protocol"], claim["port"])
            if key not in candidate_by_key:
                retained.append(claim)
            elif claim not in candidate_by_key[key]:
                return {}
        document = {
            "apiVersion": registry["apiVersion"],
            "kind": registry["kind"],
            "owner": registry["owner"],
            "claims": retained,
        }
        return {
            "valid": True,
            "registry": document if retained else {},
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_bmc_record_claim_identity(
    value, expected_context, expected_provider, expected_host, provider_state_dir
):
    if not isinstance(value, dict):
        return {}
    if (
        value.get("apiVersion") != "bootwright.io/ownership/v1alpha1"
        or value.get("kind") != "bmc-emulator"
        or value.get("name") != expected_provider
        or value.get("owner") != "bootwright"
        or value.get("context") != expected_context
        or value.get("host") != expected_host
        or value.get("provider") != expected_provider
        or value.get("role", "") not in ["", "owner"]
    ):
        return {}
    claim = {
        "apiVersion": "bootwright.io/bmc-service-claim/v1alpha1",
        "kind": "bmc-emulator",
        "name": expected_provider,
        "owner": "bootwright",
        "context": expected_context,
        "host": expected_host,
        "provider": expected_provider,
        "paths": value.get("paths"),
        "attributes": value.get("attributes"),
    }
    return bootwright_bmc_claim_identity(
        json.dumps(claim),
        expected_context,
        expected_provider,
        expected_host,
        provider_state_dir,
    )


def _bootwright_bmc_port(value):
    if not isinstance(value, str):
        return 0
    if re.fullmatch(r"[1-9][0-9]{0,4}", value) is None:
        return 0
    port = int(value)
    return port if port <= 65535 else 0


def bootwright_systemd_show_identity(value, expected_fragment_path):
    if (
        not isinstance(value, str)
        or not isinstance(expected_fragment_path, str)
        or not expected_fragment_path.startswith("/")
        or posixpath.normpath(expected_fragment_path) != expected_fragment_path
    ):
        return {}
    fields = {}
    for line in value.splitlines():
        if "=" not in line:
            return {}
        key, item = line.split("=", 1)
        if key not in ["LoadState", "FragmentPath"] or key in fields:
            return {}
        fields[key] = item
    if set(fields) != {"LoadState", "FragmentPath"}:
        return {}
    if fields == {"LoadState": "loaded", "FragmentPath": expected_fragment_path}:
        return {
            "present": True,
            "loadState": "loaded",
            "fragmentPath": expected_fragment_path,
        }
    if fields == {"LoadState": "not-found", "FragmentPath": ""}:
        return {"present": False, "loadState": "not-found", "fragmentPath": ""}
    return {}


def bootwright_mount_targets(value, roots):
    if (
        not isinstance(value, str)
        or not isinstance(roots, list)
        or not roots
        or any(
            not isinstance(root, str)
            or not root.startswith("/")
            or root == "/"
            or posixpath.normpath(root) != root
            for root in roots
        )
        or len(set(roots)) != len(roots)
    ):
        return {}
    targets = []
    for line in value.splitlines():
        if (
            not line
            or line.strip() != line
            or not line.startswith("/")
            or posixpath.normpath(line) != line
            or any(character.isspace() for character in line)
            or line in targets
            or not any(line == root or line.startswith(root + "/") for root in roots)
        ):
            return {}
        targets.append(line)
    return {"valid": True, "targets": sorted(targets)}


def bootwright_declared_managed_os_paths(value, root, stage_template):
    if (
        not isinstance(value, list)
        or not isinstance(root, str)
        or not root.startswith("/")
        or not isinstance(stage_template, str)
    ):
        return False
    if not value or any(
        not isinstance(path, str) or not path.startswith("/") for path in value
    ):
        return False
    if len(value) != len(set(value)):
        return False
    patterns = [re.compile(re.escape(root))]
    if stage_template:
        token = "__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__"
        expression = re.escape(stage_template)
        if token in stage_template:
            expression = expression.replace(re.escape(token), r"[A-Za-z0-9_-]+")
        patterns.append(re.compile(expression))
    return all(any(pattern.fullmatch(path) for pattern in patterns) for path in value)


def _bootwright_infra_port_list(value, protocol, multiple):
    if not isinstance(value, str) or not value:
        return None
    entries = value.split(",") if multiple else [value]
    ports = []
    for entry in entries:
        match = re.fullmatch(r"([1-9][0-9]{0,4})/([a-z]+)", entry)
        if match is None or match.group(2) != protocol:
            return None
        number = int(match.group(1))
        if number > 65535:
            return None
        ports.append(f"{number}/{protocol}")
    if len(ports) != len(set(ports)):
        return None
    if ports != sorted(ports, key=lambda item: int(item.split("/", 1)[0])):
        return None
    return ports


def _bootwright_infra_common(record, managed_services_dir):
    if (
        not isinstance(record, dict)
        or not isinstance(managed_services_dir, str)
        or not managed_services_dir.startswith("/")
        or managed_services_dir == "/"
        or posixpath.normpath(managed_services_dir) != managed_services_dir
        or set(record)
        != {
            "apiVersion",
            "kind",
            "name",
            "owner",
            "context",
            "host",
            "hostFacts",
            "provider",
            "paths",
            "labels",
            "attributes",
            "updatedAt",
        }
        or record.get("apiVersion") != "bootwright.io/ownership/v1alpha1"
        or record.get("kind") != "infra-component"
        or record.get("owner") != "bootwright"
    ):
        return None
    provider = record.get("provider")
    labels = record.get("labels")
    attributes = record.get("attributes")
    paths = record.get("paths")
    host_facts = record.get("hostFacts")
    if (
        not isinstance(provider, str)
        or re.fullmatch(r"[A-Za-z0-9_.-]+", provider or "") is None
        or not isinstance(labels, dict)
        or not isinstance(attributes, dict)
        or not isinstance(paths, list)
        or not isinstance(host_facts, dict)
        or set(host_facts)
        != {
            "bootwright_host_name",
            "ansible_host",
            "ansible_user",
            "ansible_connection",
            "ansible_ssh_common_args",
        }
        or any(not isinstance(item, str) for item in host_facts.values())
        or not isinstance(record.get("updatedAt"), str)
        or not isinstance(record.get("context"), str)
        or not isinstance(record.get("host"), str)
    ):
        return None
    component_kind = labels.get("bootwright.kind")
    component_name = labels.get("bootwright.name")
    if (
        set(labels)
        != {"bootwright.kind", "bootwright.provider", "bootwright.name"}
        or labels.get("bootwright.provider") != provider
        or not isinstance(component_name, str)
        or re.fullmatch(r"[A-Za-z0-9_.-]+", component_name or "") is None
        or record.get("name") != f"{provider}-{component_name}"
        or attributes.get("componentKind") != component_kind
    ):
        return None
    phase = attributes.get("destroyPhase", "")
    if phase not in ["", "external-cleanup-complete"]:
        return None
    return (
        provider,
        component_name,
        component_kind,
        attributes,
        paths,
        phase,
        f"{managed_services_dir}/{component_name}",
    )


def bootwright_infra_component_record_composite(record, managed_services_dir):
    try:
        common = _bootwright_infra_common(record, managed_services_dir)
        if common is None:
            return {}
        provider, name, kind, attrs, paths, phase, state_root = common
        base_keys = {"componentKind"}
        phase_keys = {"destroyPhase"} if phase else set()
        role = ""
        component = {"providerName": provider, "name": name}
        ports = []
        config_path = ""
        if kind == "artifacts":
            ports = _bootwright_infra_port_list(attrs.get("listenerPorts"), "tcp", True)
            if (
                set(attrs) != base_keys | {"container", "listenerPorts"} | phase_keys
                or attrs.get("container") != f"bootwright-artifacts-{provider}-{name}"
                or ports is None
                or paths != [state_root]
            ):
                return {}
            role = "bootwright.core.infra_component_artifact_server_http"
            component["listeners"] = [
                {"protocol": "http", "port": int(item.split("/", 1)[0])}
                for item in ports
            ]
        elif kind == "load-balancer":
            ports = _bootwright_infra_port_list(attrs.get("frontendPorts"), "tcp", True)
            if (
                set(attrs) != base_keys | {"container", "frontendPorts"} | phase_keys
                or attrs.get("container") != f"bootwright-haproxy-{provider}-{name}"
                or ports is None
                or paths != [state_root, "/etc/sysctl.d/99-bootwright-haproxy.conf"]
            ):
                return {}
            role = "bootwright.core.infra_component_load_balancer_haproxy"
            component["frontends"] = []
        elif kind == "nameResolution":
            tcp = _bootwright_infra_port_list(attrs.get("port"), "tcp", False)
            udp = _bootwright_infra_port_list(attrs.get("udpPort"), "udp", False)
            if (
                set(attrs) != base_keys | {"container", "port", "udpPort"} | phase_keys
                or attrs.get("container") != f"bootwright-dnsmasq-{provider}-{name}"
                or tcp != ["53/tcp"]
                or udp != ["53/udp"]
                or paths != [state_root]
            ):
                return {}
            role = "bootwright.core.infra_component_name_resolution_dnsmasq"
            ports = tcp + udp
        elif kind == "ntp":
            ports = _bootwright_infra_port_list(attrs.get("port"), "udp", False)
            include_dir = {"chrony": "/etc/chrony/conf.d", "chronyd": "/etc/chrony.d"}.get(attrs.get("service"))
            config_path = f"{include_dir}/bootwright-{provider}-{name}.conf" if include_dir else ""
            if (
                set(attrs) != base_keys | {"service", "port"} | phase_keys
                or ports is None
                or not config_path
                or paths != [state_root, config_path]
            ):
                return {}
            role = "bootwright.core.infra_component_ntp_chrony"
            component["port"] = int(ports[0].split("/", 1)[0])
        elif kind == "proxy":
            ports = _bootwright_infra_port_list(attrs.get("port"), "tcp", False)
            if (
                set(attrs) != base_keys | {"container", "port"} | phase_keys
                or attrs.get("container") != f"bootwright-squid-{provider}-{name}"
                or ports is None
                or paths != [state_root]
            ):
                return {}
            role = "bootwright.core.infra_component_proxy_squid"
            component["port"] = int(ports[0].split("/", 1)[0])
        elif kind == "registry":
            ports = _bootwright_infra_port_list(attrs.get("port"), "tcp", False)
            anchor = attrs.get("trustAnchor")
            checksum = attrs.get("trustBundleSHA256")
            expected_anchor = f"/etc/pki/ca-trust/source/anchors/bootwright-mirror-{provider}-{name}.crt"
            if (
                set(attrs)
                != base_keys
                | {"container", "port", "trustAnchor", "trustBundleSHA256"}
                | phase_keys
                or attrs.get("container") != f"bootwright-mirror-registry-{provider}-{name}"
                or ports is None
                or not isinstance(anchor, str)
                or not isinstance(checksum, str)
                or ((anchor, checksum) != ("", "") and (anchor != expected_anchor or re.fullmatch(r"[a-f0-9]{64}", checksum) is None))
                or paths != [state_root]
            ):
                return {}
            role = "bootwright.core.infra_component_registry_mirror"
            component["port"] = int(ports[0].split("/", 1)[0])
        else:
            return {}
        return {
            "role": role,
            "kind": kind,
            "name": record["name"],
            "component": component,
            "stateRoot": state_root,
            "configPath": config_path,
            "ports": ports,
            "phaseComplete": phase == "external-cleanup-complete",
            "record": record,
            "transition": {
                "kind": kind,
                "provider": provider,
                "name": name,
                "paths": paths,
                "labels": record["labels"],
                "attributes": attrs,
            },
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_record_digests(record, managed_services_dir):
    try:
        composite = bootwright_infra_component_record_composite(
            record, managed_services_dir
        )
        if not composite:
            return {}
        side = json.loads(json.dumps(composite["transition"]))
        side["attributes"].pop("destroyPhase", None)
        digest = bootwright_bmc_claim_digest(side)
        if not digest:
            return {}
        return {
            "valid": True,
            "selectionDigest": digest,
            "claimDigest": digest,
            "side": side,
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_selection_digest(component, expected_host):
    try:
        if not isinstance(component, dict) or not isinstance(expected_host, str):
            return ""
        normalized = json.loads(json.dumps(component))
        normalized.pop("clusterName", None)
        normalized.pop("consumingClusters", None)
        kind = normalized.get("kind")
        if (
            kind
            not in {
                "artifactServer",
                "loadBalancer",
                "nameResolution",
                "ntp",
                "proxy",
                "registry",
            }
            or normalized.get("machineRef") != expected_host
            or any(
                re.fullmatch(r"[A-Za-z0-9_.-]+", normalized.get(key, "")) is None
                for key in ["providerName", "name", "machineRef"]
            )
            or not isinstance(normalized.get("applyRole"), str)
            or not normalized["applyRole"]
            or not isinstance(normalized.get("destroyRole"), str)
            or not normalized["destroyRole"]
        ):
            return ""
        document = {
            "apiVersion": "bootwright.io/infra-component-service-selection/v1alpha1",
            "kind": kind,
            "host": expected_host,
            "component": normalized,
        }
        return bootwright_bmc_claim_digest(document)
    except (AttributeError, KeyError, TypeError, ValueError):
        return ""


def bootwright_infra_component_transition_composite(value, managed_services_dir):
    try:
        if not isinstance(value, dict) or set(value) != {
            "kind",
            "provider",
            "name",
            "paths",
            "labels",
            "attributes",
        }:
            return {}
        fake = {
            "apiVersion": "bootwright.io/ownership/v1alpha1",
            "kind": "infra-component",
            "name": f"{value['provider']}-{value['name']}",
            "owner": "bootwright",
            "context": "context",
            "host": "host",
            "hostFacts": {
                "bootwright_host_name": "host",
                "ansible_host": "",
                "ansible_user": "",
                "ansible_connection": "",
                "ansible_ssh_common_args": "",
            },
            "provider": value["provider"],
            "paths": value["paths"],
            "labels": value["labels"],
            "attributes": value["attributes"],
            "updatedAt": "",
        }
        result = bootwright_infra_component_record_composite(
            fake, managed_services_dir
        )
        if not result or result["kind"] != value["kind"]:
            return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_transition_claim(
    value, expected_context, expected_host, managed_services_dir
):
    try:
        if isinstance(value, str):
            value = json.loads(value)
        if (
            not isinstance(value, dict)
            or set(value)
            != {
                "apiVersion",
                "kind",
                "name",
                "owner",
                "context",
                "host",
                "previous",
                "desired",
            }
            or value.get("apiVersion")
            != "bootwright.io/infra-component-transition/v1alpha1"
            or value.get("kind") != "infra-component"
            or value.get("owner") != "bootwright"
            or value.get("context") != expected_context
            or value.get("host") != expected_host
            or not isinstance(value.get("previous"), list)
            or len(value["previous"]) > 64
        ):
            return {}
        desired = bootwright_infra_component_transition_composite(
            value.get("desired"), managed_services_dir
        )
        previous = [
            bootwright_infra_component_transition_composite(
                item, managed_services_dir
            )
            for item in value["previous"]
        ]
        if (
            not desired
            or any(not item for item in previous)
            or value.get("name")
            != f"{desired['provider']}-{desired['name']}"
            or any(
                (item["kind"], item["provider"], item["name"])
                != (desired["kind"], desired["provider"], desired["name"])
                for item in previous
            )
            or len(
                {
                    json.dumps(item, sort_keys=True, separators=(",", ":"))
                    for item in previous
                }
            )
            != len(previous)
        ):
            return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_transition_next(
    existing, previous_record, desired, expected_context, expected_host, managed_services_dir
):
    try:
        normalized_desired = bootwright_infra_component_transition_composite(
            desired, managed_services_dir
        )
        if not normalized_desired:
            return {}
        if existing:
            normalized_existing = bootwright_infra_component_transition_claim(
                existing, expected_context, expected_host, managed_services_dir
            )
            if not normalized_existing:
                return {}
            candidates = normalized_existing["previous"] + [
                normalized_existing["desired"]
            ]
        else:
            candidates = []
        if previous_record:
            normalized_previous = bootwright_infra_component_transition_composite(
                previous_record, managed_services_dir
            )
            if not normalized_previous:
                return {}
            candidates.append(normalized_previous)
        previous = []
        seen = set()
        desired_key = json.dumps(
            normalized_desired, sort_keys=True, separators=(",", ":")
        )
        for item in candidates:
            key = json.dumps(item, sort_keys=True, separators=(",", ":"))
            if key == desired_key or key in seen:
                continue
            seen.add(key)
            previous.append(item)
        claim = {
            "apiVersion": "bootwright.io/infra-component-transition/v1alpha1",
            "kind": "infra-component",
            "name": f"{normalized_desired['provider']}-{normalized_desired['name']}",
            "owner": "bootwright",
            "context": expected_context,
            "host": expected_host,
            "previous": previous,
            "desired": normalized_desired,
        }
        return bootwright_infra_component_transition_claim(
            claim, expected_context, expected_host, managed_services_dir
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def _bootwright_transition_ports(value):
    kind = value["kind"]
    attrs = value["attributes"]
    if kind == "artifacts":
        return _bootwright_infra_port_list(attrs["listenerPorts"], "tcp", True)
    if kind == "load-balancer":
        return _bootwright_infra_port_list(attrs["frontendPorts"], "tcp", True)
    if kind == "nameResolution":
        return [attrs["port"], attrs["udpPort"]]
    return [attrs["port"]]


def bootwright_infra_component_transition_ports(claim):
    try:
        if not isinstance(claim, dict):
            return {}
        composites = claim.get("previous", []) + [claim.get("desired")]
        if any(not isinstance(item, dict) or not item for item in composites):
            return {}
        ports = []
        for item in composites:
            ports.extend(_bootwright_transition_ports(item))
        return {"valid": True, "ports": sorted(set(ports))}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_transition_record(record, managed_services_dir):
    try:
        if isinstance(record, str):
            record = json.loads(record)
        if (
            not isinstance(record, dict)
            or not isinstance(managed_services_dir, str)
            or not managed_services_dir.startswith("/")
            or managed_services_dir == "/"
            or posixpath.normpath(managed_services_dir) != managed_services_dir
            or set(record)
            != {
                "apiVersion",
                "kind",
                "name",
                "owner",
                "context",
                "host",
                "hostFacts",
                "provider",
                "paths",
                "labels",
                "attributes",
                "updatedAt",
            }
            or record.get("apiVersion") != "bootwright.io/ownership/v1alpha1"
            or record.get("kind") != "infra-component-transition"
            or record.get("owner") != "bootwright"
        ):
            return {}
        provider = record.get("provider")
        labels = record.get("labels")
        attributes = record.get("attributes")
        host_facts = record.get("hostFacts")
        name = labels.get("bootwright.name") if isinstance(labels, dict) else None
        if (
            not isinstance(provider, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", provider or "") is None
            or not isinstance(name, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", name or "") is None
            or record.get("name") != f"{provider}-{name}"
            or set(labels)
            != {"bootwright.kind", "bootwright.provider", "bootwright.name"}
            or labels.get("bootwright.provider") != provider
            or not isinstance(attributes, dict)
            or set(attributes) != {"componentKind", "claimPath"}
            or attributes.get("componentKind") != labels.get("bootwright.kind")
            or labels.get("bootwright.kind")
            not in {"artifacts", "load-balancer", "nameResolution", "ntp", "proxy", "registry"}
            or not isinstance(host_facts, dict)
            or set(host_facts)
            != {
                "bootwright_host_name",
                "ansible_host",
                "ansible_user",
                "ansible_connection",
                "ansible_ssh_common_args",
            }
            or any(not isinstance(item, str) for item in host_facts.values())
            or not isinstance(record.get("context"), str)
            or not isinstance(record.get("host"), str)
            or not isinstance(record.get("updatedAt"), str)
        ):
            return {}
        if (
            not isinstance(attributes.get("claimPath"), str)
            or not attributes["claimPath"].startswith("/")
            or posixpath.normpath(attributes["claimPath"])
            != attributes["claimPath"]
            or record.get("paths") != [attributes["claimPath"]]
        ):
            return {}
        return {
            "name": record["name"],
            "provider": provider,
            "componentName": name,
            "kind": labels["bootwright.kind"],
            "claimPath": attributes["claimPath"],
            "record": record,
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_transition_record_digest(record, managed_services_dir):
    try:
        normalized = bootwright_infra_component_transition_record(
            record, managed_services_dir
        )
        if not normalized:
            return ""
        source = normalized["record"]
        document = {
            "apiVersion": "bootwright.io/infra-component-transition-selection/v1alpha1",
            "kind": normalized["kind"],
            "name": source["name"],
            "context": source["context"],
            "host": source["host"],
            "provider": source["provider"],
            "paths": source["paths"],
            "labels": source["labels"],
            "attributes": source["attributes"],
        }
        return bootwright_bmc_claim_digest(document)
    except (AttributeError, KeyError, TypeError, ValueError):
        return ""


def bootwright_infra_component_transition_dispatch(
    claim, expected_context, expected_host, managed_services_dir
):
    try:
        normalized = bootwright_infra_component_transition_claim(
            claim, expected_context, expected_host, managed_services_dir
        )
        if not normalized:
            return {}
        desired = normalized["desired"]
        record = {
            "apiVersion": "bootwright.io/ownership/v1alpha1",
            "kind": "infra-component",
            "name": normalized["name"],
            "owner": "bootwright",
            "context": expected_context,
            "host": expected_host,
            "hostFacts": {
                "bootwright_host_name": expected_host,
                "ansible_host": "",
                "ansible_user": "",
                "ansible_connection": "",
                "ansible_ssh_common_args": "",
            },
            "provider": desired["provider"],
            "paths": desired["paths"],
            "labels": desired["labels"],
            "attributes": desired["attributes"],
            "updatedAt": "transition",
        }
        composite = bootwright_infra_component_record_composite(
            record, managed_services_dir
        )
        if not composite:
            return {}
        composite["record"] = {}
        composite["claim"] = normalized
        return composite
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_claim_digest(provider, name):
    try:
        if (
            not isinstance(provider, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", provider or "") is None
            or not isinstance(name, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", name or "") is None
        ):
            return ""
        identity = json.dumps([provider, name], separators=(",", ":"))
        return hashlib.sha256(identity.encode("utf-8")).hexdigest()
    except (AttributeError, TypeError, ValueError):
        return ""


def _bootwright_infra_composite_key(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def _bootwright_infra_exact_side_set(values, managed_services_dir, allow_empty=False):
    if (
        not isinstance(values, list)
        or len(values) > 65
        or (not allow_empty and not values)
    ):
        return None
    normalized = [
        bootwright_infra_component_transition_composite(
            item, managed_services_dir
        )
        for item in values
    ]
    if any(not item for item in normalized):
        return None
    keys = [_bootwright_infra_composite_key(item) for item in normalized]
    if len(keys) != len(set(keys)) or keys != sorted(keys):
        return None
    if normalized and any(
        (item["kind"], item["provider"], item["name"])
        != (
            normalized[0]["kind"],
            normalized[0]["provider"],
            normalized[0]["name"],
        )
        for item in normalized
    ):
        return None
    return normalized


def _bootwright_infra_sorted_side_set(values):
    unique = {}
    for item in values:
        unique[_bootwright_infra_composite_key(item)] = item
    return [unique[key] for key in sorted(unique)]


def bootwright_infra_component_global_claim(
    value, expected_context, expected_host, managed_services_dir
):
    try:
        if isinstance(value, str):
            value = json.loads(value)
        common = {
            "apiVersion",
            "kind",
            "name",
            "owner",
            "context",
            "host",
            "state",
        }
        if (
            not isinstance(value, dict)
            or value.get("apiVersion")
            != "bootwright.io/infra-component-service-claim/v1alpha1"
            or value.get("kind") != "infra-component"
            or value.get("owner") != "bootwright"
            or value.get("context") != expected_context
            or value.get("host") != expected_host
            or value.get("state") not in {"steady", "applying", "destroying"}
        ):
            return {}
        state = value["state"]
        if state == "steady":
            if set(value) != common | {"active"}:
                return {}
            sides = _bootwright_infra_exact_side_set(
                [value.get("active")], managed_services_dir
            )
        elif state == "applying":
            if set(value) != common | {"previous", "desired"}:
                return {}
            previous = _bootwright_infra_exact_side_set(
                value.get("previous"), managed_services_dir, allow_empty=True
            )
            desired = bootwright_infra_component_transition_composite(
                value.get("desired"), managed_services_dir
            )
            if previous is None or not desired:
                return {}
            sides = _bootwright_infra_sorted_side_set(previous + [desired])
            if (
                previous
                != [item for item in sides if item != desired]
                or len(sides) != len(previous) + 1
            ):
                return {}
        else:
            if set(value) != common | {"targets"}:
                return {}
            sides = _bootwright_infra_exact_side_set(
                value.get("targets"), managed_services_dir
            )
        if not sides:
            return {}
        first = sides[0]
        if value.get("name") != f"{first['provider']}-{first['name']}":
            return {}
        return value
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_apply_next(
    existing,
    previous_record,
    local_transition,
    desired,
    expected_context,
    expected_host,
    managed_services_dir,
):
    try:
        normalized_desired = bootwright_infra_component_transition_composite(
            desired, managed_services_dir
        )
        if not normalized_desired:
            return {}
        candidates = []
        normalized_existing = {}
        if existing:
            normalized_existing = bootwright_infra_component_global_claim(
                existing, expected_context, expected_host, managed_services_dir
            )
            if not normalized_existing or normalized_existing["state"] == "destroying":
                return {}
            if normalized_existing["state"] == "steady":
                candidates = [normalized_existing["active"]]
            else:
                if normalized_existing["desired"] != normalized_desired:
                    return {}
                candidates = normalized_existing["previous"] + [
                    normalized_existing["desired"]
                ]
        normalized_local = {}
        if local_transition:
            normalized_local = bootwright_infra_component_transition_claim(
                local_transition,
                expected_context,
                expected_host,
                managed_services_dir,
            )
            if not normalized_local:
                return {}
            local_sides = normalized_local["previous"] + [normalized_local["desired"]]
            if normalized_existing and any(item not in candidates for item in local_sides):
                return {}
            candidates.extend(local_sides)
        normalized_previous = {}
        if previous_record:
            normalized_previous = bootwright_infra_component_transition_composite(
                previous_record, managed_services_dir
            )
            if not normalized_previous:
                return {}
            if normalized_existing and normalized_previous not in candidates:
                return {}
            candidates.append(normalized_previous)
        expected_identity = (
            normalized_desired["kind"],
            normalized_desired["provider"],
            normalized_desired["name"],
        )
        if any(
            (item["kind"], item["provider"], item["name"]) != expected_identity
            for item in candidates
        ):
            return {}
        previous = _bootwright_infra_sorted_side_set(
            [item for item in candidates if item != normalized_desired]
        )
        claim = {
            "apiVersion": "bootwright.io/infra-component-service-claim/v1alpha1",
            "kind": "infra-component",
            "name": f"{normalized_desired['provider']}-{normalized_desired['name']}",
            "owner": "bootwright",
            "context": expected_context,
            "host": expected_host,
            "state": "applying",
            "previous": previous,
            "desired": normalized_desired,
        }
        return bootwright_infra_component_global_claim(
            claim, expected_context, expected_host, managed_services_dir
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_destroy_next(
    existing,
    previous_record,
    local_transition,
    expected_kind,
    expected_provider,
    expected_name,
    expected_context,
    expected_host,
    managed_services_dir,
):
    try:
        candidates = []
        normalized_existing = {}
        if existing:
            normalized_existing = bootwright_infra_component_global_claim(
                existing, expected_context, expected_host, managed_services_dir
            )
            if not normalized_existing:
                return {}
            if normalized_existing["state"] == "steady":
                candidates = [normalized_existing["active"]]
            elif normalized_existing["state"] == "applying":
                candidates = normalized_existing["previous"] + [
                    normalized_existing["desired"]
                ]
            else:
                candidates = normalized_existing["targets"]
        local_sides = []
        if local_transition:
            normalized_local = bootwright_infra_component_transition_claim(
                local_transition,
                expected_context,
                expected_host,
                managed_services_dir,
            )
            if not normalized_local:
                return {}
            local_sides = normalized_local["previous"] + [normalized_local["desired"]]
        owner_side = []
        if previous_record:
            normalized_previous = bootwright_infra_component_transition_composite(
                previous_record, managed_services_dir
            )
            if not normalized_previous:
                return {}
            owner_side = [normalized_previous]
        controller_sides = local_sides + owner_side
        if normalized_existing and any(item not in candidates for item in controller_sides):
            return {}
        candidates.extend(controller_sides)
        targets = _bootwright_infra_sorted_side_set(candidates)
        expected_identity = (expected_kind, expected_provider, expected_name)
        if not targets or any(
            (item["kind"], item["provider"], item["name"]) != expected_identity
            for item in targets
        ):
            return {}
        claim = {
            "apiVersion": "bootwright.io/infra-component-service-claim/v1alpha1",
            "kind": "infra-component",
            "name": f"{expected_provider}-{expected_name}",
            "owner": "bootwright",
            "context": expected_context,
            "host": expected_host,
            "state": "destroying",
            "targets": targets,
        }
        return bootwright_infra_component_global_claim(
            claim, expected_context, expected_host, managed_services_dir
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_steady(
    applying, expected_context, expected_host, managed_services_dir
):
    try:
        normalized = bootwright_infra_component_global_claim(
            applying, expected_context, expected_host, managed_services_dir
        )
        if not normalized or normalized["state"] != "applying":
            return {}
        claim = {
            "apiVersion": normalized["apiVersion"],
            "kind": normalized["kind"],
            "name": normalized["name"],
            "owner": normalized["owner"],
            "context": normalized["context"],
            "host": normalized["host"],
            "state": "steady",
            "active": normalized["desired"],
        }
        return bootwright_infra_component_global_claim(
            claim, expected_context, expected_host, managed_services_dir
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_ports(claim):
    try:
        if not isinstance(claim, dict):
            return {}
        if claim.get("state") == "steady":
            composites = [claim.get("active")]
        elif claim.get("state") == "applying":
            composites = claim.get("previous", []) + [claim.get("desired")]
        elif claim.get("state") == "destroying":
            composites = claim.get("targets", [])
        else:
            return {}
        if any(not isinstance(item, dict) or not item for item in composites):
            return {}
        ports = []
        for item in composites:
            ports.extend(_bootwright_transition_ports(item))
        return {"valid": True, "ports": sorted(set(ports))}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_claim_any(value, managed_services_dir):
    try:
        if isinstance(value, str):
            document = json.loads(value)
        else:
            document = value
        if not isinstance(document, dict):
            return {}
        context = document.get("context")
        host = document.get("host")
        if (
            not isinstance(context, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", context or "") is None
            or not isinstance(host, str)
            or re.fullmatch(r"[A-Za-z0-9_.-]+", host or "") is None
        ):
            return {}
        return bootwright_infra_component_global_claim(
            document, context, host, managed_services_dir
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_claim_key(claim, managed_services_dir):
    try:
        normalized = bootwright_infra_component_global_claim_any(
            claim, managed_services_dir
        )
        if not normalized:
            return ""
        side = _bootwright_infra_global_sides(normalized)[0]
        return bootwright_infra_component_claim_digest(
            side["provider"], side["name"]
        )
    except (AttributeError, KeyError, TypeError, ValueError):
        return ""


def _bootwright_infra_global_sides(claim):
    if claim["state"] == "steady":
        return [claim["active"]]
    if claim["state"] == "applying":
        return claim["previous"] + [claim["desired"]]
    return claim["targets"]


def bootwright_infra_component_global_claim_digests(claim, managed_services_dir):
    try:
        normalized = bootwright_infra_component_global_claim_any(
            claim, managed_services_dir
        )
        if not normalized:
            return {}
        digests = sorted(
            {bootwright_bmc_claim_digest(side) for side in _bootwright_infra_global_sides(normalized)}
        )
        if any(not digest for digest in digests):
            return {}
        return {"valid": True, "digests": digests}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def _bootwright_infra_consequences(composite):
    attributes = composite["attributes"]
    consequences = {f"path:{path}" for path in composite["paths"]}
    for port in _bootwright_transition_ports(composite):
        consequences.add(f"endpoint:{port}")
    container = attributes.get("container")
    if container:
        consequences.add(f"container:{container}")
    if composite["kind"] == "ntp":
        consequences.add("service:host-ntp")
    anchor = attributes.get("trustAnchor")
    if anchor:
        consequences.add(f"trust:{anchor}")
    return consequences


def _bootwright_bmc_claim_any(value):
    try:
        if isinstance(value, str):
            document = json.loads(value)
        else:
            document = value
        if not isinstance(document, dict):
            return {}
        if document.get("apiVersion") == "bootwright.io/bmc-service-transition/v1alpha1":
            sample = document.get("pending")
        else:
            sample = document
        if not isinstance(sample, dict) or not isinstance(sample.get("attributes"), dict):
            return {}
        context = sample.get("context")
        provider = sample.get("provider")
        host = sample.get("host")
        state_root = sample["attributes"].get("stateRoot")
        if (
            not isinstance(state_root, str)
            or posixpath.basename(state_root) != provider
            or posixpath.basename(posixpath.dirname(state_root)) != "bmc"
        ):
            return {}
        provider_state_dir = posixpath.dirname(posixpath.dirname(state_root))
        if document.get("apiVersion") == "bootwright.io/bmc-service-transition/v1alpha1":
            normalized = bootwright_bmc_transition_identity(
                json.dumps(document), context, provider, host, provider_state_dir
            )
            claims = [normalized.get("active"), normalized.get("pending")] if normalized else []
        else:
            normalized = bootwright_bmc_claim_identity(
                json.dumps(document), context, provider, host, provider_state_dir
            )
            claims = [normalized] if normalized else []
        if not normalized or any(not item for item in claims):
            return {}
        resources = set()
        for claim in claims:
            resources.update(f"path:{item}" for item in claim["paths"])
            resources.add(
                f"pool:{claim['attributes']['libvirtURI']}|{claim['attributes']['pool']}"
            )
            resources.add(f"endpoint:{claim['attributes']['redfishPort']}/tcp")
            resources.add(f"endpoint:{claim['attributes']['vMediaPort']}/tcp")
        return {
            "family": "bmc-emulator",
            "kind": "bmc-emulator",
            "name": provider,
            "context": context,
            "host": host,
            "resources": sorted(resources),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def _bootwright_infra_claim_any_unbound(value):
    try:
        if isinstance(value, str):
            document = json.loads(value)
        else:
            document = value
        if not isinstance(document, dict):
            return {}
        if document.get("apiVersion") == "bootwright.io/infra-component-transition/v1alpha1":
            sides = document.get("previous", []) + [document.get("desired")]
        elif document.get("apiVersion") == "bootwright.io/infra-component-service-claim/v1alpha1":
            if document.get("state") == "steady":
                sides = [document.get("active")]
            elif document.get("state") == "applying":
                sides = document.get("previous", []) + [document.get("desired")]
            elif document.get("state") == "destroying":
                sides = document.get("targets", [])
            else:
                return {}
        else:
            return {}
        if not sides or not isinstance(sides[0], dict) or not isinstance(sides[0].get("paths"), list) or not sides[0]["paths"]:
            return {}
        managed_services_dir = posixpath.dirname(sides[0]["paths"][0])
        context = document.get("context")
        host = document.get("host")
        if document.get("apiVersion") == "bootwright.io/infra-component-transition/v1alpha1":
            normalized = bootwright_infra_component_transition_claim(
                document, context, host, managed_services_dir
            )
            normalized_sides = normalized.get("previous", []) + [normalized.get("desired")] if normalized else []
        else:
            normalized = bootwright_infra_component_global_claim(
                document, context, host, managed_services_dir
            )
            normalized_sides = _bootwright_infra_global_sides(normalized) if normalized else []
        if not normalized or any(not item for item in normalized_sides):
            return {}
        resources = set()
        for side in normalized_sides:
            resources.update(_bootwright_infra_consequences(side))
        first = normalized_sides[0]
        return {
            "family": "infra-component",
            "kind": "infra-component",
            "name": f"{first['provider']}-{first['name']}",
            "context": context,
            "host": host,
            "componentKind": first["kind"],
            "resources": sorted(resources),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_shared_service_prepublication_conflicts(
    candidate_family,
    candidate,
    bmc_documents,
    infra_documents,
    endpoint_registry,
):
    try:
        classifiers = {
            "bmc-emulator": _bootwright_bmc_claim_any,
            "infra-component": _bootwright_infra_claim_any_unbound,
        }
        classifier = classifiers.get(candidate_family)
        if classifier is None or not isinstance(bmc_documents, list) or not isinstance(infra_documents, list):
            return {}
        normalized_candidate = classifier(candidate)
        normalized_existing = [
            *[_bootwright_bmc_claim_any(item) for item in bmc_documents],
            *[_bootwright_infra_claim_any_unbound(item) for item in infra_documents],
        ]
        if not normalized_candidate or any(not item for item in normalized_existing):
            return {}
        registry = bootwright_host_endpoint_registry_any(endpoint_registry) if endpoint_registry else {
            "apiVersion": "bootwright.io/host-endpoint-registry/v1alpha1",
            "kind": "host-endpoint-registry",
            "owner": "bootwright",
            "claims": [],
        }
        if not registry:
            return {}
        candidate_key = tuple(
            normalized_candidate[key] for key in ["family", "kind", "name", "context", "host"]
        )
        candidate_resources = set(normalized_candidate["resources"])
        conflicts = []
        for existing in normalized_existing:
            existing_key = tuple(
                existing[key] for key in ["family", "kind", "name", "context", "host"]
            )
            if existing["host"] != normalized_candidate["host"]:
                return {}
            overlap = sorted(candidate_resources & set(existing["resources"]))
            if existing_key != candidate_key and overlap:
                conflicts.append(
                    {
                        "source": existing["family"],
                        "kind": existing["kind"],
                        "name": existing["name"],
                        "context": existing["context"],
                        "resources": overlap,
                    }
                )
        for claim in registry["claims"]:
            owner = claim["owner"]
            owner_family = "bmc-emulator" if owner["kind"] == "bmc-emulator" else "infra-component"
            owner_key = (owner_family, owner["kind"], owner["name"], owner["context"], owner["host"])
            resource = f"endpoint:{claim['port']}/{claim['protocol']}"
            if owner["host"] != normalized_candidate["host"]:
                return {}
            if owner_key != candidate_key and resource in candidate_resources:
                conflicts.append(
                    {
                        "source": "endpoint-registry",
                        "kind": owner["kind"],
                        "name": owner["name"],
                        "context": owner["context"],
                        "resources": [resource],
                    }
                )
        return {"valid": True, "candidate": normalized_candidate, "conflicts": conflicts}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_host_shared_service_scan_paths(
    bmc_root_stat,
    bmc_entries,
    infra_root_stat,
    infra_entries,
    transition_root_stat,
    transition_entries,
):
    try:
        safe_segment = re.compile(r"^[A-Za-z0-9_.-]+$")
        digest_name = re.compile(r"^[a-f0-9]{64}\.json$")

        def closed_root(value):
            if not isinstance(value, dict) or not isinstance(value.get("exists"), bool):
                return False
            if not value["exists"]:
                return True
            return (
                value.get("isdir") is True
                and value.get("islnk") is False
                and value.get("pw_name") == "root"
                and value.get("gr_name") == "root"
                and value.get("mode") == "0700"
            )

        def regular_entry(value, path):
            return (
                isinstance(value, dict)
                and value.get("path") == path
                and value.get("isreg") is True
                and value.get("islnk") is False
                and value.get("pw_name") == "root"
                and value.get("gr_name") == "root"
                and value.get("mode") == "0600"
            )

        roots = [
            (bmc_root_stat, bmc_entries),
            (infra_root_stat, infra_entries),
            (transition_root_stat, transition_entries),
        ]
        if any(
            not closed_root(stat)
            or not isinstance(entries, list)
            or (not stat["exists"] and entries)
            for stat, entries in roots
        ):
            return {}

        bmc_root = "/var/lib/bootwright/shared-services/bmc-emulator"
        bmc_documents = []
        providers = {
            entry.get("path", "")[len(bmc_root) + 1 :]
            for entry in bmc_entries
            if isinstance(entry, dict)
            and isinstance(entry.get("path"), str)
            and entry["path"].startswith(bmc_root + "/")
            and "/" not in entry["path"][len(bmc_root) + 1 :]
            and entry.get("isdir") is True
        }
        for entry in bmc_entries:
            path = entry.get("path") if isinstance(entry, dict) else None
            if not isinstance(path, str) or not path.startswith(bmc_root + "/"):
                return {}
            relative = path[len(bmc_root) + 1 :]
            parts = relative.split("/")
            if len(parts) == 1:
                if (
                    safe_segment.fullmatch(parts[0]) is None
                    or entry.get("isdir") is not True
                    or entry.get("islnk") is not False
                    or entry.get("pw_name") != "root"
                    or entry.get("gr_name") != "root"
                    or entry.get("mode") != "0700"
                ):
                    return {}
                continue
            if (
                len(parts) != 2
                or safe_segment.fullmatch(parts[0]) is None
                or parts[0] not in providers
                or parts[1]
                not in {"claim.json", "transition.json", ".bootwright-context"}
                or not regular_entry(entry, path)
            ):
                return {}
            if parts[1] != ".bootwright-context":
                bmc_documents.append(path)

        infra_root = "/var/lib/bootwright/shared-services/infra-component/claims"
        infra_documents = []
        for entry in infra_entries:
            path = entry.get("path") if isinstance(entry, dict) else None
            if (
                not isinstance(path, str)
                or posixpath.dirname(path) != infra_root
                or digest_name.fullmatch(posixpath.basename(path)) is None
                or not regular_entry(entry, path)
            ):
                return {}
            infra_documents.append(path)

        transition_root = transition_root_stat.get("path")
        if transition_root_stat["exists"] and (
            not isinstance(transition_root, str)
            or not posixpath.isabs(transition_root)
            or posixpath.normpath(transition_root) != transition_root
            or not transition_root.endswith("/transitions/infra-component")
        ):
            return {}
        for entry in transition_entries:
            path = entry.get("path") if isinstance(entry, dict) else None
            if (
                not isinstance(path, str)
                or posixpath.dirname(path) != transition_root
                or not path.endswith(".json")
                or safe_segment.fullmatch(posixpath.basename(path)[:-5]) is None
                or not regular_entry(entry, path)
            ):
                return {}
            infra_documents.append(path)

        return {
            "valid": True,
            "bmcDocumentPaths": sorted(bmc_documents),
            "infraDocumentPaths": sorted(infra_documents),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_global_conflicts(claims, candidate, managed_services_dir):
    try:
        if not isinstance(claims, list):
            return {}
        normalized_candidate = bootwright_infra_component_global_claim_any(
            candidate, managed_services_dir
        )
        normalized_claims = [
            bootwright_infra_component_global_claim_any(item, managed_services_dir)
            for item in claims
        ]
        if not normalized_candidate or any(not item for item in normalized_claims):
            return {}
        candidate_sides = _bootwright_infra_global_sides(normalized_candidate)
        candidate_identity = (
            candidate_sides[0]["provider"],
            candidate_sides[0]["name"],
        )
        candidate_resources = set()
        for item in candidate_sides:
            candidate_resources.update(_bootwright_infra_consequences(item))
        conflicts = []
        for claim in normalized_claims:
            sides = _bootwright_infra_global_sides(claim)
            identity = (sides[0]["provider"], sides[0]["name"])
            if identity == candidate_identity:
                continue
            resources = set()
            for item in sides:
                resources.update(_bootwright_infra_consequences(item))
            overlap = sorted(candidate_resources & resources)
            if overlap:
                conflicts.append(
                    {
                        "name": claim["name"],
                        "context": claim["context"],
                        "host": claim["host"],
                        "resources": overlap,
                    }
                )
        return {
            "valid": True,
            "conflicts": sorted(
                conflicts,
                key=lambda item: (item["context"], item["name"], item["host"]),
            ),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_desired_ports(services):
    try:
        if not isinstance(services, list):
            return {}
        ports = []
        for service in services:
            if not isinstance(service, dict):
                return {}
            kind = service.get("kind")
            if kind == "artifacts":
                listeners = service.get("listeners")
                if not isinstance(listeners, list) or not listeners:
                    return {}
                values = [item.get("port") for item in listeners if isinstance(item, dict)]
                protocol = "tcp"
            elif kind == "load-balancer":
                frontends = service.get("frontends")
                if not isinstance(frontends, list):
                    return {}
                entries = []
                for frontend in frontends:
                    if not isinstance(frontend, dict) or not isinstance(frontend.get("ports"), list):
                        return {}
                    entries.extend(frontend["ports"])
                values = [item.get("listenPort") for item in entries if isinstance(item, dict)]
                protocol = "tcp"
            elif kind == "nameResolution":
                ports.extend(["53/tcp", "53/udp"])
                continue
            elif kind in {"ntp", "proxy", "registry"}:
                values = [service.get("port")]
                protocol = "udp" if kind == "ntp" else "tcp"
            else:
                return {}
            if len(values) == 0 or any(
                isinstance(item, bool)
                or not isinstance(item, int)
                or item < 1
                or item > 65535
                for item in values
            ):
                return {}
            ports.extend(f"{item}/{protocol}" for item in values)
        return {"valid": True, "ports": sorted(set(ports))}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_record_ports(
    records, expected_host, excluded_name, managed_services_dir
):
    try:
        if not isinstance(records, list) or not isinstance(expected_host, str):
            return {}
        ports = []
        for record in records:
            if not isinstance(record, dict):
                return {}
            if record.get("kind") != "infra-component" or record.get("role", "") == "reference":
                continue
            if record.get("host") != expected_host or record.get("name") == excluded_name:
                continue
            composite = bootwright_infra_component_record_composite(
                record, managed_services_dir
            )
            if not composite:
                return {}
            ports.extend(composite["ports"])
        return {"valid": True, "ports": sorted(set(ports))}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_transition_retire_ports(
    claim, desired_ports, recorded_ports
):
    try:
        if (
            not isinstance(claim, dict)
            or not isinstance(desired_ports, dict)
            or not desired_ports.get("valid")
            or not isinstance(recorded_ports, dict)
            or not recorded_ports.get("valid")
        ):
            return {}
        previous_ports = []
        for item in claim.get("previous", []):
            ports = _bootwright_transition_ports(item)
            if ports is None:
                return {}
            previous_ports.extend(ports)
        survivors = set(desired_ports.get("ports", [])) | set(
            recorded_ports.get("ports", [])
        )
        return {
            "valid": True,
            "ports": sorted(set(previous_ports) - survivors),
        }
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


def bootwright_infra_component_endpoint_conflicts(composites, selected_names):
    try:
        if (
            not isinstance(composites, list)
            or not isinstance(selected_names, list)
            or any(not isinstance(item, dict) or not item for item in composites)
            or any(not isinstance(item, str) or not item for item in selected_names)
            or len(selected_names) != len(set(selected_names))
        ):
            return {}
        names = [item.get("name") for item in composites]
        if (
            any(not isinstance(item, str) or not item for item in names)
            or len(names) != len(set(names))
            or any(item not in names for item in selected_names)
            or any(not isinstance(item.get("ports"), list) for item in composites)
        ):
            return {}
        selected = [item for item in composites if item["name"] in selected_names]
        survivors = [item for item in composites if item["name"] not in selected_names]
        conflicts = []
        for target in selected:
            for survivor in survivors:
                for port in sorted(set(target["ports"]) & set(survivor["ports"])):
                    conflicts.append({"port": port, "selected": target["name"], "survivor": survivor["name"]})
        return {"valid": True, "conflicts": conflicts}
    except (AttributeError, KeyError, TypeError, ValueError):
        return {}


class FilterModule:
    def filters(self):
        return {
            "bootwright_libvirt_explicit_absence": bootwright_libvirt_explicit_absence,
            "bootwright_podman_container_identity": bootwright_podman_container_identity,
            "bootwright_podman_container_context": bootwright_podman_container_context,
            "bootwright_podman_explicit_absence": bootwright_podman_explicit_absence,
            "bootwright_bmc_pool_identity": bootwright_bmc_pool_identity,
            "bootwright_bmc_pool_uuid": bootwright_bmc_pool_uuid,
            "bootwright_systemd_unit_identity": bootwright_systemd_unit_identity,
            "bootwright_bmc_sushy_config_identity": bootwright_bmc_sushy_config_identity,
            "bootwright_bmc_vmedia_unit_runtime": bootwright_bmc_vmedia_unit_runtime,
            "bootwright_bmc_sushy_unit_runtime": bootwright_bmc_sushy_unit_runtime,
            "bootwright_bmc_claim_identity": bootwright_bmc_claim_identity,
            "bootwright_bmc_transition_identity": bootwright_bmc_transition_identity,
            "bootwright_bmc_claim_digest": bootwright_bmc_claim_digest,
            "bootwright_bmc_consequence_digest": bootwright_bmc_consequence_digest,
            "bootwright_host_operation_guard": bootwright_host_operation_guard,
            "bootwright_host_operation_guard_any": bootwright_host_operation_guard_any,
            "bootwright_host_endpoint_claim": bootwright_host_endpoint_claim,
            "bootwright_host_endpoint_claim_any": bootwright_host_endpoint_claim_any,
            "bootwright_host_endpoint_claim_set": bootwright_host_endpoint_claim_set,
            "bootwright_host_endpoint_claim_groups": bootwright_host_endpoint_claim_groups,
            "bootwright_bmc_endpoint_transition": bootwright_bmc_endpoint_transition,
            "bootwright_host_endpoint_registry_any": bootwright_host_endpoint_registry_any,
            "bootwright_host_endpoint_registry_next": bootwright_host_endpoint_registry_next,
            "bootwright_host_endpoint_registry_remove": bootwright_host_endpoint_registry_remove,
            "bootwright_bmc_record_claim_identity": bootwright_bmc_record_claim_identity,
            "bootwright_systemd_show_identity": bootwright_systemd_show_identity,
            "bootwright_mount_targets": bootwright_mount_targets,
            "bootwright_declared_managed_os_paths": bootwright_declared_managed_os_paths,
            "bootwright_infra_component_record_composite": bootwright_infra_component_record_composite,
            "bootwright_infra_component_record_digests": bootwright_infra_component_record_digests,
            "bootwright_infra_component_selection_digest": bootwright_infra_component_selection_digest,
            "bootwright_infra_component_transition_composite": bootwright_infra_component_transition_composite,
            "bootwright_infra_component_transition_claim": bootwright_infra_component_transition_claim,
            "bootwright_infra_component_transition_next": bootwright_infra_component_transition_next,
            "bootwright_infra_component_transition_ports": bootwright_infra_component_transition_ports,
            "bootwright_infra_component_transition_record": bootwright_infra_component_transition_record,
            "bootwright_infra_component_transition_record_digest": bootwright_infra_component_transition_record_digest,
            "bootwright_infra_component_transition_dispatch": bootwright_infra_component_transition_dispatch,
            "bootwright_infra_component_claim_digest": bootwright_infra_component_claim_digest,
            "bootwright_infra_component_global_claim": bootwright_infra_component_global_claim,
            "bootwright_infra_component_global_apply_next": bootwright_infra_component_global_apply_next,
            "bootwright_infra_component_global_destroy_next": bootwright_infra_component_global_destroy_next,
            "bootwright_infra_component_global_steady": bootwright_infra_component_global_steady,
            "bootwright_infra_component_global_ports": bootwright_infra_component_global_ports,
            "bootwright_infra_component_global_claim_any": bootwright_infra_component_global_claim_any,
            "bootwright_infra_component_global_claim_key": bootwright_infra_component_global_claim_key,
            "bootwright_infra_component_global_claim_digests": bootwright_infra_component_global_claim_digests,
            "bootwright_infra_component_global_conflicts": bootwright_infra_component_global_conflicts,
            "bootwright_host_shared_service_prepublication_conflicts": bootwright_host_shared_service_prepublication_conflicts,
            "bootwright_host_shared_service_scan_paths": bootwright_host_shared_service_scan_paths,
            "bootwright_infra_component_desired_ports": bootwright_infra_component_desired_ports,
            "bootwright_infra_component_record_ports": bootwright_infra_component_record_ports,
            "bootwright_infra_component_transition_retire_ports": bootwright_infra_component_transition_retire_ports,
            "bootwright_infra_component_endpoint_conflicts": bootwright_infra_component_endpoint_conflicts,
        }

"""Native request validation: the closed cluster scope's checks, its option pairs, hosts with their disks, and services with their placements."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import ipaddress
import re
import uuid

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    DEVICE, DEVICE_CLASS, HOST_LABEL, Refusal, SERIAL_DEVICE, SUBVOLUME_UUID, class_groups, digest, named, package_proxy, package_url,
    registry_authority, require, selected_device, tunnel_helpers, validate_auxiliary,
)


def scope_checks(request):
    require(request["version"] == "ceph-core-v1")
    if "security" in request:
        security = request["security"]
        require(isinstance(security, dict) and set(security) == {"fips", "cephxKeyType"})
        require(type(security["fips"]) is bool and security["cephxKeyType"] == "aes")
    require(named(request["controller"]) and named(request["cluster"]) and named(request["owner"]))
    try:
        require(str(uuid.UUID(request["fsid"])) == request["fsid"])
        ipaddress.ip_address(request["monIP"])
    except (ValueError, TypeError, AttributeError):
        raise Refusal("request") from None
    require(isinstance(request["image"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}", request["image"]) is not None)
    tool = request["cephadm"]
    require(isinstance(tool, dict) and set(tool) == {"path", "packageVersion"})
    require(tool["path"] in ("/usr/sbin/cephadm", "/usr/bin/cephadm"))
    for coordinate in (tool["packageVersion"], request["release"], request["ansiblePackageVersion"]):
        require(isinstance(coordinate, str) and 0 < len(coordinate.encode()) <= 1024 and not any(ord(char) < 32 or ord(char) == 127 for char in coordinate))
    if "nfsGaneshaPackageVersion" in request:
        coordinate = request["nfsGaneshaPackageVersion"]
        require(isinstance(coordinate, str) and 0 < len(coordinate.encode()) <= 1024 and not any(ord(char) < 32 or ord(char) == 127 for char in coordinate))
    from urllib.parse import urlsplit
    repository = urlsplit(package_url(request["packageRepository"]))
    key = urlsplit(package_url(request["packageRepositoryKey"]))
    require((repository.scheme, repository.hostname, repository.port or 443) == (key.scheme, key.hostname, key.port or 443), "artifact")
    require(named(request["clusterSSHUser"]))
    require(request["clusterSSHTransport"] == "verifiedTunnel")
    require(named(request["clusterKeyRef"]) and named(request["registryRef"]))
    require("registryTrustBundleRef" not in request or named(request["registryTrustBundleRef"]))
    require(isinstance(request["registry"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]{1,255}", request["registry"]) is not None)
    registry_authority(request)
    validate_auxiliary(request)
    require(request["callHome"] in ("enabled", "disabled"))
    for key in ("publicNetworks", "clusterNetworks"):
        require(request[key] is None or isinstance(request[key], list) and len(request[key]) <= 8)
        for network in request[key] or []:
            try:
                ipaddress.ip_network(network, strict=True)
            except (ValueError, TypeError):
                raise Refusal("request") from None
    require(bool(request["publicNetworks"]))
    validate_config(request)
    hosts, seen = validate_hosts(request)
    # Go admits the profile (internal/storage/cephcluster/topology.go); its one non-stretch host stays checked here
    # because this worker applies osd_crush_chooseleaf_type=0, which on more hosts puts every replica on one host.
    if "singleHostDefaults" in request:
        require(request["singleHostDefaults"] is True and len(hosts) == 1 and "stretch" not in request, "request")
    package = request.get("packages")
    if package is not None:
        require(isinstance(package, dict) and {"version", "licenseAccepted", "sources", "repositoryProxy"} <= set(package)
                <= {"version", "licenseAccepted", "sources", "repositoryProxy", "packageRepositoryTrustBundleRef"})
        package_proxy(package["repositoryProxy"])
        require(package["version"] == "ceph-packages-v1" and package["licenseAccepted"] is True)
        require("packageRepositoryTrustBundleRef" not in package or named(package["packageRepositoryTrustBundleRef"]))
        require(isinstance(package["sources"], list) and len(package["sources"]) == len(hosts))
        names = []
        for source in package["sources"]:
            validate_package_source(source)
            names.append(source["host"])
        require(names == sorted(host["name"] for host in hosts))
    validate_services(request, hosts, seen)
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
    ceph_native_stretch.validate(request, hosts)
    return hosts


def validate_package_source(source):
    require(isinstance(source, dict))
    required_source = {"host", "profile", "media", "publicationPath", "url", "certificateRef"}
    if "subscription" in source:
        required_source = {"host", "profile", "certificateRef"}
        require(set(source) == required_source | {"subscription"} and isinstance(source["subscription"], dict)
                and set(source["subscription"]) == {"hostname", "contentBaseURL"})
        require(all(isinstance(value, str) and 0 < len(value) <= 2048 for value in source["subscription"].values()))
    require(required_source <= set(source) and not set(source) - required_source - {"mediaSHA256", "mediaSize", "subscription"})
    require(all(isinstance(source[k], str) and 0 < len(source[k]) <= 2048 for k in required_source))
    require(named(source["host"]) and named(source["profile"]) and named(source["certificateRef"]))
    require(("mediaSHA256" in source) == ("mediaSize" in source))
    if "mediaSHA256" in source:
        require(digest(source["mediaSHA256"]) and type(source["mediaSize"]) is int and 0 < source["mediaSize"] <= 64 << 30)


def validate_config(request):
    """Admit the closed native memory and pool-default option pairs as canonical decimal strings.

    Their qualified values (the 2 GiB memory floor, autotuning off, the 2 and 2 pool defaults) are Go's admission
    (internal/storage/cephcluster/config.go)."""
    if "config" not in request:
        return
    config = request["config"]
    require(isinstance(config, dict) and bool(config) and set(config) <= {"osd", "global"}, "config-unqualified")
    if "global" in config:
        global_options = config["global"]
        require(isinstance(global_options, dict) and set(global_options) == {"osd_pool_default_size", "osd_pool_default_min_size"}, "config-unqualified")
        require(all(isinstance(value, str) and re.fullmatch(r"[1-9][0-9]{0,2}", value) is not None for value in global_options.values()),
                "config-unqualified")
    if "osd" not in config:
        return
    osd = config["osd"]
    require(isinstance(osd, dict) and set(osd) == {"osd_memory_target", "osd_memory_target_autotune"}, "config-unqualified")
    require(osd["osd_memory_target_autotune"] in ("true", "false"), "config-unqualified")
    target = osd["osd_memory_target"]
    require(isinstance(target, str) and re.fullmatch(r"[1-9][0-9]{0,19}", target) is not None, "config-unqualified")
    require(int(target) <= 2 ** 64 - 1, "config-unqualified")


def validate_hosts(request):
    """Bind every eligible host and disk to one explicit identity."""
    hosts = request["hosts"]
    require(isinstance(hosts, list) and 1 <= len(hosts) <= 64)
    seen = set()
    wwns = set()
    selected_serials = set()
    for host in hosts:
        require(isinstance(host, dict) and set(host) >= {"name", "machine", "address", "user", "port", "devices", "labels", "location", "sshKeyRef"})
        require(not set(host) - {"name", "machine", "address", "user", "port", "devices", "labels", "location", "sshKeyRef",
                                 "hostKeyRef", "knownHosts", "knownHostsRef", "physical", "hardware", "provided", "retainRoot", "rootSerial", "rootWWN"})
        provided = host.get("provided", False)
        require(type(provided) is bool and ("provided" not in host or provided))
        require(not provided or named(host.get("knownHostsRef")) and not host["devices"]
                and not {"hostKeyRef", "knownHosts", "physical", "hardware", "retainRoot", "rootSerial", "rootWWN"} & set(host))
        require(provided or "knownHostsRef" not in host)
        require(all(named(host[k]) for k in ("name", "machine", "user")))
        require(named(host["sshKeyRef"]))
        require(host["labels"] is None or isinstance(host["labels"], list) and all(isinstance(v, str) and HOST_LABEL.fullmatch(v) for v in host["labels"]))
        require(isinstance(host["location"], dict) and all(named(k) and named(v) for k, v in host["location"].items()))
        require(not host.get("hostKeyRef") or named(host["hostKeyRef"]))
        require(not host.get("knownHosts") or isinstance(host["knownHosts"], str) and len(host["knownHosts"]) <= 65536)
        require(host["name"] not in seen)
        seen.add(host["name"])
        try:
            ipaddress.ip_address(host["address"])
        except (ValueError, TypeError):
            raise Refusal("request") from None
        require(type(host["port"]) is int and 1 <= host["port"] <= 65535)
        require(isinstance(host["devices"], list) and len(host["devices"]) <= 128)
        physical = host.get("physical", False)
        require("retainRoot" not in host or physical and host["retainRoot"] is True)
        require(type(physical) is bool and ("hardware" not in host or physical))
        # The Machine's declared root disk serial, which no selected disk may report.
        require("rootWWN" not in host or physical and re.fullmatch(r"0x[0-9a-f]{16}(?:[0-9a-f]{16})?", host["rootWWN"]) is not None)
        require("rootSerial" not in host or physical and re.fullmatch(r"[A-Za-z0-9._:+/=-]{1,256}", host["rootSerial"]) is not None)
        if physical:
            hardware = host.get("hardware")
            require(isinstance(hardware, dict) and set(hardware) == {"uuid", "macs"})
            require(isinstance(hardware["uuid"], str) and SUBVOLUME_UUID.fullmatch(hardware["uuid"]) is not None)
            require(isinstance(hardware["macs"], list) and 1 <= len(hardware["macs"]) <= 32
                    and all(isinstance(m, str) and re.fullmatch(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}", m) for m in hardware["macs"])
                    and hardware["macs"] == sorted(set(hardware["macs"])))
        paths = set()
        serials = set()
        for device in host["devices"]:
            require(isinstance(device, dict) and set(device) >= {"path", "serial", "sizeBytes"}
                    and not set(device) - {"path", "serial", "sizeBytes", "rotational", "wwn"})
            require(isinstance(device["path"], str) and DEVICE.fullmatch(device["path"]) is not None)
            if physical and device["path"].startswith("/dev/disk/by-serial/"):
                serial = SERIAL_DEVICE.fullmatch(device["path"])
                require(set(device) == {"path", "serial", "sizeBytes"} and serial is not None and device["serial"] == serial.group(1))
                require(type(device["sizeBytes"]) is int and 4096 <= device["sizeBytes"] <= 64 << 40 and device["sizeBytes"] % 4096 == 0)
                require(device["path"] not in paths and device["serial"] not in selected_serials)
                selected_serials.add(device["serial"])
                paths.add(device["path"])
                continue
            if physical:
                require(set(device) == {"path", "serial", "sizeBytes", "wwn"} and device["serial"] == "" and device["sizeBytes"] == 0)
                require(isinstance(device["wwn"], str) and re.fullmatch(r"0x[0-9a-f]{16}(?:[0-9a-f]{16})?", device["wwn"]) is not None)
                require(device["path"] == "/dev/disk/by-id/wwn-" + device["wwn"] and device["path"] not in paths and device["wwn"] not in wwns)
                wwns.add(device["wwn"])
                paths.add(device["path"])
                continue
            require("wwn" not in device and not device["path"].startswith("/dev/disk/by-serial/"))
            require(named(device["serial"]) and device["path"] not in paths and device["serial"] not in serials)
            paths.add(device["path"])
            serials.add(device["serial"])
            require(type(device["sizeBytes"]) is int and device["sizeBytes"] > 0)
            require(type(device.get("rotational", False)) is bool)
    require(len({host["name"].split(".", 1)[0] for host in hosts}) == len(hosts), "host-identity")
    require(request["bootstrap"] in seen and not any(host.get("provided") and host["name"] == request["bootstrap"] for host in hosts))
    require(request["monIP"] == next(h["address"] for h in hosts if h["name"] == request["bootstrap"]))
    return hosts, seen


def validate_services(request, hosts, seen):
    """Preserve native placement while bounding all OSD effect targets."""
    services = request["services"]
    require(isinstance(services, list) and 2 <= len(services) <= 128)
    kinds = set()
    ids = set()
    selected_devices = set()
    for service in services:
        require(isinstance(service, dict))
        kind = service.get("service_type")
        require(kind in ("mon", "mgr", "osd", "mds"), "service-unqualified")
        kinds.add(kind)
        require(not set(service) - {"service_type", "service_id", "placement", "spec"}, "service-unqualified")
        identity = (kind, service.get("service_id", ""))
        require(identity not in ids)
        ids.add(identity)
        placement = service.get("placement")
        require(isinstance(placement, dict) and "hosts" in placement and not set(placement) - {"hosts", "count", "count_per_host"}, "placement-unqualified")
        require(isinstance(placement["hosts"], list) and bool(placement["hosts"]))
        require(all(isinstance(h, str) and h in seen for h in placement["hosts"]))
        require(len(set(placement["hosts"])) == len(placement["hosts"]))
        require("count" not in placement or type(placement["count"]) is int and 1 <= placement["count"] <= len(placement["hosts"]), "placement-unqualified")
        # One daemon per host is Go's admission (internal/storage/ceph/services.go).
        require("count_per_host" not in placement or type(placement["count_per_host"]) is int and 1 <= placement["count_per_host"] <= 128,
                "placement-unqualified")
        if kind == "mds":
            identifier = service.get("service_id")
            require(named(identifier) and not identifier[0].isdigit(), "service-unqualified")
            require("spec" not in service, "service-unqualified")
            continue
        if kind != "osd":
            # A MON's crush_locations are the stretch topology's, which ceph_native_stretch.validate proves.
            require((kind == "mon" or not service.get("spec")) and not service.get("service_id"), "service-unqualified")
            continue
        require(named(service.get("service_id")))
        require(placement.get("count", len(placement["hosts"])) == len(placement["hosts"]), "device-unproved")
        spec = service.get("spec")
        require(isinstance(spec, dict) and "data_devices" in spec
                and not set(spec) - {"data_devices", "db_devices", "wal_devices", "encrypted", "tpm2", "crush_device_class"}, "osd-unqualified")
        require(all(type(spec.get(field, False)) is bool for field in ("encrypted", "tpm2")), "osd-unqualified")
        require("crush_device_class" not in spec or isinstance(spec["crush_device_class"], str)
                and DEVICE_CLASS.fullmatch(spec["crush_device_class"]) is not None, "osd-unqualified")
        for field in ("data_devices", "db_devices", "wal_devices"):
            if field not in spec:
                continue
            selection = spec[field]
            require(isinstance(selection, dict) and set(selection) == {"paths"}, "osd-unqualified")
            require(isinstance(selection["paths"], list) and bool(selection["paths"]))
            require(field == "data_devices" or all(isinstance(path, str) for path in selection["paths"]), "osd-unqualified")
            for name in placement["hosts"]:
                host = next(h for h in hosts if h["name"] == name)
                for path in selection["paths"]:
                    selected = selected_device(host, path)
                    require((name, selected["path"]) not in selected_devices, "device-overlap")
                    selected_devices.add((name, selected["path"]))
        # One native document per device class cannot share a DB or WAL device with another.
        require(len(class_groups(spec)) == 1 or not ({"db_devices", "wal_devices"} & set(spec)), "osd-unqualified")
    require(kinds >= {"mon", "mgr"})
    require(request["bootstrap"] in next(s["placement"]["hosts"] for s in services if s["service_type"] == "mon"))
    helper, unused = tunnel_helpers()
    helper.targets(request)
    require(request["bootstrap"] in next(s["placement"]["hosts"] for s in services if s["service_type"] == "mgr"), "native-tunnel-unproved")
    require(selected_devices == {(h["name"], d["path"]) for h in hosts for d in h["devices"]}, "device-unproved")

"""The object gateway worker: the claim and record before any radosgw-admin call, adoption only under an attempt record, the
zone's pools within the frozen set, the period committed before RGW starts, and read-only observation.

The scripted monitor answers the upstream v20.2.1 shapes the worker reads:
- every radosgw-admin access to the config store creates `.rgw.root` when it is absent (src/rgw/driver/rados/config/impl.h:93,
  src/rgw/driver/rados/rgw_tools.cc:24-52), and `osd pool ls detail` lists `pool_name` (src/osd/osd_types.cc:1568-1653);
- `realm list`, `zonegroup list` and `zone list` print `default_info` and the names under `realms`, `zonegroups` and `zones`
  (src/rgw/radosgw-admin/radosgw-admin.cc:5192-5215, :5649-5675, :6341-6370); `realm create` sets the default realm only with
  `--default` (:5110-5133); `zone create --master` writes the zone, then adds it to its zone group (:6100-6200);
- `zone get` prints `id`, `name`, every `*_pool` and `domain_root` as `<pool>[:<namespace>]`, `system_key` and `placement_pools` as
  key/val entries (src/rgw/rgw_zone.cc:311-338), with the names `init_zone_pool_names` derives (src/rgw/rgw_zone.cc:1277-1312);
- `period get` prints `realm_id`, `master_zonegroup`, `master_zone` and `period_map.zonegroups`, each with its `zones`;
- `orch ps` rows carry `daemon_type`, `daemon_id`, `hostname`, `service_name`, `status`, `container_image_digests`, `ports` and `ip`
  (src/pybind/mgr/orchestrator/_interface.py:1471-1500); `orch apply -i -` stores each document (src/pybind/mgr/orchestrator/module.py:1826-1846).
"""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import json
import os
import uuid

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_native, ceph_native_object, ceph_native_observability, ceph_tunnel_staging,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings

FSID = settings.FSID
OWNER = settings.OWNER
ACCESS = "AKSYNTHETICACCESS0001"
SECRET_KEY = "Sx9synthetic-system-secret-key-value"
HOSTS = ["node-1", "node-2", "node-3"]
DERIVED = ["objects.rgw." + suffix for suffix in ("buckets.data", "buckets.index", "buckets.non-ec", "control", "dedup", "log", "meta", "otp")]


def document(spec=None):
    value = {"rgw_realm": "objects", "rgw_zone": "objects", "rgw_zonegroup": "objects"}
    value.update(spec or {})
    return {"placement": {"count": 2, "hosts": list(HOSTS)}, "service_id": "objects", "service_type": "rgw", "spec": value}


def gateway_request(spec=None, **members):
    gateway = {"bindings": [], "dataExtraPool": "objects.rgw.buckets.non-ec", "dataPool": "objects.rgw.buckets.data", "documents": [document(spec)],
               "endpoint": "", "indexPool": "objects.rgw.buckets.index", "name": "objects", "realm": "objects", "zone": "objects", "zoneGroup": "objects",
               "zonePools": list(DERIVED)}
    gateway.update(members)
    request = dict(settings.cluster(), clusterSSHTransport="verified-tunnel", action="gateway", gateway=gateway)
    request["delegated"] = [{"name": "rgw.objects", "hosts": list(HOSTS)}]
    return request


def zone_document(name, identifier, realm):
    """A zone as `zone get` prints it, with the pool names `init_zone_pool_names` derives and a synthetic system key."""
    meta, log = name + ".rgw.meta", name + ".rgw.log"
    return {"id": identifier, "name": name, "domain_root": meta + ":root", "control_pool": name + ".rgw.control", "dedup_pool": name + ".rgw.dedup",
            "gc_pool": log + ":gc", "lc_pool": log + ":lc", "log_pool": log, "intent_log_pool": log + ":intent", "usage_log_pool": log + ":usage",
            "roles_pool": meta + ":roles", "reshard_pool": log + ":reshard", "user_keys_pool": meta + ":users.keys", "user_email_pool": meta + ":users.email",
            "user_swift_pool": meta + ":users.swift", "user_uid_pool": meta + ":users.uid", "otp_pool": name + ".rgw.otp", "notif_pool": log + ":notif",
            "topics_pool": meta + ":topics", "account_pool": meta + ":accounts", "group_pool": meta + ":groups",
            "system_key": {"access_key": ACCESS, "secret_key": SECRET_KEY},
            "placement_pools": [{"key": "default-placement", "val": {"index_pool": name + ".rgw.buckets.index",
                                                                     "storage_classes": {"STANDARD": {"data_pool": name + ".rgw.buckets.data"}},
                                                                     "data_extra_pool": name + ".rgw.buckets.non-ec", "index_type": 0, "inline_data": True}}],
            "tier_config": [], "realm_id": realm, "restore_pool": log + ":restore"}


def words(command):
    """The subcommand words, the option values and the flags of one radosgw-admin argv."""
    found, values, flags, index = [], {}, set(), 0
    while index < len(command):
        token = command[index]
        if token in ("--master", "--commit", "--default"):
            flags.add(token)
            index += 1
        elif token.startswith("--"):
            values[token] = command[index + 1]
            index += 2
        else:
            found.append(token)
            index += 1
    return tuple(found), values, flags


class Monitor:
    """A scripted cluster answering `ceph` and `radosgw-admin` by the argv after `--format json`; effects are logged and applied."""

    def __init__(self, request, pools=None, specs=None, running=None):
        self.request = request
        self.pools = list(pools or [])
        self.specs = dict(specs or {})
        self.running = list(running or [])
        self.realms, self.zonegroups, self.zones, self.periods = {}, {}, {}, {}
        self.joins = True
        self.zone_extra = {}
        self.placement_sticks = True
        self.log, self.mutations, self.batches, self.on_radosgw = [], [], [], None
        self.config, self.apply_error = [], None

    def run(self, argv, data=b"", timeout=60):
        del timeout
        assert "--force" not in argv
        tool = os.path.basename(argv[0])
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append((tool, command, data))
        if tool == "radosgw-admin":
            if self.on_radosgw is not None:
                self.on_radosgw()
            if not any(pool["pool_name"] == ".rgw.root" for pool in self.pools):
                self.pools.append({"pool_name": ".rgw.root", "pool_id": 1, "application_metadata": {"rgw": {}}})
            return self.radosgw(command)
        answers = {("fsid",): {"fsid": FSID}, ("osd", "pool", "ls", "detail"): self.pools,
                   ("orch", "ls", "--export"): [dict(value, service_name=name) for name, value in self.specs.items()], ("orch", "ps"): self.running,
                   ("config", "dump"): self.config}
        if command in answers:
            return 0, json.dumps(answers[command]).encode()
        self.mutations.append((tool, command))
        assert command == ("orch", "apply", "-i", "-")
        if self.apply_error is not None:
            return 22, ceph_native.NativeOutput(b"", self.apply_error(data))
        batch = [json.loads(part) for part in data.split(b"---\n")]
        self.batches.append(batch)
        for value in batch:
            self.specs[value["service_type"] + "." + value["service_id"]] = value
        return 0, b""

    def radosgw(self, command):
        found, values, flags = words(command)
        lists = {("realm", "list"): ("realms", self.realms), ("zonegroup", "list"): ("zonegroups", self.zonegroups), ("zone", "list"): ("zones", self.zones)}
        if found in lists:
            member, table = lists[found]
            return 0, json.dumps({"default_info": "", member: sorted(table)}).encode()
        gets = {("realm", "get"): (self.realms, "--rgw-realm"), ("zonegroup", "get"): (self.zonegroups, "--rgw-zonegroup"),
                ("zone", "get"): (self.zones, "--rgw-zone")}
        if found in gets:
            table, option = gets[found]
            if values[option] not in table:
                return 2, ceph_native.NativeOutput(b"", b"ERROR: failed to load\n")
            value = copy.deepcopy(table[values[option]])
            if found == ("zone", "get"):
                value.update(self.zone_extra)
            return 0, json.dumps(value).encode()
        if found == ("period", "get"):
            return 0, json.dumps(self.periods[values["--rgw-realm"]]).encode()
        self.mutations.append(("radosgw-admin", command))
        self.effect(found, values, flags)
        return 0, b"{}"

    def effect(self, found, values, flags):
        if found == ("realm", "create"):
            identifier = str(uuid.uuid4())
            self.realms[values["--rgw-realm"]] = {"id": identifier, "name": values["--rgw-realm"], "current_period": str(uuid.uuid4()), "epoch": 1}
            self.periods[values["--rgw-realm"]] = {"id": str(uuid.uuid4()), "realm_id": identifier, "master_zonegroup": "", "master_zone": "",
                                                   "period_map": {"zonegroups": []}}
        elif found == ("zonegroup", "create"):
            realm = self.realms[values["--rgw-realm"]]["id"]
            self.zonegroups[values["--rgw-zonegroup"]] = {"id": str(uuid.uuid4()), "name": values["--rgw-zonegroup"], "realm_id": realm,
                                                          "master_zone": "", "zones": []}
        elif found == ("zone", "create"):
            realm = self.realms[values["--rgw-realm"]]["id"]
            self.zones[values["--rgw-zone"]] = zone_document(values["--rgw-zone"], str(uuid.uuid4()), realm)
            if self.joins:
                self.join(values)
        elif found == ("zonegroup", "add"):
            assert "--master" in flags
            self.join(values)
        elif found == ("zone", "placement", "modify"):
            assert values["--placement-id"] == "default-placement"
            if self.placement_sticks:
                target = self.zones[values["--rgw-zone"]]["placement_pools"][0]["val"]
                for option, key in (("--index-pool", "index_pool"), ("--data-extra-pool", "data_extra_pool")):
                    if option in values:
                        target[key] = values[option]
                if "--data-pool" in values:
                    target["storage_classes"]["STANDARD"]["data_pool"] = values["--data-pool"]
        elif found == ("period", "update"):
            assert "--commit" in flags
            group = self.zonegroups[values["--rgw-zonegroup"]]
            period = self.periods[values["--rgw-realm"]]
            period.update(master_zonegroup=group["id"], master_zone=group["master_zone"], period_map={"zonegroups": [copy.deepcopy(group)]})
        else:
            raise AssertionError(found)

    def join(self, values):
        group = self.zonegroups[values["--rgw-zonegroup"]]
        zone = self.zones[values["--rgw-zone"]]
        if zone["id"] not in [entry["id"] for entry in group["zones"]]:
            group["zones"].append({"id": zone["id"], "name": zone["name"]})
        group["master_zone"] = zone["id"]

    def radosgw_calls(self):
        return [command for tool, command, unused in self.log if tool == "radosgw-admin"]


PROBES = []
REAL_ANSWERS = ceph_native_object.answers
BOTH = [{"name": "rgw.media", "hosts": list(HOSTS)}, {"name": "rgw.objects", "hosts": list(HOSTS)}]


@pytest.fixture(autouse=True)
def native_boundaries(monkeypatch):
    """Identity normalization and held custody, a verified ceph-common and radosgw-admin hash, and every frontend answering."""
    PROBES.clear()
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **options: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "verify_tool", lambda self: None)
    monkeypatch.setattr(ceph_native_object.ObjectWorker, "tool_digest", lambda self: "e" * 64)
    monkeypatch.setattr(ceph_native_object, "answers", lambda address, number: PROBES.append((address, number)) or True)


def scope(request):
    cluster_request = dict({key: value for key, value in request.items() if key != "gateway"}, action="cluster")
    return ceph_tunnel_staging.recovery_scope(cluster_request)


def markers_for(tmp_path, request):
    markers = ceph_native.Markers(request, root=str(tmp_path))
    os.makedirs(markers.path, mode=0o700)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", scope(request))
    return markers


def records(markers):
    return sorted(os.listdir(markers.path))


def worker(monitor, markers):
    native = ceph_native_object.ObjectWorker(monitor.request, "node-1", run=monitor.run, markers=markers)
    native.pause = lambda seconds: None
    return native


def intent(request, name="objects"):
    return {"owner": OWNER, "fsid": FSID, "gateway": name, "realm": request["gateway"]["realm"], "zoneGroup": request["gateway"]["zoneGroup"],
            "zone": request["gateway"]["zone"]}


def apply_all(monitor, markers):
    return [getattr(worker(monitor, markers), phase)() for phase in ceph_native_object.APPLY_PHASES]


def daemon(request, host, status=1, image=None, **extra):
    row = {"daemon_type": "rgw", "daemon_id": "objects." + host + ".abcdef", "hostname": host, "service_name": "rgw.objects", "status": status,
           "container_image_digests": [image or request["image"]]}
    row.update(extra)
    return row


def ready_gateway(tmp_path, request=None):
    request = request or gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_all(monitor, markers)
    monitor.running = [daemon(request, host) for host in ("node-1", "node-2")]
    return markers, monitor


def observe(monitor, markers):
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def test_marker_precedes_any_radosgw_admin_call(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    seen = []
    monitor.on_radosgw = lambda: seen.append((records(markers), list(monitor.log[:-1])))
    assert worker(monitor, markers).gateway_zone() == {"changed": True}
    first, before = seen[0]
    assert "gateway-objects-intent.json" in first and "rgw-root.json" in first
    assert [(tool, command) for tool, command, unused in before] == [("ceph", ("fsid",)), ("ceph", ("osd", "pool", "ls", "detail"))]
    assert markers.read("rgw-root.json") == {"owner": OWNER, "fsid": FSID, "pool": ".rgw.root"}
    assert markers.read("gateway-objects-intent.json") == intent(request)


def test_crash_after_absence_proof_before_realm_create_adopts(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    markers.write("rgw-root.json", {"owner": OWNER, "fsid": FSID, "pool": ".rgw.root"})
    markers.write("gateway-objects-intent.json", intent(request))
    monitor = Monitor(request, pools=[{"pool_name": ".rgw.root", "pool_id": 1}])
    assert worker(monitor, markers).gateway_zone() == {"changed": True}
    assert set(monitor.realms) == {"objects"} and set(monitor.zones) == {"objects"}
    assert markers.read("gateway-objects-zone-id.json") == {"id": monitor.zones["objects"]["id"]}


def test_realm_refuses_foreign_rgw_root(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request, pools=[{"pool_name": ".rgw.root", "pool_id": 1}])
    monitor.realms["objects"] = {"id": str(uuid.uuid4()), "name": "objects"}
    before = records(markers)
    with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$") as caught:
        worker(monitor, markers).gateway_zone()
    assert caught.value.native_error["cause"] == ".rgw.root"
    assert monitor.radosgw_calls() == [] and records(markers) == before


def test_sibling_gateway_claim_admits_shared_rgw_root(tmp_path):
    first = dict(gateway_request(), delegated=copy.deepcopy(BOTH))
    markers = markers_for(tmp_path, first)
    apply_all(Monitor(first), markers)
    second = gateway_request(spec={"rgw_realm": "media", "rgw_zonegroup": "media", "rgw_zone": "media"}, name="media", realm="media", zoneGroup="media",
                             zone="media", indexPool="media.rgw.buckets.index", dataPool="media.rgw.buckets.data",
                             dataExtraPool="media.rgw.buckets.non-ec", zonePools=sorted(pool.replace("objects.", "media.") for pool in DERIVED))
    second["gateway"]["documents"][0]["service_id"] = "media"
    second["delegated"] = copy.deepcopy(BOTH)
    monitor = Monitor(second, pools=[{"pool_name": ".rgw.root", "pool_id": 1}])
    monitor.realms["objects"] = {"id": str(uuid.uuid4()), "name": "objects"}
    sibling = ceph_native.Markers(second, root=str(tmp_path))
    assert worker(monitor, sibling).gateway_zone() == {"changed": True}
    assert "media" in monitor.realms and sibling.read("gateway-media-intent.json") == intent(second, "media")


@pytest.mark.parametrize("kind", ["realm", "zonegroup", "zone"])
def test_realm_retry_adopts_only_with_attempt_marker(tmp_path, kind):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    original = worker(monitor, markers)
    original.gateway_zone()
    created = {"realm": monitor.realms, "zonegroup": monitor.zonegroups, "zone": monitor.zones}[kind]["objects"]["id"]
    os.unlink(os.path.join(markers.path, "gateway-objects-" + kind + "-id.json"))
    monitor.mutations.clear()
    assert worker(monitor, markers).gateway_zone() == {"changed": False}
    assert markers.read("gateway-objects-" + kind + "-id.json") == {"id": created} and monitor.mutations == []
    os.unlink(os.path.join(markers.path, "gateway-objects-" + kind + "-id.json"))
    os.unlink(os.path.join(markers.path, "gateway-objects-" + kind + "-attempt.json"))
    for later in ("realm", "zonegroup", "zone")[("realm", "zonegroup", "zone").index(kind) + 1:]:
        os.unlink(os.path.join(markers.path, "gateway-objects-" + later + "-id.json"))
        os.unlink(os.path.join(markers.path, "gateway-objects-" + later + "-attempt.json"))
    with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$") as caught:
        worker(monitor, markers).gateway_zone()
    assert caught.value.native_error["cause"] == kind + " objects"
    assert not any(command[1] == "create" for unused, command in monitor.mutations)


@pytest.mark.parametrize("case", ["gone", "other id", "other realm"])
def test_recorded_entity_gone_or_changed_refuses(tmp_path, case):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_all(monitor, markers)
    if case == "gone":
        del monitor.zones["objects"]
    elif case == "other id":
        monitor.zones["objects"]["id"] = str(uuid.uuid4())
    else:
        monitor.zones["objects"]["realm_id"] = str(uuid.uuid4())
    for phase in ceph_native_object.APPLY_PHASES:
        with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$"):
            getattr(worker(monitor, markers), phase)()
    with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$"):
        observe(monitor, markers)


def test_recorded_gateway_with_rgw_root_gone_refuses_without_radosgw_admin(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_all(monitor, markers)
    monitor.pools = [pool for pool in monitor.pools if pool["pool_name"] != ".rgw.root"]
    monitor.realms, monitor.zonegroups, monitor.zones = {}, {}, {}
    before = len(monitor.radosgw_calls())
    with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$") as caught:
        worker(monitor, markers).gateway_zone()
    assert caught.value.native_error["cause"] == "realm objects is gone"
    assert len(monitor.radosgw_calls()) == before
    assert not any(pool["pool_name"] == ".rgw.root" for pool in monitor.pools)


def test_renamed_gateway_applies_after_a_foreign_name_refusal(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    markers.write("rgw-root.json", {"owner": OWNER, "fsid": FSID, "pool": ".rgw.root"})
    monitor = Monitor(request, pools=[{"pool_name": ".rgw.root", "pool_id": 1}])
    monitor.realms["objects"] = {"id": str(uuid.uuid4()), "name": "objects"}
    before = records(markers)
    with pytest.raises(ceph_native.NativeFailure, match="^object-realm-foreign$") as caught:
        worker(monitor, markers).gateway_zone()
    assert caught.value.native_error["cause"] == "realm objects"
    assert records(markers) == before and monitor.mutations == []
    renamed = gateway_request(spec={"rgw_realm": "renamed"}, realm="renamed")
    monitor.request = renamed
    again = ceph_native.Markers(renamed, root=str(tmp_path))
    assert worker(monitor, again).gateway_zone() == {"changed": True}
    assert again.read("gateway-objects-intent.json") == intent(renamed) and "renamed" in monitor.realms


def test_realm_create_never_passes_default(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_all(monitor, markers)
    calls = monitor.radosgw_calls()
    assert not any("--default" in command or "default" in command for command in calls)
    assert [command for command in calls if command[1:2] == ("create",)] == [
        ("realm", "create", "--rgw-realm", "objects"),
        ("zonegroup", "create", "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--master"),
        ("zone", "create", "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--rgw-zone", "objects", "--master"),
    ]


def test_zone_membership_is_repaired_once(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    monitor.joins = False
    assert worker(monitor, markers).gateway_zone() == {"changed": True}
    add = ("zonegroup", "add", "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--rgw-zone", "objects", "--master")
    assert [command for unused, command in monitor.mutations if command[:2] == ("zonegroup", "add")] == [add]
    monitor.mutations.clear()
    assert worker(monitor, markers).gateway_zone() == {"changed": False}
    assert monitor.mutations == []


def test_zone_pool_set_must_be_within_frozen_set(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).gateway_zone()
    monitor.zone_extra = {"notif_pool": "objects.rgw.extra:notif"}
    with pytest.raises(ceph_native.NativeFailure, match="^object-zone-pools$") as caught:
        worker(monitor, markers).gateway_placement()
    assert caught.value.native_error["cause"] == "objects.rgw.extra"
    assert not any(command[:2] in (("period", "update"), ("orch", "apply")) for unused, command in monitor.mutations)
    monitor.zone_extra = {"dedup_pool": ""}
    assert worker(monitor, markers).gateway_placement() == {"changed": True}


def test_declared_placement_is_modified_then_verified(tmp_path):
    pools = sorted(set(DERIVED) - {"objects.rgw.buckets.index"} | {"index"})
    request = gateway_request(indexPool="index", zonePools=pools)
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).gateway_zone()
    worker(monitor, markers).gateway_placement()
    modify = ("zone", "placement", "modify", "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--rgw-zone", "objects",
              "--placement-id", "default-placement", "--index-pool", "index")
    assert [command for unused, command in monitor.mutations if command[:3] == ("zone", "placement", "modify")] == [modify]
    monitor.mutations.clear()
    worker(monitor, markers).gateway_placement()
    assert monitor.mutations == []
    other = gateway_request(dataPool="data", zonePools=sorted(set(DERIVED) | {"data"}))
    stuck = markers_for(tmp_path / "stuck", other)
    monitor = Monitor(other)
    monitor.placement_sticks = False
    worker(monitor, stuck).gateway_zone()
    with pytest.raises(ceph_native.NativeFailure, match="^object-zone-pools$") as caught:
        worker(monitor, stuck).gateway_placement()
    assert caught.value.native_error["cause"] == "default-placement"


def test_period_commit_only_when_the_zone_is_not_committed(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).gateway_zone()
    assert worker(monitor, markers).gateway_placement() == {"changed": True}
    commit = ("period", "update", "--commit", "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--rgw-zone", "objects")
    assert [command for unused, command in monitor.mutations if command[:2] == ("period", "update")] == [commit]
    monitor.mutations.clear()
    assert worker(monitor, markers).gateway_placement() == {"changed": False}
    assert monitor.mutations == []


def test_gateway_service_waits_for_the_committed_period(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).gateway_zone()
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        worker(monitor, markers).gateway_service()
    worker(monitor, markers).gateway_placement()
    monitor.periods["objects"]["master_zone"] = ""
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        worker(monitor, markers).gateway_service()
    assert monitor.batches == [] and not any(name.startswith("service-") for name in records(markers))


def test_rgw_apply_on_stdin_only(tmp_path):
    request = gateway_request(spec={"rgw_frontend_port": 8080})
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    assert apply_all(monitor, markers)[-1] == {"changed": True}
    applies = [(command, data) for tool, command, data in monitor.log if command[:2] == ("orch", "apply")]
    assert len(applies) == 1 and applies[0][0] == ("orch", "apply", "-i", "-")
    stored = json.loads(applies[0][1])
    assert stored == request["gateway"]["documents"][0]
    assert {stored["spec"][key] for key in ("rgw_realm", "rgw_zonegroup", "rgw_zone")} == {"objects"}
    assert markers.read("service-rgw.objects-complete.json") == {"scope": scope(request), "service": stored}
    monitor.mutations.clear()
    assert worker(monitor, markers).gateway_service() == {"changed": False} and monitor.mutations == []


def test_backend_readiness_counts_rgw_daemons(tmp_path):
    markers, monitor = ready_gateway(tmp_path)
    request = monitor.request
    ready, blocking, evidence = observe(monitor, markers)
    assert ready is True and blocking == []
    assert evidence["daemons"] == [{"host": host, "name": "rgw.objects." + host + ".abcdef", "service": "rgw.objects"} for host in ("node-1", "node-2")]
    monitor.running = [daemon(request, "node-1"), daemon(request, "node-2", status=0)]
    ready, blocking, unused = observe(monitor, markers)
    assert ready is False and blocking == ["rgw@rgw.objects"]
    monitor.running = [daemon(request, "node-1"), daemon(request, "node-2", image="registry.example.test/ceph@sha256:" + "f" * 64)]
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        observe(monitor, markers)
    monitor.running = [daemon(request, "node-1"), daemon(request, "node-2", container_image_digests=[])]
    assert observe(monitor, markers)[:2] == (False, ["rgw@rgw.objects"])


@pytest.mark.parametrize("spec,port", [({}, 80), ({"rgw_frontend_port": 8080}, 8080)])
def test_backend_readiness_needs_an_http_status_line(tmp_path, monkeypatch, spec, port):
    markers, monitor = ready_gateway(tmp_path, gateway_request(spec=spec))

    class Connection:
        def __init__(self, reply):
            self.reply, self.sent = reply, b""

        def __enter__(self):
            return self

        def __exit__(self, *unused):
            return False

        def sendall(self, data):
            self.sent += data

        def settimeout(self, value):
            assert 0 < value <= ceph_native_observability.PROBE_TIMEOUT

        def recv(self, size):
            reply, self.reply = self.reply[:size], self.reply[size:]
            return reply

    targets, replies = [], []

    def connect(address, timeout):
        targets.append((address, timeout))
        return replies.pop(0)

    monkeypatch.setattr(ceph_native_object, "answers", REAL_ANSWERS)
    monkeypatch.setattr(ceph_native_observability.socket, "create_connection", connect)
    replies[:] = [Connection(b""), Connection(b"")]
    ready, blocking, unused = observe(monitor, markers)
    assert ready is False and blocking == ["http@rgw.objects"]
    assert targets == [(("192.0.2.1", port), 5), (("192.0.2.2", port), 5)]
    replies[:] = [Connection(b"HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\n\r\n"), Connection(b"HTTP/1.1 403 Forbidden\r\n")]
    assert observe(monitor, markers)[:2] == (True, [])
    monitor.running[0]["ports"] = [port + 1]
    replies[:] = [Connection(b"HTTP/1.1 200 OK\r\n")]
    assert observe(monitor, markers)[:2] == (False, ["http@rgw.objects"])


def test_gateway_never_reads_or_creates_object_users(tmp_path):
    markers, monitor = ready_gateway(tmp_path)
    observe(monitor, markers)
    forbidden = {"user", "key", "caps", "metadata", "--access-key", "--secret", "--secret-key", "--system"}
    assert not any(forbidden & set(command) for command in monitor.radosgw_calls())


def test_gateway_evidence_has_no_secret(tmp_path):
    markers, monitor = ready_gateway(tmp_path)
    ready, blocking, evidence = observe(monitor, markers)
    assert ready and ACCESS not in json.dumps(evidence) and SECRET_KEY not in json.dumps(evidence)
    monitor.zone_extra = {"notif_pool": "objects.rgw.extra:notif"}
    for call in (lambda: worker(monitor, markers).gateway_placement(), lambda: observe(monitor, markers)):
        with pytest.raises(ceph_native.NativeFailure) as caught:
            call()
        assert ACCESS not in json.dumps(caught.value.native_error) and SECRET_KEY not in json.dumps(caught.value.native_error)
    monitor.zone_extra = {"id": str(uuid.uuid4())}
    with pytest.raises(ceph_native.NativeFailure) as caught:
        observe(monitor, markers)
    assert SECRET_KEY not in json.dumps(caught.value.native_error) and ACCESS not in json.dumps(caught.value.native_error)


def test_gateway_observe_reads_only(tmp_path):
    markers, monitor = ready_gateway(tmp_path)
    os.unlink(os.path.join(markers.path, "gateway-objects-placement.json"))
    before = records(markers)
    monitor.mutations.clear()
    ready, unused, evidence = observe(monitor, markers)
    assert ready is False and monitor.mutations == [] and records(markers) == before
    assert evidence["owned"] and evidence["exists"]


def test_gateway_observation_never_creates_the_rgw_root(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    ready, unused, evidence = observe(monitor, markers)
    assert ready is False and monitor.radosgw_calls() == [] and monitor.pools == []
    assert not evidence["owned"] and not evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}


def test_gateway_unfinished_reports_its_own_record(tmp_path):
    request = gateway_request()
    markers = markers_for(tmp_path, request)
    native = worker(Monitor(request), markers)
    assert native.unfinished(native.empty_evidence())["owned"] is False
    markers.write("gateway-objects-intent.json", intent(request))
    evidence = native.unfinished(native.empty_evidence())
    assert evidence["owned"] and evidence["exists"] and evidence["partial"]


def with_spec(member, value):
    request = gateway_request()
    request["gateway"]["documents"][0]["spec"][member] = value
    return request


def without_index():
    request = gateway_request()
    request["gateway"]["zonePools"].remove("objects.rgw.buckets.index")
    return request


INVALID = {
    "ssl in the spec": lambda: with_spec("ssl", True),
    "disagreeing rgw_zone": lambda: with_spec("rgw_zone", "other"),
    "another frontend": lambda: with_spec("rgw_frontend_type", "civetweb"),
    "document not in delegated": lambda: dict(gateway_request(), delegated=[{"name": "rgw.other", "hosts": list(HOSTS)}]),
    "zonePools missing the index pool": without_index,
    "extra member": lambda: gateway_request(extra=True),
    "second document": lambda: gateway_request(documents=[document(), dict(document(), service_id="more")]),
    "bindings not empty": lambda: gateway_request(bindings=[{"path": "/spec/ssl_cert", "secretRef": "gateway-tls", "serviceRef": "rgw.objects"}]),
    "endpoint not a name": lambda: gateway_request(endpoint="https://objects.example.test"),
    "realm not a label": lambda: gateway_request(realm="Objects"),
    "gateway on a pool action": lambda: dict(gateway_request(), action="pool", pool={"name": "volumes"}),
    "gateway on a cluster action": lambda: dict(gateway_request(), action="cluster"),
    "pool member on a gateway action": lambda: dict(gateway_request(), pool={"name": "volumes"}),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_gateway_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


def test_gateway_request_admits_the_frozen_shape():
    request = gateway_request(spec={"rgw_frontend_port": 8080, "rgw_frontend_type": "beast", "only_bind_port_on_networks": True,
                                    "rgw_exit_timeout_secs": 120}, endpoint="objects.ceph.example.test")
    request["gateway"]["documents"][0]["networks"] = ["192.0.2.0/24"]
    assert ceph_native.validate(request) is request


def test_dispatch_routes_a_gateway_to_the_object_worker(monkeypatch):
    reached = []
    for phase in ceph_native_object.APPLY_PHASES:
        monkeypatch.setattr(ceph_native_object.ObjectWorker, phase, lambda self, phase=phase: reached.append((type(self), phase)) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = gateway_request()
    for phase in ceph_native_object.APPLY_PHASES:
        assert ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": {}}) == {"changed": False}
    assert reached == [(ceph_native_object.ObjectWorker, phase) for phase in ceph_native_object.APPLY_PHASES]
    for phase in ("unit", "pool"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": {}})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: False)
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": request, "host": "node-1", "phase": "gateway_zone", "private": {}})


def observation(request, ready, daemons=(), resource=None):
    return {"version": ceph_native.EVIDENCE_VERSION, "request": "", "action": "gateway", "owner": OWNER, "fsid": FSID, "owned": True, "exists": True,
            "ready": ready, "removed": False, "partial": not ready, "health": "HEALTH_OK", "hosts": [h["name"] for h in request["hosts"]],
            "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": resource or {"id": "", "path": ""},
            "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
            "endpoint": {"certificateSHA256": "", "reachable": ready}, "daemons": list(daemons), "sso": ""}


class Transport:
    def __init__(self, request, ready_after=1, observed=None):
        self.request, self.calls, self.ready_after = request, [], ready_after
        self.observed = dict(observed or {})

    def call(self, host, phase, private=None, *, timeout=None):
        assert private is None
        self.calls.append((host["name"], phase))
        if phase == "observe":
            assert timeout is None or 0 < timeout <= 660
            ready = sum(call[1] == "observe" for call in self.calls) >= self.ready_after
            daemons = [{"host": "node-1", "name": "rgw.objects.node-1.abcdef", "service": "rgw.objects"}] if ready else []
            resource = {"id": str(uuid.UUID(int=7)), "path": ""} if ready else None
            return dict(observation(self.request, ready, daemons, resource), blocking=[] if ready else ["rgw@rgw.objects"], **self.observed)
        if phase == "prerequisites":
            return {"name": host["name"], "owned": True, "exists": True}
        return {"changed": phase in ceph_native_object.APPLY_PHASES}


def test_gateway_native_call_count_within_budget(monkeypatch):
    """The Go deadline budgets five worker round trips (one covers every ingress host's firewall check) and one readiness wait for an apply
    (internal/storage/ceph/gateway.go)."""
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = gateway_request()
    transport = Transport(request, ready_after=2)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["changed"] is True and result["evidence"]["ready"] is True
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "gateway_zone"), ("node-1", "gateway_placement"),
                               ("node-1", "gateway_service"), ("node-1", "observe"), ("node-1", "observe")]
    assert len([call for call in transport.calls if call[1] != "observe"]) == 4
    transport = Transport(request)
    assert ceph_core.execute(request, "observe", "d" * 64, transport)["changed"] is False and transport.calls == [("node-1", "observe")]
    removed = {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": [], "resource": {"id": "", "path": ""}}
    transport = Transport(request, observed=removed)
    assert ceph_core.execute(request, "destroy", "d" * 64, transport)["evidence"]["removed"] is True
    assert transport.calls == [("node-1", "destroy"), ("node-1", "observe")]


def test_gateway_readiness_timeout_names_the_backend(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = gateway_request()
    clock = iter(range(0, 100000, 300))
    transport = Transport(request, ready_after=99)
    with pytest.raises(ceph_core.ReadinessRefusal) as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    assert ceph_core.refusal(caught.value, ceph_core.CLUSTER_REASONS) == ("not-ready", "not-ready:rgw@rgw.objects")
    assert ceph_core.blocking_entry("http@rgw.objects") and not ceph_core.blocking_entry("http@ingress.objects")


def test_core_evidence_admits_gateway_resource_and_daemons():
    request = gateway_request(endpoint="objects.ceph.example.test")
    row = {"host": "node-1", "name": "rgw.objects.node-1.abcdef", "service": "rgw.objects"}
    resource = {"id": str(uuid.uuid4()), "path": "objects.ceph.example.test"}
    value = dict(observation(request, True, [row], resource), request="d" * 64)
    assert ceph_core.evidence(copy.deepcopy(value), "d" * 64, request)["resource"] == resource
    assert ceph_core.evidence(dict(value, resource={"id": "", "path": ""}), "d" * 64, request)
    other = gateway_request(endpoint="objects.ceph.example.test")
    other["gateway"]["documents"][0]["placement"]["hosts"] = ["node-1", "node-2"]
    other["delegated"] = [{"name": "rgw.objects", "hosts": ["node-1", "node-2"]}]
    for bad, against in ((dict(value, resource=dict(resource, path="other.example.test")), request),
                         (dict(value, resource=dict(resource, id="17")), request),
                         (dict(value, resource={"id": "", "path": "objects.ceph.example.test"}), request),
                         (dict(value, daemons=[dict(row, host="node-3")]), other),
                         (dict(value, daemons=[dict(row, service="rgw.other")]), request),
                         (dict(value, daemons=[dict(row, secret=SECRET_KEY)]), request)):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64, against)


def test_gateway_evidence_carries_the_endpoint_member(tmp_path, monkeypatch):
    markers, monitor = ready_gateway(tmp_path)
    ready, unused, evidence = observe(monitor, markers)
    assert ready is True and evidence["endpoint"] == {"certificateSHA256": "", "reachable": True}
    monkeypatch.setattr(ceph_native_object, "answers", lambda address, number: False)
    ready, blocking, evidence = observe(monitor, markers)
    assert ready is False and blocking == ["http@rgw.objects"] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}


def test_core_evidence_admits_only_a_gateway_endpoint():
    request = gateway_request(endpoint="objects.ceph.example.test")
    plain = dict(observation(request, True), request="d" * 64)
    assert ceph_core.evidence(copy.deepcopy(plain), "d" * 64, request)["endpoint"] == {"certificateSHA256": "", "reachable": True}
    pool = dict(observation(request, True), request="d" * 64, action="pool", endpoint={"certificateSHA256": "", "reachable": False})
    assert ceph_core.evidence(copy.deepcopy(pool), "d" * 64)["action"] == "pool"
    tls = copy.deepcopy(request)
    tls["gateway"]["bindings"] = [{"path": "/spec/ssl_cert", "secretRef": "gateway-tls", "serviceRef": "rgw.objects"},
                                  {"path": "/spec/ssl_key", "secretRef": "gateway-tls", "serviceRef": "rgw.objects"}]
    served = dict(plain, endpoint={"certificateSHA256": "a" * 64, "reachable": True})
    assert ceph_core.evidence(copy.deepcopy(served), "d" * 64, tls)["endpoint"]["certificateSHA256"] == "a" * 64
    for bad, against in ((dict(pool, endpoint={"certificateSHA256": "", "reachable": True}), None),
                         (dict(plain, endpoint={"certificateSHA256": "a" * 64, "reachable": True}), request),
                         (dict(plain, endpoint={"certificateSHA256": "", "reachable": True, "url": ""}), request),
                         (dict(plain, endpoint={"certificateSHA256": "A" * 64, "reachable": True}), tls),
                         (dict(plain, endpoint={"certificateSHA256": "", "reachable": "yes"}), request),
                         (dict(plain, endpoint={"certificateSHA256": "", "reachable": True}), tls)):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64, against)


def pem(kind, payload):
    """Synthetic PEM text around arbitrary bytes, in short lines no opaque-run redaction would catch."""
    body = base64.b64encode(payload).decode()
    return "-----BEGIN " + kind + "-----\n" + "\n".join(body[i:i + 24] for i in range(0, len(body), 24)) + "\n-----END " + kind + "-----\n"


LEAF = b"synthetic gateway leaf certificate for s3.example.test"
CERT = pem("CERTIFICATE", LEAF) + pem("CERTIFICATE", b"synthetic issuing authority")
KEY = pem("PRIVATE KEY", b"private key material only the gateway holds")
OTHER_CERT = pem("CERTIFICATE", b"another leaf the Secret does not hold")
OTHER_KEY = pem("PRIVATE KEY", b"another private key")
LEAF_SHA256 = hashlib.sha256(LEAF).hexdigest()
MARKS = [line for text in (CERT, KEY) for line in text.splitlines() if "-----" not in line]
ENDPOINT = "s3.example.test"
INGRESS = "ingress.rgw.objects"


def leaked(value):
    if isinstance(value, bytes):
        value = value.decode("utf-8", errors="replace")
    return [mark for mark in MARKS if mark in value or json.dumps(mark)[1:-1] in value]


def bindings(service, secret="gateway-tls"):
    return [{"path": "/spec/ssl_cert", "secretRef": secret, "serviceRef": service}, {"path": "/spec/ssl_key", "secretRef": secret, "serviceRef": service}]


def ingress_document(**spec):
    value = {"backend_service": "rgw.objects", "frontend_port": 443, "monitor_port": 1967, "ssl": True, "virtual_ip": "192.0.2.50/24"}
    value.update(spec)
    return {"placement": {"count": 2, "hosts": list(HOSTS)}, "service_id": "rgw.objects", "service_type": "ingress", "spec": value}


def tls_request(ingress=False, ssl=True):
    """Direct TLS on the rgw service, or an ingress in front of plain rgw, terminating TLS when ssl."""
    if not ingress:
        return gateway_request(spec={"ssl": True}, endpoint=ENDPOINT, bindings=bindings("rgw.objects"))
    request = gateway_request(endpoint=ENDPOINT, bindings=bindings(INGRESS) if ssl else [])
    request["gateway"]["documents"].append(ingress_document() if ssl else ingress_document(ssl=False))
    request["delegated"] = [{"name": INGRESS, "hosts": list(HOSTS)}, {"name": "rgw.objects", "hosts": list(HOSTS)}]
    request["auxiliary"] = copy.deepcopy(settings.AUXILIARY)
    return request


def images_held(monitor):
    monitor.config = [settings.entry(settings.HAPROXY_KEY, settings.HAPROXY), settings.entry(settings.KEEPALIVED_KEY, settings.KEEPALIVED)]
    return monitor


def tls_worker(monitor, markers, phase="observe"):
    native = worker(monitor, markers)
    if monitor.request["gateway"]["bindings"]:
        native.tls = {"gateway_service": {"gatewayCertificate": CERT, "gatewayKey": KEY}, "observe": {"gatewayCertificate": CERT}}.get(phase, {})
    return native


def apply_tls(monitor, markers):
    return [getattr(tls_worker(monitor, markers, phase), phase)() for phase in ceph_native_object.APPLY_PHASES]


def ingress_daemon(request, kind, host, image, status=1):
    return {"daemon_type": kind, "daemon_id": "rgw.objects." + host + ".xyzabc", "hostname": host, "service_name": INGRESS, "status": status,
            "container_image_digests": [image]}


def ingress_rows(request, image=None):
    return [ingress_daemon(request, kind, host, image or {"haproxy": settings.HAPROXY, "keepalived": settings.KEEPALIVED}[kind])
            for kind in ("haproxy", "keepalived") for host in ("node-1", "node-2")]


def ready_tls(tmp_path, request):
    markers = markers_for(tmp_path, request)
    monitor = images_held(Monitor(request))
    apply_tls(monitor, markers)
    monitor.running = [daemon(request, host) for host in ("node-1", "node-2")]
    if len(request["gateway"]["documents"]) == 2:
        monitor.running += ingress_rows(request)
    return markers, monitor


def observe_tls(monitor, markers):
    native = tls_worker(monitor, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def with_documents(request, change):
    change(request["gateway"]["documents"], request)
    return request


TLS_INVALID = {
    "one leaf only": lambda: gateway_request(spec={"ssl": True}, endpoint=ENDPOINT, bindings=bindings("rgw.objects")[:1]),
    "two Secrets": lambda: gateway_request(spec={"ssl": True}, endpoint=ENDPOINT,
                                           bindings=[bindings("rgw.objects")[0], bindings("rgw.objects", "other-tls")[1]]),
    "binding on a service without ssl": lambda: gateway_request(endpoint=ENDPOINT, bindings=bindings("rgw.objects")),
    "ssl without bindings": lambda: gateway_request(spec={"ssl": True}, endpoint=ENDPOINT),
    "rgw ssl with an ingress": lambda: with_documents(tls_request(True), lambda documents, request: documents[0]["spec"].update(ssl=True)),
    "ingress of another rgw": lambda: with_documents(tls_request(True), lambda documents, request: documents[1]["spec"].update(backend_service="rgw.other")),
    "keepalive_only ingress": lambda: with_documents(tls_request(True), lambda documents, request: documents[1]["spec"].update(keepalive_only=True)),
    "bindings with an empty endpoint": lambda: gateway_request(spec={"ssl": True}, bindings=bindings("rgw.objects")),
    "cephadm-signed source": lambda: gateway_request(spec={"ssl": True, "certificate_source": "cephadm-signed"}, endpoint=ENDPOINT,
                                                     bindings=bindings("rgw.objects")),
    "ssl_cert in a frozen spec": lambda: gateway_request(spec={"ssl": True, "ssl_cert": CERT}, endpoint=ENDPOINT, bindings=bindings("rgw.objects")),
    "ingress without its haproxy image": lambda: with_documents(tls_request(True), lambda documents, request: request.update(
        auxiliary=[entry for entry in settings.AUXILIARY if entry["name"] != "haproxy"])),
    "ingress without a monitor port": lambda: with_documents(tls_request(True), lambda documents, request: documents[1]["spec"].pop("monitor_port")),
    "ingress virtual IP without a prefix": lambda: with_documents(tls_request(True), lambda documents, request: documents[1]["spec"].update(
        virtual_ip="192.0.2.50")),
    "ingress not in delegated": lambda: with_documents(tls_request(True), lambda documents, request: request.update(delegated=request["delegated"][1:])),
    "bindings out of order": lambda: gateway_request(spec={"ssl": True}, endpoint=ENDPOINT, bindings=bindings("rgw.objects")[::-1]),
    "ingress bound while rgw terminates": lambda: with_documents(tls_request(True), lambda documents, request: request["gateway"].update(
        bindings=bindings("rgw.objects"))),
}


@pytest.mark.parametrize("case", sorted(TLS_INVALID))
def test_gateway_tls_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(TLS_INVALID[case]())


def test_gateway_tls_request_admits_the_frozen_shapes():
    direct = gateway_request(spec={"ssl": True, "certificate_source": "inline", "rgw_frontend_port": 8443}, endpoint=ENDPOINT, bindings=bindings("rgw.objects"))
    assert ceph_native.validate(direct) is direct
    fronted = tls_request(True)
    fronted["gateway"]["documents"][1]["spec"].update(certificate_source="inline", first_virtual_router_id=50, health_check_interval="2s",
                                                      virtual_interface_networks=["192.0.2.0/24"])
    assert ceph_native.validate(fronted) is fronted
    plain = tls_request(True, ssl=False)
    assert ceph_native.validate(plain) is plain


def test_gateway_private_material_is_admitted_per_phase(monkeypatch):
    reached = []
    for phase in ceph_native_object.APPLY_PHASES:
        monkeypatch.setattr(ceph_native_object.ObjectWorker, phase, lambda self, phase=phase: reached.append((phase, self.tls)) or {"changed": False})
    monkeypatch.setattr(ceph_native_object.ObjectWorker, "observe", lambda self: reached.append(("observe", self.tls)) or {})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = tls_request()

    def dispatch(phase, private):
        return ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": private})

    both, certificate = {"gatewayCertificate": CERT, "gatewayKey": KEY}, {"gatewayCertificate": CERT}
    dispatch("gateway_service", both)
    dispatch("observe", certificate)
    dispatch("gateway_zone", {})
    assert reached == [("gateway_service", both), ("observe", certificate), ("gateway_zone", {})]
    for phase, private in (("gateway_service", certificate), ("observe", both), ("gateway_zone", certificate), ("gateway_placement", both),
                           ("gateway_service", dict(both, gatewayCertificate="not a certificate")),
                           ("gateway_service", dict(both, gatewayKey="-----BEGIN CERTIFICATE-----")),
                           ("observe", {"gatewayCertificate": "-----BEGIN CERTIFICATE-----\n!!!!\n-----END CERTIFICATE-----\n"}),
                           ("observe", dict(certificate, extra=CERT))):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            dispatch(phase, private)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch({"request": gateway_request(), "host": "node-1", "phase": "observe", "private": certificate})


def test_ingress_ssl_material_spliced_only_in_memory(tmp_path, monkeypatch):
    contained = []
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained",
                        lambda self, image, user, script, arguments=(), **options: contained.append(arguments) or json.loads(arguments[0]))
    request = tls_request(True)
    markers = markers_for(tmp_path, request)
    monitor = images_held(Monitor(request))
    assert apply_tls(monitor, markers)[-1] == {"changed": True}
    applies = [data for tool, command, data in monitor.log if command[:2] == ("orch", "apply")]
    assert len(applies) == 1 and leaked(applies[0]) == MARKS
    stream = [json.loads(part) for part in applies[0].split(b"---\n")]
    assert [value["service_type"] for value in stream] == ["rgw", "ingress"]
    assert stream[1]["spec"]["ssl_cert"] == CERT and stream[1]["spec"]["ssl_key"] == KEY and "ssl_cert" not in stream[0]["spec"]
    assert not any(leaked(" ".join(command)) for tool, command, unused in monitor.log)
    assert contained and not any(leaked(" ".join(arguments)) for arguments in contained)
    for root, unused, files in os.walk(str(tmp_path)):
        for name in files:
            with open(os.path.join(root, name), "rb") as handle:
                assert not leaked(handle.read()), name
    placeholders = {"ssl_cert": "bootwright-bound:/spec/ssl_cert", "ssl_key": "bootwright-bound:/spec/ssl_key"}
    for name in ("service-" + INGRESS + "-attempt.json", "service-" + INGRESS + "-complete.json"):
        assert {key: markers.read(name)["service"]["spec"][key] for key in placeholders} == placeholders
    inputs = markers.read("gateway-objects-inputs.json")
    assert {key: inputs["services"][1][0]["spec"][key] for key in placeholders} == placeholders


@pytest.mark.parametrize("exported", ["with leaves", "without leaves"])
def test_gateway_replay_after_apply_before_endpoint_ready(tmp_path, exported):
    request = tls_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_tls(monitor, markers)
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    if exported == "without leaves":
        for key in ("ssl_cert", "ssl_key"):
            del monitor.specs["rgw.objects"]["spec"][key]
    monitor.mutations.clear()
    assert tls_worker(monitor, markers, "gateway_service").gateway_service() == {"changed": False}
    assert monitor.mutations == [] and markers.read("service-rgw.objects-complete.json") is not None


@pytest.mark.parametrize("stored", ["other certificate", "other key", "certificate only", "key only"])
def test_stored_certificate_other_than_bound_refuses(tmp_path, stored):
    request = tls_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    apply_tls(monitor, markers)
    spec = monitor.specs["rgw.objects"]["spec"]
    {"other certificate": lambda: spec.update(ssl_cert=OTHER_CERT), "other key": lambda: spec.update(ssl_key=OTHER_KEY),
     "certificate only": lambda: spec.pop("ssl_key"), "key only": lambda: spec.pop("ssl_cert")}[stored]()
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    monitor.mutations.clear()
    with pytest.raises(ceph_native.NativeFailure, match="^object-endpoint-certificate$") as caught:
        tls_worker(monitor, markers, "gateway_service").gateway_service()
    assert caught.value.native_error["cause"] == "rgw.objects" and monitor.mutations == []
    if stored != "other key":
        monitor.running = [daemon(request, host) for host in ("node-1", "node-2")]
        with pytest.raises(ceph_native.NativeFailure, match="^object-endpoint-certificate$") as caught:
            observe_tls(monitor, markers)
        assert caught.value.native_error["cause"] == "rgw.objects"


@pytest.mark.parametrize("ingress", [False, True])
def test_endpoint_fingerprint_must_equal_bound_certificate(tmp_path, monkeypatch, ingress):
    request = tls_request(ingress)
    markers, monitor = ready_tls(tmp_path, request)
    probes, replies = [], []
    monkeypatch.setattr(ceph_native_object, "served", lambda address, number, name: probes.append((address, number, name)) or replies.pop(0))
    targets = [("192.0.2.50", 443, ENDPOINT)] if ingress else [("192.0.2.1", 443, ENDPOINT), ("192.0.2.2", 443, ENDPOINT)]
    service = INGRESS if ingress else "rgw.objects"
    other = b"another served leaf"
    replies[:] = [(other, True)] * len(targets)
    ready, blocking, evidence = observe_tls(monitor, markers)
    assert ready is False and blocking == ["tls@" + service] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}
    assert probes == targets
    replies[:] = [(LEAF, True)] * len(targets)
    ready, blocking, evidence = observe_tls(monitor, markers)
    assert ready is True and blocking == [] and evidence["endpoint"] == {"certificateSHA256": LEAF_SHA256, "reachable": True}
    replies[:] = [(LEAF, False)] * len(targets)
    ready, blocking, evidence = observe_tls(monitor, markers)
    assert ready is False and blocking == [("vip@" if ingress else "http@") + service] and evidence["endpoint"]["reachable"] is False
    assert all(ceph_core.blocking_entry(entry) for entry in ("tls@rgw.objects", "tls@" + INGRESS, "vip@" + INGRESS, "haproxy@" + INGRESS))


@pytest.mark.parametrize("config", ["foreign haproxy", "missing keepalived"])
def test_gateway_refuses_ingress_when_image_keys_differ(tmp_path, config):
    request = tls_request(True)
    markers = markers_for(tmp_path, request)
    monitor = images_held(Monitor(request))
    if config == "foreign haproxy":
        monitor.config[0]["value"] = "registry.example.test/ceph/haproxy@sha256:" + "d" * 64
    else:
        monitor.config = monitor.config[:1]
    before = records(markers)
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        tls_worker(monitor, markers, "gateway_zone").gateway_zone()
    assert caught.value.native_error["cause"] == (settings.HAPROXY_KEY if config == "foreign haproxy" else settings.KEEPALIVED_KEY)
    assert records(markers) == before and monitor.radosgw_calls() == [] and monitor.mutations == []
    held = images_held(Monitor(request))
    apply_tls(held, markers_for(tmp_path / "held", request))
    assert not any(command[:1] == ("config",) and command[1:2] != ("dump",) for unused, command, unused in held.log)


def test_ingress_daemons_run_on_the_frozen_auxiliary_images(tmp_path):
    request = tls_request(True, ssl=False)
    markers, monitor = ready_tls(tmp_path, request)
    ready, blocking, evidence = observe(monitor, markers)
    assert ready is True and blocking == [] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": True}
    assert PROBES[-1] == ("192.0.2.50", 443)
    assert {row["service"] for row in evidence["daemons"]} == {"rgw.objects", INGRESS}
    monitor.running[-1]["container_image_digests"] = ["registry.example.test/ceph/keepalived@sha256:" + "d" * 64]
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        observe(monitor, markers)
    monitor.running[-1]["container_image_digests"] = [settings.KEEPALIVED]
    monitor.running = [row for row in monitor.running if not (row["daemon_type"] == "haproxy" and row["hostname"] == "node-2")]
    ready, blocking, unused = observe(monitor, markers)
    assert ready is False and blocking == ["haproxy@" + INGRESS]


def test_gateway_tls_material_never_reaches_a_refusal_detail(tmp_path):
    request = tls_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    monitor.apply_error = lambda data: b"Error EINVAL: cannot parse " + data
    tls_worker(monitor, markers, "gateway_zone").gateway_zone()
    tls_worker(monitor, markers, "gateway_placement").gateway_placement()
    with pytest.raises(ceph_native.NativeFailure, match="^native-command$") as caught:
        tls_worker(monitor, markers, "gateway_service").gateway_service()
    assert caught.value.native_error["cause"].startswith("Error EINVAL: cannot parse") and not leaked(json.dumps(caught.value.native_error))

    def write(name, data):
        path = str(tmp_path / name)
        ceph_native.private_write(path, data.encode())
        return path

    material = {"sshKey": write("identity", "fleet-private"), "knownHosts": write("known", "192.0.2.1 ssh-ed25519 AAAA\n"),
                "gatewayCertificate": write("certificate", CERT), "gatewayKey": write("key", KEY)}
    flattened = " ".join(KEY.splitlines())
    sent = []

    def run(argv, data=b"", timeout=None):
        del timeout
        sent.append((argv, data))
        return 0, json.dumps({"ok": False, "reason": "native-refusal",
                              "nativeError": {"coordinate": "registry.example.test/ceph", "cause": "orch apply failed: " + flattened}}).encode()

    transport = ceph_core.Transport(request, material, run=run)
    with pytest.raises(ceph_native.RemoteRefusal) as caught:
        transport.call(request["hosts"][0], "gateway_service", transport.gateway_material(True))
    assert caught.value.native_error["cause"].startswith("orch apply failed:") and not leaked(caught.value.native_error["cause"])
    assert not leaked(" ".join(sent[0][0])) and leaked(sent[0][1]) == MARKS


class TLSTransport(Transport):
    """The coordinator's view: which private material each phase received."""

    def __init__(self, request, digest=LEAF_SHA256, **options):
        super().__init__(request, **options)
        self.private, self.digest = [], digest

    def gateway_material(self, key):
        return {"gatewayCertificate": CERT, "gatewayKey": KEY} if key else {"gatewayCertificate": CERT}

    def call(self, host, phase, private=None, *, timeout=None):
        self.private.append((phase, sorted(private or {})))
        result = super().call(host, phase, timeout=timeout)
        if phase == "observe":
            result["endpoint"] = {"certificateSHA256": self.digest if result["ready"] else "", "reachable": result["ready"]}
            result["resource"] = dict(result["resource"], path=ENDPOINT) if result["ready"] else result["resource"]
        return result


def test_execute_gateway_passes_bound_material_per_phase(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = tls_request()
    transport = TLSTransport(request, ready_after=2)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["endpoint"] == {"certificateSHA256": LEAF_SHA256, "reachable": True}
    certificate = ["gatewayCertificate"]
    assert transport.private == [("prerequisites", []), ("gateway_zone", []), ("gateway_placement", []),
                                 ("gateway_service", ["gatewayCertificate", "gatewayKey"]), ("observe", certificate), ("observe", certificate)]
    assert len([call for call in transport.calls if call[1] != "observe"]) == 4
    transport = TLSTransport(request)
    ceph_core.execute(request, "observe", "d" * 64, transport)
    assert transport.private == [("observe", certificate)]
    removed = {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": [], "resource": {"id": "", "path": ""}}
    transport = TLSTransport(request, observed=removed)
    ceph_core.execute(request, "destroy", "d" * 64, transport)
    assert transport.private == [("destroy", certificate), ("observe", certificate)]
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.execute(request, "observe", "d" * 64, TLSTransport(request, digest="a" * 64))


def test_transport_admits_the_gateway_material(tmp_path):
    def write(name, data=None):
        path = str(tmp_path / name)
        ceph_native.private_write(path, name.encode() if data is None else data)
        return path

    request = gateway_request()
    material = {"sshKey": write("identity"), "knownHosts": write("known", b"192.0.2.1 ssh-ed25519 PUBLIC"), "gatewayCertificate": write("certificate"),
                "gatewayKey": write("key")}
    calls = []

    def run(argv, data=b"", timeout=None):
        calls.append(argv)
        return 0, json.dumps({"ok": True, "result": {"changed": False}}).encode()

    assert ceph_core.Transport(request, material, run=run).call(request["hosts"][0], "gateway_zone") == {"changed": False}
    assert ceph_core.Transport(request, material, run=run).gateway_material(True) == {"gatewayCertificate": "certificate", "gatewayKey": "key"}
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.Transport(request, dict(material, gatewayChain=write("chain")), run=run).call(request["hosts"][0], "gateway_zone")
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.Transport(request, {"sshKey": material["sshKey"], "knownHosts": material["knownHosts"]}, run=run).gateway_material(False)
    assert len(calls) == 1

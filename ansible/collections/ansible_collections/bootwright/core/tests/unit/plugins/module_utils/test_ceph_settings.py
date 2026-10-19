"""The settings block: frozen auxiliary image settings and derived manager modules, written once and never overwritten.

The scripted monitor answers the upstream v20.2.1 shapes the worker reads:
- `config dump` lists one object per section and option with `section`, then the masked option's `name`, `value`, `level`,
  `can_update_at_runtime` and `mask`, and `location_type`/`location_value` when masked (src/mon/ConfigMonitor.cc:242-291,
  src/mon/ConfigMap.cc:73-81);
- cephadm registers one manager option per default image, `mgr/cephadm/container_image_<name>`
  (src/pybind/mgr/cephadm/module.py:499-500; src/python-common/ceph/cephadm/images.py:17), set with
  `ceph config set mgr <option> <value>` (doc/cephadm/services/monitoring.rst:190-225);
- `mgr module ls` lists `always_on_modules`, `force_disabled_modules` and `enabled_modules` as string arrays, the enabled ones
  without the always-on ones (src/mon/MgrMonitor.cc:1016-1050); `mgr module enable` of an enabled module succeeds without effect
  (src/mon/MgrMonitor.cc:1279-1318).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_resources

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
OWNER = "apply-123"
HAPROXY = "registry.example.test/ceph/haproxy@sha256:" + "b" * 64
KEEPALIVED = "registry.example.test/ceph/keepalived@sha256:" + "c" * 64
AUXILIARY = [{"image": HAPROXY, "name": "haproxy"}, {"image": KEEPALIVED, "name": "keepalived"}]
HAPROXY_KEY = "mgr/cephadm/container_image_haproxy"
KEEPALIVED_KEY = "mgr/cephadm/container_image_keepalived"


def cluster():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [h["name"] for h in hosts]
    return {"controller": "controller", "version": "ceph-core-v16", "action": "cluster", "cluster": "example", "owner": OWNER, "fsid": FSID,
            "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible",
            "packageRepository": "https://packages.example.test/native", "bootstrap": "node-1",
            "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key",
            "registryRef": "registry-key", "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"],
            "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


def settings_request(modules=("nfs",), auxiliary=None):
    request = dict(cluster(), action="settings", settings={"modules": list(modules)})
    request["auxiliary"] = AUXILIARY if auxiliary is None else auxiliary
    if not request["auxiliary"]:
        del request["auxiliary"]
    return request


def entry(name, value, section="mgr", **extra):
    return dict({"section": section, "name": name, "value": value, "level": "advanced", "can_update_at_runtime": True, "mask": ""}, **extra)


class Monitor:
    """A scripted monitor: reads answer from its tables, every other command is a logged effect it applies."""

    def __init__(self, config=None, enabled=("iostat",), always_on=("balancer", "crash")):
        self.config = list(config or [])
        self.enabled, self.always_on = list(enabled), list(always_on)
        self.log, self.mutations = [], []

    def run(self, argv, data=b"", timeout=60):
        del data, timeout
        assert "--force" not in argv
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append(command)
        if command == ("config", "dump"):
            return 0, json.dumps(self.config).encode()
        if command == ("mgr", "module", "ls"):
            return 0, json.dumps({"always_on_modules": self.always_on, "force_disabled_modules": [], "enabled_modules": self.enabled,
                                  "disabled_modules": []}).encode()
        self.mutations.append(command)
        if command[:3] == ("config", "set", "mgr"):
            self.config.append(entry(command[3], command[4]))
        elif command[:3] == ("mgr", "module", "enable") and command[3] not in self.enabled + self.always_on:
            self.enabled.append(command[3])
        return 0, b""


class Markers:
    path = "/private/cluster"

    @staticmethod
    def owned():
        return True

    @staticmethod
    def read(name="owner.json"):
        del name

    def write(self, name, value):
        raise AssertionError("the settings block keeps no record: " + name + repr(value))


def worker(monitor, request=None):
    return ceph_native_resources.ResourceWorker(request or settings_request(), "node-1", run=monitor.run, markers=Markers())


def test_settings_writes_each_absent_frozen_image_key_in_name_order():
    monitor = Monitor(enabled=("iostat", "nfs"))
    assert worker(monitor).settings() == {"changed": True}
    assert monitor.mutations == [("config", "set", "mgr", HAPROXY_KEY, HAPROXY), ("config", "set", "mgr", KEEPALIVED_KEY, KEEPALIVED)]
    monitor.mutations = []
    assert worker(monitor).settings() == {"changed": False}
    assert monitor.mutations == []


def test_settings_writes_only_the_absent_key_beside_a_held_one():
    monitor = Monitor([entry(HAPROXY_KEY, HAPROXY)], enabled=("nfs",))
    assert worker(monitor).settings() == {"changed": True}
    assert monitor.mutations == [("config", "set", "mgr", KEEPALIVED_KEY, KEEPALIVED)]


FOREIGN = {
    "other value": [entry(HAPROXY_KEY, "registry.example.test/ceph/haproxy@sha256:" + "d" * 64)],
    "global section": [entry(HAPROXY_KEY, HAPROXY, section="global")],
    "daemon section": [entry(HAPROXY_KEY, HAPROXY, section="mgr.node-1")],
    "mask": [dict(entry(HAPROXY_KEY, HAPROXY), mask="host:node-1", location_type="host", location_value="node-1")],
    "location": [entry(HAPROXY_KEY, HAPROXY, location_type="host")],
    "duplicate": [entry(HAPROXY_KEY, HAPROXY), entry(HAPROXY_KEY, HAPROXY)],
}


@pytest.mark.parametrize("name", sorted(FOREIGN))
def test_settings_refuses_foreign_image_value_without_overwrite(name):
    monitor = Monitor(FOREIGN[name], enabled=())
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        worker(monitor).settings()
    assert caught.value.native_error["cause"] == HAPROXY_KEY
    assert "sha256" not in caught.value.native_error["cause"]
    assert monitor.mutations == []
    assert ("mgr", "module", "ls") not in monitor.log


class ForeignMarkers(Markers):
    @staticmethod
    def owned():
        return False


def test_settings_refuses_a_cluster_this_operation_does_not_own_before_any_read_or_write():
    monitor = Monitor()
    native = ceph_native_resources.ResourceWorker(settings_request(), "node-1", run=monitor.run, markers=ForeignMarkers())
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.settings()
    assert monitor.log == []
    assert monitor.mutations == []


def test_settings_refusal_names_every_foreign_key_and_no_value():
    monitor = Monitor([entry(KEEPALIVED_KEY, "x", section="global"), entry(HAPROXY_KEY, "y")])
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        worker(monitor).settings()
    assert caught.value.native_error["cause"] == HAPROXY_KEY + ", " + KEEPALIVED_KEY


def test_settings_enables_derived_nfs_module_when_an_nfs_service_is_declared():
    held = [entry(HAPROXY_KEY, HAPROXY), entry(KEEPALIVED_KEY, KEEPALIVED)]
    monitor = Monitor(held, enabled=("iostat",))
    assert worker(monitor).settings() == {"changed": True}
    assert monitor.mutations == [("mgr", "module", "enable", "nfs")]
    for enabled, always_on in ((("iostat", "nfs"), ()), ((), ("nfs",))):
        monitor = Monitor(held, enabled=enabled, always_on=always_on)
        assert worker(monitor).settings() == {"changed": False}
        assert monitor.mutations == []
    monitor = Monitor(held, enabled=())
    assert worker(monitor, settings_request(modules=())).settings() == {"changed": False}
    assert monitor.mutations == []


def test_settings_proves_its_writes_took_effect():
    class Ignoring(Monitor):
        def run(self, argv, data=b"", timeout=60):
            command = tuple(argv[argv.index("--format") + 2:])
            if command[:3] == ("mgr", "module", "enable"):
                self.log.append(command)
                self.mutations.append(command)
                return 0, b""
            return super().run(argv, data, timeout)

    monitor = Ignoring([entry(HAPROXY_KEY, HAPROXY), entry(KEEPALIVED_KEY, KEEPALIVED)], enabled=())
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        worker(monitor).settings()


def test_settings_observe_reads_only():
    monitor = Monitor([entry(HAPROXY_KEY, HAPROXY)], enabled=("nfs",))
    native = worker(monitor)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is False
    assert monitor.mutations == []
    assert evidence["resource"] == {"id": "", "path": ""} and evidence["client"] == ""
    monitor = Monitor([entry(HAPROXY_KEY, HAPROXY), entry(KEEPALIVED_KEY, KEEPALIVED)], enabled=("nfs",))
    native = worker(monitor)
    assert native.observe_resource(native.empty_evidence(), True) is True
    assert native.observe_resource(native.empty_evidence(), False) is False
    assert monitor.mutations == []
    monitor = Monitor([entry(HAPROXY_KEY, HAPROXY), entry(KEEPALIVED_KEY, KEEPALIVED)], enabled=())
    native = worker(monitor)
    assert native.observe_resource(native.empty_evidence(), True) is False
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$"):
        worker(Monitor([entry(HAPROXY_KEY, "other")])).observe_resource({}, True)


@pytest.mark.parametrize("change", [
    lambda r: r.pop("settings"),
    lambda r: r.update(settings={"modules": ["smb"]}),
    lambda r: r.update(settings={"modules": ["nfs", "nfs"]}),
    lambda r: r.update(settings={"modules": "nfs"}),
    lambda r: r.update(settings={"modules": ["nfs"], "images": []}),
    lambda r: r.update(settings={"modules": []}) or r.pop("auxiliary"),
    lambda r: r.update(pool={"name": "volumes"}),
])
def test_settings_request_validation(change):
    request = settings_request()
    assert ceph_native.validate(request) is request
    assert ceph_native.validate(settings_request(modules=())) is not None
    assert ceph_native.validate(settings_request(auxiliary=[])) is not None
    change(request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


@pytest.mark.parametrize("action", ["cluster", "pool"])
def test_a_non_settings_action_never_carries_settings(action):
    request = dict(cluster(), action=action, settings={"modules": ["nfs"]})
    if action == "pool":
        request["pool"] = {"name": "volumes"}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


def test_a_host_scope_never_carries_settings():
    scope = {key: value for key, value in cluster().items() if key != "action"}
    assert ceph_native.validate_scope(dict(scope)) is not None
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate_scope(dict(scope, settings={"modules": ["nfs"]}))


def evidence_of(request, ready, resource=None):
    return {"version": "ceph-core-evidence-v6", "request": "d" * 64, "action": request["action"], "owner": request["owner"], "fsid": request["fsid"],
            "owned": True, "exists": True, "ready": ready, "removed": False, "partial": not ready, "health": "HEALTH_OK",
            "hosts": [h["name"] for h in request["hosts"]], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [],
            "resource": resource or {"id": "", "path": ""}, "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
            "endpoint": {"certificateSHA256": "", "reachable": False}, "daemons": []}


def test_core_execute_dispatches_the_settings_phase(monkeypatch):
    request = settings_request()
    calls = []

    class Transport:
        def call(self, host, phase, timeout=None):
            del timeout
            calls.append((host["name"], phase))
            if phase == "observe":
                return dict(evidence_of(request, ready=("node-1", "settings") in calls), request="")
            if phase == "host_state":
                return {"name": host["name"], "owned": True, "exists": True}
            return {"changed": phase == "settings"}

    clock = [0.0]

    def pause(seconds):
        clock[0] += seconds

    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    result = ceph_core.execute(request, "apply", "d" * 64, Transport(), now=lambda: clock[0], pause=pause)
    assert result["changed"] and result["evidence"]["ready"] and result["evidence"]["action"] == "settings"
    assert [call for call in calls if call[1] == "settings"] == [("node-1", "settings")]
    assert calls.index(("node-1", "settings")) > calls.index(("node-1", "observe"))
    assert ceph_core.evidence(evidence_of(request, True), "d" * 64, request)["action"] == "settings"
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(evidence_of(request, True, {"id": "7", "path": ""}), "d" * 64, request)

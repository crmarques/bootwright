"""The remote SMB worker: passwords only on stdin, masked readback, exact continuation, declared services and the generated daemons.

Every request, record and native answer here is a synthetic fixture. The scripted monitor answers the shapes the worker reads:
- `smb apply -i -` loads the resources on stdin and answers a result group `{"results": [...], "success": bool}`, each result
  `{"resource": ..., "success": bool, "msg"?: str}`, exiting -EAGAIN unless every result succeeded; an invalid resource is answered
  with its raw input (src/pybind/mgr/smb/module.py:145-193, src/pybind/mgr/smb/results.py:29-37, :68-88, :131-145, v20.2.1);
- `smb show <name> --password-filter=hidden` answers the one matched resource itself, otherwise `{"resources": [...]}`, each
  password as sixteen asterisks (module.py:389-412, src/pybind/mgr/smb/resources.py:37, :770-782); a resource prints its unset
  optional members as null and its defaults filled (resources.py:135-142, :180-183, :216-236, :514-534, :617-658);
- `smb cluster ls` lists the SMB cluster IDs (module.py:195-198);
- `orch ps` rows as DaemonDescription.to_json emits them (src/pybind/mgr/orchestrator/_interface.py:1471-1500), cephadm's daemon type
  for SMB being `smb` (src/cephadm/cephadmlib/daemons/smb.py:552-555);
- `config dump` and `mgr module ls` as the settings block's tests describe them (test_ceph_settings.py).
"""

from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_native, ceph_native_nfs, ceph_native_observe, ceph_native_worker, ceph_smb_native, ceph_tunnel_staging,
)

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
OWNER = "apply-123"
DIGEST = "d" * 64
SAMBA = "registry.example.test/example/samba@sha256:" + "b" * 64
METRICS = "registry.example.test/example/samba-metrics@sha256:" + "c" * 64
IMAGES = [{"image": SAMBA, "name": "samba"}, {"image": METRICS, "name": "samba_metrics"}]
SAMBA_KEY = "mgr/cephadm/container_image_samba"
METRICS_KEY = "mgr/cephadm/container_image_samba_metrics"
JOIN_SECRET = "JOIN-PASSWORD-SENTINEL"
USER_SECRETS = ["USER-PASSWORD-SENTINEL-0", "USER-PASSWORD-SENTINEL-1"]
NATIVE_SENTINEL = "NATIVE-MESSAGE-SENTINEL"
TYPES = sorted(ceph_smb_native.TYPES)


def scope(transport="native"):
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/sda", "serial": "drive-scsi" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v20", "cluster": "example", "owner": OWNER, "fsid": FSID,
            "image": "registry.example.test/example/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "synthetic-package"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible", "packageRepository": "https://publisher.example.test/ceph",
            "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": transport,
            "clusterKeyRef": "cluster-key", "registryRef": "registry-key", "registry": "registry.example.test", "callHome": "disabled",
            "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts, "auxiliary": copy.deepcopy(IMAGES),
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/sda"]}}}]}


def service(hosts=("node-1", "node-2"), clustered=False):
    return {"clustered": clustered, "hosts": list(hosts), "name": "smb.files"}


def request(resource_type, transport="native", **served):
    """One completed request of a resource type, as the Go SMB capability completes it."""
    base = {"cluster": "example", "controller": "controller", "machines": ["machine-1", "machine-2", "machine-3"],
            "object": "smb-" + resource_type.split(".")[-1], "resourceType": resource_type, "scope": scope(transport), "version": "ceph-smb-v1",
            "bindings": []}
    if resource_type == "ceph.smb.cluster":
        native = {"resource_type": resource_type, "cluster_id": "files", "auth_mode": "user", "placement": {"count": 2, "hosts": ["node-2", "node-1"]},
                  "user_group_settings": [{"ref": "users"}]}
        return dict(base, id="files", native=native, service=served.get("service", service()), clusterIDs=["files"], images=copy.deepcopy(IMAGES))
    if resource_type == "ceph.smb.share":
        native = {"resource_type": resource_type, "cluster_id": "files", "share_id": "docs", "cephfs": {"volume": "files", "subvolume": "docs"}}
        return dict(base, id="docs", native=native, service=served.get("service", service()), clusterIDs=["files"], images=copy.deepcopy(IMAGES))
    if resource_type == "ceph.smb.join.auth":
        native = {"resource_type": resource_type, "auth_id": "join", "auth": {"username": "operator"}}
        return dict(base, id="join", native=native, bindings=[{"path": "/auth/password", "secretRef": "join-password"}])
    native = {"resource_type": resource_type, "users_groups_id": "users", "values": {"users": [{"name": "smbuser"}, {"name": "other"}], "groups": []}}
    bindings = [{"path": "/values/users/0/password", "secretRef": "smb-user-password"}, {"path": "/values/users/1/password", "secretRef": "smb-other-password"}]
    return dict(base, id="users", native=native, bindings=bindings)


def private_of(value):
    if value["resourceType"] == "ceph.smb.join.auth":
        return {"smbSecret0": JOIN_SECRET}
    if value["resourceType"] == "ceph.smb.usersgroups":
        return {"smbSecret0": USER_SECRETS[0], "smbSecret1": USER_SECRETS[1]}
    return {}


def name_of(value):
    native = value["native"]
    prefix = native["cluster_id"] + "." if value["resourceType"] == "ceph.smb.share" else ""
    return value["resourceType"] + "." + prefix + value["id"]


def masked(value):
    if isinstance(value, dict):
        return {key: ceph_smb_native.MASK if key == "password" else masked(item) for key, item in value.items()}
    if isinstance(value, list):
        return [masked(item) for item in value]
    return value


def simplified(resource):
    """The resource as the manager prints it: defaults filled, unset optional members null (resources.py:135-142, :216-236, :514-534)."""
    value = dict(copy.deepcopy(resource), intent="present")
    kind = value["resource_type"]
    if kind == "ceph.smb.share":
        value.update(name=value.get("name") or value["share_id"], readonly=value.get("readonly", False), browseable=value.get("browseable", True),
                     restrict_access=value.get("restrict_access", False), comment=None, max_connections=None, custom_smb_share_options=None,
                     login_control=None)
        value["cephfs"] = dict({"path": "/", "subvolumegroup": "", "subvolume": "", "provider": "samba-vfs"}, **value["cephfs"])
    elif kind == "ceph.smb.cluster":
        value.update(custom_smb_global_options=None, custom_ports=None, bind_addrs=None, remote_control=None, domain_settings=None,
                     custom_dns=None, public_addrs=value.get("public_addrs"))
        value["user_group_settings"] = [dict({"source_type": "resource"}, **entry) for entry in value["user_group_settings"]]
    else:
        value["linked_to_cluster"] = None
    return value


def cluster_resource(**changes):
    return dict(simplified(request("ceph.smb.cluster")["native"]), **changes)


def entry(name, value):
    return {"section": "mgr", "name": name, "value": value, "level": "advanced", "can_update_at_runtime": True, "mask": ""}


def daemon(host="node-1", identifier=None, status=1, digests=(SAMBA,), service_name="smb.files"):
    return {"daemon_type": "smb", "daemon_id": identifier or "files.0.0." + host + ".abcdef", "hostname": host, "service_name": service_name,
            "status": status, "status_desc": NATIVE_SENTINEL, "container_image_digests": list(digests)}


class Monitor:
    """A scripted cluster: reads answer from its tables, `smb apply` stores the resource, every other command is a logged mutation."""

    def __init__(self, enabled=("iostat", "smb"), always_on=("balancer",), config=None):
        self.enabled, self.always_on = list(enabled), list(always_on)
        self.config = [entry(SAMBA_KEY, SAMBA), entry(METRICS_KEY, METRICS)] if config is None else list(config)
        self.resources, self.cluster_ids, self.services, self.daemons = {}, ["files"], [], []
        self.log, self.mutations, self.answer = [], [], None

    def run(self, argv, data=b"", timeout=60):
        del timeout
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append((command, data))
        if command == ("fsid",):
            return 0, json.dumps({"fsid": FSID}).encode()
        if command == ("mgr", "module", "ls"):
            return 0, json.dumps({"always_on_modules": self.always_on, "enabled_modules": self.enabled, "disabled_modules": []}).encode()
        if command == ("config", "dump"):
            return 0, json.dumps(self.config).encode()
        if command == ("orch", "ls"):
            return 0, json.dumps(self.services).encode() if self.services else b"No services reported\n"
        if command == ("orch", "ps"):
            return 0, json.dumps(self.daemons).encode()
        if command == ("smb", "cluster", "ls"):
            return 0, json.dumps(self.cluster_ids).encode()
        if command[:2] == ("smb", "show"):
            found = self.resources.get(command[2])
            return 0, json.dumps(masked(found) if found is not None else {"resources": []}).encode()
        if command[:2] == ("smb", "apply"):
            return self.apply(data)
        self.mutations.append(command)
        return 0, b""

    def apply(self, data):
        if self.answer is not None:
            code, stdout, stderr = self.answer
            return code, ceph_native.NativeOutput(stdout, stderr)
        loaded = json.loads(data)["resources"]
        results = []
        for resource in loaded:
            stored = simplified(resource)
            kind = resource["resource_type"]
            prefix = resource["cluster_id"] + "." if kind == "ceph.smb.share" else ""
            self.resources[kind + "." + prefix + resource[ceph_smb_native.TYPES[kind]]] = stored
            results.append({"resource": masked(stored), "success": True})
        return 0, json.dumps({"results": results, "success": True}).encode()


class Markers:
    path = "/private/cluster"

    def __init__(self, records=None, owned=True):
        self.records, self.writes, self._owned = dict(records or {}), [], owned

    def owned(self):
        return self._owned

    def read(self, name="owner.json"):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        assert name not in self.records or self.records[name] == value
        self.writes.append((name, copy.deepcopy(value)))
        self.records[name] = copy.deepcopy(value)


def worker(value, monitor, markers=None):
    return ceph_smb_native.SMBWorker(value, "node-1", run=monitor.run, markers=markers or Markers())


def intent(value):
    return {"bindings": [binding["path"] for binding in value["bindings"]], "fsid": FSID, "native": value["native"], "object": value["object"],
            "owner": OWNER, "resource": name_of(value)}


def records(value, attempt=True, applied=False):
    result = {}
    if attempt:
        result["smb-attempt." + name_of(value) + ".json"] = intent(value)
    if applied:
        result["smb-applied." + name_of(value) + ".json"] = intent(value)
    return result


def present(monitor, value):
    monitor.resources[name_of(value)] = simplified(value["native"])


def smb_commands(monitor):
    return [command for command, unused in monitor.log if command[:1] == ("smb",)]


def applies(monitor):
    return [(command, data) for command, data in monitor.log if command[:2] == ("smb", "apply")]


def test_smb_apply_puts_secrets_only_on_stdin():
    for kind, secrets in (("ceph.smb.join.auth", [JOIN_SECRET]), ("ceph.smb.usersgroups", USER_SECRETS)):
        value, monitor, markers = request(kind), Monitor(), Markers()
        result = worker(value, monitor, markers).smb_apply(private_of(value))
        assert result == {"changed": True}
        sent = applies(monitor)
        assert len(sent) == 1
        assert all(secret.encode() in sent[0][1] for secret in secrets)
        for secret in secrets:
            assert all(secret not in repr(command) for command, unused in monitor.log)
            assert all(data == b"" for command, data in monitor.log if command[:2] != ("smb", "apply"))
            assert secret not in repr(markers.writes) and secret not in repr(result)
        assert [name for name, unused in markers.writes] == ["smb-attempt." + name_of(value) + ".json", "smb-applied." + name_of(value) + ".json"]


@pytest.mark.parametrize("kind", TYPES)
def test_every_smb_command_hides_passwords_in_output(kind):
    value, monitor, markers = request(kind), Monitor(), Markers()
    if kind == "ceph.smb.share":
        monitor.resources["ceph.smb.cluster.files"] = cluster_resource()
        monitor.daemons = [daemon("node-1"), daemon("node-2")]
    worker(value, monitor, markers).smb_apply(private_of(value))
    observed = worker(value, monitor, markers).smb_observe()
    assert observed["ready"] is True
    for command in smb_commands(monitor):
        if command[:2] == ("smb", "apply"):
            assert command == ("smb", "apply", "-i", "-", "--password-filter=none", "--password-filter-out=hidden")
        elif command[:2] == ("smb", "show"):
            assert command[3:] == ("--password-filter=hidden",)
        else:
            assert command == ("smb", "cluster", "ls")
    assert len(applies(monitor)) == 1


def test_smb_show_refuses_an_unmasked_password():
    value, monitor = request("ceph.smb.join.auth"), Monitor()
    monitor.resources[name_of(value)] = simplified(dict(value["native"], auth={"username": "operator", "password": JOIN_SECRET}))
    original = monitor.run

    def cleartext(argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:])
        if command[:2] == ("smb", "show"):
            monitor.log.append((command, data))
            return 0, json.dumps(monitor.resources[command[2]]).encode()
        return original(argv, data, timeout)

    monitor.run = cleartext
    with pytest.raises(ceph_native.Refusal, match="^native-output$") as caught:
        worker(value, monitor, Markers(records(value))).smb_observe()
    assert JOIN_SECRET not in repr(vars(caught.value)) and JOIN_SECRET not in repr(caught.value.args)


@pytest.mark.parametrize("kind", TYPES)
def test_existing_resource_without_marker_refuses_identity(kind):
    value, monitor, markers = request(kind), Monitor(), Markers()
    present(monitor, value)
    with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$") as caught:
        worker(value, monitor, markers).smb_apply(private_of(value))
    assert caught.value.native_error["cause"] == name_of(value) + " has no creation record of this operation"
    assert applies(monitor) == [] and markers.writes == []
    with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$"):
        worker(value, monitor, markers).smb_observe()


@pytest.mark.parametrize("kind", ["ceph.smb.cluster", "ceph.smb.usersgroups"])
def test_smb_apply_continues_after_an_interruption(kind):
    value = request(kind)
    applied_name = "smb-applied." + name_of(value) + ".json"
    monitor, markers = Monitor(), Markers(records(value))
    assert worker(value, monitor, markers).smb_apply(private_of(value)) == {"changed": True}
    assert len(applies(monitor)) == 1 and [name for name, unused in markers.writes] == [applied_name]
    monitor, markers = Monitor(), Markers(records(value))
    present(monitor, value)
    assert worker(value, monitor, markers).smb_apply(private_of(value)) == {"changed": True}
    assert applies(monitor) == [] and [name for name, unused in markers.writes] == [applied_name]
    markers = Markers(records(value, applied=True))
    assert worker(value, monitor, markers).smb_apply(private_of(value)) == {"changed": False}
    assert applies(monitor) == [] and markers.writes == [] and monitor.mutations == []
    for phrase, change in (("that this operation created is gone", lambda m: m.resources.clear()),
                           ("that this operation created changed", lambda m: m.resources[name_of(value)].update(intent="present", **changed(kind)))):
        monitor, markers = Monitor(), Markers(records(value, applied=True))
        present(monitor, value)
        change(monitor)
        with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$") as caught:
            worker(value, monitor, markers).smb_apply(private_of(value))
        assert caught.value.native_error["cause"] == name_of(value) + " " + phrase
        assert applies(monitor) == [] and markers.writes == []


def changed(kind):
    if kind == "ceph.smb.cluster":
        return {"auth_mode": "active-directory"}
    return {"values": {"users": [{"name": "smbuser", "password": "x"}], "groups": []}}


def test_a_differing_attempt_record_refuses_identity():
    value, monitor = request("ceph.smb.cluster"), Monitor()
    markers = Markers({"smb-attempt." + name_of(value) + ".json": dict(intent(value), owner="apply-456")})
    with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$") as caught:
        worker(value, monitor, markers).smb_apply({})
    assert caught.value.native_error["cause"] == name_of(value) + " is not the resource this operation attempted"
    assert applies(monitor) == []


def unsuccessful_answers(value):
    echoed = dict(value["native"], auth={"username": "operator", "password": JOIN_SECRET})
    failed = {"results": [{"resource": echoed, "msg": NATIVE_SENTINEL, "success": False}], "success": False}
    good = {"resource": masked(simplified(value["native"])), "success": True}
    other = dict(good, resource=dict(good["resource"], auth_id="other"))
    return [(11, json.dumps(failed).encode(), NATIVE_SENTINEL.encode()),
            (0, json.dumps(dict(failed, results=[dict(failed["results"][0])])).encode(), b""),
            (0, json.dumps({"results": [good, good], "success": True}).encode(), b""),
            (0, json.dumps({"results": [other], "success": True}).encode(), b""),
            (0, b"not json " + NATIVE_SENTINEL.encode(), b""),
            (11, json.dumps({"results": [good], "success": True}).encode(), NATIVE_SENTINEL.encode())]


@pytest.mark.parametrize("index", range(6))
def test_unsuccessful_apply_result_refuses_without_native_message(index):
    value, monitor, markers = request("ceph.smb.join.auth"), Monitor(), Markers()
    monitor.answer = unsuccessful_answers(value)[index]
    with pytest.raises(ceph_native.Refusal, match="^smb-result$") as caught:
        worker(value, monitor, markers).smb_apply(private_of(value))
    assert caught.type is ceph_native.Refusal and not hasattr(caught.value, "native_error")
    assert NATIVE_SENTINEL not in repr(vars(caught.value)) and JOIN_SECRET not in repr(vars(caught.value))
    assert caught.value.__cause__ is None and caught.value.__context__ is None
    assert [name for name, unused in markers.writes] == ["smb-attempt." + name_of(value) + ".json"]


@pytest.mark.parametrize("kind", ["ceph.smb.cluster", "ceph.smb.share"])
@pytest.mark.parametrize("foreign", ["cluster", "service"])
def test_undeclared_smb_cluster_refuses_foreign_service(kind, foreign):
    value, monitor, markers = request(kind), Monitor(), Markers()
    if foreign == "cluster":
        monitor.cluster_ids = ["files", "other"]
    else:
        monitor.services = [{"service_type": "smb", "service_name": "smb.files"}, {"service_type": "smb", "service_name": "smb.other"}]
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-service$") as caught:
        worker(value, monitor, markers).smb_apply({})
    assert caught.value.native_error["cause"] == ("SMB cluster other" if foreign == "cluster" else "service smb.other")
    assert applies(monitor) == [] and markers.writes == []
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-service$"):
        worker(value, monitor, markers).smb_observe()


@pytest.mark.parametrize("kind", ["ceph.smb.join.auth", "ceph.smb.usersgroups"])
def test_unserved_resources_issue_no_cluster_listing(kind):
    value, monitor = request(kind), Monitor()
    monitor.cluster_ids = ["other"]
    worker(value, monitor).smb_apply(private_of(value))
    assert ("smb", "cluster", "ls") not in smb_commands(monitor)
    assert all(command not in (("orch", "ls"), ("config", "dump")) for command, unused in monitor.log)


@pytest.mark.parametrize("kind", ["ceph.smb.cluster", "ceph.smb.share"])
def test_smb_blocks_verify_samba_images_before_the_first_share(kind):
    for config, missing in (([], SAMBA_KEY + ", " + METRICS_KEY), ([entry(SAMBA_KEY, SAMBA)], METRICS_KEY)):
        value, monitor, markers = request(kind), Monitor(config=config), Markers()
        with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
            worker(value, monitor, markers).smb_apply({})
        assert caught.value.native_error["cause"] == missing
        assert applies(monitor) == [] and monitor.mutations == [] and markers.writes == []
    value, monitor = request(kind), Monitor()
    worker(value, monitor).smb_apply({})
    assert len(applies(monitor)) == 1 and monitor.mutations == []
    value = request(kind)
    value["images"][0]["image"] = "registry.example.test/example/samba@sha256:" + "e" * 64
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        worker(value, Monitor()).smb_apply({})


def test_foreign_samba_image_value_refuses_without_overwrite():
    other = "registry.example.test/example/samba@sha256:" + "f" * 64
    value, monitor, markers = request("ceph.smb.share"), Monitor(config=[entry(SAMBA_KEY, other), entry(METRICS_KEY, METRICS)]), Markers()
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        worker(value, monitor, markers).smb_apply({})
    assert caught.value.native_error["cause"] == SAMBA_KEY and "f" * 64 not in repr(caught.value.native_error)
    assert monitor.mutations == [] and applies(monitor) == [] and markers.writes == []


@pytest.mark.parametrize("kind", TYPES)
def test_smb_module_disabled_refuses_before_any_effect(kind):
    value, monitor, markers = request(kind), Monitor(enabled=("iostat",), always_on=("balancer",)), Markers()
    with pytest.raises(ceph_native.Refusal, match="^smb-module-disabled$"):
        worker(value, monitor, markers).smb_apply(private_of(value))
    assert smb_commands(monitor) == [] and markers.writes == []
    with pytest.raises(ceph_native.Refusal, match="^smb-module-disabled$"):
        worker(value, monitor, markers).smb_observe()
    value, monitor = request(kind), Monitor(enabled=("iostat",), always_on=("balancer", "smb"))
    if kind == "ceph.smb.share":
        monitor.resources["ceph.smb.cluster.files"] = cluster_resource()
    assert worker(value, monitor).smb_apply(private_of(value)) == {"changed": True}


def test_ownership_is_proved_before_any_read():
    value, monitor = request("ceph.smb.cluster"), Monitor()
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker(value, monitor, Markers(owned=False)).smb_apply({})
    assert monitor.log == []
    monitor.run = lambda argv, data=b"", timeout=60: (0, json.dumps({"fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc01"}).encode())
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker(value, monitor).smb_observe()


def share_shapes():
    value = request("ceph.smb.share")
    native = value["native"]
    full = simplified(native)
    objects = dict(full, extra_leaf={"x": "y"})
    return value, [full, {key: item for key, item in full.items() if item is not None}, objects]


def test_smb_readback_accepts_native_shapes_and_refuses_refused_leaves():
    value, shapes = share_shapes()
    native = worker(value, Monitor())
    for shape in shapes:
        assert native.matches(shape) is True
    assert native.matches(dict(shapes[0], readonly=True)) is False
    assert native.matches(dict(shapes[0], name="Other")) is False
    with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$") as caught:
        native.matches(dict(shapes[0], custom_smb_share_options={"x": "y"}))
    assert caught.value.native_error["cause"] == name_of(value) + " holds a field Bootwright does not realize"
    for empty in ({}, []):
        assert native.matches(dict(shapes[0], custom_smb_share_options=empty)) is True
    cluster = request("ceph.smb.cluster")
    native = worker(cluster, Monitor())
    observed = cluster_resource(client_compat="default", placement={"count": 2, "hosts": [{"hostname": "node-1", "network": "", "name": ""}, "node-2"]})
    assert native.matches(observed) is True
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.matches(cluster_resource(placement={"count": 2, "hosts": [{"hostname": "node-1", "network": "192.0.2.0/24"}, "node-2"]}))
    with pytest.raises(ceph_native.NativeFailure, match="^smb-identity$"):
        native.matches(cluster_resource(client_compat="strict"))
    users = request("ceph.smb.usersgroups")
    native = worker(users, Monitor())
    stored = masked(simplified(dict(users["native"], values={"users": [{"name": "smbuser", "password": "p"}, {"name": "other", "password": "q"}],
                                                             "groups": []})))
    assert native.matches(stored) is True
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.matches(dict(stored, values={"users": [{"name": "smbuser", "password": USER_SECRETS[0]}, stored["values"]["users"][1]], "groups": []}))
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.matches(dict(stored, extra={"password": ceph_smb_native.MASK}))


def test_smb_show_answers_one_resource_itself_or_in_a_list():
    value, monitor = request("ceph.smb.cluster"), Monitor()
    native = worker(value, monitor)
    assert native.show() is None
    present(monitor, value)
    assert native.show() == simplified(value["native"])
    listed = {"resources": [simplified(value["native"])]}
    for answer, reason in ((listed, None), ({"resources": [listed["resources"][0]] * 2}, "native-output"),
                           (dict(listed["resources"][0], cluster_id="other"), "native-output"), ([], "native-output")):
        native.run = lambda argv, data=b"", timeout=60, answer=answer: (0, json.dumps(answer).encode())
        if reason is None:
            assert native.show() == listed["resources"][0]
        else:
            with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
                native.show()


def test_cluster_readiness_does_not_wait_for_daemons():
    value, monitor = request("ceph.smb.cluster"), Monitor()
    present(monitor, value)
    observed = worker(value, monitor, Markers(records(value, applied=True))).smb_observe()
    assert observed["ready"] is True and observed["daemons"] == [] and observed["blocking"] == [] and observed["directory"] is False
    assert all(command != ("orch", "ps") for command, unused in monitor.log)


def observed_share(daemons, cluster=None, reach=True, monkeypatch=None, served=None):
    value = request("ceph.smb.share", service=served or service())
    monitor = Monitor()
    present(monitor, value)
    monitor.resources["ceph.smb.cluster.files"] = cluster if cluster is not None else cluster_resource()
    monitor.daemons = daemons + [{"daemon_type": "mon", "daemon_id": "node-1", "hostname": "node-1", "status": 1}]
    if monkeypatch is not None:
        monkeypatch.setattr(ceph_native_nfs, "reachable", lambda host, port: reach and port == 445 and host == "192.0.2.80")
    return worker(value, monitor, Markers(records(value, applied=True))).smb_observe()


def test_share_daemons_must_run_on_eligible_hosts_with_the_frozen_image(monkeypatch):
    result = observed_share([daemon("node-2"), daemon("node-1")])
    assert result["ready"] is True and result["blocking"] == []
    assert result["daemons"] == [{"host": "node-1", "name": "smb.files.0.0.node-1.abcdef", "service": "smb.files"},
                                 {"host": "node-2", "name": "smb.files.0.0.node-2.abcdef", "service": "smb.files"}]
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-service$") as caught:
        observed_share([daemon("node-1"), daemon("node-3")])
    assert caught.value.native_error["cause"] == "daemon smb.files.0.0.node-3.abcdef"
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-service$"):
        observed_share([daemon("node-1", service_name="smb.other")])
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        observed_share([daemon("node-1", digests=("registry.example.test/example/samba@sha256:" + "e" * 64,))])
    result = observed_share([daemon("node-1"), daemon("node-2", status=0, digests=())])
    assert result["ready"] is False and result["partial"] is True and result["blocking"] == ["smb@smb.files"]
    assert NATIVE_SENTINEL not in json.dumps(result)
    one = cluster_resource(placement={"count": 1, "hosts": ["node-1", "node-2"]})
    assert observed_share([daemon("node-1")], cluster=one)["ready"] is True
    clustered = service(clustered=True)
    vip = cluster_resource(clustering="always", public_addrs=[{"address": "192.0.2.80/24"}])
    result = observed_share([daemon("node-1"), daemon("node-2")], cluster=vip, reach=False, monkeypatch=monkeypatch, served=clustered)
    assert result["ready"] is False and result["blocking"] == ["vip@smb.files"]
    result = observed_share([daemon("node-1"), daemon("node-2")], cluster=vip, reach=True, monkeypatch=monkeypatch, served=clustered)
    assert result["ready"] is True and result["blocking"] == []
    assert all(ceph_smb_native.PENDING.fullmatch(item) for item in ["smb@smb.files", "vip@smb.files"])


def test_a_share_of_an_active_directory_cluster_reports_its_directory():
    result = observed_share([], cluster=cluster_resource(auth_mode="active-directory"))
    assert result["directory"] is True and result["ready"] is False and result["blocking"] == ["smb@smb.files"]
    value = request("ceph.smb.share")
    monitor = Monitor()
    present(monitor, value)
    result = worker(value, monitor, Markers(records(value, applied=True))).smb_observe()
    assert result["ready"] is False and result["directory"] is False and result["blocking"] == ["smb@smb.files"]


def observations(kind):
    """Absent, attempted, applied and ready observations of one type."""
    value = request(kind)
    found = []
    monitor = Monitor()
    if kind == "ceph.smb.share":
        monitor.resources["ceph.smb.cluster.files"] = cluster_resource()
        monitor.daemons = [daemon("node-1"), daemon("node-2")]
    found.append(worker(value, monitor).smb_observe())
    found.append(worker(value, monitor, Markers(records(value))).smb_observe())
    markers = Markers()
    worker(value, monitor, markers).smb_apply(private_of(value))
    found.append(worker(value, monitor, markers).smb_observe())
    monitor.resources.pop(name_of(value))
    found.append(worker(value, monitor, markers).smb_observe())
    return value, found


@pytest.mark.parametrize("kind", TYPES)
def test_smb_evidence_is_closed_and_secret_free(kind):
    value, found = observations(kind)
    absent, attempted, ready, gone = found
    assert absent["exists"] is False and absent["resource"] == "" and absent["daemons"] == [] and absent["blocking"] == []
    assert attempted["partial"] is True and attempted["resource"] == ""
    assert ready["ready"] is True and ready["resource"] == name_of(value)
    assert gone["ready"] is False and gone["partial"] is True and gone["resource"] == ""
    for observation in found:
        assert set(observation) == ceph_core.SMB_EVIDENCE - {"request"} | {"blocking", "directory"}
        evidence = {key: item for key, item in observation.items() if key not in ("blocking", "directory")}
        evidence["request"] = DIGEST
        assert ceph_core.smb_evidence(copy.deepcopy(evidence), DIGEST, value) == evidence
        text = json.dumps(observation)
        assert all(secret not in text for secret in [JOIN_SECRET] + USER_SECRETS + [NATIVE_SENTINEL])
    if kind == "ceph.smb.share":
        assert len(ready["daemons"]) == 2


def recorded_scope(value, delegated):
    core = dict(copy.deepcopy(value["scope"]), action="cluster")
    if delegated:
        core["delegated"] = delegated
    return ceph_tunnel_staging.recovery_scope(core)


@pytest.mark.parametrize("kind", ["ceph.smb.share", "ceph.smb.join.auth"])
def test_smb_recovery_scope_binds_every_recorded_member_but_delegated(kind):
    value = request(kind, transport="verified-tunnel")
    own = [{"hosts": ["node-1", "node-2"], "name": "smb.files"}]
    rgw = [{"hosts": ["node-3"], "name": "rgw.objects"}]
    record = recorded_scope(value, rgw + own)
    native = worker(value, Monitor(), Markers({"bootstrap-scope.json": record}))
    assert native.recovery_scope() == record
    changed_scope = copy.deepcopy(record)
    changed_scope["cluster"]["callHome"] = "enabled"
    refused = [changed_scope, {"cluster": []}, None]
    if kind == "ceph.smb.share":
        refused += [recorded_scope(value, rgw), recorded_scope(value, [{"hosts": ["node-1"], "name": "smb.files"}]),
                    recorded_scope(value, own + [{"hosts": ["node-1"], "name": "smb.other"}])]
    else:
        assert worker(value, Monitor(), Markers({"bootstrap-scope.json": recorded_scope(value, None)})).recovery_scope() is not None
    for recorded in refused:
        with pytest.raises(ceph_native.Refusal, match="^ownership$"):
            worker(value, Monitor(), Markers({"bootstrap-scope.json": recorded})).recovery_scope()


def test_dispatch_routes_smb_phases_to_the_smb_worker(monkeypatch):
    value = request("ceph.smb.share", transport="verified-tunnel")
    reached = []
    monkeypatch.setattr(ceph_smb_native, "dispatch", lambda envelope: reached.append(envelope) or {"routed": True})
    envelope = {"request": value, "host": "node-1", "phase": "smb_observe", "private": {}}
    assert ceph_native.dispatch(copy.deepcopy(envelope)) == {"routed": True} and reached == [envelope]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, phase="observe"))
    monkeypatch.undo()
    core = dict(copy.deepcopy(value["scope"]), action="cluster")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, request=core, phase="smb_apply"))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, host="node-2"))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, request=request("ceph.smb.share"), phase="smb_apply"))
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(dict(envelope, private={"smbSecret0": JOIN_SECRET}))
    assert not ceph_smb_native.PHASES & (ceph_native.CORE_PHASES | ceph_native.HOST_PHASES)


@pytest.mark.parametrize("private", [{}, {"smbSecret0": JOIN_SECRET, "smbSecret1": "x"}, {"smbSecret0": ""}, {"smbSecret0": "a\nb"},
                                     {"smbSecret0": "a\x00b"}, {"smbSecret0": "x" * 1025}, {"smbSecret0": "\ud800"}, {"smbSecret0": 1}])
def test_material_admits_one_bound_password_per_binding(private):
    value = request("ceph.smb.join.auth")
    assert ceph_smb_native.material(value, {"smbSecret0": "é" * 512}) is not None
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_smb_native.material(value, private)


def test_core_observation_delegates_smb_daemons_of_declared_clusters():
    delegated = {"delegated": [{"name": "smb.files", "hosts": ["node-1", "node-2"]}]}
    ceph_native.delegated_daemon({"daemon_type": "smb", "service_name": "smb.files", "hostname": "node-1"}, delegated)
    for refused in ({"daemon_type": "smb", "service_name": "smb.files", "hostname": "node-3"},
                    {"daemon_type": "smb", "service_name": "smb.other", "hostname": "node-1"}):
        with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
            ceph_native.delegated_daemon(refused, delegated)
    assert ceph_native.DAEMON_TYPES["smb"] == ("delegated", "delegated", False, "none")
    assert ceph_native_observe.delegated_service("smb.files.node-1.abcdef", None, ["smb.files"]) == "smb.files"
    assert ceph_native.DELEGATED_SERVICE.fullmatch("smb.files") is not None
    assert "smb" not in ceph_native_observe.SINGLETONS


def test_smb_worker_ships_after_its_imports():
    modules = ceph_native_worker.MODULES
    at = modules.index("ceph_smb_native")
    assert modules[at - 1] == "ceph_native_object"
    assert all(modules.index(name) < at for name in ("ceph_native", "ceph_native_resources", "ceph_native_nfs"))
    assert modules.index("native_specializations") == modules.index("native_diagnostics") + 1
    assert tuple(ceph_core.Transport.native_sources()) == modules


def test_core_keeps_the_smb_names_as_aliases():
    assert ceph_core.SMB_VERSION == ceph_smb_native.VERSION == "ceph-smb-v1"
    assert ceph_core.SMB_EVIDENCE_VERSION == ceph_smb_native.EVIDENCE_VERSION == "ceph-smb-evidence-v1"
    assert ceph_core.SMB_TYPES is ceph_smb_native.TYPES
    value = request("ceph.smb.share")
    assert ceph_core.smb_request(copy.deepcopy(value)) == value

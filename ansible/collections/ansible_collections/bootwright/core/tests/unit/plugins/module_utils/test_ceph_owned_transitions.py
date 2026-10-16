"""Owned native transitions settle lost replies from exact observed results."""

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request


@pytest.fixture
def world(core_request):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=fixtures.Markers())
    native.prove_registry = lambda: None
    native.provider_registry_absent = lambda: None
    native.config_database = lambda: None
    native.normalized_services = lambda groups: groups
    native.recovery_scope = lambda: {"cluster": copy.deepcopy(core_request)}
    hosts, services, effects = {}, {}, []

    def ceph(*args, **kwargs):
        if args == ("fsid",):
            return {"fsid": core_request["fsid"]}
        if args == ("orch", "host", "ls"):
            return list(copy.deepcopy(hosts).values())
        if args == ("orch", "ls", "--export"):
            return list(copy.deepcopy(services).values())
        raise AssertionError(args)

    def effect(*args, **kwargs):
        effects.append(args)
        if args == ("orch", "apply", "-i", "-"):
            value = json.loads(kwargs["data"])
            if value["service_type"] == "host":
                hosts[value["hostname"]] = {"hostname": value["hostname"], "addr": value["addr"], "labels": value["labels"]}
            else:
                key = value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")
                services[key] = value
        else:
            raise AssertionError(args)

    native.ceph = ceph
    native.tunnel_effect = effect
    return native, hosts, services, effects


@pytest.mark.parametrize("phase", ["enroll", "services"])
def test_lost_native_success_is_observed_without_repeating_effect(world, phase):
    native, unused_hosts, unused_services, effects = world
    original = native.tunnel_effect

    def lost(*args, **kwargs):
        original(*args, **kwargs)
        raise ceph_native.Refusal("native-command")

    native.tunnel_effect = lost
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        getattr(native, phase)()
    first = list(effects)
    native.tunnel_effect = original
    getattr(native, phase)()
    expected = len(native.request["hosts"]) if phase == "enroll" else sum(len(native.service_spec(value)) for value in native.request["services"])
    assert len(effects) == expected
    assert effects[:1] == first
    assert getattr(native, phase)() == {"changed": False}
    assert len(effects) == expected


@pytest.mark.parametrize("phase,prefix", [("enroll", "enrollment-"), ("services", "service-")])
def test_failed_intent_publication_prevents_native_effect(world, phase, prefix):
    native, unused_hosts, unused_services, effects = world
    original = native.markers.write

    def write(name, value):
        if name.startswith(prefix) and name.endswith("-attempt.json"):
            raise ceph_native.Refusal("ownership")
        original(name, value)

    native.markers.write = write
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        getattr(native, phase)()
    assert effects == []


def test_service_same_name_without_owned_attempt_is_foreign(world):
    native, unused_hosts, services, effects = world
    services["mon"] = native.service_spec(native.request["services"][0])[0]
    with pytest.raises(ceph_native.Refusal, match="foreign-service"):
        native.services()
    assert effects == []


def test_completed_host_or_service_drift_never_overwrites_native_state(world):
    native, hosts, services, effects = world
    native.enroll()
    native.services()
    count = len(effects)
    hosts["node-1"]["addr"] = "192.0.2.99"
    with pytest.raises(ceph_native.Refusal, match="foreign-host"):
        native.enroll()
    services["mon"]["placement"]["hosts"] = ["foreign"]
    with pytest.raises(ceph_native.Refusal, match="foreign-service"):
        native.services()
    assert len(effects) == count


def test_osd_effects_use_only_frozen_host_specific_volume_aliases(world):
    native, unused_hosts, services, unused_effects = world
    native.services(service=2)
    assert len(services) == len(native.request["hosts"])
    for value in services.values():
        names = value["placement"]["hosts"]
        assert len(names) == 1
        host = next(host for host in native.request["hosts"] if host["name"] == names[0])
        assert value["spec"]["data_devices"]["paths"] == ["/dev/disk/by-id/virtio-" + host["devices"][0]["serial"]]


@pytest.fixture
def device_world(world, monkeypatch):
    native, unused_hosts, unused_services, unused_effects = world
    disk = {"path": "/dev/vdb", "type": "disk", "serial": "disk1", "size": 1024 ** 3, "fstype": None, "mountpoints": []}
    signatures, rows, calls = [], [], []
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda path: "/dev/vdb" if path == "/dev/disk/by-id/virtio-disk1" else path)

    def command(argv, **kwargs):
        calls.append(argv)
        if argv[0] == "/usr/bin/lsblk":
            return {"blockdevices": [copy.deepcopy(disk)]}
        if argv[0] == "/usr/sbin/wipefs":
            assert "--no-act" in argv
            return {"signatures": list(signatures)}
        if argv[0] == "/usr/sbin/lvs":
            return {"report": [{"lv": copy.deepcopy(rows)}]}
        raise AssertionError(argv)

    native.command = command
    return native, disk, signatures, rows, calls


def partial_osd(native, disk, signatures, rows):
    disk["fstype"] = "LVM2_member"
    disk["children"] = [{"path": "/dev/ceph/block", "type": "lvm", "fstype": "ceph_bluestore", "mountpoints": []}]
    signatures.append({"type": "LVM2_member"})
    rows.append({"lv_path": "/dev/ceph/block", "lv_uuid": "synthetic-lv-uuid", "devices": "/dev/vdb(0)",
                 "lv_tags": "ceph.cluster_fsid=" + native.request["fsid"]
                 + ",ceph.type=block,ceph.osd_id=3,ceph.osd_fsid=6da65e47-8789-4a1c-88e8-3bbd195719a1"})


def test_empty_device_intent_precedes_service_and_owned_partial_identity_is_retained(device_world):
    native, disk, signatures, rows, calls = device_world
    assert native.osd_prepare() == {"changed": True}
    partial_osd(native, disk, signatures, rows)
    original = copy.deepcopy(native.markers.records)
    assert native.devices() == {"devices": 1}
    assert native.markers.records == original
    assert native.osd_prepare() == {"changed": True}
    assert native.osd_prepare() == {"changed": False}
    rows[0]["lv_tags"] = rows[0]["lv_tags"].replace("6da65e47", "7da65e47")
    with pytest.raises(ceph_native.Refusal, match="foreign-osd"):
        native.devices()
    assert all(argv[0] in ("/usr/bin/lsblk", "/usr/sbin/wipefs", "/usr/sbin/lvs") for argv in calls)


@pytest.mark.parametrize("change", ["no-intent", "foreign-fsid", "foreign-signature", "mounted", "extra-leg", "serial", "size"])
def test_nonempty_or_changed_device_never_gains_ownership(device_world, change):
    native, disk, signatures, rows, unused_calls = device_world
    if change != "no-intent":
        native.osd_prepare()
    partial_osd(native, disk, signatures, rows)
    if change == "foreign-fsid":
        rows[0]["lv_tags"] = rows[0]["lv_tags"].replace(native.request["fsid"], "foreign")
    elif change == "foreign-signature":
        signatures.append({"type": "linux_raid_member"})
    elif change == "mounted":
        disk["children"][0]["mountpoints"] = ["/"]
    elif change == "extra-leg":
        rows[0]["devices"] += ",/dev/vdc(0)"
    elif change == "serial":
        disk["serial"] = "foreign"
    elif change == "size":
        disk["size"] += 512
    before = copy.deepcopy(native.markers.records)
    with pytest.raises(ceph_native.Refusal):
        native.osd_prepare()
    assert native.markers.records == before


def test_replaced_stable_alias_refuses_before_any_intent(device_world, monkeypatch):
    native, unused_disk, unused_signatures, unused_rows, calls = device_world
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda path: "/dev/vdc" if path.startswith("/dev/disk/by-id/") else path)
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        native.osd_prepare()
    assert native.markers.writes == [] and len(calls) == 1


def test_pool_setter_lost_success_is_read_back_without_a_second_write(core_request):
    marks, pools, effects = fixtures.Markers(), [], []
    native = fixtures.pool_worker(core_request, marks, pools, effects)
    original = native.ceph
    failed = False

    def ceph(*args, **kwargs):
        nonlocal failed
        result = original(*args, **kwargs)
        if args[:3] == ("osd", "pool", "set") and not failed:
            failed = True
            raise ceph_native.Refusal("native-command")
        return result

    native.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.pool()
    native.pool()
    setters = [effect for effect in effects if effect[:3] == ("osd", "pool", "set")]
    assert len(setters) == 1
    count = len(effects)
    assert native.pool() == {"changed": False}
    assert len(effects) == count
    pools[0]["size"] = 4
    with pytest.raises(ceph_native.Refusal, match="pool-identity"):
        native.pool()
    assert len(effects) == count


def test_rbd_init_lost_reply_retries_only_the_same_owned_pool(core_request):
    marks, pools, effects = fixtures.Markers(), [], []
    native = fixtures.pool_worker(core_request, marks, pools, effects)
    original = native.command
    failed = False

    def command(argv, **kwargs):
        nonlocal failed
        result = original(argv, **kwargs)
        if argv[0] == "/usr/bin/rbd" and not failed:
            failed = True
            raise ceph_native.Refusal("native-command")
        return result

    native.command = command
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.pool()
    assert "pool-rbd-attempt-volumes.json" in marks.records
    assert "pool-rbd-complete-volumes.json" not in marks.records
    native.pool()
    inits = [effect for effect in effects if effect[0] == "/usr/bin/rbd"]
    assert len(inits) == 2 and inits[0] == inits[1]
    count = len(effects)
    pools[0]["pool_id"] += 1
    with pytest.raises(ceph_native.Refusal, match="pool-identity"):
        native.pool()
    assert len(effects) == count


def test_selected_native_service_normalization_is_frozen_and_revalidated(world, monkeypatch):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    native, unused_hosts, unused_services, unused_effects = world
    del native.normalized_services
    groups = [native.service_spec(value) for value in native.request["services"]]
    calls = []
    native.runtime_profile = lambda: {"imageID": "sha256:" + "a" * 64, "uid": 100, "gid": 100}

    def contained(self, image, user, source, arguments):
        calls.append((image, user))
        result = json.loads(arguments[0])
        for group in result:
            for value in group:
                value["service_name"] = value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")
                value["unmanaged"] = False
                if value["service_type"] == "osd":
                    value["spec"]["objectstore"] = "bluestore"
        return result

    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", contained)
    normalized = native.normalized_services(groups)
    assert native.normalized_services(groups) == normalized
    assert len(calls) == 1
    record = native.markers.read("service-inputs.json")
    record["services"][-1][0]["placement"] = {"hosts": ["foreign"]}
    with pytest.raises(ceph_native.Refusal, match="foreign-service"):
        native.normalized_services(groups)


@pytest.mark.parametrize("size", [513, 1024])
def test_native_selected_version_coordinates_preserve_long_authored_strings(core_request, size):
    value = "synthetic+" + "x" * (size - 10)
    core_request.update(release=value, ansiblePackageVersion=value, nfsGaneshaPackageVersion=value)
    core_request["cephadm"]["packageVersion"] = value
    assert ceph_native.validate(json.loads(json.dumps(core_request))) == core_request


def test_manager_retry_preserves_native_diagnostic_on_exhaustion(world):
    native, unused_hosts, unused_services, unused_effects = world
    error = ceph_native.NativeFailure("native-command", "ceph orch host ls", b"manager unavailable")
    now = [0]
    native.clock = lambda: now[0]
    native.pause = lambda value: now.__setitem__(0, now[0] + value)
    native.horizon = 2 * ceph_native.MANAGER_INTERVAL

    def unavailable():
        raise error
    with pytest.raises(ceph_native.NativeFailure) as failure:
        native.manager_retry(unavailable)
    assert failure.value is error
    assert failure.value.native_error == error.native_error
    with pytest.raises(ceph_native.ManagerRefusal) as transient:
        with native.manager_served():
            raise error
    assert transient.value.native_cause is error


def test_osd_child_identity_is_bounded_and_hashes_untruncated_intent(world):
    native, unused_hosts, unused_services, unused_effects = world
    service = next(copy.deepcopy(value) for value in native.request["services"] if value["service_type"] == "osd")
    service["service_id"] = "a" * 127 + "b"
    first = native.service_spec(service)
    service["service_id"] = "a" * 127 + "c"
    second = native.service_spec(service)
    assert all(len(value["service_id"]) <= 63 for value in first + second)
    assert {value["service_id"] for value in first}.isdisjoint(value["service_id"] for value in second)


def test_qualified_scsi_ssd_proof_and_osd_submission_use_same_stable_alias(device_world, monkeypatch):
    native, disk, unused_signatures, unused_rows, unused_calls = device_world
    device = native.host["devices"][0]
    device["path"] = disk["path"] = "/dev/sda"
    alias = "/dev/disk/by-id/scsi-0QEMU_QEMU_HARDDISK_" + device["serial"]
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda path: "/dev/sda" if path == alias else path)
    original_command = native.command
    udev = {"value": ("ID_VENDOR=QEMU\nID_MODEL=QEMU_HARDDISK\nID_SCSI_SERIAL=" + device["serial"]
                      + "\nID_SERIAL=0QEMU_QEMU_HARDDISK_" + device["serial"] + "\n").encode()}
    native.command = lambda argv, **kwargs: udev["value"] if argv[0] == "/usr/bin/udevadm" else original_command(argv, **kwargs)
    assert native.osd_prepare() == {"changed": True}
    service = next(copy.deepcopy(value) for value in native.request["services"] if value["service_type"] == "osd")
    service["placement"] = {"hosts": [native.host["name"]]}
    service["spec"]["data_devices"]["paths"] = ["/dev/sda"]
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        native.service_spec(service)
    rows = [{"host": host["name"], "path": value["path"], "serial": value["serial"],
             "deviceID": "QEMU_QEMU_HARDDISK_" + value["serial"]}
            for host in native.request["hosts"] for value in host["devices"]]
    native.device_identities(rows)
    result = native.service_spec(service)
    assert result[0]["spec"]["data_devices"]["paths"] == [alias]
    assert result[0]["placement"] == {"hosts": [native.host["name"]]}
    original_udev = udev["value"]
    udev["value"] = original_udev.replace(b"ID_VENDOR=QEMU", b"ID_VENDOR=FOREIGN")
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        native.osd_prepare()
    udev["value"] = original_udev
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda path: "/dev/sdb" if path == alias else path)
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        native.osd_prepare()

"""Explicit OSD variants preserve bounded identities and refuse partial auxiliaries."""

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request


def variants(request):
    for host in request["hosts"]:
        host["devices"] += [{"path": "/dev/vdc", "serial": host["name"] + "-db", "sizeBytes": 1024 ** 3},
                            {"path": "/dev/vdd", "serial": host["name"] + "-wal", "sizeBytes": 1024 ** 3}]
    request["services"][-1]["spec"].update(db_devices={"paths": ["/dev/vdc"]}, wal_devices={"paths": ["/dev/vdd"]}, encrypted=True, tpm2=True)
    return request


def test_variants_project_every_device_to_its_stable_identity(core_request):
    variants(core_request)
    assert ceph_native.validate(core_request) is core_request
    native, calls = fixtures.worker(core_request, {})
    projected = native.service_spec(core_request["services"][-1])
    assert len(projected) == 3
    for host, service in zip(core_request["hosts"], projected):
        assert service["spec"]["encrypted"] and service["spec"]["tpm2"]
        for index, field in enumerate(("data_devices", "db_devices", "wal_devices")):
            assert service["spec"][field]["paths"] == ["/dev/disk/by-id/virtio-" + host["devices"][index]["serial"]]
    assert calls == []


@pytest.mark.parametrize("damage", ["data-db", "db-wal", "other-service", "dynamic", "unencrypted", "boolean"])
def test_variant_request_refuses_overlap_and_unqualified_options(core_request, damage):
    variants(core_request)
    spec = core_request["services"][-1]["spec"]
    if damage == "data-db":
        spec["db_devices"]["paths"] = ["/dev/vdb"]
    elif damage == "db-wal":
        spec["wal_devices"]["paths"] = ["/dev/vdc"]
    elif damage == "other-service":
        service = copy.deepcopy(core_request["services"][-1])
        service["service_id"] = "other"
        core_request["services"].append(service)
    elif damage == "dynamic":
        spec["db_devices"] = {"all": True}
    elif damage == "unencrypted":
        spec["encrypted"] = False
    else:
        spec["encrypted"] = 1
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(core_request)


@pytest.mark.parametrize("field", ["db_devices", "wal_devices"])
@pytest.mark.parametrize("state", ["filesystem", "partition", "mounted", "signature"])
def test_any_partial_auxiliary_refuses_without_recovery(core_request, monkeypatch, field, state):
    variants(core_request)
    path = core_request["services"][-1]["spec"][field]["paths"][0]
    host = core_request["hosts"][0]
    devices = [{"path": d["path"], "serial": d["serial"], "size": d["sizeBytes"], "type": "disk"} for d in host["devices"]]
    item = next(d for d in devices if d["path"] == path)
    if state == "filesystem":
        item["fstype"] = "LVM2_member"
    elif state == "partition":
        item["children"] = [{"path": path + "1"}]
    elif state == "mounted":
        item["mountpoints"] = ["/data"]
    answers = {("/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "PATH,TYPE,SERIAL,SIZE,FSTYPE,MOUNTPOINTS"): {"blockdevices": devices}}
    for device in devices:
        signatures = [{"type": "LVM2_member"}] if state == "signature" and device["path"] == path else []
        answers[("/usr/sbin/wipefs", "--no-act", "--json", device["path"])] = {"signatures": signatures}
    aliases = {"/dev/disk/by-id/virtio-" + d["serial"]: d["path"] for d in host["devices"]}
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda value: aliases.get(value, value))
    native, calls = fixtures.worker(core_request, answers)
    native.markers.records["osd-device-intent.json"] = native.osd_device_scope()
    with pytest.raises(ceph_native.Refusal, match="^device-not-empty$"):
        native.osd_prepare()
    assert native.markers.writes == []
    assert all(argv[0] == "/usr/bin/lsblk" or argv[:2] == ["/usr/sbin/wipefs", "--no-act"] for argv in calls)


@pytest.mark.parametrize("damage", [None, "missing-db", "foreign-wal", "duplicate-data", "foreign-host"])
def test_osd_readiness_binds_data_and_auxiliaries_without_counting_them_as_osds(core_request, damage):
    variants(core_request)
    core_request["config"] = fixtures.memory_config()
    answers = fixtures.memory_observation_answers(core_request)
    for host, item in zip(core_request["hosts"], answers[("osd", "metadata")]):
        item["device_ids"] = ",".join(d["path"].rsplit("/", 1)[-1] + "=" + d["serial"] for d in host["devices"])
    first = answers[("osd", "metadata")][0]
    if damage == "missing-db":
        first["device_ids"] = first["device_ids"].replace(",vdc=node-1-db", "")
    elif damage == "foreign-wal":
        first["device_ids"] = first["device_ids"].replace("node-1-wal", "foreign")
    elif damage == "duplicate-data":
        first["device_ids"] += ",vdb=disk1"
    elif damage == "foreign-host":
        first["hostname"] = "foreign"
    native, unused = fixtures.worker(core_request, answers)
    observed = native.observe()
    assert observed["ready"] is (damage is None)
    assert len(observed["osds"]) == (3 if damage is None else 2)
    assert native.markers.writes == []


@pytest.mark.parametrize("damage", [None, "db_devices", "wal_devices", "encrypted", "tpm2", "osds_per_device"])
def test_native_normalization_cannot_change_variant_effects(core_request, monkeypatch, damage):
    import json
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    variants(core_request)
    native, unused = fixtures.worker(core_request, {})
    groups = [native.service_spec(value) for value in core_request["services"]]
    native.runtime_profile = lambda: {"imageID": "sha256:" + "a" * 64, "uid": 100, "gid": 100}

    def contained(self, image, user, source, arguments):
        result = json.loads(arguments[0])
        for value in result[-1]:
            spec = value["spec"]
            if damage in ("db_devices", "wal_devices"):
                spec[damage] = {"paths": ["/dev/foreign"]}
            elif damage in ("encrypted", "tpm2"):
                spec[damage] = False
            elif damage:
                spec[damage] = 2
        return result

    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", contained)
    if damage is None:
        assert native.normalized_services(groups) == groups
    else:
        with pytest.raises(ceph_native.Refusal, match="^osd-unqualified$"):
            native.normalized_services(groups)
        assert native.markers.writes == []


@pytest.mark.parametrize("damage", [None, "encryption", "tpm", "mapping", "token", "db-link", "foreign", "extra", "retained"])
def test_completed_variant_proof_observes_native_crypto_and_auxiliary_identity(core_request, damage):
    variants(core_request)
    native, unused = fixtures.worker(core_request, {})
    native.markers.records["osd-device-intent.json"] = native.osd_device_scope()
    host = core_request["hosts"][0]
    rows, disks = [], []
    for index, device in enumerate(host["devices"]):
        role = ("block", "db", "wal")[index]
        uuid = "volume-" + role
        tags = {"ceph.cluster_fsid": core_request["fsid"], "ceph.type": role, "ceph.osd_id": "0",
                "ceph.osd_fsid": "6da65e47-8789-4a1c-88e8-3bbd195719a1", "ceph.encrypted": "1", "ceph.with_tpm": "1",
                "ceph.block_uuid": "volume-block", "ceph.db_uuid": "volume-db", "ceph.wal_uuid": "volume-wal"}
        rows.append({"lv_path": "/dev/ceph/" + role, "lv_uuid": uuid, "devices": device["path"] + "(0)", "tags": tags})
        disks.append({"path": device["path"], "type": "disk", "serial": device["serial"], "size": device["sizeBytes"],
                      "children": [{"path": "/dev/ceph/" + role, "type": "lvm", "fstype": "crypto_LUKS", "children": [
                          {"path": "/dev/mapper/" + uuid, "type": "crypt", "fstype": "ceph_bluestore"}]}]})
    if damage in ("encryption", "tpm"):
        rows[0]["tags"]["ceph.encrypted" if damage == "encryption" else "ceph.with_tpm"] = "0"
    elif damage == "mapping":
        disks[0]["children"][0]["children"][0]["path"] = "/dev/mapper/foreign"
    elif damage == "db-link":
        rows[0]["tags"]["ceph.db_uuid"] = "foreign"
    elif damage == "foreign":
        rows[1]["tags"]["ceph.cluster_fsid"] = "foreign"
    elif damage == "extra":
        rows.append(copy.deepcopy(rows[1]))
    elif damage == "retained":
        native.markers.records[native.osd_identity_marker("/dev/vdb")] = {"id": 99}
    for row in rows:
        row["lv_tags"] = ",".join(k + "=" + v for k, v in row.pop("tags").items())
    calls = []

    def command(argv, **kwargs):
        calls.append(argv)
        if argv[0] == "/usr/bin/lsblk":
            return {"blockdevices": disks}
        if argv[0] == "/usr/sbin/lvs":
            return {"report": [{"lv": rows}]}
        assert argv[:2] == ["/usr/sbin/cryptsetup", "luksDump"]
        return b"Tokens:\n  0: systemd-tpm2\nDigests:\n" if damage != "token" else b"Tokens:\nDigests:\n"

    native.command = command
    if damage is None:
        native.prove_completed_osd_variants()
    else:
        with pytest.raises(ceph_native.Refusal):
            native.prove_completed_osd_variants()
    assert native.markers.writes == []
    assert all("--dump-master-key" not in argv and "--dump-volume-key" not in argv for argv in calls)


def test_existing_host_readiness_round_requires_variant_proof(core_request):
    native, unused = fixtures.worker(core_request, {})
    native.daemons = lambda: [{"name": "osd.0"}]
    native.prove_registry = lambda: None
    calls = []
    native.prove_completed_osd_variants = lambda: calls.append("proved")
    assert native.host_state()["exists"]
    assert calls == ["proved"]

    def refuse():
        raise ceph_native.Refusal("foreign-osd")

    native.prove_completed_osd_variants = refuse
    with pytest.raises(ceph_native.Refusal, match="foreign-osd"):
        native.host_state()

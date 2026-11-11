"""Explicit OSD variants preserve bounded identities and refuse partial auxiliaries."""

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
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
    answers = {ceph_native.DEVICE_TREE: {"blockdevices": devices}}
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


@pytest.mark.parametrize("token", ["device-not-empty", "foreign-osd"])
def test_a_dirty_osd_disk_reaches_the_cluster_block_by_its_own_name(token):
    error = ceph_native.RemoteRefusal("native-refusal:" + token + "@ceph_native.devices:2144")
    assert ceph_core.refusal(error, ceph_core.CLUSTER_REASONS) == (token, error.detail)


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


def util_linux(argv, tree):
    """lsblk as util-linux answers it: the tree is drawn in the NAME column, so without NAME the JSON is a flat list."""
    columns = argv[argv.index("--output") + 1].split(",")
    assert argv[:5] == ["/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output"] and "PATH" in columns

    def project(node, nested):
        value = {column.lower(): node.get(column.lower()) for column in columns if column != "NAME"}
        if "NAME" in columns:
            value["name"] = node.get("path")
        if nested and node.get("children"):
            value["children"] = [project(child, True) for child in node["children"]]
        return value

    if "NAME" in columns:
        return {"blockdevices": [project(node, True) for node in tree]}
    flat = []

    def walk(nodes):
        for node in nodes:
            flat.append(project(node, False))
            walk(node.get("children") or [])

    walk(tree)
    return {"blockdevices": flat}


def test_the_device_tree_requests_the_column_util_linux_draws_its_tree_in():
    columns = ceph_native.DEVICE_TREE[ceph_native.DEVICE_TREE.index("--output") + 1].split(",")
    assert columns[0] == "NAME" and {"PATH", "TYPE", "SERIAL", "SIZE", "FSTYPE", "MOUNTPOINTS"} <= set(columns)
    tree = [{"path": "/dev/sda", "type": "disk", "children": [{"path": "/dev/mapper/ceph--vg-osd--block--a", "type": "lvm"}]}]
    assert util_linux(list(ceph_native.DEVICE_TREE), tree)["blockdevices"][0]["children"][0]["type"] == "lvm"
    flat = util_linux(["/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "PATH,TYPE"], tree)["blockdevices"]
    assert [node["path"] for node in flat] == ["/dev/sda", "/dev/mapper/ceph--vg-osd--block--a"] and "children" not in flat[0]


@pytest.mark.parametrize("prepare", [False, True])
@pytest.mark.parametrize("damage", [None, "encryption", "tpm", "mapping", "token", "db-link", "foreign", "extra", "retained"])
def test_completed_variant_proof_observes_native_crypto_and_auxiliary_identity(core_request, monkeypatch, damage, prepare):
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
            return util_linux(argv, disks)
        if argv[0] == "/usr/sbin/lvs":
            return {"report": [{"lv": rows}]}
        if argv[:3] == ["/usr/sbin/wipefs", "--no-act", "--json"]:
            return {"signatures": [{"type": "LVM2_member"}]}
        assert argv[:2] == ["/usr/sbin/cryptsetup", "luksDump"]
        return b"Tokens:\n  0: systemd-tpm2\nDigests:\n" if damage != "token" else b"Tokens:\nDigests:\n"

    native.command = command
    aliases = {"/dev/disk/by-id/virtio-" + d["serial"]: d["path"] for d in host["devices"]}
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda value: aliases.get(value, value))
    if prepare and damage != "retained":
        native.markers.records[native.osd_identity_marker("/dev/vdb")] = {
            "path": "/dev/vdb", "id": 0, "uuid": "6da65e47-8789-4a1c-88e8-3bbd195719a1", "lvUUID": "volume-block"}
    action = native.osd_prepare if prepare else native.prove_completed_osd_variants
    if damage is None:
        result = action()
        if prepare:
            assert result == {"changed": False}
    else:
        with pytest.raises(ceph_native.Refusal):
            action()
    assert native.markers.writes == []
    assert all("--dump-master-key" not in argv and "--dump-volume-key" not in argv for argv in calls)


def test_existing_host_readiness_round_requires_variant_proof(core_request):
    native, unused = fixtures.worker(core_request, {})
    native.daemons = lambda: [{"name": "osd.0"}]
    native.prove_registry = lambda: None
    calls = []
    native.prove_completed_osd_variants = lambda physical: calls.append("proved")
    assert native.host_state()["exists"]
    assert calls == ["proved"]

    def refuse(physical):
        raise ceph_native.Refusal("foreign-osd")

    native.prove_completed_osd_variants = refuse
    with pytest.raises(ceph_native.Refusal, match="foreign-osd"):
        native.host_state()


def classed(request, spec_class="", path_classes=()):
    """Give the OSD service a spec-level class and, per data path, a {path, crush_device_class} record."""
    spec = request["services"][-1]["spec"]
    for host in request["hosts"]:
        host["devices"].append({"path": "/dev/vdc", "serial": host["name"] + "-fast", "sizeBytes": 1024 ** 3})
    paths = ["/dev/vdb", "/dev/vdc"]
    spec["data_devices"]["paths"] = [{"path": path, "crush_device_class": c} if c else path
                                     for path, c in zip(paths, list(path_classes) + [""] * (2 - len(path_classes)))]
    if spec_class:
        spec["crush_device_class"] = spec_class
    return request


def test_a_spec_class_stays_one_native_document_per_host(core_request):
    classed(core_request, spec_class="fast")
    assert ceph_native.validate(core_request) is core_request
    native, calls = fixtures.worker(core_request, {})
    plain = copy.deepcopy(core_request["services"][-1])
    del plain["spec"]["crush_device_class"]
    projected, unclassed = native.service_spec(core_request["services"][-1]), native.service_spec(plain)
    assert [s["service_id"] for s in projected] == [s["service_id"] for s in unclassed]
    assert all(s["spec"]["crush_device_class"] == "fast" and len(s["spec"]["data_devices"]["paths"]) == 2 for s in projected)
    assert all("crush_device_class" not in s["spec"] for s in unclassed)
    assert calls == []


def test_path_classes_become_one_spec_class_document_per_class(core_request):
    classed(core_request, spec_class="slow", path_classes=("fast",))
    assert ceph_native.validate(core_request) is core_request
    native, unused = fixtures.worker(core_request, {})
    projected = native.service_spec(core_request["services"][-1])
    assert len(projected) == 6 and len({s["service_id"] for s in projected}) == 6
    for host, pair in zip(core_request["hosts"], (projected[i:i + 2] for i in range(0, 6, 2))):
        assert [(s["spec"]["crush_device_class"], s["spec"]["data_devices"]["paths"]) for s in pair] == [
            ("fast", ["/dev/disk/by-id/virtio-" + host["devices"][0]["serial"]]),
            ("slow", ["/dev/disk/by-id/virtio-" + host["devices"][1]["serial"]])]
        assert all(s["placement"] == {"hosts": [host["name"]]} and len(s["service_id"]) <= 63 for s in pair)


@pytest.mark.parametrize("damage", ["spec-name", "path-name", "db-record", "unknown-member", "shared-db"])
def test_an_osd_class_refuses_where_cephadm_cannot_realize_it(core_request, damage):
    classed(core_request, path_classes=("fast",))
    spec = core_request["services"][-1]["spec"]
    if damage == "spec-name":
        spec["crush_device_class"] = "fa st"
    elif damage == "path-name":
        spec["data_devices"]["paths"][0]["crush_device_class"] = "f" * 65
    elif damage == "db-record":
        for host in core_request["hosts"]:
            host["devices"].append({"path": "/dev/vdd", "serial": host["name"] + "-db", "sizeBytes": 1024 ** 3})
        spec["data_devices"]["paths"] = ["/dev/vdb", "/dev/vdc"]
        spec["db_devices"] = {"paths": [{"path": "/dev/vdd", "crush_device_class": "fast"}]}
    elif damage == "unknown-member":
        spec["data_devices"]["paths"][0]["weight"] = 1
    else:
        for host in core_request["hosts"]:
            host["devices"].append({"path": "/dev/vdd", "serial": host["name"] + "-db", "sizeBytes": 1024 ** 3})
        spec["db_devices"] = {"paths": ["/dev/vdd"]}
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(core_request)


@pytest.mark.parametrize("damage", [None, "class"])
def test_native_normalization_cannot_drop_a_declared_class(core_request, monkeypatch, damage):
    import json
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    classed(core_request, path_classes=("fast",))
    native, unused = fixtures.worker(core_request, {})
    groups = [native.service_spec(value) for value in core_request["services"]]
    native.runtime_profile = lambda: {"imageID": "sha256:" + "a" * 64, "uid": 100, "gid": 100}

    def contained(self, image, user, source, arguments):
        result = json.loads(arguments[0])
        if damage:
            for value in result[-1]:
                value["spec"].pop("crush_device_class", None)
        return result

    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", contained)
    if damage is None:
        assert native.normalized_services(groups) == groups
    else:
        with pytest.raises(ceph_native.Refusal, match="^osd-unqualified$"):
            native.normalized_services(groups)


@pytest.mark.parametrize("realized,up,ready", [("fast", 1, True), (None, 0, False), ("ssd", 0, False), (None, 1, False), ("ssd", 1, "osd-class-drift")])
def test_osd_readiness_proves_the_declared_class(core_request, realized, up, ready):
    core_request["services"][-1]["spec"]["crush_device_class"] = "fast"
    core_request["config"] = fixtures.memory_config()
    answers = fixtures.memory_observation_answers(core_request)
    answers[("osd", "dump")]["osds"][0]["up"] = up
    nodes = [{"id": -1, "type": "root", "name": "default"}]
    nodes += [{"id": i, "type": "osd", "name": "osd." + str(i), **({"device_class": "fast"} if i else {})} for i in range(3)]
    if realized:
        nodes[1]["device_class"] = realized
    answers[("osd", "tree")] = {"nodes": nodes}
    native, calls = fixtures.worker(core_request, answers)
    if isinstance(ready, str):
        with pytest.raises(ceph_native.Refusal, match="^" + ready + "$"):
            native.observe()
        return
    observed = native.observe()
    assert observed["ready"] is ready and len(observed["osds"]) == 3
    assert sum("osd" in argv and "tree" in argv for argv in calls) == 1
    assert native.markers.writes == []

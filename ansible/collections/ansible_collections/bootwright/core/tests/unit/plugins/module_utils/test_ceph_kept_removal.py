"""A never-used cluster taken back from a kept host that stays running (B1862): proof before effect, the FSID-scoped zap, the records
last, positive absence, a lost reply repeated under its attempt, and a fresh bootstrap that finds nothing foreign afterwards.

The host's fixed layout is the tiebreaker removal's temporary root; LVM and ceph-volume are scripted over volumes held in memory.
"""

from __future__ import annotations

import json
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_evidence, ceph_export_native, ceph_host_runtime, ceph_native, ceph_native_kept as kept, ceph_packages,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_host_execution import host_request, request_value, retained_host_request  # noqa: F401 pylint: disable=unused-import
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tiebreaker_removal as base
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_tiebreaker_removal import tiebreaker  # noqa: F401 pylint: disable=unused-import

FSID, OTHER_FSID = base.FSID, base.OTHER_FSID
NODE, STABLE, OTHER_NODE = "/dev/vdb", "/dev/disk/by-id/virtio-node-5", "/dev/vdc"
PV, OTHER_PV = "aaaaaa-bbbb-cccc-dddd-eeee-ffff-000001", "aaaaaa-bbbb-cccc-dddd-eeee-ffff-000002"
DEVICE_NODES = kept.KeptHostRemoval.device_nodes
WWN_PATH, WWN_NODE = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "/dev/sdb"
SERIAL_PATH, ALIAS, ALIAS_NODE = "/dev/disk/by-serial/S3EVNX0K123456", "/dev/disk/by-id/nvme-SAMSUNG_MZ7LH_S3EVNX0K123456", "/dev/nvme0n1"
SERIAL_PV = "aaaaaa-bbbb-cccc-dddd-eeee-ffff-000003"


class DirMarkers:
    """The cluster's marker directory as files under the host root, so a discard of the directory removes the records too."""

    def __init__(self, root, records):
        self.root, self.path, self.lose_after = root, ceph_native.ROOT + "/" + FSID, None
        os.makedirs(self.real(), mode=0o755, exist_ok=True)
        for name, value in records.items():
            self.write(name, value)

    def real(self, name=""):
        return os.path.join(self.root, self.path.lstrip("/"), name)

    def read(self, name="owner.json"):
        try:
            with open(self.real(name), "rb") as handle:
                return json.loads(handle.read())
        except FileNotFoundError:
            return None

    def owned(self):
        return self.read() is not None

    def write(self, name, value):
        existing = self.read(name)
        ceph_native.require(existing in (None, value), "ownership")
        with open(self.real(name), "wb") as handle:
            handle.write(ceph_native.canonical(value))
        if name == self.lose_after:
            self.lose_after = None
            raise ceph_native.Refusal("native-unknown")


class KeptHost(base.Host):
    """The tiebreaker's native side plus LVM: this cluster's OSD volume on the named disk, and another's volume on a disk it does not name."""

    def adopt(self, host):
        self.__dict__.update(host.__dict__)
        self.lvs = [{"lv_path": "/dev/ceph-a/osd-block-1", "lv_uuid": "lv-1", "devices": NODE + "(0)",
                     "lv_tags": "ceph.cluster_fsid=" + FSID + ",ceph.osd_id=0,ceph.type=block"},
                    {"lv_path": "/dev/ceph-b/osd-block-2", "lv_uuid": "lv-2", "devices": OTHER_NODE + "(0)",
                     "lv_tags": "ceph.cluster_fsid=" + OTHER_FSID + ",ceph.osd_id=0,ceph.type=block"}]
        self.pvs = [{"pv_name": NODE, "pv_uuid": PV, "vg_name": "ceph-a"}, {"pv_name": OTHER_NODE, "pv_uuid": OTHER_PV, "vg_name": "ceph-b"}]
        self.devices_file = {PV.replace("-", ""), OTHER_PV.replace("-", "")}
        self.native, self.zap_lose, self.zap_fail, self.at_zap = [], False, False, []
        self.zap_partial = False
        self.stable = {STABLE: NODE}
        return self

    def command(self, argv, timeout=60, json_result=True):
        if argv[0] == "/usr/sbin/lvs":
            return {"report": [{"lv": [dict(row) for row in self.lvs]}]}
        if argv[0] == "/usr/sbin/pvs":
            return {"report": [{"pv": [dict(row) for row in self.pvs]}]}
        if argv[:2] == ["/usr/bin/systemctl", "stop"] or argv[0] == "/usr/sbin/lvmdevices" or "ceph-volume" in argv:
            self.native.append(list(argv))
            if "ceph-volume" in argv:
                self.zap(argv)
            if argv[0] == "/usr/sbin/lvmdevices":
                self.devices_file.discard(argv[-1])
            return b""
        return base.Host.command(self, argv, timeout=timeout, json_result=json_result)

    def zap(self, argv):
        assert argv[:-1] == [self.tool, "--image", self.request["image"], "ceph-volume", "--fsid", FSID, "--", "lvm", "zap", "--destroy"]
        node = self.stable[argv[-1]]
        self.at_zap.append({"credential": os.path.lexists(self.path("etc/ceph/podman-auth.json")), "rm-cluster": bool(self.commands)})
        if self.zap_fail:
            self.zap_fail = False
            raise ceph_native.NativeFailure("native-command", "ceph-volume zap", b"device busy")
        self.lvs = [row for row in self.lvs if not row["devices"].startswith(node + "(")]
        if self.zap_partial:
            self.zap_partial = False
            raise ceph_native.NativeFailure("native-command", "ceph-volume zap", b"pvremove failed")
        self.pvs = [row for row in self.pvs if row["pv_name"] != node]
        if self.zap_lose:
            self.zap_lose = False
            raise ceph_native.Refusal("native-unknown")


@pytest.fixture
def host(tiebreaker, monkeypatch):  # noqa: F811 pylint: disable=redefined-outer-name
    value = KeptHost.__new__(KeptHost).adopt(tiebreaker)
    value.markers = DirMarkers(value.root, tiebreaker.markers.records)
    os.makedirs(value.path("var/lib/ceph", FSID, "osd.0"))
    os.makedirs(value.path("var/lib/bootwright/ceph-packages"), exist_ok=True)
    monkeypatch.setattr(kept.KeptHostRemoval, "device_nodes", lambda self: {node: stable for stable, node in value.stable.items()})
    monkeypatch.setattr(kept, "lvm_devices_lists", lambda identifier: identifier in value.devices_file)
    return value


def removal(host):  # pylint: disable=redefined-outer-name
    return kept.KeptHostRemoval(host)


def drained(host):  # pylint: disable=redefined-outer-name
    """Every call of one host's removal, repeated while it reports work remaining, as the controller's rounds do."""
    results = []
    while not results or results[-1]["remaining"]:
        assert len(results) < 8
        results.append(removal(host).remove())
    return results


def assert_clean(host):  # pylint: disable=redefined-outer-name
    for relative in ("var/lib/ceph/" + FSID, "var/log/ceph/" + FSID, "etc/ceph/ceph.conf", "etc/ceph/podman-auth.json",
                     "etc/ssh/sshd_config.d/" + base.policy_name(host.request), "var/lib/bootwright/ceph/" + FSID):
        assert not os.path.lexists(host.path(relative)), relative
    assert open(host.path("home/operator/.ssh/authorized_keys"), "rb").read() == (base.OPERATOR_LINE + "\n").encode()
    assert sorted(os.listdir(host.path("etc/ssh/sshd_config.d"))) == ["50-operator.conf"]
    assert [row["lv_uuid"] for row in host.lvs] == ["lv-2"] and [row["pv_name"] for row in host.pvs] == [OTHER_NODE]
    assert host.devices_file == {OTHER_PV.replace("-", "")}
    for retained in ("var/lib/ceph", "etc/systemd/system/ceph.target", "var/lib/bootwright/ceph-packages"):
        assert os.path.lexists(host.path(retained)), retained


def test_a_kept_host_loses_the_whole_fsid_and_its_records_and_keeps_everything_else(host):  # pylint: disable=redefined-outer-name
    assert removal(host).observe() == {"removed": False, "attempted": False}
    assert drained(host) == [{"changed": True, "removed": False, "attempted": True, "remaining": True},
                             {"changed": True, "removed": True, "attempted": True, "remaining": False}]
    zap = [argv for argv in host.native if "ceph-volume" in argv]
    assert host.native[0] == ["/usr/bin/systemctl", "stop", "ceph-" + FSID + ".target"] and len(zap) == 1
    assert host.commands == [[host.tool, "rm-cluster", "--fsid", FSID, "--force"]]
    assert all("--zap-osds" not in argv for argv in host.commands)
    assert_clean(host)
    assert removal(host).observe() == {"removed": True, "attempted": False}
    assert removal(host).remove() == {"changed": False, "removed": True, "attempted": False, "remaining": False}
    assert len(host.commands) == 1 and len(zap) == 1


def test_the_zap_runs_before_the_registry_credential_goes_and_before_rm_cluster(host):  # pylint: disable=redefined-outer-name
    drained(host)
    assert host.at_zap == [{"credential": True, "rm-cluster": False}]


def test_the_zap_names_only_options_the_cephadm_parser_declares_before_any_zap(host, monkeypatch):  # pylint: disable=redefined-outer-name
    declared = []

    def undeclared(argv):
        declared.append(list(argv))
        if "ceph-volume" in argv:
            raise ceph_native.NativeFailure("native-option-undeclared", "cephadm", b"--image")

    monkeypatch.setattr(host, "require_declared_options", undeclared)
    with pytest.raises(ceph_native.Refusal, match="^native-option-undeclared$"):
        drained(host)
    assert [argv for argv in declared if "ceph-volume" in argv] == [[host.tool, "--image", host.request["image"], "ceph-volume", "--fsid", FSID]]
    assert not [argv for argv in host.native if "ceph-volume" in argv] and host.markers.read(kept.zap_record(STABLE)) is None


def test_a_zap_that_fails_before_any_effect_is_zapped_again_and_the_removal_completes(host):  # pylint: disable=redefined-outer-name
    host.zap_fail = True
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-failed$"):
        removal(host).remove()
    assert host.markers.read(kept.zap_record(STABLE))["pvs"] == [PV] and [row["pv_uuid"] for row in host.pvs] == [PV, OTHER_PV]
    assert removal(host).observe() == {"removed": False, "attempted": True}
    assert removal(host).owed_devices() == []
    assert drained(host)[-1] == {"changed": True, "removed": True, "attempted": True, "remaining": False}
    assert len([argv for argv in host.native if "ceph-volume" in argv]) == 2
    assert_clean(host)


def test_a_zap_that_removed_the_volumes_but_not_the_pv_is_zapped_again_and_never_read_removed(host):  # pylint: disable=redefined-outer-name
    host.zap_partial = True
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-failed$"):
        removal(host).remove()
    assert removal(host).tagged() == [] and PV in [row["pv_uuid"] for row in host.pvs]
    assert removal(host).zap_residue() == [STABLE] and removal(host).observe() == {"removed": False, "attempted": True}
    assert drained(host)[-1] == {"changed": True, "removed": True, "attempted": True, "remaining": False}
    assert len([argv for argv in host.native if "ceph-volume" in argv]) == 2 and PV not in [row["pv_uuid"] for row in host.pvs]
    assert_clean(host)


def test_a_zap_residue_beside_another_clusters_volume_refuses_before_any_effect(host):  # pylint: disable=redefined-outer-name
    host.zap_partial = True
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-failed$"):
        removal(host).remove()
    node = host.stable[STABLE]
    host.lvs.append({"lv_path": "/dev/other/lv", "lv_uuid": "lv-x", "devices": node + "(0)", "lv_tags": "ceph.cluster_fsid=" + "1" * 8 + FSID[8:]})
    zaps = len([argv for argv in host.native if "ceph-volume" in argv])
    with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
        removal(host).remove()
    assert len([argv for argv in host.native if "ceph-volume" in argv]) == zaps and not host.commands


def test_each_device_is_zapped_in_its_own_call_and_a_lost_reply_between_calls_goes_on(host):  # pylint: disable=redefined-outer-name
    for index in (2, 3):
        node, stable, pv = "/dev/vd" + "xyz"[index - 2], "/dev/disk/by-id/virtio-node-5-" + str(index), PV[:-1] + str(index + 2)
        host.stable[stable] = node
        host.lvs.append({"lv_path": "/dev/ceph-a" + str(index) + "/osd-block", "lv_uuid": "lv-o" + str(index), "devices": node + "(0)",
                         "lv_tags": "ceph.cluster_fsid=" + FSID + ",ceph.osd_id=" + str(index) + ",ceph.type=block"})
        host.pvs.append({"pv_name": node, "pv_uuid": pv, "vg_name": "ceph-a" + str(index)})
        host.devices_file.add(pv.replace("-", ""))

    def zaps():
        return [argv[-1] for argv in host.native if "ceph-volume" in argv]

    assert removal(host).remove()["remaining"] is True and len(zaps()) == 1
    host.zap_lose = True
    with pytest.raises(ceph_native.Refusal, match="^native-unknown$"):
        removal(host).remove()
    assert len(zaps()) == 2 and removal(host).observe() == {"removed": False, "attempted": True}
    results = drained(host)
    assert [result["remaining"] for result in results] == [True, False] and len(host.commands) == 1
    assert sorted(zaps()) == sorted(host.stable) and len(set(zaps())) == 3
    assert_clean(host)


@pytest.mark.parametrize("lost", ["attempt", "zap", "rm-cluster", "completion"])
def test_a_lost_reply_is_observed_as_attempted_and_the_next_removal_finishes_without_repeating_what_is_gone(host, lost):  # pylint: disable=redefined-outer-name
    if lost == "attempt":
        host.markers.lose_after = kept.ATTEMPT
    elif lost == "completion":
        host.markers.lose_after = kept.COMPLETE
    elif lost == "zap":
        host.zap_lose = True
    else:
        host.lose = True
    with pytest.raises(ceph_native.Refusal, match="^native-unknown$"):
        drained(host)
    assert removal(host).observe() == {"removed": False, "attempted": True}
    assert drained(host)[-1]["removed"] is True
    assert len([argv for argv in host.native if "ceph-volume" in argv]) == 1
    assert len(host.commands) == 1
    assert_clean(host)


DISCARDED = ["owner.json", kept.ATTEMPT, kept.COMPLETE, "native-removal-sshd.json", "native-removal-sshd-reloaded.json", "ssh-authorization.json"]


@pytest.mark.parametrize("interrupted", DISCARDED)
def test_a_discard_interrupted_after_any_record_is_read_back_as_a_residue_and_finished(host, monkeypatch, interrupted):  # pylint: disable=redefined-outer-name
    original, fired = kept.remove_tree, []

    def lost(parent, name, depth=0):
        original(parent, name, depth)
        if name == interrupted:
            fired.append(name)
            raise ceph_native.Refusal("native-unknown")

    monkeypatch.setattr(kept, "remove_tree", lost)
    with pytest.raises(ceph_native.Refusal, match="^native-unknown$"):
        drained(host)
    assert fired == [interrupted]
    monkeypatch.setattr(kept, "remove_tree", original)
    assert not os.path.lexists(host.markers.real()) and kept.directory_present(host.markers.path)
    assert removal(host).observe() == {"removed": False, "attempted": True}
    assert removal(host).remove() == {"changed": True, "removed": True, "attempted": True, "remaining": False}
    assert_clean(host) is None and not kept.directory_present(host.markers.path)
    assert removal(host).observe() == {"removed": True, "attempted": False}


@pytest.mark.parametrize("shape", ["beside-another", "unnamed-device", "shared-group", "another-fsid-directory", "altered-drop-in"])
def test_a_host_holding_what_this_cluster_cannot_prove_its_own_refuses_before_any_effect(host, shape):  # pylint: disable=redefined-outer-name
    if shape == "another-fsid-directory":
        os.makedirs(host.path("var/lib/ceph", OTHER_FSID, "mon.node-5"))
    elif shape == "altered-drop-in":
        with open(host.path("etc/ssh/sshd_config.d", base.policy_name(host.request)), "ab") as handle:
            handle.write(b"PermitRootLogin yes\n")
    elif shape == "beside-another":
        host.lvs.append({"lv_path": "/dev/ceph-a/other", "lv_uuid": "lv-3", "devices": NODE + "(9)", "lv_tags": "ceph.cluster_fsid=" + OTHER_FSID})
    elif shape == "unnamed-device":
        host.lvs.append({"lv_path": "/dev/ceph-c/osd-block-3", "lv_uuid": "lv-3", "devices": "/dev/vdd(0)", "lv_tags": "ceph.cluster_fsid=" + FSID})
    else:
        host.pvs.append({"pv_name": "/dev/vde", "pv_uuid": "aaaaaa-bbbb-cccc-dddd-eeee-ffff-000003", "vg_name": "ceph-a"})
    before = base.tree(host.root)
    with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
        removal(host).remove()
    assert base.tree(host.root) == before and host.native == [] and host.commands == [] and host.sshd == []


@pytest.fixture
def physical(host, monkeypatch):  # pylint: disable=redefined-outer-name
    """The kept host as a physical one: this cluster's volumes on a WWN selection and on a serial selection, each reached only by the stable
    path the request or the host preparation's binding names, which resolve to kernel nodes no call is ever given."""
    monkeypatch.setattr(kept.KeptHostRemoval, "device_nodes", DEVICE_NODES)
    host.host.update(physical=True, devices=[{"path": WWN_PATH, "wwn": "0x5000c500a1b2c3d4", "serial": "", "sizeBytes": 0},
                                             {"path": SERIAL_PATH, "serial": "S3EVNX0K123456", "sizeBytes": 1 << 40}])
    host.lvs = [dict(row, devices=WWN_NODE + "(0)") if row["lv_uuid"] == "lv-1" else row for row in host.lvs] + [
        {"lv_path": "/dev/ceph-p/osd-block-3", "lv_uuid": "lv-3", "devices": ALIAS_NODE + "(0)", "lv_tags": "ceph.cluster_fsid=" + FSID + ",ceph.osd_id=1"}]
    host.pvs = [dict(row, pv_name=WWN_NODE) if row["pv_uuid"] == PV else row for row in host.pvs] + [
        {"pv_name": ALIAS_NODE, "pv_uuid": SERIAL_PV, "vg_name": "ceph-p"}]
    host.devices_file.add(SERIAL_PV.replace("-", ""))
    with open(host.markers.real("owner.json"), "wb") as handle:
        handle.write(ceph_native.canonical(ceph_native.marker_identity(host.request)))
    host.stable = {WWN_PATH: WWN_NODE, ALIAS: ALIAS_NODE}
    host.bound = {SERIAL_PATH: {"path": SERIAL_PATH, "serial": "S3EVNX0K123456", "alias": ALIAS, "kernelPath": ALIAS_NODE, "wwn": "", "sizeBytes": 1 << 40},
                  WWN_PATH: {"path": WWN_PATH, "serial": "WD-1", "wwn": "0x5000c500a1b2c3d4", "kernelPath": WWN_NODE, "sizeBytes": 1 << 40}}
    host.binding_reads, host.binding_failure = [], None

    def physical_devices():
        host.binding_reads.append(True)
        if host.binding_failure is not None:
            raise ceph_native.Refusal(host.binding_failure)
        return dict(host.bound)

    real = os.path.realpath
    monkeypatch.setattr(os.path, "realpath", lambda path, *options: host.stable.get(path) or real(path, *options))
    host.physical_devices = physical_devices
    return host


def test_a_physical_host_zaps_its_disks_only_by_their_stable_paths_and_reads_them_free(physical):  # pylint: disable=redefined-outer-name
    results = drained(physical)
    assert [result["remaining"] for result in results] == [True, True, False] and results[-1]["removed"] is True
    zapped = [argv[-1] for argv in physical.native if "ceph-volume" in argv]
    assert sorted(zapped) == sorted([WWN_PATH, ALIAS]) and not [path for path in zapped if path in (WWN_NODE, ALIAS_NODE, SERIAL_PATH)]
    assert physical.binding_reads and [row["lv_uuid"] for row in physical.lvs] == ["lv-2"] and [row["pv_name"] for row in physical.pvs] == [OTHER_NODE]
    assert physical.devices_file == {OTHER_PV.replace("-", "")} and physical.commands == [[physical.tool, "rm-cluster", "--fsid", FSID, "--force"]]
    assert removal(physical).observe() == {"removed": True, "attempted": False} and removal(physical).tagged() == []


@pytest.mark.parametrize("change", ["no-binding", "serial-unbound", "wwn-unbound", "wwn-moved", "duplicate-node", "readback-failed"])
def test_a_physical_disk_its_binding_no_longer_proves_refuses_before_any_effect(physical, change):  # pylint: disable=redefined-outer-name
    if change == "no-binding":
        physical.bound = {}
    elif change == "serial-unbound":
        del physical.bound[SERIAL_PATH]
    elif change == "wwn-unbound":
        del physical.bound[WWN_PATH]
    elif change == "wwn-moved":
        physical.bound[WWN_PATH] = dict(physical.bound[WWN_PATH], kernelPath="/dev/sdc")
    elif change == "duplicate-node":
        physical.stable[ALIAS] = WWN_NODE
        physical.bound[SERIAL_PATH] = dict(physical.bound[SERIAL_PATH], kernelPath=WWN_NODE)
    else:
        physical.binding_failure = "device-identity"
    before = base.tree(physical.root)
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        removal(physical).remove()
    assert base.tree(physical.root) == before and physical.native == [] and physical.commands == []


def test_an_unclaimed_physical_host_reads_no_binding_and_a_volume_of_this_fsid_on_its_serial_disk_refuses(physical):  # pylint: disable=redefined-outer-name
    os.unlink(physical.markers.real("owner.json"))
    with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
        removal(physical).tagged()
    assert physical.binding_reads == [] and physical.native == []


def test_a_fresh_bootstrap_after_the_teardown_finds_nothing_foreign(host):  # pylint: disable=redefined-outer-name
    restored = dict(host.request, owner="op-" + "e" * 32, fsid="1b2c3d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d")

    def admission():
        fd = os.open("/var/lib/ceph", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            ceph_packages.ceph_directory(fd, restored, False)
        finally:
            os.close(fd)

    with pytest.raises(ceph_native.Refusal, match="^foreign-cluster$"):
        admission()
    drained(host)
    admission()
    assert not [row for row in host.lvs if NODE in row["devices"] or "ceph.cluster_fsid=" + FSID in row["lv_tags"]]
    assert not [name for name in os.listdir(host.path("etc/ssh/sshd_config.d")) if name.startswith("05-bootwright-ceph-")]
    keys = open(host.path("home/operator/.ssh/authorized_keys"), "rb").read().decode()
    assert "bootwright:" + host.request["owner"] not in keys.split()
    assert not os.path.lexists(host.path("var/lib/bootwright/ceph", FSID)) and host.daemons() == []


class Transport:
    """Each kept host's answers to the teardown's rounds, in host order."""

    def __init__(self, request, answers):
        self.request, self.answers, self.calls = request, answers, []

    def call(self, host, phase, *unused, **unused_options):
        self.calls.append((host["name"], phase))
        answer = self.answers(host, phase)
        if isinstance(answer, Exception):
            raise answer
        return answer


def answered(host, phase):
    return {"kept_unused": {"unused": True}, "kept_managers_stop": {"changed": True}, "kept_managers_observe": {"stopped": True},
            "kept_host_remove": {"changed": True, "removed": True, "attempted": True, "remaining": False},
            "kept_host_observe": {"removed": True, "attempted": False}}[phase]


def request():
    return base.request()


def test_the_cluster_teardown_stops_every_manager_before_any_host_is_cleaned():
    value = request()
    transport = Transport(value, answered)
    result = ceph_core.execute_kept(value, "destroy", base.DIGEST, transport, {})
    names = [host["name"] for host in value["hosts"]]
    phases = [phase for unused, phase in transport.calls]
    assert transport.calls[0] == (value["bootstrap"], "kept_unused")
    assert phases == ["kept_unused"] + ["kept_managers_stop"] * len(names) + ["kept_managers_observe"] * len(names) + ["kept_host_remove"] * len(names)
    assert result["changed"] is True
    assert [host["host"] for host in result["evidence"]["hosts"]] == names
    assert ceph_evidence.kept_evidence(result["evidence"], base.DIGEST, value) == result["evidence"]
    observed = ceph_core.execute_kept(value, "observe", base.DIGEST, Transport(value, answered), {})
    assert all(host["removed"] for host in observed["evidence"]["hosts"])


@pytest.mark.parametrize("failure, reason", [("running", "managers-running"), ("unclean", "not-removed"), ("unreachable", "transport"),
                                             ("used", "kept-foreign-client")])
def test_the_cluster_teardown_refuses_a_running_manager_an_unclean_host_or_one_that_does_not_answer(failure, reason):
    value = request()

    def answers(host, phase):
        if host["name"] == value["hosts"][1]["name"]:
            if failure == "running" and phase == "kept_managers_observe":
                return {"stopped": False}
            if failure == "unclean" and phase == "kept_host_remove":
                return {"changed": True, "removed": False, "attempted": True, "remaining": False}
            if failure == "unreachable" and phase == "kept_managers_stop":
                return ceph_native.Refusal("transport")
        if failure == "used" and phase == "kept_unused":
            return ceph_native.RemoteRefusal("native-refusal:kept-foreign-client@fixed")
        return answered(host, phase)

    transport = Transport(value, answers)
    with pytest.raises(ceph_native.Refusal) as refused:
        ceph_core.execute_kept(value, "destroy", base.DIGEST, transport, {})
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == reason
    if failure != "unclean":
        assert not [phase for unused, phase in transport.calls if phase == "kept_host_remove"]
    if failure == "used":
        assert [phase for unused, phase in transport.calls] == ["kept_unused"]


def test_the_teardown_calls_each_host_again_while_it_reports_work_remaining_and_refuses_past_its_bound():
    value = request()
    value["hosts"][0]["devices"] = [{"path": "/dev/vd" + letter} for letter in "bcd"]
    calls = {}

    def answers(host, phase):
        if phase != "kept_host_remove":
            return answered(host, phase)
        calls[host["name"]] = calls.get(host["name"], 0) + 1
        remaining = host["name"] == value["hosts"][0]["name"] and calls[host["name"]] <= len(host.get("devices", []))
        return {"changed": calls[host["name"]] == 1, "removed": not remaining, "attempted": True, "remaining": remaining}

    result = ceph_core.execute_kept(value, "destroy", base.DIGEST, Transport(value, answers), {})
    first = value["hosts"][0]
    assert calls[first["name"]] == len(first.get("devices", [])) + 1 and all(calls[host["name"]] == 1 for host in value["hosts"][1:])
    assert all(host["removed"] and host["attempted"] for host in result["evidence"]["hosts"]) and result["changed"] is True

    def endless(host, phase):
        if phase != "kept_host_remove":
            return answered(host, phase)
        return {"changed": True, "removed": False, "attempted": True, "remaining": True}

    transport = Transport(value, endless)
    with pytest.raises(ceph_native.Refusal, match="^not-removed$"):
        ceph_core.execute_kept(value, "destroy", base.DIGEST, transport, {})
    bound = max(len(host.get("devices", [])) for host in value["hosts"]) + 2
    assert len([phase for unused, phase in transport.calls if phase == "kept_host_remove"]) == bound * len(value["hosts"])


def test_only_the_kept_selector_reaches_the_kept_teardown():
    value = request()
    ceph_core.admit_selectors(value, "destroy", {}, ceph_core.KEPT_RETIREMENT, None)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.admit_selectors(value, "destroy", {}, "retire-everything", None)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute_kept(dict(value, action="pool"), "destroy", base.DIGEST, Transport(value, answered), {})


class ReleaseTransport:
    def __init__(self):
        self.calls = []

    def call(self, host, phase, *unused, **unused_options):
        self.calls.append((host["name"], phase))
        return {"changed": True, "removed": True} if phase == "host_release" else {"removed": True}


def test_a_kept_host_release_runs_only_its_release_phase(host_request):  # noqa: F811 pylint: disable=redefined-outer-name
    transport = ReleaseTransport()
    released = ceph_core.execute_host(host_request, "destroy", base.DIGEST, transport, kept=True)
    observed = ceph_core.execute_host(host_request, "observe-remove", base.DIGEST, transport, kept=True)
    assert transport.calls == [("node-2", "host_release"), ("node-2", "host_release_observe")]
    assert released["changed"] is True and released["evidence"]["removed"] is True and observed["evidence"]["removed"] is True
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute_host(host_request, "observe", base.DIGEST, transport, kept=True)


def test_a_kept_physical_host_release_runs_only_its_release_phase(retained_host_request):  # noqa: F811 pylint: disable=redefined-outer-name
    transport = ReleaseTransport()
    released = ceph_core.execute_host(retained_host_request, "destroy", base.DIGEST, transport, kept=True)
    observed = ceph_core.execute_host(retained_host_request, "observe-remove", base.DIGEST, transport, kept=True)
    assert transport.calls == [("node-2", "host_release"), ("node-2", "host_release_observe")]
    assert released["evidence"]["removed"] is True and observed["evidence"]["removed"] is True


def test_the_host_release_removes_its_preparation_records_and_keeps_the_package_records(request_value, tmp_path, monkeypatch):  # noqa: F811 pylint: disable=redefined-outer-name
    real_fstat = os.fstat

    def rooted(fd):
        actual = real_fstat(fd)
        if not stat.S_ISDIR(actual.st_mode):
            return actual
        return SimpleNamespace(**dict({name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}, st_uid=0, st_gid=0))

    markers = ceph_native.Markers(request_value, root=str(tmp_path / "ceph-host"))
    packages = ceph_native.Markers(request_value, root=str(tmp_path / "ceph-packages"))
    os.makedirs(markers.path, mode=0o755)
    os.makedirs(packages.path, mode=0o755)
    runtime = ceph_host_runtime.HostRuntime(request_value, "node-1", markers=markers, packages=packages)
    markers.write("owner.json", ceph_native.marker_identity(runtime.request))
    markers.write("scope.json", runtime.scope())
    markers.write("route.json", {"version": ceph_host_runtime.ROUTE_VERSION})
    monkeypatch.setattr(os, "fstat", rooted)
    assert runtime.host_release_observe() == {"removed": False}
    assert runtime.host_release() == {"changed": True, "removed": True}
    assert not os.path.lexists(markers.path) and os.path.isdir(packages.path)
    assert runtime.host_release_observe() == {"removed": True}
    assert runtime.host_release() == {"changed": False, "removed": True}
    os.makedirs(markers.path, mode=0o755)
    markers.write("owner.json", ceph_native.marker_identity(runtime.request))
    markers.write("scope.json", dict(runtime.scope(), host="node-2"))
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        runtime.host_release()
    assert os.path.lexists(markers.path)


ALLOWED = ["mon.", "client.admin", "client.bootstrap-osd", "client.bootstrap-rbd-mirror", "mgr.node-1.abcdef", "osd.0", "osd.12",
           "client.crash.node-1", "client.ceph-exporter.node-2"]


class UsageWorker:
    """The bootstrap host's native reads for the usage check: its claim, its FSID's daemon directories and the two admin reads."""

    def __init__(self, value):
        self.request = value
        self.host = next(host for host in value["hosts"] if host["name"] == value["bootstrap"])
        self.markers = base.Markers({"owner.json": ceph_native.marker_identity(value)})
        self.entities = list(ALLOWED)
        self.pools = [{"name": ".mgr", "id": 1, "stats": {"objects": 4, "stored": 9000}}, {"name": "rbd", "id": 2, "stats": {"objects": 0, "stored": 0}}]
        self.reads, self.unread = [], False

    def ceph(self, *args, **unused):
        self.reads.append(args)
        if self.unread:
            raise ceph_native.NativeFailure("native-timeout", "ceph df", b"timed out")
        if args == ("auth", "ls"):
            return {"auth_dump": [{"entity": name, "key": "AQ-secret"} for name in self.entities]}
        assert args == ("df",)
        return {"stats": {}, "pools": self.pools}


@pytest.fixture
def usage(monkeypatch):
    worker = UsageWorker(request())
    worker.daemons = ["mon." + worker.host["name"], "mgr." + worker.host["name"] + ".x"]
    monkeypatch.setattr(kept, "listing", lambda path: list(worker.daemons) if path == "/var/lib/ceph/" + FSID
                        else sorted(worker.markers.records) if path == worker.markers.path else [])
    return worker


def check(worker):
    return kept.KeptUsage(worker).check()


def test_a_cluster_only_bootwright_used_passes_and_records_the_pass_so_a_later_attempt_needs_no_manager(usage):  # pylint: disable=redefined-outer-name
    assert check(usage) == {"unused": True}
    assert usage.reads == [("auth", "ls"), ("df",)]
    assert usage.markers.read(kept.UNUSED) == kept.removal_record(usage.request, usage.host)
    usage.unread = True
    assert check(usage) == {"unused": True} and usage.reads[2:] == [("auth", "ls")]


def test_a_client_created_between_attempts_refuses_while_a_monitor_answers(usage):  # pylint: disable=redefined-outer-name
    assert check(usage) == {"unused": True}
    assert check(usage) == {"unused": True} and usage.reads[2:] == [("auth", "ls")]
    usage.entities.append("client.app")
    with pytest.raises(ceph_native.Refusal, match="^kept-foreign-client$"):
        check(usage)
    assert usage.reads[3:] == [("auth", "ls")]


@pytest.mark.parametrize("entity", ["client.app", "client.rgw.gateway.node-1.abc", "client.nfs.share", "mds.fs.node-1.abc", "client.bootstrap-x",
                                    "osd.01", "mon.node-1"])
def test_a_key_neither_the_bootstrap_nor_the_frozen_services_create_refuses_with_nothing_recorded(usage, entity):  # pylint: disable=redefined-outer-name
    usage.entities.append(entity)
    with pytest.raises(ceph_native.Refusal, match="^kept-foreign-client$"):
        check(usage)
    assert usage.markers.read(kept.UNUSED) is None


def test_an_mds_key_passes_only_while_the_frozen_services_place_an_mds(usage):  # pylint: disable=redefined-outer-name
    usage.entities.append("mds.fs.node-1.abc")
    usage.request = dict(usage.request, services=usage.request["services"] + [{"service_type": "mds", "service_id": "fs", "placement": {"hosts": []}}])
    assert check(usage) == {"unused": True}


@pytest.mark.parametrize("stats", [{"objects": 1, "stored": 0}, {"objects": 0, "stored": 1}])
def test_data_in_any_pool_but_the_managers_own_refuses_with_nothing_recorded(usage, stats):  # pylint: disable=redefined-outer-name
    usage.pools[1]["stats"] = stats
    with pytest.raises(ceph_native.Refusal, match="^kept-user-data$"):
        check(usage)
    assert usage.markers.read(kept.UNUSED) is None


@pytest.mark.parametrize("pools", [None, [{"name": "rbd", "stats": {"objects": "1", "stored": 0}}], [{"name": "rbd", "stats": {"objects": -1, "stored": 0}}]])
def test_a_usage_report_ceph_would_not_write_refuses_native_output(usage, pools):  # pylint: disable=redefined-outer-name
    usage.pools = pools
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        check(usage)


def test_a_cluster_that_cannot_answer_refuses_unread_with_the_native_cause(usage):  # pylint: disable=redefined-outer-name
    usage.unread = True
    with pytest.raises(ceph_native.Refusal, match="^kept-unread$") as refused:
        check(usage)
    assert refused.value.native_error and usage.markers.read(kept.UNUSED) is None


@pytest.mark.parametrize("state", ["unclaimed", "no-monitor"])
def test_a_host_the_cluster_never_reached_or_never_held_a_monitor_on_passes_without_a_read(usage, state):  # pylint: disable=redefined-outer-name
    if state == "unclaimed":
        usage.markers = base.Markers({})
    else:
        usage.daemons = ["crash." + usage.host["name"]]
    assert check(usage) == {"unused": True}
    assert usage.reads == [] and usage.markers.read(kept.UNUSED) is None


def dependents(usage):  # pylint: disable=redefined-outer-name
    """What an SMB cluster or a consumer would leave had it run: a dependent of a cluster taken back here never ran, so none of it passes."""
    usage.request = dict(usage.request, delegated=[{"name": "smb.files", "hosts": [usage.host["name"]]}])
    scope = {"owner": usage.request["owner"], "fsid": usage.request["fsid"], "export": {"name": "vault", "principals": ["health"]}}
    usage.markers.records.update({"export-vault.json": scope, "export-vault-health.json": scope})
    return ceph_export_native.prefix({"scope": {"owner": usage.request["owner"]}, "name": "vault"})


@pytest.mark.parametrize("entity", ["client.smb.fs.cluster.files", "client.smb.config.files.node-1.abcdef", "consumer"])
def test_a_dependents_key_refuses_even_beside_its_own_records(usage, entity):  # pylint: disable=redefined-outer-name
    identifier = dependents(usage)
    usage.entities.append("client." + identifier + "-health" if entity == "consumer" else entity)
    with pytest.raises(ceph_native.Refusal, match="^kept-foreign-client$"):
        check(usage)
    assert usage.markers.read(kept.UNUSED) is None


def test_the_smb_managers_pool_holding_objects_refuses_even_with_a_delegated_smb_cluster(usage):  # pylint: disable=redefined-outer-name
    dependents(usage)
    usage.pools.append({"name": ".smb", "id": 3, "stats": {"objects": 7, "stored": 4096}})
    with pytest.raises(ceph_native.Refusal, match="^kept-user-data$"):
        check(usage)


@pytest.mark.parametrize("entity", ["client.rbd-mirror-peer", "client.rbd-mirror.node-1.abcdef"])
def test_a_replications_peer_key_still_refuses(usage, entity):  # pylint: disable=redefined-outer-name
    usage.entities.append(entity)
    with pytest.raises(ceph_native.Refusal, match="^kept-foreign-client$"):
        check(usage)


def test_another_clusters_pass_record_refuses_ownership(usage):  # pylint: disable=redefined-outer-name
    usage.markers.write(kept.UNUSED, dict(kept.removal_record(usage.request, usage.host), owner="op-other"))
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        check(usage)
    assert usage.reads == []


def test_the_usage_check_is_a_core_phase_with_an_effect_and_names_closed_reasons():
    assert "kept_unused" in ceph_native.CORE_PHASES and "kept_unused" not in ceph_native.CORE_READ_ONLY_PHASES
    assert {"kept-foreign-client", "kept-user-data", "kept-unread"} <= set(ceph_core.CLUSTER_REASONS)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        kept.KeptUsage(SimpleNamespace(request=request(), host={"name": "node-5"}))

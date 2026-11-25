"""The managers' retirement: every physical host's managers of the FSID stopped and disabled before any FSID removal, then read back.

A running manager keeps the SSH connections it opened past the removal of its key line and writes the FSID's layout back onto a host
just cleaned, so the cluster block stops them all before the tiebreaker or any host block removes the FSID. systemd and podman are
scripted; the unit names are cephadm's ceph-<fsid>@mgr.<daemon id>.service wanted by ceph-<fsid>.target.
"""

from __future__ import annotations

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_evidence, ceph_native, ceph_native_stretch as stretch, ceph_transport
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tiebreaker_removal

FSID = test_ceph_tiebreaker_removal.FSID
DIGEST = test_ceph_tiebreaker_removal.DIGEST
UNIT = "ceph-" + FSID + "@mgr.node-1.abcdef.service"
WANTS = stretch.UNIT_ROOT + "/ceph-" + FSID + ".target.wants"


def test_only_the_manager_stop_waits_for_the_guest_boot(monkeypatch):
    request = physical_request()
    transport = ceph_transport.Transport(request, {})
    host = next(host for host in request["hosts"] if host.get("physical"))
    calls = []
    monkeypatch.setattr(transport, "wait_for_guest", lambda target: calls.append(("wait", target["name"])))

    def call(target, _source, payload, _timeout, **_):
        calls.append((payload["payload"]["phase"], target["name"]))

    monkeypatch.setattr(transport, "_call", call)
    transport.call(host, "managers_stop")
    transport.call(host, "managers_observe")
    assert calls == [("wait", host["name"]), ("managers_stop", host["name"]), ("managers_observe", host["name"])]


def physical_request():
    """The stretch request with its data hosts physical; the provided tiebreaker stays a provided host."""
    value = test_ceph_tiebreaker_removal.request()
    services = [service for service in value["services"] if service["service_type"] != "osd"]
    for number, host in enumerate(value["hosts"], start=1):
        if host.get("provided") is True:
            continue
        host.update(physical=True, hardware={"uuid": "11111111-1111-4111-8111-1111111111%02d" % number, "macs": ["02:00:00:00:%02x:01" % number]})
        # A physical host selects its disk by serial number, never by a kernel name.
        serial = "SYNTH-SERIAL-%d" % number
        host["devices"] = [{"path": "/dev/disk/by-serial/" + serial, "serial": serial, "sizeBytes": 1 << 40}]
        services.append({"service_type": "osd", "service_id": "data-%d" % number, "placement": {"hosts": [host["name"]]},
                         "spec": {"data_devices": {"paths": [host["devices"][0]["path"]]}}})
    value["services"] = services
    return value


class Host:
    """One physical host's systemd and podman: a manager unit's state, its enablement link and its container."""

    def __init__(self, value, name="node-1", state="active", enabled=True, container=True):
        self.request = value
        self.host = next(host for host in value["hosts"] if host["name"] == name)
        self.state, self.enabled, self.container = state, enabled, container
        self.commands = []
        self.restarts = False
        self.listed = None

    def command(self, argv, timeout=60, json_result=True):
        self.commands.append(list(argv))
        if argv[:2] == ["/usr/bin/systemctl", "list-units"]:
            assert argv[-1] == "ceph-" + FSID + "@mgr.*.service" and not json_result
            if self.listed is not None:
                return self.listed
            return (UNIT + " loaded " + self.state + " running Ceph mgr\n").encode() if self.state else b""
        if argv[:2] == ["/usr/bin/podman", "ps"]:
            assert "--all" not in argv and argv[-1] == "name=^ceph-" + FSID + "-mgr-"
            return [{"Names": ["ceph-" + FSID + "-mgr-node-1-abcdef"]}] if self.container else []
        assert argv[:3] == ["/usr/bin/systemctl", "disable", "--now"] and timeout == stretch.MANAGER_STOP_TIMEOUT and not json_result
        if not self.restarts:
            self.state, self.enabled, self.container = "inactive", False, False
        return b""


@pytest.fixture
def wants(monkeypatch):
    host = {}
    real = stretch.listing

    def listing(path):
        if path == WANTS:
            if host.get("foreign"):
                raise ceph_native.Refusal("tiebreaker-foreign")
            return ([UNIT] if host["value"].enabled else []) + host.get("extra", [])
        return real(path)

    monkeypatch.setattr(stretch, "listing", listing)
    return host


def manager_host(wants, **kwargs):
    host = Host(physical_request(), **kwargs)
    wants["value"] = host
    return host


def disables(host):
    return [argv for argv in host.commands if argv[:2] == ["/usr/bin/systemctl", "disable"]]


def test_a_running_enabled_manager_is_disabled_and_stopped_by_its_name_then_read_back_stopped(wants):
    host = manager_host(wants)
    assert stretch.ManagerStop(host).observe() == {"stopped": False}
    assert stretch.ManagerStop(host).stop() == {"changed": True}
    assert disables(host) == [["/usr/bin/systemctl", "disable", "--now", UNIT]]
    assert stretch.ManagerStop(host).observe() == {"stopped": True}


@pytest.mark.parametrize("state,enabled", [("inactive", True), ("failed", True), ("active", False), ("activating", False), ("deactivating", False)])
def test_a_manager_either_running_or_enabled_is_stopped(wants, state, enabled):
    host = manager_host(wants, state=state, enabled=enabled, container=False)
    assert stretch.ManagerStop(host).stop() == {"changed": True}
    assert disables(host) == [["/usr/bin/systemctl", "disable", "--now", UNIT]]


def test_a_host_with_no_manager_of_the_fsid_changes_nothing(wants):
    host = manager_host(wants, state="", enabled=False, container=False)
    assert stretch.ManagerStop(host).stop() == {"changed": False}
    assert not disables(host)
    host.state = "inactive"
    assert stretch.ManagerStop(host).stop() == {"changed": False} and not disables(host)


def test_a_manager_that_still_runs_after_the_stop_refuses(wants):
    host = manager_host(wants)
    host.restarts = True
    with pytest.raises(ceph_native.Refusal, match="^managers-running$"):
        stretch.ManagerStop(host).stop()
    container_only = manager_host(wants, state="", enabled=False, container=True)
    with pytest.raises(ceph_native.Refusal, match="^managers-running$"):
        stretch.ManagerStop(container_only).stop()
    assert not disables(container_only)


@pytest.mark.parametrize("listed", [b"ceph-" + FSID.encode() + b"@mgr.x.service; loaded active running\n",
                                    b"ceph-" + FSID.encode() + b"@mon.node-1.service loaded active running\n",
                                    b"ceph-" + FSID.encode() + b"@mgr.x.service loaded\n", "ceph-é@mgr.x.service loaded active running\n".encode()])
def test_a_listing_naming_anything_but_this_fsids_manager_units_refuses_before_any_stop(wants, listed):
    host = manager_host(wants)
    host.listed = listed
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        stretch.ManagerStop(host).stop()
    assert not disables(host)


def test_a_wants_entry_systemd_would_not_read_as_a_manager_unit_is_no_enablement_and_an_unreadable_wants_is_native_output(wants):
    host = manager_host(wants, state="", enabled=False, container=False)
    wants["extra"] = [UNIT + ".bak", "ceph-" + FSID + "@mon.node-1.service"]
    assert stretch.ManagerStop(host).stop() == {"changed": False} and not disables(host)
    wants["foreign"] = True
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        stretch.ManagerStop(host).observe()


def test_only_a_cluster_block_on_a_physical_host_stops_managers(wants):
    host = manager_host(wants, name="node-5")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        stretch.ManagerStop(host)
    virtual = manager_host(wants)
    del virtual.host["physical"]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        stretch.ManagerStop(virtual)
    other = manager_host(wants)
    other.request = dict(other.request, action="pool")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        stretch.ManagerStop(other)


class Transport:
    """Each host's answer per phase; the calls record the order the rounds reached the hosts in."""

    def __init__(self, answers):
        self.answers, self.calls = answers, []

    def call(self, host, phase, private=None, **unused):
        self.calls.append((host["name"], phase))
        answer = self.answers[phase]
        answer = answer(host["name"]) if callable(answer) else answer
        if isinstance(answer, Exception):
            raise answer
        return copy.deepcopy(answer)


def physical_hosts(value):
    return [host["name"] for host in value["hosts"] if host.get("physical") is True]


def test_the_retirement_stops_every_physical_hosts_managers_then_reads_every_one_back_before_any_removal():
    value = physical_request()
    for member in ("physical", "hardware"):
        value["hosts"][3].pop(member)
    value["hosts"][3]["devices"] = [{"path": "/dev/vdb", "serial": "disk4", "sizeBytes": 1024 ** 3}]
    value["services"] = [service for service in value["services"] if service.get("service_id") != "data-4"]
    value["services"].append({"service_type": "osd", "service_id": "data-4", "placement": {"hosts": ["node-4"]},
                              "spec": {"data_devices": {"paths": ["/dev/vdb"]}}})
    transport = Transport({"managers_stop": lambda name: {"changed": name == "node-2"}, "managers_observe": {"stopped": True}})
    result = ceph_core.execute(value, "destroy", DIGEST, transport, retirement="managers")
    hosts = physical_hosts(value)
    assert hosts == ["node-1", "node-2", "node-3"]
    assert sorted(transport.calls[:len(hosts)]) == sorted((name, "managers_stop") for name in hosts)
    assert sorted(transport.calls[len(hosts):]) == sorted((name, "managers_observe") for name in hosts)
    assert result == {"changed": True, "evidence": {"version": ceph_evidence.MANAGERS_EVIDENCE, "request": DIGEST, "owner": value["owner"],
                                                    "fsid": FSID, "stopped": True}}


def test_a_manager_started_again_after_its_hosts_stop_refuses_the_retirement_and_an_observation_reports_it():
    value = physical_request()
    answers = {"managers_stop": {"changed": True}, "managers_observe": lambda name: {"stopped": name != "node-3"}}
    with pytest.raises(ceph_native.Refusal, match="^managers-running$"):
        ceph_core.execute(value, "destroy", DIGEST, Transport(answers), retirement="managers")
    transport = Transport(answers)
    result = ceph_core.execute(value, "observe", DIGEST, transport, retirement="managers")
    assert result["changed"] is False and result["evidence"]["stopped"] is False
    assert {phase for unused, phase in transport.calls} == {"managers_observe"}


@pytest.mark.parametrize("phase,answer,reason", [
    ("managers_stop", ceph_native.Refusal("transport"), "transport"),
    ("managers_observe", ceph_native.Refusal("transport"), "transport"),
    ("managers_stop", ceph_native.RemoteRefusal("native-refusal:managers-running@fixed"), "managers-running"),
    ("managers_stop", {"changed": "yes"}, "evidence"),
    ("managers_observe", {"stopped": True, "extra": 1}, "evidence"),
])
def test_a_host_that_does_not_answer_or_answers_outside_the_closed_shape_never_reads_stopped(phase, answer, reason):
    answers = {"managers_stop": {"changed": False}, "managers_observe": {"stopped": True}}
    answers[phase] = lambda name: answer if name == "node-2" else answers_default(phase)
    with pytest.raises(ceph_native.Refusal) as refused:
        ceph_core.execute(physical_request(), "destroy", DIGEST, Transport(answers), retirement="managers")
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == reason


def answers_default(phase):
    return {"changed": False} if phase == "managers_stop" else {"stopped": True}


def test_the_managers_retirement_is_a_cluster_selector_with_no_output_and_only_its_reading_is_read_only():
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(test_ceph_tiebreaker_removal.request(), "destroy", DIGEST, Transport({}), retirement="managers")
    value = physical_request()
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(value, "apply", DIGEST, Transport({}), retirement="managers")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(dict(value, action="pool"), "destroy", DIGEST, Transport({}), retirement="managers")
    assert {"managers_observe", "managers_stop"} <= ceph_native.CORE_PHASES
    assert "managers_observe" in ceph_native.CORE_READ_ONLY_PHASES and "managers_stop" not in ceph_native.CORE_READ_ONLY_PHASES
    assert "managers-running" in ceph_core.CLUSTER_REASONS

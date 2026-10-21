"""Bounded cluster rounds, enrollment progress and service submissions."""

from __future__ import annotations

import copy
import threading
from collections import Counter

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core, test_ceph_bootstrap_recovery as recovery
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as tunnel

core_request = core.core_request
cluster_request = tunnel.cluster_request
native = tunnel.native


def six_hosts(request):
    for number in range(4, 7):
        host = copy.deepcopy(request["hosts"][0])
        host.update(name="node-" + str(number), machine="machine-" + str(number), address="192.0.2." + str(number))
        host["devices"][0]["serial"] = "disk" + str(number)
        request["hosts"].append(host)
    for service in request["services"]:
        service["placement"]["hosts"] = [host["name"] for host in request["hosts"]]


def test_six_host_fresh_call_plan_has_concurrent_rounds(core_request, monkeypatch, tmp_path):
    six_hosts(core_request)
    core_request["clusterSSHTransport"] = "verified-tunnel"
    core.package_intent(core_request)
    extra = copy.deepcopy(core_request["services"][-1])
    extra["service_id"] = "second"
    extra["placement"]["hosts"] = extra["placement"]["hosts"][3:]
    core_request["services"][-1]["placement"]["hosts"] = core_request["services"][-1]["placement"]["hosts"][:3]
    core_request["services"].append(extra)
    hosts = core_request["hosts"]
    phases, counts, lock = [], Counter(), threading.Lock()
    barriers = {phase: threading.Barrier(6) for phase in
                ("prerequisites", "devices", "tunnel_preflight", "claim", "authorize", "host_devices", "osd_prepare")}
    barriers["cache"] = threading.Barrier(5)

    class Transport(core.ReadinessTransport):
        def call(self, host, phase, private=None, **kwargs):
            with lock:
                counts[phase] += 1
                phases.append(phase)
            if phase in barriers and (phase != "cache" or host != hosts[0]):
                barriers[phase].wait(timeout=5)
            if phase == "prerequisites":
                return {"name": host["name"], "owned": False, "exists": False,
                        "recovery": staging.recovery_record(core_request, host["name"], "unclaimed")}
            if phase == "bootstrap_state":
                state = staging.recovery_record(core_request, host["name"], "unclaimed")
                state["evidence"] = dict(core.observation(core_request), owned=False, exists=False, removed=False)
                return state
            if phase in ("tunnel_configure", "tunnel_mount"):
                return {"changed": True}
            if phase == "enroll":
                return {"changed": True, "remaining": counts[phase] < 6}
            return super().call(host, phase, private, **kwargs)

    clock = core.ReadinessClock()
    transport = Transport(core_request, clock, ready_after=1)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    assert ceph_core.execute(core_request, "apply", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")},
                             now=clock.now, pause=clock.pause)["evidence"]["ready"]
    assert counts == Counter(bootstrap_state=1, dashboard=1, observe=2, host_state=12, prerequisites=6, devices=12,
                             tunnel_preflight=6, claim=6, authorize=6, bootstrap=1,
                             tunnel_configure=1, tunnel_mount=1, cache=6, enroll=6, license=1, host_devices=6, osd_prepare=6, services=4)
    # Nine host rounds plus the seed cache, five single calls (state/configure/mount/license/dashboard), n enrollment and S services.
    assert 10 + 5 + counts["enroll"] + counts["services"] == 25
    assert max(index for index, phase in enumerate(phases) if phase == "osd_prepare") < phases.index("services")


def test_round_joins_every_call_and_returns_first_refusal_in_host_order():
    barrier, returned = threading.Barrier(3), []

    class Transport:
        def call(self, host, phase, private):
            barrier.wait(timeout=5)
            returned.append(host)
            raise ceph_native.Refusal("first" if host == 1 else "later")

    with pytest.raises(ceph_native.Refusal, match="^first$"):
        ceph_core.host_round(Transport(), [1, 2, 3], "claim")
    assert sorted(returned) == [1, 2, 3]


def test_enrollment_batches_reserve_readback_and_resume_with_all_host_attempts(native):
    six_hosts(native.request)
    clock = recovery.manual_clock(native)
    inventory, events = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    native.execution_deadline = 160
    for count in range(1, 7):
        result = native.enroll()
        assert result == ({"changed": True, "remaining": True} if count < 6 else {"changed": True})
        assert len(inventory) == count
        assert native.markers.read("enrollment-node-" + str(count) + "-complete.json") is not None
    native.execution_deadline = clock["value"] + 640
    assert native.enroll() == {"changed": False}
    assert native.license() == {"changed": False}
    assert [event for event in events if event.startswith("node-")] == [host["name"] for host in native.request["hosts"]]


def test_enrollment_no_admissible_host_has_no_effect(native):
    recovery.manual_clock(native)
    inventory, events = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    native.execution_deadline = 119
    before = copy.deepcopy(native.markers.records)
    with pytest.raises(ceph_native.Refusal, match="^enrollment-budget$"):
        native.enroll()
    assert native.markers.read("bootstrap-enrollment.json") == staging.recovery_scope(native.request)
    assert {name: value for name, value in native.markers.records.items() if name != "bootstrap-enrollment.json"} == before
    assert not inventory
    assert not any(event.startswith("node-") for event in events)


@pytest.mark.parametrize("stop", ["applied", "last-completion", "license"])
def test_enrollment_interruption_continues_without_reapplying_hosts(native, stop):
    inventory, events = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    effect, write = native.tunnel_effect, native.markers.write
    interrupted = [False]

    def fail_once():
        if not interrupted[0]:
            interrupted[0] = True
            raise ceph_native.Refusal("native-timeout")

    def apply(*args, **kwargs):
        value = effect(*args, **kwargs)
        if stop == "applied":
            fail_once()
        return value

    def complete(name, value):
        write(name, value)
        if stop == "last-completion" and name == "enrollment-node-3-complete.json":
            fail_once()

    def license_accepted():
        if stop == "license":
            fail_once()
        return True

    native.tunnel_effect, native.markers.write, native.license_accepted = apply, complete, license_accepted
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        native.enroll()
        native.license()
    assert len(inventory) == 3
    assert native.enroll() == {"changed": False}
    assert native.license() == {"changed": False}
    assert len([event for event in events if event.startswith("node-")]) == 3


def test_osd_children_have_one_apply_between_two_inventories(native):
    native.verify_tunnel_configuration = lambda: None
    native.normalized_services = lambda groups: groups
    native.recovery_scope = lambda: {"fixture": True}
    inventory, calls = {}, []

    def specs():
        calls.append("read")
        return copy.deepcopy(inventory)

    def apply(*args, **kwargs):
        assert args == ("orch", "apply", "-i", "-")
        documents = list(yaml.safe_load_all(kwargs["data"]))
        assert len(documents) == 3
        for value in documents:
            key = "osd." + value["service_id"]
            assert native.markers.read("service-" + key + "-attempt.json") is not None
            inventory[key] = value
        calls.append("apply")

    native.ceph = lambda *args, **kwargs: {"fsid": native.request["fsid"]}
    native.service_specs, native.tunnel_effect = specs, apply
    assert native.services(tunnel.device_rows(native.request), service=2) == {"changed": True}
    assert calls == ["read", "apply", "read"]
    assert native.services(tunnel.device_rows(native.request), service=2) == {"changed": False}
    assert calls == ["read", "apply", "read", "read"]


def test_license_has_no_effect_without_every_host_completion(native):
    unused, events = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    native.license_accepted = lambda: pytest.fail("license read preceded host completion proofs")
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.license()
    assert not any(event.startswith("node-") for event in events)
    native.enroll()
    native.markers.records.pop("enrollment-node-3-complete.json")
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.license()


def test_license_transport_keeps_its_own_native_budget(core_request, monkeypatch):
    transport = ceph_core.Transport(core_request, {})
    received = []
    monkeypatch.setattr(transport, "_call", lambda host, source, payload, timeout, **kwargs: received.append((payload, timeout, kwargs)) or {})
    transport.call(core_request["hosts"][0], "license")
    payload, timeout, options = received[0]
    assert payload["payload"]["phase"] == "license" and timeout == 660
    assert payload["budget"] == 640 and options["worker_budget"] == 640


def test_batch_readback_does_not_retry_beyond_reserved_command(native):
    inventory, unused = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    original = native.ceph
    reads = []

    def ceph(*args, **kwargs):
        if args == ("orch", "host", "ls") and inventory:
            reads.append(args)
            raise ceph_native.Refusal("native-command")
        return original(*args, **kwargs)

    native.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        native.enroll()
    assert len(reads) == 1 and len(inventory) == 3
    native.ceph = original
    assert native.enroll() == {"changed": False}

"""Synthetic seed finalization preserves fences and proves native acceptance."""

from __future__ import annotations

import hashlib
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_bootstrap_recovery as recovery
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as fixtures

cluster_request = fixtures.cluster_request
native = fixtures.native
private = fixtures.private
core_request = core_fixtures.core_request


def test_failed_native_bootstrap_keeps_attempt_and_never_repeats(native, private):
    fixtures.prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.daemons = lambda: []
    failure = ceph_native.NativeFailure("native-command", "synthetic image", b"synthetic bootstrap refusal")
    calls = []

    def command(argv, **kwargs):
        del kwargs
        assert "bootstrap" in argv and native.markers.read("bootstrap-attempt.json") == staging.recovery_scope(native.request)
        calls.append(argv)
        raise failure

    native.command = command
    with pytest.raises(ceph_native.NativeFailure) as caught:
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    assert caught.value is failure
    assert native.markers.read("bootstrap-attempt.json") == staging.recovery_scope(native.request)
    with pytest.raises(ceph_native.Refusal, match="^partial-cluster$"):
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    assert len(calls) == 1


@pytest.fixture
def call_home(native):
    native.markers.records["transport-ready.json"] = {"custodyID": "synthetic-custody"}
    native.markers.records["bootstrap-scope.json"] = staging.recovery_scope(native.request)
    state = {"flag": None, "enabled": False, "lost": False, "publish": True}
    effects = []

    def ceph(*args, **kwargs):
        assert not kwargs
        if args == ("config", "dump"):
            return [] if state["flag"] is None else [{"name": "mgr/cephadm/call_home_needs_acceptance", "section": "mgr",
                                                     "mask": "", "value": str(state["flag"])}]
        assert args == ("mgr", "module", "ls")
        return {"enabled_modules": ["call_home_agent"] if state["enabled"] else [], "always_on_modules": ["cephadm"],
                "force_disabled_modules": []}

    def effect(*args, **kwargs):
        assert args == ("orch", "enable-call-home", "--raise-automation-warning") and kwargs == {"json_result": False}
        intent = dict(native.configuration_intent(), step="call_home_initialize")
        assert native.markers.read("tunnel-config-attempt-call_home_initialize.json") == intent
        assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is None
        effects.append(args)
        if state["publish"]:
            state.update(flag=True, enabled=True)
        if state["lost"]:
            raise ceph_native.NativeFailure("native-command", "synthetic image", b"synthetic lost reply")

    native.ceph, native.tunnel_effect = ceph, effect
    return state, effects


def test_missing_call_home_flag_is_initialized_after_its_scope_fence(native, call_home):
    state, effects = call_home
    assert native.initialize_call_home(True) == (True, True)
    assert state["flag"] is True and state["enabled"] and len(effects) == 1
    intent = dict(native.configuration_intent(), step="call_home_initialize")
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") == intent
    assert native.initialize_call_home(True) == (True, False) and len(effects) == 1


def test_missing_call_home_flag_observation_never_initializes(native, call_home):
    unused, effects = call_home
    assert native.initialize_call_home(False) == (False, False)
    assert effects == [] and native.markers.writes == []


def test_call_home_initialization_requires_enabled_intent(native, call_home):
    unused, effects = call_home
    native.request["callHome"] = "disabled"
    with pytest.raises(ceph_native.Refusal, match="^call-home-unqualified$"):
        native.initialize_call_home(True)
    assert effects == [] and native.markers.writes == []


@pytest.mark.parametrize("marker", [
    "tunnel-config-attempt-call_home.json", "tunnel-config-complete-call_home.json",
    "call-home-accepted.json", "tunnel-configured.json",
])
def test_lost_native_flag_after_acceptance_evidence_cannot_reinitialize(native, call_home, marker):
    unused, effects = call_home
    native.markers.records[marker] = {"synthetic": "existing acceptance evidence"}
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.initialize_call_home(True)
    assert effects == [] and native.markers.writes == []


@pytest.mark.parametrize("case", ["foreign-attempt", "foreign-complete", "complete-without-attempt"])
def test_call_home_initialization_refuses_mismatched_receipts(native, call_home, case):
    unused, effects = call_home
    intent = dict(native.configuration_intent(), step="call_home_initialize")
    native.markers.records["tunnel-config-attempt-call_home_initialize.json"] = intent
    if case == "foreign-attempt":
        native.markers.records["tunnel-config-attempt-call_home_initialize.json"] = dict(intent, custodyID="foreign")
    else:
        native.markers.records["tunnel-config-complete-call_home_initialize.json"] = dict(intent, custodyID="foreign") if case == "foreign-complete" else intent
        if case == "complete-without-attempt":
            native.markers.records.pop("tunnel-config-attempt-call_home_initialize.json")
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.initialize_call_home(True)
    assert effects == [] and native.markers.writes == []


def test_lost_enable_reply_completes_from_native_module_and_flag_without_reissue(native, call_home):
    state, effects = call_home
    state["lost"] = True
    native.horizon = native.clock()
    with pytest.raises(ceph_native.NativeFailure):
        native.initialize_call_home(True)
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is None
    native.horizon = None
    assert native.initialize_call_home(True) == (True, False)
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is not None and len(effects) == 1


@pytest.mark.parametrize("flag,enabled", [(None, False), (True, False), (False, True)])
def test_attempted_initialization_without_exact_native_result_refuses(native, call_home, flag, enabled):
    state, effects = call_home
    state.update(flag=flag, enabled=enabled)
    native.markers.records["tunnel-config-attempt-call_home_initialize.json"] = dict(native.configuration_intent(), step="call_home_initialize")
    with pytest.raises(ceph_native.Refusal):
        native.initialize_call_home(True)
    assert effects == [] and native.markers.writes == []


def test_enable_success_without_observed_flag_cannot_publish_completion(native, call_home):
    state, effects = call_home
    state["publish"] = False
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.initialize_call_home(True)
    assert len(effects) == 1 and native.markers.read("tunnel-config-complete-call_home_initialize.json") is None


def provider_license(**changes):
    value = {"ceph_version": "ceph version 20.2.2 (synthetic) tentacle", "image_id": "c" * 64,
             "accept_time": "2026-10-18T12:00:00+00:00", "license": "Synthetic license text\n"}
    value.update(changes)
    digest = hashlib.md5(value["license"].encode(), usedforsecurity=False).hexdigest()
    key = "mgr/cephadm/" + value["ceph_version"].replace(" ", "_") + "_license_" + digest
    return key, json.dumps(value).encode()


@pytest.fixture
def license_records(native):
    # This call already holds the host preparation profile; profile authentication
    # and read-only Podman inspection are exercised by the runtime tests.
    native._profile = {"imageID": "c" * 64}
    records, calls = {}, []

    def ceph(*args, **kwargs):
        assert args == ("config-key", "ls") and not kwargs
        calls.append(args)
        return list(records)

    def ceph_text(*args):
        assert args[:2] == ("config-key", "get") and len(args) == 3
        calls.append(args)
        return records[args[2]]

    native.ceph, native.ceph_text = ceph, ceph_text
    return records, calls


@pytest.mark.parametrize("timestamp", ["2026-10-18T12:00:00+00:00", "2026-10-18T09:00:00-03:00", "2026-10-18 12:00:00+00:00"])
def test_license_uses_provider_md5_key_and_frozen_image_id_with_mon_reads(native, license_records, timestamp):
    records, calls = license_records
    key, raw = provider_license(accept_time=timestamp)
    records[key] = raw
    assert native.request["image"].endswith("a" * 64) and native.license_accepted() is True
    assert calls == [("config-key", "ls"), ("config-key", "get", key)] and native.markers.writes == []


@pytest.mark.parametrize("image_id", ["a" * 64, "d" * 64])
def test_another_image_or_registry_manifest_digest_is_not_license_acceptance(native, license_records, image_id):
    records, unused = license_records
    key, raw = provider_license(image_id=image_id)
    records[key] = raw
    assert native.license_accepted() is False


def test_absent_license_is_false_without_loading_an_image_profile(native, license_records):
    unused, calls = license_records
    native.runtime_profile = lambda: pytest.fail("absent acceptance must not inspect an image")
    assert native.license_accepted() is False and calls == [("config-key", "ls")]


@pytest.mark.parametrize("change", [
    "wrong-key", "missing-field", "extra-field", "duplicate-field", "not-json", "not-object",
    "wrong-id-type", "prefixed-id", "uppercase-id", "naive-time", "invalid-time", "empty-license",
    "long-version", "long-time", "oversize",
])
def test_malformed_provider_acceptance_never_proves_readiness(native, license_records, change):
    records, unused = license_records
    key, raw = provider_license()
    value = json.loads(raw)
    if change == "wrong-key":
        key = key[:-32] + "0" * 32
    elif change == "missing-field":
        value.pop("license")
    elif change == "extra-field":
        value["foreign"] = "value"
    elif change == "duplicate-field":
        raw = raw[:-1] + b', "image_id": "' + b"c" * 64 + b'"}'
    elif change == "not-json":
        raw = b"not-json"
    elif change == "not-object":
        value = []
    elif change == "wrong-id-type":
        value["image_id"] = 123
    elif change in ("prefixed-id", "uppercase-id"):
        value["image_id"] = "sha256:" + "c" * 64 if change == "prefixed-id" else "C" * 64
    elif change in ("naive-time", "invalid-time", "long-time"):
        value["accept_time"] = {"naive-time": "2026-10-18T12:00:00", "invalid-time": "never", "long-time": "x" * 129}[change]
    elif change == "empty-license":
        value["license"] = ""
    elif change == "long-version":
        value["ceph_version"] = "v" * 1025
    else:
        raw = b"x" * (ceph_native.MAX_OUTPUT + 1)
    if change not in ("duplicate-field", "not-json", "oversize"):
        raw = json.dumps(value).encode()
    records[key] = raw
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.license_accepted()
    assert native.markers.writes == []


def test_license_record_count_and_total_read_budget_are_bounded(native, license_records):
    records, calls = license_records
    for index in range(129):
        records["mgr/cephadm/synthetic_license_" + format(index, "032x")] = b"{}"
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.license_accepted()
    assert calls == [("config-key", "ls")]
    records.clear()
    for number in range(2):
        key, raw = provider_license(license=str(number) + "x" * (ceph_native.MAX_OUTPUT // 2))
        records[key] = raw
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.license_accepted()


def enrollment(native):
    inventory, events = recovery.enrollment_fixture(native, {"config": 0, "inventory": 0})
    ceph = native.ceph
    state = {"accepted": False, "publish": True, "fail": False,
             "failure": ceph_native.NativeFailure("native-command", "synthetic image", b"synthetic license refusal")}
    native.license_accepted = lambda: state["accepted"]
    native.verify_transport_bundle = lambda: events.append("transport-proof")

    def effect(*args, **kwargs):
        if args[:2] != ("orch", "accept-license"):
            return ceph(*args, **kwargs)
        assert args == ("orch", "accept-license", "--image", native.request["image"]) and kwargs == {"json_result": False}
        assert "transport-proof" in events and "config" in events
        assert native.markers.read("bootstrap-enrollment.json") == staging.recovery_scope(native.request)
        assert len(inventory) == len(native.request["hosts"])
        for host in native.request["hosts"]:
            attempt = native.markers.read("enrollment-" + host["name"] + "-attempt.json")
            assert attempt is not None and native.markers.read("enrollment-" + host["name"] + "-complete.json") == attempt
        events.append("accept-license")
        if state["fail"]:
            raise state["failure"]
        state["accepted"] = state["publish"]
        return b""

    native.tunnel_effect = effect
    return state, inventory, events


def test_enrollment_accepts_license_only_after_all_owned_hosts_and_replay_reads(native):
    state, inventory, events = enrollment(native)
    assert native.enroll() == {"changed": True} and state["accepted"]
    assert len(inventory) == 3 and events[-1] == "accept-license"
    assert native.enroll() == {"changed": False} and events.count("accept-license") == 1


@pytest.mark.parametrize("proof", ["transport", "configuration", "mounts"])
def test_enrollment_never_accepts_license_when_tunnel_proof_refuses(native, proof):
    unused_1, unused_2, events = enrollment(native)

    def refuse(*args, **kwargs):
        del args, kwargs
        raise ceph_native.Refusal("native-tunnel-unproved")

    setattr(native, {"transport": "verify_transport_bundle", "configuration": "verify_tunnel_configuration", "mounts": "verify_manager_mounts"}[proof], refuse)
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.enroll()
    assert "accept-license" not in events and native.markers.read("bootstrap-enrollment.json") is None


def test_late_license_native_failure_propagates_and_readback_is_required(native):
    state, unused, events = enrollment(native)
    failure = state["failure"]
    state["fail"] = True
    native.horizon = native.clock()
    with pytest.raises(ceph_native.NativeFailure) as caught:
        native.enroll()
    assert caught.value is failure and events.count("accept-license") == 1
    state.update(fail=False, publish=False)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.enroll()
    assert not state["accepted"] and events.count("accept-license") == 2


def test_health_ok_without_license_is_not_ready_and_observation_never_accepts(core_request):
    core_request["config"] = core_fixtures.memory_config()
    answers = core_fixtures.memory_observation_answers(core_request)
    worker, calls = core_fixtures.worker(core_request, answers)
    worker.license_accepted = ceph_native.Worker.license_accepted.__get__(worker)
    answers[("config-key", "ls")] = []
    result = worker.observe()
    assert result["health"] == "HEALTH_OK" and not result["ready"] and result["partial"]
    assert "IBM_LICENSE_NOT_ACCEPTED" in worker.blocking and worker.markers.writes == []
    assert all(argv[0] == "/usr/bin/ceph" for argv in calls)
    assert all("accept-license" not in argv and "inspect-image" not in argv and "get-license" not in argv for argv in calls)

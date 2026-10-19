"""Synthetic seed finalization preserves fences and proves native acceptance."""

from __future__ import annotations

import ast
import copy
import hashlib
import json
import os
import pathlib

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_bootstrap_recovery as recovery
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_staging as staging_cases

cluster_request = fixtures.cluster_request
native = fixtures.native
private = fixtures.private
core_request = core_fixtures.core_request
fact_set = fixtures.fact_set


@pytest.mark.usefixtures("fact_set")
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
    state = {"flag": None, "enabled": False, "lost": 0, "publish": True, "refuse": 0}
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
        if state["refuse"]:
            state["refuse"] -= 1
            raise ceph_native.Refusal("native-tunnel-unproved")
        effects.append(args)
        if state["publish"]:
            state.update(flag=True, enabled=True)
        if state["lost"]:
            state["lost"] -= 1
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


def untouched(*args, **kwargs):
    pytest.fail("an opt-in Call Home left disabled read or wrote a marker: " + repr((args, kwargs)))


def test_opt_in_call_home_left_disabled_has_no_step_marker_or_effect(native, call_home):
    state, effects = call_home
    native.request["callHome"] = "disabled"
    native.selection = fixtures.built(fixtures.OLD)
    native.markers.read = native.markers.write = untouched
    assert native.initialize_call_home(True) == (True, False) and native.initialize_call_home(False) == (True, False)
    assert native.configure_call_home(True) == (True, False) and native.configure_call_home(False) == (True, False)
    assert native.accept_call_home() is False
    assert effects == [] and native.selection.applied == {"ceph-call-home-opt-in"}
    state["enabled"] = True
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.initialize_call_home(False)


@pytest.mark.parametrize("host,image", [(fixtures.CURRENT, fixtures.CURRENT), (fixtures.OLD, fixtures.CURRENT), (fixtures.CURRENT, fixtures.OLD),
                                        (fixtures.OLD, "unanchored")])
@pytest.mark.parametrize("step", ["initialize", "configure", "accept"])
def test_every_other_disabled_call_home_path_keeps_the_enabled_only_guard(native, call_home, host, image, step):
    unused, effects = call_home
    native.request["callHome"] = "disabled"
    native.selection = fixtures.built(host, fixtures.specs.zipapp(fixtures.specs.ANCHOR[:-1]) if image == "unanchored" else image)
    native.markers.read = native.markers.write = untouched
    with pytest.raises(ceph_native.Refusal, match="^call-home-unqualified$"):
        {"initialize": lambda: native.initialize_call_home(True), "configure": lambda: native.configure_call_home(True),
         "accept": native.accept_call_home}[step]()
    assert effects == [] and native.selection.applied == set()


def test_enabled_intent_on_an_opt_in_manager_keeps_the_general_call_home_step(native, call_home):
    del call_home
    native.selection = fixtures.built(fixtures.OLD)
    calls = []
    native.ceph = lambda *args, **kwargs: calls.append(args) or b""
    assert native.accept_call_home() is True
    assert calls == [("orch", "accept", "call-home-enabled")] and native.markers.read("call-home-accepted.json") == {"callHome": "enabled"}
    assert native.selection.applied == set()


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


def manager_clock(native):
    now = [0.0]
    native.clock = lambda: now[0]
    native.pause = lambda seconds: now.__setitem__(0, now[0] + seconds)
    native.horizon = None
    return now


def test_lost_enable_reply_repeats_the_idempotent_enable_while_the_manager_restarts(native, call_home):
    state, effects = call_home
    manager_clock(native)
    state["lost"] = 1
    assert native.initialize_call_home(True) == (True, True)
    intent = dict(native.configuration_intent(), step="call_home_initialize")
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") == intent and len(effects) == 2
    assert native.markers.writes.count("tunnel-config-complete-call_home_initialize.json") == 1
    assert native.markers.writes.count("tunnel-config-attempt-call_home_initialize.json") == 1


def test_lost_enable_reply_after_the_manager_wait_completes_from_native_module_and_flag(native, call_home):
    state, effects = call_home
    state["lost"] = 1
    native.horizon = native.clock()
    with pytest.raises(ceph_native.NativeFailure):
        native.initialize_call_home(True)
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is None
    native.horizon = None
    assert native.initialize_call_home(True) == (True, False)
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is not None and len(effects) == 1


def attempted_initialization(native):
    intent = dict(native.configuration_intent(), step="call_home_initialize")
    native.markers.records["tunnel-config-attempt-call_home_initialize.json"] = intent
    return intent


@pytest.mark.parametrize("flag,enabled", [(None, False), (True, False)])
def test_attempted_initialization_without_its_native_result_reissues_the_idempotent_enable(native, call_home, flag, enabled):
    state, effects = call_home
    state.update(flag=flag, enabled=enabled)
    intent = attempted_initialization(native)
    assert native.initialize_call_home(True) == (True, True)
    assert len(effects) == 1 and state["flag"] is True and state["enabled"]
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") == intent
    assert native.markers.writes == ["tunnel-config-complete-call_home_initialize.json"]
    assert native.initialize_call_home(True) == (True, False) and len(effects) == 1


@pytest.mark.parametrize("flag,enabled", [(None, False), (True, False)])
def test_attempted_initialization_observation_never_reissues(native, call_home, flag, enabled):
    state, effects = call_home
    state.update(flag=flag, enabled=enabled)
    attempted_initialization(native)
    with pytest.raises(ceph_native.Refusal):
        native.initialize_call_home(False)
    assert effects == [] and native.markers.writes == []


@pytest.mark.parametrize("flag,enabled,marker", [
    (False, True, None), (False, False, None),
    (None, False, "tunnel-config-attempt-call_home.json"), (None, False, "tunnel-config-complete-call_home.json"),
    (None, False, "call-home-accepted.json"), (None, False, "tunnel-configured.json"),
    (True, False, "tunnel-config-attempt-call_home.json"), (True, False, "call-home-accepted.json"),
])
def test_attempted_initialization_after_acceptance_never_reissues(native, call_home, flag, enabled, marker):
    state, effects = call_home
    state.update(flag=flag, enabled=enabled)
    attempted_initialization(native)
    if marker is not None:
        native.markers.records[marker] = {"synthetic": "existing acceptance evidence"}
    with pytest.raises(ceph_native.Refusal):
        native.initialize_call_home(True)
    assert effects == [] and native.markers.writes == []


def test_enable_refused_before_the_native_command_is_reissued_by_the_next_apply(native, call_home):
    state, effects = call_home
    state["refuse"] = 1
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.initialize_call_home(True)
    intent = dict(native.configuration_intent(), step="call_home_initialize")
    assert effects == [] and native.markers.read("tunnel-config-attempt-call_home_initialize.json") == intent
    assert native.markers.read("tunnel-config-complete-call_home_initialize.json") is None
    assert native.initialize_call_home(True) == (True, True)
    assert len(effects) == 1 and native.markers.read("tunnel-config-complete-call_home_initialize.json") == intent


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


def test_transient_license_failure_repeats_until_the_license_record_reads_back(native):
    state, unused, events = enrollment(native)
    manager_clock(native)

    def recover(*args, **kwargs):
        del args, kwargs
        state["fail"] = False

    native.pause = recover
    state["fail"] = True
    assert native.enroll() == {"changed": True} and state["accepted"] and events.count("accept-license") == 2
    assert native.enroll() == {"changed": False} and events.count("accept-license") == 2


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


def test_a_manager_without_the_license_contract_enrolls_without_accepting_a_license(native):
    state, inventory, events = enrollment(native)
    native.selection = fixtures.built(fixtures.OLD)
    assert native.enroll() == {"changed": True} and not state["accepted"]
    assert len(inventory) == 3 and "accept-license" not in events
    assert native.enroll() == {"changed": False} and "accept-license" not in events
    assert native.selection.applied == {"ceph-mgr-without-license-contract"}


@pytest.mark.parametrize("image", [fixtures.CURRENT, "unanchored"])
def test_an_ibm_or_unknown_manager_still_accepts_its_license_at_enrollment(native, image):
    state, unused, events = enrollment(native)
    native.selection = fixtures.built(fixtures.OLD, fixtures.specs.zipapp(fixtures.specs.ANCHOR[:-1]) if image == "unanchored" else image)
    assert native.enroll() == {"changed": True} and state["accepted"] and events.count("accept-license") == 1
    assert native.selection.applied == set()


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


LICENSE_ID, TWENTY_TWO, TWENTY_ONE = fixtures.specs.LICENSE_CALL_ID, "20.2.2-349.el9cp", "20.1.0-221.el9cp"
METADATA_RECORDS = ("bootstrap-output-source.json", "bootstrap-original-reserved.json", "bootstrap-original-ready.json")


def license_world(native, private, host, result, seed=True, daemons=None, after=None):
    """A real Worker.command and capture over one bootstrap run whose host and image cephadm are 20.2.x-shaped unless host says otherwise."""
    fixtures.prepared_bootstrap(native, private)
    del native.capture_bootstrap_outputs
    native.__dict__.pop("command", None)
    native.__dict__.pop("prove_seed", None)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    recovery.private_sources(native)
    native.selection = fixtures.built(host, fixtures.CURRENT)
    world = {"seeded": [], "runs": [], "proofs": 0}
    native.daemons = lambda: (daemons or fixtures.initial_daemons)(native) if world["seeded"] else []

    def run(argv, data=b"", timeout=60):
        if len(argv) < 4 or argv[3] != "bootstrap":
            pytest.fail("the recovery ran another native command: " + repr(argv))
        assert timeout == ceph_native.BOOTSTRAP_TIMEOUT and data == b""
        world["runs"].append(argv)
        if seed:
            world["seeded"].append(recovery.cephadm_outputs(native))
            if after:
                after(native)
        return result(native)

    proved = native.prove_seed

    def prove_seed(*args, **kwargs):
        world["proofs"] += 1
        return proved(*args, **kwargs)

    native.run, native.prove_seed = run, prove_seed
    return world


def refused(native):
    return staging_cases.license_call_output(native.request)


def succeeded(native):
    del native
    return 0, ceph_native.NativeOutput(b"", b"")


def general_failure(native, output, coordinate=None):
    coordinate = coordinate or native.request["image"] + "; host package " + native.request["cephadm"]["packageVersion"]
    return ceph_native.NativeFailure("native-command", coordinate, output.stderr, secrets=(b"",)).native_error


def reset(native):
    for name in os.listdir(native.markers.path):
        os.unlink(os.path.join(native.markers.path, name))
    native.markers.records, native.markers.writes = {}, []


def test_ibm_license_call_refusal_continues_the_owned_seed_in_the_same_apply(native, private):
    native.request["cephadm"]["packageVersion"] = TWENTY_TWO
    worlds = []
    for result in (refused, succeeded):
        reset(native)
        world = license_world(native, private, TWENTY_TWO, result)
        assert native.tunnel_bootstrap(fixtures.ssh_material(private)) == {"changed": True}
        assert len(world["runs"]) == 1
        assert native.markers.read("bootstrap-attempt.json") == staging.recovery_scope(native.request)
        assert native.markers.read("bootstrap-output-source.json") is not None
        worlds.append((world, copy.deepcopy(native.markers.records), list(native.markers.writes), set(native.selection.applied)))
    (recovered, records, writes, applied), (twin, twin_records, twin_writes, twin_applied) = worlds
    assert writes == twin_writes and set(records) == set(twin_records)
    for name in records:
        if name in METADATA_RECORDS:
            assert set(records[name]) == set(twin_records[name]), name
        else:
            assert records[name] == twin_records[name], name
    assert recovered["proofs"] == 1 and twin["proofs"] == 0
    assert applied == {LICENSE_ID} and twin_applied == set()


def assert_kept_failure(native, private, world, caught, coordinate=None):
    output = world["result"]
    assert str(caught.value) == "native-command"
    assert caught.value.native_error == general_failure(native, output, coordinate)
    assert native.markers.read("bootstrap-output-source.json") is None
    assert native.markers.read("bootstrap-attempt.json") == staging.recovery_scope(native.request)
    with pytest.raises(ceph_native.Refusal, match="^partial-cluster$"):
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    assert len(world["runs"]) == 1


def failing_world(native, private, host, result, **kwargs):
    outputs = []

    def recorded(worker):
        outputs.append(result(worker))
        return outputs[-1]

    world = license_world(native, private, host, recorded, **kwargs)
    with pytest.raises(ceph_native.Refusal) as caught:
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    assert isinstance(caught.value, ceph_native.NativeFailure), repr(caught.value)
    world["result"] = outputs[0][1]
    return world, caught


def test_another_native_bootstrap_failure_keeps_the_attempt_and_the_failure(native, private):
    native.request["cephadm"]["packageVersion"] = TWENTY_TWO
    world, caught = failing_world(native, private, TWENTY_TWO, lambda worker: staging_cases.license_call_output(worker.request, code=2))
    assert_kept_failure(native, private, world, caught)
    assert world["proofs"] == 0 and LICENSE_ID not in native.selection.applied


DIFFERENT = {
    "another-errno": {"refusal": "Error ENOENT: no hosts defined"},
    "another-image": {"image": "registry.example.test/ceph@sha256:" + "b" * 64},
    "later-error": {"stderr_tail": ("ERROR: later failure",)},
}


@pytest.mark.parametrize("case", sorted(DIFFERENT))
def test_a_different_bootstrap_refusal_keeps_the_failure(native, private, case):
    native.request["cephadm"]["packageVersion"] = TWENTY_TWO
    changes = dict(DIFFERENT[case])
    if "image" in changes:
        changes["call"] = staging_cases.license_call(native.request, changes.pop("image"))
    world, caught = failing_world(native, private, TWENTY_TWO, lambda worker: staging_cases.license_call_output(worker.request, **changes))
    assert_kept_failure(native, private, world, caught)
    assert world["proofs"] == 0 and LICENSE_ID not in native.selection.applied


def stopped_manager(native):
    daemons = fixtures.initial_daemons(native)
    daemons[1]["state"] = "stopped"
    return daemons


def foreign_manager(native):
    daemons = fixtures.initial_daemons(native)
    daemons[1]["name"] = "mgr.foreign"
    return daemons


UNPROVED = {
    "no-daemons": {"seed": False},
    "stopped-manager": {"daemons": stopped_manager},
    "foreign-manager": {"daemons": foreign_manager},
    "missing-keyring": {"after": lambda worker: os.unlink(worker.bootstrap_sources()["keyring"])},
}


@pytest.mark.parametrize("case", sorted(UNPROVED))
def test_an_unproved_seed_keeps_the_native_failure_not_the_proof_refusal(native, private, case):
    native.request["cephadm"]["packageVersion"] = TWENTY_TWO
    world, caught = failing_world(native, private, TWENTY_TWO, refused, **UNPROVED[case])
    suffixed = native.request["image"] + "; host package " + TWENTY_TWO + "; specializations " + LICENSE_ID
    assert_kept_failure(native, private, world, caught, suffixed)
    assert world["proofs"] == 1 and native.selection.applied == {LICENSE_ID}


def test_a_build_without_the_license_call_keeps_the_failure(native, private):
    native.request["cephadm"]["packageVersion"] = TWENTY_ONE
    world, caught = failing_world(native, private, TWENTY_ONE, refused)
    coordinate = native.request["image"] + "; host package " + TWENTY_ONE + "; specializations ceph-bootstrap-without-license-flag"
    assert_kept_failure(native, private, world, caught, coordinate)
    assert world["proofs"] == 0 and LICENSE_ID not in native.selection.applied
    assert "--automatically-accept-license" not in world["runs"][0]


def unknown_host(case):
    declared = fixtures.specs.committed(TWENTY_TWO)["declared"]
    if case == "unanchored":
        names = fixtures.specs.ANCHOR[:-1]
        return fixtures.specs.zipapp(names, fixtures.specs.IBM_OVERRIDE, main=fixtures.specs.licensed(names))
    return fixtures.specs.zipapp(declared, "IBM_BUILD = False\n", main=fixtures.specs.licensed(declared))


@pytest.mark.parametrize("case", ["unanchored", "unrecognized-override"])
def test_unknown_license_call_facts_keep_the_failure(native, private, case):
    native.request["cephadm"]["packageVersion"] = TWENTY_TWO
    host = unknown_host(case)
    assert fixtures.built(host).fact("host-cephadm.bootstrap-license-call") is None
    world, caught = failing_world(native, private, host, refused)
    assert_kept_failure(native, private, world, caught)
    assert world["proofs"] == 0 and LICENSE_ID not in native.selection.applied


def test_only_the_staged_bootstrap_may_recover_a_native_failure():
    tree = ast.parse(pathlib.Path(ceph_native.__file__).read_text(encoding="utf-8"))
    recovering, referenced = [], []

    def walk(node, function):
        for child in ast.iter_child_nodes(node):
            inner = child.name if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)) else function
            if isinstance(child, ast.keyword) and child.arg == "recover":
                recovering.append((function, ast.dump(child.value)))
            if isinstance(child, ast.Attribute) and child.attr == "license_call_continues":
                referenced.append(function)
            walk(child, inner)

    walk(tree, None)
    assert recovering == [("tunnel_bootstrap", ast.dump(ast.parse("self.license_call_continues", mode="eval").body))]
    assert referenced == ["tunnel_bootstrap"]


def test_command_without_recovery_is_unchanged(native):
    del native.command
    code, output = staging_cases.license_call_output(native.request)
    native.run = lambda argv, data=b"", timeout=60: (code, output)
    argv = [native.tool, "bootstrap"]
    failures = []
    for recover in ({}, {"recover": lambda value, result: False}):
        with pytest.raises(ceph_native.NativeFailure) as caught:
            native.command(argv, json_result=False, **recover)
        failures.append(caught.value.native_error)
    assert failures[0] == failures[1] == general_failure(native, output)
    assert native.command(argv, json_result=False, recover=lambda value, result: (value, result) == (code, output)) is output

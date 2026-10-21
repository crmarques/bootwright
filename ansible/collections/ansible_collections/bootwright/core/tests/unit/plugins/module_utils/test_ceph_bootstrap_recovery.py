"""Candidate bootstrap recovery exercises synthetic custody and lost responses."""

from __future__ import annotations

import contextlib
import copy
import hashlib
import itertools
import json
import os

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_tunnel_custody as custody, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as fixtures, test_ceph_core as core_fixtures

cluster_request = fixtures.cluster_request
native = fixtures.native
private = fixtures.private
core_request = core_fixtures.core_request


def protocol(native):
    scope = staging.recovery_scope(native.request)
    for name in ("bootstrap-scope.json", "bootstrap-protocol.json", "bootstrap-attempt.json", "bootstrap-prepared.json"):
        native.markers.records[name] = copy.deepcopy(scope)
    fixtures.staged(native)
    native.prove_preparation = lambda: None
    return scope


def sources(native):
    for name, path in native.bootstrap_sources().items():
        ceph_native.private_write(path, ("private synthetic " + name).encode())

    @contextlib.contextmanager
    def source(path, **kwargs):
        assert path in native.bootstrap_sources().values() and kwargs == {"root_only": True}
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb") as handle:
            yield handle

    native.native_source = source


@pytest.mark.parametrize("field", ["services", "publicNetworks", "clusterNetworks", "monIP", "clusterKeyRef", "registryRef", "config"])
def test_complete_nonsecret_bootstrap_scope_rejects_configuration_drift(cluster_request, field):
    original = staging.recovery_scope(cluster_request)
    mutated = copy.deepcopy(cluster_request)
    if field == "services":
        mutated[field][1]["placement"]["count"] = 1
    elif field == "monIP":
        mutated["hosts"][0]["address"] = mutated[field] = "192.0.2.20"
    elif field.endswith("Networks"):
        mutated[field] = ["192.0.3.0/24"]
    elif field == "config":
        mutated[field] = {"osd": {"osd_memory_target": "2147483648"}}
    else:
        mutated[field] = "other-ref"
    assert staging.recovery_scope(mutated) != original


def test_scope_is_shared_with_pool_and_client_without_private_package_inputs(cluster_request):
    wanted = staging.recovery_scope(cluster_request)
    cluster_request.update(action="client", client={"name": "consumer"}, packages={"fixture": "public package source"})
    assert staging.recovery_scope(cluster_request) == wanted
    policy = {key: value for key, value in cluster_request.items() if key not in ("client", "packages")}
    policy.update(action="policy", policy={"deviceClass": "ssd", "failureDomain": "host", "name": "fast", "root": "default", "rule": "fast", "size": 2})
    assert staging.recovery_scope(policy) == wanted
    rbd = {key: value for key, value in cluster_request.items() if key not in ("client", "packages")}
    rbd.update(action="rbd", rbd={"dataPool": "", "features": ["layering"], "name": "disk", "pool": "data", "sizeBytes": 1073741824})
    assert staging.recovery_scope(rbd) == wanted
    filesystem = {key: value for key, value in cluster_request.items() if key not in ("client", "packages")}
    group = {"gid": 0, "mode": "755", "name": "csi", "pool": "cephfs-data", "sizeBytes": 0, "uid": 0}
    frozen = {"activeCount": 1, "dataPools": ["cephfs-data"], "defaultDataPool": "cephfs-data", "groups": [group], "mdsService": "mds.files",
              "metadataPool": "cephfs-metadata", "name": "files", "standbyCountWanted": 0, "standbyReplay": False}
    filesystem.update(action="filesystem", filesystem=frozen)
    assert staging.recovery_scope(filesystem) == wanted
    subvolume = {key: value for key, value in cluster_request.items() if key not in ("client", "packages")}
    subvolume.update(action="subvolume", subvolume={"filesystem": "files", "gid": 0, "group": "csi", "mode": "755", "name": "docs", "pool": "cephfs-data",
                                                    "sizeBytes": 1073741824, "uid": 0})
    assert staging.recovery_scope(subvolume) == wanted


@pytest.mark.parametrize("attempted", [False, True])
def test_empty_seed_never_restarts_after_attempt(native, private, attempted):
    fixtures.prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.daemons = lambda: []
    native.prove_bootstrap_absence = lambda: None
    scope = staging.recovery_scope(native.request)
    native.markers.records["bootstrap-protocol.json"] = scope
    fixtures.staged(native)
    if attempted:
        native.markers.records["bootstrap-attempt.json"] = scope
    effects = []
    native.command = lambda *args, **kwargs: effects.append(args)
    if attempted:
        with pytest.raises(ceph_native.Refusal, match="partial-cluster"):
            native.tunnel_bootstrap(fixtures.ssh_material(private))
        assert effects == []
    else:
        native.tunnel_bootstrap(fixtures.ssh_material(private))
        assert len(effects) == 1 and native.markers.read("bootstrap-attempt.json") == scope


@pytest.mark.parametrize("name", ["bootstrap-protocol.json", "tunnel-stage.json", "bootstrap-attempt.json"])
def test_each_attempt_fence_precedes_effect_and_publication_failure_stops_it(native, private, name):
    fixtures.prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.daemons = lambda: []
    native.prove_bootstrap_absence = lambda: None
    original_write = native.markers.write

    def write(marker, value):
        ceph_native.require(marker != name, "ownership")
        original_write(marker, value)

    native.markers.write = write
    native.command = lambda *args, **kwargs: pytest.fail("native effect preceded its fence")
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    assert not any(item.startswith(".bootstrap-") for item in os.listdir(native.markers.path))


@pytest.mark.parametrize("refused", ["native-option-undeclared", "call-home-unqualified"])
def test_a_refusal_before_the_attempt_record_continues_under_a_corrected_selection(native, private, refused):
    calls, captured = fixtures.bootstrap_world(native, private)
    if refused == "native-option-undeclared":
        names = [name for name in fixtures.specs.committed(fixtures.CURRENT)["declared"] if name != "--allow-fqdn-hostname"]
        native.selection = fixtures.built(fixtures.specs.zipapp(names, fixtures.specs.IBM_OVERRIDE))
    else:
        native.selection = fixtures.built(fixtures.OLD)
    with pytest.raises(ceph_native.Refusal, match="^" + refused + "$"):
        native.tunnel_bootstrap(fixtures.ssh_material(private))
    scope = staging.recovery_scope(native.request)
    assert native.markers.read("bootstrap-attempt.json") is None and native.markers.read("bootstrap-protocol.json") == scope and calls == []
    native.selection = fixtures.built(fixtures.CURRENT)
    assert native.tunnel_bootstrap(fixtures.ssh_material(private)) == {"changed": True}
    assert len(calls) == 1 and captured == [scope] and native.markers.read("bootstrap-attempt.json") == scope


def test_the_cluster_block_names_an_undeclared_option_with_its_native_cause():
    error = ceph_native.NativeFailure("native-option-undeclared", "synthetic image; host package synthetic", "--allow-fqdn-hostname")
    reason, detail = ceph_core.refusal(error, ceph_core.CLUSTER_REASONS)
    assert reason == "native-option-undeclared" and detail.startswith("native-option-undeclared")
    assert error.native_error["cause"] == "--allow-fqdn-hostname"


def test_complete_seed_can_be_observed_before_original_capture_without_reading_or_writing(native):
    protocol(native)
    sources(native)
    native.daemons = lambda: fixtures.initial_daemons(native)
    original = copy.deepcopy(native.markers.records)
    assert native.bootstrap_state()["stage"] == "seed"
    value = native.observe()
    assert value["owned"] and value["exists"] and value["partial"] and not value["ready"]
    assert native.markers.records == original and native.markers.writes == []
    assert native.host_state() == {"name": native.host["name"], "owned": True, "exists": True}


@pytest.mark.parametrize("change", ["mon-only", "extra", "wrong-image", "wrong-id", "no-attempt"])
def test_unknown_or_foreign_seed_is_not_adopted(native, change):
    protocol(native)
    sources(native)
    daemons = fixtures.initial_daemons(native)
    if change == "mon-only":
        daemons.pop()
    elif change == "extra":
        daemons.append(copy.deepcopy(daemons[0]))
    elif change == "wrong-image":
        daemons[0]["container_image_name"] = "foreign"
    elif change == "wrong-id":
        daemons[1]["name"] = "mgr.foreign.aaaaaa"
    else:
        native.markers.records.pop("bootstrap-attempt.json")
    native.daemons = lambda: daemons
    with pytest.raises((ceph_native.Refusal, staging.StageRefusal)):
        native.bootstrap_state()
    assert native.markers.writes == []


@pytest.mark.parametrize("container", ["", "b" * 64])
def test_seed_manager_between_containers_is_observed_not_ready_while_an_action_refuses(native, container):
    protocol(native)
    sources(native)
    daemons = fixtures.initial_daemons(native)
    daemons[1].update(state="stopped" if not container else "unknown", container_id=container,
                      container_image_name=native.request["image"] if container else "")
    native.daemons = lambda: daemons
    original = copy.deepcopy(native.markers.records)
    assert native.bootstrap_state()["stage"] == "seed"
    value = native.observe()
    assert value["owned"] and value["exists"] and value["partial"] and not value["ready"]
    assert native.markers.records == original and native.markers.writes == []
    with pytest.raises(staging.StageRefusal):
        native.prove_seed(daemons)


def test_mounted_seed_manager_between_containers_is_named_pending(native):
    protocol(native)
    sources(native)
    daemons = fixtures.initial_daemons(native)
    daemons[1].update(state="stopped", container_id="", container_image_name="")
    native.daemons = lambda: daemons
    native.markers.records["tunnel-configured.json"] = {"fixture": "complete"}
    native.markers.records["tunnel-redeploy.json"] = {"fixture": "attempted"}
    native.configuration_complete = lambda: True
    seen = []
    native.prove_mounted_seed = lambda observed, observing=False: seen.append(observing) or ([daemons[1]["name"]] if observing else [])
    assert native.bootstrap_state()["stage"] == "configured" and native.blocking == [daemons[1]["name"]] and seen == [True]


def test_bootstrap_original_is_retained_before_consumption_without_secret_derived_evidence(native):
    protocol(native)
    sources(native)
    native.daemons = lambda: fixtures.initial_daemons(native)
    source = native.native_source

    @contextlib.contextmanager
    def guarded(path, **kwargs):
        with source(path, **kwargs) as handle:
            read = handle.read

            def private_read(*args):
                assert native.markers.read("bootstrap-output-source.json") is not None
                return read(*args)

            handle.read = private_read
            yield handle

    native.native_source = guarded
    native.capture_bootstrap_outputs()
    evidence = ceph_native.canonical(native.markers.records)
    with native.bootstrap_hold() as material:
        for value in material.values():
            assert value not in evidence and hashlib.sha256(value).hexdigest().encode() not in evidence
    original = copy.deepcopy(native.markers.records)
    native.capture_bootstrap_outputs()
    assert native.markers.records == original


def test_replaced_matching_bootstrap_output_refuses_before_private_read(native):
    protocol(native)
    sources(native)
    native.daemons = lambda: fixtures.initial_daemons(native)
    native.capture_bootstrap_outputs()
    path = native.bootstrap_sources()["keyring"]
    replacement = path + ".replacement"
    ceph_native.private_write(replacement, b"private synthetic keyring")
    os.replace(replacement, path)
    source = native.native_source

    @contextlib.contextmanager
    def guarded(name, **kwargs):
        with source(name, **kwargs) as handle:
            if name == path:
                handle.read = lambda *args: pytest.fail("replacement content read before custody refusal")
            yield handle

    native.native_source = guarded
    with pytest.raises(custody.CustodyRefusal):
        with native.bootstrap_hold():
            pytest.fail("matching replacement was adopted")


@pytest.mark.parametrize("command", ["set-priv-key", "set-pub-key", "set-ssh-config", "set-user", "call-home-enabled"])
def test_lost_setting_response_recovers_exact_original_without_repeating_effect(native, private, command):
    unused, calls = fixtures.configuration_fixture(native, private)
    ceph = native.ceph
    lost = [False]

    def interrupted(*args, **kwargs):
        result = ceph(*args, **kwargs)
        if command in args and not lost[0]:
            lost[0] = True
            raise ceph_native.Refusal("native-command")
        return result

    native.ceph = interrupted
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.tunnel_configure(fixtures.ssh_material(private))
    native.ceph = ceph
    native.tunnel_configure(fixtures.ssh_material(private))
    assert sum(command in args for args, unused in calls) == 1
    previous = copy.deepcopy(native.markers.records)
    assert native.configuration_complete()
    assert native.markers.records == previous
    assert native.tunnel_configure(fixtures.ssh_material(private)) == {"changed": False}
    assert sum(command in args for args, unused in calls) == 1


def test_attempted_missing_setting_is_ambiguous_and_never_retried(native, private):
    unused, calls = fixtures.configuration_fixture(native, private)
    intent = native.configuration_intent()
    native.markers.records["tunnel-config-intent.json"] = intent
    native.markers.records["tunnel-config-attempt-ssh_identity_key.json"] = dict(intent, step="ssh_identity_key")
    with pytest.raises(ceph_native.Refusal, match="partial-cluster"):
        native.tunnel_configure(fixtures.ssh_material(private))
    assert not any(args[0] == "cephadm" for args, unused in calls)


@pytest.mark.parametrize("bound", [False, True])
def test_matching_unbound_or_different_bound_native_private_key_refuses(native, private, bound):
    state, calls = fixtures.configuration_fixture(native, private)
    state["values"]["ssh_identity_key"] = b"foreign" if bound else private["clusterPrivateKey"].encode()
    if bound:
        intent = native.configuration_intent()
        native.markers.records["tunnel-config-attempt-ssh_identity_key.json"] = dict(intent, step="ssh_identity_key")
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved" if bound else "ownership"):
        native.tunnel_configure(fixtures.ssh_material(private))
    assert not any(args[0] == "cephadm" for args, unused in calls)
    assert any(args[:2] == ("config-key", "get") for args, unused in calls) == bound


@pytest.mark.parametrize("stage", ["unclaimed", "claimed", "prepared", "seed", "configured", "mounted"])
def test_coordinator_runs_only_missing_proved_transitions(core_request, monkeypatch, stage, tmp_path):
    core_request["clusterSSHTransport"] = "verified-tunnel"
    core_fixtures.package_intent(core_request)
    clock = core_fixtures.ReadinessClock()

    class Transport(core_fixtures.ReadinessTransport):
        def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
            if phase == "prerequisites":
                self.calls.append((host["name"], phase, timeout))
                local = stage if stage in ("unclaimed", "claimed", "prepared") else "complete" if host["name"] == core_request["bootstrap"] else "prepared"
                return {"name": host["name"], "owned": local != "unclaimed", "exists": local == "complete",
                        "recovery": staging.recovery_record(core_request, host["name"], local)}
            if phase == "bootstrap_state":
                self.calls.append((host["name"], phase, timeout))
                value = staging.recovery_record(core_request, host["name"], stage)
                value["evidence"] = dict(core_fixtures.observation(core_request), owned=stage != "unclaimed", exists=stage != "unclaimed",
                                         partial=stage != "unclaimed", removed=False)
                return value
            if phase in ("tunnel_configure", "tunnel_mount"):
                self.calls.append((host["name"], phase, timeout))
                return {"changed": True}
            return super().call(host, phase, private, timeout=timeout, devices=devices)

    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    transport = Transport(core_request, clock, ready_after=1)
    outputs = {"dashboard": str(tmp_path / "dashboard")}
    assert ceph_core.execute(core_request, "apply", "d" * 64, transport, outputs, now=clock.now, pause=clock.pause)["evidence"]["ready"]
    phases = [phase for unused, phase, unused in transport.calls]
    assert phases.count("claim") == (3 if stage == "unclaimed" else 0)
    assert phases.count("authorize") == (3 if stage in ("unclaimed", "claimed") else 0)
    assert phases.count("bootstrap") == (1 if stage in ("unclaimed", "claimed", "prepared") else 0)
    assert phases.count("tunnel_configure") == (0 if stage in ("configured", "mounted") else 1)
    assert phases.count("tunnel_mount") == (0 if stage == "mounted" else 1)
    assert phases.count("enroll") == 1
    assert phases.count("services") == len(core_request["services"])
    assert phases.count("cache") == phases.count("host_devices") == 3 and "packages" not in phases
    assert max(index for index, phase in enumerate(phases) if phase == "cache") < phases.index("enroll")
    assert phases.count("tunnel_preflight") == (3 if stage in ("unclaimed", "claimed") else 0)
    preconvergence = phases[:phases.index("services") + 1]
    assert len(preconvergence) <= 34


def test_partial_enrollment_fence_resumes_without_erasing_ownership(native):
    scope = protocol(native)
    native.markers.records["bootstrap-enrollment.json"] = scope
    native.daemons = lambda: fixtures.initial_daemons(native)
    native.observe = lambda: dict(native.empty_evidence(), exists=True, partial=True)
    assert native.bootstrap_state()["stage"] == "enrolling"
    assert native.markers.writes == []


def test_legacy_owner_without_recovery_scope_refuses_instead_of_becoming_prepared(native):
    native.daemons = lambda: []
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.preparation_state()
    assert native.markers.writes == []


@pytest.mark.parametrize("receipt", ["bootstrap-output-source.json", "bootstrap-original-ready.json", "tunnel-configured.json", "tunnel-redeploy.json"])
def test_absent_attempt_with_later_receipts_is_contradictory_not_a_fresh_cluster(native, receipt):
    native.daemons = lambda: []
    native.markers.records[receipt] = {"foreign": "later-stage"}
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.prove_bootstrap_absence()
    assert native.markers.writes == []


def test_observed_mount_without_completion_receipts_requests_only_safe_finalization(native):
    protocol(native)
    sources(native)
    native.daemons = lambda: fixtures.initial_daemons(native)
    native.markers.records["tunnel-configured.json"] = {"fixture": "complete"}
    native.markers.records["tunnel-redeploy.json"] = {"fixture": "attempted"}
    native.configuration_complete = lambda: True
    native.prove_mounted_seed = lambda daemons, observing=False: []
    original = copy.deepcopy(native.markers.records)
    assert native.bootstrap_state()["stage"] == "configured"
    assert native.markers.records == original and native.markers.writes == []
    native.markers.records["tunnel-mounted.json"] = {"fixture": "complete"}
    assert native.bootstrap_state()["stage"] == "configured"
    native.markers.records["tunnel-manager-files.json"] = {"fixture": "complete"}
    assert native.bootstrap_state()["stage"] == "mounted"


def test_enrollment_fence_precedes_first_host_effect(native, private):
    fixtures.configuration_fixture(native, private)
    native.markers.records["tunnel-mounted.json"] = {"fixture": "complete"}
    native.markers.records["cache-ready.json"] = {"fixture": "complete"}
    native.verify_transport_bundle = lambda: None
    native.verify_tunnel_configuration = lambda: None
    native.verify_manager_mounts = lambda daemons, observing=False: []
    write = native.markers.write

    def stop(name, value):
        if name == "bootstrap-enrollment.json":
            raise ceph_native.Refusal("ownership")
        write(name, value)

    native.markers.write = stop
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.enroll()
    assert "bootstrap-enrollment.json" not in native.markers.writes


@pytest.mark.parametrize("route", ["json", "effective-config", "ssh-config"])
def test_every_ceph_cli_route_refuses_missing_original_before_consuming_admin_keyring(native, route):
    protocol(native)
    sources(native)
    native.ceph = lambda *args, **kwargs: pytest.fail("unexpected patched route")
    if route == "json":
        native.ceph = ceph_native.Worker.ceph.__get__(native)

        def call():
            return native.ceph("fsid")
    elif route == "effective-config":
        native.request["config"] = {"osd": {"osd_memory_target": "2147483648"}}
        native.markers.records["bootstrap-scope.json"] = staging.recovery_scope(native.request)

        def call():
            return native.config_effective(0)
    else:
        call = native.verify_tunnel_configuration
    native.markers.records["bootstrap-output-source.json"] = {"scope": native.recovery_scope(), "files": {"config": {}, "keyring": {}}}
    with pytest.raises(custody.CustodyRefusal):
        call()


def test_lost_mount_reply_is_finalized_by_coordinator_before_enrollment_without_redeploy(native, private):
    daemons, unused_1, unused_2 = fixtures.mount_fixture(native)
    mgr_source = native.native_source
    protocol(native)
    sources(native)
    root_source = native.native_source

    @contextlib.contextmanager
    def source(path, **kwargs):
        provider = root_source if path in native.bootstrap_sources().values() else mgr_source
        with provider(path, **kwargs) as handle:
            yield handle

    native.native_source = source
    native.capture_bootstrap_outputs()
    native.configuration_complete = lambda: True
    native.license_accepted = lambda: True
    inventory, effects = [], []
    intent = staging.stage_intent(native.request)
    generations = itertools.count(1)

    def ceph(*args, **kwargs):
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("orch", "host", "ls"):
            return copy.deepcopy(inventory)
        if args in (("orch", "ls"), ("osd", "ls")):
            if args == ("orch", "ls") and kwargs.get("json_result") is False:
                return b"[]"
            return []
        if args == ("quorum_status",):
            return {"quorum_names": [intent["monID"]]}
        if args == ("mgr", "dump"):
            return {"available": True, "active_name": intent["mgrID"], "active_gid": next(generations), "standbys": []}
        if args == ("orch", "status"):
            return {"available": True, "backend": "cephadm"}
        assert args == ("orch", "apply", "-i", "-")
        assert native.markers.read("bootstrap-enrollment.json") == staging.recovery_scope(native.request)
        for spec in yaml.safe_load_all(kwargs["data"]):
            inventory.append({name: spec[name] for name in ("hostname", "addr", "labels")})
        return b""

    native.ceph = ceph

    def lost_reply(argv, **kwargs):
        assert argv[3] == "deploy"
        effects.append(argv)
        raise ceph_native.Refusal("native-command")

    native.command = lost_reply
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.tunnel_mount()
    assert len(effects) == 1 and native.markers.read("tunnel-mounted.json") is None
    state = native.bootstrap_state()
    assert state["stage"] == "configured"
    assert native.markers.read("tunnel-mounted.json") is None
    preparations = []
    for host in native.request["hosts"]:
        local = "complete" if host["name"] == native.request["bootstrap"] else "prepared"
        preparations.append({"name": host["name"], "owned": True, "exists": local == "complete",
                             "recovery": staging.recovery_record(native.request, host["name"], local)})

    class Transport:
        def bootstrap_material(self):
            return private

        def call(self, host, phase, private=None, devices=None, service=None):
            del private, devices
            if phase == "tunnel_mount":
                return native.tunnel_mount()
            if phase == "cache":
                if host == native.host:
                    native.markers.write("cache-ready.json", {"fixture": "complete"})
                return {"changed": True}
            if phase == "host_devices":
                return {"devices": [{"host": host["name"], "path": device["path"], "serial": device["serial"], "deviceID": device["serial"]}
                                    for device in host["devices"]]}
            if phase == "enroll":
                assert native.markers.read("tunnel-mounted.json") is not None
                return native.enroll()
            assert phase in ("devices", "license", "services", "osd_prepare")
            return {"changed": False}

    ceph_core.apply_tunnel_cluster(native.request, native.host, Transport(), state, preparations)
    assert len(effects) == 1 and len(inventory) == 3
    assert native.markers.read("tunnel-manager-files.json") is not None


def cephadm_outputs(native):
    """What cephadm 20.2.1 leaves under sudo's umask 0022: the config from a plain open (0644), the keyring from write_new (0600)."""
    paths = native.bootstrap_sources()
    previous = os.umask(0o022)
    try:
        with open(paths["config"], "w", encoding="ascii") as handle:
            handle.write("[global]\n\tfsid = " + native.request["fsid"] + "\n")
        descriptor = os.open(paths["keyring"], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w", encoding="ascii") as handle:
            handle.write("[client.admin]\n\tkey = private synthetic keyring\n")
    finally:
        os.umask(previous)
    return paths


def private_sources(native):
    """The descriptors capture reads, held to native_source's root-only private proof: closed to group and others."""
    @contextlib.contextmanager
    def source(path, **kwargs):
        assert path in native.bootstrap_sources().values() and kwargs == {"root_only": True}
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb") as handle:
            ceph_native.require(os.fstat(handle.fileno()).st_mode & 0o077 == 0, "file")
            yield handle

    native.native_source = source


def test_bootstrap_capture_makes_cephadms_public_config_private_before_recording_it(native, private):
    fixtures.prepared_bootstrap(native, private)
    del native.capture_bootstrap_outputs
    native.markers.records["transport-ready.json"] = {"fixture": True}
    seeded = []
    native.daemons = lambda: fixtures.initial_daemons(native) if seeded else []
    private_sources(native)

    def cephadm(argv, **kwargs):
        assert argv[3] == "bootstrap" and kwargs["timeout"] == ceph_native.BOOTSTRAP_TIMEOUT
        seeded.append(cephadm_outputs(native))

    native.command = cephadm
    assert native.tunnel_bootstrap(fixtures.ssh_material(private)) == {"changed": True}
    recorded = native.markers.read("bootstrap-output-source.json")["files"]
    assert {name: os.stat(path).st_mode & 0o777 for name, path in seeded[0].items()} == {"config": 0o600, "keyring": 0o600}
    assert recorded["config"]["mode"] == recorded["keyring"]["mode"] == 0o600
    with native.bootstrap_hold() as material:
        assert material["config"].startswith(b"[global]\n") and material["keyring"].startswith(b"[client.admin]\n")
    original = copy.deepcopy(native.markers.records)
    native.capture_bootstrap_outputs()
    assert native.markers.records == original


def test_an_uncaptured_seed_is_proved_with_cephadms_own_modes_and_never_changed(native):
    protocol(native)
    private_sources(native)
    native.daemons = lambda: fixtures.initial_daemons(native)
    paths = cephadm_outputs(native)
    assert native.prove_seed(fixtures.initial_daemons(native)) == staging.stage_intent(native.request)
    assert os.stat(paths["config"]).st_mode & 0o777 == 0o644 and native.markers.writes == []
    os.chmod(paths["config"], 0o666)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.prove_seed(fixtures.initial_daemons(native))
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.capture_bootstrap_outputs()
    assert os.stat(paths["config"]).st_mode & 0o777 == 0o666 and native.markers.read("bootstrap-output-source.json") is None


def admin_cli(native, stored):
    """The 20.2.1 host CLI: config-key get prints the stored value as it is, after a blank line when asked for JSON."""
    calls = []

    def run(argv, data=b"", timeout=60):
        del timeout
        assert argv[:5] == ["/usr/bin/ceph", "--conf", native.bootstrap_sources()["config"], "--keyring", native.bootstrap_sources()["keyring"]]
        arguments = argv[5:]
        formatted = arguments[:2] == ["--format", "json"]
        arguments = arguments[2:] if formatted else arguments
        calls.append((formatted, arguments))
        if arguments == ["config-key", "ls"]:
            return 0, json.dumps(sorted(stored)).encode()
        if arguments[:2] == ["config-key", "get"]:
            return 0, (b"\n" if formatted else b"") + stored[arguments[2]]
        names = {"set-user": "ssh_user", "set-ssh-config": "ssh_config"}
        assert arguments[0] == "cephadm" and arguments[1] in names
        stored["mgr/cephadm/" + names[arguments[1]]] = arguments[2].encode() if arguments[1] == "set-user" else data
        return 0, b""

    del native.ceph, native.command
    native.run = run
    native.bootstrap_hold = contextlib.nullcontext
    return calls


@pytest.mark.parametrize("name,wanted,command", [
    ("ssh_user", b"operator", ("cephadm", "set-user", "operator", "--skip-pre-steps")),
    ("ssh_config", b"Host *\n  User operator\n", ("cephadm", "set-ssh-config", "-i", "-")),
])
def test_a_stored_setting_is_read_back_as_the_cli_prints_it(native, private, name, wanted, command):
    fixtures.staged(native)
    fixtures.prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"custodyID": "d31ad0d4-2fcf-4a8b-9b92-5010197978ce"}
    stored = {}
    calls = admin_cli(native, stored)
    assert native.configure_setting(name, wanted, command, True) == (True, True)
    assert native.markers.read("tunnel-config-complete-" + name + ".json")["step"] == name
    assert [formatted for formatted, arguments in calls if arguments[:2] == ["config-key", "get"]] == [False]
    assert native.configure_setting(name, wanted, command, False) == (True, False)


@pytest.mark.parametrize("value,needed", [("true", True), ("false", False), ("True", True), ("False", False)])
def test_the_call_home_acceptance_flag_reads_both_spellings_of_one_boolean(native, value, needed):
    native.ceph = lambda *args, **kwargs: [{"section": "mgr", "name": "mgr/cephadm/call_home_needs_acceptance", "value": value, "mask": ""}]
    assert native.call_home_acceptance() is needed


@pytest.mark.parametrize("value", ["yes", "0", "", None, True])
def test_any_other_call_home_acceptance_value_is_drift(native, value):
    native.ceph = lambda *args, **kwargs: [{"section": "mgr", "name": "mgr/cephadm/call_home_needs_acceptance", "value": value, "mask": ""}]
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.call_home_acceptance()


def manual_clock(native):
    clock = {"value": 0.0}
    native.clock = lambda: clock["value"]
    native.pause = lambda seconds: clock.update(value=clock["value"] + seconds)
    return clock


def enrollment_fixture(native, failures):
    native.markers.records.update({"tunnel-mounted.json": {"fixture": True}, "cache-ready.json": {"fixture": True},
                                   "bootstrap-scope.json": staging.recovery_scope(native.request)})
    native.verify_transport_bundle = lambda: None
    native.verify_manager_mounts = lambda observed, observing=False: []
    native.daemons = lambda: fixtures.initial_daemons(native)
    inventory, events = [], []

    def configuration():
        events.append("config")
        if failures["config"]:
            failures["config"] -= 1
            raise ceph_native.Refusal("native-timeout")

    def ceph(*args, **kwargs):
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("orch", "host", "ls"):
            events.append("inventory")
            if failures["inventory"]:
                failures["inventory"] -= 1
                raise ceph_native.Refusal("native-command")
            return copy.deepcopy(inventory)
        assert args == ("orch", "apply", "-i", "-") and native.markers.read("bootstrap-enrollment.json") is not None
        for spec in yaml.safe_load_all(kwargs["data"]):
            events.append(spec["hostname"])
            inventory.append({"hostname": spec["hostname"], "addr": spec["addr"], "labels": spec["labels"]})
        return b""

    native.verify_tunnel_configuration = configuration
    native.ceph = ceph
    # These restarted-manager fixtures begin after license acceptance; the
    # finalization suite models absent acceptance and the late native effect.
    native.license_accepted = lambda: True
    return inventory, events


def test_enrollment_waits_out_the_restarted_orchestrator_before_its_fence(native):
    clock = manual_clock(native)
    inventory, events = enrollment_fixture(native, {"config": 1, "inventory": 2})
    assert native.enroll() == {"changed": True}
    assert events[:5] == ["config", "config", "inventory", "inventory", "inventory"]
    assert [value for value in events if value.startswith("node-")] == ["node-1", "node-2", "node-3"]
    assert events[-1] == "inventory"
    assert [host["hostname"] for host in inventory] == ["node-1", "node-2", "node-3"]
    assert clock["value"] == 3 * ceph_native.MANAGER_INTERVAL


def test_an_orchestrator_that_never_returns_refuses_before_the_enrollment_fence(native):
    clock = manual_clock(native)
    native.horizon = 120
    enrollment_fixture(native, {"config": 0, "inventory": 10 ** 6})
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        native.enroll()
    assert native.markers.read("bootstrap-enrollment.json") is None and clock["value"] <= native.horizon


def test_the_initial_manager_redeploy_waits_for_the_restarted_manager_before_any_proof(native):
    fixtures.mount_fixture(native)
    clock = manual_clock(native)
    intent = staging.stage_intent(native.request)
    maps = iter([(True, intent["mgrID"], 7), (True, intent["mgrID"], 7), (False, "", 0), (True, intent["mgrID"], 9), (True, intent["mgrID"], 9)])
    unanswered = [ceph_native.Refusal("native-command")]
    events = []

    def ceph(*args, **kwargs):
        del kwargs
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("mgr", "dump"):
            events.append("map")
            available, name, generation = next(maps)
            return {"available": available, "active_name": name, "active_gid": generation, "standbys": []}
        if args == ("orch", "status"):
            events.append("status")
            if unanswered:
                raise unanswered.pop()
            return {"available": True, "backend": "cephadm", "paused": False, "workers": 10}
        return []

    native.ceph = ceph
    native.command = lambda argv, **kwargs: events.append("deploy")
    native.verify_manager_mounts = lambda observed: events.append("mounts")
    native.verify_tunnel_configuration = lambda: events.append("config")
    assert native.tunnel_mount() == {"changed": True}
    assert events == ["map", "deploy", "mounts", "map", "map", "map", "status", "map", "status", "config"]
    assert clock["value"] == 3 * ceph_native.MANAGER_INTERVAL


def test_a_manager_that_never_returns_after_the_redeploy_times_out_within_the_wait(native):
    fixtures.mount_fixture(native)
    clock = manual_clock(native)
    native.ceph = lambda *args, **kwargs: ({"fsid": native.request["fsid"]} if args == ("fsid",) else
                                           {"available": False, "active_name": "", "active_gid": 0, "standbys": []} if args == ("mgr", "dump") else [])
    native.command = lambda argv, **kwargs: None
    native.verify_tunnel_configuration = lambda: pytest.fail("a proof ran before the manager returned")
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        native.tunnel_mount()
    assert ceph_native.MANAGER_WAIT - ceph_native.MANAGER_INTERVAL <= clock["value"] <= ceph_native.MANAGER_WAIT
    assert native.markers.read("tunnel-redeploy.json") is not None and native.markers.read("tunnel-mounted.json") is None

"""NVMe-oF proofs bind authorization and every native resource to one attempt."""

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_nvmeof as nvmeof
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request


@pytest.fixture
def nvme_request(core_request):
    core_request["action"] = "nvmeof"
    core_request["delegated"] = [{"name": "nvmeof.config.group", "hosts": ["node-1", "node-2"]}]
    core_request["nvmeof"] = {
        "name": "subsystem", "service": "config.group", "group": "group", "pool": "config", "mtls": "enable_auth",
        "nqn": "nqn.2026-01.test.example:subsystem", "hosts": ["nqn.2026-01.test.example:initiator"],
        "gateways": [{"host": "node-1", "address": "192.0.2.1", "port": 5500}, {"host": "node-2", "address": "192.0.2.2", "port": 5500}],
        "listeners": [{"host": "node-1", "address": "192.0.2.1", "port": 4420}, {"host": "node-2", "address": "192.0.2.2", "port": 4420}],
        "namespaces": [{"id": 1, "image": {"name": "image", "pool": "images", "dataPool": "", "sizeBytes": 1024 ** 3, "features": ["layering"]}}],
    }
    return core_request


@pytest.mark.parametrize("damage", [None, "wildcard", "missing-image", "duplicate-id", "foreign-host", "tls-bypass", "command"])
def test_frozen_request_admission(nvme_request, damage):
    frozen = nvme_request["nvmeof"]
    if damage == "wildcard":
        frozen["hosts"] = ["*"]
    elif damage == "missing-image":
        del frozen["namespaces"][0]["image"]
    elif damage == "duplicate-id":
        frozen["namespaces"].append(copy.deepcopy(frozen["namespaces"][0]))
    elif damage == "foreign-host":
        frozen["listeners"][0]["host"] = "foreign"
    elif damage == "tls-bypass":
        frozen["mtls"] = "insecure"
    elif damage == "command":
        frozen["command"] = ["anything"]
    if damage is None:
        assert ceph_native.validate(nvme_request) is nvme_request
    else:
        with pytest.raises(ceph_native.Refusal):
            ceph_native.validate(nvme_request)


def test_attempt_identity_is_stable_and_changes_with_original_owner(nvme_request):
    native = nvmeof.NVMeOFWorker(nvme_request, "node-1", markers=fixtures.Markers())
    first = native.nvme_identity()
    assert first == native.nvme_identity()
    nvme_request["owner"] = "other-attempt"
    assert first != native.nvme_identity()


@pytest.fixture
def world(nvme_request, monkeypatch):
    monkeypatch.setattr(nvmeof, "listener_reachable", lambda listener: True)
    native = nvmeof.NVMeOFWorker(nvme_request, "node-1", markers=fixtures.Markers())
    frozen = nvme_request["nvmeof"]
    service = {"service_type": "nvmeof", "service_id": frozen["service"], "placement": {"hosts": ["node-1", "node-2"]},
               "spec": {"pool": frozen["pool"], "group": frozen["group"], "enable_auth": True,
                        **{key: "private-certificate-material" for key in nvmeof.TLS_FIELDS}}}
    held = {"scope": {"owner": nvme_request["owner"], "fsid": nvme_request["fsid"]}, "service": copy.deepcopy(service)}
    for suffix in ("attempt", "complete"):
        native.markers.records["service-nvmeof.config.group-" + suffix + ".json"] = copy.deepcopy(held)
    image = frozen["namespaces"][0]["image"]
    image_info = {"id": "image-native-id", "size": image["sizeBytes"], "format": 2, "features": ["layering"]}
    native.markers.records["rbd-image.json"] = image
    native.markers.records["rbd-attempt.image.json"] = {
        "version": "ceph-rbd-image-attempt-v1", "owner": nvme_request["owner"], "fsid": nvme_request["fsid"], "rbd": image}
    native.markers.records["rbd-id.image.json"] = {"id": "image-native-id"}
    native.service_specs = lambda **kwargs: {"nvmeof.config.group": service}
    native.owned_pool = lambda pool: None
    native.rbd_names = lambda pool: ["image"]
    native.rbd_info = lambda image: image_info
    state = {"subsystems": [], "hosts": [], "listeners": [], "namespaces": [], "effects": [], "interrupt": None, "ana": "NON_OPTIMIZED"}
    gateway_entries = [{"service_url": g["address"] + ":5500", "group": "group", "daemon_name": "nvmeof.config.group." + g["host"] + ".abcdef"}
                       for g in frozen["gateways"]]

    def command(*args, **kwargs):
        del kwargs
        if args == ("--help",):
            return b"\n".join(("nvmeof " + family + " " + verb).encode() for family, verb in nvmeof.COMMANDS)
        if args == ("fsid",):
            return {"fsid": nvme_request["fsid"]}
        if args == ("dashboard", "nvmeof-gateway-list"):
            return {"gateways": {"nvmeof.config.group": gateway_entries}}
        assert args[0] == "nvmeof"
        family, verb = args[1:3]
        options = {}
        index = 3
        while index < len(args):
            token = args[index]
            if "=" in token:
                key, value = token.split("=", 1)
                index += 1
            else:
                key, value = token, args[index + 1]
                index += 2
            options[key.removeprefix("--")] = value
        assert options["gw_group"] == "group"
        if family == "gateway":
            g = next(g for g in frozen["gateways"] if g["address"] == options["server_address"])
            group = frozen["gateways"].index(g) + 1
            if verb == "info":
                return {"status": 0, "hostname": g["host"], "group": "group", "addr": g["address"], "port": 5500,
                        "name": "client.nvmeof.config.group." + g["host"] + ".abcdef", "gateway_initialization_over": True,
                        "load_balancing_group": group}
            assert verb == "listener_info"
            return {"status": 0, "gw_listeners": [{"listener": dict(l, active=True), "lb_states": [
                {"grp_id": 1, "state": "OPTIMIZED" if group == 1 else state["ana"]}]} for l in state["listeners"] if l["host_name"] == g["host"]]}
        member = {"subsystem": "subsystems", "listener": "listeners", "namespace": "namespaces", "host": "hosts"}[family]
        if verb == "list":
            return {"status": 0, member: copy.deepcopy(state[member]), "allow_any_host": False, "subsystem_nqn": frozen["nqn"]}
        assert verb == "add"
        state["effects"].append((family, options))
        if family == "subsystem":
            assert options["no_group_append"] == "true"
            state[member].append({"nqn": options["nqn"], "serial_number": options["serial_number"], "enable_ha": True,
                                  "allow_any_host": False, "has_dhchap_key": False, "max_namespaces": int(options["max_namespaces"])})
        elif family == "namespace":
            assert options["create_image"] == options["trash_image"] == options["force"] == "false"
            state[member].append({"nsid": int(options["nsid"]), "uuid": options["uuid"], "rbd_pool_name": options["rbd_pool"],
                                  "rbd_image_name": options["rbd_image_name"], "rbd_image_size": str(image["sizeBytes"]),
                                  "ns_subsystem_nqn": frozen["nqn"], "block_size": 512, "auto_visible": True, "trash_image": False,
                                  "load_balancing_group": int(options["load_balancing_group"])})
        elif family == "listener":
            assert options["verify_host_name"] == "true" and options["force"] == "false"
            state[member].append({"host_name": options["host_name"], "traddr": options["traddr"], "trsvcid": int(options["trsvcid"]),
                                  "adrfam": int(options["adrfam"]), "trtype": "TCP", "secure": False})
        else:
            assert options["host_nqn"] != "*"
            state[member].append({"nqn": options["host_nqn"], "use_psk": False, "use_dhchap": False})
        if state["interrupt"] == family:
            state["interrupt"] = None
            raise ceph_native.Refusal("native-command")
        return {"status": 0}

    native.ceph = native.tunnel_effect = command
    return native, state, service, image_info, gateway_entries


@pytest.mark.parametrize("interrupt", [None, "subsystem", "namespace", "listener", "host"])
def test_native_create_lost_reply_and_replay_preserve_one_identity(world, interrupt):
    native, state, unused_service, unused_image, unused_gateways = world
    state["interrupt"] = interrupt
    if interrupt is not None:
        with pytest.raises(ceph_native.Refusal, match="nvmeof-native"):
            native.nvmeof()
    assert native.nvmeof() == {"changed": True}
    assert native.nvmeof() == {"changed": False}
    assert len(state["effects"]) == 5
    before = copy.deepcopy(native.markers.records)
    evidence = {}
    assert native.observe_resource(evidence, True)
    assert evidence["owned"] and evidence["exists"] and evidence["resource"]["id"].startswith("bw")
    assert native.markers.records == before
    assert "private-certificate-material" not in repr(evidence)


@pytest.mark.parametrize("damage", ["tls", "verify", "image", "missing-image", "group", "foreign", "owner", "config-pool"])
def test_identity_and_mtls_fail_closed_before_any_effect(world, damage):
    native, state, service, image, gateways = world
    if damage == "tls":
        service["spec"]["enable_auth"] = False
    elif damage == "verify":
        service["spec"]["verify_listener_ip"] = False
    elif damage == "image":
        image["id"] = "foreign-image"
    elif damage == "missing-image":
        native.rbd_names = lambda pool: []
    elif damage == "group":
        gateways[0]["group"] = "other"
    elif damage == "foreign":
        state["subsystems"] = [{"nqn": native.request["nvmeof"]["nqn"], "serial_number": "foreign", "enable_ha": True,
                                "allow_any_host": False, "has_dhchap_key": False}]
    elif damage == "owner":
        native.request["owner"] = "other-owner"
    else:
        service["spec"]["pool"] = "foreign"
    before = list(native.markers.writes)
    with pytest.raises(ceph_native.Refusal) as error:
        native.nvmeof()
    assert "private-certificate-material" not in str(error.value)
    assert state["effects"] == [] and native.markers.writes == before


@pytest.mark.parametrize("damage", ["denied-nqn", "wildcard", "namespace", "listener", "ha", "missing", "ana"])
def test_readiness_rejects_foreign_authorization_and_changed_or_unavailable_paths(world, damage):
    native, state, unused_service, unused_image, unused_gateways = world
    native.nvmeof()
    if damage in ("denied-nqn", "wildcard"):
        state["hosts"].append({"nqn": "*" if damage == "wildcard" else "nqn.2026-01.test.example:denied"})
    elif damage == "namespace":
        state["namespaces"][0]["uuid"] = "foreign"
    elif damage == "listener":
        state["listeners"][0]["traddr"] = "192.0.2.99"
    elif damage == "ha":
        state["subsystems"][0]["enable_ha"] = False
    elif damage == "missing":
        state["namespaces"] = []
    else:
        state["ana"] = "INACCESSIBLE"
    effects = list(state["effects"])
    if damage == "ana":
        assert not native.observe_resource({}, True)
    else:
        with pytest.raises(ceph_native.Refusal):
            native.observe_resource({}, True)
    assert state["effects"] == effects


def test_missing_registered_native_command_refuses_before_claim(world):
    native, state, unused_service, unused_image, unused_gateways = world
    original = native.ceph

    def command(*args, **kwargs):
        if args == ("--help",):
            return b"nvmeof subsystem list\n"
        return original(*args, **kwargs)

    native.ceph = command
    with pytest.raises(ceph_native.Refusal, match="nvmeof-native"):
        native.nvmeof()
    assert state["effects"] == [] and native.markers.writes == []


def test_listener_unreachable_is_not_ready(world, monkeypatch):
    native = world[0]
    native.nvmeof()
    monkeypatch.setattr(nvmeof, "listener_reachable", lambda listener: False)
    assert not native.observe_resource({}, True)


@pytest.mark.parametrize("mode", ["enable_auth", "enable_mtls"])
def test_gateway_unit_closed_tls_binding_and_custody(nvme_request, mode):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_units as units
    request = copy.deepcopy(nvme_request)
    del request["nvmeof"]
    request["action"] = "service"
    request["auxiliary"] = [{"name": "nvmeof", "image": request["registry"] + "/nvmeof-synthetic@sha256:" + "c" * 64}]
    spec = {"pool": "config", "group": "group", "port": 5500, mode: True}
    unit = {"type": "nvmeof", "id": "config.group", "documents": [{"service_type": "nvmeof", "service_id": "config.group",
            "placement": {"hosts": ["node-1", "node-2"]}, "spec": spec}]}
    if mode == "enable_auth":
        unit["bindings"] = [{"serviceRef": "nvmeof.config.group", "path": "/spec/" + field,
                             "secretRef": "tls-" + ("ca" if field == "root_ca_cert" else field.split("_")[0])}
                            for field in sorted(nvmeof.TLS_FIELDS)]
    request["service"] = unit
    assert ceph_native.validate(request) is request
    assert units.material_names(request, "observe") == frozenset()
    assert units.material_names(request, "unit") == (frozenset("nvmeof_" + f for f in nvmeof.TLS_FIELDS) if mode == "enable_auth" else frozenset())
    damaged = copy.deepcopy(request)
    damaged["service"]["documents"][0]["spec"]["skip_verify"] = True
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(damaged)
    if mode == "enable_auth":
        damaged = copy.deepcopy(request)
        damaged["service"]["bindings"].pop()
        with pytest.raises(ceph_native.Refusal):
            ceph_native.validate(damaged)


def test_managed_mtls_mode_is_observed_and_not_inferred(world):
    native, state, service, unused_image, unused_gateways = world
    native.request["nvmeof"]["mtls"] = "enable_mtls"
    service["spec"] = {"pool": "config", "group": "group", "enable_mtls": True}
    for suffix in ("attempt", "complete"):
        native.markers.records["service-nvmeof.config.group-" + suffix + ".json"]["service"] = copy.deepcopy(service)
    native.nvmeof()
    assert native.observe_resource({}, True)
    service["spec"]["enable_mtls"] = False
    with pytest.raises(ceph_native.Refusal, match="nvmeof-identity"):
        native.observe_resource({}, True)


def test_gateway_address_selection_cannot_route_to_another_service(world):
    native, state, unused_service, unused_image, gateways = world
    original = native.ceph

    def registry(*args, **kwargs):
        if args == ("dashboard", "nvmeof-gateway-list"):
            return {"gateways": {"nvmeof.other": [dict(gateways[0], group="other")], "nvmeof.config.group": gateways}}
        return original(*args, **kwargs)

    native.ceph = registry
    with pytest.raises(ceph_native.Refusal, match="nvmeof-identity"):
        native.nvmeof()
    assert not state["effects"]
    assert not any(k.startswith("nvmeof-attempt.") for k in native.markers.records)


@pytest.mark.parametrize("value", [True, 1.5, "1.0", "-1", "01", "18446744073709551616"])
def test_native_uint64_size_cannot_coerce_an_ambiguous_identity(value):
    with pytest.raises(ceph_native.Refusal, match="native-output"):
        nvmeof.native_size(value)


@pytest.mark.parametrize("interrupt", [None, "subsystem", "namespace", "listener", "host"])
def test_execution_bound_covers_first_apply_replay_and_observation(world, interrupt):
    native, state, unused_service, unused_image, unused_gateways = world
    calls = []
    for name in ("ceph", "tunnel_effect", "service_specs", "owned_pool", "rbd_names", "rbd_info"):
        original = getattr(native, name)

        def counted(*args, _name=name, _original=original, **kwargs):
            calls.append(_name)
            return _original(*args, **kwargs)

        setattr(native, name, counted)
    state["interrupt"] = interrupt
    if interrupt:
        with pytest.raises(ceph_native.Refusal):
            native.nvmeof()
        calls.clear()
    native.nvmeof()
    assert len(calls) <= 24
    if interrupt is None:
        assert len(calls) == 24
    assert nvmeof.phase_budget(native.request, "nvmeof") == 24 * 60 + 6 * 30 + 60
    calls.clear()
    assert native.nvmeof_observe()["ready"]
    assert len(calls) == 16
    assert nvmeof.phase_budget(native.request, "nvmeof_observe") == 16 * 60 + 4 + 60
    assert nvmeof.execution_budget(native.request, "apply") == 5024


def test_execution_width_is_rejected_before_native_work(nvme_request):
    template = nvme_request["nvmeof"]["namespaces"][0]
    nvme_request["nvmeof"]["namespaces"] = [dict(copy.deepcopy(template), id=index + 1) for index in range(128)]
    for index, namespace in enumerate(nvme_request["nvmeof"]["namespaces"]):
        namespace["image"]["name"] = "image-" + str(index)
    with pytest.raises(ceph_native.Refusal, match="nvmeof-budget"):
        ceph_native.validate(nvme_request)


def test_cluster_phase_does_not_spend_resource_observation_budget(world, monkeypatch):
    native = world[0]
    calls = []

    def cluster_observation():
        native.blocking = []
        calls.append(native.observe_resource({}, True))
        return {"ready": True}

    monkeypatch.setattr(native, "observe", cluster_observation)
    assert native.nvmeof_cluster() == {"ready": True, "blocking": []}
    assert calls == [True]


def test_controller_uses_separate_request_bounded_phases(nvme_request):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_core
    calls = []
    observed = fixtures.observation(nvme_request, ready=True)
    observed["resource"] = {"id": "bw" + "a" * 18, "path": ""}

    class Transport:
        applied = []

        def call(self, host, phase, **options):
            calls.append((phase, options.get("timeout", 660)))
            if phase in ("prerequisites", "nvmeof"):
                return {"changed": True}
            if phase == "nvmeof_cluster":
                return observed
            assert phase == "nvmeof_observe"
            return {key: observed[key] for key in ("owned", "exists", "ready", "partial", "removed", "resource")}

    result = ceph_core.execute_nvmeof(nvme_request, "apply", "d" * 64, Transport(), nvme_request["hosts"][0], now=lambda: 0, pause=lambda delay: None)
    assert result["changed"] and result["evidence"]["ready"]
    assert calls == [("prerequisites", 660), ("nvmeof", 1700), ("nvmeof_cluster", 660), ("nvmeof_observe", 1044)]


@pytest.mark.parametrize("phase", ["nvmeof", "nvmeof_observe"])
def test_extended_launcher_is_fixed_to_one_phase_and_budget(nvme_request, monkeypatch, phase):
    import json
    import subprocess
    import sys
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native_worker
    transport = ceph_core.Transport(nvme_request, {})
    captured = []
    monkeypatch.setattr(transport, "_call", lambda *args, **kwargs: captured.append((args, kwargs)))
    transport.call(nvme_request["hosts"][0], phase, timeout=nvmeof.phase_budget(nvme_request, phase) + 20)
    args, options = captured[0]
    assert args[2]["budget"] == options["worker_budget"] == nvmeof.phase_budget(nvme_request, phase)
    assert options["worker_ceiling"] == 21600
    sources = {name: '"""Fixed fixture."""\n' for name in ceph_native_worker.MODULES}
    sources["native_diagnostics"] = "def valid(value): return False\n"
    sources["ceph_native"] = "class Refusal(ValueError): pass\ndef dispatch(value):\n    return {'phase': value['phase']}\n"
    for selected, budget, reason in ((phase, 3, None), ("bootstrap", 3, "native-refusal"), (phase, 21601, "native-unknown")):
        payload = {"sources": sources, "payload": {"phase": selected}, "budget": budget}
        result = subprocess.run([sys.executable, "-I", "-c", args[1], str(budget)], input=json.dumps(payload).encode(), capture_output=True,
                                timeout=10, check=False)
        assert result.returncode == 0 and result.stderr == b""
        value = json.loads(result.stdout)
        assert value.get("reason") == reason
        assert value["ok"] == (reason is None)
    with pytest.raises(ceph_native.Refusal, match="transport"):
        transport.call(nvme_request["hosts"][0], "nvmeof_cluster", timeout=661)


def test_controller_retry_window_cannot_extend_execution_bound(nvme_request):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_core
    clock = [0]
    calls = []
    observed = fixtures.observation(nvme_request, ready=True)
    observed.update(ready=False, partial=True, resource={"id": "bw" + "a" * 18, "path": ""})

    class Transport:
        applied = []

        def call(self, host, phase, **options):
            timeout = options.get("timeout", 660)
            calls.append((phase, timeout))
            clock[0] += timeout
            if phase in ("prerequisites", "nvmeof"):
                return {"changed": True}
            if phase == "nvmeof_cluster":
                return observed
            return {key: observed[key] for key in ("owned", "exists", "ready", "partial", "removed", "resource")}

    def pause(delay):
        clock[0] += delay

    with pytest.raises(ceph_core.ReadinessRefusal):
        ceph_core.execute_nvmeof(nvme_request, "apply", "d" * 64, Transport(), nvme_request["hosts"][0], now=lambda: clock[0], pause=pause)
    assert clock[0] == nvmeof.execution_budget(nvme_request, "apply") - 60
    assert calls[-1][0] == "nvmeof_observe" and 0 < calls[-1][1] < 1044


@pytest.fixture
def nvme_unit_request(nvme_request):
    request = copy.deepcopy(nvme_request)
    del request["nvmeof"]
    request["action"] = "service"
    request["auxiliary"] = [{"name": "nvmeof", "image": request["registry"] + "/nvmeof-synthetic@sha256:" + "c" * 64}]
    request["service"] = {"type": "nvmeof", "id": "config.group", "documents": [
        {"service_type": "nvmeof", "service_id": "config.group", "placement": {"hosts": ["node-1", "node-2"]},
         "spec": {"pool": "config", "group": "group", "port": 5500, "enable_mtls": True}}]}
    return request


def test_service_gateway_reads_have_their_own_additional_budget(nvme_unit_request, monkeypatch):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native_units
    native = ceph_native_units.UnitWorker(nvme_unit_request, "node-1", markers=fixtures.Markers())
    calls = []

    def command(*args):
        calls.append(args)
        host = next(h for h in nvme_unit_request["hosts"] if h["address"] == args[-1])
        return {"hostname": host["name"], "group": "group", "status": 0, "gateway_initialization_over": True}

    monkeypatch.setattr(native, "ceph", command)

    def observe():
        native.blocking = []
        return {"ready": nvmeof.readiness_unit(native, nvme_unit_request["service"], [])}

    monkeypatch.setattr(native, "observe", observe)
    assert native.nvmeof_unit_observe() == {"ready": True, "blocking": []}
    assert len(calls) == 2
    assert nvmeof.phase_budget(nvme_unit_request, "nvmeof_unit_observe") == 640 + len(calls) * 60 + 60
    transport = ceph_core.Transport(nvme_unit_request, {})
    captured = []
    monkeypatch.setattr(transport, "_call", lambda *args, **kwargs: captured.append((args, kwargs)))
    transport.call(nvme_unit_request["hosts"][0], "nvmeof_unit_observe", timeout=840)
    assert captured[0][0][2]["budget"] == 820 and captured[0][1]["worker_ceiling"] == 21600
    assert "PHASE='nvmeof_unit_observe'" in captured[0][0][1]
    nvme_unit_request["service"]["type"] = "crash"
    with pytest.raises(ceph_native.Refusal):
        native.nvmeof_unit_observe()
    with pytest.raises(ceph_native.Refusal):
        nvmeof.phase_budget(nvme_unit_request, "nvmeof_unit_observe")


def test_service_readiness_retry_clips_and_destroy_never_mutates(nvme_unit_request):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_core
    clock, calls = [0], []
    observed = fixtures.observation(nvme_unit_request, ready=True)
    observed.update(ready=False, partial=True)

    class Transport:
        def call(self, host, phase, **options):
            assert phase == "nvmeof_unit_observe"
            timeout = options["timeout"]
            calls.append(timeout)
            clock[0] += timeout
            return observed

    def pause(delay):
        clock[0] += delay

    with pytest.raises(ceph_core.ReadinessRefusal):
        ceph_core.wait_unit(nvme_unit_request, "d" * 64, nvme_unit_request["hosts"][0], Transport(), lambda: clock[0], pause)
    assert clock[0] == 840 + 900
    assert calls[0] == 840 and 0 < calls[-1] < 840
    calls.clear()
    with pytest.raises(ceph_native.Refusal, match="client-quiescence-unqualified"):
        ceph_core.execute_unit(nvme_unit_request, "destroy", "d" * 64, Transport(), nvme_unit_request["hosts"][0], now=lambda: clock[0], pause=pause)
    assert calls == [840]

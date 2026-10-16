"""Host-independent guards for the contained native SSH route probe."""

from __future__ import annotations

import asyncio
import base64
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import types

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_route_probe as PROBE


def synthetic():
    keys = {}
    for index, name in enumerate(PROBE.KEYS):
        blob = (11).to_bytes(4, "big") + b"ssh-ed25519" + (32).to_bytes(4, "big") + bytes([index + 1]) * 32
        public = "ssh-ed25519 " + base64.b64encode(blob).decode()
        private = "-----BEGIN OPENSSH PRIVATE KEY-----\nsynthetic-" + name + "\n-----END OPENSSH PRIVATE KEY-----\n"
        keys[name] = {"private": private, "public": public}
    return {"schema": PROBE.SYNTHETIC_SCHEMA, "uid": 4242, "gid": 4343, "user": PROBE.USER, "cryptoPolicy": "", "keys": keys}


def test_synthetic_input_is_closed_and_has_no_operator_endpoints():
    value = synthetic()
    assert PROBE.validate_synthetic(value, 4242, 4343) is value
    assert {"address", "port", "command", "executable"}.isdisjoint(value)


@pytest.mark.parametrize("change", [
    "extra", "identity", "uid", "gid", "bool-uid", "key-name", "key-option", "key-multiline", "private-format", "duplicate-key", "policy-large",
])
def test_synthetic_input_rejects_unbound_identity_and_key_shape(change):
    value = synthetic()
    if change == "extra":
        value["command"] = "arbitrary-command"
    elif change == "identity":
        value["user"] = "different-user"
    elif change in ("uid", "gid"):
        value[change] += 1
    elif change == "bool-uid":
        value["uid"] = True
    elif change == "key-name":
        value["keys"]["arbitrary"] = value["keys"]["outer"]
    elif change == "key-option":
        value["keys"]["client"]["public"] = 'command="arbitrary" ' + value["keys"]["client"]["public"]
    elif change == "key-multiline":
        value["keys"]["client"]["public"] += "\n"
    elif change == "private-format":
        value["keys"]["client"]["private"] = "arbitrary private content"
    elif change == "duplicate-key":
        value["keys"]["replacement"] = value["keys"]["outer"]
    elif change == "policy-large":
        value["cryptoPolicy"] = "x" * 65537
    with pytest.raises(PROBE.Refusal, match="^route-refusal$"):
        PROBE.validate_synthetic(value, 4242, 4343)


def test_duplicate_json_members_cannot_hide_synthetic_overrides():
    with pytest.raises(PROBE.Refusal):
        json.loads('{"uid":4242,"uid":4243}', object_pairs_hook=PROBE.unique_object)


def test_private_keys_must_match_their_declared_public_components():
    value = synthetic()
    fake = types.SimpleNamespace(import_private_key=lambda data: types.SimpleNamespace(export_public_key=lambda format: b"ssh-ed25519 mismatched"))
    with pytest.raises(PROBE.Refusal):
        PROBE.load_keys(fake, value)


def test_prepare_material_is_private_protocol_and_never_promotes(monkeypatch):
    value = synthetic()
    names = iter(PROBE.KEYS)

    def keygen(algorithm):
        assert algorithm == "ssh-ed25519"
        key = value["keys"][next(names)]
        return types.SimpleNamespace(export_private_key=lambda format: key["private"].encode(),
                                     export_public_key=lambda format: key["public"].encode())

    monkeypatch.setattr(PROBE, "account", lambda: (4242, 4343))
    monkeypatch.setattr(PROBE, "public_crypto_policy", lambda: "")
    result = PROBE.prepare(types.SimpleNamespace(generate_private_key=keygen))
    assert set(result) == {"schema", "ok", "synthetic"}
    assert result["ok"] is True
    assert result["synthetic"] == value


def test_mutable_synthetic_mount_refuses_before_reading_material(tmp_path, monkeypatch):
    tmp_path.chmod(0o700)
    (tmp_path / "synthetic.json").write_text(json.dumps(synthetic()))
    monkeypatch.setattr(PROBE, "SYNTHETIC_ROOT", str(tmp_path))
    with pytest.raises(PROBE.Refusal):
        PROBE.read_synthetic(os.geteuid(), os.getegid())


def test_mutable_provider_source_refuses_before_execution(tmp_path, monkeypatch):
    effect = tmp_path / "effect"
    source = tmp_path / "ssh.py"
    source.write_text("open(" + repr(str(effect)) + ", 'w').write('unexpected source')\n")
    source.chmod(0o666)
    monkeypatch.setattr(PROBE, "PROVIDER_SOURCE", str(source))
    with pytest.raises(PROBE.Refusal):
        PROBE.provider_module()
    assert not effect.exists()


def test_provider_import_stubs_are_only_error_bookkeeping_and_are_restored(tmp_path, monkeypatch):
    source = tmp_path / "ssh.py"
    raw = (b"from orchestrator import OrchestratorError\nfrom ceph.deployment.hostspec import normalize_hostname\n"
           b"normalized = normalize_hostname('route-target')\nerror = OrchestratorError\n")
    source.write_bytes(raw)
    sentinel = types.ModuleType("orchestrator")
    monkeypatch.setitem(sys.modules, "orchestrator", sentinel)
    monkeypatch.setattr(PROBE, "PROVIDER_SOURCE", str(source))
    module = PROBE.provider_module()
    assert module.normalized == "route-target" and module.error is PROBE.Refusal
    assert sys.modules["orchestrator"] is sentinel


class NativeHostError(ValueError):
    pass


def provider():
    return types.SimpleNamespace(HostConnectionError=NativeHostError,
                                 RemoteCommand=lambda executable: executable, Executables=types.SimpleNamespace(TRUE="true"))


def counters():
    return {name: 0 for name in PROBE.COUNTERS}


def simulated_case(value, increments=None, response=("route-ok", "", 0), error=None, delay=0):
    async def execute(host, command):
        assert host in (PROBE.HOST, PROBE.UNKNOWN_HOST) and command == "true"
        for name, count in (increments or {}).items():
            value[name] += count
        if delay:
            await asyncio.sleep(delay)
        if error is not None:
            raise error
        return response
    return types.SimpleNamespace(_execute_command=execute)


def test_positive_case_requires_both_ssh_layers_and_exact_effect():
    value = counters()
    expected = {name: int(name != "directConnections") for name in PROBE.COUNTERS}
    native = simulated_case(value, expected)
    assert asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, True, "correctKey")) == expected


def test_server_authenticates_exact_serialized_public_identity_across_distinct_key_objects():
    public = b"ssh-ed25519 synthetic-public-identity\n"
    private_key = types.SimpleNamespace(export_public_key=lambda format: public)
    incoming_key = types.SimpleNamespace(export_public_key=lambda format: public)
    wrong_key = types.SimpleNamespace(export_public_key=lambda format: b"ssh-ed25519 replaced-public-identity\n")
    assert incoming_key is not private_key
    server = PROBE.server_factory(types.SimpleNamespace(SSHServer=object), "outer", counters(), set(), private_key)()
    assert server.validate_public_key(PROBE.USER, incoming_key) is True
    assert server.validate_public_key(PROBE.USER, wrong_key) is False
    assert server.validate_public_key("unbound-user", incoming_key) is False


def test_failed_positive_case_preserves_only_observed_bounded_counters():
    value = counters()
    native = simulated_case(value, {"outerConnections": 1}, error=NativeHostError("private native diagnostics"))
    with pytest.raises(PROBE.CaseRefusal) as caught:
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, True, "correctKey"))
    assert caught.value.case == "correctKey"
    assert caught.value.counters == dict(counters(), outerConnections=1)
    assert caught.value.reason == "correct-key-refusal"


def test_native_wrapper_diagnosis_retains_only_allowlisted_deep_exception_class():
    inner = FileNotFoundError("private fixture file")
    outer = NativeHostError("private provider wrapper")
    outer.__cause__ = inner
    assert PROBE.exception_class(outer) == "FileNotFoundError"
    inner.__context__ = outer
    assert PROBE.exception_class(outer) == "FileNotFoundError"
    private = type("PrivateEnvironmentClass", (ValueError,), {})("private fixture material")
    assert PROBE.exception_class(private) == "unknown"
    assert PROBE.exception_class(None) == "none"
    refused = pytest.raises(PROBE.Refusal, PROBE.CaseRefusal, "correctKey", counters(), "PrivateEnvironmentClass")
    assert not isinstance(refused.value, PROBE.CaseFailure)


def test_exception_chain_diagnosis_has_a_fixed_eight_link_bound():
    current = FileNotFoundError("private deep value")
    for _depth in range(8):
        outer = RuntimeError("private wrapper value")
        outer.__context__ = current
        current = outer
    assert PROBE.exception_class(current) == "RuntimeError"


@pytest.mark.parametrize("change", ["bool", "negative", "large", "extra", "missing"])
def test_failure_counter_schema_cannot_emit_unbounded_or_arbitrary_diagnostics(change):
    value = counters()
    if change == "bool":
        value["effects"] = False
    elif change == "negative":
        value["effects"] = -1
    elif change == "large":
        value["effects"] = PROBE.COUNTER_LIMIT + 1
    elif change == "extra":
        value["private"] = "fixture key or source path"
    else:
        del value["effects"]
    refused = pytest.raises(PROBE.Refusal, PROBE.CaseRefusal, "correctKey", value)
    assert not isinstance(refused.value, PROBE.CaseFailure)


@pytest.mark.parametrize("case,host,increment", [
    ("wrongKey", PROBE.HOST, {"outerConnections": 1}),
    ("unknownTarget", PROBE.UNKNOWN_HOST, {}),
    ("unavailableProxy", PROBE.HOST, {}),
    ("missingNativeConfig", PROBE.HOST, {}),
])
def test_negative_case_requires_native_rejection_and_no_effects(case, host, increment):
    value = counters()
    native = simulated_case(value, increment, error=NativeHostError("not reflected"))
    result = asyncio.run(PROBE.run_case(native, provider(), value, host, False, case))
    assert result["directConnections"] == result["effects"] == result["forwardings"] == 0


@pytest.mark.parametrize("native_error", [None, NativeHostError("private-fixture-message"), RuntimeError("private-fixture-message")])
def test_missing_native_config_direct_contact_never_counts_as_safe_refusal(native_error):
    value = counters()
    native = simulated_case(value, {"directConnections": 1}, error=native_error)
    with pytest.raises(PROBE.DirectFallback, match="^missing-native-config-direct-contact$"):
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, False, "missingNativeConfig"))


def test_per_case_timeout_retains_direct_contact_failure_priority(monkeypatch):
    value = counters()
    monkeypatch.setattr(PROBE, "CASE_LIMIT", 0.01)
    native = simulated_case(value, {"directConnections": 1}, delay=1)
    with pytest.raises(PROBE.DirectFallback, match="^missing-native-config-direct-contact$"):
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, False, "missingNativeConfig"))


def test_timeout_without_direct_contact_is_a_failed_case_not_a_negative_success(monkeypatch):
    value = counters()
    monkeypatch.setattr(PROBE, "CASE_LIMIT", 0.01)
    native = simulated_case(value, delay=1)
    with pytest.raises(PROBE.CaseRefusal, match="^unavailable-proxy-refusal$"):
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, False, "unavailableProxy"))


@pytest.mark.parametrize("counter", ["effects", "forwardings", "innerConnections", "outerAuthentications"])
def test_negative_case_cannot_hide_remote_activity(counter):
    value = counters()
    native = simulated_case(value, {counter: 1}, error=NativeHostError("refused after effect"))
    with pytest.raises(PROBE.CaseRefusal):
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.UNKNOWN_HOST, False, "unknownTarget"))


def test_connection_close_failure_cannot_be_discarded_as_completed_cleanup():
    class Connection:
        def close(self):
            return None

        async def wait_closed(self):
            raise ValueError("private close detail")

    with pytest.raises(ValueError):
        asyncio.run(PROBE.close_connections({Connection()}))


def test_asyncssh_213_acceptor_uses_public_sockets_without_get_port():
    acceptor = types.SimpleNamespace(sockets=(types.SimpleNamespace(getsockname=lambda: (PROBE.INNER_ADDRESS, PROBE.INNER_PORT)),))
    assert not hasattr(acceptor, "get_port")
    PROBE.prove_listener(acceptor, PROBE.INNER_ADDRESS, PROBE.INNER_PORT)


@pytest.mark.parametrize("endpoint", [("0.0.0.0", 22), ("127.0.0.1", 2222), ("::1", 22, 0, 0), None])
def test_listener_must_bind_only_the_requested_fixed_loopback_endpoint(endpoint):
    sockets = () if endpoint is None else (types.SimpleNamespace(getsockname=lambda: endpoint),)
    with pytest.raises(PROBE.Refusal):
        PROBE.prove_listener(types.SimpleNamespace(sockets=sockets), PROBE.INNER_ADDRESS, PROBE.INNER_PORT)


def parser_fixture():
    calls = []
    initial = object()

    def options(**arguments):
        calls.append(arguments)
        if len(calls) == 1:
            return initial
        assert arguments["options"] is initial
        return types.SimpleNamespace(proxy_command=["/usr/bin/python3", "-I", PROBE.TUNNEL_ROOT + "/proxy.py", "0"],
                                     agent_path="", pkcs11_provider=None)

    return types.SimpleNamespace(SSHClientConnectionOptions=options), calls


def test_preflight_reconstructs_native_options_and_uses_only_bounded_config_dump(monkeypatch):
    ssh, options = parser_fixture()
    invocation = []
    fixed = ["/usr/bin/ssh", "-T", "-F", PROBE.TUNNEL_ROOT + "/outer_config", "-W", "127.0.0.1:22", "--", PROBE.ADDRESS]
    helper = types.SimpleNamespace(proxy_argv=lambda manifest, selector: fixed)

    def run(argv, **arguments):
        invocation.append((argv, arguments))
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr(PROBE.subprocess, "run", run)
    PROBE.parser_preflight(ssh, helper, {})
    assert options == [{"keepalive_interval": 2, "keepalive_count_max": 1},
                       {"options": options[1]["options"], "host": PROBE.ADDRESS, "username": PROBE.USER,
                        "client_keys": [PROBE.TUNNEL_ROOT + "/identity"], "known_hosts": None,
                        "config": [PROBE.TUNNEL_ROOT + "/native_config"], "preferred_auth": ["publickey"]}]
    assert invocation == [(["/usr/bin/ssh", "-G"] + fixed[1:],
                           {"stdin": subprocess.DEVNULL, "stdout": subprocess.DEVNULL, "stderr": subprocess.DEVNULL,
                            "timeout": 2, "env": {"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "HOME": "/"},
                            "cwd": "/", "check": False})]


def test_invalid_native_proxy_selection_refuses_before_any_openssh_process(monkeypatch):
    ssh = types.SimpleNamespace(SSHClientConnectionOptions=lambda **args: types.SimpleNamespace(proxy_command=None))
    monkeypatch.setattr(PROBE.subprocess, "run", lambda *args, **kwargs: pytest.fail("must not execute SSH"))
    with pytest.raises(PROBE.PhaseRefusal, match="^route-native-options-refusal$"):
        PROBE.parser_preflight(ssh, None, {})


@pytest.mark.parametrize("failure", ["exit", "timeout"])
def test_outer_configuration_parser_failure_is_closed_and_cannot_reach_native_connection(monkeypatch, failure):
    ssh = parser_fixture()[0]
    helper = types.SimpleNamespace(proxy_argv=lambda manifest, selector: ["/usr/bin/ssh", "-F", "fixed", PROBE.ADDRESS])

    def run(*args, **kwargs):
        if failure == "timeout":
            raise subprocess.TimeoutExpired("private native command", 2, output=b"private fixture material")
        return types.SimpleNamespace(returncode=255)

    monkeypatch.setattr(PROBE.subprocess, "run", run)
    with pytest.raises(PROBE.PhaseRefusal, match="^route-outer-config-refusal$"):
        PROBE.parser_preflight(ssh, helper, {})


@pytest.mark.parametrize("stderr,category", [(b"Load key /private/fixture: invalid format", "key"),
                                             (b"Permission denied for confidential-user", "permission"),
                                             (b"connect /private/endpoint: Connection refused", "network"),
                                             (b"REMOTE HOST IDENTIFICATION HAS CHANGED private", "host-key"),
                                             (b"private unrecognized vendor output", "unknown")])
def test_control_diagnostic_emits_only_allowlisted_category(stderr, category):
    assert PROBE.control_category(stderr) == category


def control_process(monkeypatch, program, startup_signal=False, proxy=False):
    actual = PROBE.subprocess.Popen
    children = []

    def spawn(argv, **arguments):
        assert argv[:4] == ["/usr/bin/python3", "-I", "-c", PROBE.CONTROL_EXEC]
        expected = ["/usr/bin/python3", "-I", PROBE.TUNNEL_ROOT + "/proxy.py", "0"] if proxy else ["/usr/bin/ssh", "-T", "fixed-fixture-argument"]
        assert argv[5:] == expected
        process = actual(argv[:5] + [sys.executable, "-I", "-c", program], **arguments)
        children.append(process)
        if startup_signal:
            os.kill(os.getpid(), signal.SIGALRM)
        return process

    monkeypatch.setattr(PROBE.subprocess, "Popen", spawn)
    helper = types.SimpleNamespace(proxy_argv=lambda manifest, selector: ["/usr/bin/ssh", "-T", "fixed-fixture-argument"])
    return helper, children


def assert_control_reaped(children):
    assert len(children) == 1 and children[0].returncode is not None
    with pytest.raises(ProcessLookupError):
        os.killpg(children[0].pid, 0)


def test_actual_control_launcher_restores_mask_and_closes_owned_process_group(monkeypatch):
    before = signal.pthread_sigmask(signal.SIG_BLOCK, (signal.SIGUSR1,))
    expected = sorted(int(value) for value in signal.pthread_sigmask(signal.SIG_BLOCK, ()))
    program = ("import signal,sys,time\nmask=sorted(int(value) for value in signal.pthread_sigmask(signal.SIG_BLOCK,()))\n"
               "assert mask == " + repr(expected) + "\nsys.stdout.buffer.write(b'SSH-2.0-AsyncSSH_fixture\\r\\n');sys.stdout.flush();time.sleep(20)\n")
    helper, children = control_process(monkeypatch, program)
    try:
        asyncio.run(PROBE.outer_control(helper, {}))
        assert_control_reaped(children)
        assert sorted(int(value) for value in signal.pthread_sigmask(signal.SIG_BLOCK, ())) == expected
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, before)


def test_failed_outer_control_is_fixed_and_killed_before_reap(monkeypatch):
    program = "import sys;sys.stderr.write('Load key /private/fixture-key: invalid format');sys.exit(255)"
    helper, children = control_process(monkeypatch, program)
    with pytest.raises(PROBE.PhaseRefusal, match="^route-outer-control-key-refusal$"):
        asyncio.run(PROBE.outer_control(helper, {}))
    assert_control_reaped(children)


@pytest.mark.parametrize("success", [True, False])
def test_proxy_entrypoint_control_uses_exact_frozen_command_and_closed_failure(monkeypatch, success):
    program = ("import sys,time;sys.stdout.buffer.write(b'SSH-2.0-AsyncSSH_fixture\\r\\n');sys.stdout.flush();time.sleep(20)"
               if success else "import sys;sys.stderr.write('private interpreter or bundle details');sys.exit(255)")
    helper, children = control_process(monkeypatch, program, proxy=True)
    if success:
        asyncio.run(PROBE.outer_control(helper, {}, proxy=True))
    else:
        with pytest.raises(PROBE.PhaseRefusal, match="^route-proxy-entrypoint-refusal$"):
            asyncio.run(PROBE.outer_control(helper, {}, proxy=True))
    assert_control_reaped(children)


def test_proxy_entrypoint_control_cannot_select_freeform_argv_or_selector(monkeypatch):
    monkeypatch.setattr(PROBE.subprocess, "Popen", lambda *args, **kwargs: pytest.fail("must not start a process"))
    with pytest.raises(PROBE.Refusal):
        asyncio.run(PROBE.outer_control(None, {}, proxy="arbitrary"))


@pytest.mark.parametrize("proxy", [False, True])
def test_valid_banner_cannot_hide_proxy_stderr_eof(monkeypatch, proxy):
    program = ("import os,sys,time;os.close(2);"
               "sys.stdout.buffer.write(b'SSH-2.0-AsyncSSH_fixture\\r\\n');sys.stdout.flush();time.sleep(20)")
    helper, children = control_process(monkeypatch, program, proxy=proxy)
    reason = "proxy-entrypoint" if proxy else "outer-control-unknown"
    with pytest.raises(PROBE.PhaseRefusal, match="^route-" + reason + "-refusal$"):
        asyncio.run(PROBE.outer_control(helper, {}, proxy=proxy))
    assert_control_reaped(children)


def test_control_timeout_never_leaves_the_outer_process_running(monkeypatch):
    monkeypatch.setattr(PROBE, "CONTROL_LIMIT", 0.05)
    helper, children = control_process(monkeypatch, "import time;time.sleep(20)")
    with pytest.raises(PROBE.PhaseRefusal, match="^route-outer-control-unknown-refusal$"):
        asyncio.run(PROBE.outer_control(helper, {}))
    assert_control_reaped(children)


def test_control_output_limit_is_bounded_and_disclosed_only_as_fixed_refusal(monkeypatch):
    helper, children = control_process(monkeypatch, "import sys,time;sys.stderr.write('x'*10000);sys.stderr.flush();time.sleep(20)")
    with pytest.raises(PROBE.Refusal, match="^route-refusal$"):
        asyncio.run(PROBE.outer_control(helper, {}))
    assert_control_reaped(children)


def test_fast_control_leader_exit_kills_inherited_delayed_descendant(monkeypatch, tmp_path):
    marker = tmp_path / "unexpected-child-effect"
    program = ("import os,signal,sys,time\nready,done=os.pipe()\npid=os.fork()\nif pid==0:\n"
               " os.close(1);os.close(2);os.close(ready);os.write(done,b'x');os.close(done);"
               "signal.signal(signal.SIGTERM,signal.SIG_IGN);time.sleep(.2);open(" + repr(str(marker)) + ",'w').write('late')\n"
               "else:\n os.close(done);os.read(ready,1);os.close(2);sys.stdout.buffer.write(b'SSH-2.0-AsyncSSH_fixture\\r\\n');sys.stdout.flush()\n")
    process_class = PROBE.subprocess.Popen
    original_kill, original_wait = PROBE.os.killpg, process_class.wait
    events = []

    def kill(pid, value):
        assert "wait" not in events
        events.append("kill")
        return original_kill(pid, value)

    def wait(process, timeout=None):
        assert events == ["kill"] and timeout == PROBE.CLOSE_LIMIT
        events.append("wait")
        return original_wait(process, timeout=timeout)

    monkeypatch.setattr(PROBE.os, "killpg", kill)
    monkeypatch.setattr(process_class, "wait", wait)
    helper, children = control_process(monkeypatch, program)
    with pytest.raises(PROBE.PhaseRefusal, match="^route-outer-control-unknown-refusal$"):
        asyncio.run(PROBE.outer_control(helper, {}))
    assert events == ["kill", "wait"]
    assert len(children) == 1 and children[0].returncode is not None
    # A killed orphan can remain a zombie in this group until PID 1 reaps it.
    # Assert no live delayed effect instead of demanding instant disappearance.
    time.sleep(0.25)
    assert not marker.exists()


def test_control_startup_alarm_cannot_escape_registration_cleanup(monkeypatch):
    before = signal.pthread_sigmask(signal.SIG_BLOCK, ())

    def alarm(*_arguments):
        raise PROBE.Refusal("route-refusal")

    previous = signal.signal(signal.SIGALRM, alarm)
    helper, children = control_process(monkeypatch, "import time;time.sleep(20)", startup_signal=True)
    try:
        with pytest.raises(PROBE.Refusal):
            asyncio.run(PROBE.outer_control(helper, {}))
        assert_control_reaped(children)
        assert signal.pthread_sigmask(signal.SIG_BLOCK, ()) == before
    finally:
        signal.signal(signal.SIGALRM, previous)
        signal.pthread_sigmask(signal.SIG_SETMASK, before)


def test_control_counters_are_reset_only_after_connections_close_without_late_effects(monkeypatch):
    value = dict(counters(), innerConnections=1)
    closed = []

    async def close(connections):
        closed.append(True)

    monkeypatch.setattr(PROBE, "close_connections", close)
    asyncio.run(PROBE.seal_control(value, set(), dict(value)))
    assert closed == [True] and value == counters()

    async def late(connections):
        value["directConnections"] += 1

    monkeypatch.setattr(PROBE, "close_connections", late)
    with pytest.raises(PROBE.PhaseRefusal, match="^route-control-cleanup-refusal$"):
        asyncio.run(PROBE.seal_control(value, set(), counters()))
    assert value["directConnections"] == 1


@pytest.mark.parametrize("stage", PROBE.PHASES)
def test_fixed_stage_diagnosis_never_exposes_exception_text(stage):
    with pytest.raises(PROBE.PhaseRefusal, match="^route-" + stage + "-refusal$"):
        with PROBE.phase(stage):
            raise ValueError("private fixture and source details")


@pytest.mark.parametrize("late_error", [False, True])
def test_direct_contact_during_native_cleanup_cannot_escape_case_snapshot(monkeypatch, late_error):
    value = counters()
    native = simulated_case(value, error=NativeHostError("native rejected"))

    async def close(*args):
        value["directConnections"] += 1
        if late_error:
            raise ValueError("private cleanup detail")

    monkeypatch.setattr(PROBE, "close_native", close)
    with pytest.raises(PROBE.DirectFallback, match="^missing-native-config-direct-contact$"):
        asyncio.run(PROBE.run_case(native, provider(), value, PROBE.HOST, False, "missingNativeConfig", set()))


@pytest.mark.parametrize("late_counter", ["directConnections", "effects"])
def test_final_listener_cleanup_cannot_add_unreported_connections_or_effects(monkeypatch, late_counter):
    value = counters()

    class Listener:
        def close(self):
            value[late_counter] += 1

        async def wait_closed(self):
            return None

    async def close(*args):
        return None

    monkeypatch.setattr(PROBE, "close_native", close)
    expected = PROBE.DirectFallback if late_counter == "directConnections" else PROBE.CaseRefusal
    with pytest.raises(expected):
        asyncio.run(PROBE.finalize_cases(None, set(), [Listener()], value, "missingNativeConfig"))


@pytest.mark.parametrize("reason", [None, "missingNativeConfig", "wrongKey"])
def test_actual_entrypoint_suppresses_vendor_text_and_preserves_fixed_failure(reason):
    source = Path(PROBE.__file__).read_text().replace('if __name__ == "__main__":\n    sys.exit(main())', "")
    exception = "raise RuntimeError('private-fixture-message')" if reason is None else "raise DirectFallback(" + repr(reason) + ")"
    source += "\ndef execute(arguments):\n    print('private-fixture-message')\n    " + exception + "\nsys.exit(main(['exercise']))\n"
    result = subprocess.run([sys.executable, "-I", "-c", source], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 1 and result.stderr == b""
    value = json.loads(result.stdout)
    expected = "route-refusal" if reason is None else PROBE.CASE_NAMES[reason] + "-direct-contact"
    assert value == {"schema": PROBE.SCHEMA, "ok": False, "reason": expected}
    assert b"private-fixture-message" not in result.stdout


def test_actual_entrypoint_preserves_only_allowlisted_stage_failure():
    source = Path(PROBE.__file__).read_text().replace('if __name__ == "__main__":\n    sys.exit(main())', "")
    source += "\ndef execute(arguments):\n    with phase('bundle'):\n        raise OSError('private fixture path')\nsys.exit(main(['exercise']))\n"
    result = subprocess.run([sys.executable, "-I", "-c", source], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 1 and result.stderr == b""
    assert json.loads(result.stdout) == {"schema": PROBE.SCHEMA, "ok": False, "reason": "route-bundle-refusal"}


def test_actual_entrypoint_publishes_only_fixed_failed_case_and_six_integer_counters():
    source = Path(PROBE.__file__).read_text().replace('if __name__ == "__main__":\n    sys.exit(main())', "")
    source += ("\ndef execute(arguments):\n"
               "    raise CaseRefusal('correctKey', {name: int(name == 'outerConnections') for name in COUNTERS})\n"
               "sys.exit(main(['exercise']))\n")
    result = subprocess.run([sys.executable, "-I", "-c", source], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 1 and result.stderr == b""
    assert json.loads(result.stdout) == {
        "schema": PROBE.SCHEMA, "ok": False, "reason": "correct-key-refusal", "case": "correctKey",
        "counters": dict(counters(), outerConnections=1), "errorClass": "none",
    }


@pytest.mark.parametrize("arguments", [[], ["exercise", "path"], ["arbitrary"], ["--help"]])
def test_only_two_fixed_actions_are_accepted(arguments):
    with pytest.raises(PROBE.Refusal):
        PROBE.execute(arguments)

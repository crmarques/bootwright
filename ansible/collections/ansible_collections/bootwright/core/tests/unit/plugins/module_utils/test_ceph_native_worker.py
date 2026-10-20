"""The shipped native worker bounds sources, output and inherited processes."""

from __future__ import annotations

import inspect
import io
import json
import os
import re
import signal
import subprocess
import sys
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_host_worker, ceph_native_worker, native_diagnostics, native_specializations


def source_bundle():
    return {"native_diagnostics": inspect.getsource(native_diagnostics), "native_specializations": inspect.getsource(native_specializations),
            "ceph_pull_broker": '"""Fixed fixture."""\n',
            "ceph_tunnel": '"""Fixed fixture."""\n', "ceph_tunnel_staging": '"""Fixed fixture."""\n', "ceph_tunnel_custody": '"""Fixed fixture."""\n',
            "ceph_native_observe": '"""Fixed fixture."""\n', "ceph_native_resources": '"""Fixed fixture."""\n',
            "ceph_native_nfs": '"""Fixed fixture."""\n', "ceph_native_observability": '"""Fixed fixture."""\n',
            "ceph_native_units": '"""Fixed fixture."""\n',
            "ceph_native_object": '"""Fixed fixture."""\n',
            "ceph_host_route_probe": '"""Fixed fixture."""\n', "ceph_host_runtime": '"""Fixed fixture."""\n',
            "ceph_native": ('class Refusal(ValueError): pass\nCOMMAND_SESSION = True\ndef dispatch(value):\n'
                            '    return {"session": COMMAND_SESSION, "source": bool(_BOOTWRIGHT_SOURCE)}\n')}


def test_native_sources_and_names_are_closed_before_any_module_load():
    bundle = source_bundle()
    for invalid in ({}, dict(bundle, arbitrary='raise RuntimeError("no")'),
                    dict(bundle, ceph_tunnel=""), dict(bundle, ceph_native=None),
                    dict(bundle, ceph_native="a" * (ceph_native_worker.MAX_SOURCE + 1))):
        with pytest.raises(ValueError, match="^fixed native worker$"):
            ceph_native_worker.load(invalid)


def test_specialization_registry_ships_in_the_closed_bundle_after_diagnostics():
    names = ceph_native_worker.MODULES
    assert names.index("native_specializations") == names.index("native_diagnostics") + 1
    sources = ceph_core.Transport.native_sources()
    assert tuple(sources) == names and sources["native_specializations"] == inspect.getsource(native_specializations)


def test_isolated_loader_binds_fixed_source_and_keeps_commands_in_owned_group():
    fixture = inspect.getsource(ceph_native_worker)
    fixture = fixture.replace('if __name__ == "__main__":\n    main()', '')
    fixture += '\nmodule=load(json.loads(sys.stdin.read()))\nprint(json.dumps(module.dispatch({})))\n'
    result = subprocess.run([sys.executable, "-I", "-c", fixture], input=json.dumps(source_bundle()).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"session": False, "source": True}
    assert result.stderr == b""


@pytest.mark.parametrize("value", [None, {"ok": 1, "result": {}}, {"ok": True, "result": [], "private": "secret"},
                                   {"ok": False, "reason": "vendor secret output"}, {"ok": True, "result": {}, "extra": "secret"}])
def test_result_shape_cannot_disclose_arbitrary_failure_or_extra_fields(value):
    with pytest.raises(ValueError, match="^fixed native worker$"):
        ceph_native_worker.result_bytes(value)


def test_result_byte_limit_is_an_aggregate_bound():
    with pytest.raises(ValueError):
        ceph_native_worker.result_bytes({"ok": True, "result": {"data": "a" * ceph_native_worker.MAX_RESULT}})


def test_parent_drains_a_result_larger_than_a_pipe_without_deadlock():
    result = {"ok": True, "result": {"data": "a" * (256 * 1024)}}
    assert ceph_native_worker.supervise(lambda: result, budget=2, grace=0) == result


def test_fixed_worker_publication_drains_large_result_completely():
    sources = source_bundle()
    sources["ceph_native"] = "class Refusal(ValueError): pass\ndef dispatch(value):\n    return {'data': 'a' * (256 * 1024)}\n"
    payload = {"sources": sources, "payload": {}, "budget": 2}
    result = subprocess.run([sys.executable, "-I", "-c", inspect.getsource(ceph_native_worker)], input=json.dumps(payload).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=4, check=False)
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"ok": True, "result": {"data": "a" * (256 * 1024)}}
    assert result.stderr == b""


def test_blocked_result_reader_cannot_leave_a_worker_waiting_indefinitely():
    sources = source_bundle()
    sources["ceph_native"] = "class Refusal(ValueError): pass\ndef dispatch(value):\n    return {'data': 'a' * (256 * 1024)}\n"
    payload = {"sources": sources, "payload": {}, "budget": 2}
    process = subprocess.Popen([sys.executable, "-I", "-c", inspect.getsource(ceph_native_worker)],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        process.stdin.write(json.dumps(payload).encode())
        process.stdin.close()
        assert process.wait(timeout=4) == 0
        # Deliberately consume only after exit: a full pipe cannot block cleanup.
        assert len(process.stdout.read()) < 256 * 1024
        assert process.stderr.read() == b""
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=1)
        process.stdout.close()
        process.stderr.close()


def run_work(payload, started_ago=0, ceiling=ceph_native_worker.REMOTE_LIMIT):
    def worker():
        sys.stdin = io.TextIOWrapper(io.BytesIO(json.dumps(payload).encode()))
        return ceph_native_worker.work(time.monotonic() - started_ago, ceiling)
    return ceph_native_worker.supervise(worker, budget=2, grace=0)


def unknown(result):
    """An unexpected worker failure names only its exception type and code site."""
    return set(result) == {"ok", "reason", "cause"} and result["ok"] is False and result["reason"] == "native-unknown" \
        and result["cause"].startswith("ValueError@ceph_native_worker.") and ceph_native_worker.valid_cause(result["cause"])


def test_work_accepts_only_a_bounded_controller_allowance():
    payload = {"sources": source_bundle(), "payload": {}, "budget": 1}
    assert run_work(payload) == {"ok": True, "result": {"session": False, "source": True}}
    for value in (True, False, 0, -1, 641, "10", None, float("nan"), float("inf")):
        payload["budget"] = value
        assert unknown(run_work(payload))


def test_envelope_cannot_raise_the_argv_ceiling_before_source_loading(tmp_path):
    effect = tmp_path / "effect"
    sources = source_bundle()
    sources["ceph_tunnel"] = "open(" + repr(str(effect)) + ", 'w').write('unexpected load')\n"
    payload = {"sources": sources, "payload": {}, "budget": 1}
    assert unknown(run_work(payload, ceiling=0.5))
    assert not effect.exists()


def test_partial_payload_with_stdin_open_is_stopped_by_argv_ceiling(tmp_path):
    effect = tmp_path / "effect"
    sources = source_bundle()
    sources["ceph_tunnel"] = "open(" + repr(str(effect)) + ", 'w').write('unexpected load')\n"
    payload = {"sources": sources, "payload": {}, "budget": 0.1}
    process = subprocess.Popen([sys.executable, "-I", "-c", inspect.getsource(ceph_native_worker), "0.1"],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    started = time.monotonic()
    try:
        process.stdin.write(json.dumps(payload).encode()[:-1])
        process.stdin.flush()
        assert process.wait(timeout=4) == 0
        assert time.monotonic() - started < 3
        assert json.loads(process.stdout.read()) == {"ok": False, "reason": "native-timeout"}
        assert process.stderr.read() == b""
        assert not effect.exists()
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=1)
        process.stdin.close()
        process.stdout.close()
        process.stderr.close()


def test_real_main_stops_term_ignoring_descendant_before_delayed_effect(tmp_path):
    effect = tmp_path / "effect"
    sources = source_bundle()
    sources["ceph_native"] = '''import os, signal, time
class Refusal(ValueError): pass
def dispatch(value):
    ready, notify = os.pipe()
    child = os.fork()
    if not child:
        os.close(ready)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        for descriptor in (0, 1, 2):
            os.close(descriptor)
        os.write(notify, b"ready")
        os.close(notify)
        time.sleep(0.3)
        open(value["effect"], "w").write("unexpected descendant effect")
        os._exit(0)
    os.close(notify)
    assert os.read(ready, 5) == b"ready"
    os.close(ready)
    return {"ready": True}
'''
    payload = {"sources": sources, "payload": {"effect": str(effect)}, "budget": 2}
    result = subprocess.run([sys.executable, "-I", "-c", inspect.getsource(ceph_native_worker), "2"],
                            input=json.dumps(payload).encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=4, check=False)
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"ok": True, "result": {"ready": True}}
    assert result.stderr == b""
    time.sleep(0.4)
    assert not effect.exists()


@pytest.mark.parametrize("arguments", [["0"], ["-1"], ["641"], ["nan"], ["inf"], ["1", "2"],
                                       ["x"], ["9" * 33], ["１"]])
def test_argv_cannot_remove_or_raise_the_aggregate_ceiling(arguments):
    with pytest.raises(ValueError):
        ceph_native_worker.execution_budget(arguments)


def test_fixed_argv_allowance_defaults_to_remote_ceiling_and_accepts_short_calls():
    assert ceph_native_worker.execution_budget([]) == ceph_native_worker.REMOTE_LIMIT
    assert ceph_native_worker.execution_budget(["0.125"]) == 0.125


def test_slow_payload_never_loads_sources_or_executes_after_its_allowance(tmp_path):
    effect = tmp_path / "effect"
    sources = source_bundle()
    sources["ceph_tunnel"] = "open(" + repr(str(effect)) + ", 'w').write('unexpected load')\n"
    payload = {"sources": sources, "payload": {}, "budget": 0.1}
    assert run_work(payload, started_ago=0.2) == {"ok": False, "reason": "native-timeout"}
    assert not effect.exists()


def test_smaller_allowance_interrupts_native_execution_without_transcript():
    sources = source_bundle()
    sources["ceph_native"] = "import time\nclass Refusal(ValueError): pass\ndef dispatch(value):\n    time.sleep(1)\n    return {}\n"
    payload = {"sources": sources, "payload": {}, "budget": 0.1}
    started = time.monotonic()
    assert run_work(payload) == {"ok": False, "reason": "native-timeout"}
    assert time.monotonic() - started < 0.8


def delayed_effect(effect, delay=0.4):
    child = os.fork()
    if child == 0:
        for sig in ceph_native_worker.INTERRUPTS:
            signal.signal(sig, signal.SIG_IGN)
        time.sleep(delay)
        effect.write_text("unwanted descendant effect")
        os._exit(0)


def test_natural_worker_exit_stops_descendant_before_releasing_pid(tmp_path):
    effect = tmp_path / "effect"

    def finished_worker():
        delayed_effect(effect)
        return {"ok": True, "result": {"ready": True}}

    assert ceph_native_worker.supervise(finished_worker, budget=1, grace=0.05) == {"ok": True, "result": {"ready": True}}
    time.sleep(0.5)
    assert not effect.exists()


def test_remote_deadline_stops_worker_and_inherited_delayed_effect(tmp_path):
    effect = tmp_path / "effect"

    def stalled_worker():
        delayed_effect(effect)
        time.sleep(2)
        return {"ok": True, "result": {}}

    started = time.monotonic()
    assert ceph_native_worker.supervise(stalled_worker, budget=0.15, grace=0.05) == {"ok": False, "reason": "native-timeout"}
    assert time.monotonic() - started < 1
    time.sleep(0.5)
    assert not effect.exists()


@pytest.mark.parametrize("interrupt", [signal.SIGHUP, signal.SIGTERM, signal.SIGINT])
def test_pending_startup_signal_cannot_leave_unsupervised_worker(tmp_path, interrupt):
    effect = tmp_path / "effect"
    fixture = '''
import json, os, signal, sys, time
namespace = {"__name__": "fixed_worker_fixture"}
exec(compile(sys.stdin.read(), "<fixed-worker-fixture>", "exec"), namespace)
original = os.fork
def interrupted_fork():
    child = original()
    if child:
        os.kill(os.getpid(), int(sys.argv[2]))
    return child
os.fork = interrupted_fork
def worker():
    time.sleep(0.4)
    open(sys.argv[1], "w").write("unexpected effect")
    return {"ok": True, "result": {}}
print(json.dumps(namespace["supervise"](worker, budget=1, grace=0.05)))
'''
    result = subprocess.run([sys.executable, "-I", "-c", fixture, str(effect), str(int(interrupt))],
                            input=inspect.getsource(ceph_native_worker).encode(), stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"ok": False, "reason": "native-timeout"}
    assert result.stderr == b""
    time.sleep(0.5)
    assert not effect.exists()


def test_waitid_observes_without_reaping(monkeypatch):
    calls = []
    monkeypatch.setattr(ceph_native_worker.os, "waitid", lambda *args: calls.append(args))
    assert ceph_native_worker.child_status(123) is None
    assert calls == [(os.P_PID, 123, os.WEXITED | os.WNOHANG | os.WNOWAIT)]


def test_stop_kills_group_before_the_only_bounded_reap(monkeypatch):
    calls = []
    monkeypatch.setattr(ceph_native_worker.os, "killpg", lambda *args: calls.append(("group",) + args))
    monkeypatch.setattr(ceph_native_worker.os, "waitpid", lambda *args: calls.append(("reap",) + args))
    monkeypatch.setattr(ceph_native_worker.time, "sleep", lambda *args: calls.append(("grace",) + args))
    ceph_native_worker.stop(123, 0.1)
    assert calls == [("group", 123, signal.SIGTERM), ("grace", 0.1), ("group", 123, signal.SIGKILL), ("reap", 123, os.WNOHANG)]


def test_child_before_setsid_is_stopped_by_reserved_pid(monkeypatch):
    calls = []

    def missing_group(*args):
        raise ProcessLookupError()

    monkeypatch.setattr(ceph_native_worker.os, "killpg", missing_group)
    monkeypatch.setattr(ceph_native_worker.os, "kill", lambda *args: calls.append(args))
    monkeypatch.setattr(ceph_native_worker.os, "waitpid", lambda *args: None)
    ceph_native_worker.stop(123, 0)
    assert calls == [(123, signal.SIGTERM), (123, signal.SIGKILL)]


@pytest.mark.parametrize("budget,grace", [(True, 0), (0, 0), (641, 0), (1, 2), (1, -1)])
def test_budget_never_bypasses_remote_or_termination_ceiling(budget, grace):
    with pytest.raises(ValueError):
        ceph_native_worker.supervise(lambda: {}, budget=budget, grace=grace)


def test_a_refusal_cause_names_the_shipped_frame_that_required_it_or_stays_bare():
    namespace = {}
    exec(compile("class Refusal(ValueError): pass\ndef require(ok, reason):\n    if not ok:\n        raise Refusal(reason)\n"
                 "def member(reason):\n    require(False, reason)\n", "<bootwright-fixed-ceph_package_sources>", "exec"), namespace)
    causes = []
    for reason in ("packages", "a" * 41):
        with pytest.raises(ValueError) as raised:
            namespace["member"](reason)
        causes.append(ceph_native_worker.cause(raised.value))
    assert causes == ["packages@ceph_package_sources.member:6", "a" * 41]
    assert ceph_native_worker.cause(namespace["Refusal"]("packages")) == "packages"
    with pytest.raises(ValueError) as raised:
        namespace["require"](False, "packages")
    assert ceph_native_worker.cause(raised.value) == "packages"
    assert ceph_native_worker.valid_cause(causes[0]) and ceph_native_worker.refusal_cause(causes[0])


def test_failure_cause_is_a_closed_reason_or_a_code_site_never_a_value():
    sources = source_bundle()
    sources["ceph_native"] = ("class Refusal(ValueError): pass\ndef dispatch(value):\n"
                              "    if value['kind'] == 'refusal':\n        raise Refusal('package-limit')\n"
                              "    if value['kind'] == 'valued':\n        raise Refusal('synthetic secret value')\n"
                              "    raise OSError('synthetic-secret')\n")
    results = {kind: run_work({"sources": sources, "payload": {"kind": kind}, "budget": 1}) for kind in ("refusal", "valued", "error")}
    assert results["refusal"] == {"ok": False, "reason": "native-refusal", "cause": "package-limit@ceph_native.dispatch:4"}
    assert results["valued"]["reason"] == "native-refusal" and re.fullmatch(r"Refusal@ceph_native\.dispatch:[0-9]+", results["valued"]["cause"])
    assert results["error"]["reason"] == "native-unknown" and re.fullmatch(r"OSError@ceph_native\.dispatch:[0-9]+", results["error"]["cause"])
    assert all("secret" not in json.dumps(value) for value in results.values())
    for value in (dict(results["error"], cause="secret value"), dict(results["error"], cause="OSError@ceph_native.dispatch"), dict(results["error"], cause="")):
        with pytest.raises(ValueError, match="^fixed native worker$"):
            ceph_native_worker.result_bytes(value)


@pytest.mark.parametrize("phase", ["host_observe", "profile", "bootstrap"])
def test_clean_supervisor_preserves_redacted_native_failure_through_each_launcher(phase):
    sources = ceph_core.Transport.native_sources()
    sources["ceph_native"] += (
        "\ndef dispatch(value):\n"
        "    raise NativeFailure('image-pull', 'registry.example.test/ceph:authored', "
        "'pull refused password=synthetic-secret known-private', secrets=('known-private',))\n"
    )
    supervisor = inspect.getsource(ceph_native_worker)
    host = inspect.getsource(ceph_host_worker)
    if phase == "profile":
        source = ceph_host_worker.launcher(supervisor, host)
    elif phase == "bootstrap":
        source = ceph_core.bootstrap_launcher(supervisor, host)
    else:
        source = ceph_native_worker.launcher(supervisor) + "s['main']()"
    payload = {"sources": sources, "payload": {"phase": phase}, "budget": 3}
    result = subprocess.run([sys.executable, "-I", "-c", source, "3"], input=json.dumps(payload).encode(),
                            capture_output=True, timeout=10, check=False)
    assert result.returncode == 0 and result.stderr == b""
    value = json.loads(result.stdout)
    assert value["ok"] is False and value["reason"] == "native-refusal"
    assert value["cause"].startswith("image-pull@ceph_native.dispatch:")
    assert value["nativeError"] == {"coordinate": "registry.example.test/ceph:authored",
                                    "cause": "pull refused password=[redacted] [redacted]"}
    assert b"synthetic-secret" not in result.stdout and b"known-private" not in result.stdout


@pytest.mark.parametrize("malformed", [
    {"coordinate": "selected", "cause": "native failure"}, {}, {"coordinate": "selected", "cause": ""},
    {"coordinate": "selected", "cause": "line\nbreak"},
    {"coordinate": "selected", "cause": "x" * (native_diagnostics.MAX_CAUSE + 1)},
    {"coordinate": "selected", "cause": "error", "extra": "private"},
])
def test_clean_parent_rejects_malformed_diagnostics_even_when_child_sends_them(malformed):
    source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker))
    source += """
import json

received = []
validate = s['result_bytes']
child_work = s['child_work']

def parent_validate(value):
    received.append(value)
    return validate(value)

def raw_child(*args):
    # This runs after fork, before child_work resolves its serialization callable.
    s['result_bytes'] = lambda value: json.dumps(value).encode()
    child_work(*args)

s['result_bytes'] = parent_validate
s['child_work'] = raw_child
value = {'ok': False, 'reason': 'native-refusal', 'nativeError': """ + repr(malformed) + """}
result = s['supervise'](lambda: value, budget=3, grace=0)
print(json.dumps({'result': result, 'received': received}))
"""
    result = subprocess.run([sys.executable, "-I", "-c", source], capture_output=True, timeout=10, check=False)
    assert result.returncode == 0 and result.stderr == b""
    observed = json.loads(result.stdout)
    transmitted = {"ok": False, "reason": "native-refusal", "nativeError": malformed}
    assert observed["received"] == [transmitted]
    expected = transmitted if native_diagnostics.valid(malformed) else {"ok": False, "reason": "native-unknown"}
    assert observed["result"] == expected

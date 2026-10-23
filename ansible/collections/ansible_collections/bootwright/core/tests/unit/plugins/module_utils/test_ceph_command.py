"""Native command cancellation owns its group until every effect is stopped."""

from __future__ import annotations

import inspect
import json
import os
import signal
import subprocess
import sys
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_worker, native_diagnostics


DESCENDANT = '''
import os, signal, sys, time
child = os.fork()
if child == 0:
    for fd in (0, 1, 2):
        os.close(fd)
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    time.sleep(0.35)
    open(sys.argv[1], "w").write("unexpected descendant effect")
    os._exit(0)
'''


def command(source, *args, **options):
    return ceph_native.bounded_command([sys.executable, "-I", "-c", source, *map(str, args)], **options)


def test_natural_command_exit_stops_closed_pipe_descendant_before_reaping(tmp_path, monkeypatch):
    effect = tmp_path / "effect"
    events = []
    wait, kill = subprocess.Popen.wait, os.killpg

    def observed_wait(process, *args, **kwargs):
        events.append(("wait", kwargs.get("timeout")))
        return wait(process, *args, **kwargs)

    def observed_kill(*args):
        events.append(("kill", args[1]))
        return kill(*args)

    monkeypatch.setattr(subprocess.Popen, "wait", observed_wait)
    monkeypatch.setattr(ceph_native.os, "killpg", observed_kill)
    assert command(DESCENDANT + '\nos.write(1, b"complete")\n', effect, timeout=1) == (0, b"complete")
    assert events == [("kill", signal.SIGKILL), ("wait", 1)]
    time.sleep(0.4)
    assert not effect.exists()


def test_command_timeout_stops_leader_and_descendant_without_transcript(tmp_path):
    effect = tmp_path / "effect"
    started = time.monotonic()
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        command(DESCENDANT + '\nsys.stderr.write("private transcript"); sys.stderr.flush(); time.sleep(2)\n',
                effect, timeout=0.08)
    assert time.monotonic() - started < 1.5
    time.sleep(0.4)
    assert not effect.exists()


@pytest.mark.parametrize("descriptor", [1, 2])
def test_aggregate_output_limit_stops_descendant_on_either_pipe(tmp_path, descriptor):
    effect = tmp_path / "effect"
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        command(DESCENDANT + '\nos.write(' + str(descriptor) + ', b"private transcript" * 2048); time.sleep(2)\n',
                effect, timeout=1, limit=1024)
    time.sleep(0.4)
    assert not effect.exists()


def test_early_child_signal_is_returned_without_reap_before_group_cleanup():
    assert command("import os, signal; os.kill(os.getpid(), signal.SIGTERM)", timeout=1) == (-signal.SIGTERM, b"")


def test_large_stdin_is_drained_without_deadlocking_native_output():
    payload = b"bounded-private-input" * 8192
    source = "import sys; data=sys.stdin.buffer.read(); sys.stdout.write(str(len(data)))"
    assert command(source, data=payload, timeout=2) == (0, str(len(payload)).encode())


def test_closed_pipes_do_not_let_a_live_command_outlive_deadline():
    source = "import os, time; os.close(0); os.close(1); os.close(2); time.sleep(2)"
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        command(source, timeout=0.08)


def test_spawn_errors_never_disclose_executable_or_exception(tmp_path):
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        ceph_native.bounded_command([str(tmp_path / "private-missing-executable")])


def test_remote_command_never_signals_the_supervisors_shared_group(monkeypatch):
    monkeypatch.setattr(ceph_native, "COMMAND_SESSION", False)
    monkeypatch.setattr(ceph_native.os, "killpg", lambda *args: pytest.fail("command killed its supervisor group"))
    assert command('print("complete")', timeout=1) == (0, b"complete\n")
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        command("import time; time.sleep(2)", timeout=0.08)


def test_actual_shipped_command_works_inside_supervised_remote_group(tmp_path):
    effect = tmp_path / "effect"
    invocation = [sys.executable, "-I", "-c", DESCENDANT, str(effect)]
    sources = {"native_diagnostics": inspect.getsource(native_diagnostics), "ceph_pull_broker": '"""Fixed fixture."""\n',
               "ceph_tunnel": '"""Fixed fixture."""\n', "ceph_tunnel_staging": '"""Fixed fixture."""\n', "ceph_tunnel_custody": '"""Fixed fixture."""\n',
               "ceph_native_observe": '"""Fixed fixture."""\n', "ceph_host_route_probe": '"""Fixed fixture."""\n',
               "physical_machine_identity": '"""Fixed fixture."""\n',
               "ceph_host_runtime": '"""Fixed fixture."""\n', "native_specializations": '"""Fixed fixture."""\n',
               "ceph_native_resources": '"""Fixed fixture."""\n',
               "ceph_native_nvmeof": '"""Fixed fixture."""\n', "ceph_native_nfs": '"""Fixed fixture."""\n',
               "ceph_native_observability": '"""Fixed fixture."""\n', "ceph_native_units": '"""Fixed fixture."""\n',
               "ceph_native_object": '"""Fixed fixture."""\n', "ceph_smb_native": '"""Fixed fixture."""\n', "ceph_replication_native": '"""Fixed fixture."""\n',
               "ceph_native": inspect.getsource(ceph_native) +
               '\ndef dispatch(value):\n    code, output = bounded_command(' + repr(invocation) +
               ', timeout=1)\n    return {"code": code, "session": COMMAND_SESSION}\n'}
    payload = {"sources": sources, "payload": {}, "budget": 2}
    result = subprocess.run([sys.executable, "-I", "-c", inspect.getsource(ceph_native_worker), "2"],
                            input=json.dumps(payload).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3, check=False)
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"ok": True, "result": {"code": 0, "session": False}}
    assert result.stderr == b""
    time.sleep(0.4)
    assert not effect.exists()

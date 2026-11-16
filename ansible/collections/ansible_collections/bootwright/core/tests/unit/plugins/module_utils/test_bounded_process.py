"""The one bounded runner returns a process's output and stops it and its descendants within the budget."""

from __future__ import annotations

import os
import signal
import sys
import threading
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process


ENVIRONMENT = {"PATH": "/usr/bin:/bin"}


def run(source, *args, **options):
    options.setdefault("environment", ENVIRONMENT)
    return bounded_process.run([sys.executable, "-I", "-c", source, *map(str, args)], **options)


def test_returns_the_exit_code_and_both_streams():
    code, out, err = run("import sys; sys.stdout.write('out'); sys.stderr.write('err'); sys.exit(3)")
    assert (code, out, err) == (3, b"out", b"err")


def test_feeds_standard_input_and_a_closed_input_still_finishes():
    assert run("import sys; sys.stdout.write(sys.stdin.read().upper())", data=b"private bytes").stdout == b"PRIVATE BYTES"
    assert run("import sys; sys.stdin.close(); print('done')", data=b"x" * (1 << 20)).stdout == b"done\n"


def test_discarded_standard_error_is_not_captured():
    assert run("import sys; print('a'); sys.stderr.write('b')", capture_stderr=False) == (0, b"a\n", b"")


@pytest.mark.parametrize("stream", ["stdout", "stderr"])
def test_output_past_the_limit_is_refused_across_both_streams(stream):
    with pytest.raises(bounded_process.Failure) as refused:
        run("import sys; sys.%s.write('x' * 8192)" % stream, limit=128)
    assert refused.value.kind == "output"
    with pytest.raises(bounded_process.Failure) as split:
        run("import sys; sys.stdout.write('x' * 100); sys.stderr.write('y' * 100)", limit=150)
    assert split.value.kind == "output"


def test_a_process_past_its_time_is_killed_with_its_descendants(tmp_path):
    marker = tmp_path / "pid"
    source = ("import os, sys, time\n"
              "child = os.fork()\n"
              "if child == 0:\n"
              "    time.sleep(30)\n"
              "else:\n"
              "    open(sys.argv[1], 'w').write(str(child))\n"
              "    time.sleep(30)\n")
    started = time.monotonic()
    with pytest.raises(bounded_process.Failure) as refused:
        run(source, marker, timeout=1.5)
    assert refused.value.kind == "timeout" and time.monotonic() - started < 8
    descendant = int(marker.read_text())
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline:
        try:
            os.kill(descendant, 0)
        except ProcessLookupError:
            return
        time.sleep(0.05)
    os.kill(descendant, signal.SIGKILL)
    pytest.fail("the descendant outlived its group")


def test_a_cancel_from_another_thread_stops_the_group_as_a_timeout(tmp_path):
    marker, cancel = tmp_path / "pid", threading.Event()
    source = ("import os, sys, time\n"
              "child = os.fork()\n"
              "if child == 0:\n"
              "    time.sleep(30)\n"
              "else:\n"
              "    open(sys.argv[1], 'w').write(str(child))\n"
              "    time.sleep(30)\n")
    timer = threading.Timer(0.5, cancel.set)
    started = time.monotonic()
    timer.start()
    try:
        with pytest.raises(bounded_process.Failure) as refused:
            run(source, marker, timeout=60, cancel=cancel)
    finally:
        timer.cancel()
        timer.join(timeout=5)
    assert cancel.is_set() and refused.value.kind == "timeout" and time.monotonic() - started < 8
    descendant = int(marker.read_text())
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline:
        try:
            os.kill(descendant, 0)
        except ProcessLookupError:
            return
        time.sleep(0.05)
    os.kill(descendant, signal.SIGKILL)
    pytest.fail("the descendant outlived its cancelled group")


def test_a_cancelled_run_starts_no_process(monkeypatch):
    cancel = threading.Event()
    cancel.set()
    monkeypatch.setattr(bounded_process.subprocess, "Popen", lambda *args, **kwargs: pytest.fail("a cancelled run started a process"))
    with pytest.raises(bounded_process.Failure) as refused:
        run("pass", cancel=cancel)
    assert refused.value.kind == "timeout"


def test_a_command_that_cannot_start_or_is_not_absolute_is_refused(tmp_path):
    for argv in ([str(tmp_path / "missing")], ["relative"], [], [sys.executable, "a\x00b"], [sys.executable, b"bytes"]):
        with pytest.raises(bounded_process.Failure) as refused:
            bounded_process.run(argv, environment=ENVIRONMENT)
        assert refused.value.kind == "command"


def test_the_environment_is_required_and_exactly_what_the_caller_passes(monkeypatch):
    with pytest.raises(TypeError):
        bounded_process.run([sys.executable, "-I", "-c", "pass"])  # pylint: disable=missing-kwoa
    monkeypatch.setenv("BOOTWRIGHT_INHERITED_SENTINEL", "leaked")
    source = "import os; print(sorted(os.environ))"
    names = run(source, environment={"ONLY": "this"}).stdout
    assert b"'ONLY'" in names and b"BOOTWRIGHT_INHERITED_SENTINEL" not in names
    with pytest.raises(bounded_process.Failure) as refused:
        run(source, environment=None)
    assert refused.value.kind == "command"


class Started(Exception):
    """A process was started by a call that had to refuse first."""


@pytest.mark.parametrize("options", [
    {"data": "text"}, {"data": bytearray(b"x")}, {"timeout": 0}, {"timeout": -1}, {"timeout": "5"}, {"timeout": True},
    {"timeout": float("nan")}, {"timeout": float("inf")}, {"limit": -1}, {"limit": 1.5}, {"environment": {"A": 1}},
    {"environment": [("A", "b")]}, {"cancel": object()},
])
def test_an_invalid_argument_is_refused_before_any_process_starts(monkeypatch, options):
    def forbidden(*args, **kwargs):
        raise Started

    monkeypatch.setattr(bounded_process.subprocess, "Popen", forbidden)
    with pytest.raises(bounded_process.Failure) as refused:
        run("pass", **options)
    assert refused.value.kind == "command"


def test_a_failure_after_the_start_still_kills_and_reaps_the_child(monkeypatch):
    started = []
    popen = bounded_process.subprocess.Popen

    def recording(*args, **kwargs):
        started.append(popen(*args, **kwargs))
        return started[-1]

    def broken(*args, **kwargs):
        raise RuntimeError("setup failed")

    monkeypatch.setattr(bounded_process.subprocess, "Popen", recording)
    monkeypatch.setattr(bounded_process.selectors, "DefaultSelector", broken)
    with pytest.raises(RuntimeError):
        run("import time; time.sleep(30)")
    assert len(started) == 1 and started[0].returncode is not None
    with pytest.raises(ProcessLookupError):
        os.killpg(started[0].pid, 0)


def gone(pid, seconds=3):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


def test_a_reap_that_outlasts_its_second_keeps_the_timeout(monkeypatch):
    stuck = []
    popen = bounded_process.subprocess.Popen

    class Unreaped(popen):
        def wait(self, timeout=None):
            if timeout == 1 and not stuck:
                stuck.append(self)
                raise bounded_process.subprocess.TimeoutExpired(self.args, timeout)
            return super().wait(timeout)

    monkeypatch.setattr(bounded_process.subprocess, "Popen", Unreaped)
    try:
        with pytest.raises(bounded_process.Failure) as refused:
            run("import time; time.sleep(30)", timeout=0.3)
    finally:
        for process in stuck:
            process.wait(5)
    assert len(stuck) == 1 and refused.value.kind == "timeout"


LINGERING = ("import os, sys, time\n"
             "child = os.fork()\n"
             "if child == 0:\n"
             "    time.sleep(30)\n"
             "    os._exit(0)\n"
             "open(sys.argv[1], 'w').write(str(child))\n"
             "sys.stdout.write('done')\n"
             "sys.exit(4)\n")


def test_an_exited_leader_whose_descendant_holds_its_pipes_returns_promptly(tmp_path):
    marker = tmp_path / "pid"
    started = time.monotonic()
    result = run(LINGERING, marker, timeout=20)
    assert result == (4, b"done", b"") and time.monotonic() - started < 10
    descendant = int(marker.read_text())
    if not gone(descendant):
        os.kill(descendant, signal.SIGKILL)
        pytest.fail("the descendant outlived its group")


def test_without_a_session_a_lingering_descendant_is_left_to_its_supervisor(tmp_path):
    marker = tmp_path / "pid"
    started = time.monotonic()
    try:
        result = run(LINGERING, marker, timeout=20, session=False)
    finally:
        descendant = int(marker.read_text())
        alive = not gone(descendant, 0.2)
        if alive:
            os.kill(descendant, signal.SIGKILL)
    assert result == (4, b"done", b"") and time.monotonic() - started < 10 and alive


def test_the_run_command_shape_runs_with_exactly_the_environment_given(monkeypatch):
    monkeypatch.setenv("BOOTWRIGHT_INHERITED_SENTINEL", "leaked")
    call = bounded_process.runner(10)
    source = "import os, sys; sys.stdout.write(repr(sorted(os.environ))); sys.stderr.buffer.write(b'\\xff'); sys.exit(2)"
    code, out, err = call([sys.executable, "-I", "-c", source], check_rc=False, environ_update={"ONLY": "this"}, handle_exceptions=False)
    assert code == 2 and "'ONLY'" in out and "BOOTWRIGHT_INHERITED_SENTINEL" not in out and err == "\udcff"
    with pytest.raises(bounded_process.Failure):
        call([sys.executable, "-I", "-c", "pass"], check_rc=True, environ_update={})


def test_the_run_command_shape_raises_each_failure_as_an_os_error(tmp_path):
    with pytest.raises(FileNotFoundError):
        bounded_process.runner(10)([str(tmp_path / "missing")], environ_update={})
    with pytest.raises(TimeoutError):
        bounded_process.runner(0.3)([sys.executable, "-I", "-c", "import time; time.sleep(30)"], environ_update={})
    with pytest.raises(OSError) as flood:
        bounded_process.runner(10, 16)([sys.executable, "-I", "-c", "print('x' * 64)"], environ_update={})
    assert "output" in str(flood.value) and not isinstance(flood.value, (FileNotFoundError, TimeoutError))


def test_a_umask_reaches_the_child_and_a_bad_one_is_refused():
    assert run("import os; print(oct(os.umask(0)))", umask=0o077).stdout == b"0o77\n"
    with pytest.raises(bounded_process.Failure) as refused:
        run("pass", umask=0o1000)
    assert refused.value.kind == "command"

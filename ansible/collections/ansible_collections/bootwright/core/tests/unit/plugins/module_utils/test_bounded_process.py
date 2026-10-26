"""The one bounded runner returns a process's output and stops it and its descendants within the budget."""

from __future__ import annotations

import os
import signal
import sys
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
    {"environment": [("A", "b")]},
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

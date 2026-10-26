"""The pinned controller runtime reaps native Ansible workers and commands."""

from __future__ import annotations

import errno
import json
import os
import signal
import socket
import subprocess
import sys
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_supervisor as supervisor,
)


def running(pid):
    try:
        with open("/proc/%d/stat" % pid, "rb") as stream:
            stat = stream.read()
    except OSError:
        return False
    return stat[stat.rfind(b")") + 2 :][:1] not in (b"Z", b"X")


LINUX = pytest.mark.skipif(not os.path.isdir("/proc/self"), reason="needs /proc")


@LINUX
@pytest.mark.parametrize("killed", [False, True], ids=["normal-exit", "killed-supervisor"])
def test_real_ansible_leaves_no_worker_or_command(tmp_path, killed):
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as probe:
            probe.bind("\0bootwright-supervisor-test-%d" % os.getpid())
    except OSError as error:
        if error.errno in (errno.EACCES, errno.EPERM):
            pytest.skip("the test environment prohibits Ansible's local Unix IPC socket")
        raise
    record, playbook, inventory = (tmp_path / name for name in ("pid", "play.json", "inventory.json"))
    inventory.write_text(json.dumps({"all": {"hosts": {"local-test": {
        "ansible_connection": "local", "ansible_python_interpreter": sys.executable,
        "ansible_remote_tmp": str(tmp_path / "remote"),
    }}}}))
    command = "import os,time; open(%r, 'w').write(str(os.getpid())); time.sleep(%s)" % (str(record), 30 if killed else 0.3)
    playbook.write_text(json.dumps([{"hosts": "all", "gather_facts": False, "tasks": [
        {"name": "Hold a disposable native test process", "ansible.builtin.command": {"argv": [sys.executable, "-c", command]},
         "changed_when": False},
    ]}]))
    environment = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(tmp_path),
                   "ANSIBLE_LOCAL_TEMP": str(tmp_path / "local"), "ANSIBLE_NOCOLOR": "1"}
    output = tmp_path / "output"
    with output.open("wb") as log:
        process = subprocess.Popen([sys.executable, "-I", "-B", supervisor.__file__, "--lifecycle", "-i", str(inventory), str(playbook)],
                                   env=environment, stdout=log, stderr=log, start_new_session=True)
        descendants = set()
        try:
            deadline = time.monotonic() + 15
            while not record.exists() and process.poll() is None and time.monotonic() < deadline:
                time.sleep(0.02)
            assert record.exists(), output.read_text()
            task = int(record.read_text())
            descendants = supervisor.descendants(process.pid, supervisor.running_parents())
            if killed:
                assert task in descendants and len(descendants) >= 3
                assert any(os.getsid(pid) != os.getsid(process.pid) for pid in descendants if running(pid))
                process.kill()
                process.wait(timeout=5)
            else:
                assert process.wait(timeout=5) == 0, output.read_text()
            descendants.add(task)
            deadline = time.monotonic() + 5
            while any(running(pid) for pid in descendants) and time.monotonic() < deadline:
                time.sleep(0.02)
            assert not [pid for pid in descendants if running(pid)], output.read_text()
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
            for pid in filter(running, descendants):
                os.kill(pid, signal.SIGKILL)

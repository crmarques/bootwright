"""A plain pytest run loads the collection the way ansible-test does.

ansible-test's unit runner installs Ansible's collection loader over the
collection before it collects a test, so a task that names a plugin by its
collection name, such as bootwright.core.to_installer_yaml, resolves. pytest
alone, with ansible/collections on PYTHONPATH, imports the collection as plain
namespace packages that carry no collection metadata, and the same lookup finds
nothing. When no runner has installed a loader, this installs the one
ansible-test would, over the collections directory this file sits in, before
any test imports the collection.
"""

from __future__ import annotations

import fcntl
import json
import os
import pathlib
import signal

import pytest
from ansible.module_utils.testing import patch_module_args
from ansible.utils.collection_loader import AnsibleCollectionConfig
from ansible.utils.collection_loader._collection_finder import _AnsibleCollectionFinder

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]


def pytest_configure(config):
    config.addinivalue_line(
        "markers",
        "serial: forks or sends a process-directed signal, so the gate's plain loop runs it alone, in process, with no xdist worker thread",
    )
    if AnsibleCollectionConfig.collection_finder is None:
        _AnsibleCollectionFinder(paths=[str(COLLECTIONS)])._install()


@pytest.fixture(autouse=True)
def only_a_serial_test_forks_or_signals_itself(request, monkeypatch):
    """Refuse a fork or a process-directed signal from a test the plain loop does not run alone.

    Under pytest-xdist every worker has an I/O thread of its own: a fork there
    may deadlock in the child and a signal sent to the process may reach that
    thread. The gate runs the tests marked serial alone in process, so a test
    that forks, arms a timer or signals its own process carries the marker.
    """
    if request.node.get_closest_marker("serial"):
        return

    def refused(what):
        raise AssertionError(what + " needs @pytest.mark.serial: the gate runs only those tests alone, in process")

    kill, setitimer, alarm = os.kill, signal.setitimer, signal.alarm

    def guarded_fork():
        refused("os.fork")

    def guarded_forkpty():
        refused("os.forkpty")

    def guarded_kill(pid, number):
        if pid in (0, os.getpid()) and number != 0:
            refused("a signal sent to the test's own process")
        return kill(pid, number)

    def guarded_setitimer(which, seconds, interval=0.0):
        if seconds or interval:
            refused("signal.setitimer")
        return setitimer(which, seconds, interval)

    def guarded_alarm(seconds):
        if seconds:
            refused("signal.alarm")
        return alarm(seconds)

    def guarded_raise_signal(number):
        refused("signal.raise_signal")

    monkeypatch.setattr(os, "fork", guarded_fork)
    monkeypatch.setattr(os, "forkpty", guarded_forkpty)
    monkeypatch.setattr(os, "kill", guarded_kill)
    monkeypatch.setattr(signal, "setitimer", guarded_setitimer)
    monkeypatch.setattr(signal, "alarm", guarded_alarm)
    monkeypatch.setattr(signal, "raise_signal", guarded_raise_signal)


@pytest.fixture
def run_module(capsys):
    """run_module(module, arguments) runs the module's main() in process, as a task would, and returns what it printed.

    The arguments are its task arguments; exit_json and fail_json each end
    the run, and the result is the JSON they print.
    """
    def run(module, arguments):
        capsys.readouterr()
        with patch_module_args(arguments), pytest.raises(SystemExit):
            module.main()
        return json.loads(capsys.readouterr().out)

    return run


@pytest.fixture(autouse=True)
def no_host_network_manager_keyfiles(tmp_path_factory, monkeypatch):
    """Keep an observation from reading this machine's NetworkManager keyfiles: a test names its own directory."""
    from ansible_collections.bootwright.core.plugins.module_utils import libvirt_nm_connections
    monkeypatch.setattr(libvirt_nm_connections, "CONNECTIONS", str(tmp_path_factory.getbasetemp() / "no-system-connections"))


@pytest.fixture(scope="session")
def once_per_run(tmp_path_factory):
    """once_per_run(name, compute) is compute()'s JSON result, once per run."""
    return lambda name, compute: computed_once(tmp_path_factory, name, compute)


def computed_once(tmp_path_factory, name, compute):
    """compute()'s JSON result, computed once per test run.

    pytest-xdist hands a module's tests to several workers and tears a
    module-scoped fixture down between them, so a fixture that runs a playbook
    would run once per test. Under xdist the first worker computes the result
    under a lock in the run's shared temporary directory and the others read
    it; a computation that fails writes nothing, so every worker fails alike.
    Outside xdist, compute() runs as a module-scoped fixture runs.
    """
    run = os.environ.get("PYTEST_XDIST_TESTRUNUID")
    if not run:
        return compute()
    shared = tmp_path_factory.getbasetemp().parent / ("once-" + run)
    shared.mkdir(exist_ok=True)
    result = shared / (name + ".json")
    with open(shared / (name + ".lock"), "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if result.exists():
            return json.loads(result.read_text())
        value = compute()
        partial = shared / (name + ".partial")
        partial.write_text(json.dumps(value))
        os.replace(partial, result)
        return value


# The controller's SSH crypto policy, which a Ceph transport reads and
# evaluates with ssh -G, is host state: a unit test sees a fixed policy that
# admits every pinned key type unless it supplies its own.
CEPH_TEST_POLICY = {
    "configuration": "",
    "hostKeyAlgorithms": "ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-256,rsa-sha2-512",
}


@pytest.fixture(autouse=True)
def hermetic_ceph_ssh_policy(monkeypatch):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_ssh_policy
    monkeypatch.setattr(ceph_ssh_policy, "controller_policy", lambda: dict(CEPH_TEST_POLICY))

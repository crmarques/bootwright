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

import pytest
from ansible.utils.collection_loader import AnsibleCollectionConfig
from ansible.utils.collection_loader._collection_finder import _AnsibleCollectionFinder

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]


def pytest_configure():
    if AnsibleCollectionConfig.collection_finder is None:
        _AnsibleCollectionFinder(paths=[str(COLLECTIONS)])._install()


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

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

import pathlib

import pytest
from ansible.utils.collection_loader import AnsibleCollectionConfig
from ansible.utils.collection_loader._collection_finder import _AnsibleCollectionFinder

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]


def pytest_configure():
    if AnsibleCollectionConfig.collection_finder is None:
        _AnsibleCollectionFinder(paths=[str(COLLECTIONS)])._install()


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

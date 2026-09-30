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

from ansible.utils.collection_loader import AnsibleCollectionConfig
from ansible.utils.collection_loader._collection_finder import _AnsibleCollectionFinder

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]


def pytest_configure():
    if AnsibleCollectionConfig.collection_finder is None:
        _AnsibleCollectionFinder(paths=[str(COLLECTIONS)])._install()

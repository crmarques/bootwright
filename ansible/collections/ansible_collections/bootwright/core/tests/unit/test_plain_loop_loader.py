"""The plain pytest loop, which runs the serial tests alone, still loads the collection as ansible-test does.

conftest.py installs Ansible's collection loader when no runner has. This is the
one test the plain loop runs that proves a plugin resolves by its collection
name there, so a change to that installation fails the gate's plain lane and not
only the tests that happen to name a plugin.
"""

from __future__ import annotations

import pytest
from ansible.plugins.loader import filter_loader
from ansible.utils.collection_loader import AnsibleCollectionConfig


@pytest.mark.serial
def test_a_plugin_resolves_by_its_collection_name_under_the_installed_loader():
    assert AnsibleCollectionConfig.collection_finder is not None
    assert filter_loader.get("bootwright.core.to_installer_yaml") is not None

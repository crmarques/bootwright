"""Shared Ceph host readings through the full Ansible runtime: the action plugin and the role's templating.

These need the whole ansible package, which the module_utils units group lacks, so they live beside the other
action-plugin tests rather than with the reading's own tests.
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, native_diagnostics
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_shared_host_reading as shared


def test_the_protocol_publishes_a_reading_only_as_an_observation():
    from ansible_collections.bootwright.core.plugins.action import ceph_cluster_protocol
    blocks = shared.resources()
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], shared.Transport().bound(blocks[0][0]), reading="hosts")["evidence"]
    assert ceph_cluster_protocol.completion({"evidence": reading, "digest": blocks[0][1], "observed": True}) == (reading, None)
    assert ceph_cluster_protocol.completion({"evidence": reading, "digest": blocks[0][1]})[1] is not None
    with pytest.raises(ceph_native.Refusal):
        ceph_cluster_protocol.completion({"evidence": reading, "digest": blocks[1][1], "observed": True})
    assert native_diagnostics.valid(shared.NATIVE_ERROR)


def test_a_lent_reading_stays_data_through_the_roles_templating():
    """The runner marks every extra variable a string of data (ansible.ExtraVariables), so the role hands the JSON on unrendered."""
    from ansible.parsing.dataloader import DataLoader
    from ansible.template import Templar, trust_as_template

    def marked(value):
        if isinstance(value, str):
            return {"__ansible_unsafe": value}
        if isinstance(value, dict):
            return {key: marked(item) for key, item in value.items()}
        return value

    text = json.dumps({"version": ceph_core.HOST_READING, "hosts": [{"name": "{{ 7 * 7 }}", "state": {}}]})
    loader = DataLoader()
    variables = loader.load(json.dumps(marked({"bootwright_ceph_material": {"hosts": text, "knownHosts": "/run/known"}})))
    rendered = Templar(loader=loader, variables=variables).template(trust_as_template("{{ bootwright_ceph_material }}"))
    assert str(rendered["hosts"]) == text

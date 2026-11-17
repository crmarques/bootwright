"""The erase protocol action admits exactly the refusals the erase module names.

The module's refusal tables are tested in test_physical_erase_run.py; this case
lives with the action plugins because importing the action needs the
controller's ansible.plugins, which the module lane does not provide.
"""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.action import physical_erase_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules import physical_erase as module


def test_the_module_names_exactly_the_refusals_its_protocol_and_go_remedy():
    # Go's eraseRefusals gives each of these fifteen a diagnostic (TestPhysicalEraseNamesEachRefusalWithItsCauseAndTheDestroyRemedy).
    assert set(module.NAMED.values()) | set(module.BUILT.values()) == set(protocol.REFUSALS) and len(protocol.REFUSALS) == 16
    assert "refused" in protocol.CAPABILITY.phases and protocol.CAPABILITY.detail is True

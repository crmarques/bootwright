"""A removal that leaves its unit listed failed proves no absence and names the unit.

The observation reports a removed unit systemd still lists failed
(plugins/module_utils/infra_service.py and artifact_server.py, unit_state), so
a destroy that could not take that state back is refused here and reobserved
as unfinished by the next one.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.action import (
    artifact_server_protocol,
    infra_service_protocol,
)

PROTOCOLS = (infra_service_protocol, artifact_server_protocol)


@pytest.mark.parametrize("protocol", PROTOCOLS, ids=["infra_service", "artifact_server"])
def test_a_removal_leaving_a_unit_listed_failed_proves_no_absence(protocol):
    evidence, unreached = protocol.completion({
        "removed": True,
        "digest": "d" * 64,
        "observation": {"unit": "failed", "container": "", "containerPresent": False, "contentRoot": False},
    })
    assert evidence["postcondition"] is False
    assert unreached is not None
    assert "unit" in unreached.split(": ", 1)[1].split(", ")

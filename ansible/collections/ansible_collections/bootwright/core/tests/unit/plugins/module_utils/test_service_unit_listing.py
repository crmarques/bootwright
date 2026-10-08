"""A removed unit systemd still lists failed is observed, which a removal's absence proof refuses.

systemctl show --value prints the property's value alone (systemctl(1),
--value), and for a unit that is not loaded it prints ActiveState=inactive, as
systemd documents. The answers are faked below; the definition file is a real
file under tmp_path.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    artifact_server,
    infra_service,
)

SERVICE = "bootwright-lab-dns-resolver.service"
MODULES = (infra_service, artifact_server)


def shows(module, value):
    def runner(argv, check_rc=False, environ_update=None):
        assert check_rc is False and environ_update == module.ENVIRONMENT
        assert argv == [module.SYSTEMCTL, "show", "--property=ActiveState", "--value", SERVICE]
        return 0, value + "\n", ""
    return runner


@pytest.mark.parametrize("module", MODULES, ids=["infra_service", "artifact_server"])
def test_a_removed_unit_still_listed_failed_is_observed(module, tmp_path):
    unit_path = str(tmp_path / "unit.container")
    assert module.unit_state(shows(module, "failed"), unit_path, SERVICE) == "failed"
    for shown in ("inactive", "active", ""):
        assert module.unit_state(shows(module, shown), unit_path, SERVICE) == "", shown
    (tmp_path / "unit.container").write_text("[Container]\n")
    for shown in module.UNIT_STATES:
        assert module.unit_state(shows(module, shown), unit_path, SERVICE) == shown
    assert module.unit_state(shows(module, "reloading"), unit_path, SERVICE) == ""

"""The wildcard label resolve.yml proves is the one preflight checks.

Preflight container-cluster derives the name beneath the applications wildcard
in Go (WildcardLabel in internal/containercluster/openshift/endpoints.go), and
TestWildcardLabelMatchesTheInstallRoleTable there reads the same table this
module reads, goldens/containercluster_wildcard_labels.json. Each side renders
its own rule over that table, so a change to either rule fails its side.

Rendering the role's task file needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins/modules, so this check lives here.
"""

from __future__ import annotations

import json
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

UNIT = pathlib.Path(__file__).resolve().parent
ROLE = UNIT.parents[1] / "roles" / "containercluster_install_agent"
TABLE = json.loads((UNIT / "goldens" / "containercluster_wildcard_labels.json").read_text(encoding="utf-8"))
LOADER = DataLoader()


def label(cluster):
    """The wildcard label resolve.yml renders for one cluster name."""
    loaded = LOADER.load_from_file(str(ROLE / "tasks" / "resolve.yml"), trusted_as_template=True)
    task = next(task for task in loaded
                if isinstance(task, dict) and "containercluster_install_agent_probes" in (task.get("ansible.builtin.set_fact") or {}))
    scope = {"bootwright_cluster_install_request": {"identity": {"cluster": cluster}}}
    return Templar(loader=LOADER, variables=scope).template(task["vars"]["containercluster_install_agent_wildcard_label"])


@pytest.mark.parametrize("row", TABLE, ids=[row["case"] for row in TABLE])
def test_the_role_renders_the_wildcard_label_of_the_shared_table(row):
    assert label(row["cluster"]) == row["label"]


def test_the_shared_table_cuts_trailing_hyphens():
    assert any(len("bootwright-" + row["cluster"]) > 63 and ("bootwright-" + row["cluster"])[:63].endswith("-")
               for row in TABLE)

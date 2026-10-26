"""A work area another context incarnation built is neither removed nor handed over.

A context deleted with --allow-orphans leaves its clusters' work areas, and a
context recreated under the same name derives the same paths. The build
receipt records the incarnation that built the area; the removal refuses before
it removes anything from an area another incarnation built, and the
observation that hands the kept kubeconfig to custody before such a removal
refuses naming the area instead of skipping the hand-over. The conditions are
rendered through Ansible's Templar against the role's own defaults.
"""

from __future__ import annotations

import base64
import json
import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_media_agent"
LOADER = DataLoader()
OWN = "64768:1234:b1761490000.123456789"
EARLIER = "64768:1234:b1761400000.987654321"


def tasks(name):
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True) if isinstance(task, dict)]


def position(entries, predicate):
    matches = [index for index, task in enumerate(entries) if predicate(task)]
    assert len(matches) == 1, matches
    return matches[0]


def scope(receipt, **variables):
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    values["bootwright_cluster_media_request"] = {"workRoot": "/var/lib/bootwright-clusters/lab/sno", "image": {"path": ""}}
    values["bootwright_cluster_media_material"] = {"incarnation": OWN}
    if receipt is not None:
        encoded = base64.b64encode(json.dumps(receipt).encode()).decode()
        values["containercluster_media_agent_built"] = {"content": encoded}
    else:
        values["containercluster_media_agent_built"] = {"failed": True}
    values.update(variables)
    return Templar(loader=LOADER, variables=values)


def holds(templar, conditions):
    conditions = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in conditions)


def test_only_a_receipt_naming_another_incarnation_is_foreign():
    for receipt, foreign in (({"incarnation": EARLIER}, True), ({"incarnation": OWN}, False), ({"inputs": "x"}, False), (None, False)):
        assert holds(scope(receipt), "containercluster_media_agent_foreign | bool") is foreign, receipt


def test_the_removal_refuses_an_area_another_incarnation_built_before_it_removes_anything():
    entries = tasks("destroy.yml")
    read = position(entries, lambda task: task.get("register") == "containercluster_media_agent_built")
    refuse = position(entries, lambda task: "ansible.builtin.fail" in task)
    removals = [index for index, task in enumerate(entries) if (task.get("ansible.builtin.file") or {}).get("state") == "absent"]
    assert len(removals) == 2 and read < refuse < min(removals)
    assert entries[read]["ansible.builtin.slurp"] == {"src": "{{ containercluster_media_agent_receipt }}"}
    assert holds(scope({"incarnation": EARLIER}), entries[refuse]["when"])
    assert not holds(scope({"incarnation": OWN}), entries[refuse]["when"])
    assert not holds(scope(None), entries[refuse]["when"])
    assert "nothing was removed" in entries[refuse]["ansible.builtin.fail"]["msg"]
    assert "{{ containercluster_media_agent_work }}" in entries[refuse]["ansible.builtin.fail"]["msg"]


def test_the_hand_over_refuses_naming_an_area_another_incarnation_built():
    entries = tasks("observe.yml")
    read = position(entries, lambda task: task.get("register") == "containercluster_media_agent_built")
    refuse = position(entries, lambda task: "ansible.builtin.fail" in task)
    copy = position(entries, lambda task: "ansible.builtin.copy" in task)
    assert read < refuse < copy
    output = {"bootwright_cluster_media_output": {"kubeconfig": "/out"}}
    assert holds(scope({"incarnation": EARLIER}, **output), entries[refuse]["when"])
    assert not holds(scope({"incarnation": OWN}, **output), entries[refuse]["when"])
    assert not holds(scope({"incarnation": EARLIER}), entries[refuse]["when"])
    assert "{{ bootwright_cluster_media_request.workRoot }}" in entries[refuse]["ansible.builtin.fail"]["msg"]


def test_every_entry_point_reads_the_incarnation_in_the_form_the_controller_derives():
    form = "^[0-9]+:[0-9]+:(b[0-9]+[.][0-9]+|g[0-9]+)$"
    for name in ("apply.yml", "destroy.yml"):
        validation = tasks(name)[0]["ansible.builtin.assert"]["that"]
        assert "bootwright_cluster_media_material.incarnation | default('') is match('%s')" % form in validation, name
    templar = scope(None)
    for value, valid in ((OWN, True), ("64768:1234:g42", True), ("64768:1234", False), ("", False)):
        assert holds(templar, "'%s' is match('%s')" % (value, form)) is valid, value

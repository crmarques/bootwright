"""A refused administrator login names its reason to the runner.

The storage_ceph_admin_credentials entry points run the module under no_log with its failure kept, publish the module's closed
reason as a `refused` record through the shared protocol and then fail, so the runner reports the diagnostic Go gave that reason
(loginRefusals in internal/storage/ceph). Rendering needs Ansible's controller (Templar), so these checks live beside the other
role-rendering tests.
"""

from __future__ import annotations

import pathlib

import pytest
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import storage_ceph_admin_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "storage_ceph_admin_credentials" / "tasks"
FAILURE = "The owned administrator login could not be proved or updated"


def named(entry):
    tasks = [task for task in LOADER.load_from_file(str(ROLE / (entry + ".yml")), trusted_as_template=True) if isinstance(task, dict)]
    index = next(i for i, task in enumerate(tasks) if "bootwright.core.ceph_admin_credentials" in task)
    call, refusal, unproved = tasks[index:index + 3]
    assert call["register"] == "storage_ceph_admin_credentials_result" and call["no_log"] is True and call["failed_when"] is False
    assert refusal["bootwright.core.storage_ceph_admin_protocol"]["phase"] == "refused" and refusal["no_log"] is True
    assert "ansible.builtin.fail" in unproved and unproved["when"] == "storage_ceph_admin_credentials_result.proved is not defined"
    return refusal


@pytest.mark.parametrize("entry", ["prepare", "update"])
def test_the_role_publishes_a_refusal_in_place_of_completion_and_fails_every_other_failure(entry):
    refusal = named(entry)
    registered = "storage_ceph_admin_credentials_result"

    def published(result):
        rendering = Templar(loader=LOADER, variables={registered: result})
        if not rendering.evaluate_conditional(refusal["when"]):
            return []
        records = []
        outcome = adapter_protocol.publish(rendering.template(refusal["bootwright.core.storage_ceph_admin_protocol"]), protocol.CAPABILITY,
                                           lambda record, acknowledge=False: records.append(record))
        assert not outcome.get("failed"), outcome
        return records

    assert published({"changed": False, "proved": True}) == []
    assert published({"failed": False, "msg": FAILURE}) == []
    assert published({"failed": False, "msg": FAILURE, "reason": "grafana-login-drift"}) == [{"phase": "refused", "reason": "grafana-login-drift"}]
    assert adapter_protocol.publish({"phase": "refused", "reason": "remote-refusal"}, protocol.CAPABILITY, lambda record, acknowledge=False: None) == {
        "failed": True, "msg": protocol.CAPABILITY.unpublished}

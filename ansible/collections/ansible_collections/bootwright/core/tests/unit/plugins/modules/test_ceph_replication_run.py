"""The ceph_replication module's main() runs over stubbed transports and a stubbed lifecycle, as a task would run it.

The lifecycle's own phases are tested in test_ceph_replication.py over native
fakes. These cases hold the module's part: which material reaches which side's
transport, what the lifecycle is handed, and the refusal record a failure ends in.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.plugins.module_utils import ceph_replication as lifecycle
from ansible_collections.bootwright.core.plugins.module_utils import ceph_replication_native
from ansible_collections.bootwright.core.plugins.modules import ceph_replication as module

DIGEST = "e" * 64
REQUEST = {"primary": {"scope": {"hosts": []}}, "secondary": {"scope": {"hosts": []}}}
MATERIAL = {"primarySSHKey": "/run/lent/primary", "secondarySSHKey": "/run/lent/secondary", "knownHosts": "/run/lent/known_hosts"}
ARGUMENTS = {"request": REQUEST, "operation": "observe", "digest": DIGEST, "material": MATERIAL}


@pytest.fixture(name="stubbed")
def stubbed_lifecycle(monkeypatch):
    """The two sides' SSH transports and the replication lifecycle, answering from `answer` and recording what they were handed."""
    state = {"transports": [], "executed": [], "answer": {"changed": False, "evidence": {"ready": True}}}

    class Transport:
        def __init__(self, request, material, **options):
            state["transports"].append((request, material, options))

    def execute(request, operation, digest, transports):
        state["executed"].append((request, operation, digest, sorted(transports)))
        if isinstance(state["answer"], Exception):
            raise state["answer"]  # pylint: disable=raising-bad-type
        return state["answer"]

    monkeypatch.setattr(ceph_core, "Transport", Transport)
    monkeypatch.setattr(ceph_replication_native, "validate", lambda request: request)
    monkeypatch.setattr(lifecycle, "execute", execute)
    return state


def test_each_side_gets_its_own_key_and_the_shared_known_hosts_and_the_lifecycle_answer_is_returned(stubbed, run_module):
    result = run_module(module, ARGUMENTS)
    assert result["changed"] is False and result["evidence"] == {"ready": True} and not result.get("failed")
    assert [(material, options) for _request, material, options in stubbed["transports"]] == [
        ({"sshKey": "/run/lent/primary", "knownHosts": "/run/lent/known_hosts"}, {"scope": True}),
        ({"sshKey": "/run/lent/secondary", "knownHosts": "/run/lent/known_hosts"}, {"scope": True}),
    ]
    assert stubbed["executed"] == [(REQUEST, "observe", DIGEST, ["primary", "secondary"])]


def test_a_destroy_and_a_digest_that_is_not_hex_are_refused_before_any_transport_is_built(stubbed, run_module):
    for arguments in (dict(ARGUMENTS, operation="destroy"), dict(ARGUMENTS, digest="not-a-digest")):
        result = run_module(module, arguments)
        assert result["failed"] is True and result["msg"] == "Ceph replication operation refused"
        assert result["reason"] in ("request", "unknown")
    assert stubbed["transports"] == [] and stubbed["executed"] == []


def test_a_material_set_that_is_not_exactly_the_two_keys_and_known_hosts_is_refused_as_material(stubbed, run_module):
    result = run_module(module, dict(ARGUMENTS, material={"primarySSHKey": "/run/lent/primary", "knownHosts": "/run/lent/known_hosts"}))
    assert result["failed"] is True and result["reason"] == "material"
    assert stubbed["transports"] == []


def test_a_refusal_from_the_lifecycle_names_its_closed_reason_and_never_a_value(stubbed, run_module):
    stubbed["answer"] = ceph_native.Refusal("foreign-pool")
    result = run_module(module, ARGUMENTS)
    assert result["failed"] is True and result["msg"] == "Ceph replication operation refused"
    assert result["reason"] == "foreign-pool"
    assert "evidence" not in result

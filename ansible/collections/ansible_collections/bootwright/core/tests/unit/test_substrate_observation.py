"""A libvirt observation publishes what it found, in the form the protocol chooses.

A caller that picked the absence form from a field or two published it over a
machine with only its disks left and over a hypervisor that did not answer, and
the engine then read those as nothing it could converge. The protocol publishes
absence only when everything it reads is gone, so an observation never names a
form. Losing that is invisible to lint and to a syntax check.
"""

from __future__ import annotations

import pathlib

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"

OBSERVATIONS = {
    "substrate_libvirt_host": "bootwright.core.substrate_host_protocol",
    "substrate_libvirt_machine": "bootwright.core.substrate_machine_protocol",
}


def publications(role, action):
    parsed = yaml.safe_load((ROLES / role / "tasks" / "observe.yml").read_text()) or []
    return [
        task[action] for task in parsed
        if isinstance(task, dict) and isinstance(task.get(action), dict) and task[action].get("phase") == "completed"
    ]


def test_the_libvirt_observations_let_the_protocol_choose_their_form():
    for role, action in OBSERVATIONS.items():
        published = publications(role, action)
        assert len(published) == 1, role
        assert published[0].get("observed") is True, role
        assert "removed" not in published[0], role

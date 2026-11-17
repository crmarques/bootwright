"""A root disk declared by serial number has the WWN it reported to the
installer recorded in its installation's evidence.

The installer's %pre, the proof that matched the disk's serial number and exact
size, writes the WWN the disk reports (empty when it reports none), and a
--nochroot %post installs it at /etc/bootwright/root-wwn. After the network
converged, and in every observation, the role reads that record over the
pinned fleet connection guest.yml owns, and the completion publishes it. Any
other installation reads nothing.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest

from ansible.template import Templar
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
INCLUDE = "ansible.builtin.include_tasks"
PROTOCOL = "bootwright.core.managedos_install_protocol"
INSPECT = "bootwright.core.managedos_install_inspect"
REQUIRED = "managedos_install_anaconda_root_wwn_required | bool"
REACHABLE = "managedos_install_anaconda_reachable.rc | default(1) == 0"
SERIAL = {"physical": True, "hardware": {"serialNumber": "ZC1A2B3C", "sizeBytes": 480103981056, "interfaces": []}}
SHAPES = {
    "serial": SERIAL,
    "wwn": {"physical": True, "hardware": {"rootDevice": "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "wwn": "0x5000c500a1b2c3d4",
                                           "serialNumber": "ZC1A2B3C", "interfaces": []}},
    "volume": {"physical": True, "hardware": {"rootVolumeRef": {"firmware": True}, "interfaces": []}},
    "virtual": {"physical": False},
    "unframed": None,
}


def flattened(name):
    """A task file's tasks in play order, each block opened and its conditions carried onto every task inside it."""
    return [dict(visit.task, when=list(visit.conditions))
            for visit in role_tasks.visits(role_tasks.role_tasks(ROLE, name)) if "block" not in visit.task]


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def included(name):
    return lambda task: task.get(INCLUDE) == name


def render(value, shape, frozen, **variables):
    request = {"identity": {"context": "lab", "object": "metal-01"},
               "requires": {"fips": False, "registration": False, "rootEncryption": False, "rootWWN": frozen, "update": False}}
    if SHAPES[shape] is not None:
        request["target"] = SHAPES[shape]
    return Templar(loader=LOADER, variables=dict(variables, bootwright_os_install_request=request)).template(value)


# Which root reads its WWN is Go's to derive (a physical root declared by
# serial number, Request.serialRoot): the role reads the frozen requirement and
# never the target it was derived from, whatever shape that target has.
@pytest.mark.parametrize("shape", sorted(SHAPES))
@pytest.mark.parametrize("frozen", [True, False])
def test_the_wwn_is_read_exactly_when_the_frozen_request_requires_it(shape, frozen):
    tasks = flattened("root_wwn.yml")
    required = tasks[0]["ansible.builtin.set_fact"]["managedos_install_anaconda_root_wwn_required"]
    assert render(required, shape, frozen) is frozen
    for name in ("apply.yml", "observe.yml"):
        completion = flattened(name)
        arguments = completion[only(completion, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish")][PROTOCOL]
        assert not [key for key in arguments if key.endswith("Required")]
        assert render(arguments["requires"], shape, frozen)["rootWWN"] is frozen


def test_the_record_is_read_through_the_pinned_connection_and_writes_nothing():
    tasks = flattened("root_wwn.yml")
    guest = tasks[only(tasks, included("guest.yml"), "read the record")]
    assert guest["vars"] == {"managedos_install_anaconda_guest_command": ["/usr/bin/head", "-c", "258", "--", "/etc/bootwright/root-wwn"]}
    assert guest["when"] == [REQUIRED]
    assert not any("ansible.builtin.command" in task or task.get("become") for task in tasks)


@pytest.mark.parametrize("guest, required, read, value", [
    ({"rc": 0, "stdout": "naa.5000c500a1b2c3d4"}, True, True, "naa.5000c500a1b2c3d4"),
    ({"rc": 0, "stdout": ""}, True, True, ""),
    ({"rc": 1, "stdout": ""}, True, False, ""),
    ({"rc": 0, "stdout": "naa.5000c500a1b2c3d4"}, False, False, ""),
], ids=["reported", "none-reported", "unread", "not-required"])
def test_the_read_names_what_the_record_holds(guest, required, read, value):
    facts = flattened("root_wwn.yml")[-1]["ansible.builtin.set_fact"]
    variables = {"managedos_install_anaconda_root_wwn_required": required, "managedos_install_anaconda_guest": guest}
    templar = Templar(loader=LOADER, variables=variables)
    assert templar.template(facts["managedos_install_anaconda_root_wwn_read"]) is read
    assert templar.template(facts["managedos_install_anaconda_root_wwn"]) == value


def test_an_apply_reads_the_record_after_its_network_converged_and_before_its_completion():
    applied = flattened("apply.yml")
    converged = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("group") == "converge-network"
                     and task[PROTOCOL].get("status") == "ok", "report network convergence")
    read = only(applied, included("root_wwn.yml"), "read the root disk WWN")
    inspected = only(applied, lambda task: INSPECT in task and task.get("register") == "managedos_install_anaconda_after", "inspect")
    completed = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the completion")
    assert converged < read < inspected < completed
    arguments = applied[completed][PROTOCOL]
    assert arguments["rootWWN"] == '{{ managedos_install_anaconda_root_wwn | default("") }}'
    assert arguments["rootWWNRead"] == "{{ managedos_install_anaconda_root_wwn_read | default(false) }}"

    observed = flattened("observe.yml")
    read = only(observed, included("root_wwn.yml"), "read the root disk WWN")
    assert observed[read]["when"][-1] == REACHABLE
    published = only(observed, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the observation")
    assert read < published
    assert observed[published][PROTOCOL]["rootWWN"] == arguments["rootWWN"]
    assert observed[published][PROTOCOL]["rootWWNRead"] == arguments["rootWWNRead"]

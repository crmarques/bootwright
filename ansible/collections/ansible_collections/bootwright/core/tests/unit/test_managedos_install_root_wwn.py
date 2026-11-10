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

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
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
    def opened(listed, inherited):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section), conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), []))


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def included(name):
    return lambda task: task.get(INCLUDE) == name


def render(value, shape, **variables):
    request = {"identity": {"context": "lab", "object": "metal-01"}}
    if SHAPES[shape] is not None:
        request["target"] = SHAPES[shape]
    return Templar(loader=LOADER, variables=dict(variables, bootwright_os_install_request=request)).template(value)


@pytest.mark.parametrize("shape, expected", [("serial", True), ("wwn", False), ("volume", False), ("virtual", False), ("unframed", False)])
def test_only_a_physical_root_declared_by_serial_number_reads_its_wwn(shape, expected):
    tasks = flattened("root_wwn.yml")
    required = tasks[0]["ansible.builtin.set_fact"]["managedos_install_anaconda_root_wwn_required"]
    assert render(required, shape) is expected
    for name in ("apply.yml", "observe.yml"):
        completion = flattened(name)
        arguments = completion[only(completion, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish")][PROTOCOL]
        assert render(arguments["rootWWNRequired"], shape) is expected


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

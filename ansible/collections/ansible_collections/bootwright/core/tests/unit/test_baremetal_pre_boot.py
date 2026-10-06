"""A physical machine is erased only while it answers as the system this operation proved.

Both consumers of the bare-metal pre-boot proof (roles/substrate_baremetal_machine/
tasks/pre_boot.yml), the managed-OS installation and the cluster boot, repeat it
immediately before destructive media is inserted. It refuses a declaration with
no NIC, whose complete set would prove nothing, and compares the UUID and serial
the controller reports with the pin the Machine's bare-metal block proved
earlier in the same operation, exactly as a day-2 power operation does
(test_machine_power_pin.py, specs/substrates.md): the UUID other than in letter
case or surrounding space, the serial other than in surrounding space, a value
the proof recorded empty not at all, and no pin nothing.

Each consumer receives the pin base64-encoded in its material, which the runner
passes as --extra-vars @request.json with every string marked unsafe, so
ansible-core reads it as data and never renders it
(test_request_strings_are_data.py). These cases load the material trusted as a
template on purpose, as ansible-core would read it unmarked, so the encoding
alone keeps a raw value from being evaluated; they render the proof's variables
through each consumer's own include and evaluate the proof's real `that`
expressions.
Rendering needs Ansible's controller (DataLoader and Templar), which ansible-test
does not offer to unit tests under tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import json
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
BAREMETAL = ROLES / "substrate_baremetal_machine"
MANAGED_OS = ROLES / "managedos_install_anaconda"
CLUSTER = ROLES / "containercluster_install_agent"
LOADER = DataLoader()

ASSERT = "ansible.builtin.assert"
INCLUDE = "ansible.builtin.include_role"
INSPECT = "bootwright.core.redfish_system_inspect"
OBSERVED = "substrate_baremetal_machine_target"
PINS = ("substrate_baremetal_machine_target_pinned_uuid_base64", "substrate_baremetal_machine_target_pinned_serial_base64")

ENDPOINT = "https://metal-bmc.lab.example.test/redfish/v1/Systems/1"
UUID = "4c4c4544-0042-3510-8052-b4c04f4d4e31"
SERIAL = "CZJ2440ABC"
TEMPLATE = "{{ 6 * 7 }}"
MACS = ["aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:01"]
DECLARED = {"interfaces": [{"macAddress": MACS[0], "name": "enp2s0"}, {"macAddress": MACS[1], "name": "enp1s0"}]}
REMEDY = ("correct spec.hardware.management.bmc.address, or destroy and apply this context so the machine is "
          "proved again")


def loaded(path):
    """A task file as a play loads it: its templates trusted."""
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def one(found, description):
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def proof():
    return loaded(BAREMETAL / "tasks" / "pre_boot.yml")


def asserting(needle, description):
    return one([task for task in proof() if ASSERT in task and needle in json.dumps(task[ASSERT])], description)


def pinned():
    return asserting(PINS[0], "compare the pin")


def declared():
    return asserting("substrate_baremetal_machine_target_expected", "compare the declared NICs")


def include(role):
    """A consumer's bare-metal pre-boot include, by its role and the tasks_from stem."""
    return one([task for task in walk(loaded(role / "tasks" / "boot.yml"))
                if (task.get(INCLUDE) or {}).get("name") == "bootwright.core.substrate_baremetal_machine"
                and pathlib.PurePosixPath(str(task[INCLUDE].get("tasks_from", ""))).stem == "pre_boot"],
               "include the bare-metal pre-boot proof")


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def written(tmp_path, variables):
    """Variables as the runner writes them and ansible-core loads them."""
    path = tmp_path / "request.json"
    path.write_text(json.dumps(variables))
    # The loader caches by path, and every case writes the same path.
    return LOADER.load_from_file(str(path), cache="none", trusted_as_template=True)


def managed_os(tmp_path, pins, hardware):
    """The managed-OS installation of one physical Machine."""
    material = {"controllerUser": str(tmp_path / "bmc-user"), "controllerPassword": str(tmp_path / "bmc-password")}
    material.update(pins)
    variables = dict(LOADER.load_from_file(str(MANAGED_OS / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(written(tmp_path, {
        "bootwright_os_install_material": material,
        "bootwright_os_install_request": {"identity": {"object": "metal"}, "target": {
            "controller": {"endpoint": ENDPOINT, "tlsVerify": True}, "hardware": hardware,
            "physical": True, "substrate": "baremetal"}},
    }))
    variables.update(include(MANAGED_OS)["vars"])
    return variables


def cluster(tmp_path, pins, hardware):
    """Node 1 of a cluster whose node 0 carries another machine's pin, so a
    proof that read node 0's shows."""
    material = {
        "controllerUserNode1": str(tmp_path / "bmc-user"), "controllerPasswordNode1": str(tmp_path / "bmc-password"),
        "pinnedUUIDBase64Node0": encoded("4c4c4544-0042-3510-8052-b4c04f4d4e30"),
        "pinnedSerialBase64Node0": encoded("CZJ2440AB0"),
    }
    material.update({key + "Node1": value for key, value in pins.items()})
    node = {"controller": {"endpoint": ENDPOINT, "tlsVerify": True}, "hardware": hardware, "machine": "metal",
            "name": "master-1", "physical": True, "substrate": "baremetal"}
    variables = dict(LOADER.load_from_file(str(CLUSTER / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(written(tmp_path, {"bootwright_cluster_install_material": material}))
    variables.update(one([task for task in loaded(CLUSTER / "tasks" / "apply.yml")
                          if task.get("ansible.builtin.include_tasks") == "boot.yml"], "boot each node")["vars"])
    variables.update({"containercluster_install_agent_node": node, "containercluster_install_agent_index": 1})
    variables.update(include(CLUSTER)["vars"])
    return variables


CONSUMERS = {"managed OS": managed_os, "cluster": cluster}


def scope(tmp_path, consumer, observation, pin_uuid=None, pin_serial=None, hardware=None):
    """One consumer's proof of the machine, with what its controller reported."""
    pins = {}
    if pin_uuid is not None:
        pins["pinnedUUIDBase64"] = encoded(pin_uuid)
    if pin_serial is not None:
        pins["pinnedSerialBase64"] = encoded(pin_serial)
    variables = dict(LOADER.load_from_file(str(BAREMETAL / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(CONSUMERS[consumer](tmp_path, pins, DECLARED if hardware is None else hardware))
    variables[OBSERVED] = {"observation": observation}
    return variables


def reported(uuid=UUID, serial=SERIAL):
    """What redfish_system_inspect reports; every field is "" when the system read fails."""
    return {"addresses": list(MACS), "failures": [], "manufacturer": "Dell Inc.", "media": {},
            "model": "PowerEdge R740", "power": "Off", "serial": serial, "uuid": uuid}


def accepts(task, variables):
    """Whether an assertion holds, each item evaluated as the assert action does."""
    rendering = Templar(loader=LOADER, variables=variables)
    return all(rendering.evaluate_conditional(item) for item in task[ASSERT]["that"])


def test_the_pin_is_compared_after_the_inventory_and_before_the_power_state():
    tasks = proof()
    read = one([index for index, task in enumerate(tasks) if INSPECT in task], "read the machine")
    power = one([index for index, task in enumerate(tasks)
                 if ASSERT in task and "observation.power" in json.dumps(task[ASSERT])], "compare the power state")
    assert read < tasks.index(declared()) < tasks.index(pinned()) < power
    assert tasks[read]["register"] == OBSERVED and tasks[read]["no_log"] is True
    assert pinned()[ASSERT]["quiet"] is True
    assert not {"no_log", "when"} & set(pinned())
    for task in (declared(), pinned()):
        assert not {"failed_when", "ignore_errors"} & set(task)


def test_a_material_value_is_evaluated_when_it_is_read(tmp_path):
    variables = managed_os(tmp_path, {"raw": TEMPLATE}, DECLARED)
    rendering = Templar(loader=LOADER, variables=variables)
    assert rendering.evaluate_expression(trust_as_template("bootwright_os_install_material.raw")) == 42


COMPARED = {
    "the same system": (reported(), UUID, SERIAL, True),
    "another uuid": (reported(uuid="4c4c4544-0042-3510-8052-b4c04f4d4e32"), UUID, SERIAL, False),
    "another serial": (reported(serial="CZJ2440ABD"), UUID, SERIAL, False),
    "the uuid in other case and space": (reported(uuid=" " + UUID.upper() + "\n"), UUID, SERIAL, True),
    "a pin recorded in upper case": (reported(uuid=UUID.upper()), UUID.upper(), SERIAL, True),
    "a pin in upper case against a lower-case report": (reported(), UUID.upper(), SERIAL, True),
    "the serial in surrounding space": (reported(serial=" " + SERIAL + " "), UUID, SERIAL, True),
    "the serial in other case": (reported(serial=SERIAL.lower()), UUID, SERIAL, False),
    "no pinned serial": (reported(serial="another"), UUID, None, True),
    "no pinned uuid": (reported(uuid="another"), None, SERIAL, True),
    "a pinned uuid reported empty": (reported(uuid=""), UUID, SERIAL, False),
    "a pinned serial reported empty": (reported(serial=""), UUID, SERIAL, False),
    "an unreadable controller": (reported(uuid="", serial=""), UUID, SERIAL, False),
    "a template pin against its value": (reported(uuid="42", serial="42"), TEMPLATE, TEMPLATE, False),
    "a template pin against its text": (reported(uuid=TEMPLATE, serial=TEMPLATE), TEMPLATE, TEMPLATE, True),
}


@pytest.mark.parametrize("consumer", sorted(CONSUMERS))
@pytest.mark.parametrize("observation, pin_uuid, pin_serial, same", COMPARED.values(), ids=COMPARED.keys())
def test_a_physical_target_is_compared_with_the_pin_its_machine_block_proved(
        tmp_path, consumer, observation, pin_uuid, pin_serial, same):
    assert accepts(pinned(), scope(tmp_path, consumer, observation, pin_uuid, pin_serial)) is same


@pytest.mark.parametrize("consumer", sorted(CONSUMERS))
def test_a_machine_with_no_pin_is_compared_with_nothing(tmp_path, consumer):
    assert accepts(pinned(), scope(tmp_path, consumer, reported(uuid="another", serial="another"))) is True


@pytest.mark.parametrize("consumer", sorted(CONSUMERS))
def test_a_declaration_with_no_nic_proves_nothing_and_is_refused(tmp_path, consumer):
    assert accepts(declared(), scope(tmp_path, consumer, reported())) is True
    assert accepts(declared(), scope(tmp_path, consumer, reported(), hardware={"interfaces": []})) is False


def test_a_managed_os_target_frozen_without_hardware_is_refused(tmp_path):
    assert accepts(declared(), scope(tmp_path, "managed OS", reported(), hardware={})) is False


@pytest.mark.parametrize("consumer", sorted(CONSUMERS))
def test_the_refusal_names_the_controller_and_the_remedy_and_no_identity(tmp_path, consumer):
    variables = scope(tmp_path, consumer, reported(uuid="4c4c4544-0042-3510-8052-b4c04f4d4e32", serial="CZJ2440ABD"),
                      UUID, SERIAL)
    assert accepts(pinned(), variables) is False
    message = Templar(loader=LOADER, variables=variables).template(pinned()[ASSERT]["fail_msg"])
    assert message == (
        "the machine answering at %s is not the system this operation proved for its Machine, so it must not be "
        "erased; %s" % (ENDPOINT, REMEDY))


def test_the_managed_os_hands_its_proof_the_pin_or_nothing(tmp_path):
    arguments = include(MANAGED_OS)["vars"]
    for pins, want in (
            ({"pinnedUUIDBase64": encoded(UUID), "pinnedSerialBase64": encoded(SERIAL)}, (encoded(UUID), encoded(SERIAL))),
            ({}, ("", ""))):
        rendering = Templar(loader=LOADER, variables=managed_os(tmp_path, pins, DECLARED))
        assert tuple(rendering.template(arguments[name]) for name in PINS) == want

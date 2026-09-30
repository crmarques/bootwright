"""A physical machine that answers as another system refuses before any power request.

While a physical Machine's apply is the context's current operation, the UUID
and serial its bare-metal block proved are that Machine's pin
(specs/substrates.md, Physical machine realization). A power run receives the
pin base64-encoded in bootwright_machine_power_material, reads the identity
its controller reports, and asserts they are the same system before it
includes either power task file. These cases pin that order and evaluate the
real `when` and `that` expressions over the variables the runner writes.

The runner passes those variables as `--extra-vars @request.json`, which
ansible-core loads trusted as a template, so a raw value there would be
evaluated when it is read; the scope here is loaded the same way, and a control
case proves it. Rendering the role's task files needs Ansible's controller
(DataLoader and Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import json
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "machine_power_redfish"
LOADER = DataLoader()

INSPECT = "bootwright.core.redfish_system_inspect"
ASSERT = "ansible.builtin.assert"
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
IDENTITY = "machine_power_redfish_identity"

ENDPOINT = "https://bmc.example.test/redfish/v1/Systems/1"
UUID = "4c4c4544-0042-3510-8052-b4c04f4d4e31"
SERIAL = "CZJ2440ABC"
TEMPLATE = "{{ 6 * 7 }}"
REMEDY = ("Correct spec.hardware.management.bmc.address, or destroy and apply this context so the machine is "
          "proved again.")


def tasks():
    """power.yml as a play loads it: its templates trusted."""
    loaded = LOADER.load_from_file(str(ROLE / "tasks" / "power.yml"), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def one(found, description):
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def indexed(predicate, description):
    return one([index for index, task in enumerate(tasks()) if predicate(task)], description)


def reading():
    return tasks()[indexed(lambda task: INSPECT in task, "read the reported identity")]


def refusal():
    return tasks()[indexed(lambda task: ASSERT in task and IDENTITY in json.dumps(task[ASSERT]), "compare the pin")]


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def scope(tmp_path, observation=None, pin_uuid=None, pin_serial=None, **raw):
    """The run's variables as the runner writes and ansible-core loads them, then one inspection."""
    material = {"controllerUser": str(tmp_path / "bmc-user"), "controllerPassword": str(tmp_path / "bmc-password")}
    if pin_uuid is not None:
        material["pinnedUUIDBase64"] = encoded(pin_uuid)
    if pin_serial is not None:
        material["pinnedSerialBase64"] = encoded(pin_serial)
    material.update(raw)
    written = tmp_path / "request.json"
    written.write_text(json.dumps({
        "bootwright_machine_power_digest": "d" * 64,
        "bootwright_machine_power_material": material,
        "bootwright_machine_power_request": {
            "controller": {"credentialsRef": "metal-bmc", "endpoint": ENDPOINT, "tlsVerify": True},
            "force": False,
            "identity": {"context": "lab", "object": "metal"},
            "placement": {"machine": "controller"},
            "verb": "stop",
            "version": "machine-power-redfish-v2",
        },
    }))
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    # The loader caches by path, and every case writes the same path.
    variables.update(LOADER.load_from_file(str(written), cache="none", trusted_as_template=True))
    if observation is not None:
        variables[IDENTITY] = {"observation": observation}
    return variables


def reported(uuid=UUID, serial=SERIAL):
    """What redfish_system_inspect reports; every field is "" when the system read fails."""
    return {"addresses": [], "failures": [], "manufacturer": "Dell Inc.", "media": {}, "model": "PowerEdge R740",
            "power": "On", "serial": serial, "uuid": uuid}


def gated(variables):
    rendering = Templar(loader=LOADER, variables=variables)
    gates = [rendering.evaluate_conditional(task["when"]) for task in (reading(), refusal())]
    assert gates[0] == gates[1]
    return gates[0]


def accepts(variables):
    """Whether the assertion holds, each item evaluated as the assert action does."""
    rendering = Templar(loader=LOADER, variables=variables)
    return all(rendering.evaluate_conditional(item) for item in refusal()[ASSERT]["that"])


def test_the_identity_is_read_and_compared_before_any_power_request():
    loaded = tasks()
    read = indexed(lambda task: INSPECT in task, "read the reported identity")
    compared = indexed(lambda task: ASSERT in task and IDENTITY in json.dumps(task[ASSERT]), "compare the pin")
    before = indexed(lambda task: task.get("register") == "machine_power_redfish_before", "read the power state")
    includes = [index for index, task in enumerate(loaded) if any(action in task for action in INCLUDES)]
    assert len(includes) == 2, "the walk no longer sees both power task files included"
    assert before < read < compared < min(includes)
    assert loaded[read]["register"] == IDENTITY and loaded[read]["no_log"] is True
    arguments = loaded[read][INSPECT]
    assert (arguments["endpoint"], arguments["verify"]) == (
        "{{ bootwright_machine_power_request.controller.endpoint }}",
        "{{ bootwright_machine_power_request.controller.tlsVerify }}")
    assert loaded[compared][ASSERT]["quiet"] is True
    assert "no_log" not in loaded[compared]
    assert loaded[read]["when"] == loaded[compared]["when"]
    for task in (loaded[read], loaded[compared]):
        assert not {"failed_when", "ignore_errors"} & set(task)


def test_a_material_value_is_evaluated_when_it_is_read(tmp_path):
    variables = scope(tmp_path, raw=TEMPLATE)
    rendering = Templar(loader=LOADER, variables=variables)
    assert rendering.evaluate_expression(trust_as_template("bootwright_machine_power_material.raw")) == 42


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


@pytest.mark.parametrize("observation, pin_uuid, pin_serial, same", COMPARED.values(), ids=COMPARED.keys())
def test_a_pinned_machine_is_compared_with_what_its_controller_reports(tmp_path, observation, pin_uuid, pin_serial, same):
    variables = scope(tmp_path, observation, pin_uuid, pin_serial)
    assert gated(variables) is True
    assert accepts(variables) is same


def test_a_machine_with_no_pin_compares_nothing(tmp_path):
    assert gated(scope(tmp_path)) is False


def test_the_refusal_names_the_machine_both_identities_and_the_remedy(tmp_path):
    variables = scope(tmp_path, reported(uuid="4c4c4544-0042-3510-8052-b4c04f4d4e32", serial=TEMPLATE),
                      UUID, TEMPLATE)
    assert accepts(variables) is False
    message = Templar(loader=LOADER, variables=variables).template(refusal()[ASSERT]["fail_msg"])
    assert message == (
        "Machine/metal answers at %s as UUID '4c4c4544-0042-3510-8052-b4c04f4d4e32' and serial '%s', but this "
        "context's current apply proved UUID '%s' and serial '%s', so no power request was sent. %s"
        % (ENDPOINT, TEMPLATE, UUID, TEMPLATE, REMEDY))


def test_the_refusal_prints_each_identity_bounded_and_printable(tmp_path):
    """What a controller reported, then or now, reaches the retained output printable and at most 128 characters.

    Every character str.isprintable() refuses is removed before the value is
    cut, so a value padded with them still shows its first 128 printable
    characters: the C0 and C1 controls and DEL, and the format characters and
    separators that print as nothing or reorder what follows them, such as a
    bidi override or isolate, a zero-width space, a byte-order mark, a line or
    paragraph separator and a space other than U+0020. A printable letter
    outside ASCII is kept. Each of the four identities is longer than the bound
    and carries such characters, so each one's removal and cut is proved on its
    own.
    """
    variables = scope(tmp_path, reported(uuid="\x1b[31m" + UUID + "\u202e\x1b]0;owned\x07" + "U" * 200,
                                         serial="S" * 100 + "\r\n\t\x00\x7f\x85\x9b\u200b\ufeff\u2028\u2029\xa0\u3000"
                                         + "T" * 100),
                      UUID + "\x08\x9b2J\u2066" + "V" * 200, "\x00\x1b\x9b\ufeff\u200b\xe9" + "P" * 300)
    assert accepts(variables) is False
    message = Templar(loader=LOADER, variables=variables).template(refusal()[ASSERT]["fail_msg"])
    assert message == (
        "Machine/metal answers at %s as UUID '%s' and serial '%s', but this context's current apply "
        "proved UUID '%s' and serial '%s', so no power request was sent. %s"
        % (ENDPOINT, "[31m" + UUID + "]0;owned" + "U" * 80, "S" * 100 + "T" * 28, UUID + "2J" + "V" * 90,
           "\xe9" + "P" * 127, REMEDY))
    assert message.isprintable()

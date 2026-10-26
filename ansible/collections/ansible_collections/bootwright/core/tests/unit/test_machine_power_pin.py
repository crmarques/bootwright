"""A physical machine that answers as another system refuses before any power request.

While a physical Machine's apply is the context's current operation, the UUID
and serial its bare-metal block proved are that Machine's pin
(specs/substrates.md, Physical machine realization). A power run receives the
pin base64-encoded in bootwright_machine_power_material, reads the identity
its controller reports, and asserts they are the same system before it
includes either power task file; when they are not, it first names the
refusal to its runner, which reports it as the power service's own diagnostic.
These cases pin that order and evaluate the real `when` and `that`
expressions over the variables the runner writes.

The runner passes those variables as `--extra-vars @request.json` with every
string marked unsafe, so ansible-core reads them as data and never renders them
(test_request_strings_are_data.py). The scope here is loaded trusted as a
template on purpose, as ansible-core would read it unmarked, so the pin's
encoding alone keeps a raw value from being evaluated, and a control case
proves it. Rendering the role's task files needs Ansible's controller
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
PROTOCOL = "bootwright.core.machine_power_protocol"
ASSERT = "ansible.builtin.assert"
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
IDENTITY = "machine_power_redfish_identity"
PIN = "pinnedUUIDBase64"

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


def is_identity_read(task):
    return (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "state_read" and "inspect" in str(task.get("vars", {}))


def reading():
    return tasks()[indexed(is_identity_read, "read the reported identity")]


def compares(task):
    return ASSERT in task and IDENTITY in json.dumps(task[ASSERT]) and PIN in json.dumps(task[ASSERT])


def refusal():
    return tasks()[indexed(compares, "compare the pin")]


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
    return Templar(loader=LOADER, variables=variables).evaluate_conditional(refusal()["when"])


def accepts(variables):
    """Whether the assertion holds, each item evaluated as the assert action does."""
    rendering = Templar(loader=LOADER, variables=variables)
    return all(rendering.evaluate_conditional(item) for item in refusal()[ASSERT]["that"])


def test_the_identity_is_read_and_compared_before_any_power_request():
    loaded = tasks()
    read = indexed(is_identity_read, "read the reported identity")
    stopped = indexed(lambda task: ASSERT in task and IDENTITY + ".msg" in json.dumps(task[ASSERT]),
                      "stop at a read the controller did not answer")
    compared = indexed(compares, "compare the pin")
    before = indexed(lambda task: "machine_power_redfish_before" in task.get("ansible.builtin.set_fact", {}), "read the power state")
    includes = [index for index, task in enumerate(loaded) if any(action in task for action in INCLUDES)]
    assert len(includes) == 2, "the walk no longer sees both power task files included"
    assert before < read < stopped < compared < min(includes)
    arguments = loaded[read]["vars"]["bootwright_machine_port"]
    assert "machine_power_redfish_access" in arguments and "'inspect': true" in arguments
    retained = loaded[read + 1]
    assert IDENTITY in retained["ansible.builtin.set_fact"] and retained["no_log"] is True
    assert retained["when"] == loaded[read]["when"] == "machine_power_redfish_physical | bool"
    assert loaded[compared][ASSERT]["quiet"] is True
    assert "no_log" not in loaded[compared]
    # The read tolerates its own failure only so that the controller's message
    # reaches the output uncensored: the step after it refuses there, so a read
    # that failed is never compared as an empty identity.
    native = LOADER.load_from_file(str(ROLE.parent / "substrate_baremetal_machine/tasks/state_read.yml"))
    inspect = one([task for task in native if INSPECT in task], "inspect via state_read")
    assert inspect["failed_when"] is False and inspect["no_log"] is True
    assert "ignore_errors" not in loaded[read]
    assert loaded[stopped][ASSERT]["that"] == [IDENTITY + ".msg is not defined"]
    assert not {"failed_when", "ignore_errors", "no_log", "when"} & set(loaded[stopped])
    assert not {"failed_when", "ignore_errors"} & set(loaded[compared])


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
    assert names_the_refusal(scope(tmp_path)) is False


def naming():
    return tasks()[indexed(lambda task: (task.get(PROTOCOL) or {}).get("reason") == "identity-mismatch", "name the refusal")]


def names_the_refusal(variables):
    return Templar(loader=LOADER, variables=variables).evaluate_conditional(naming()["when"])


def test_the_runner_is_told_of_the_refusal_before_the_run_fails():
    """The runner reports a named refusal as the power service's own diagnostic,
    which carries the Machine and the remedy (internal/machine/power), so the
    refusal is named after the identity is read and before the assert fails. A
    read the controller did not answer stops the run first, so its empty
    identity is never named as another system."""
    loaded = tasks()
    read = indexed(is_identity_read, "read the reported identity")
    stopped = indexed(lambda task: ASSERT in task and IDENTITY + ".msg" in json.dumps(task[ASSERT]),
                      "stop at a read the controller did not answer")
    named = indexed(lambda task: (task.get(PROTOCOL) or {}).get("reason") == "identity-mismatch", "name the refusal")
    compared = indexed(compares, "compare the pin")
    assert read < stopped < named < compared
    assert loaded[named][PROTOCOL] == {"phase": "refused", "reason": "identity-mismatch"}
    assert loaded[named]["no_log"] is True
    assert not {"failed_when", "ignore_errors", "register"} & set(loaded[named])


@pytest.mark.parametrize("observation, pin_uuid, pin_serial, same", COMPARED.values(), ids=COMPARED.keys())
def test_the_refusal_is_named_exactly_when_the_machine_is_refused(tmp_path, observation, pin_uuid, pin_serial, same):
    variables = scope(tmp_path, observation, pin_uuid, pin_serial)
    assert names_the_refusal(variables) is (not same)


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


HOST_UUID = "8e1b6a3c-0d7f-4c2a-9a51-3f0c2d4b5e61"
HOST_MACS = "aa:bb:cc:00:00:01,aa:bb:cc:00:00:02"


def check(reason):
    """The refusal a run names for reason, and the assert that refuses it."""
    loaded = tasks()
    named = indexed(lambda task: (task.get(PROTOCOL) or {}).get("reason") == reason, "name " + reason)
    asserted = next(index for index in range(named + 1, len(loaded)) if ASSERT in loaded[index])
    return loaded[named], loaded[asserted]


def holds(variables, task):
    rendering = Templar(loader=LOADER, variables=variables)
    gate = task.get("when", True)
    gates = gate if isinstance(gate, list) else [gate]
    if not all(rendering.evaluate_conditional(item) for item in gates):
        return None
    return all(rendering.evaluate_conditional(item) for item in task[ASSERT]["that"])


HOST = {
    "a foreign machine": (dict(reported(), addresses=["52:54:00:9a:1b:01"]), True),
    "the controller host's uuid": (dict(reported(uuid=" " + HOST_UUID.upper()), addresses=["52:54:00:9a:1b:01"]), False),
    "one of the controller host's MACs": (dict(reported(), addresses=["52:54:00:9a:1b:01", "aa:bb:cc:00:00:02"]), False),
}


# A power command never reaches the host that runs Bootwright: a machine whose
# controller reports this host's own UUID, in any case or space, or any of its
# MAC addresses refuses before any power request, and names the refusal first.
@pytest.mark.parametrize("observation, foreign", HOST.values(), ids=HOST.keys())
def test_a_machine_reporting_the_controller_hosts_identity_refuses(tmp_path, observation, foreign):
    variables = scope(tmp_path, observation, substrate="baremetal", controllerHostUUIDBase64=encoded(HOST_UUID),
                      controllerHostMACsBase64=encoded(HOST_MACS))
    named, asserted = check("controller-host")
    assert holds(variables, asserted) is foreign
    rendering = Templar(loader=LOADER, variables=variables)
    assert all(rendering.evaluate_conditional(item) for item in named["when"]) is (not foreign)
    compared = indexed(compares, "compare the pin")
    assert tasks().index(asserted) < compared


DECLARED = "52:54:00:9a:1b:01,52:54:00:9a:1b:02"
PROVED = {
    "exactly the declared set": (dict(reported(), addresses=["52:54:00:9a:1b:02", "52:54:00:9a:1b:01"]), True),
    "an extra reported address": (
        dict(reported(), addresses=["52:54:00:9a:1b:01", "52:54:00:9a:1b:02", "52:54:00:9a:1b:09"]), False),
    "a declared address missing": (dict(reported(), addresses=["52:54:00:9a:1b:01"]), False),
    "an incomplete inventory": (dict(reported(), addresses=["52:54:00:9a:1b:01", "52:54:00:9a:1b:02"],
                                     failures=["member[2] could not be read"]), False),
}


# A physical Machine with no pin is proved by the full target proof before any
# power request: a complete inventory reporting exactly its declared MAC set.
@pytest.mark.parametrize("observation, proved", PROVED.values(), ids=PROVED.keys())
def test_an_unpinned_machine_must_report_exactly_its_declared_set(tmp_path, observation, proved):
    variables = scope(tmp_path, observation, substrate="baremetal", expectedMACsBase64=encoded(DECLARED),
                      controllerHostMACsBase64=encoded(HOST_MACS))
    named, asserted = check("hardware-mismatch")
    assert holds(variables, asserted) is proved
    rendering = Templar(loader=LOADER, variables=variables)
    assert all(rendering.evaluate_conditional(item) for item in named["when"]) is (not proved)


def test_a_pinned_machine_skips_the_unpinned_proof(tmp_path):
    variables = scope(tmp_path, dict(reported(), addresses=["52:54:00:9a:1b:09"]), UUID, SERIAL, substrate="baremetal",
                      controllerHostMACsBase64=encoded(HOST_MACS))
    assert holds(variables, check("hardware-mismatch")[1]) is None

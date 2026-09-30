"""A bare-metal apply proves a machine and writes nothing to it.

Its claim on the machine is the operation's reservation, taken when the
operation registers, not by the adapter (specs/substrates.md, Physical machine
realization). So every proving apply, the first included, publishes no change:
the first proof and a replay of it are the same read. These cases pin that the
proof publishes a literal `unchanged`, that it does so under every proving
observation, and that the apply runs nothing but reads and the protocol.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import pathlib
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import substrate_physical_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_baremetal_machine"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.substrate_physical_protocol"
# What an apply may run: assertions over its request, the protocol, and one
# read of the machine through its controller.
READS = ("ansible.builtin.assert", PROTOCOL, "bootwright.core.redfish_system_inspect")

DIGEST = "c" * 64
DECLARED = ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"]
ENDPOINT = "https://bmc.example.test/redfish/v1/Systems/1"


def observation(**overrides):
    """What redfish_system_inspect reports for the declared machine."""
    observed = {
        "addresses": list(DECLARED),
        "failures": [],
        "manufacturer": "Acme",
        "model": "R740",
        "power": "Off",
        "serial": "SN1",
        "uuid": "uuid-1",
    }
    observed.update(overrides)
    return observed


PROOFS = {
    "powered off with exactly the declared addresses": observation(),
    "powered on with an address beyond the declaration": observation(
        power="On", addresses=DECLARED + ["aa:bb:cc:dd:ee:09"]),
}


def tasks(entry="apply.yml"):
    """One of the role's task files as a play loads it: its templates trusted."""
    loaded = LOADER.load_from_file(str(ROLE / "tasks" / entry), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def completes(task):
    arguments = task.get(PROTOCOL)
    return isinstance(arguments, dict) and arguments.get("phase") == "completed"


def completion(entry="apply.yml"):
    found = [task for task in tasks(entry) if completes(task)]
    assert len(found) == 1, "%d tasks publish the proof" % len(found)
    return found[0][PROTOCOL]


def scope(observed):
    """The role's defaults, the frozen request's addresses and one inspection."""
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(
        bootwright_substrate_physical_request={
            "controller": {"endpoint": ENDPOINT}, "hardware": [{"macAddress": address} for address in DECLARED]},
        bootwright_substrate_physical_digest=DIGEST,
        substrate_baremetal_machine_observed={"observation": observed},
    )
    return variables


def run(arguments):
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=arguments)
    return module.run(task_vars={})


def test_the_proof_publishes_a_literal_no_change():
    outcome = completion()["outcome"]
    assert outcome == "unchanged"
    assert "{{" not in outcome


@pytest.mark.parametrize("observed", list(PROOFS.values()), ids=list(PROOFS))
def test_a_proving_apply_publishes_no_change(observed, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = Templar(loader=LOADER, variables=scope(observed)).template(completion())
    for attempt in ("the first proof", "a replay"):
        assert run(arguments) == {"changed": False}, attempt
    assert [message["outcome"] for message in published] == ["unchanged", "unchanged"]
    assert [message["evidence"]["postcondition"] for message in published] == [True, True]
    # Determinism only. Both runs read one observation, and every completed
    # publication returns {'changed': False}, so neither this equality nor that
    # return proves the replay: the published outcome above does.
    assert published[0] == published[1]


def test_a_bare_metal_apply_writes_nothing():
    loaded = tasks()
    for task in loaded:
        actions = [key for key in task if "." in key]
        assert len(actions) == 1 and actions[0] in READS, "%s runs %s" % (task.get("name"), actions)
    assert len([task for task in loaded if completes(task)]) == 1


# A reported identity the evidence may not carry refuses naming the controller
# the frozen request declares, which both the apply's proof and the observe
# run's observation hand their publication.
@pytest.mark.parametrize("entry", ["apply.yml", "observe.yml"])
def test_an_unprintable_identity_refuses_naming_the_declared_controller(entry, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = Templar(loader=LOADER, variables=scope(observation(serial="SN\u202e1"))).template(completion(entry))
    assert run(arguments) == {
        "failed": True,
        "msg": "the management controller at %s reported a SerialNumber holding a character that is not printable"
               % ENDPOINT,
    }
    assert not published

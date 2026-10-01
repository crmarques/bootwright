"""A bare-metal apply proves a machine and writes nothing to it.

Its claim on the machine is the operation's reservation, taken when the
operation registers, not by the adapter (specs/substrates.md, Physical machine
realization). So every proving apply, the first included, publishes no change:
the first proof and a replay of it are the same read. These cases pin that the
proof publishes a literal `unchanged`, that it does so under every proving
observation, and that the apply runs nothing but reads and the protocol; and
that a refused proof's reason is printed where the operator is sent to read it.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import substrate_physical_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_baremetal_machine"
COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]
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


# What an operator is told about a refused proof is its refusal in the
# attempt's retained output, which holds what the adapter printed, raw
# (specs/cli/output.md, Private operation logs). The publication reads no bound
# material, so its result is not censored, and the refusal names fields, counts
# and the controller, never a reported value.
REFUSALS = {
    "a declared address the controller does not report": (
        observation(addresses=DECLARED[:1], serial="SN-missing", uuid="uuid-missing"),
        "the machine was not proved to be the one this Machine declares; missing: 1 of 2 declared addresses"),
    "a serial holding a bidi override": (
        observation(serial="SN\u202e1", uuid="uuid-bidi"),
        "the management controller at %s reported a SerialNumber holding a character that is not printable"
        % ENDPOINT),
    "a UUID of 129 characters": (
        observation(serial="SN-long", uuid="u" * 129),
        "the management controller at %s reported a UUID longer than 128 characters" % ENDPOINT),
}
ENTRIES = ("apply.yml", "observe.yml")
FAILED = "fatal: [localhost]: FAILED! => "


def label(entry, case):
    return "%s: %s" % (entry, case)


@pytest.fixture(scope="module")
def retained(tmp_path_factory):
    """What the adapter prints under each refused publication, by its label.

    The role's own publication tasks run through ansible-playbook with what the
    shipped ansible.cfg and the runner set for the adapter's streams: the
    bootwright.core.censored callback, no verbosity, no task arguments
    displayed, no color, and standard output and error in one stream
    (ansible/ansible.cfg,
    internal/reconciliation/ansiblerunner/process_linux_amd64.go). The request
    and the observations arrive as extra variables, as a run's request does,
    so no value is in the playbook text an error's origin quotes. Each
    publication continues past its refusal, so one run prints every refusal.
    """
    work = tmp_path_factory.mktemp("retained")
    defaults = scope(observation())
    del defaults["substrate_baremetal_machine_observed"]
    extra = {name: defaults.pop(name) for name in ("bootwright_substrate_physical_request", "bootwright_substrate_physical_digest")}
    extra["refused_observations"] = []
    published = []
    for entry in ENTRIES:
        task = [task for task in tasks(entry) if completes(task)][0]
        for case, (observed, _refusal) in REFUSALS.items():
            observed_at = "{{ refused_observations[%d] }}" % len(extra["refused_observations"])
            extra["refused_observations"].append(observed)
            published.append(dict(task, name=label(entry, case), ignore_errors=True,
                                  vars={"substrate_baremetal_machine_observed": {"observation": observed_at}}))
    (work / "request.json").write_text(json.dumps(extra))
    (work / "refusals.yml").write_text(json.dumps([{
        "hosts": "localhost", "gather_facts": False, "connection": "local", "vars": defaults, "tasks": published,
    }], indent=1))
    (work / "ansible.cfg").write_text("")
    environment = dict(
        os.environ,
        ANSIBLE_CONFIG=str(work / "ansible.cfg"),
        ANSIBLE_HOME=str(work / "home"),
        ANSIBLE_LOCAL_TEMP=str(work / "local"),
        ANSIBLE_COLLECTIONS_PATH=str(COLLECTIONS),
        ANSIBLE_STDOUT_CALLBACK="bootwright.core.censored",
        ANSIBLE_DISPLAY_ARGS_TO_STDOUT="False",
        ANSIBLE_LOAD_CALLBACK_PLUGINS="0",
        ANSIBLE_NOCOLOR="1",
        ANSIBLE_FORCE_COLOR="0",
        LC_ALL="C.UTF-8",
    )
    ran = subprocess.run(
        [sys.executable, "-m", "ansible.cli.playbook", "-i", "localhost,",
         "--extra-vars", "@" + str(work / "request.json"), str(work / "refusals.yml")],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=environment, cwd=str(work),
        check=False)
    printed = ran.stdout.decode()
    sections = {}
    for section in printed.split("TASK [")[1:]:
        name, _banner, body = section.partition("]")
        sections[name] = body.split("PLAY RECAP", 1)[0]
    assert set(sections) == {label(entry, case) for entry in ENTRIES for case in REFUSALS}, printed
    return sections


@pytest.mark.parametrize("entry", ENTRIES)
@pytest.mark.parametrize("case", list(REFUSALS))
def test_each_refusal_reaches_the_retained_output(retained, entry, case):
    observed, refusal = REFUSALS[case]
    printed = retained[label(entry, case)]
    results = [json.loads(line[len(FAILED):]) for line in printed.splitlines() if line.startswith(FAILED)]
    assert [result.get("msg") for result in results] == [refusal], printed
    assert "censored" not in printed
    assert observed["serial"] not in printed and observed["uuid"] not in printed

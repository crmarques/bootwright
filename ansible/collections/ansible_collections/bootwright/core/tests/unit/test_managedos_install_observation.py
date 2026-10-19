"""An installation's observation reads what the resolution it serves decides.

The apply's resolution reads the machine as an apply proves it: its identity
channel, its fleet account and the power and media its controller reports. A
removal's resolution reads the published content the removal takes back and,
for a registering request alone, the guest's registration through its identity
channel, so the capability scopes that observation with the material value
`observes: removal` and none of the apply's machine reads runs for it. Either way the
observation publishes the presence form, which carries the power the
controller reported: the absence form reports none, and without it an apply
stopped before it published anything could never prove it had no effect.
Rendering the task files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
DIGEST = "1" * 64
PUBLISH = "bootwright.core.managedos_install_protocol"
INSPECT = "bootwright.core.managedos_install_inspect"
READ = "bootwright.core.redfish_system_read"
NOTHING = {"image": False, "private": False, "tree": False, "treeContent": False}


def tasks():
    """observe.yml's tasks in the order a play reaches them, each block opened
    and its condition carried onto every task inside it, as a play applies it."""
    def opened(listed, inherited):
        for task in listed:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section) or [], conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True), []))


def defaults():
    return LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True)


def scope(observes, unregister="guest", **registered):
    """The variables one observation's tasks see, the capability having sent
    observes, and for a removal whether it reads the guest, as material values
    when it names them."""
    material = {"controllerUser": "/run/material/bmc-user", "controllerPassword": "/run/material/bmc-password"}
    if observes is not None:
        material["observes"] = observes
    if unregister is not None:
        material["unregister"] = unregister
    variables = {
        "bootwright_os_install_digest": DIGEST,
        "bootwright_os_install_material": material,
        "bootwright_os_install_request": {"address": "198.51.100.11"},
        "managedos_install_anaconda_observes_removal": defaults()["managedos_install_anaconda_observes_removal"],
        "managedos_install_anaconda_unregisters": defaults()["managedos_install_anaconda_unregisters"],
    }
    variables.update(registered)
    return variables


def runs(task, variables):
    """Whether every condition of one task holds under variables."""
    conditions = task.get("when", [])
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(condition)
               for condition in (conditions if isinstance(conditions, list) else [conditions]))


def machine_reads(scoped="not (managedos_install_anaconda_observes_removal | bool)"):
    """The tasks of one scope that read the machine rather than the published content."""
    found = [task for task in tasks() if scoped in task["when"]]
    found = [task for task in found
             if READ in task
             or task.get("ansible.builtin.include_tasks") in (
                 "identity.yml", "reachable.yml", "network_observe.yml", "host_security.yml", "registration_observe.yml")
             or (task.get("bootwright.core.lifecycle_scratch") or {}).get("state", "present") == "present" and "bootwright.core.lifecycle_scratch" in task]
    # Private connection files, the identity, fleet, network, FIPS, registration and controller reads,
    # or a registering removal's private connection files, identity and registration reads.
    assert len(found) == (7 if scoped.startswith("not") else 3)
    return found


@pytest.mark.parametrize("observes", [None, ""], ids=["unscoped", "empty"])
def test_an_apply_observation_reads_the_machine(observes):
    variables = scope(observes, managedos_install_anaconda_host_key="ssh-ed25519 AAAAHOST",
                      managedos_install_anaconda_reachable={"rc": 0},
                      bootwright_os_install_request={"address": "198.51.100.11", "registration": {}})
    assert all(runs(task, variables) for task in machine_reads())


def test_a_removal_observation_reads_only_the_published_content():
    variables = scope("removal", managedos_install_anaconda_host_key="ssh-ed25519 AAAAHOST")
    assert not any(runs(task, variables) for task in machine_reads())
    assert not any(runs(task, variables) for task in machine_reads(REMOVAL))
    kept = [task for task in tasks() if runs(task, variables)]
    assert any(INSPECT in task for task in kept)
    assert (kept[-1].get(PUBLISH) or {}).get("phase") == "completed"


REMOVAL = "managedos_install_anaconda_observes_removal | bool"


def test_a_registering_removal_observation_reads_the_guest_registration_alone():
    marker = '{"request":"%s"}' % DIGEST
    variables = scope("removal", managedos_install_anaconda_host_key="ssh-ed25519 AAAAHOST",
                      managedos_install_anaconda_answered=True, managedos_install_anaconda_marker=marker,
                      bootwright_os_install_request={"address": "198.51.100.11", "registration": {}})
    variables["bootwright_os_install_material"]["marker"] = marker
    variables.update(next(task for task in LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True)
                          if task.get("name") == "Read the registration a removal takes back")["vars"])
    assert not any(runs(task, variables) for task in machine_reads())
    assert all(runs(task, variables) for task in machine_reads(REMOVAL))


def published(variables, monkeypatch):
    """The evidence the observation's completion publishes under variables."""
    completion = tasks()[-1]
    arguments = Templar(loader=LOADER, variables=variables).template(completion[PUBLISH])
    emitted = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: emitted.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=arguments)
    assert module.run(task_vars={}) == {"changed": False}
    return emitted[0]["evidence"]


def test_the_observation_never_publishes_the_absence_form():
    assert "removed" not in tasks()[-1][PUBLISH]


# The machine is off, holds no marker and nothing is published: an apply stopped
# before it published anything. The presence form carries that power, which is
# what positive no effect reads.
def test_an_apply_stopped_before_it_published_anything_publishes_its_power(monkeypatch):
    evidence = published(scope(
        None,
        managedos_install_anaconda_observed={"observation": NOTHING},
        managedos_install_anaconda_controller={"changed": False, "media": "", "power": "Off"},
    ), monkeypatch)
    assert evidence["absent"] is False
    assert evidence["power"] == "Off"
    assert evidence["marker"] == ""
    assert not any(evidence[name] for name in ("image", "private", "tree", "treeContent"))


# A removal stopped while it deleted the tree left the directory without its
# marker; the scoped observation reports it and nothing of the machine.
def test_a_removal_observation_publishes_the_content_left_and_nothing_of_the_machine(monkeypatch):
    evidence = published(scope(
        "removal",
        managedos_install_anaconda_observed={"observation": dict(NOTHING, treeContent=True)},
        managedos_install_anaconda_controller={"changed": False, "skipped": True},
    ), monkeypatch)
    assert evidence["treeContent"] is True
    assert evidence["tree"] is False
    assert (evidence["marker"], evidence["hostKey"], evidence["power"], evidence["media"]) == ("", "", "", "")
    assert evidence["reachable"] is False


# The observation that resolves either an apply or a removal reads the staging
# tree and the work area through the paths the role derives, so a removal whose
# only remnant is either one never resolves as withdrawn.
@pytest.mark.parametrize("observes", [None, "removal"], ids=["unscoped", "removal"])
def test_the_observation_reports_what_a_killed_apply_left(tmp_path, monkeypatch, observes):
    tree = tmp_path / "public" / "os" / "rhel-9-8" / "tree"
    parent = tmp_path / "var" / "lib" / "bootwright-install"
    work = parent / "work"
    (tree.parent / "tree.staging" / "Packages").mkdir(parents=True)
    # The role creates the root-only parent before the work area beneath it.
    parent.mkdir(parents=True, mode=0o700)
    work.mkdir()
    variables = scope(observes, managedos_install_anaconda_controller={"changed": False, "media": "", "power": "Off"})
    variables["bootwright_os_install_request"].update({
        "image": {"path": str(tmp_path / "public" / "os" / "rhel-01" / "install.iso")},
        "tree": {"path": str(tree)},
    })
    variables["managedos_install_anaconda_staged_tree"] = defaults()["managedos_install_anaconda_staged_tree"]
    variables["managedos_install_anaconda_work"] = str(work)
    inspection = next(task for task in tasks() if INSPECT in task)
    assert runs(inspection, variables)
    arguments = Templar(loader=LOADER, variables=variables).template(inspection[INSPECT])
    # The module's argument spec: staging defaults to empty, work is required.
    observation = observe(arguments["request"], arguments.get("staging", ""), arguments["work"])
    variables[inspection["register"]] = {"changed": False, "observation": observation}
    evidence = published(variables, monkeypatch)
    assert (evidence["treeStaging"], evidence["work"]) == (True, True)
    assert not any(evidence[name] for name in ("image", "private", "tree", "treeContent"))


# A removal reads a registering Machine that answers with another marker or none
# as holding nothing of this installation, and leaves the registration of one
# that does not answer unread, which Go never reads as released.
@pytest.mark.parametrize("answered, marker, state", [
    (True, '{"request":"another"}', "absent"),
    (True, "", "absent"),
    (False, "", ""),
    (False, '{"request":"another"}', ""),
    (False, '{"request":"%s"}' % DIGEST, ""),
], ids=["answered-another", "answered-none", "unanswered", "unanswered-another", "unanswered-ours"])
def test_a_removal_observation_never_reads_an_unanswered_machine_as_released(answered, marker, state, monkeypatch):
    variables = scope("removal", managedos_install_anaconda_host_key="", managedos_install_anaconda_answered=answered,
                      managedos_install_anaconda_marker=marker,
                      managedos_install_anaconda_observed={"observation": NOTHING},
                      managedos_install_anaconda_controller={"changed": False, "skipped": True},
                      bootwright_os_install_request={"address": "198.51.100.11", "registration": {}})
    variables["bootwright_os_install_material"]["marker"] = '{"request":"%s"}' % DIGEST
    variables.update(next(task for task in LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True)
                          if task.get("name") == "Read the registration a removal takes back")["vars"])
    named = [task for task in tasks() if task.get("name") == "Name a machine holding nothing of this installation"]
    assert len(named) == 1 and REMOVAL in named[0]["when"]
    if runs(named[0], variables):
        fact = named[0]["ansible.builtin.set_fact"]["managedos_install_anaconda_registration"]
        variables["managedos_install_anaconda_registration"] = Templar(loader=LOADER, variables=variables).template(fact)
    assert runs(named[0], variables) is (state == "absent")
    assert not any(runs(task, variables) for task in tasks() if task.get("ansible.builtin.include_tasks") == "registration_observe.yml")
    assert published(variables, monkeypatch)["registration"] == {"consumer": "", "state": state}


def validated(variables):
    """Whether observe.yml's request validation holds under variables."""
    checks = LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True)[0]["ansible.builtin.assert"]["that"]
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(check) for check in checks if "unregister" in check)


# The capability decides whether a removal reads the guest: a Machine whose
# apply never recorded its marker proof never registered, and one the operator
# unregistered outside Bootwright is not read. Neither connects to the guest,
# and its evidence carries no registration and does not require one.
@pytest.mark.parametrize("unregister", ["guest", "skip", None], ids=["guest", "skip", "missing"])
def test_removal_observation_reads_the_guest_only_when_told_to_unregister(unregister, monkeypatch):
    marker = '{"request":"%s"}' % DIGEST
    variables = scope("removal", unregister, managedos_install_anaconda_host_key="ssh-ed25519 AAAAHOST",
                      managedos_install_anaconda_answered=True, managedos_install_anaconda_marker=marker,
                      managedos_install_anaconda_observed={"observation": NOTHING},
                      managedos_install_anaconda_controller={"changed": False, "skipped": True},
                      bootwright_os_install_request={"address": "198.51.100.11", "registration": {}})
    variables["bootwright_os_install_material"]["marker"] = marker
    variables.update(next(task for task in LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True)
                          if task.get("name") == "Read the registration a removal takes back")["vars"])
    assert validated(variables) is (unregister is not None)
    assert validated(scope(None, None, bootwright_os_install_request={"registration": {}}))
    assert validated(scope("removal", None))
    unregisters = Templar(loader=LOADER, variables=variables).template(variables["managedos_install_anaconda_unregisters"])
    assert unregisters is (unregister == "guest")
    if unregister is None:
        return
    reads = [runs(task, variables) for task in machine_reads(REMOVAL)]
    assert all(reads) if unregister == "guest" else not any(reads)
    assert not any(runs(task, variables) for task in machine_reads())
    completion = tasks()[-1][PUBLISH]
    required = Templar(loader=LOADER, variables=variables).template(completion["registrationRequired"])
    assert required is (unregister == "guest")
    if unregister == "skip":
        assert published(variables, monkeypatch)["registration"] == {"consumer": "", "state": ""}

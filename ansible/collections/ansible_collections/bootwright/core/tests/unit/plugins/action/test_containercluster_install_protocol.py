"""An installation is complete only when the cluster itself says so.

The role reads ClusterVersion through the build's kubeconfig and resolves
`completed` from three fields, printed one per line by its JSONPath read: the
status of the `Available` condition, then `status.history[0].state` and
`status.history[0].version`. Their values are upstream's
(https://raw.githubusercontent.com/openshift/api/master/config/v1/): the
condition status is `ConditionTrue` = "True" (types_cluster_operator.go), the
condition type `OperatorAvailable` = "Available" (types_cluster_operator.go),
and an update history entry's state is `CompletedUpdate` = "Completed" or
`PartialUpdate` = "Partial" (types_cluster_version.go). The cluster version
operator opens the first history entry Partial at the desired release and marks
it Completed once the release is applied, and it defaults `Available` to False
until then (pkg/cvo/status.go, mergeOperatorHistory and
updateClusterVersionStatus, in openshift/cluster-version-operator
release-4.21).
"""

from __future__ import annotations

import pathlib
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import containercluster_install_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[4] / "roles" / "containercluster_install_agent"
LOADER = DataLoader()
DIGEST = "1" * 64
IDENTITY = "4245f5e001d445663c189f04c0a614168ed3c05a522d38680d1aa1ca248d6049"
RELEASE = "4.21.15"


def state(**overrides):
    """What the role's state read resolves for the lab-sno cluster, installed."""
    values = {
        "cluster": IDENTITY,
        "completed": True,
        "media": [],
        "missing": [],
        "powered": ["sno-01"],
        "release": RELEASE,
    }
    values.update(overrides)
    return values


def arguments(**overrides):
    values = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "identity": IDENTITY, "state": state()}
    values.update(overrides)
    return values


def test_a_cluster_reporting_its_installation_completed_proves_the_postcondition():
    found = protocol.evidence(arguments(), DIGEST, False)
    assert found["completed"] is True
    assert found["postcondition"] is True
    assert protocol.unproved(found) == []


# A cluster answering with this build's identity, at the declared release and
# whole, is what an apply interrupted during the installation wait leaves. It
# is still installing, so it proves nothing, and the refusal says why.
def test_a_cluster_still_installing_proves_no_postcondition():
    found = protocol.evidence(arguments(state=state(completed=False, media=["sno-01"])), DIGEST, False)
    assert found["completed"] is False
    assert found["postcondition"] is False
    assert "the cluster does not report its installation completed at the declared release" in protocol.unproved(found)


# Only a real boolean true counts: a string, or a state read that resolved no
# completion at all, is an installation not yet done.
@pytest.mark.parametrize("completed", [False, "True", "true", 1, None], ids=repr)
def test_only_a_boolean_completion_counts(completed):
    found = protocol.evidence(arguments(state=state(completed=completed)), DIGEST, False)
    assert found["completed"] is False
    assert found["postcondition"] is False


def test_a_state_without_a_completion_proves_no_postcondition():
    without = state()
    del without["completed"]
    assert protocol.evidence(arguments(state=without), DIGEST, False)["postcondition"] is False


# A removal proves only that the media is gone. The cluster it retains is still
# read, so the evidence records whether that cluster reports itself complete.
def test_a_removal_records_the_completion_without_requiring_it():
    for completed in (True, False):
        found = protocol.evidence(arguments(state=state(completed=completed)), DIGEST, True)
        assert found["postcondition"] is True
        assert found["completed"] is completed


def action(args):
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module


def test_an_apply_whose_cluster_is_still_installing_publishes_nothing(monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda *args, **kwargs: published.append(args))
    result = action(arguments(state=state(completed=False))).run(task_vars={})
    assert result["failed"] is True
    assert "installation completed at the declared release" in result["msg"]
    assert published == []


def test_an_apply_whose_cluster_completed_publishes_its_evidence(monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    assert action(arguments()).run(task_vars={}) == {"changed": False}
    assert published[0]["evidence"]["completed"] is True
    assert published[0]["evidence"]["postcondition"] is True


# An observation publishes an installation still under way as it found it, so
# the engine resolves it as partial rather than completed.
def test_an_observation_publishes_a_cluster_still_installing(monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    observed = arguments(outcome="unchanged", observed=True, state=state(completed=False, media=["sno-01"]))
    assert action(observed).run(task_vars={}) == {"changed": False}
    assert published[0]["evidence"]["completed"] is False
    assert published[0]["evidence"]["postcondition"] is False


def tasks(name):
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)
            if isinstance(task, dict)]


def one(name, predicate):
    found = [task for task in tasks(name) if predicate(task)]
    assert len(found) == 1
    return found[0]


def completion_read():
    return one("state.yml", lambda task: task.get("register") == "containercluster_install_agent_completion")


def resolve():
    return one("state.yml", lambda task: "containercluster_install_agent_state" in (
        task.get("ansible.builtin.set_fact") or {}))


def test_the_completion_is_read_from_clusterversion_through_the_builds_kubeconfig():
    read = completion_read()
    argv = [str(value) for value in read["ansible.builtin.command"]["argv"]]
    assert argv[1:3] == ["--kubeconfig", "{{ containercluster_install_agent_kubeconfig }}"]
    assert argv[3:8] == ["get", "clusterversion", "version", "--output",
                         'jsonpath={.status.conditions[?(@.type=="Available")].status}{"\\n"}'
                         '{.status.history[0].state}{"\\n"}{.status.history[0].version}']
    assert not [value for value in argv if "insecure" in value or "certificate-authority" in value]
    # The JSONPath carries no template delimiter, so it reaches oc as written.
    template = read["ansible.builtin.command"]["argv"][-1]
    assert Templar(loader=LOADER, variables={}).template(template) == template
    assert read["changed_when"] is False
    assert read["failed_when"] is False


def resolved_completion(rc, stdout):
    """The `completed` the state read resolves from one completion read.

    stdout is what the command module returns: oc's JSONPath output with its
    trailing empty lines stripped.
    """
    task = resolve()
    scope = dict(task.get("vars") or {})
    scope["bootwright_cluster_install_request"] = {"release": {"version": RELEASE}}
    scope["containercluster_install_agent_completion"] = {"rc": rc, "stdout": stdout, "stderr": ""}
    return Templar(loader=LOADER, variables=scope).template(
        task["ansible.builtin.set_fact"]["containercluster_install_agent_state"]["completed"])


def test_available_and_a_completed_history_at_the_declared_release_is_completed():
    assert resolved_completion(0, "True\nCompleted\n" + RELEASE) is True


@pytest.mark.parametrize("rc, stdout", [
    # Installing: the first history entry is Partial and Available defaults to
    # False until the release is applied.
    (0, "False\nPartial\n" + RELEASE),
    (0, "True\nPartial\n" + RELEASE),
    (0, "False\nCompleted\n" + RELEASE),
    # Completed at another release than the one this operation declared.
    (0, "True\nCompleted\n4.21.14"),
    # A history entry whose version the payload did not define is empty, and
    # the command module strips the empty last line.
    (0, "True\nCompleted"),
    # No Available condition yet.
    (0, "\nCompleted\n" + RELEASE),
    (0, "Unknown\nCompleted\n" + RELEASE),
    # No history yet: oc's JSONPath prints nothing for an absent key.
    (0, "False"),
    # oc's JSONPath refuses history[0] of an empty history as out of bounds
    # and prints nothing (client-go util/jsonpath, evalArray and Execute), and
    # a cluster that does not answer prints nothing either.
    (1, ""),
    # Whatever a failed read printed, it proved nothing.
    (1, "True\nCompleted\n" + RELEASE),
], ids=["installing", "partial", "unavailable", "another release", "no version", "no condition",
        "unknown", "no history", "empty history or nothing answered", "failed read"])
def test_anything_short_of_that_is_not_completed(rc, stdout):
    assert resolved_completion(rc, stdout) is False


def settled(completed):
    decide = one("apply.yml", lambda task: "containercluster_install_agent_settled" in (
        task.get("ansible.builtin.set_fact") or {}))
    return Templar(loader=LOADER, variables={
        "bootwright_cluster_install_request": {"release": {"version": RELEASE}},
        "containercluster_install_agent_before": {"observation": {"identity": IDENTITY}},
        "containercluster_install_agent_state": state(completed=completed),
    }).template(decide["ansible.builtin.set_fact"]["containercluster_install_agent_settled"])


# The settled decision skips both waits and releases the media, so it needs the
# same proof the completion evidence needs: a cluster with this build's
# identity, at the declared release and whole, that is still installing is not
# settled, and a retry waits for it.
def test_the_settled_decision_requires_the_cluster_to_report_its_installation_completed():
    assert settled(True) is True
    assert settled(False) is False
    assert settled("True") is False


def test_the_settled_decision_and_the_completion_evidence_agree():
    for completed in (True, False):
        found = protocol.evidence(arguments(state=state(completed=completed)), DIGEST, False)
        assert settled(completed) is found["postcondition"] is completed

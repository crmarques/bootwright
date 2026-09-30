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
IDENTITY = "d21b91799c1b2d06e949151732b30343fc42b122f785c10685fad98570b00a02"
RELEASE = "4.21.15"


def state(**overrides):
    """What the role's state read resolves for the lab-sno cluster, installed."""
    values = {
        "cluster": IDENTITY,
        "completed": True,
        "media": [],
        "missing": [],
        "ownMedia": [],
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
    template = read["ansible.builtin.command"]["argv"][7]
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


# The state read separates the nodes presenting the image this cluster's media
# block published from those presenting any other. The published address is
# the frozen base, the 64 hexadecimal digits `openssl rand -hex 32` minted for
# the attempt, and the image name (containercluster_media_agent/tasks/build.yml;
# containercluster_install_inspect.published). Each controller result is what
# a media read of redfish_system_read returns, `media` the image the controller
# presents or the empty string and `power` its power state
# (plugins/modules/redfish_system_read.py, RETURN), beside the loop item it read.
TOKEN = "3f" * 32
BASE = "https://192.0.2.1:8443/private/clusters/sno/"
PUBLISHED = BASE + TOKEN + "/agent.iso"


def controller(machine, media, power="On"):
    return {"changed": False, "item": {"machine": machine}, "media": media, "power": power}


def resolved_media(results, published=PUBLISHED):
    """The `media` and `ownMedia` the state read resolves from its controller reads."""
    task = resolve()
    scope = dict(task.get("vars") or {})
    scope["containercluster_install_agent_controllers"] = {"results": results}
    scope["containercluster_install_agent_before"] = {"observation": {"identity": IDENTITY, "url": published}}
    templar = Templar(loader=LOADER, variables=scope)
    fields = task["ansible.builtin.set_fact"]["containercluster_install_agent_state"]
    return list(templar.template(fields["media"])), list(templar.template(fields["ownMedia"]))


def test_a_node_presenting_the_published_image_presents_its_own():
    assert resolved_media([controller("sno-01", PUBLISHED)]) == (["sno-01"], ["sno-01"])


# Some controllers echo an inserted image back without its default port
# (.agents/knowledge/redfish-physical-bmc.md), and the insert's own read-back
# compares scheme and host without case (redfish_discovery.image_matches).
@pytest.mark.parametrize("published, presented", [
    ("https://192.0.2.1:443/private/clusters/sno/" + TOKEN + "/agent.iso",
     "https://192.0.2.1/private/clusters/sno/" + TOKEN + "/agent.iso"),
    ("https://artifacts.example:8443/private/clusters/sno/" + TOKEN + "/agent.iso",
     "HTTPS://Artifacts.Example:8443/private/clusters/sno/" + TOKEN + "/agent.iso"),
], ids=["default port dropped", "scheme and host in another case"])
def test_scheme_host_and_path_are_what_is_compared(published, presented):
    assert resolved_media([controller("sno-01", presented)], published) == (["sno-01"], ["sno-01"])


@pytest.mark.parametrize("presented", [
    BASE + "e0" * 32 + "/agent.iso",
    PUBLISHED + ".old",
    BASE + TOKEN + "/",
    BASE + TOKEN,
    "https://192.0.2.2:8443/private/clusters/sno/" + TOKEN + "/agent.iso",
    "http://192.0.2.1:8443/private/clusters/sno/" + TOKEN + "/agent.iso",
    "https://192.0.2.1:8443/private/clusters/SNO/" + TOKEN + "/agent.iso",
    # urllib.parse.urlsplit refuses both of these, which a read must survive.
    "https://[192.0.2.1:8443/private/clusters/sno/" + TOKEN + "/agent.iso",
    "https://192.0.2.9:port/private/clusters/sno/" + TOKEN + "/agent.iso",
], ids=["another attempt's token", "a path the published one prefixes", "its directory",
        "its directory without a separator", "another server", "another scheme", "a path in another case",
        "an unclosed address literal", "a port that is no number"])
def test_any_other_image_is_foreign(presented):
    assert resolved_media([controller("sno-01", presented)]) == (["sno-01"], [])


def test_one_foreign_image_is_told_apart_from_its_own():
    results = [controller("sno-01", PUBLISHED), controller("sno-02", BASE + "e0" * 32 + "/agent.iso", "Off")]
    assert resolved_media(results) == (["sno-01", "sno-02"], ["sno-01"])


# An image with no scheme, host or path splits exactly as the empty address
# does, so only the guard keeps it from comparing equal.
@pytest.mark.parametrize("presented", [PUBLISHED, "?", "#"], ids=["the image", "a bare query", "a bare fragment"])
def test_nothing_is_own_while_nothing_is_published(presented):
    assert resolved_media([controller("sno-01", presented)], "") == (["sno-01"], [])


def test_a_node_presenting_nothing_or_not_answering_presents_nothing():
    results = [controller("sno-01", ""), {"changed": False, "failed": True, "item": {"machine": "sno-02"}}]
    assert resolved_media(results) == ([], [])


# A node presenting nothing, or whose read failed, ahead of the nodes that do
# present must not shift an image onto another node's name: a retry would then
# skip the idle node and boot the live one again.
def test_each_image_is_named_by_the_node_presenting_it():
    results = [
        controller("sno-01", ""),
        {"changed": False, "failed": True, "item": {"machine": "sno-02"}},
        controller("sno-03", PUBLISHED),
        controller("sno-04", BASE + "e0" * 32 + "/agent.iso"),
    ]
    assert resolved_media(results) == (["sno-03", "sno-04"], ["sno-03"])


def test_the_evidence_publishes_own_media_as_bounded_names():
    found = protocol.evidence(arguments(state=state(
        completed=False, media=["sno-02", "sno-01"], ownMedia=["sno-02", "sno-01"])), DIGEST, False)
    assert found["ownMedia"] == ["sno-01", "sno-02"]
    assert found["postcondition"] is False
    without = state()
    del without["ownMedia"]
    assert protocol.evidence(arguments(state=without), DIGEST, False)["ownMedia"] == []
    with pytest.raises(ValueError):
        protocol.evidence(arguments(state=state(ownMedia=["sno"] * (protocol.MAX_NAMES + 1))), DIGEST, False)


# A retry never boots a node already running from this cluster's image: it
# neither inserts media into it nor sets a boot override on it.
def boot_skip():
    return one("boot.yml", lambda task: "block" in task)


def beneath(block):
    """Every task a block holds, its nested blocks' included, in order."""
    for task in block:
        if "block" in task:
            yield from beneath(task["block"])
        else:
            yield task


def test_every_boot_effect_sits_under_the_skip():
    boot = tasks("boot.yml")
    skip = boot_skip()
    assert boot[-1] == skip
    assert all("ansible.builtin.assert" in task for task in boot[:-1])
    inside = [task for task in beneath(skip["block"])
              if "bootwright.core.redfish_boot" in task or "ansible.builtin.include_role" in task]
    assert [task.get("bootwright.core.redfish_boot", {}).get("operation") for task in inside] == [
        None, None, "insert", None, None]
    assert [(task.get("ansible.builtin.include_role") or {}).get("name") for task in inside] == [
        "bootwright.core.substrate_libvirt_machine", "bootwright.core.substrate_baremetal_machine",
        None, "bootwright.core.substrate_libvirt_machine", "bootwright.core.substrate_baremetal_machine"]


def boots(powered, own, media=None):
    """Whether the boot file inserts and boots node sno-01 given the state read."""
    return Templar(loader=LOADER, variables={
        "containercluster_install_agent_node": {"machine": "sno-01", "name": "master-0"},
        "containercluster_install_agent_state": state(
            completed=False, powered=powered, ownMedia=own, media=own if media is None else media),
    }).evaluate_conditional(boot_skip()["when"])


def test_a_node_running_from_its_own_image_is_not_booted_again():
    assert boots(["sno-01"], ["sno-01"]) is False


@pytest.mark.parametrize("powered, own, media", [
    ([], ["sno-01"], None),
    (["sno-01"], [], ["sno-01"]),
    (["sno-01"], [], []),
    ([], [], []),
    (["sno-02"], ["sno-02"], None),
], ids=["its own image, not running", "running a foreign image", "running without media", "neither",
        "another node running from its own image"])
def test_every_other_node_is_booted(powered, own, media):
    assert boots(powered, own, media) is True


# A removal's resolution reads only the media each node presents, so the
# capability scopes that observation with the material value `observes:
# removal`, and the state read then reads none of the cluster the removal
# leaves running. Each node's controller is still read: a node that cannot be
# read fails the read, which leaves the removal's resolution unknown.
CLUSTER_READS = ("containercluster_install_agent_cluster", "containercluster_install_agent_release",
                 "containercluster_install_agent_completion", "containercluster_install_agent_nodes")


def defaults():
    return LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True)


def scoped(observes):
    material = {} if observes is None else {"observes": observes}
    return {
        "bootwright_cluster_install_material": material,
        "containercluster_install_agent_observes_removal": defaults()["containercluster_install_agent_observes_removal"],
    }


def reads(observes):
    """Which of the state read's reads run under one observation scope."""
    templar = Templar(loader=LOADER, variables=scoped(observes))
    return {task["register"]: templar.evaluate_conditional(task["when"]) if "when" in task else True
            for task in tasks("state.yml") if "register" in task}


@pytest.mark.parametrize("observes", [None, ""], ids=["unscoped", "empty"])
def test_every_other_observation_reads_the_cluster_and_every_node(observes):
    ran = reads(observes)
    assert all(ran[name] for name in CLUSTER_READS)
    assert ran["containercluster_install_agent_controllers"] is True


def test_a_removal_observation_reads_every_node_and_none_of_the_cluster():
    ran = reads("removal")
    assert not any(ran[name] for name in CLUSTER_READS)
    assert ran["containercluster_install_agent_controllers"] is True


# The state a removal's observation resolves when its node presents an image
# this cluster did not publish, with every cluster read skipped: nothing reads
# as answering and every declared node as missing, since none of the cluster
# was read. The evidence it publishes is install-evidence-release-partial-foreign
# in internal/containercluster/agentinstall/testdata, which Go reads as a
# removal part way through, because repeating the removal ejects any image
# (D27).
def test_a_removal_observation_of_a_foreign_image_publishes_the_media_alone():
    task = resolve()
    variables = dict(task.get("vars") or {})
    variables.update(scoped("removal"))
    skipped = {"changed": False, "skipped": True}
    variables.update({name: skipped for name in CLUSTER_READS})
    variables["bootwright_cluster_install_request"] = {
        "release": {"version": RELEASE}, "nodes": [{"machine": "sno-01", "name": "master-0"}]}
    variables["containercluster_install_agent_controllers"] = {
        "results": [controller("sno-01", BASE + "e0" * 32 + "/agent.iso")]}
    variables["containercluster_install_agent_before"] = {"observation": {"identity": IDENTITY, "url": PUBLISHED}}
    resolved = Templar(loader=LOADER, variables=variables).template(
        task["ansible.builtin.set_fact"]["containercluster_install_agent_state"])
    found = protocol.evidence(arguments(outcome="unchanged", observed=True, state=resolved), DIGEST, False)
    assert found == {
        "absent": False, "cluster": "", "completed": False, "identity": IDENTITY, "media": ["sno-01"],
        "missing": ["master-0"], "ownMedia": [], "postcondition": False, "powered": ["sno-01"],
        "release": "", "request": DIGEST,
    }

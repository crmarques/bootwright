"""How the install role reads the cluster through the build's own kubeconfig.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins/modules, so these checks live here while the inspection's
own tests stay beside its module.
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
import pathlib
from unittest import mock

from ansible_collections.bootwright.core.tests.unit.install_phase_tasks import apply_tasks as phased_tasks

import pytest
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import containercluster_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import containercluster_cluster_read as reading
from ansible_collections.bootwright.core.plugins.module_utils import containercluster_install_wait as waiting
from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import KEPT, KUBECONFIG, keep, observe
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"


def pem(kind, body):
    return ("-----BEGIN %s-----\n%s\n-----END %s-----\n" % (kind, body, kind)).encode()


# The same placeholder bodies as the inspection's tests: only their digest matters.
AUTHORITY = pem("CERTIFICATE", "bG9hZGJhbGFuY2Vy") + pem("CERTIFICATE", "bG9jYWxob3N0") + pem("CERTIFICATE", "c2VydmljZQ==")
CLIENT = pem("CERTIFICATE", "YWRtaW4=")
KEY = pem("RSA PRIVATE KEY", "a2V5")
# The router CA bundle, the wildcard serving certificate then the ingress
# operator's CA, as the inspection's tests take it from the default-ingress-cert
# ConfigMap.
ROUTER = pem("CERTIFICATE", "d2lsZGNhcmQ=") + pem("CERTIFICATE", "aW5ncmVzcy1vcGVyYXRvcg==")


def anchor(client=CLIENT):
    return hashlib.sha256(b"bootwright/containercluster/identity/v1\0" + client).hexdigest()


def encoded(data):
    return base64.b64encode(data).decode()


# auth/kubeconfig once `agent wait-for install-complete` ran:
# addRouterCAToClusterCA prepended the router CA bundle to the authority
# AgentAdminClient wrote and clientcmd.WriteToFile wrote the file back, adding
# apiVersion and kind in their sorted places and the client certificate and key
# unchanged (cmd/openshift-install/command/waitfor.go:65-84 and :291-334 in
# openshift/installer release-4.21, with the vendored client-go v0.34.1
# tools/clientcmd loader.go:448-465 and :495-497). The layout is the one
# rewritten() in the inspection's tests builds.
REWRITTEN = (
    "apiVersion: v1\n"
    "clusters:\n"
    "- cluster:\n"
    "    certificate-authority-data: %s\n"
    "    server: https://api.sno.lab.example:6443\n"
    "  name: sno\n"
    "contexts:\n"
    "- context:\n"
    "    cluster: sno\n"
    "    user: admin\n"
    "  name: admin\n"
    "current-context: admin\n"
    "kind: Config\n"
    "users:\n"
    "- name: admin\n"
    "  user:\n"
    "    client-certificate-data: %s\n"
    "    client-key-data: %s\n"
) % (encoded(ROUTER + AUTHORITY), encoded(CLIENT), encoded(KEY))


def trusted(path):
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def one(name, predicate):
    tasks = [task for task in (apply_tasks() if name == "apply.yml" else trusted(ROLE / "tasks" / name)) if predicate(task)]
    assert len(tasks) == 1
    return tasks[0]


def state_task(predicate):
    return one("cluster_state.yml", predicate)


def test_the_cluster_is_read_through_the_kubeconfig_the_identity_is_taken_from():
    # The inspection reads through the kept copy it names the identity from
    # (test_containercluster_cluster_read.py), and the role's own reads, the
    # placement's among them, go through the same file.
    read = state_task(lambda task: task.get("register") == "containercluster_install_agent_cluster_read")
    resolve = state_task(lambda task: "ansible.builtin.set_fact" in task)
    assert "containercluster_install_agent_cluster_read.state" in resolve["ansible.builtin.set_fact"][
        "containercluster_install_agent_state"]
    assert read[INSPECT]["cluster"] is True
    assert not [value for value in map(str, read[INSPECT].values()) if "insecure" in value or "certificate-authority" in value]
    assert "failed_when" not in read
    defaults = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    defaults["bootwright_cluster_install_request"] = {"workRoot": "/var/lib/bootwright-hosted/lab/clusters/sno"}
    rendered = Templar(loader=LOADER, variables=defaults).template(defaults["containercluster_install_agent_kubeconfig"])
    assert rendered == os.path.join("/var/lib/bootwright-hosted/lab/clusters/sno", KEPT)
    assert rendered != os.path.join("/var/lib/bootwright-hosted/lab/clusters/sno", KUBECONFIG)


# What oc prints on stderr, through StandardErrorMessage in
# https://raw.githubusercontent.com/openshift/oc/release-4.21/vendor/k8s.io/kubectl/pkg/cmd/util/helpers.go:
# line 273 wraps a transport error as "Unable to connect to the server: %v",
# line 271 names a refused connection, line 253 prints a 401 as "error: You must
# be logged in to the server (%s)" and line 255 any other status as "Error from
# server (%s): %s". The transport errors are Go 1.24's: crypto/tls
# CertificateVerificationError ("tls: failed to verify certificate: %s",
# common.go) around crypto/x509 UnknownAuthorityError (verify.go), and net
# OpError ("dial tcp <addr>: ", net.go) around internal/poll's "i/o timeout".
# The 401 message is kube-apiserver's NewUnauthorized("Unauthorized")
# (k8s.io/apiserver pkg/endpoints/filters/authentication.go) and the 403 one
# NewForbidden's "%s %q is forbidden: %v" (k8s.io/apimachinery
# pkg/api/errors/errors.go) around forbiddenMessage's cluster-scope form
# (k8s.io/apiserver pkg/endpoints/handlers/responsewriters/errors.go).
UNKNOWN_AUTHORITY = ("Unable to connect to the server: tls: failed to verify certificate: "
                     "x509: certificate signed by unknown authority")
UNAUTHORIZED = "error: You must be logged in to the server (Unauthorized)"
FORBIDDEN = ('Error from server (Forbidden): clusterversions.config.openshift.io "version" is forbidden: '
             'User "system:anonymous" cannot get resource "clusterversions" in API group '
             '"config.openshift.io" at the cluster scope')
REFUSED = "The connection to the server api.sno.lab.example:6443 was refused - did you specify the right host or port?"
TIMEOUT = "Unable to connect to the server: dial tcp 192.0.2.10:6443: i/o timeout"
IDENTITY = anchor()


def cluster_read(request, identity, version, nodes):
    """What the state file resolves from the inspection's two reads, each (rc, stdout, stderr) as oc answers it."""
    def run(oc, kubeconfig, argv, deadline, body=None):
        return version if argv[1] == "clusterversion" else nodes
    with mock.patch.object(reading.native, "run", run):
        found, read = reading.read("/usr/bin/oc", "/work/" + KEPT, request, identity)
    registered = {"containercluster_install_agent_cluster_read": {"changed": False, "state": found, "answered": read}}
    task = state_task(lambda task: "ansible.builtin.set_fact" in task)
    state = Templar(loader=LOADER, variables=dict(registered, bootwright_cluster_install_request=request)).template(
        task["ansible.builtin.set_fact"]["containercluster_install_agent_state"])
    return state, registered


def answered(identity, rc, stderr=""):
    """The cluster the state file resolves from one read of ClusterVersion."""
    request = {"release": {"version": "4.21.0"}, "nodes": []}
    version = (rc, '{"status": {}}' if rc == 0 else "", stderr)
    return cluster_read(request, identity, version, (0, "", ""))[0]["cluster"]


def foreign():
    return reading.FOREIGN


def test_a_cluster_that_accepts_the_anchor_answers_with_this_builds_identity():
    assert answered(IDENTITY, 0) == IDENTITY


@pytest.mark.parametrize("stderr", [UNKNOWN_AUTHORITY, UNAUTHORIZED, FORBIDDEN],
                         ids=["unknown authority", "401", "403"])
def test_an_api_that_rejects_the_anchor_is_foreign_never_nothing(stderr):
    assert answered(IDENTITY, 1, stderr) == foreign() != ""


def test_a_success_through_an_anchor_that_names_nothing_is_foreign():
    assert answered("", 0) == foreign()


@pytest.mark.parametrize("stderr", [REFUSED, TIMEOUT], ids=["refused", "timeout"])
def test_nothing_answering_is_empty(stderr):
    assert answered(IDENTITY, 1, stderr) == ""


def test_the_foreign_marker_can_never_equal_an_identity():
    marker = foreign()
    assert marker and len(IDENTITY) == 64
    assert len(marker) != 64 or set(marker) - set("0123456789abcdef")


def publication(name):
    """The arguments of the completion evidence one task file publishes."""
    def publishes(task):
        arguments = task.get("bootwright.core.containercluster_install_protocol")
        return isinstance(arguments, dict) and arguments.get("phase") == "completed"
    return one(name, publishes)["bootwright.core.containercluster_install_protocol"]


def completion(name):
    """The identity argument of the evidence one task file publishes."""
    return publication(name)["identity"]


# The evidence names the identity the inspection took before any effect, never
# what the cluster answered: a read that succeeds through a kubeconfig naming no
# identity answers "foreign", and publishing that as the identity would make
# identity and cluster agree on a cluster no anchor proves. The scope holds only
# the registers the task files set, so a reference to any other one fails.
@pytest.mark.parametrize("name", ["apply.yml", "observed.yml", "destroy_finish.yml"])
@pytest.mark.parametrize("before, cluster", [("", "foreign"), (IDENTITY, "")],
                         ids=["no anchor answered foreign", "an anchor nothing answered"])
def test_the_evidence_publishes_the_identity_the_inspection_named(name, before, cluster):
    scope = {
        "containercluster_install_agent_before": {"observation": {"identity": before}},
        "containercluster_install_agent_state": {"cluster": cluster},
    }
    assert cluster in ("", foreign())
    assert Templar(loader=LOADER, variables=scope).template(completion(name)) == before


# The completion publishes the very identity the settled decision compared the
# cluster's answer against, so the evidence says what the decision relied on.
@pytest.mark.parametrize("before, settled", [(IDENTITY, True), (anchor(client=CLIENT + CLIENT), False)],
                         ids=["the same anchor", "another anchor"])
def test_the_completion_publishes_the_identity_the_settled_decision_compared(before, settled):
    decide = one("apply.yml", lambda task: "containercluster_install_agent_settled" in (
        task.get("ansible.builtin.set_fact") or {}))
    templar = Templar(loader=LOADER, variables={
        "bootwright_cluster_install_request": {"release": {"version": "4.21.0"}},
        "containercluster_install_agent_before": {"observation": {"identity": before}},
        "containercluster_install_agent_state": {"cluster": IDENTITY, "release": "4.21.0", "missing": [],
                                                 "completed": True},
    })
    assert templar.template(decide["ansible.builtin.set_fact"]["containercluster_install_agent_settled"]) is settled
    assert templar.template(completion("apply.yml")) == before


RELEASE = "4.21.15"
DIGEST = "1" * 64


def inspected(root, config):
    """What an apply's inspection names over a work area whose installer wrote one kubeconfig."""
    (root / "auth").mkdir(parents=True)
    (root / KUBECONFIG).write_text(config)
    request = {"workRoot": str(root), "image": {"path": str(root / "served"), "url": "https://192.0.2.1:8443"}}
    keep(request)
    return observe(request)


def resolved(observation):
    """The state the state file resolves for a whole cluster reporting its installation completed.

    The cluster read is what oc answers the inspection's two reads with, and
    the media and power are what a media read of redfish_system_read returns
    (plugins/modules/redfish_system_read.py, RETURN) for a node running with its
    media released.
    """
    request = {"release": {"version": RELEASE}, "nodes": [{"name": "master-0", "machine": "sno-01"}]}
    version = json.dumps({"status": {"conditions": [{"type": "Available", "status": "True"}],
                                     "desired": {"version": RELEASE},
                                     "history": [{"state": "Completed", "version": RELEASE}]}})
    result = cluster_read(request, observation["identity"], (0, version, ""), (0, "master-0", ""))[0]
    return dict(result, media=[], ownMedia=[], powered=["sno-01"])


# A retry, or the resolution of an attempt interrupted after `agent wait-for
# install-complete` rewrote the kubeconfig, inspects the rewritten file before
# any effect. It still names this build's identity, so a read that succeeds
# through it answers that identity rather than the foreign marker, the settled
# decision holds for a completed cluster, and the evidence proves completion.
def test_after_the_install_complete_rewrite_a_completed_cluster_still_proves_this_build(tmp_path):
    observation = inspected(tmp_path / "work", REWRITTEN)
    assert observation["identity"] == IDENTITY
    assert answered(observation["identity"], 0) == IDENTITY != foreign()
    state = resolved(observation)
    assert state["cluster"] == IDENTITY
    decide = one("apply.yml", lambda task: "containercluster_install_agent_settled" in (
        task.get("ansible.builtin.set_fact") or {}))
    scope = {
        "bootwright_cluster_install_request": {"release": {"version": RELEASE}},
        "bootwright_cluster_install_digest": DIGEST,
        "containercluster_install_agent_before": {"observation": observation},
        "containercluster_install_agent_state": state,
        "containercluster_install_agent_placement_read": {"placement": [], "unplaced": []},
    }
    settled = Templar(loader=LOADER, variables=scope).template(
        decide["ansible.builtin.set_fact"]["containercluster_install_agent_settled"])
    assert settled is True
    scope["containercluster_install_agent_settled"] = settled
    arguments = Templar(loader=LOADER, variables=scope).template(publication("apply.yml"))
    assert arguments["outcome"] == "unchanged"
    found = protocol.evidence(arguments, DIGEST, False)
    assert found["identity"] == found["cluster"] == IDENTITY
    assert found["postcondition"] is True
    assert protocol.unproved(found) == []


# The completion evidence records whether a wait of this apply put the kept
# copy back in place of the installer's own kubeconfig, which the last wait's
# result carries (wait.yml); an apply whose waits restored nothing, or that
# waited not at all, records no restore.
@pytest.mark.parametrize("facts, expected", [
    ({}, False), ({"containercluster_install_agent_wait": {"outcome": "reached", "restored": False}}, False),
    ({"containercluster_install_agent_wait": {"outcome": "reached", "restored": True}}, True)],
    ids=["no wait", "nothing restored", "restored"])
def test_the_completion_evidence_records_a_restore_of_the_installers_kubeconfig(tmp_path, facts, expected):
    observation = inspected(tmp_path / "work", REWRITTEN)
    scope = dict({
        "bootwright_cluster_install_digest": DIGEST,
        "containercluster_install_agent_before": {"observation": observation},
        "containercluster_install_agent_state": resolved(observation),
        "containercluster_install_agent_settled": False,
        "containercluster_install_agent_placement_read": {"placement": [], "unplaced": []},
    }, **facts)
    arguments = Templar(loader=LOADER, variables=scope).template(publication("apply.yml"))
    assert arguments["restored"] is expected
    found = protocol.evidence(arguments, DIGEST, False)
    assert found["restored"] is expected and found["postcondition"] is True


INSPECT = "bootwright.core.containercluster_install_inspect"


def apply_tasks():
    return phased_tasks(LOADER, ROLE)


def only(entries, predicate):
    matches = [index for index, task in enumerate(entries) if predicate(task)]
    assert len(matches) == 1, matches
    return matches[0]


# Only an apply keeps the installer's kubeconfig. An observation, a removal's
# included, is read-only against the frozen request and reads the copy the last
# apply kept.
def test_only_an_apply_keeps_the_installers_kubeconfig():
    assert [task[INSPECT].get("keep") for task in apply_tasks() if INSPECT in task] == [True, True]
    for name in ("observe.yml", "destroy.yml"):
        found = [task for task in trusted(ROLE / "tasks" / name) if INSPECT in task]
        assert found and not [task for task in found if task[INSPECT].get("keep", False)], name


# Only a path string check stood between the apply and the work area it writes
# the booted mark and the kept kubeconfig into, so the apply proves each
# existing hosted directory down to the cluster's by no-follow descent before
# any task but its request checks and the handoff.
def test_the_apply_proves_the_hosted_cluster_directories_before_any_effect():
    entries = apply_tasks()
    proof = only(entries, lambda task: "bootwright.core.hosted_area" in task)
    assert entries[proof]["bootwright.core.hosted_area"] == {
        "context": "{{ bootwright_cluster_install_request.identity.context }}",
        "path": "{{ containercluster_install_agent_area }}", "state": "proved"}
    assert all(set(task) - {"name", "no_log", "vars", "loop", "loop_control"} <= {
        "ansible.builtin.assert", "bootwright.core.containercluster_install_protocol", "ansible.builtin.include_role"}
        for task in entries[:proof])
    defaults = dict(LOADER.load_from_file(str(ROLE / "defaults/main.yml"), trusted_as_template=True))
    templar = Templar(loader=LOADER, variables={**defaults, "bootwright_cluster_install_request": {
        "identity": {"context": "lab", "cluster": "sno"}, "workRoot": "/var/lib/bootwright-hosted/lab/clusters/sno"}})
    assert templar.template(defaults["containercluster_install_agent_area"]) == ["clusters", "sno"]
    equality = next(item for item in entries[0]["ansible.builtin.assert"]["that"] if "/clusters/" in item)
    assert templar.evaluate_conditional(equality)
    other = Templar(loader=LOADER, variables={"bootwright_cluster_install_request": {
        "identity": {"context": "lab", "cluster": "sno"}, "workRoot": "/var/lib/bootwright-hosted/lab/machines/node/install"}})
    assert not other.evaluate_conditional(equality)


def refused(observation):
    """Whether the apply's refusal fires over what its first inspection named, and its message."""
    refusal = one("apply.yml", lambda task: task.get("name") == "Refuse an installation with no whole kubeconfig to read its cluster through")
    templar = Templar(loader=LOADER, variables={
        "bootwright_cluster_install_request": {"identity": {"cluster": "sno", "context": "lab"}},
        "containercluster_install_agent_before": {"observation": observation},
    })
    return templar.evaluate_conditional(refusal["when"]), templar.template(refusal["ansible.builtin.fail"]["msg"])


# The copy every read goes through is kept before the first effect, and an
# apply with none naming this build refuses there, before it reads, marks the
# work area or boots a node, naming the remedy.
def test_the_apply_keeps_its_kubeconfig_and_refuses_without_one_before_its_first_effect():
    entries = apply_tasks()
    kept = only(entries, lambda task: task.get("register") == "containercluster_install_agent_before")
    refusal = only(entries, lambda task: task.get("name") == "Refuse an installation with no whole kubeconfig to read its cluster through")
    first_read = min(i for i, task in enumerate(entries) if task.get("ansible.builtin.import_tasks") in ("cluster_state.yml", "collect_state.yml"))
    mark = only(entries, lambda task: str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith(
        "/.bootwright-booted"))
    boot = only(entries, lambda task: task.get("ansible.builtin.import_tasks") == "boot.yml")
    assert entries[kept][INSPECT]["keep"] is True
    assert kept < refusal < first_read < mark < boot
    assert refused({"identity": IDENTITY})[0] is False
    fires, message = refused({"identity": ""})
    assert fires is True
    assert "cluster sno" in message and "Nothing was changed" in message
    assert "bootwright destroy --context lab" in message


# A first apply over an installer file an interrupted rewrite cut short keeps
# nothing and refuses, rather than booting nodes it could never prove.
def test_an_apply_over_a_truncated_installer_file_with_no_kept_copy_refuses(tmp_path):
    observation = inspected(tmp_path / "work", REWRITTEN[:-10])
    assert observation["identity"] == ""
    assert not (tmp_path / "work" / KEPT).exists()
    assert refused(observation)[0] is True


# The installation wait rewrites the installer's file; the apply keeps that
# rewrite, when it is whole, before the read that proves completion and the
# copy offered for custody, whether or not this attempt waited.
def test_the_rewrite_the_waits_leave_is_kept_before_the_final_read_and_the_custody_copy():
    entries = apply_tasks()
    keeps = [index for index, task in enumerate(entries) if (task.get(INSPECT) or {}).get("keep") is True]
    wait = only(entries, lambda task: task.get("ansible.builtin.include_tasks") == "wait.yml"
                and "install-complete" in str(task.get("vars")))
    last_read = max(i for i, task in enumerate(entries) if task.get("ansible.builtin.import_tasks") in ("cluster_state.yml", "collect_state.yml"))
    custody = only(entries, lambda task: "bootwright_cluster_install_output.kubeconfig" in str(task.get("ansible.builtin.copy", "")))
    assert len(keeps) == 2
    assert wait < keeps[1] < last_read < custody
    assert "when" not in entries[keeps[1]]


COMPACT = {
    "identity": {"cluster": "compact", "context": "lab"}, "release": {"version": RELEASE},
    "rendezvous": "192.0.2.10", "nodes": [
        {"name": "master-0", "address": "192.0.2.10", "machine": "machine-00"},
        {"name": "master-1", "address": "192.0.2.11", "machine": "machine-01"}],
    "vips": [{"slot": "api", "address": "192.0.2.5", "bridge": "virbr-lab"},
             {"slot": "ingress", "address": "192.0.2.7", "bridge": "virbr-lab"}]}


def reads(nodes="master-0 master-1", nodes_rc=0, cluster_rc=0, history_state="Completed"):
    """What cluster_state.yml registers and resolves from the inspection's two reads, as oc answers them."""
    version = json.dumps({"status": {"conditions": [{"type": "Available", "status": "True"}],
                                     "desired": {"version": RELEASE},
                                     "history": [{"state": history_state, "version": RELEASE}]}})
    registered = cluster_read(
        COMPACT, IDENTITY, (0, version, "") if cluster_rc == 0 else (cluster_rc, "", TIMEOUT),
        (0, nodes, "") if nodes_rc == 0 else (nodes_rc, "", TIMEOUT))[1]
    return dict(registered, containercluster_install_agent_before={"observation": {"identity": IDENTITY}})


def resolved_state(registered):
    """The state cluster_state.yml resolves from those reads."""
    task = state_task(lambda task: "ansible.builtin.set_fact" in task)
    return Templar(loader=LOADER, variables=dict(registered, bootwright_cluster_install_request=COMPACT)).template(
        task["ansible.builtin.set_fact"]["containercluster_install_agent_state"])


def release_gate(registered, state):
    """Whether the media release gate fires over that state, and its message."""
    entries = apply_tasks()
    task = entries[only(entries, lambda task: task.get("name") ==
                        "Refuse media release until every declared node completed installation")]
    scope = dict(task["vars"], bootwright_cluster_install_request=COMPACT,
                 containercluster_install_agent_state=state, **registered)
    templar = Templar(loader=LOADER, variables=scope)
    return templar.evaluate_conditional(task["when"]), " ".join(templar.template(task["ansible.builtin.fail"]["msg"]).split())


def test_media_release_is_gated_by_a_fresh_read_after_the_installation_wait():
    entries = apply_tasks()
    gate = only(entries, lambda task: task.get("name") ==
                "Refuse media release until every declared node completed installation")
    release = only(entries, lambda task: task.get("ansible.builtin.import_tasks") == "release.yml")
    wait = only(entries, lambda task: task.get("ansible.builtin.include_tasks") == "wait.yml"
                and task.get("vars", {}).get("containercluster_install_agent_milestone") == "install-complete")
    assert wait < gate < release
    assert entries[gate - 1]["ansible.builtin.import_tasks"] == "cluster_state.yml"


@pytest.mark.parametrize("registered,refuses,stranded", [
    (reads(), False, False),
    (reads(nodes="master-0"), True, False),
    (reads(nodes="master-1"), True, True),
    (reads(history_state="Partial"), True, False),
])
def test_media_release_names_the_stranded_rendezvous_only_from_a_read_that_lacks_it(registered, refuses, stranded):
    fires, message = release_gate(registered, resolved_state(registered))
    assert fires is refuses
    if refuses:
        assert "No installation media was released" in message
        assert "could not be read" not in message
        assert ("bootstrap control plane" in message and "Destroy this context and perform a fresh apply" in message) is stranded
        if not stranded:
            assert "Keep the installation media attached" in message


# An apply after a host error found the rendezvous host missing goes back
# through the boot path, which leaves every running node as it is, and fails at
# the gate exactly as the first apply did.
def test_a_replay_of_a_stranded_rendezvous_fails_the_same_way():
    registered = reads(nodes="master-1")
    first = release_gate(registered, resolved_state(registered))
    machines = [node["machine"] for node in COMPACT["nodes"]]
    state = dict(resolved_state(registered), powered=machines, ownMedia=machines, media=machines)
    scope = dict(registered, bootwright_cluster_install_request=COMPACT, containercluster_install_agent_state=state)
    entries = apply_tasks()
    settled = entries[only(entries, lambda task: task.get("name") == "Decide whether this cluster is already installed")]
    assert Templar(loader=LOADER, variables=scope).template(
        settled["ansible.builtin.set_fact"]["containercluster_install_agent_settled"]) is False
    boot = one("boot.yml", lambda task: task.get("name") == "Boot a node not already running from this cluster's image")
    for node in COMPACT["nodes"]:
        templar = Templar(loader=LOADER, variables=dict(scope, containercluster_install_agent_node=node))
        assert templar.evaluate_conditional(boot["when"]) is False
    probe = one("preboot_vips.yml", lambda task: task.get("register") == "containercluster_install_agent_vip_probes")
    for vip in COMPACT["vips"]:
        templar = Templar(loader=LOADER, variables=dict(scope, item=vip))
        assert not all(templar.evaluate_conditional(condition) for condition in probe["when"])
    assert release_gate(registered, state) == first
    assert first[0] is True and "Destroy this context and perform a fresh apply" in first[1]


def test_the_host_error_hint_names_a_destroy_and_a_fresh_apply_never_a_repeat():
    hint = waiting.HOST_ERROR_HINT
    assert "destroy this context and perform a fresh apply" in hint
    assert "repeat the apply" not in hint.lower()


# A read that fails after the installation wait counts every declared node
# missing, the rendezvous host among them, yet proves nothing: the gate still
# releases nothing, says the cluster could not be read and offers no destroy.
@pytest.mark.parametrize("registered", [reads(nodes_rc=1), reads(cluster_rc=1)])
def test_a_failed_read_after_the_wait_releases_nothing_and_offers_no_destroy(registered):
    state = resolved_state(registered)
    fires, message = release_gate(registered, state)
    assert fires is True
    assert "could not be read" in message and "Repeat the apply" in message
    assert "No installation media was released" in message
    assert "Destroy" not in message and "bootstrap control plane" not in message


def test_a_failed_node_read_cannot_prove_presence_from_partial_stdout():
    request = {"release": {"version": RELEASE}, "nodes": [{"name": "master-0"}]}
    state, registered = cluster_read(request, IDENTITY, (0, '{"status": {}}', ""), (1, "master-0", TIMEOUT))
    assert state["missing"] == ["master-0"]
    assert registered["containercluster_install_agent_cluster_read"]["answered"] is False

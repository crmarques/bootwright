"""How the install role reads the cluster through the build's trust anchor.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins/modules, so these checks live here while the inspection's
own tests stay beside its module.
"""

from __future__ import annotations

import hashlib
import os
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import KUBECONFIG

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
LOADER = DataLoader()


def pem(kind, body):
    return ("-----BEGIN %s-----\n%s\n-----END %s-----\n" % (kind, body, kind)).encode()


# The same placeholder bodies as the inspection's tests: only their digest matters.
AUTHORITY = pem("CERTIFICATE", "bG9hZGJhbGFuY2Vy") + pem("CERTIFICATE", "bG9jYWxob3N0") + pem("CERTIFICATE", "c2VydmljZQ==")
CLIENT = pem("CERTIFICATE", "YWRtaW4=")


def anchor(authority=AUTHORITY, client=CLIENT):
    return hashlib.sha256(authority + b"\0" + client).hexdigest()


def trusted(path):
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def one(name, predicate):
    tasks = [task for task in trusted(ROLE / "tasks" / name) if predicate(task)]
    assert len(tasks) == 1
    return tasks[0]


def state_task(predicate):
    return one("state.yml", predicate)


def test_the_cluster_is_read_through_the_kubeconfig_the_identity_is_taken_from():
    read = state_task(lambda task: task.get("register") == "containercluster_install_agent_cluster")
    resolve = state_task(lambda task: "ansible.builtin.set_fact" in task and "vars" in task)
    assert "containercluster_install_agent_cluster.rc" in resolve["ansible.builtin.set_fact"][
        "containercluster_install_agent_state"]["cluster"]
    argv = [str(value) for value in read["ansible.builtin.command"]["argv"]]
    assert argv[1:3] == ["--kubeconfig", "{{ containercluster_install_agent_kubeconfig }}"]
    assert argv[3:6] == ["get", "clusterversion", "version"]
    assert not [value for value in argv if "insecure" in value or "certificate-authority" in value]
    assert read["failed_when"] is False
    defaults = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    defaults["bootwright_cluster_install_request"] = {"workRoot": "/var/lib/bootwright-clusters/lab/sno"}
    rendered = Templar(loader=LOADER, variables=defaults).template(defaults["containercluster_install_agent_kubeconfig"])
    assert rendered == os.path.join("/var/lib/bootwright-clusters/lab/sno", KUBECONFIG)


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


def answered(identity, rc, stderr=""):
    """The cluster the state file resolves from one read of ClusterVersion."""
    task = state_task(lambda task: "ansible.builtin.set_fact" in task and "vars" in task)
    scope = dict(task["vars"])
    scope["containercluster_install_agent_before"] = {"observation": {"identity": identity}}
    scope["containercluster_install_agent_cluster"] = {"rc": rc, "stdout": "", "stderr": stderr}
    cluster = task["ansible.builtin.set_fact"]["containercluster_install_agent_state"]["cluster"]
    return Templar(loader=LOADER, variables=scope).template(cluster)


def foreign():
    return state_task(lambda task: "ansible.builtin.set_fact" in task and "vars" in task)["vars"][
        "containercluster_install_agent_foreign"]


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


def completion(name):
    """The identity argument of the evidence one task file publishes."""
    def publishes(task):
        arguments = task.get("bootwright.core.containercluster_install_protocol")
        return isinstance(arguments, dict) and arguments.get("phase") == "completed"
    return one(name, publishes)["bootwright.core.containercluster_install_protocol"]["identity"]


# The evidence names the identity the inspection took before any effect, never
# what the cluster answered: a read that succeeds through a kubeconfig naming no
# identity answers "foreign", and publishing that as the identity would make
# identity and cluster agree on a cluster no anchor proves. The scope holds only
# the registers the task files set, so a reference to any other one fails.
@pytest.mark.parametrize("name", ["apply.yml", "observe.yml", "destroy.yml"])
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
        "containercluster_install_agent_state": {"cluster": IDENTITY, "release": "4.21.0", "missing": []},
    })
    assert templar.template(decide["ansible.builtin.set_fact"]["containercluster_install_agent_settled"]) is settled
    assert templar.template(completion("apply.yml")) == before

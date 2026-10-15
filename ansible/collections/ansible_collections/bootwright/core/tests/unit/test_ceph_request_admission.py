"""Each Ceph role entry point admits the whole request the Go side sends.

The runner hands an entry point the frozen request as `<Variable>_request`, and
ansible-core, validating the role's argument specification before the entry
point's first task, refuses a key a dict declaring options does not declare.
A specification that declared only `version` therefore refused every real
request (the run stopped before the adapter's `loaded` record) while every
test that sent `{version: ...}` stayed green. These cases validate, with the
validator ansible-core's prepended task uses, a request carrying every member
the Go side encodes: Request in internal/storage/ceph/requests.go,
ImageRequest in internal/storage/ceph/image.go and HostRequest in
internal/storage/ceph/host.go.
The Go test TestEveryCephRoleEntryPointDeclaresExactlyTheRequestItReceives holds
each specification to that struct's members.
"""

from __future__ import annotations

import copy
import pathlib

import pytest
import yaml
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
DIGEST = "c" * 64

CORE = {
    "action": "cluster",
    "bootstrap": "node-1",
    "callHome": "enabled",
    "release": "synthetic-release",
    "ansiblePackageVersion": "synthetic-tool",
    "nfsGaneshaPackageVersion": "synthetic-nfs",
    "packageRepository": "https://packages.example.test/selected",
    "cephadm": {"packageVersion": "20.2.1-405.el9cp", "path": "/usr/sbin/cephadm"},
    "client": {"access": "rw", "name": "app", "pool": "data"},
    "cluster": "example",
    "clusterKeyRef": "cluster-key",
    "clusterNetworks": ["198.51.100.0/24"],
    "clusterSSHTransport": "native",
    "clusterSSHUser": "operator",
    "config": {"global": {"osd_pool_default_size": "2"}},
    "controller": "controller",
    "fsid": "5effe704-a878-5cae-8b43-c81f7112832a",
    "hosts": [{
        "address": "192.0.2.1",
        "devices": [{"path": "/dev/vdb", "serial": "node-1", "sizeBytes": 1073741824}],
        "hostKeyRef": "node-1-host-key",
        "knownHosts": "known-hosts",
        "labels": ["storage"],
        "location": {},
        "machine": "node-1",
        "name": "node-1",
        "port": 22,
        "sshKeyRef": "fleet-key",
        "user": "operator",
    }],
    "image": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + DIGEST,
    "monIP": "192.0.2.1",
    "owner": "op-0123456789abcdef",
    "packages": {
        "licenseAccepted": True,
        "sources": [{
            "certificateRef": "package-ca",
            "host": "node-1",
            "media": "ceph-packages",
            "mediaSHA256": DIGEST,
            "mediaSize": 1024,
            "profile": "ceph",
            "publicationPath": "/packages",
            "url": "https://192.0.2.9/packages",
        }],
        "version": "ceph-packages-v2",
    },
    "policy": {"deviceClass": "ssd", "failureDomain": "host", "name": "ssd-hosts", "root": "default", "rule": "ssd-hosts", "size": 2},
    "pool": {
        "application": "rbd",
        "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
        "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""},
        "crushRule": "data",
        "erasure": {
            "codingChunks": 2,
            "dataChunks": 4,
            "deviceClass": "",
            "failureDomain": "host",
            "plugin": "isa",
            "profile": "data",
            "profileDefaults": {"crush-device-class": "", "crush-num-failure-domains": "0", "crush-osds-per-failure-domain": "0"},
            "root": "default",
            "stripeUnit": 4096,
            "technique": "reed_sol_van",
        },
        "minSize": 5,
        "name": "data",
        "overwrites": True,
        "pgNum": 32,
        "quota": {"maxBytes": 0, "maxObjects": 0},
        "role": "rbd",
        "size": 6,
        "type": "erasure",
    },
    "publicNetworks": ["192.0.2.0/24"],
    "rbd": {"dataPool": "", "features": ["layering"], "name": "disk", "pool": "data", "sizeBytes": 1073741824},
    "registry": "cp.icr.io",
    "registryRef": "ibm-registry",
    "services": [{"placement": {"count": 2, "hosts": ["node-1"]}, "service_type": "mgr"}],
    "version": "ceph-core-v8",
}

IMAGE = {
    "base": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9",
    "cluster": "example",
    "controller": "controller",
    "image": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074",
    "registry": "cp.icr.io",
    "registryRef": "ibm-registry",
    "release": "9.9.1.2",
    "tag": "v9.9.1-25074",
    "version": "ceph-image-v2",
}

HOST = {"cluster": {key: value for key, value in CORE.items() if key != "action"}, "host": "node-1", "version": "ceph-host-v2"}

# Role, its entry points, the request variable and the request carrying every
# member the Go side encodes, with the variables its entry point also requires.
ROLE_REQUESTS = (
    ("storage_ceph_core", ("apply", "observe", "destroy"), "bootwright_ceph", CORE),
    ("storage_ceph_image", ("resolve", "observe", "destroy"), "bootwright_ceph_image", IMAGE),
    ("storage_ceph_host", ("prepare", "observe", "destroy"), "bootwright_ceph_host", HOST),
)

ENTRY_POINTS = [(role, entry, prefix, request) for role, entries, prefix, request in ROLE_REQUESTS
                for entry in entries]
IDS = [role + "/" + entry for role, entry, _prefix, _request in ENTRY_POINTS]


def options(role, entry):
    return yaml.safe_load((ROLES / role / "meta" / "argument_specs.yml").read_text())["argument_specs"][entry][
        "options"]


def run(role, entry, prefix, request):
    """What the prepended validation of one entry point says of a request and
    the digest and material mapping a run hands it beside it, each when the
    entry point declares it."""
    declared = options(role, entry)
    values = {prefix + "_request": request, prefix + "_digest": DIGEST, prefix + "_material": {}}
    values = {name: value for name, value in values.items() if name in declared}
    return ArgumentSpecValidator(declared).validate(values, validate_role_argument_spec=True)


@pytest.mark.parametrize("role, entry, prefix, frozen", ENTRY_POINTS, ids=IDS)
def test_an_entry_point_admits_the_whole_request_the_go_side_sends(role, entry, prefix, frozen):
    result = run(role, entry, prefix, copy.deepcopy(frozen))
    assert not result.error_messages, result.error_messages


@pytest.mark.parametrize("role, entry, prefix, frozen", ENTRY_POINTS, ids=IDS)
def test_an_entry_point_still_refuses_a_member_the_go_side_never_sends(role, entry, prefix, frozen):
    extra = dict(copy.deepcopy(frozen), unsent="member")
    assert any("unsent" in message for message in run(role, entry, prefix, extra).error_messages)


@pytest.mark.parametrize("role, entry, prefix, frozen", ENTRY_POINTS, ids=IDS)
def test_an_entry_point_still_refuses_a_request_without_a_member_the_go_side_always_sends(
        role, entry, prefix, frozen):
    for required, declared in options(role, entry)[prefix + "_request"]["options"].items():
        if declared.get("required"):
            partial = {name: value for name, value in copy.deepcopy(frozen).items() if name != required}
            assert any(required in message for message in run(role, entry, prefix, partial).error_messages), required

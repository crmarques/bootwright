"""The install identity is the trust anchor this build created.

`openshift-install agent create image` persists only the agent image, the
administrator kubeconfig and the administrator password; it writes no
metadata.json (cmd/openshift-install/agent.go, agentImageTarget, in
https://raw.githubusercontent.com/openshift/installer/release-4.21/cmd/openshift-install/agent.go).
The identity is therefore the digest of the certificate authority and client
certificate in that kubeconfig, and the cluster answers with it only when a
read through the same kubeconfig verifies and authenticates.
"""

from __future__ import annotations

import base64
import hashlib
import json

import pytest

from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import observe


def pem(kind, body):
    return ("-----BEGIN %s-----\n%s\n-----END %s-----\n" % (kind, body, kind)).encode()


# Placeholder certificate bodies: the inspection hashes the bytes and never
# parses them. The authority is a bundle, as KubeAPIServerCompleteCABundle is.
AUTHORITY = pem("CERTIFICATE", "bG9hZGJhbGFuY2Vy") + pem("CERTIFICATE", "bG9jYWxob3N0") + pem("CERTIFICATE", "c2VydmljZQ==")
CLIENT = pem("CERTIFICATE", "YWRtaW4=")
KEY = pem("RSA PRIVATE KEY", "a2V5")


# Names go.yaml.in/yaml/v2 resolves as a boolean and an integer.
READS_AS_ANOTHER_TYPE = ("on", "123")


def encoded(data):
    return base64.b64encode(data).decode()


def kubeconfig(authority=AUTHORITY, client=CLIENT, cluster_extra="", tail="", name="sno"):
    """The kubeconfig AgentAdminClient writes, byte for byte in layout.

    pkg/asset/kubeconfig/agent.go names the cluster after the ClusterDeployment
    and the user and context "admin", at https://api.<name>.<domain>:6443;
    pkg/asset/kubeconfig/kubeconfig.go marshals the clientcmd v1 Config through
    sigs.k8s.io/yaml, whose go.yaml.in/yaml/v2 emitter sorts keys, leaves
    sequences inside mappings unindented and never breaks a plain scalar
    without spaces, and double-quotes a string that would read as another
    type. The vendored client-go v0.34.1 omits empty preferences.
    """
    quoted = '"%s"' % name if name in READS_AS_ANOTHER_TYPE else name
    return (
        "clusters:\n"
        "- cluster:\n"
        "    certificate-authority-data: %s\n"
        "%s"
        "    server: https://api.%s.lab.example:6443\n"
        "  name: %s\n"
        "contexts:\n"
        "- context:\n"
        "    cluster: %s\n"
        "    user: admin\n"
        "  name: admin\n"
        "current-context: admin\n"
        "%s"
        "users:\n"
        "- name: admin\n"
        "  user:\n"
        "    client-certificate-data: %s\n"
        "    client-key-data: %s\n"
    ) % (encoded(authority), cluster_extra, name, quoted, quoted, tail, encoded(client), encoded(KEY))


def work_area(root, config):
    """A work area as the agent installer leaves it after building the image.

    agent.x86_64.iso and rendezvousIP from pkg/asset/agent/image/agentimage.go,
    auth/kubeconfig from pkg/asset/kubeconfig/admin.go, auth/kubeadmin-password
    from pkg/asset/password/password.go and .openshift_install_state.json from
    pkg/asset/store/store.go, all at release-4.21. There is no metadata.json.
    """
    (root / "auth").mkdir(parents=True)
    (root / "agent.x86_64.iso").write_bytes(b"")
    (root / "rendezvousIP").write_text("192.0.2.10")
    (root / ".openshift_install_state.json").write_text(json.dumps({}))
    (root / "auth" / "kubeadmin-password").write_text("xxxxx-xxxxx-xxxxx-xxxxx")
    if config is not None:
        (root / "auth" / "kubeconfig").write_text(config)
    return root


def observed(tmp_path, config=None, metadata=None):
    work = work_area(tmp_path / "work", kubeconfig() if config is None else config)
    if metadata is not None:
        (work / "metadata.json").write_text(json.dumps(metadata))
    return observe({"workRoot": str(work), "image": {"path": str(tmp_path / "served"), "url": "https://192.0.2.1:8443"}})


def anchor(authority=AUTHORITY, client=CLIENT):
    return hashlib.sha256(authority + b"\0" + client).hexdigest()


def test_the_identity_is_the_anchor_the_image_build_wrote_and_needs_no_metadata(tmp_path):
    observation = observed(tmp_path)
    assert not (tmp_path / "work" / "metadata.json").exists()
    assert observation["identity"] == anchor()
    # The identity the Go capability fixtures carry (install_test.go).
    assert anchor() == "4245f5e001d445663c189f04c0a614168ed3c05a522d38680d1aa1ca248d6049"
    assert observation["kubeconfig"] is True
    assert observation["url"] == ""


def test_another_build_names_another_identity(tmp_path):
    other = pem("CERTIFICATE", "b3RoZXI=")
    assert observed(tmp_path / "a", kubeconfig(authority=other))["identity"] == anchor(authority=other)
    assert observed(tmp_path / "b", kubeconfig(client=other))["identity"] == anchor(client=other)
    assert anchor(authority=other) != anchor() != anchor(client=other)


# An older installer vendoring a client-go without omitzero writes empty
# preferences, and a cluster whose name reads as a boolean or a number has it
# double-quoted; nothing about the anchor changes.
@pytest.mark.parametrize("config", [kubeconfig(tail="preferences: {}\n"), kubeconfig(name="on"), kubeconfig(name="123")],
                         ids=["empty preferences", "a boolean name", "a numeric name"])
def test_what_the_installer_also_writes_leaves_the_identity_unchanged(tmp_path, config):
    assert observed(tmp_path, config)["identity"] == anchor()


@pytest.mark.parametrize("config", [
    kubeconfig(cluster_extra="    insecure-skip-tls-verify: true\n"),
    kubeconfig() + "    token: abc\n",
    kubeconfig().replace("  name: sno\n", "  name: sno\n- cluster:\n    server: https://api.other.example:6443\n  name: other\n"),
    kubeconfig().replace("- name: admin\n  user:\n", "  - name: admin\n    user:\n").replace("\n    client-", "\n      client-"),
    kubeconfig(authority=b"not a certificate"),
    kubeconfig().replace(": %s\n" % encoded(AUTHORITY), ": '%s'\n" % encoded(AUTHORITY)),
    kubeconfig().replace(encoded(CLIENT), "abc"),
    kubeconfig().replace("    certificate-authority-data: %s\n" % encoded(AUTHORITY), ""),
    kubeconfig().replace("    client-certificate-data: %s\n" % encoded(CLIENT), ""),
    kubeconfig(cluster_extra="    certificate-authority-data: %s\n" % encoded(pem("CERTIFICATE", "b3RoZXI="))),
    "",
], ids=["verification disabled", "a token", "a second cluster", "an indented sequence", "no certificate",
        "a quoted value", "undecodable", "no authority", "no client certificate", "a duplicated authority",
        "empty"])
def test_a_kubeconfig_the_installer_would_not_write_names_no_identity(tmp_path, config):
    assert observed(tmp_path, config)["identity"] == ""


def test_a_work_area_without_its_kubeconfig_names_no_identity(tmp_path):
    work = work_area(tmp_path / "work", None)
    observation = observe({"workRoot": str(work), "image": {"path": str(tmp_path), "url": "https://192.0.2.1:8443"}})
    assert observation == {"identity": "", "kubeconfig": False, "url": ""}


# The agent installer writes no metadata.json, so one found in the work area is
# stale or planted. Its clusterID, which the reader before S18 took, is never
# the identity, neither in place of the anchor nor when the kubeconfig names
# none: a read through a kubeconfig that skips verification would otherwise
# answer with an identity no anchor backs.
@pytest.mark.parametrize("config, expected", [
    (kubeconfig(), anchor()),
    (kubeconfig(cluster_extra="    insecure-skip-tls-verify: true\n"), ""),
], ids=["an anchor", "verification disabled"])
def test_a_metadata_file_in_the_work_area_never_names_the_identity(tmp_path, config, expected):
    observation = observed(tmp_path, config, metadata={"clusterID": "9d8f2c4e-6b1a-4f3d-8e2a-5c7b9d0e1f23"})
    assert (tmp_path / "work" / "metadata.json").is_file()
    assert observation["identity"] == expected

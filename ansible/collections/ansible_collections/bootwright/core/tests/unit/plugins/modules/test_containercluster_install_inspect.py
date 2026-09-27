"""The install identity is the administrator client certificate this build created.

`openshift-install agent create image` persists only the agent image, the
administrator kubeconfig and the administrator password; it writes no
metadata.json (cmd/openshift-install/agent.go, agentImageTarget, in
https://raw.githubusercontent.com/openshift/installer/release-4.21/cmd/openshift-install/agent.go).
The identity is therefore a domain-separated digest of the client certificate
in that kubeconfig, which `agent wait-for install-complete` leaves unchanged
when it rewrites the file, and the cluster answers with it only when a read
through the same kubeconfig verifies and authenticates.
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
# The ca-bundle.crt of the default-ingress-cert ConfigMap: the tls.crt of the
# default ingress certificate (certificate-publisher/controller.go:241 and
# publish_ca.go:24 in openshift/cluster-ingress-operator release-4.21), which
# the operator generates as the wildcard serving certificate followed by the
# ingress operator's CA (certificate/default_cert.go:88-105, and library-go
# crypto.go MakeServerCert:802 and EncodeCertificates:1156-1162 as it vendors
# them), each PEM block ending in a newline.
ROUTER = pem("CERTIFICATE", "d2lsZGNhcmQ=") + pem("CERTIFICATE", "aW5ncmVzcy1vcGVyYXRvcg==")


# Names go.yaml.in/yaml/v2 resolves as a boolean and an integer.
READS_AS_ANOTHER_TYPE = ("on", "123")

# An exec credential plugin under the admin user, as clientcmd.WriteToFile
# would write an ExecConfig with a command, an API version and an interactive
# mode: args, env and provideClusterInfo carry no omitempty and are written
# anyway (vendored client-go v0.34.1 tools/clientcmd/api/v1/types.go:207-247).
# apiVersion is omitempty (:222), so an ExecConfig without one is written
# without the line, and the plugin is then refused by its own keys alone.
EXEC = (
    "    exec:\n"
    "      apiVersion: client.authentication.k8s.io/v1\n"
    "      args: null\n"
    "      command: /usr/local/bin/credential-helper\n"
    "      env: null\n"
    "      interactiveMode: Never\n"
    "      provideClusterInfo: false\n"
)


def encoded(data):
    return base64.b64encode(data).decode()


def kubeconfig(authority=AUTHORITY, client=CLIENT, cluster_extra="", tail="", name="sno", typed=False):
    """The kubeconfig AgentAdminClient writes, byte for byte in layout.

    pkg/asset/kubeconfig/agent.go:28-46 names the cluster after the
    ClusterDeployment and the user and context "admin", at
    https://api.<name>.<domain>:6443; pkg/asset/kubeconfig/kubeconfig.go:30-61
    marshals the clientcmd v1 Config through sigs.k8s.io/yaml, whose
    go.yaml.in/yaml/v2 emitter sorts keys, leaves sequences inside mappings
    unindented and never breaks a plain scalar without spaces, and
    double-quotes a string that would read as another type. The vendored
    client-go v0.34.1 omits empty preferences (tools/clientcmd/api/v1/types.go:41,
    omitzero) and the unset apiVersion and kind (types.go:33-38, omitempty).
    typed adds the two lines clientcmd.WriteToFile writes, where it sorts
    them; see rewritten.
    """
    quoted = '"%s"' % name if name in READS_AS_ANOTHER_TYPE else name
    return (
        "%s"
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
        "%s"
        "users:\n"
        "- name: admin\n"
        "  user:\n"
        "    client-certificate-data: %s\n"
        "    client-key-data: %s\n"
    ) % ("apiVersion: v1\n" if typed else "", encoded(authority), cluster_extra, name, quoted, quoted,
         "kind: Config\n" if typed else "", tail, encoded(client), encoded(KEY))


def rewritten(times=1, **overrides):
    """The same kubeconfig after `agent wait-for install-complete` ran times times.

    WaitForInstallComplete calls addRouterCAToClusterCA once the cluster
    initializes (cmd/openshift-install/command/waitfor.go:65-84 in
    openshift/installer release-4.21). That loads auth/kubeconfig (:305),
    prepends the router CA bundle to each cluster's certificate-authority-data
    (:327-328) and writes the file back with clientcmd.WriteToFile (:330), on
    every run, so a second run prepends it again. WriteToFile encodes through
    clientcmdlatest.Codec (the vendored client-go v0.34.1 tools/clientcmd
    loader.go:448-465 and :495-497, api/latest/latest.go:49-61), whose
    conversion sets apiVersion v1 and kind Config (api/v1/register.go:50-53)
    and whose YAML serializer is json.Marshal then sigs.k8s.io/yaml JSONToYAML
    (apimachinery runtime/serializer/json/json.go:226-237), the emitter the
    image build used: keys stay sorted, so apiVersion leads and kind follows
    current-context, and the client certificate and key are written back
    unchanged.
    """
    values = {"authority": ROUTER * times + AUTHORITY, "typed": True}
    values.update(overrides)
    return kubeconfig(**values)


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


def anchor(client=CLIENT):
    return hashlib.sha256(b"bootwright/containercluster/identity/v2\0" + client).hexdigest()


def test_the_identity_is_the_client_certificate_the_image_build_wrote_and_needs_no_metadata(tmp_path):
    observation = observed(tmp_path)
    assert not (tmp_path / "work" / "metadata.json").exists()
    assert observation["identity"] == anchor()
    # The identity the Go capability fixtures carry (install_test.go).
    assert anchor() == "d21b91799c1b2d06e949151732b30343fc42b122f785c10685fad98570b00a02"
    assert observation["kubeconfig"] is True
    assert observation["url"] == ""


# The prefix is versioned, so the identity equals neither a bare digest of the
# certificate nor the digest of authority and client certificate that the
# inspection named before S26.
def test_the_identity_is_domain_separated():
    assert anchor() != hashlib.sha256(CLIENT).hexdigest()
    assert anchor() != hashlib.sha256(AUTHORITY + b"\0" + CLIENT).hexdigest()


def test_the_install_complete_rewrite_leaves_the_identity_unchanged(tmp_path):
    forms = {"as built": kubeconfig(), "rewritten": rewritten(), "rewritten twice": rewritten(2)}
    assert len(set(forms.values())) == len(forms)
    identities = {name: observed(tmp_path / name, config)["identity"] for name, config in forms.items()}
    assert identities == dict.fromkeys(forms, anchor())


# Each run prepends the router bundle again, so the identity outlives only as
# many rewrites as fit within the 64 KiB the inspection reads; a file one
# rewrite past that bound, in the same shape, names none.
def test_the_identity_outlives_rewrites_only_within_the_read_bound(tmp_path):
    times = 2
    while len(rewritten(times + 1).encode()) <= 64 * 1024:
        times += 1
    assert observed(tmp_path / "within", rewritten(times))["identity"] == anchor()
    assert observed(tmp_path / "past", rewritten(times + 1))["identity"] == ""


def test_only_another_client_certificate_names_another_identity(tmp_path):
    other = pem("CERTIFICATE", "b3RoZXI=")
    assert observed(tmp_path / "a", kubeconfig(authority=other))["identity"] == anchor()
    assert observed(tmp_path / "b", kubeconfig(client=other))["identity"] == anchor(client=other)
    assert observed(tmp_path / "c", rewritten(client=other))["identity"] == anchor(client=other) != anchor()


# An older installer vendoring a client-go without omitzero writes empty
# preferences, and a cluster whose name reads as a boolean or a number has it
# double-quoted; nothing about the anchor changes.
@pytest.mark.parametrize("config", [
    kubeconfig(tail="preferences: {}\n"), kubeconfig(name="on"), kubeconfig(name="123"),
    rewritten(tail="preferences: {}\n"), rewritten(name="on"),
], ids=["empty preferences", "a boolean name", "a numeric name", "rewritten with empty preferences",
        "rewritten with a boolean name"])
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
    rewritten(cluster_extra="    insecure-skip-tls-verify: true\n"),
    rewritten() + "    token: abc\n",
    rewritten() + EXEC,
    rewritten() + EXEC.replace("      apiVersion: client.authentication.k8s.io/v1\n", ""),
    rewritten() + "    tokenFile: /var/run/secrets/token\n",
    rewritten() + "    password: xxxxx-xxxxx-xxxxx-xxxxx\n    username: kubeadmin\n",
    rewritten().replace("  user:\n", "  user:\n    auth-provider:\n      config: null\n      name: oidc\n"),
    rewritten().replace("    certificate-authority-data:",
                        "    certificate-authority: /etc/pki/ca.crt\n    certificate-authority-data:"),
    rewritten().replace("kind: Config\n", ""),
    rewritten().replace("apiVersion: v1\n", ""),
    rewritten().replace("apiVersion: v1\n", "apiVersion: v2\n"),
    rewritten().replace("kind: Config\n", "kind: Foo\n"),
    rewritten().replace("apiVersion: v1\n", "").replace("    server:", "    apiVersion: v1\n    server:"),
    rewritten().replace("apiVersion: v1\n", "apiVersion: v1\napiVersion: v1\n"),
    rewritten().replace("apiVersion: v1\n", "apiVersion: v1\napiVersion: v1\n").replace(
        "kind: Config\n", "kind: Config\nkind: Config\n"),
    rewritten().replace("apiVersion: v1\n", '"apiVersion": v1\n'),
    kubeconfig().replace("current-context: admin\n", "current-context: admin\n- apiVersion: v1\n- kind: Config\n"),
    rewritten().replace("kind: Config\n", "kind: Config\n- cluster:\n    server: https://api.other.example:6443\n"),
    rewritten().replace("apiVersion: v1\n", "").replace("- cluster:\n", "- apiVersion: v1\n  cluster:\n"),
    rewritten().replace("kind: Config\n", "").replace("- name: admin\n  user:", "- kind: Config\n  name: admin\n  user:"),
    rewritten().replace("    certificate-authority-data: %s\n" % encoded(ROUTER + AUTHORITY), ""),
    rewritten().replace("    client-certificate-data: %s\n" % encoded(CLIENT), ""),
], ids=["verification disabled", "a token", "a second cluster", "an indented sequence", "no certificate",
        "a quoted value", "undecodable", "no authority", "no client certificate", "a duplicated authority",
        "empty", "rewritten with verification disabled", "rewritten with a token", "an exec plugin",
        "an exec plugin without apiVersion",
        "a token file", "a username and password", "an auth provider", "an authority path",
        "apiVersion without kind", "kind without apiVersion", "apiVersion v2", "kind Foo",
        "an indented apiVersion", "a duplicated apiVersion", "duplicated apiVersion and kind",
        "a quoted apiVersion key", "apiVersion and kind as sequence items", "an item under kind",
        "apiVersion in the cluster item", "kind in the user item",
        "rewritten without authority", "rewritten without client certificate"])
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

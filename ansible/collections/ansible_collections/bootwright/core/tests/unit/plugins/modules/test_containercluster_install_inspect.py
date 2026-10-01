"""The install identity is the administrator client certificate this build created.

`openshift-install agent create image` persists only the agent image, the
administrator kubeconfig and the administrator password; it writes no
metadata.json (cmd/openshift-install/agent.go, agentImageTarget, in
https://raw.githubusercontent.com/openshift/installer/release-4.21/cmd/openshift-install/agent.go).
The identity is therefore a domain-separated digest of the client certificate
in that kubeconfig, which `agent wait-for install-complete` leaves unchanged
when it rewrites the file, and the cluster answers with it only when a read
through the same kubeconfig verifies and authenticates. That kubeconfig is the
copy the installation keeps of the installer's file while it is whole, so the
installer's in-place rewrite can neither grow it past the read bound nor leave
it cut short.
"""

from __future__ import annotations

import base64
import hashlib
import http.server
import json
import os
import socket
import stat
import threading
import time

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect as inspect
from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import (
    AGENT_CONFIG, AUTH_CONFIG, KEPT, MAX_KUBECONFIG, REFUSED, RESTORED, STATE, UNTOUCHED, admitted, fetch, keep, named,
    observe, registration, restore)


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


def request(tmp_path, work):
    return {"workRoot": str(work), "image": {"path": str(tmp_path / "served"), "url": "https://192.0.2.1:8443"}}


def observed(tmp_path, config=None, metadata=None):
    """What an apply's first inspection names: it keeps the installer's file, then observes."""
    work = work_area(tmp_path / "work", kubeconfig() if config is None else config)
    if metadata is not None:
        (work / "metadata.json").write_text(json.dumps(metadata))
    keep(request(tmp_path, work))
    return observe(request(tmp_path, work))


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


# Each run prepends the router bundle again, so an installer's file is kept
# only while it fits within the 64 KiB the inspection reads; a file one rewrite
# past that bound, in the same shape, is never kept and names none.
def test_the_identity_outlives_rewrites_only_within_the_read_bound(tmp_path):
    times = 2
    while len(rewritten(times + 1).encode()) <= 64 * 1024:
        times += 1
    assert observed(tmp_path / "within", rewritten(times))["identity"] == anchor()
    assert observed(tmp_path / "past", rewritten(times + 1))["identity"] == ""


def last_within_the_bound():
    times = 1
    while len(rewritten(times + 1).encode()) <= MAX_KUBECONFIG:
        times += 1
    return times


# A retried installation wait runs install-complete again after each give-up it
# resumes, and a later apply runs it again, each run prepending the router
# bundle to the installer's file once more. The kept copy follows each rewrite
# while it fits the bound and then stays at the last one that did, so every
# read goes through a file within the bound and keeps naming this build.
def test_reads_stay_within_the_bound_over_repeated_waits(tmp_path):
    work = work_area(tmp_path / "work", kubeconfig())
    installer, kept = work / "auth" / "kubeconfig", work / KEPT
    assert keep(request(tmp_path, work)) is True
    assert kept.read_text() == kubeconfig()
    last = last_within_the_bound()
    for times in range(1, last + 6):
        installer.write_text(rewritten(times))
        assert keep(request(tmp_path, work)) is (times <= last), times
        assert observe(request(tmp_path, work))["identity"] == anchor(), times
        assert kept.stat().st_size <= MAX_KUBECONFIG, times
        assert kept.read_text() == rewritten(min(times, last)), times
    assert installer.stat().st_size > MAX_KUBECONFIG


# clientcmd.WriteToFile writes through os.WriteFile, which truncates the file
# before it writes, so a budget kill during the installer's rewrite leaves any
# prefix of the new file. No proper prefix names an identity, and none replaces
# the copy an earlier inspection kept, which keeps naming this build.
@pytest.mark.parametrize("config", [kubeconfig(), rewritten(), kubeconfig(tail="preferences: {}\n")],
                         ids=["as built", "rewritten", "with empty preferences"])
def test_a_truncated_file_refuses_rather_than_misreads(tmp_path, config):
    data = config.encode()
    assert named(data) == anchor()
    prefixes = [data[:length] for length in range(len(data))]
    assert not [prefix for prefix in prefixes if named(prefix)]
    work = work_area(tmp_path / "work", kubeconfig())
    assert keep(request(tmp_path, work)) is True
    installer, kept = work / "auth" / "kubeconfig", work / KEPT
    for prefix in prefixes[::7] + [data[:data.rindex(b"\n", 0, len(data) - 1) + 1], data[:-1]]:
        installer.write_bytes(prefix)
        assert keep(request(tmp_path, work)) is False
        assert kept.read_text() == kubeconfig()
        assert observe(request(tmp_path, work))["identity"] == anchor()


# The cut the inspection once misread: a prefix ending inside the client key at
# a base64 boundary kept every line the shape check read, so it named this
# build's identity from a file no read could authenticate with. The same cut
# with a newline after it is refused too, by the key's own end line.
@pytest.mark.parametrize("newline", [b"", b"\n"], ids=["as cut", "with a newline after the cut"])
def test_a_file_cut_inside_the_client_key_names_no_identity(tmp_path, newline):
    data = rewritten().encode()
    key = data.rindex(b"client-key-data: ") + len(b"client-key-data: ")
    cut = data[:key + 4 * ((len(data) - key - 1) // 8)] + newline
    assert len(cut) - len(newline) > key and cut != data
    assert named(cut) == ""
    assert observed(tmp_path, cut.decode())["identity"] == ""


def test_the_first_kept_copy_is_the_whole_installer_file_private_to_its_owner(tmp_path):
    work = work_area(tmp_path / "work", rewritten())
    assert keep(request(tmp_path, work)) is True
    kept = work / KEPT
    assert kept.read_text() == rewritten()
    assert stat.S_IMODE(kept.stat().st_mode) == 0o600
    assert sorted(os.listdir(work)) == sorted([".openshift_install_state.json", KEPT, "agent.x86_64.iso", "auth",
                                               "rendezvousIP"])
    assert keep(request(tmp_path, work)) is False


# A kept copy is never replaced by a file naming another identity, and a kept
# copy that names none is never replaced at all: the copy anchors the identity
# every read proves, so losing it refuses rather than adopting another.
@pytest.mark.parametrize("before, expected", [
    (kubeconfig(client=pem("CERTIFICATE", "b3RoZXI=")), anchor(client=pem("CERTIFICATE", "b3RoZXI="))),
    ("", ""),
    (kubeconfig()[:-40], ""),
], ids=["another identity", "empty", "cut short"])
def test_a_kept_copy_changes_only_to_the_identity_it_names(tmp_path, before, expected):
    work = work_area(tmp_path / "work", rewritten())
    (work / KEPT).write_text(before)
    assert keep(request(tmp_path, work)) is False
    assert (work / KEPT).read_text() == before
    assert observe(request(tmp_path, work))["identity"] == expected


def test_an_observation_keeps_nothing_and_reads_only_the_kept_copy(tmp_path):
    work = work_area(tmp_path / "work", rewritten())
    assert observe(request(tmp_path, work)) == {"identity": "", "kubeconfig": False, "url": ""}
    assert not (work / KEPT).exists()


def test_check_mode_reports_the_copy_it_would_keep_and_writes_nothing(tmp_path):
    work = work_area(tmp_path / "work", kubeconfig())
    assert keep(request(tmp_path, work), check_mode=True) is True
    assert sorted(os.listdir(work)) == sorted([".openshift_install_state.json", "agent.x86_64.iso", "auth",
                                               "rendezvousIP"])


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


OTHER = pem("CERTIFICATE", "b3RoZXI=")


def restoring(root, installer, kept):
    """A work area whose installer file and kept copy hold these bytes; None leaves either out."""
    work = work_area(root, None)
    for path, data in ((work / "auth" / "kubeconfig", installer), (work / KEPT, kept)):
        if data is not None:
            path.write_bytes(data)
    return work


# `agent wait-for` loads auth/kubeconfig from its asset directory itself
# (cmd/openshift-install/agent/waitfor.go joins it as
# filepath.Join(assetDir, "auth", "kubeconfig") for both milestones, and
# pkg/agent/cluster.go NewCluster ends in logrus.Fatal when
# NewClusterKubeAPIClient cannot load it, release-4.21), so a file a kill cut
# short stops every later wait. Any prefix of the installer's write is put
# back whole from the kept copy, in one rename and private to its owner, which
# leaves the kept copy and the identity as they were.
@pytest.mark.parametrize("config", [kubeconfig(), rewritten()], ids=["as built", "rewritten"])
def test_a_cut_short_installer_file_is_restored_from_the_kept_copy(tmp_path, config):
    data, kept = config.encode(), rewritten().encode()
    prefixes = [data[:length] for length in range(len(data))]
    for position, prefix in enumerate(prefixes[::7] + [data[:data.rindex(b"\n", 0, len(data) - 1) + 1], data[:-1]]):
        work = restoring(tmp_path / str(position), prefix, kept)
        installer = work / "auth" / "kubeconfig"
        assert restore(request(tmp_path, work), anchor()) == RESTORED, len(prefix)
        assert installer.read_bytes() == kept and (work / KEPT).read_bytes() == kept
        assert stat.S_IMODE(installer.stat().st_mode) == 0o600
        assert sorted(os.listdir(work / "auth")) == ["kubeadmin-password", "kubeconfig"]
        assert observe(request(tmp_path, work))["identity"] == anchor()
        assert restore(request(tmp_path, work), anchor()) == UNTOUCHED


# A restore hands the installer only a kept copy that still names the identity
# the apply took from it before its first effect. A copy of another cluster, or
# one that names none, refuses and changes nothing, and so does an apply that
# recorded no identity.
@pytest.mark.parametrize("kept, recorded", [
    (kubeconfig(client=OTHER).encode(), anchor()),
    (b"", anchor()),
    (kubeconfig().encode()[:-40], anchor()),
    (None, anchor()),
    (kubeconfig().encode(), ""),
    (kubeconfig().encode(), anchor(client=OTHER)),
], ids=["another cluster", "empty", "cut short", "missing", "no identity recorded", "another identity recorded"])
def test_a_restore_never_hands_the_installer_a_copy_of_another_cluster(tmp_path, kept, recorded):
    cut = rewritten().encode()[:200]
    work = restoring(tmp_path / "work", cut, kept)
    assert restore(request(tmp_path, work), recorded) == REFUSED
    assert (work / "auth" / "kubeconfig").read_bytes() == cut
    assert sorted(os.listdir(work / "auth")) == ["kubeadmin-password", "kubeconfig"]
    if kept is None:
        assert not (work / KEPT).exists()
    else:
        assert (work / KEPT).read_bytes() == kept


# A file that names an identity parses, whichever identity it names, so it is
# the installer's to read. One past the bound, missing, or not a regular file
# is never read, so nothing shows that it does not parse, and it is left too.
@pytest.mark.parametrize("installer", [
    kubeconfig().encode(), rewritten(client=OTHER).encode(), "past the bound", None, "a link",
], ids=["this build's", "another identity", "past the bound", "missing", "a link to a cut file"])
def test_an_installer_file_that_parses_or_is_never_read_is_left_as_it_is(tmp_path, installer):
    if installer == "past the bound":
        installer = rewritten(last_within_the_bound() + 1).encode()
        assert len(installer) > MAX_KUBECONFIG
    work = restoring(tmp_path / "work", None if installer == "a link" else installer, kubeconfig().encode())
    target = work / "auth" / "kubeconfig"
    cut = kubeconfig().encode()[:200]
    if installer == "a link":
        (tmp_path / "cut").write_bytes(cut)
        target.symlink_to(tmp_path / "cut")
    assert restore(request(tmp_path, work), anchor()) == UNTOUCHED
    if installer == "a link":
        assert target.is_symlink() and (tmp_path / "cut").read_bytes() == cut
    elif installer is None:
        assert not target.exists()
    else:
        assert target.read_bytes() == installer


def test_check_mode_reports_the_restore_it_would_make_and_writes_nothing(tmp_path):
    cut = kubeconfig().encode()[:200]
    work = restoring(tmp_path / "work", cut, kubeconfig().encode())
    assert restore(request(tmp_path, work), anchor(), check_mode=True) == RESTORED
    assert (work / "auth" / "kubeconfig").read_bytes() == cut


def run(arguments, capsys):
    """One module run in process, as a task would make it, and what it returned."""
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        inspect.main()
    return json.loads(capsys.readouterr().out)


# The role reads the outcome from the result, never from a message, because
# no_log censors the task's output along with the published image address.
def test_the_module_reports_the_restore_and_refuses_it_beside_keep(tmp_path, capsys):
    work = restoring(tmp_path / "work", kubeconfig().encode()[:200], kubeconfig().encode())
    arguments = {"request": request(tmp_path, work), "restore": True, "identity": anchor()}
    result = run(arguments, capsys)
    assert result["restore"] == RESTORED and result["changed"] is True
    assert result["observation"]["identity"] == anchor()
    result = run(arguments, capsys)
    assert result["restore"] == UNTOUCHED and result["changed"] is False
    assert "restore" not in run({"request": request(tmp_path, work), "keep": True}, capsys)
    result = run(dict(arguments, keep=True), capsys)
    assert result["failed"] is True and result["msg"] == "keep and restore are exclusive"


# After a wait stalls, the role reads once which declared nodes registered with
# the assisted service on the rendezvous host (B32, D49). The asset state below
# carries the two members that read takes, in the shape openshift-install
# 4.21.10 (built from 6285755d) wrote them when `agent create config-image`
# ran over a one-node agent-config.yaml: *gencrypto.AuthConfig with PublicKey,
# AgentAuthToken, UserAuthToken, WatcherAuthToken, AuthTokenExpiry and
# AuthType, and *agentconfig.AgentConfig with File, Config and Template, whose
# Config carries kind, apiVersion, metadata, rendezvousIP and hosts. The
# installer's token is an ES256 JWT; this one is a placeholder.
TOKEN = "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.cGxhY2Vob2xkZXI.c2lnbmF0dXJl"
NODES = [{"name": "master-0", "address": "127.0.0.1"}, {"name": "master-1", "address": "192.0.2.11"},
         {"name": "master-2", "address": "192.0.2.12"}]


def asset_state(root, rendezvous="127.0.0.1", token=TOKEN):
    """A work area holding the installer's asset state."""
    root.mkdir(parents=True, exist_ok=True)
    (root / STATE).write_text(json.dumps({
        AUTH_CONFIG: {"PublicKey": "", "AgentAuthToken": "agent", "UserAuthToken": "user", "WatcherAuthToken": token,
                      "AuthTokenExpiry": "", "AuthType": "agent-installer-local"},
        AGENT_CONFIG: {"File": {"Filename": "agent-config.yaml", "Data": ""}, "Template": "",
                       "Config": {"kind": "AgentConfig", "apiVersion": "v1beta1", "metadata": {"name": "sno"},
                                  "rendezvousIP": rendezvous, "hosts": []}},
    }))
    return root


def declared(tmp_path, work):
    return dict(request(tmp_path, work), nodes=NODES)


def host(requested="", reported=""):
    """One registered host as the service lists it. The assisted-service
    models the installer vendors name its members (models.Host's
    RequestedHostname as requested_hostname and Inventory, a text column, as
    inventory, both omitted when empty); the inventory is the JSON document
    the host's agent reports, whose hostname the image's set-hostname.sh sets
    to the declared name when one of the host's MAC addresses matches."""
    entry = {"id": "8f1b4e2a-0000-4000-8000-000000000001", "role": "master", "status": "known"}
    if requested:
        entry["requested_hostname"] = requested
    if reported:
        entry["inventory"] = json.dumps({"hostname": reported})
    return entry


def listed(*hosts, identity="6a4ba3e8-0000-4000-8000-000000000000"):
    """What v2ListClusters answers with its hosts: one cluster, whose hosts
    models.Cluster names hosts."""
    return json.dumps([{"id": identity, "name": "sno", "status": "ready", "hosts": list(hosts)}]).encode()


# The read's wire contract, spelled here rather than taken from the module, so
# a test fails when the module's spelling drifts from it: port 8090 and the
# base the image's common.sh builds from SERVICE_BASE_URL, which the Waiting
# paragraph of specs/container-clusters.md states, the cluster list asked with
# its hosts, and the header the installer's watcher-token client sends the
# token in (openshift-install 4.21.10 carries "/v2/clusters", "with_hosts" and
# "Watcher-Authorization"), with the time bound that paragraph states.
PORT = 8090
SECONDS = 10
READ = ("/api/assisted-install/v2/clusters?with_hosts=true", TOKEN)


class Service(http.server.BaseHTTPRequestHandler):
    """The assisted service on the rendezvous host: it records each request
    and answers with the status, body and headers it was given."""

    def do_GET(self):
        self.server.seen.append((self.path, self.headers.get("Watcher-Authorization")))
        status, body, headers = self.server.answer
        self.send_response(status)
        for name, value in headers.items():
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        return


@pytest.fixture(name="service")
def service_fixture(request, monkeypatch):
    server = http.server.HTTPServer(("127.0.0.1", 0), Service)
    server.seen, server.answer = [], (200, listed(host(reported="master-0")), {})
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    monkeypatch.setattr(inspect, "SERVICE_PORT", server.server_address[1])

    def stop():
        server.shutdown()
        server.server_close()
        thread.join()

    request.addfinalizer(stop)
    return server


def test_the_read_names_each_declared_node_no_registered_host_carries_the_name_of(tmp_path, service):
    work = asset_state(tmp_path / "work")
    service.answer = (200, listed(host(reported="master-0"), host(requested="master-2", reported="localhost")), {})
    assert registration(declared(tmp_path, work)) == {"read": True, "unregistered": ["master-1"]}
    # One request, to the clusters with their hosts, carrying the watcher token
    # in the header the service reads it from.
    assert service.seen == [READ]
    service.answer = (200, listed(host(reported="master-2"), host(requested="master-1"), host(reported="master-0")), {})
    assert registration(declared(tmp_path, work)) == {"read": True, "unregistered": []}


class Refused:
    """A connection to the rendezvous host that records where it was opened and is refused."""

    opened = []

    def __init__(self, address, port, timeout=None):
        self.opened.append((address, port, timeout))

    def request(self, method, path, headers=None):
        raise ConnectionRefusedError(method, path, headers)

    def close(self):
        return


def test_the_read_reaches_the_rendezvous_address_on_the_port_the_image_serves_the_service_on(tmp_path, monkeypatch):
    monkeypatch.setattr(Refused, "opened", [])
    monkeypatch.setattr(inspect.http.client, "HTTPConnection", Refused)
    assert registration(declared(tmp_path, asset_state(tmp_path / "work"))) == {"read": False, "unregistered": []}
    assert Refused.opened == [("127.0.0.1", PORT, SECONDS)]


# The address comes from a file, so the token goes only to an address the
# frozen request declares a node at, in whichever form the file spells it.
def test_the_token_goes_only_to_a_rendezvous_address_the_request_declares(tmp_path, service):
    for rendezvous in ("192.0.2.99", "127.0.0.2", "localhost", ""):
        work = asset_state(tmp_path / ("work-" + rendezvous), rendezvous=rendezvous)
        assert registration(declared(tmp_path, work)) == {"read": False, "unregistered": []}
    assert service.seen == []
    assert admitted("fd00:0::10", {"nodes": [{"address": "192.0.2.10"}, {"address": "fd00::10"}]}) == "fd00::10"
    assert admitted("192.0.2.10", {"nodes": [{"address": "not an address"}, {"address": "192.0.2.10"}]}) == "192.0.2.10"
    assert admitted("192.0.2.10", {"nodes": []}) == ""


# The rendezvous host registers itself, so an answer naming no host, any
# cluster but exactly one, or a host without a name proves nothing about which
# declared node is missing, and neither does any status but 200. A redirect
# is never followed.
@pytest.mark.parametrize("status, body, headers", [
    (401, b'{"code":"401","reason":"unauthorized"}', {}),
    (302, b"", {"Location": "http://127.0.0.1:1/api/assisted-install/v2/clusters"}),
    (200, b"not json", {}),
    (200, b"[]", {}),
    (200, json.dumps(json.loads(listed(host(reported="master-0"))) + json.loads(listed(
        host(reported="master-1"), host(reported="master-2"), identity="6a4ba3e8-0000-4000-8000-000000000001"))).encode(), {}),
    (200, listed(), {}),
    (200, json.dumps([{"id": "6a4ba3e8-0000-4000-8000-000000000000", "hosts": None}]).encode(), {}),
    (200, listed(host(reported="master-0"), host()), {}),
    (200, listed(host(reported="master-0"), dict(host(), inventory="{\"hostname\": \"\"}")), {}),
    (200, listed(host(reported="master-0"), dict(host(), inventory="not json")), {}),
], ids=["unauthorized", "redirect", "not json", "no cluster", "two clusters", "no host", "hosts null",
        "a host without a name", "an empty inventory name", "an inventory that does not parse"])
def test_an_answer_that_cannot_prove_which_node_is_missing_names_none(tmp_path, service, status, body, headers):
    service.answer = (status, body, headers)
    assert registration(declared(tmp_path, asset_state(tmp_path / "work"))) == {"read": False, "unregistered": []}
    assert service.seen == [READ]


@pytest.mark.parametrize("state", [
    None, b"not json", b"[]", json.dumps({AGENT_CONFIG: {"Config": {"rendezvousIP": "127.0.0.1"}}}).encode(),
    json.dumps({AUTH_CONFIG: {"WatcherAuthToken": ""}, AGENT_CONFIG: {"Config": {"rendezvousIP": "127.0.0.1"}}}).encode(),
    json.dumps({AUTH_CONFIG: {"WatcherAuthToken": TOKEN}, AGENT_CONFIG: {"Config": {}}}).encode(),
], ids=["missing", "not json", "not an object", "no auth config", "an empty token", "no rendezvous address"])
def test_an_asset_state_the_read_cannot_take_reaches_nothing(tmp_path, service, state):
    work = tmp_path / "work"
    work.mkdir()
    if state is not None:
        (work / STATE).write_bytes(state)
    assert registration(declared(tmp_path, work)) == {"read": False, "unregistered": []}
    assert service.seen == []


def test_an_asset_state_past_its_bound_or_not_a_regular_file_is_never_read(tmp_path, service, monkeypatch):
    work = asset_state(tmp_path / "work")
    monkeypatch.setattr(inspect, "MAX_STATE", (work / STATE).stat().st_size - 1)
    assert registration(declared(tmp_path, work)) == {"read": False, "unregistered": []}
    monkeypatch.undo()
    monkeypatch.setattr(inspect, "SERVICE_PORT", service.server_address[1])
    linked = tmp_path / "linked"
    linked.mkdir()
    os.symlink(work / STATE, linked / STATE)
    assert registration(declared(tmp_path, linked)) == {"read": False, "unregistered": []}
    assert service.seen == []
    assert registration(declared(tmp_path, work)) == {"read": True, "unregistered": ["master-1", "master-2"]}


def test_the_read_is_bounded_in_size(service):
    body = listed(host(reported="master-0"))
    service.answer = (200, body, {})
    port = service.server_address[1]
    assert fetch("127.0.0.1", TOKEN, port, limit=len(body)) == body
    assert fetch("127.0.0.1", TOKEN, port, limit=len(body) - 1) is None


class Silent:
    """A rendezvous host that accepts the connection, then sends the headers
    and a byte at a time, each well within any socket timeout, or nothing at
    all."""

    def __init__(self, trickle):
        self.trickle = trickle
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen(1)
        self.port = self.listener.getsockname()[1]
        self.done = threading.Event()
        self.thread = threading.Thread(target=self.serve, daemon=True)
        self.thread.start()

    def serve(self):
        connection = self.listener.accept()[0]
        with connection:
            connection.recv(65536)
            if self.trickle:
                connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 100000\r\n\r\n")
            while not self.done.wait(0.05):
                if self.trickle:
                    try:
                        connection.sendall(b" ")
                    except OSError:
                        return

    def stop(self):
        self.done.set()
        self.thread.join()
        self.listener.close()


@pytest.mark.parametrize("trickle", [False, True], ids=["nothing", "a byte at a time"])
def test_the_read_is_bounded_in_time_however_slowly_the_answer_arrives(trickle):
    silent = Silent(trickle)
    try:
        started = time.monotonic()
        assert fetch("127.0.0.1", TOKEN, silent.port, seconds=0.5) is None
        assert time.monotonic() - started < 2
    finally:
        silent.stop()


def test_the_module_reports_the_registration_and_never_the_token(tmp_path, service, capsys):
    arguments = {"request": declared(tmp_path, asset_state(tmp_path / "work")), "registered": True}
    result = run(arguments, capsys)
    assert result["registration"] == {"read": True, "unregistered": ["master-1", "master-2"]}
    assert result["changed"] is False and TOKEN not in json.dumps(result)
    assert "registration" not in run(dict(arguments, registered=False), capsys)
    for other in ({"keep": True}, {"restore": True, "identity": anchor()}):
        result = run(dict(arguments, **other), capsys)
        assert result["failed"] is True and result["msg"] == "registered only reads, so it is exclusive with keep and restore"
    assert service.seen == [READ]

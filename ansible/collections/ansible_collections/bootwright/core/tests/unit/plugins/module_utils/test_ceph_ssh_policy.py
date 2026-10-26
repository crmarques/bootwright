"""A Ceph controller session keeps the controller's SSH crypto policy and pins only admitted host-key algorithms.

The evaluated lists below are the host-key algorithm lists `ssh -G` reports.
DEFAULT and FIPS follow the crypto-policies OpenSSH back ends
(https://gitlab.com/redhat-crypto/fedora-crypto-policies, policies/DEFAULT.pol
and policies/FIPS.pol; RHEL 9 OpenSSH refuses Ed25519 in FIPS mode, per the
CentOS Stream 9 openssh-7.7p1-fips.patch). OPENSSH_DEFAULT is the client's own
list with no directive (KEX_DEFAULT_PK_ALG in openssh-portable myproposal.h).
"""

from __future__ import annotations

import base64
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    adapter_protocol, bounded_process, ceph_core, ceph_native, ceph_ssh_policy, ceph_tunnel, managedos_guest_ssh, ssh_key_policy,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_host_execution as host_fixtures

DEFAULT = ("ecdsa-sha2-nistp256,ecdsa-sha2-nistp256-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,ecdsa-sha2-nistp384,"
           "ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519,"
           "ssh-ed25519-cert-v01@openssh.com,sk-ssh-ed25519@openssh.com,rsa-sha2-256,rsa-sha2-256-cert-v01@openssh.com,rsa-sha2-512,"
           "rsa-sha2-512-cert-v01@openssh.com")
FIPS = ("ecdsa-sha2-nistp256,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384,ecdsa-sha2-nistp384-cert-v01@openssh.com,"
        "ecdsa-sha2-nistp521,ecdsa-sha2-nistp521-cert-v01@openssh.com,rsa-sha2-256,rsa-sha2-256-cert-v01@openssh.com,rsa-sha2-512,"
        "rsa-sha2-512-cert-v01@openssh.com")
OPENSSH_DEFAULT = ("ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,"
                   "rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,"
                   "rsa-sha2-512,rsa-sha2-256")
FLEET = "ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"
CONFIG = "Ciphers aes256-gcm@openssh.com\n"
HOSTS = [{"name": "node-1", "address": "192.0.2.1", "port": 22}, {"name": "node-2", "address": "192.0.2.2", "port": 22}]
OK = b'{"ok":true,"result":{"changed":false}}'

core_request = core_fixtures.core_request
host_request = host_fixtures.host_request
request_value = host_fixtures.request_value


def key(key_type):
    name = key_type.encode()
    return key_type + " " + base64.b64encode(len(name).to_bytes(4, "big") + name + (32).to_bytes(4, "big") + b"k" * 32).decode()


def known(*pins):
    return "".join(address + " " + key(key_type) + "\n" for address, key_type in pins).encode()


def policy(listed):
    return {"configuration": CONFIG, "hostKeyAlgorithms": listed}


def native(listed):
    """A fake ssh -G evaluation, as test_ssh_key_policy.py fakes it."""
    def run(argv, data, timeout, limit, environment, **options):
        assert argv == ["/usr/bin/ssh", "-G", "-F", "/dev/stdin", "-o", "CanonicalizeHostname=no", "localhost"]
        assert timeout == 10 and options == {"capture_stderr": False}
        return bounded_process.Result(0, ("hostkeyalgorithms %s\npubkeyacceptedalgorithms %s\n" % (listed, FLEET)).encode(), b"")
    return run


def transport(request, tmp_path, pins, listed=DEFAULT, run=None, **options):
    paths = {"sshKey": tmp_path / "identity", "knownHosts": tmp_path / "known"}
    paths["sshKey"].write_text("PRIVATE-KEY-SENTINEL")
    paths["knownHosts"].write_bytes(known(*pins))
    for path in paths.values():
        path.chmod(0o600)
    return ceph_core.Transport(request, {name: str(path) for name, path in paths.items()}, run=run or (lambda argv, data, timeout: (0, OK)),
                               policy=policy(listed), **options)


def options(argv):
    return [argv[index + 1] for index, value in enumerate(argv) if value == "-o"]


def filtered(key_type, listed):
    return ",".join(name for name in ssh_key_policy.ALGORITHMS[key_type].split(",") if name in listed.split(","))


def test_ceph_transport_uses_the_reduced_policy_config_not_dev_null(core_request, tmp_path):
    seen = []

    def run(argv, data, timeout):
        config = argv[argv.index("-F") + 1]
        seen.append((config, open(config, "rb").read(), os.stat(config).st_mode & 0o777))
        return 0, OK

    transport(core_request, tmp_path, [("192.0.2.1", "ssh-ed25519")], run=run).call(core_request["hosts"][0], "observe")
    assert len(seen) == 1 and seen[0][0] != "/dev/null" and os.path.basename(seen[0][0]) == "ssh_config"
    assert seen[0][1:] == (CONFIG.encode(), 0o600)
    with pytest.raises(ceph_native.Refusal, match="^transport$"):
        ceph_core.ssh_argv(core_request["hosts"][0], "/k", "/h", "x", ssh_config="/dev/null", host_key_algorithms="ssh-ed25519")


@pytest.mark.parametrize("listed", [DEFAULT, FIPS, OPENSSH_DEFAULT], ids=["default", "fips", "no-directive"])
@pytest.mark.parametrize("key_type", sorted(ceph_tunnel.HOST_KEY_ALGORITHMS))
def test_ceph_transport_host_key_algorithms_are_the_intersection_with_the_policy(key_type, listed):
    pins = known(("192.0.2.1", key_type))
    want = filtered(key_type, listed)
    if not want:
        with pytest.raises(ceph_ssh_policy.PolicyRefusal, match="^ssh-policy-host-excluded$"):
            ceph_ssh_policy.host_key_algorithms(policy(listed), pins, HOSTS[0])
        return
    assert ceph_ssh_policy.host_key_algorithms(policy(listed), pins, HOSTS[0]) == want
    if key_type == "ssh-rsa":
        assert want == "rsa-sha2-512,rsa-sha2-256"


def test_ceph_transport_refuses_an_empty_intersection_naming_the_host(core_request, tmp_path):
    runs = []
    pins = [("192.0.2.1", "ecdsa-sha2-nistp256"), ("192.0.2.2", "ssh-ed25519")]
    connection = transport(core_request, tmp_path, pins, FIPS, run=lambda argv, data, timeout: runs.append(argv) or (0, OK))
    with pytest.raises(ceph_ssh_policy.PolicyRefusal) as refused:
        connection.call(core_request["hosts"][0], "observe")
    assert str(refused.value) == "ssh-policy-host-excluded" and refused.value.detail == "node-2:ssh-ed25519"
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS) == ("ssh-policy-host-excluded", "node-2:ssh-ed25519")
    assert ceph_core.refusal(refused.value, ceph_core.HOST_REASONS) == ("ssh-policy-host-excluded", "node-2:ssh-ed25519")
    assert ceph_core.refusal(refused.value, ceph_core.SMB_REASONS) == ("ssh-policy-host-excluded", "node-2:ssh-ed25519")
    assert runs == []


def test_ceph_transport_under_fips_policy_admits_ecdsa_and_refuses_ed25519_pins():
    for key_type in ("ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"):
        assert ceph_ssh_policy.host_key_algorithms(policy(FIPS), known(("192.0.2.2", key_type)), HOSTS[1]) == key_type
    assert ceph_ssh_policy.host_key_algorithms(policy(FIPS), known(("192.0.2.2", "ssh-rsa")), HOSTS[1]) == "rsa-sha2-512,rsa-sha2-256"
    with pytest.raises(ceph_ssh_policy.PolicyRefusal) as refused:
        ceph_ssh_policy.host_key_algorithms(policy(FIPS), known(("192.0.2.2", "ssh-ed25519")), HOSTS[1])
    assert refused.value.detail == "node-2:ssh-ed25519"


def test_ceph_transport_option_set_matches_the_managed_os_guest_session(core_request, tmp_path):
    for name in ("config", "known", "identity"):
        (tmp_path / name).write_text(name)
    files = {"ssh_config": str(tmp_path / "config"), "known_hosts": str(tmp_path / "known"), "identity": str(tmp_path / "identity")}
    guest = managedos_guest_ssh.guest_argv(host_key_algorithms="rsa-sha2-512,rsa-sha2-256", policy_host_key_algorithms=DEFAULT,
                                           user="operator", address="192.0.2.1", remote="true", **files)
    argv = ceph_core.ssh_argv(core_request["hosts"][0], files["identity"], files["known_hosts"], "print('fixed')",
                              ssh_config=files["ssh_config"], host_key_algorithms="rsa-sha2-512,rsa-sha2-256")
    assert options(argv) == options(guest)
    assert argv[:8] == ["/usr/bin/ssh", "-T", "-F", files["ssh_config"], "-i", files["identity"], "-p", "22"]
    assert "ControlMaster=no" in options(argv) and "ControlPath=none" in options(argv)


def test_ceph_transport_keeps_strict_pinned_known_hosts(core_request):
    argv = ceph_core.ssh_argv(core_request["hosts"][0], "/k", "/private/known", "x", ssh_config="/private/config", host_key_algorithms="ssh-ed25519")
    values = options(argv)
    for value in ("StrictHostKeyChecking=yes", "GlobalKnownHostsFile=none", "KnownHostsCommand=none", "UserKnownHostsFile=/private/known",
                  "HostKeyAlgorithms=ssh-ed25519", "UpdateHostKeys=no", "VerifyHostKeyDNS=no"):
        assert values.count(value) == 1
    assert len([value for value in values if value.startswith("StrictHostKeyChecking=")]) == 1
    assert "accept-new" not in " ".join(argv)


def test_ceph_transport_evaluates_the_controller_policy_once_and_never_on_destroy(core_request, host_request, tmp_path, monkeypatch):
    evaluations = []
    monkeypatch.setattr(ceph_ssh_policy, "controller_policy", lambda: evaluations.append(1) or policy(DEFAULT))
    connection = transport(core_request, tmp_path, [("192.0.2.1", "ssh-ed25519"), ("192.0.2.2", "ssh-rsa")])
    connection.policy = None
    assert evaluations == []
    connection.call(core_request["hosts"][0], "observe")
    connection.call(core_request["hosts"][1], "observe")
    assert evaluations == [1]
    (tmp_path / "release").mkdir()
    release = transport(host_request["cluster"], tmp_path / "release", [("192.0.2.2", "ssh-ed25519")], scope=True)
    release.policy = None
    assert ceph_core.execute_host(host_request, "destroy", "d" * 64, release)["evidence"]["removed"] is True
    assert evaluations == [1]


def test_ceph_transport_policy_that_cannot_be_evaluated_refuses_unavailable():
    def unreadable():
        raise ssh_key_policy.PolicyError("unreadable")

    with pytest.raises(ceph_ssh_policy.PolicyRefusal, match="^ssh-policy-unavailable$") as refused:
        ceph_ssh_policy.evaluate_controller_policy(read=unreadable)
    assert refused.value.detail is None
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == "ssh-policy-unavailable"
    evaluated = ceph_ssh_policy.evaluate_controller_policy(read=lambda: "HostKeyAlgorithms " + FIPS + "\n",
                                                           evaluate=lambda data: ssh_key_policy.effective_policy(data, native(FIPS)))
    assert evaluated == {"configuration": "HostKeyAlgorithms " + FIPS + "\n", "hostKeyAlgorithms": FIPS}


@pytest.mark.parametrize("data", [
    b"", b"192.0.2.9 ssh-ed25519 AAAA\n", b"192.0.2.1 ssh-ed25519 AAAA\n192.0.2.1 ssh-rsa AAAA\n", b"192.0.2.1 ssh-ed25519 AAAA extra\n",
    b"192.0.2.1 ssh-dss AAAA\n", b"192.0.2.1 ssh-ed25519\n", "192.0.2.1 ssh-ed25519 é\n".encode(), b"[192.0.2.1]:22 ssh-ed25519 AAAA\n",
    b"node-1 ssh-ed25519 AAAA\n",
])
def test_pinned_key_type_requires_exactly_one_line_for_the_host_token(data):
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_ssh_policy.pinned_key_type(data, HOSTS[0])


def test_pinned_key_type_reads_the_host_token_of_a_non_default_port():
    pins = b"192.0.2.2 ssh-ed25519 AAAA\n\n[192.0.2.1]:2222 ecdsa-sha2-nistp384 AAAA\n"
    assert ceph_ssh_policy.pinned_key_type(pins, dict(HOSTS[0], port=2222)) == "ecdsa-sha2-nistp384"
    assert ceph_ssh_policy.pinned_key_type(pins, HOSTS[1]) == "ssh-ed25519"


def test_censored_output(core_request, tmp_path):
    pins = [("192.0.2.1", "ssh-ed25519"), ("192.0.2.2", "ssh-ed25519")]
    blob = key("ssh-ed25519").split()[1]
    argvs = []
    transport(core_request, tmp_path, pins, run=lambda argv, data, timeout: argvs.append(argv) or (0, OK)).call(core_request["hosts"][0], "observe")
    for value in ("PRIVATE-KEY-SENTINEL", blob, "aes256-gcm", DEFAULT):
        assert value not in repr(argvs)
    with pytest.raises(ceph_ssh_policy.PolicyRefusal) as refused:
        transport(core_request, tmp_path, pins, FIPS).call(core_request["hosts"][0], "observe")
    reason, detail = ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)
    assert reason == "ssh-policy-host-excluded" and not set(detail) - adapter_protocol.DETAIL and len(detail) <= adapter_protocol.MAX_DETAIL
    for text in (detail, str(refused.value), repr(refused.value.args)):
        assert blob not in text and FIPS not in text and "aes256-gcm" not in text
    with pytest.raises(ceph_tunnel.HostKeyRefusal) as tunnel:
        ceph_tunnel.outer_configuration("HostKeyAlgorithms ecdsa-sha2-nistp256\n", HOSTS, known(*pins))
    assert (str(tunnel.value), tunnel.value.host, tunnel.value.key_type) == ("tunnel-host-key-excluded", "node-1", "ssh-ed25519")
    assert blob not in repr(vars(tunnel.value)) + repr(tunnel.value.args)

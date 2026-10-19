"""Native tunnel input, identity, restart and process boundaries fail closed."""

from __future__ import annotations

import base64
import copy
import hashlib
import json
import os
import subprocess

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel


def key():
    algorithm = b"ssh-ed25519"
    blob = len(algorithm).to_bytes(4, "big") + algorithm + (32).to_bytes(4, "big") + b"a" * 32
    return "ssh-ed25519 " + base64.b64encode(blob).decode()


@pytest.fixture
def tunnel_request():
    return {"version": "ceph-core-v13", "clusterSSHTransport": "verified-tunnel", "owner": "apply-example",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "clusterSSHUser": "operator",
            "hosts": [{"name": "node-2", "address": "2001:db8::2", "port": 2222},
                      {"name": "node-1", "address": "192.0.2.1", "port": 22}]}


@pytest.fixture
def material(tunnel_request):
    known = ("[2001:db8::2]:2222 " + key() + "\n192.0.2.1 " + key() + "\n").encode()
    return ceph_tunnel.bundle(tunnel_request, b"private fixture", known, b"fixed fixture source\n", "Ciphers aes256-gcm@openssh.com\n", public=key())


def publish(path, material):
    path.mkdir(mode=0o700)
    for name, content in material.items():
        destination = path / name
        destination.write_bytes(content)
        destination.chmod(0o600)


def manifest(material):
    return json.loads(material["manifest.json"])


def test_bundle_freezes_only_original_endpoints_and_exact_material(material):
    value = manifest(material)
    assert value["hosts"] == [{"name": "node-1", "address": "192.0.2.1", "port": 22},
                              {"name": "node-2", "address": "2001:db8::2", "port": 2222}]
    assert value["nativeUser"] == "operator"
    assert set(material) == ceph_tunnel.FILES | {"manifest.json"}
    assert all(value["files"][name] == hashlib.sha256(material[name]).hexdigest() for name in ceph_tunnel.FILES - {"identity"})
    assert b"private fixture" not in material["manifest.json"]
    assert b"2001:db8::2" not in material["outer_config"]


@pytest.mark.parametrize("field,value", [
    ("version", "ceph-core-v2"), ("clusterSSHTransport", "native"),
    ("owner", "$(touch /tmp/owned)"), ("clusterSSHUser", "-F/etc/ssh/config"),
    ("fsid", "not-a-uuid"),
])
def test_wrong_request_cannot_render_transport(tunnel_request, field, value):
    tunnel_request[field] = value
    with pytest.raises(ceph_tunnel.TunnelRefusal, match="native-tunnel-unproved"):
        ceph_tunnel.targets(tunnel_request)


@pytest.mark.parametrize("address", ["host.example.test", "192.0.2.1;id", "0.0.0.0", "ff02::1", "fe80::1%eth0", "2001:0db8::1", None])
def test_connection_never_resolves_dns_or_uses_alternate_address(tunnel_request, address):
    tunnel_request["hosts"][0]["address"] = address
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.targets(tunnel_request)


@pytest.mark.parametrize("port", [True, 0, 65536, "22"])
def test_port_is_an_exact_numeric_endpoint(tunnel_request, port):
    tunnel_request["hosts"][0]["port"] = port
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.targets(tunnel_request)


def test_duplicate_address_refuses_ambiguous_native_host(tunnel_request):
    tunnel_request["hosts"][0]["address"] = "192.0.2.1"
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.targets(tunnel_request)


@pytest.mark.parametrize("token", ["192.0.2.2", "*", "192.0.2.1,192.0.2.2", "|1|hash|key", "@cert-authority"])
def test_known_hosts_cannot_expand_frozen_scope(tunnel_request, token):
    known = (token + " " + key() + "\n[2001:db8::2]:2222 " + key() + "\n").encode()
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.known_hosts(known, ceph_tunnel.targets(tunnel_request))


@pytest.mark.parametrize("data", [b"", b"# comment\n", b"not ascii\xff", b"192.0.2.1 ssh-ed25519 broken\n"])
def test_incomplete_or_malformed_trust_has_no_best_effort_fallback(tunnel_request, data):
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.known_hosts(data, ceph_tunnel.targets(tunnel_request))


def test_missing_duplicate_and_extra_trust_entries_refuse(tunnel_request):
    known = ("192.0.2.1 " + key() + "\n[2001:db8::2]:2222 " + key() + "\n").encode()
    for data in (known.splitlines()[0], known + known, known + ("192.0.2.3 " + key() + "\n").encode()):
        with pytest.raises(ceph_tunnel.TunnelRefusal):
            ceph_tunnel.known_hosts(data, ceph_tunnel.targets(tunnel_request))


def test_frozen_controller_trust_separator_lines_do_not_expand_scope(tunnel_request):
    hosts = ceph_tunnel.targets(tunnel_request)
    source = ("# fixed trust\n192.0.2.1 " + key() + "\n\n[2001:db8::2]:2222 " + key() + "\n\n").encode()
    assert ceph_tunnel.known_hosts(source, hosts) == ("192.0.2.1 " + key() + "\n[2001:db8::2]:2222 " + key() + "\n").encode()


def test_native_unknown_host_invokes_refusal_instead_of_direct_ssh(material):
    text = material["native_config"].decode()
    assert "Host 192.0.2.1\n  ProxyCommand /usr/bin/python3 -I /run/bootwright-ceph-ssh/proxy.py 0\n" in text
    assert "Host 2001:db8::2\n  ProxyCommand /usr/bin/python3 -I /run/bootwright-ceph-ssh/proxy.py 1\n" in text
    assert "Host *\n  StrictHostKeyChecking yes\n  ForwardAgent no\n" in text
    assert "  IdentityAgent none\n  IdentitiesOnly yes\n  IdentityFile /run/bootwright-ceph-ssh/identity\n" in text
    assert "  PreferredAuthentications publickey\n" in text
    assert "  PasswordAuthentication no\n  KbdInteractiveAuthentication no\n  HostbasedAuthentication no\n" in text
    assert "  GSSAPIAuthentication no\n  GSSAPIKeyExchange no\n  ForwardX11 no\n  ForwardX11Trusted no\n  RequestTTY no\n" in text
    assert text.endswith("  ProxyCommand /usr/bin/python3 -I /run/bootwright-ceph-ssh/proxy.py reject\n")
    assert all(token not in text for token in ("%h", "%p", "%r", "ProxyCommand none", "ProxyJump"))


def test_outer_proxy_is_one_foreground_exact_loopback_forward(material):
    first = ceph_tunnel.proxy_argv(manifest(material), "0")
    second = ceph_tunnel.proxy_argv(manifest(material), "1")
    assert first == ["/usr/bin/ssh", "-T", "-E", "/dev/null", "-F", "/run/bootwright-ceph-ssh/outer_config", "-i", "/run/bootwright-ceph-ssh/identity",
                     "-o", "AddressFamily=inet", "-l", "operator", "-p", "22", "-W", "127.0.0.1:22", "--", "192.0.2.1"]
    assert second[-3:] == ["127.0.0.1:22", "--", "2001:db8::2"]
    assert second[second.index("-p") + 1] == "2222"
    assert "AddressFamily=inet6" in second
    assert all(token not in first for token in ("-f", "-N", "-L", "-R", "-D", "-A"))


@pytest.mark.parametrize("selector", ["reject", "-1", "00", "01", "2", "64", "0;id", "$(id)", None])
def test_unknown_proxy_selector_cannot_connect(material, selector):
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.proxy_argv(manifest(material), selector)


@pytest.mark.parametrize("policy", ["Include /tmp/config", "ProxyCommand id", "Match exec id", "Host *", "Ciphers aes256-gcm@openssh.com;id"])
def test_crypto_policy_cannot_select_connection_or_process(policy):
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.outer_configuration(policy)


def test_authorization_only_allows_loopback_forward_and_native_commands():
    line = ceph_tunnel.authorization_line(key() + " author comment", "apply-example")
    assert line == 'restrict,port-forwarding,permitopen="127.0.0.1:22" ' + key() + " bootwright:apply-example"
    assert "command=" not in line
    assert "author comment" not in line
    assert "pty" not in line
    assert "permitlisten" not in line


def test_guest_policy_denies_remote_and_unix_listeners():
    assert ceph_tunnel.guest_policy("operator") == (b"Match User operator\n  AllowTcpForwarding local\n  PermitListen none\n"
                                                    b"  PermitOpen 127.0.0.1:22\n  AllowStreamLocalForwarding no\n  PermitTunnel no\nMatch all\n")
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.guest_policy("operator\nMatch all")


def test_effective_guest_policy_requires_every_forwarding_boundary():
    output = (b"allowtcpforwarding local\npermitlisten none\npermitopen 127.0.0.1:22\n"
              b"allowstreamlocalforwarding no\npermittunnel no\npasswordauthentication yes\n")
    assert ceph_tunnel.verify_guest_policy(output) == {"loopbackSSHOnly": True}
    for invalid in (output.replace(b" local\n", b" yes\n"), output.replace(b"127.0.0.1:22", b"any"),
                    output.replace(b"allowstreamlocalforwarding no\n", b""), output + b"permitlisten any\n",
                    output.replace(b"permittunnel no", b"permittunnel yes")):
        with pytest.raises(ceph_tunnel.TunnelRefusal):
            ceph_tunnel.verify_guest_policy(invalid)


@pytest.mark.parametrize("public", ["restrict " + key(), key() + "\n" + key(), "ssh-rsa " + key().split()[1], "ssh-ed25519 malformed"])
def test_operator_cannot_add_authorization_options_or_mislabel_key(public):
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.authorization_line(public, "apply-example")


def test_restart_proves_complete_files_and_expected_bytes(tmp_path, material):
    root = tmp_path / "transport"
    publish(root, material)
    assert ceph_tunnel.verify_bundle(str(root), expected=material) == manifest(material)
    expected = copy.deepcopy(material)
    expected["identity"] = b"replacement key"
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.verify_bundle(str(root), expected=expected)


@pytest.mark.parametrize("name", sorted(ceph_tunnel.FILES))
def test_replaced_file_refuses_restart(tmp_path, material, name):
    root = tmp_path / "transport"
    publish(root, material)
    (root / name).write_bytes(b"replacement")
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.verify_bundle(str(root), expected=material)


@pytest.mark.parametrize("change", ["symlink", "hardlink", "group-readable", "world-writable", "directory", "extra-file", "missing-file", "root-permissions"])
def test_unsafe_or_incomplete_material_refuses_without_subprocess(tmp_path, material, change):
    root = tmp_path / "transport"
    publish(root, material)
    destination = root / "identity"
    if change == "symlink":
        destination.unlink()
        destination.symlink_to(tmp_path / "outside")
    elif change == "hardlink":
        os.link(destination, tmp_path / "alias")
    elif change == "group-readable":
        destination.chmod(0o640)
    elif change == "world-writable":
        destination.chmod(0o602)
    elif change == "directory":
        destination.unlink()
        destination.mkdir()
    elif change == "extra-file":
        (root / "identity-cert.pub").write_text("extra certificate")
    elif change == "missing-file":
        destination.unlink()
    else:
        root.chmod(0o755)
    with pytest.raises(ceph_tunnel.TunnelRefusal, match="^native-tunnel-unproved$"):
        ceph_tunnel.verify_bundle(str(root), expected=material)


def test_wrong_file_owner_and_writable_container_mount_refuse(tmp_path, material):
    root = tmp_path / "transport"
    publish(root, material)
    for options in ({"uid": os.geteuid() + 1}, {"readonly": True}):
        with pytest.raises(ceph_tunnel.TunnelRefusal):
            ceph_tunnel.verify_bundle(str(root), **options)


@pytest.mark.parametrize("change", ["duplicate-json-key", "unknown-field", "duplicate-host", "changed-user", "changed-digest"])
def test_manifest_drift_or_malformed_state_refuses(tmp_path, material, change):
    root = tmp_path / "transport"
    publish(root, material)
    value = manifest(material)
    if change == "duplicate-json-key":
        content = material["manifest.json"].replace(b'{', b'{"version":"ceph-tunnel-v1",', 1)
    else:
        if change == "unknown-field":
            value["operatorCommand"] = "id"
        elif change == "duplicate-host":
            value["hosts"].append(value["hosts"][0])
        elif change == "changed-user":
            value["nativeUser"] = "$(id)"
        else:
            value["files"]["identity"] = "a" * 64
        content = ceph_tunnel.canonical(value) + b"\n"
    (root / "manifest.json").write_bytes(content)
    with pytest.raises(ceph_tunnel.TunnelRefusal):
        ceph_tunnel.verify_bundle(str(root), expected=material)


def test_proxy_preserves_stderr_pipe_and_suppresses_diagnostics_at_exec(monkeypatch, material):
    calls = []
    with monkeypatch.context() as patched:
        patched.setattr(ceph_tunnel, "verify_bundle", lambda path, readonly: manifest(material))
        patched.setattr(ceph_tunnel.os, "dup2", lambda *args: pytest.fail("proxy pipe was replaced"))
        patched.setattr(ceph_tunnel.os, "close", lambda *args: pytest.fail("proxy pipe was closed"))
        patched.setattr(ceph_tunnel.os, "chdir", lambda path: calls.append(("cwd", path)))
        patched.setattr(ceph_tunnel.os, "execve", lambda path, argv, environment: calls.append(("exec", path, argv, environment)))
        assert ceph_tunnel.proxy_main(["0"]) == 255
    assert len(calls) == 2 and calls[0] == ("cwd", "/")
    assert calls[1] == ("exec", "/usr/bin/ssh", ceph_tunnel.proxy_argv(manifest(material), "0"),
                        {"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "HOME": "/"})
    assert calls[1][2][2:4] == ["-E", "/dev/null"]


def test_unknown_host_refuses_before_material_or_process_access(monkeypatch):
    monkeypatch.setattr(ceph_tunnel, "verify_bundle", lambda *args, **kwargs: pytest.fail("material was read"))
    monkeypatch.setattr(ceph_tunnel.os, "execve", lambda *args: pytest.fail("process was started"))
    assert ceph_tunnel.proxy_main(["reject"]) == 255
    assert ceph_tunnel.proxy_main([]) == 255
    assert ceph_tunnel.proxy_main(["0", "1"]) == 255


def test_material_failure_refuses_before_any_connection(monkeypatch):
    def refuse(*args, **kwargs):
        raise ceph_tunnel.TunnelRefusal("native-tunnel-unproved")
    monkeypatch.setattr(ceph_tunnel, "verify_bundle", refuse)
    monkeypatch.setattr(ceph_tunnel.os, "execve", lambda *args: pytest.fail("process was started"))
    assert ceph_tunnel.proxy_main(["0"]) == 255


def test_open_ssh_evaluates_isolated_policy_without_connecting(tmp_path, material):
    config = tmp_path / "outer_config"
    config.write_bytes(material["outer_config"])
    result = subprocess.run(["/usr/bin/ssh", "-G", "-F", str(config), "-i", "/run/bootwright-ceph-ssh/identity", "192.0.2.1"],
                            env={"PATH": "/usr/bin:/bin", "LC_ALL": "C"}, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            timeout=10, check=False, text=True)
    assert result.returncode == 0
    values = dict(line.split(None, 1) for line in result.stdout.splitlines() if " " in line)
    for field, expected in {"stricthostkeychecking": "true", "batchmode": "yes", "canonicalizehostname": "false",
                            "controlmaster": "false", "controlpersist": "no", "forwardagent": "no", "forwardx11": "no",
                            "passwordauthentication": "no", "kbdinteractiveauthentication": "no", "identitiesonly": "yes",
                            "permitlocalcommand": "no", "connectionattempts": "1", "connecttimeout": "10",
                            "serveraliveinterval": "15", "serveralivecountmax": "3", "exitonforwardfailure": "yes",
                            "preferredauthentications": "publickey"}.items():
        assert values[field] == expected
    assert values["userknownhostsfile"] == "/run/bootwright-ceph-ssh/known_hosts"
    assert values["globalknownhostsfile"] == "/dev/null"
    assert values["identityfile"] == "/run/bootwright-ceph-ssh/identity"
    assert [line for line in result.stdout.splitlines() if line.startswith("identityfile ")] == ["identityfile /run/bootwright-ceph-ssh/identity"]
    assert values["ciphers"] == "aes256-gcm@openssh.com"
    assert "proxycommand" not in values
    assert "proxyjump" not in values


@pytest.mark.parametrize("address,selector", [("192.0.2.1", "0"), ("2001:db8::2", "1"), ("192.0.2.3", "reject")])
def test_open_ssh_native_host_selection_has_no_direct_fallback(tmp_path, material, address, selector):
    config = tmp_path / "native_config"
    config.write_bytes(material["native_config"])
    result = subprocess.run(["/usr/bin/ssh", "-G", "-F", str(config), address],
                            env={"PATH": "/usr/bin:/bin", "LC_ALL": "C"}, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            timeout=10, check=False, text=True)
    assert result.returncode == 0
    values = dict(line.split(None, 1) for line in result.stdout.splitlines() if " " in line)
    assert values["proxycommand"] == "/usr/bin/python3 -I /run/bootwright-ceph-ssh/proxy.py " + selector


def test_public_manifest_does_not_contain_private_identity_digest(material):
    value = manifest(material)
    assert set(value["files"]) == ceph_tunnel.FILES - {"identity"}
    assert value["identityPublicKey"] == key()
    assert hashlib.sha256(material["identity"]).hexdigest().encode() not in material["manifest.json"]


@pytest.mark.parametrize("provider", ["match", "different", "malformed", "failure"])
def test_provider_parser_proves_private_public_correspondence_without_new_crypto(provider, material, monkeypatch):
    import sys
    from types import SimpleNamespace

    calls = []

    def parse(value):
        assert value == material["identity"]
        calls.append("private-parser")
        if provider == "failure":
            raise ValueError("fixed synthetic parser failure")
        exported = (key() if provider == "match" else key("b") if provider == "different" else "invalid") + "\n"

        def export(format_name):
            assert format_name == "openssh"
            return exported.encode()

        return SimpleNamespace(export_public_key=export)

    monkeypatch.setitem(sys.modules, "asyncssh", SimpleNamespace(import_private_key=parse))
    if provider == "match":
        ceph_tunnel.identity_correspondence(material["identity"], key())
    else:
        with pytest.raises(ceph_tunnel.TunnelRefusal, match="native-tunnel-unproved"):
            ceph_tunnel.identity_correspondence(material["identity"], key())
    assert calls == ["private-parser"]

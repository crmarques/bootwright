"""Inventory SSH uses ephemeral pins and a forced read-only report command."""

import io
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_ssh as client
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_sshd as server
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh


@pytest.fixture
def custody(tmp_path, monkeypatch):
    selected = {"configuration": "HostKeyAlgorithms ecdsa-sha2-nistp256\n", "hostKeyAlgorithms": client.ALGORITHM,
                "fleetKeyAlgorithms": client.ALGORITHM}
    monkeypatch.setattr(client, "policy", lambda: selected)
    monkeypatch.setattr(client.records, "directory", lambda path, create: path.mkdir(mode=0o700) if create else None)
    monkeypatch.setattr(client.records, "read", lambda path, limit=32768: path.read_bytes())
    request = {"network": {"address": "192.0.2.10"}, "ssh": {"port": 2222}}

    def native(argv):
        path = tmp_path / "unused"
        if "-f" in argv:
            path = Path(argv[argv.index("-f") + 1])
        public = (client.ALGORITHM + " " + ("QUFB" if path.name == "host-key" else "QkJC")).encode()
        if "-q" in argv:
            path.write_bytes(b"PRIVATE-" + path.name.encode())
            path.with_name(path.name + ".pub").write_bytes(public + b" \n")
            return b""
        assert "-y" in argv
        return public + b"\n"

    client.prepare(tmp_path, request, native)
    return tmp_path, request, native


def test_keys_are_distinct_pinned_and_replay_does_not_regenerate_them(custody):
    root, request, native = custody
    before = {path.name: path.read_bytes() for path in (root / "ssh").iterdir()}
    assert before["known_hosts"] == b"[192.0.2.10]:2222 ecdsa-sha2-nistp256 QUFB\n"
    assert before["host-key"] != before["client-key"]
    client.prepare(root, request, native)
    assert before == {path.name: path.read_bytes() for path in (root / "ssh").iterdir()}
    assert not list(root.glob(".ssh-stage-*"))


@pytest.mark.parametrize("name", ("known_hosts", "host-key.pub", "client-key.pub", "ssh_config"))
def test_custody_drift_refuses_without_repairing_a_boots_credentials(custody, name):
    root, request, native = custody
    path = root / "ssh" / name
    path.write_bytes(b"FOREIGN")
    with pytest.raises(ValueError):
        client.prepare(root, request, native)
    assert path.read_bytes() == b"FOREIGN"


def test_client_uses_the_bound_port_identity_and_only_inventory(custody, monkeypatch):
    root, request, _native = custody
    run = Mock(return_value=bounded_process.Result(0, b"report", b""))
    monkeypatch.setattr(client.bounded_process, "run", run)
    assert client.pull(root, request) == (b"report", "")
    argv = run.call_args.args[0]
    assert argv[-1] == "inventory" and argv[-2] == "root@192.0.2.10"
    assert argv[argv.index("-p") + 1] == "2222"
    assert "StrictHostKeyChecking=yes" in argv and "IdentityAgent=none" in argv
    assert argv[argv.index("-i") + 1] == str(root / "ssh/client-key")
    assert run.call_args.kwargs["environment"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}


@pytest.mark.parametrize("error,cause", [(b"Connection timed out", "ssh-timeout"), (b"Connection refused", "ssh-refused"),
                                        (b"No route to host", "ssh-unreachable"), (b"PRIVATE unknown error", "ssh-not-ready")])
def test_retry_causes_are_closed_and_contain_no_native_text(custody, monkeypatch, error, cause):
    root, request, _native = custody
    monkeypatch.setattr(client.bounded_process, "run", Mock(return_value=bounded_process.Result(255, b"", error)))
    answer = client.pull(root, request)
    assert answer == (None, cause) and "PRIVATE" not in repr(answer)


@pytest.mark.parametrize("code,output,error,match", [
    (255, b"", b"Host key verification failed. PRIVATE", "foreign service"),
    (255, b"", b"Permission denied PRIVATE", "authentication refused"),
    (42, b"", b"PRIVATE", "inventory refused"), (0, b"", b"", "SSH answer"),
    (0, b"x" * (client.MAX_REPORT + 1), b"", "report size"), (1, b"PRIVATE", b"", "SSH answer"),
])
def test_definite_failures_never_turn_into_a_wait_or_disclose_native_text(custody, monkeypatch, code, output, error, match):
    root, request, _native = custody
    monkeypatch.setattr(client.bounded_process, "run", Mock(return_value=bounded_process.Result(code, output, error)))
    with pytest.raises(ValueError, match=match) as caught:
        client.pull(root, request)
    assert "PRIVATE" not in str(caught.value)


@pytest.mark.parametrize("kind", ("timeout", "output", "command"))
def test_process_bounds_are_enforced(custody, monkeypatch, kind):
    root, request, _native = custody
    monkeypatch.setattr(client.bounded_process, "run", Mock(side_effect=bounded_process.Failure(kind)))
    if kind == "timeout":
        assert client.pull(root, request) == (None, "ssh-timeout")
    else:
        with pytest.raises(ValueError, match="SSH client"):
            client.pull(root, request)


def test_server_configuration_exposes_only_key_authenticated_inventory():
    text = server.configuration("192.0.2.10", 2222).decode()
    for line in ("Port 2222", "ListenAddress 192.0.2.10", "AuthenticationMethods publickey", "PasswordAuthentication no",
                 "UsePAM no", "PermitTTY no", "DisableForwarding yes", "PermitUserRC no", "PermitUserEnvironment no",
                 "ForceCommand /usr/bin/python3 -I /run/discovery/disk_discovery_worker.py report"):
        assert line in text.splitlines()
    assert "0.0.0.0" not in text and "Subsystem " not in text


@pytest.mark.parametrize("port", (True, False, 0, -1, 65536, "22", 22.0))
def test_server_and_client_refuse_invalid_ports(tmp_path, port):
    with pytest.raises(ValueError):
        server.configuration("192.0.2.10", port)
    paths = [tmp_path / name for name in ("config", "known_hosts", "identity")]
    for path in paths:
        path.write_bytes(b"")
    with pytest.raises(managedos_guest_ssh.SessionError):
        managedos_guest_ssh.guest_argv(ssh_config=str(paths[0]), known_hosts=str(paths[1]), identity=str(paths[2]),
                                     host_key_algorithms=client.ALGORITHM, policy_host_key_algorithms=client.ALGORITHM,
                                     user="root", address="192.0.2.10", remote_argv=["inventory"], port=port)


@pytest.mark.parametrize("command", (None, "", "sh", "inventory; reboot", "cat /run/discovery/host-key", "internal-sftp"))
def test_forced_command_rejects_shells_subsystems_and_other_reads(tmp_path, monkeypatch, command):
    monkeypatch.setattr(server, "ROOT", tmp_path)
    monkeypatch.setattr(server.os, "environ", {} if command is None else {"SSH_ORIGINAL_COMMAND": command})
    output = io.BytesIO()
    monkeypatch.setattr(server.sys, "stdout", SimpleNamespace(buffer=output))
    with pytest.raises(SystemExit) as caught:
        server.report()
    assert caught.value.code == 64 and output.getvalue() == b""


def test_forced_inventory_returns_only_the_bounded_report_or_refusal(tmp_path, monkeypatch):
    monkeypatch.setattr(server, "ROOT", tmp_path)
    monkeypatch.setattr(server.os, "environ", {"SSH_ORIGINAL_COMMAND": "inventory"})
    output = io.BytesIO()
    monkeypatch.setattr(server.sys, "stdout", SimpleNamespace(buffer=output))
    (tmp_path / "report.json").write_bytes(b"report fixture")
    server.report()
    assert output.getvalue() == b"report fixture"
    output.seek(0)
    output.truncate()
    (tmp_path / "refused").write_bytes(b"refused")
    with pytest.raises(SystemExit) as caught:
        server.report()
    assert caught.value.code == 42 and output.getvalue() == b""


@pytest.mark.parametrize("name", sorted(client.FILES))
def test_abandoned_outer_stage_with_a_partial_file_is_recoverable(custody, name):
    root, request, native = custody
    stage = root / (".ssh-stage-" + "a" * 32)
    stage.mkdir(mode=0o700)
    client.stage_file(stage / name, b"partial private fixture")
    client.prepare(root, request, native)
    assert not stage.exists()
    assert (root / "ssh/known_hosts").read_bytes().startswith(b"[192.0.2.10]:2222 ")


def test_unpublished_stage_never_creates_an_inner_atomic_stage(tmp_path, monkeypatch):
    monkeypatch.setattr(client.os, "fsync", Mock(side_effect=InterruptedError("fixture cancellation")))
    with pytest.raises(InterruptedError):
        client.stage_file(tmp_path / "known_hosts", b"bounded fixture")
    assert [path.name for path in tmp_path.iterdir()] == ["known_hosts"]
    assert (tmp_path / "known_hosts").stat().st_mode & 0o777 == 0o600

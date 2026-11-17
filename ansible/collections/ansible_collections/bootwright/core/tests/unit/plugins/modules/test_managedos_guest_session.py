"""The guest session module writes one connection's known_hosts and reduced configuration, and removes them."""

from __future__ import annotations

import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh, ssh_key_policy
from ansible_collections.bootwright.core.plugins.modules import managedos_guest_session as module

KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootwrightExampleBlob"
POLICY = {"configuration": "Ciphers aes256-gcm@openssh.com\nHostKeyAlgorithms ssh-ed25519,rsa-sha2-512\n",
          "hostKeyAlgorithms": "ssh-ed25519,rsa-sha2-512,rsa-sha2-256", "fleetKeyAlgorithms": "ssh-ed25519"}


def opened(tmp_path, **values):
    params = {"state": "present", "directory": str(tmp_path), "address": "192.0.2.10", "host_key": KEY + "\n"}
    params.update(values)
    return module.execute(params, policy=lambda: POLICY)


def test_the_files_hold_the_one_key_and_the_policy_only(tmp_path):
    session = opened(tmp_path)
    assert session == {"changed": False, "ssh_config": str(tmp_path / "ssh_config"), "known_hosts": str(tmp_path / "known_hosts"),
                       "host_key_algorithms": "ssh-ed25519", "policy_host_key_algorithms": POLICY["hostKeyAlgorithms"]}
    assert (tmp_path / "known_hosts").read_text() == "192.0.2.10 " + KEY
    assert (tmp_path / "ssh_config").read_text() == POLICY["configuration"]
    for name in managedos_guest_ssh.SESSION_FILES:
        assert stat.S_IMODE(os.lstat(tmp_path / name).st_mode) == 0o600


def test_the_session_is_what_the_builder_accepts(tmp_path):
    session = opened(tmp_path)
    identity = tmp_path / "identity"
    identity.write_bytes(b"")
    names = ("ssh_config", "known_hosts", "host_key_algorithms", "policy_host_key_algorithms")
    argv = managedos_guest_ssh.guest_argv(identity=str(identity), user="fleet", address="192.0.2.10", remote_argv=["/usr/bin/true"],
                                          **{key: session[key] for key in names})
    assert argv[:3] == [managedos_guest_ssh.SSH, "-F", session["ssh_config"]]


def test_an_rsa_pin_keeps_only_its_policy_permitted_signatures(tmp_path):
    assert opened(tmp_path, host_key="ssh-rsa AAAAB3Nza")["host_key_algorithms"] == "rsa-sha2-512,rsa-sha2-256"


def test_closing_removes_both_files_and_tolerates_their_absence(tmp_path):
    opened(tmp_path)
    (tmp_path / "other").write_text("kept")
    for attempt in ("present", "gone"):
        assert attempt and module.execute({"state": "absent", "directory": str(tmp_path)}) == {"changed": False}
        assert sorted(os.listdir(tmp_path)) == ["other"]


def test_a_planted_link_is_never_followed(tmp_path):
    target = tmp_path / "target"
    target.write_text("untouched")
    work = tmp_path / "work"
    work.mkdir()
    (work / "known_hosts").symlink_to(target)
    with pytest.raises(managedos_guest_ssh.SessionError):
        opened(work)
    assert target.read_text() == "untouched"


@pytest.mark.parametrize("values", [
    {"address": "-oProxyCommand=id"}, {"address": "192.0.2.10 ssh-rsa AAAA"}, {"host_key": KEY + "\n192.0.2.11 " + KEY},
    {"host_key": ""}, {"address": None}, {"directory": "relative"},
], ids=["option address", "two fields", "second line", "no key", "no address", "relative directory"])
def test_a_refused_session_writes_nothing(tmp_path, values):
    with pytest.raises(managedos_guest_ssh.SessionError):
        opened(tmp_path, **values)
    assert os.listdir(tmp_path) == []


@pytest.mark.parametrize("key", ["", " \t\n", None])
def test_a_missing_pinned_key_names_the_missing_identity_proof(run_module, monkeypatch, tmp_path, key):
    monkeypatch.setattr(ssh_key_policy, "read_policy", lambda: "")
    monkeypatch.setattr(ssh_key_policy, "effective_policy", lambda data: dict(POLICY))
    arguments = {"state": "present", "directory": str(tmp_path), "address": "192.0.2.10"}
    if key is not None:
        arguments["host_key"] = key
    result = run_module(module, arguments)
    assert result["failed"] is True
    assert result["msg"] == "the pinned SSH host key is empty or missing; restore its identity proof before connecting"
    assert os.listdir(tmp_path) == []


def test_a_directory_that_is_a_link_or_missing_is_refused(tmp_path):
    (tmp_path / "real").mkdir()
    (tmp_path / "link").symlink_to(tmp_path / "real")
    for directory in (tmp_path / "link", tmp_path / "missing"):
        with pytest.raises(managedos_guest_ssh.SessionError):
            opened(tmp_path, directory=str(directory))
    assert os.listdir(tmp_path / "real") == []


@pytest.mark.parametrize("key, reason", [("ssh-dss AAAA", "ssh-policy-unavailable"),
                                         ("ecdsa-sha2-nistp256 AAAA", "ssh-policy-host-excluded")])
def test_a_key_the_policy_refuses_names_its_reason_and_writes_nothing(tmp_path, key, reason):
    with pytest.raises(ssh_key_policy.PolicyError) as refused:
        opened(tmp_path, host_key=key)
    assert refused.value.reason == reason and "AAAA" not in str(refused.value)
    assert os.listdir(tmp_path) == []


def test_a_policy_the_host_cannot_evaluate_refuses_before_any_file(tmp_path):
    def unavailable():
        raise ssh_key_policy.PolicyError("cannot read the placement host SSH crypto policy")

    with pytest.raises(ssh_key_policy.PolicyError):
        module.execute({"state": "present", "directory": str(tmp_path), "address": "192.0.2.10", "host_key": KEY}, policy=unavailable)
    assert os.listdir(tmp_path) == []


def test_main_opens_and_closes_as_a_task_would(run_module, monkeypatch, tmp_path):
    monkeypatch.setattr(ssh_key_policy, "read_policy", lambda: "")
    monkeypatch.setattr(ssh_key_policy, "effective_policy", lambda data: dict(POLICY))
    result = run_module(module, {"state": "present", "directory": str(tmp_path), "address": "192.0.2.10", "host_key": KEY})
    assert not result.get("failed") and result["host_key_algorithms"] == "ssh-ed25519"
    assert (tmp_path / "known_hosts").read_text() == "192.0.2.10 " + KEY
    result = run_module(module, {"state": "absent", "directory": str(tmp_path)})
    assert not result.get("failed") and os.listdir(tmp_path) == []


def test_main_fails_a_refused_key_with_its_reason(run_module, monkeypatch, tmp_path):
    monkeypatch.setattr(ssh_key_policy, "read_policy", lambda: "")
    monkeypatch.setattr(ssh_key_policy, "effective_policy", lambda data: dict(POLICY))
    result = run_module(module, {"state": "present", "directory": str(tmp_path), "address": "192.0.2.10",
                                 "host_key": "ecdsa-sha2-nistp256 AAAA"})
    assert result["failed"] is True and result["reason"] == "ssh-policy-host-excluded"
    result = run_module(module, {"state": "present", "directory": str(tmp_path), "address": "-oX", "host_key": KEY})
    assert result["failed"] is True and result["msg"] == "the guest address is not valid" and "reason" not in result

"""Readiness survives host boot, replays safely and removes only its own files."""

import os
import shlex
import subprocess
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_firewall_ready as ready

NAME = "bootwright-lab-host-firewalld-ready.service"
REQUEST = {"identity": {"context": "lab", "object": "host"}, "networks": [{"managed": True}], "firewallReadyUnit": NAME}


class Host:
    def __init__(self, root):
        self.root = root
        self.state = "inactive"
        self.enabled = True
        self.running = True
        self.fail = ""
        self.calls = []

    def run(self, argv, **kwargs):
        assert kwargs["check_rc"] is False
        self.calls.append(argv)
        if argv[0] == "/usr/bin/timeout":
            return (0 if self.running else 124), "", ""
        action = argv[1]
        if action == self.fail:
            return 1, "", "injected failure"
        if action == "show":
            present = (self.root / NAME).exists()
            return 0, ("LoadState=%s\nActiveState=%s\nFragmentPath=%s\nDropInPaths=\n" % (
                "loaded" if present else "not-found", self.state, str(self.root / NAME) if present else "")), ""
        if action == "is-enabled":
            return (0, "enabled\n", "") if self.enabled else (1, "disabled\n", "")
        if action in ("start", "stop"):
            self.state = "active" if action == "start" else "inactive"
        return 0, "", ""

    def manage(self, state):
        return ready.manage(self.run, REQUEST, state, str(self.root), os.geteuid())


def test_apply_replay_and_owned_cleanup(tmp_path):
    host = Host(tmp_path)
    foreign = tmp_path / "another.service"
    foreign.write_text("unrelated")
    assert host.manage("present")
    evidence = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert evidence == {"name": NAME, "answered": True, "present": True, "owned": True, "enabled": True, "ready": True, "state": "active"}
    assert not host.manage("present")
    assert host.manage("absent")
    assert not host.manage("absent")
    assert foreign.read_text() == "unrelated"
    assert not (tmp_path / NAME).exists()
    assert not (tmp_path / "virtnetworkd.service.wants" / NAME).is_symlink()
    assert not any(call[1] in ("enable", "disable") for call in host.calls)


@pytest.mark.parametrize("collision", ["content", "symlink", "wants", "dropin"])
def test_foreign_resources_refuse_before_mutation(tmp_path, collision):
    host = Host(tmp_path)
    path = tmp_path / NAME
    if collision == "content":
        path.write_text("foreign")
    elif collision == "symlink":
        path.symlink_to(tmp_path / "outside")
    elif collision == "wants":
        directory = tmp_path / "virtnetworkd.service.wants"
        directory.mkdir()
        (directory / NAME).symlink_to(tmp_path / "other.service")
    else:
        (tmp_path / (NAME + ".d")).mkdir()
    for state in ("present", "absent"):
        with pytest.raises(ValueError, match="unknown or foreign"):
            host.manage(state)
    assert all(call[1] == "show" for call in host.calls)


def test_interrupted_start_retains_owned_unit_for_replay(tmp_path):
    host = Host(tmp_path)
    host.fail = "start"
    with pytest.raises(ValueError):
        host.manage("present")
    assert (tmp_path / NAME).read_bytes() == ready.unit_content(NAME)
    host.fail = ""
    assert host.manage("present")
    host.fail = "daemon-reload"
    with pytest.raises(ValueError):
        host.manage("absent")
    host.fail = ""
    assert not host.manage("absent")
    assert host.calls[-1] == [ready.SYSTEMCTL, "daemon-reload"]


def test_disabled_firewall_refuses_without_installing_or_enabling_it(tmp_path):
    host = Host(tmp_path)
    host.enabled = False
    with pytest.raises(ValueError, match="must already be enabled"):
        host.manage("present")
    assert not (tmp_path / NAME).exists()
    assert [call[1] for call in host.calls] == ["show", "is-enabled"]


@pytest.mark.parametrize("ready_after,expected", [(1, 0), (4, 0), (31, 1)])
def test_boot_barrier_waits_for_real_readiness_and_bounds_failure(monkeypatch, ready_after, expected):
    calls, sleeps = [], []

    def probe(argv, **kwargs):
        assert argv == ["/usr/bin/firewall-cmd", "--state"]
        assert kwargs == {"stdout": subprocess.DEVNULL, "stderr": subprocess.DEVNULL}
        calls.append(argv)
        return 0 if len(calls) >= ready_after else 1

    monkeypatch.setattr(subprocess, "call", probe)
    monkeypatch.setattr(time, "sleep", sleeps.append)
    content = ready.unit_content(NAME).decode()
    command = next(line.removeprefix("ExecStart=") for line in content.splitlines() if line.startswith("ExecStart="))
    argv = shlex.split(command)
    assert argv[:2] == ["/usr/bin/python3", "-c"]
    with pytest.raises(SystemExit) as stopped:
        exec(argv[2], {})
    assert stopped.value.code == expected
    assert len(calls) == min(ready_after, 30)
    assert sleeps == [2] * (len(calls) - (expected == 0))
    assert "TimeoutStartSec=75\n" in content
    assert "After=firewalld.service\n" in content
    assert "Before=virtnetworkd.service\n" in content
    assert "WantedBy=virtnetworkd.service\n" in content


def test_external_only_provider_creates_no_barrier(tmp_path):
    request = dict(REQUEST, networks=[{"managed": False}], firewallReadyUnit="")
    calls = []
    assert not ready.manage(calls.append, request, "present", str(tmp_path), os.geteuid())
    assert not calls and not list(tmp_path.iterdir())


@pytest.mark.parametrize("prerequisite", ["enabled", "running"])
def test_settled_observation_detects_firewall_prerequisite_drift(tmp_path, prerequisite):
    host = Host(tmp_path)
    host.manage("present")
    setattr(host, prerequisite, False)
    evidence = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert evidence["answered"] and evidence["owned"] and evidence["state"] == "active"
    assert not evidence["ready"]
    if prerequisite == "running":
        assert host.calls[-1] == ["/usr/bin/timeout", "15", "/usr/bin/firewall-cmd", "--state"]


@pytest.mark.parametrize("state", ["active", "failed", ""])
def test_missing_fragment_does_not_hide_retained_or_unknown_unit_state(tmp_path, state):
    host = Host(tmp_path)
    host.state = state
    evidence = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert evidence["present"] and not evidence["owned"]
    assert evidence["state"] == state
    with pytest.raises(ValueError, match="unknown or foreign"):
        host.manage("absent")
    assert all(call[1] == "show" for call in host.calls)

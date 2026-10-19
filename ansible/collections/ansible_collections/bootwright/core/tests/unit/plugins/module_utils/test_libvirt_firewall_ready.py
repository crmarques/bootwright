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
        self.dropins = ""
        self.effective = {"Type": "oneshot", "RemainAfterExit": "yes", "TimeoutStartUSec": "1min 15s",
                          "Before": "virtnetworkd.service shutdown.target", "After": "sysinit.target firewalld.service"}

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
            if "--value" in argv:
                # systemctl show --value prints the bare property value (systemctl(1)).
                return 0, self.state + "\n", ""
            return 0, ("LoadState=%s\nActiveState=%s\nFragmentPath=%s\nDropInPaths=%s\n" % (
                "loaded" if present else "not-found", self.state, str(self.root / NAME) if present else "", self.dropins)
                + "".join("%s=%s\n" % item for item in self.effective.items())), ""
        if action == "is-enabled":
            return (0, "enabled\n", "") if self.enabled else (1, "disabled\n", "")
        if action == "reset-failed":
            # systemd keeps a failed unit loaded until reset-failed (systemd.unit(5) CollectMode=);
            # a unit that is not loaded fails with "Unit X not loaded." (systemd src/core/dbus-manager.c).
            if self.state != "failed" and not (self.root / NAME).exists():
                return 1, "", "Failed to reset failed state of unit %s: Unit %s not loaded.\n" % (argv[2], argv[2])
            self.state = "inactive" if self.state == "failed" else self.state
            return 0, "", ""
        if action == "start":
            self.state = "active"
        elif action == "stop" and self.state != "failed":
            self.state = "inactive"
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
    assert not any(call[1] == "reset-failed" for call in host.calls)


def failed_barrier(tmp_path):
    host = Host(tmp_path)
    assert host.manage("present")
    host.state = "failed"
    host.calls.clear()
    return host


def test_failed_barrier_removal_proves_absence(tmp_path):
    host = failed_barrier(tmp_path)
    assert host.manage("absent")
    assert [ready.SYSTEMCTL, "reset-failed", NAME] in host.calls
    evidence = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert evidence == {"name": NAME, "answered": True, "present": False, "owned": False, "enabled": False, "ready": False, "state": ""}
    assert not host.manage("absent")


def test_reset_failed_tolerates_not_loaded(tmp_path):
    host = failed_barrier(tmp_path)

    def collected(argv, **kwargs):
        if argv[1] == "reset-failed":
            host.state = "inactive"
        return host.run(argv, **kwargs)

    ready.manage(collected, REQUEST, "absent", str(tmp_path), os.geteuid())
    assert not (tmp_path / NAME).exists()
    assert not ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())["present"]


def test_reset_failed_other_error_refuses(tmp_path):
    host = failed_barrier(tmp_path)
    host.fail = "reset-failed"
    with pytest.raises(ValueError, match="restore firewalld and repeat"):
        host.manage("absent")


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
    assert host.calls[-2:] == [[ready.SYSTEMCTL, "daemon-reload"], [ready.SYSTEMCTL, "show", "--property=ActiveState", "--value", NAME]]


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
    with pytest.raises(ValueError, match="unknown or foreign") as refused:
        host.manage("absent")
    assert ("systemctl reset-failed " + NAME in str(refused.value)) == (state == "failed")
    assert all(call[1] == "show" for call in host.calls)


VENDOR = "/usr/lib/systemd/system/service.d/"


@pytest.fixture
def vendor_tree(tmp_path, monkeypatch):
    directory = tmp_path / "vendor-root"
    directory.mkdir()
    defaults = directory / VENDOR.lstrip("/")
    defaults.mkdir(parents=True)
    check = ready.vendor_dropins
    monkeypatch.setattr(ready, "vendor_dropins", lambda value: check(value, str(directory), os.geteuid()))
    return directory, defaults


@pytest.mark.parametrize("names", [("10-common.conf",), ("10-common.conf", "20-second.conf"), ("10-common defaults.conf",)])
def test_vendor_defaults_allow_completion_replay_and_owned_removal_without_global_writes(tmp_path, vendor_tree, names):
    _root, defaults = vendor_tree
    before = {}
    for name in names:
        path = defaults / name
        path.write_text("[Service]\nNice=1\n")
        before[name] = (path.read_bytes(), path.stat())
    host = Host(tmp_path)
    # systemctl's string array is space-separated, quoting elements as needed.
    host.dropins = " ".join(shlex.quote(VENDOR + name) for name in names)
    assert host.manage("present")
    observation = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert observation["answered"] and observation["owned"] and observation["ready"]
    assert not host.manage("present")
    assert host.manage("absent")
    absent = ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())
    assert absent["answered"] and not absent["present"] and not absent["owned"] and absent["state"] == ""
    assert not host.manage("absent")
    for name in names:
        assert ((defaults / name).read_bytes(), (defaults / name).stat()) == before[name]


@pytest.mark.parametrize("value", [
    None, [], " ", "'unterminated", "[" + VENDOR + "10-common.conf]", VENDOR + "nested/10-common.conf",
    VENDOR + "../10-common.conf", VENDOR + "10-common.conf/", VENDOR + "10-common.txt",
    VENDOR + "10-common.conf\x00", VENDOR + "10-common.conf\nstray",
    "/etc/systemd/system/service.d/10-common.conf", "/run/systemd/system/service.d/10-common.conf",
    "/usr/lib/systemd/system/bootwright-.service.d/10-common.conf",
    "/usr/lib/systemd/system/" + NAME + ".d/10-common.conf",
    VENDOR + "10-common.conf /etc/systemd/system/service.d/20-local.conf",
    VENDOR + "missing.conf", "x" * 16385,
    " ".join([VENDOR + "10-common.conf"] * 65),
])
def test_malformed_specific_or_unproved_dropins_refuse(value, vendor_tree):
    _root, defaults = vendor_tree
    (defaults / "10-common.conf").write_text("[Service]\nNice=1\n")
    assert not ready.vendor_dropins(value)


@pytest.mark.parametrize("unsafe", ["file-writable", "parent-writable", "file-link", "parent-link", "directory", "fifo"])
def test_vendor_provenance_rejects_unsafe_files_and_ancestors(vendor_tree, unsafe):
    root, defaults = vendor_tree
    path = defaults / "10-common.conf"
    path.write_text("[Service]\nNice=1\n")
    if unsafe == "file-writable":
        path.chmod(0o666)
    elif unsafe == "parent-writable":
        (root / "usr/lib").chmod(0o777)
    elif unsafe == "file-link":
        target = defaults / "20-target.conf"
        path.rename(target)
        path.symlink_to(target)
    elif unsafe == "parent-link":
        target = root / "relocated"
        (root / "usr/lib").rename(target)
        (root / "usr/lib").symlink_to(target)
    else:
        path.unlink()
        if unsafe == "directory":
            path.mkdir()
        else:
            os.mkfifo(path)
    assert not ready.vendor_dropins(VENDOR + "10-common.conf")


@pytest.mark.parametrize("foreign", ["file", "ancestor"])
def test_vendor_provenance_requires_trusted_ownership(monkeypatch, vendor_tree, foreign):
    root, defaults = vendor_tree
    path = defaults / "10-common.conf"
    path.write_text("[Service]\nNice=1\n")
    target = path if foreign == "file" else root / "usr/lib"
    inode = target.stat().st_ino
    original = os.fstat

    def foreign_owner(descriptor):
        info = original(descriptor)
        if info.st_ino == inode:
            fields = list(info)
            fields[4] = os.geteuid() + 1
            return os.stat_result(fields)
        return info

    monkeypatch.setattr(os, "fstat", foreign_owner)
    assert not ready.vendor_dropins(VENDOR + "10-common.conf")


@pytest.mark.parametrize("field,value", [
    ("Type", "simple"), ("RemainAfterExit", "no"), ("TimeoutStartUSec", "infinity"),
    ("TimeoutStartUSec", "76s"), ("TimeoutStartUSec", ""), ("Before", "shutdown.target"), ("After", "sysinit.target"),
])
@pytest.mark.parametrize("inherited", [False, True])
def test_effective_barrier_cannot_be_weakened_even_by_trusted_defaults(tmp_path, vendor_tree, field, value, inherited):
    _root, defaults = vendor_tree
    (defaults / "10-common.conf").write_text("[Service]\nNice=1\n")
    host = Host(tmp_path)
    host.dropins = VENDOR + "10-common.conf" if inherited else ""
    host.effective[field] = value
    with pytest.raises(ValueError, match="loaded firewall readiness unit"):
        host.manage("present")
    assert not any(call[1] == "start" for call in host.calls)
    assert (tmp_path / NAME).read_bytes() == ready.unit_content(NAME)
    assert not ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())["owned"]


def test_missing_dropin_property_does_not_prove_no_overrides(tmp_path):
    host = Host(tmp_path)
    host.manage("present")

    def missing(argv, **kwargs):
        code, output, error = host.run(argv, **kwargs)
        return code, output.replace("DropInPaths=\n", ""), error

    evidence = ready.observe(missing, REQUEST, str(tmp_path), os.geteuid())
    assert evidence["answered"] and evidence["present"] and not evidence["owned"]


@pytest.mark.parametrize("override", [
    "/etc/systemd/system/service.d/20-local.conf", "/run/systemd/system/" + NAME + ".d/20-runtime.conf",
    "/usr/lib/systemd/system/" + NAME + ".d/20-specific.conf", "/usr/lib/systemd/system/bootwright-.service.d/20-prefix.conf",
])
def test_mixed_vendor_and_specific_overrides_refuse_apply_and_cleanup(tmp_path, vendor_tree, override):
    _root, defaults = vendor_tree
    path = defaults / "10-common.conf"
    path.write_text("[Service]\nNice=1\n")
    host = Host(tmp_path)
    host.manage("present")
    host.dropins = VENDOR + "10-common.conf " + override
    host.calls.clear()
    for state in ("present", "absent"):
        with pytest.raises(ValueError, match="unknown or foreign"):
            host.manage(state)
    assert all(call[1] == "show" for call in host.calls)
    assert path.read_text() == "[Service]\nNice=1\n"
    assert (tmp_path / NAME).read_bytes() == ready.unit_content(NAME)


def test_equivalent_effective_timeout_is_accepted(tmp_path):
    host = Host(tmp_path)
    host.effective["TimeoutStartUSec"] = "75s"
    assert host.manage("present")
    assert ready.observe(host.run, REQUEST, str(tmp_path), os.geteuid())["owned"]

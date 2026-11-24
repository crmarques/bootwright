"""The provided time worker converges chrony onto declared servers, proves the clock and restores a host that stays unsynchronized."""

from __future__ import annotations

import base64
import errno
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import atomic_publish, bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_time as worker
from ansible_collections.bootwright.core.plugins.module_utils.managedos_provided_time import Refused

DIGEST = "4" * 64
SERVERS = ["192.0.2.123", "ntp.example.test"]
PRIOR = b"# vendor\npool 2.rhel.pool.ntp.org iburst\ndriftfile /var/lib/chrony/drift\nmakestep 1.0 3\nsourcedir /run/chrony-dhcp\nlogdir /var/log/chrony\n"
DESIRED = (b"# vendor\nserver 192.0.2.123 iburst\nserver ntp.example.test iburst\ndriftfile /var/lib/chrony/drift\n"
           b"makestep 1.0 3\nsourcedir /run/chrony-dhcp\nlogdir /var/log/chrony\n")
SOURCES = ("MS Name/IP address         Stratum Poll Reach LastRx Last sample\n"
           "===============================================================================\n"
           "^? 192.0.2.123                   0   6     0     -     +0ns[   +0ns] +/-    0ns\n"
           "^* 192.0.2.124                   2   6   377    11   +123us[ +456us] +/-   20ms\n")


class Clock:
    def __init__(self):
        self.now = 1000.0
        self.slept = []

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.slept.append(seconds)
        self.now += seconds


class Machine:
    """The units, package and clock of a host, answering the commands the worker runs."""

    def __init__(self, tmp_path, conf=PRIOR, installed=True, enabled=False, active=False, others=(), synchronizes=True):
        self.directory = tmp_path / "bootwright"
        self.conf = tmp_path / "chrony.conf"
        self.record = self.directory / "provided-time.json"
        if conf is not None:
            self.conf.write_bytes(conf)
            os.chmod(self.conf, 0o640)
        self.installed, self.enabled, self.active = installed, enabled, active
        self.others = dict(others)
        self.synchronizes = synchronizes
        self.synchronized = False
        self.failing = {}
        self.calls = []
        self.during = {}
        self.clock = Clock()

    def host(self):
        return worker.Host(self.run, str(self.conf), str(self.directory), os.getuid(), self.clock.monotonic, self.clock.sleep)

    def writes(self):
        return [call for call in self.calls if call[0] in ("enable", "disable", "restart", "stop", "install")]

    def run(self, argv, timeout):
        assert timeout > 0 and os.path.isabs(argv[0])
        program, arguments = os.path.basename(argv[0]), tuple(argv[1:])
        key = " ".join(arguments)
        if key in self.during:
            self.during.pop(key)(self)
        if program == "systemctl":
            verb, unit = arguments
            if unit == "chronyd":
                return self.chronyd(verb, key)
            state = self.others.get(unit, (False, False))
            if verb == "is-enabled":
                return (0, "enabled") if state[0] else (1, "")
            return (0, "active") if state[1] else (3, "inactive")
        if program == "rpm":
            return (0, "chrony-4.5") if self.installed else (1, "package chrony is not installed")
        if program == "dnf":
            self.calls.append(("install",))
            if "dnf" in self.failing:
                return self.failing["dnf"]
            self.installed = True
            if not self.conf.exists():
                self.conf.write_bytes(PRIOR)
            return 0, ""
        if program == "timedatectl":
            return 0, "yes" if self.synchronized else "no"
        if program == "chronyc":
            return self.failing.get("chronyc", (0, SOURCES))
        raise AssertionError(argv)

    def chronyd(self, verb, key):
        if verb == "is-enabled":
            return (0, "enabled") if self.enabled else (1, "disabled")
        if verb == "is-active":
            return (0, "active") if self.active else (3, "inactive")
        self.calls.append((verb,))
        if key in self.failing:
            return self.failing[key]
        if verb in ("enable", "disable"):
            self.enabled = verb == "enable"
        else:
            self.active = verb == "restart"
            self.synchronized = self.active and self.synchronizes and DESIRED_LINES <= set(self.conf.read_bytes().splitlines())
        return 0, ""


DESIRED_LINES = {b"server 192.0.2.123 iburst", b"server ntp.example.test iburst"}


def crash(machine):
    raise KeyboardInterrupt(machine.conf)


def stored(machine):
    return json.loads(machine.record.read_bytes())


@pytest.fixture(autouse=True)
def root(monkeypatch):
    def unlabeled(descriptor, name):
        raise OSError(errno.ENODATA, "no label")

    monkeypatch.setattr(worker.os, "geteuid", lambda: 0)
    monkeypatch.setattr(worker.os, "getxattr", unlabeled)


def test_the_source_lines_are_replaced_in_place_and_every_other_directive_stays():
    assert worker.rewritten(PRIOR, SERVERS) == DESIRED
    assert worker.rewritten(DESIRED, SERVERS) == DESIRED
    assert worker.rewritten(b"driftfile /x\n#server hidden\nconfdir /etc/chrony.d\ninclude /etc/a.conf\n", ["192.0.2.1"]) == (
        b"server 192.0.2.1 iburst\ndriftfile /x\n#server hidden\nconfdir /etc/chrony.d\ninclude /etc/a.conf\n")
    assert worker.rewritten(b"a\n  peer x\nb\nserver y minpoll 4\nc\npool z\n", ["192.0.2.1"]) == b"a\nserver 192.0.2.1 iburst\nb\nc\n"
    assert worker.rewritten(b"makestep 1 3\nserver old", ["192.0.2.1"]) == b"makestep 1 3\nserver 192.0.2.1 iburst\n"
    assert worker.rewritten(b"", ["192.0.2.1"]) == b"server 192.0.2.1 iburst\n"
    assert worker.rewritten(b"Server old\nPOOL other iburst\nrtcsync\n", ["2001:db8::1"]) == b"server 2001:db8::1 iburst\nrtcsync\n"


def test_apply_writes_the_servers_records_the_prior_state_first_and_proves_the_clock(tmp_path):
    machine = Machine(tmp_path, enabled=False, active=False)
    seen = []

    def at_enable(host):
        seen.append(stored(host))
        assert host.conf.read_bytes() == DESIRED
        assert oct(host.record.stat().st_mode & 0o777) == "0o600"

    machine.during["enable chronyd"] = at_enable
    result = worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert result == {"changed": True, "state": "complete", "postcondition": True, "servers": SERVERS}
    assert machine.conf.read_bytes() == DESIRED and oct(machine.conf.stat().st_mode & 0o777) == "0o640"
    assert machine.writes() == [("enable",), ("restart",)]
    assert not machine.record.exists()
    assert seen[0]["request"] == DIGEST and base64.b64decode(seen[0]["file"]) == PRIOR
    assert (seen[0]["enabled"], seen[0]["active"], seen[0]["mode"], seen[0]["label"]) == (False, False, 0o640, None)


def test_an_already_desired_host_changes_nothing_and_still_waits_for_the_clock(tmp_path):
    machine = Machine(tmp_path, conf=DESIRED, enabled=True, active=True)
    machine.synchronized = True
    result = worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert result["changed"] is False and result["postcondition"] is True
    assert machine.writes() == [] and not machine.directory.exists()
    waiting = Machine(tmp_path, conf=DESIRED, enabled=True, active=True)
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, waiting.host())
    assert refused.value.reason == "time-unproved" and waiting.writes() == [] and not waiting.directory.exists()
    assert waiting.conf.read_bytes() == DESIRED and sum(waiting.clock.slept) >= 175
    assert refused.value.detail == "192.0.2.123@sU0_192.0.2.124@sS377"


@pytest.mark.parametrize("unit,state", [
    ("systemd-timesyncd.service", (True, False)), ("ntpd.service", (False, True)), ("ntpsec.service", (True, True)),
    ("openntpd.service", (True, False)), ("timemaster.service", (False, True)),
])
def test_another_time_service_refuses_before_any_change(tmp_path, unit, state):
    machine = Machine(tmp_path, installed=False, others={unit: state})
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert (refused.value.reason, refused.value.detail) == ("time-service-conflict", unit)
    assert machine.writes() == [] and machine.conf.read_bytes() == PRIOR and not machine.directory.exists()


def test_a_missing_package_is_installed_and_the_installed_file_is_the_one_rewritten(tmp_path):
    machine = Machine(tmp_path, conf=None, installed=False)
    result = worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert result["changed"] is True and machine.installed
    assert machine.calls[0] == ("install",) and machine.conf.read_bytes() == DESIRED


@pytest.mark.parametrize("answer", [(1, "no match"), None])
def test_a_failed_package_install_refuses_without_touching_the_file(tmp_path, answer):
    machine = Machine(tmp_path, installed=False)
    machine.failing["dnf"] = answer
    if answer is None:
        machine.run = lambda argv, timeout, inner=machine.run: None if argv[0].endswith("dnf") else inner(argv, timeout)
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert refused.value.reason == "time-package" and machine.conf.read_bytes() == PRIOR and not machine.directory.exists()


@pytest.mark.parametrize("kind", ["link", "directory", "oversize"])
def test_a_configuration_that_is_not_a_small_regular_file_refuses_before_any_change(tmp_path, kind):
    machine = Machine(tmp_path, conf=None, installed=False)
    if kind == "link":
        (tmp_path / "elsewhere").write_bytes(PRIOR)
        os.symlink(tmp_path / "elsewhere", machine.conf)
    elif kind == "directory":
        machine.conf.mkdir()
    else:
        machine.conf.write_bytes(b"#" * (worker.MAX_CONF + 1))
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert refused.value.reason == "time-config" and machine.calls == [] and not machine.directory.exists()


def test_a_host_that_never_synchronizes_is_restored_from_its_record(tmp_path):
    machine = Machine(tmp_path, enabled=False, active=False, synchronizes=False)
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert (refused.value.reason, refused.value.detail) == ("time-unproved", "192.0.2.123@sU0_192.0.2.124@sS377")
    assert machine.conf.read_bytes() == PRIOR and oct(machine.conf.stat().st_mode & 0o777) == "0o640"
    assert (machine.enabled, machine.active) == (False, False) and not machine.record.exists()
    assert machine.writes() == [("enable",), ("restart",), ("disable",), ("stop",)]
    assert 175 <= sum(machine.clock.slept) <= 180 and set(machine.clock.slept) == {5}


def test_a_running_chronyd_is_restarted_on_the_prior_configuration(tmp_path):
    machine = Machine(tmp_path, enabled=True, active=True, synchronizes=False)
    with pytest.raises(Refused):
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert machine.writes() == [("enable",), ("restart",), ("enable",), ("restart",)]
    assert (machine.enabled, machine.active) == (True, True) and machine.conf.read_bytes() == PRIOR


def test_an_absent_prior_file_is_removed_again_on_restore(tmp_path):
    machine = Machine(tmp_path, conf=None, installed=True, synchronizes=False)
    with pytest.raises(Refused):
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert not machine.conf.exists() and not machine.record.exists()


def test_a_failed_restore_keeps_the_record_and_refuses_as_unavailable(tmp_path):
    machine = Machine(tmp_path, synchronizes=False)
    machine.failing["disable chronyd"] = (1, "")
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert (refused.value.reason, refused.value.detail) == ("time-unavailable", "restore")
    assert base64.b64decode(stored(machine)["file"]) == PRIOR


@pytest.mark.parametrize("verb", ["enable chronyd", "restart chronyd"])
def test_a_failed_command_after_the_record_keeps_it(tmp_path, verb):
    machine = Machine(tmp_path)
    machine.failing[verb] = (1, "")
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert refused.value.reason == "time-unavailable" and machine.record.exists()


def test_a_record_left_by_a_crash_is_the_rollback_target_and_is_never_recorded_again(tmp_path):
    machine = Machine(tmp_path, synchronizes=False)
    machine.during["restart chronyd"] = crash
    with pytest.raises(KeyboardInterrupt):
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert machine.conf.read_bytes() == DESIRED and base64.b64decode(stored(machine)["file"]) == PRIOR
    before = machine.record.read_bytes()
    retried = machine.host()
    other = "5" * 64
    with pytest.raises(Refused) as refused:
        worker.execute("apply", other, SERVERS, retried)
    assert refused.value.reason == "time-unproved"
    assert machine.conf.read_bytes() == PRIOR and not machine.record.exists()
    assert (machine.enabled, machine.active) == (False, False)
    assert before.count(DIGEST.encode()) == 1 and other.encode() not in before


def test_a_record_left_by_a_crash_is_settled_when_the_clock_synchronizes(tmp_path):
    machine = Machine(tmp_path)
    machine.during["restart chronyd"] = crash
    with pytest.raises(KeyboardInterrupt):
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    machine.during.clear()
    machine.enabled = machine.active = True
    machine.synchronized = True
    machine.calls.clear()
    result = worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert result["changed"] is True and not machine.record.exists() and machine.conf.read_bytes() == DESIRED
    assert machine.writes() == [("enable",), ("restart",)]


@pytest.mark.parametrize("damage", ["mode", "json", "keys", "file"])
def test_an_unreadable_record_refuses_before_any_change(tmp_path, damage):
    machine = Machine(tmp_path)
    machine.directory.mkdir()
    record = {"request": DIGEST, "file": None, "mode": 420, "uid": 0, "gid": 0, "label": None, "enabled": False, "active": False}
    if damage == "keys":
        del record["label"]
    if damage == "file":
        record["file"] = "not base64!"
    machine.record.write_text("{" if damage == "json" else json.dumps(record))
    os.chmod(machine.record, 0o644 if damage == "mode" else 0o600)
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert (refused.value.reason, refused.value.detail) == ("time-unavailable", "record")
    assert machine.writes() == [] and machine.conf.read_bytes() == PRIOR


def test_the_prior_owner_and_selinux_label_are_recorded_and_kept(tmp_path, monkeypatch):
    machine = Machine(tmp_path)
    label = b"system_u:object_r:etc_t:s0\0"
    stages = []
    monkeypatch.setattr(worker.os, "getxattr", lambda descriptor, name: label)
    monkeypatch.setattr(atomic_publish.os, "setxattr", lambda descriptor, name, value: stages.append((name, value)))
    worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert stages == [("security.selinux", label)]
    (tmp_path / "second").mkdir()
    machine = Machine(tmp_path / "second")
    machine.during["enable chronyd"] = lambda host: stages.append(stored(host)["label"])
    worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert stages[-1] == "system_u:object_r:etc_t:s0"


def test_observation_never_writes_and_reads_each_state(tmp_path):
    machine = Machine(tmp_path)
    assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "absent"
    machine = Machine(tmp_path, conf=DESIRED, enabled=True, active=True)
    machine.synchronized = True
    assert worker.execute("observe", DIGEST, SERVERS, machine.host()) == {"changed": False, "state": "complete", "postcondition": True, "servers": SERVERS}
    machine.synchronized = False
    assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "partial"
    machine.synchronized, machine.active = True, False
    assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "partial"
    assert machine.writes() == [] and not machine.directory.exists() and machine.conf.read_bytes() == DESIRED


def test_an_outstanding_or_unreadable_record_is_a_partial_effect_whatever_the_file_holds(tmp_path):
    for conf in (PRIOR, DESIRED):
        machine = Machine(tmp_path, conf=conf, enabled=True, active=True)
        machine.synchronized = True
        machine.directory.mkdir(exist_ok=True)
        machine.record.write_text("{")
        os.chmod(machine.record, 0o600)
        assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "partial"
        assert machine.record.read_text() == "{" and machine.writes() == []


def test_an_observed_link_or_missing_file_reads_no_effect(tmp_path):
    machine = Machine(tmp_path, conf=None)
    assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "absent"
    os.symlink(tmp_path / "elsewhere", machine.conf)
    assert worker.execute("observe", DIGEST, SERVERS, machine.host())["state"] == "absent"


def test_sources_are_summarized_within_the_closed_detail_alphabet():
    assert worker.sources_detail(SOURCES) == "192.0.2.123@sU0_192.0.2.124@sS377"
    assert worker.sources_detail("MS Name/IP address\n=====\n") == "none"
    many = "".join("^+ 2001:db8::%x   2   6   377    11   +1us[ +1us] +/- 1ms\n" % number for number in range(40))
    detail = worker.sources_detail(many)
    assert 0 < len(detail) <= worker.MAX_DETAIL and detail.endswith("377") and not set(detail) - set(
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.:@-")


@pytest.mark.parametrize("servers", [[], ["bad server"], ["-x"], ["a", "a"], "a", ["a\nb"], [""], list("abcdefghij") * 30])
def test_invalid_server_lists_refuse(tmp_path, servers):
    with pytest.raises(Refused) as refused:
        worker.execute("apply", DIGEST, servers, Machine(tmp_path).host())
    assert (refused.value.reason, refused.value.detail) == ("time-unavailable", "input")


def test_authority_and_the_digest_are_checked(tmp_path, monkeypatch):
    machine = Machine(tmp_path)
    for operation, digest in (("remove", DIGEST), ("apply", "short")):
        with pytest.raises(Refused):
            worker.execute(operation, digest, SERVERS, machine.host())
    monkeypatch.setattr(worker.os, "geteuid", lambda: 1000)
    with pytest.raises(Refused):
        worker.execute("apply", DIGEST, SERVERS, machine.host())
    assert machine.calls == []


def test_the_default_runner_is_bounded_and_maps_a_failure_to_none(monkeypatch):
    seen = []

    def run(argv, **keywords):
        seen.append((argv, keywords))
        return bounded_process.Result(0, b"yes\n", b"")

    monkeypatch.setattr(worker.bounded_process, "run", run)
    assert worker.Host.bounded(["/usr/bin/timedatectl"], 7) == (0, "yes")
    assert seen[0][1]["timeout"] == 7 and seen[0][1]["environment"]["LC_ALL"] == "C"

    def fail(argv, **keywords):
        raise bounded_process.Failure("timeout")

    monkeypatch.setattr(worker.bounded_process, "run", fail)
    assert worker.Host.bounded(["/usr/bin/timedatectl"], 7) is None

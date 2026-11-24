"""SSH admission covers both IP families without opening other listeners."""

from __future__ import annotations

import ipaddress
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_firewall_worker as worker, managedos_guest_ssh
from ansible_collections.bootwright.core.plugins.modules import managedos_firewall as guest_module


def test_policy_emits_one_input_chain_for_all_sshd_ports():
    sources = ["192.0.2.10/32", "2001:db8:42::/64"]
    assert worker.valid_ssh_sources(sources)
    script, structures = worker.ssh_rules(sources, [22, 2222], "test-mark")
    assert b'iifname "lo" return comment "test-mark"' in script
    assert b"ip saddr 192.0.2.10 return" in script
    assert b"ip6 saddr 2001:db8:42::/64 return" in script
    assert [entry["rule"]["expr"][-1] for entry in structures[2:]] == [
        {"return": None}, {"return": None}, {"return": None}, {"return": None}, {"drop": None}, {"drop": None},
    ]
    assert b"tcp dport 22 drop\n" in script and b"tcp dport 2222 drop\n" in script


@pytest.mark.parametrize("sources", [[], ["192.0.2.1"], ["192.0.2.1/24"], ["::ffff:c000:201/128"], ["fe80::1%eth0/128"], [1]])
def test_guest_refuses_noncanonical_sources(sources):
    assert not worker.valid_ssh_sources(sources)


def test_remote_application_checks_the_actual_peer_before_any_firewall_effect(monkeypatch):
    monkeypatch.setattr(worker, "ssh_peer", lambda: (ipaddress.ip_address("192.0.2.55"), 22))
    monkeypatch.setattr(worker, "ssh_ports", lambda _guest: [22])
    payload = {"operation": "apply", "mark": "test-mark", "services": [], "sshSources": ["192.0.2.10/32"]}
    with pytest.raises(worker.Refused) as err:
        worker.converge(object(), payload)
    assert err.value.cause == "ssh-admission"


MARK = "test-mark"
SOURCES = ["192.0.2.10/32"]


class FilterGuest:
    """The SSH filter's native surface: its persistent files, its unit's enablement, the kernel chain and the withdrawal timer."""

    def __init__(self, loaded=None):
        self.files = {}
        self.enabled = False
        self.runtime = None
        self.timer = False
        self.started = False
        self.loaded = loaded
        self.calls = []

    def load(self, data):
        if b"PRIOR:" in data:
            self.runtime = json.loads(data.split(b"PRIOR:", 1)[1])
        elif data.startswith(b"add table"):
            self.runtime = None
        else:
            self.runtime = structures(data)

    def run(self, argv):
        self.calls.append(list(argv))
        if argv[:5] == [worker.NFT, "-j", "-s", "-nn", "list"]:
            if argv[5:] == ["tables"]:
                return 0, json.dumps({"nftables": []}).encode()
            return (1, b"") if self.runtime is None else (0, json.dumps({"nftables": self.runtime}).encode())
        if argv == [worker.NFT, "list", "table", "inet", worker.SSH_TABLE]:
            return 0, b"PRIOR:" + json.dumps(self.runtime).encode()
        if argv[:2] == [worker.NFT, "-c"]:
            return 0, b""
        if argv[:2] == [worker.NFT, "-f"]:
            if argv[2] in self.files:
                self.load(self.files[argv[2]])
            else:
                with open(argv[2], "rb") as candidate:
                    data = candidate.read()
                if self.loaded is not None and not data.startswith(b"add table inet " + worker.SSH_TABLE.encode() + b"\ndelete"):
                    self.runtime = self.loaded
                else:
                    self.load(data)
            return 0, b""
        if argv[:2] == [worker.SYSTEMCTL, "is-enabled"]:
            return (0, b"enabled\n") if self.enabled else (1, b"disabled\n")
        if argv[:2] == [worker.SYSTEMCTL, "is-active"]:
            return (0, b"active\n") if self.timer else (3, b"inactive\n")
        if argv == [worker.SYSTEMCTL, "show", "--property=ExecMainStartTimestampMonotonic", worker.WITHDRAW_SERVICE]:
            return 0, b"ExecMainStartTimestampMonotonic=" + (b"42" if self.started else b"0") + b"\n"
        if argv[:2] == [worker.SYSTEMCTL, "show"]:
            return 0, ("FragmentPath=" + worker.SSH_UNIT_PATH + "\nDropInPaths=\n").encode()
        if argv[:2] == [worker.SYSTEMCTL, "enable"]:
            self.enabled = True
        if argv[:2] == [worker.SYSTEMCTL, "disable"]:
            self.enabled = False
        if argv[:2] == [worker.SYSTEMCTL, "reload-or-restart"]:
            self.runtime = self.loaded if self.loaded is not None else structures(self.files[worker.SSH_CONFIG])
        if argv[:1] == [worker.SYSTEMD_RUN]:
            self.timer = True
        if argv[:2] == [worker.SYSTEMCTL, "stop"] and worker.WITHDRAW_TIMER in argv:
            self.timer = False
        return 0, b""


def structures(config):
    return worker.ssh_rules(SOURCES, [22], MARK)[1] if config == worker.ssh_rules(SOURCES, [22], MARK)[0] else None


@pytest.fixture(name="guest")
def filter_guest(monkeypatch, tmp_path):
    guest = FilterGuest()
    monkeypatch.setattr(worker, "SSH_CANDIDATES", str(tmp_path))
    monkeypatch.setattr(worker, "ssh_ports", lambda _guest: [22])
    monkeypatch.setattr(worker, "ssh_file", lambda path, _mark: guest.files.get(path))

    def publish(path, data):
        if data is None:
            guest.files.pop(path, None)
        else:
            guest.files[path] = data
    monkeypatch.setattr(worker, "publish_ssh_file", publish)
    return guest


def payload(operation="apply", **values):
    return dict({"operation": operation, "mark": MARK, "services": [], "sshSources": SOURCES}, **values)


EARLIER = worker.ssh_rules(["198.51.100.0/24"], [22], MARK)


def earlier_filter(guest):
    guest.files = {worker.SSH_CONFIG: EARLIER[0], worker.SSH_UNIT_PATH: worker.ssh_unit(MARK)}
    guest.enabled = True
    guest.runtime = EARLIER[1]


def test_a_changed_sshd_port_set_reads_drifted_on_inspection(guest, monkeypatch):
    ready = worker.ssh_rules(SOURCES, [22], MARK)
    guest.files = {worker.SSH_CONFIG: ready[0], worker.SSH_UNIT_PATH: worker.ssh_unit(MARK)}
    guest.enabled = True
    guest.runtime = ready[1]
    assert worker.ssh_policy(guest, payload("inspect")) == ("ready", False)
    monkeypatch.setattr(worker, "ssh_ports", lambda _guest: [22, 2222])
    assert worker.ssh_policy(guest, payload("inspect")) == ("drifted", False)
    assert guest.files == {worker.SSH_CONFIG: ready[0], worker.SSH_UNIT_PATH: worker.ssh_unit(MARK)} and guest.runtime == ready[1]


def test_a_published_filter_whose_chain_does_not_verify_is_withdrawn_before_the_refusal(guest):
    guest.loaded = worker.ssh_rules(["203.0.113.0/24"], [22], MARK)[1]
    with pytest.raises(worker.Refused) as err:
        worker.ssh_policy(guest, payload())
    assert err.value.cause == "ssh-admission"
    assert guest.files == {} and guest.runtime is None and guest.enabled is False


def test_a_withdrawal_restores_the_filter_and_the_chain_an_earlier_apply_published(guest):
    earlier_filter(guest)
    guest.loaded = worker.ssh_rules(["203.0.113.0/24"], [22], MARK)[1]
    with pytest.raises(worker.Refused):
        worker.ssh_policy(guest, payload())
    assert guest.files == {worker.SSH_CONFIG: EARLIER[0], worker.SSH_UNIT_PATH: worker.ssh_unit(MARK)} and guest.enabled is True
    assert guest.runtime == EARLIER[1] and guest.calls[-1] == worker.ssh_restore_argv()


def test_a_withdrawal_that_cannot_put_the_earlier_file_back_leaves_the_unit_disabled_and_loads_only_the_staged_chain(guest, monkeypatch):
    earlier_filter(guest)
    guest.loaded = worker.ssh_rules(["203.0.113.0/24"], [22], MARK)[1]
    publish = worker.publish_ssh_file

    def refusing(path, data):
        if data == EARLIER[0]:
            raise worker.failed("write")
        publish(path, data)
    monkeypatch.setattr(worker, "publish_ssh_file", refusing)
    with pytest.raises(worker.Refused):
        worker.ssh_policy(guest, payload())
    assert guest.enabled is False and guest.runtime == EARLIER[1]
    assert [worker.NFT, "-f", worker.SSH_CONFIG] not in guest.calls


def test_an_installed_apply_loads_only_the_chain_under_an_armed_withdrawal(guest):
    state, changed = worker.ssh_policy(guest, payload(confirm=True))
    assert (state, changed) == ("pending", True)
    assert guest.files == {} and guest.enabled is False and guest.timer is True
    armed = next(index for index, argv in enumerate(guest.calls) if argv[:1] == [worker.SYSTEMD_RUN])
    loaded = next(index for index, argv in enumerate(guest.calls) if argv[:2] == [worker.NFT, "-f"])
    assert armed < loaded and "--collect" in guest.calls[armed] and guest.calls[armed][-3:] == worker.ssh_restore_argv()
    state, changed = worker.ssh_policy(guest, payload("confirm"))
    assert (state, changed) == ("ready", True) and guest.timer is False and guest.enabled is True
    assert set(guest.files) == {worker.SSH_CONFIG, worker.SSH_UNIT_PATH}


def test_a_confirmation_after_the_withdrawal_fired_publishes_nothing(guest):
    worker.ssh_policy(guest, payload(confirm=True))
    guest.runtime = None
    with pytest.raises(worker.Refused) as err:
        worker.ssh_policy(guest, payload("confirm"))
    assert err.value.cause == "ssh-admission" and guest.files == {} and guest.enabled is False


def test_a_confirmation_whose_withdrawal_already_started_refuses_and_restores(guest):
    earlier_filter(guest)
    worker.ssh_policy(guest, payload(confirm=True))
    guest.started = True
    with pytest.raises(worker.Refused):
        worker.ssh_policy(guest, payload("confirm"))
    assert guest.files[worker.SSH_CONFIG] == EARLIER[0] and guest.runtime == EARLIER[1]


def test_a_confirmation_refused_after_it_stopped_the_withdrawal_withdraws_the_candidate(guest, monkeypatch):
    earlier_filter(guest)
    worker.ssh_policy(guest, payload(confirm=True))
    assert guest.runtime == structures(worker.ssh_rules(SOURCES, [22], MARK)[0])

    def unproved(_guest):
        raise worker.failed("ssh-admission")
    monkeypatch.setattr(worker, "ssh_ports", unproved)
    with pytest.raises(worker.Refused):
        worker.ssh_policy(guest, payload("confirm"))
    assert guest.timer is False and guest.runtime == EARLIER[1] and guest.files[worker.SSH_CONFIG] == EARLIER[0]


def test_an_arm_left_by_an_unconfirmed_apply_puts_its_earlier_chain_back_before_staging_again(guest):
    earlier_filter(guest)
    worker.ssh_policy(guest, payload(confirm=True))
    assert guest.timer is True
    worker.ssh_policy(guest, payload(confirm=True))
    restore = guest.calls.index(worker.ssh_restore_argv())
    listing = max(index for index, argv in enumerate(guest.calls) if argv == [worker.NFT, "list", "table", "inet", worker.SSH_TABLE])
    assert restore < listing
    with open(worker.ssh_restore_path(), "rb") as staged:
        assert json.loads(staged.read().split(b"PRIOR:", 1)[1]) == EARLIER[1]


@pytest.mark.parametrize("restores", [True, False])
def test_an_arm_whose_chain_does_not_prove_keeps_the_timer_unless_its_restore_succeeded(guest, restores):
    guest.loaded = worker.ssh_rules(["203.0.113.0/24"], [22], MARK)[1]
    run = guest.run
    guest.run = lambda argv: (1, b"") if argv == worker.ssh_restore_argv() and not restores else run(argv)
    with pytest.raises(worker.Refused):
        worker.ssh_policy(guest, payload(confirm=True))
    assert guest.timer is not restores


def test_a_confirmation_session_checks_its_own_peer(monkeypatch):
    monkeypatch.setattr(worker, "ssh_peer", lambda: (ipaddress.ip_address("192.0.2.55"), 22))
    monkeypatch.setattr(worker, "ssh_ports", lambda _guest: [22])
    with pytest.raises(worker.Refused) as err:
        worker.converge(object(), payload("confirm"))
    assert err.value.cause == "ssh-admission"


@pytest.mark.parametrize("value", [dict(payload(), confirm=False), dict(payload("inspect"), confirm=True),
                                   {"operation": "confirm", "mark": MARK, "services": []}])
def test_the_worker_refuses_a_confirmation_outside_an_ssh_source_policy(value):
    with pytest.raises(worker.Refused):
        worker.parse_request(json.dumps(value).encode())


def session_params(tmp_path):
    paths = {}
    for name, data in (("ssh_config", b"Ciphers aes256-gcm@openssh.com\n"), ("known_hosts", b"192.0.2.10 ssh-ed25519 AAAA\n"), ("identity", b"key\n")):
        (tmp_path / name).write_bytes(data)
        paths[name] = str(tmp_path / name)
    return dict(paths, operation="apply", address="192.0.2.10", user="fleet", host_key_algorithms="ecdsa-sha2-nistp256",
                policy_host_key_algorithms="ecdsa-sha2-nistp256", mark=MARK, services=[], ssh_sources=SOURCES, timeout=300)


def answered(state, changed=True):
    return managedos_guest_ssh.Outcome(0, (json.dumps({"ok": True, "result": {"changed": changed, "services": [], "sshPolicy": state}}) + "\n").encode(),
                                       False, False)


LOST = managedos_guest_ssh.Outcome(None, b"", False, True)
WAIT = worker.WITHDRAW_SECONDS + worker.WITHDRAW_MARGIN


def scripted(answers, clock):
    sent = []

    def run(_argv, data, timeout):
        sent.append((json.loads(data), timeout))
        clock[0] += 2
        return answers.pop(0)
    return run, sent


@pytest.mark.parametrize("answers,operations", [([answered("pending"), answered("drifted")], ["apply", "confirm"]),
                                                ([answered("pending"), LOST, answered("drifted")], ["apply", "confirm", "inspect"]),
                                                ([answered("pending"), LOST, LOST], ["apply", "confirm", "inspect"])])
def test_an_installed_apply_no_fresh_session_confirms_reports_only_after_the_withdrawal(tmp_path, answers, operations):
    waited, clock = [], [100.0]
    run, sent = scripted(answers, clock)
    outcome = guest_module.execute(session_params(tmp_path), run=run, now=lambda: clock[0], sleep=waited.append)
    assert outcome == {"changed": False, "ok": False, "reason": "host-firewall-failed", "cause": "ssh-admission"}
    assert [value["operation"] for value, _timeout in sent] == operations and sent[0][0]["confirm"] is True
    assert sent[1][1] <= worker.WITHDRAW_SECONDS
    assert waited == [WAIT - 4]


def test_a_lost_confirmation_answer_is_ready_only_when_a_later_inspection_proves_it(tmp_path):
    waited, clock = [], [100.0]
    run, sent = scripted([answered("pending"), LOST, answered("ready", False)], clock)
    outcome = guest_module.execute(session_params(tmp_path), run=run, now=lambda: clock[0], sleep=waited.append)
    assert outcome["ok"] is True and outcome["evidence"]["sshPolicy"] == "ready" and waited == [WAIT - 4]
    assert [value["operation"] for value, _timeout in sent] == ["apply", "confirm", "inspect"]


def test_a_lost_arming_answer_waits_out_the_withdrawal_from_before_the_call(tmp_path):
    waited, clock = [], [100.0]
    run, sent = scripted([LOST], clock)
    outcome = guest_module.execute(session_params(tmp_path), run=run, now=lambda: clock[0], sleep=waited.append)
    assert outcome["ok"] is False and outcome["reason"] == "transport" and len(sent) == 1 and waited == [WAIT - 2]


def test_an_installed_apply_a_fresh_session_confirms_is_ready(tmp_path):
    answers = [answered("pending"), answered("ready")]
    outcome = guest_module.execute(session_params(tmp_path), run=lambda *_: answers.pop(0), now=lambda: 0.0, sleep=pytest.fail)
    assert outcome["ok"] is True and outcome["changed"] is True and outcome["evidence"]["sshPolicy"] == "ready"


def test_an_unchanged_installed_filter_is_verified_again_without_a_withdrawal(tmp_path):
    sent = []

    def run(_argv, data, _timeout):
        sent.append(json.loads(data)["operation"])
        return answered("ready", False)
    outcome = guest_module.execute(session_params(tmp_path), run=run, now=lambda: 0.0, sleep=pytest.fail)
    assert outcome["ok"] is True and sent == ["apply", "inspect"]


@pytest.mark.parametrize("confirm", [True, False])
def test_a_staged_restore_that_would_not_reload_refuses_before_anything_is_loaded(guest, confirm):
    earlier_filter(guest)
    run = guest.run
    guest.run = lambda argv: (1, b"") if argv == [worker.NFT, "-c", "-f", worker.ssh_restore_path()] else run(argv)
    with pytest.raises(worker.Refused) as err:
        worker.ssh_policy(guest, payload(confirm=True) if confirm else payload())
    assert err.value.cause == "ssh-admission" and guest.timer is False and guest.runtime == EARLIER[1]
    assert guest.files == {worker.SSH_CONFIG: EARLIER[0], worker.SSH_UNIT_PATH: worker.ssh_unit(MARK)}
    assert not [argv for argv in guest.calls if argv[:1] == [worker.SYSTEMD_RUN] or argv[:2] == [worker.SYSTEMCTL, "enable"]]

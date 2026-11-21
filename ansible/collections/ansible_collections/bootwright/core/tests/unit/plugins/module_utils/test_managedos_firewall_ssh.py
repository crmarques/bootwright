"""SSH admission covers both IP families without opening other listeners."""

from __future__ import annotations

import ipaddress

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_firewall_worker as worker


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

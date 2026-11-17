"""The managedos_firewall and managedos_host_firewall modules' main() run with their workers stubbed, as a task would run them.

The shared worker and the guest SSH session are tested in
test_managedos_host_firewall.py. These cases hold each module's part: the
arguments it reads, what reaches the worker, how its outcome ends the task, and
the refusal of a request that names no printable mark.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import managedos_firewall as guest_module
from ansible_collections.bootwright.core.plugins.modules import managedos_host_firewall as host_module

MARK = "owner context=lab block=os-firewall-controller apply=op-0123"
SERVICE = {"name": "dns", "networks": ["192.0.2.5/32"], "content": '<?xml version="1.0"?><service/>'}
GUEST = {"operation": "inspect", "address": "192.0.2.10", "user": "operator", "identity": "/run/lent/key", "ssh_config": "/run/lent/ssh_config",
         "known_hosts": "/run/lent/known_hosts", "host_key_algorithms": "ssh-ed25519", "policy_host_key_algorithms": "ssh-ed25519",
         "mark": MARK, "services": [SERVICE]}
HOST = {"operation": "apply", "mark": MARK, "services": [{"name": "dns"}]}


@pytest.fixture(name="seen")
def stubbed_workers(monkeypatch):
    """Each module's execute, recording its parameters and answering from `outcome`."""
    state = {"calls": [], "outcome": {"changed": True, "ok": True, "ready": True, "evidence": {"services": []}}}

    def answering(name):
        def execute(params, *unused):
            state["calls"].append((name, dict(params)))
            return state["outcome"]
        return execute

    monkeypatch.setattr(guest_module, "execute", answering("guest"))
    monkeypatch.setattr(host_module, "execute", answering("host"))
    return state


def test_the_guest_module_reads_its_session_arguments_and_returns_the_workers_outcome(seen, run_module):
    seen["outcome"] = {"changed": False, "ok": True, "evidence": {"services": [{"name": "dns"}]}}
    result = run_module(guest_module, GUEST)
    assert result["changed"] is False and result["ok"] is True and result["evidence"] == {"services": [{"name": "dns"}]}
    (_name, params), = seen["calls"]
    assert params["timeout"] == 300 and params["address"] == "192.0.2.10" and params["identity"] == "/run/lent/key"
    assert params["services"] == [dict(SERVICE, addresses=None)]


def test_the_guest_module_refuses_an_argument_it_cannot_carry_to_the_worker_and_names_the_option(run_module):
    result = run_module(guest_module, dict(GUEST, mark="owner <context>"))
    assert result["failed"] is True and result["msg"].startswith("mark is not printable ASCII")
    repeated = run_module(guest_module, dict(GUEST, services=[SERVICE, SERVICE]))
    assert repeated["failed"] is True and repeated["msg"] == "a service name is repeated"
    assert "owner <context>" not in str(result)


def test_the_host_module_returns_the_workers_outcome_with_the_ssh_admission_defaults(seen, run_module):
    result = run_module(host_module, HOST)
    assert result["changed"] is True and result["ok"] is True and result["ready"] is True
    (_name, params), = seen["calls"]
    assert params["ssh_address"] == "" and params["ssh_port"] == 0 and params["operation"] == "apply"


def test_the_host_module_refuses_a_request_with_an_unprintable_mark_before_firewalld_is_touched(run_module):
    result = run_module(host_module, dict(HOST, mark="owner\nmark"))
    assert result["failed"] is True and result["msg"] == "invalid host firewall request"

"""Each libvirt substrate module runs its host commands through the one bounded runner.

The helpers' own behaviour is tested over fake runners in the module_utils
tests. These cases run each module as a task would, with the helper replaced
by one host command, so what reaches bounded_process.run is the module's own
runner: the fixed bound and exactly the helper's environment, never Ansible's
run_command with the inherited one, and a command that outruns its bound fails
the task with the module's existing message.
"""

from __future__ import annotations

import json

import pytest
from ansible.module_utils.basic import AnsibleModule
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, substrate_libvirt
from ansible_collections.bootwright.core.plugins.modules import (
    guest_agent_read,
    libvirt_domain_inspect,
    libvirt_firewall_ready,
    libvirt_host_inspect,
    libvirt_nm_connections,
    libvirt_root_reset,
    libvirt_routing,
    libvirt_vlan_refusal,
)

ARGV = [substrate_libvirt.VIRSH, "--connect", "qemu:///system", "version"]

CASES = [
    (libvirt_domain_inspect, "observe_machine", {"request": {}}, "the machine could not be observed: TimeoutError"),
    (libvirt_host_inspect, "observe_host", {"request": {}}, "the provider host could not be observed: TimeoutError"),
    (libvirt_root_reset, "observe", {"request": {}, "removal": "op-" + "0" * 32, "step": "cluster-install-edge"},
     "the root disk could not be reset: TimeoutError"),
    (libvirt_firewall_ready, "manage", {"request": {}}, "the owned firewall readiness barrier could not be changed safely"),
    (libvirt_routing, "manage", {"request": {}}, "the owned routing table could not be changed safely"),
    (libvirt_vlan_refusal, "manage", {"request": {}}, "the owned VLAN refusal routes could not be changed safely"),
    (libvirt_nm_connections, "manage", {"request": {}}, "the owned trunks and VLAN legs could not be changed safely"),
]
IDS = [case[0].__name__.rsplit(".", 1)[-1] for case in CASES]


def one_command(runner, *_args):
    code, out, _err = runner(ARGV, check_rc=False, environ_update=substrate_libvirt.ENVIRONMENT)
    return {"code": code, "out": out} if code == 0 else False


@pytest.fixture(name="bounded")
def recorded_runs(monkeypatch):
    """bounded_process.run, recording each call and answering it from answer, with run_command forbidden."""
    calls, answer = [], {"result": bounded_process.Result(0, b"10.0.0\n", b""), "failure": None}

    def run(argv, data=b"", timeout=60, limit=bounded_process.MAX_OUTPUT, **options):
        calls.append({"argv": argv, "data": data, "timeout": timeout, "limit": limit, "options": options})
        if answer["failure"] is not None:
            raise bounded_process.Failure(answer["failure"])
        return answer["result"]

    monkeypatch.setattr(bounded_process, "run", run)
    monkeypatch.setattr(AnsibleModule, "run_command", lambda *args, **kwargs: pytest.fail("the module used run_command"))
    return calls, answer


def result_of(module, arguments, capsys):
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


@pytest.mark.parametrize("module, helper, arguments, unused", CASES, ids=IDS)
def test_a_host_command_runs_bounded_with_exactly_its_environment(monkeypatch, capsys, bounded, module, helper, arguments, unused):
    calls, unused_answer = bounded
    monkeypatch.setattr(module, helper, one_command)
    result = result_of(module, arguments, capsys)
    assert result.get("failed") is not True
    assert calls == [{"argv": ARGV, "data": b"", "timeout": substrate_libvirt.COMMAND_SECONDS, "limit": 4 * substrate_libvirt.MAX_OUTPUT,
                      "options": {"environment": substrate_libvirt.ENVIRONMENT}}]


@pytest.mark.parametrize("module, helper, arguments, message", CASES, ids=IDS)
def test_a_host_command_past_its_bound_fails_with_the_modules_message(monkeypatch, capsys, bounded, module, helper, arguments, message):
    unused_calls, answer = bounded
    answer["failure"] = "timeout"
    monkeypatch.setattr(module, helper, one_command)
    result = result_of(module, arguments, capsys)
    assert result["failed"] is True and result["msg"] == message


def test_the_guest_agent_read_is_bounded_and_an_unfinished_one_is_unanswered(monkeypatch, capsys, bounded):
    calls, answer = bounded
    arguments = {"uri": "qemu:///system", "domain": "guest", "path": "/etc/bootwright/host-key.pub"}
    answer["result"] = bounded_process.Result(0, b'{"return": 7}', b"")
    monkeypatch.setattr(guest_agent_read, "read_file", lambda runner, uri, domain, path, limit: str(guest_agent_read.agent(runner, uri, domain, {})))
    read = result_of(guest_agent_read, arguments, capsys)
    assert (read["answered"], read["content"], read["reason"]) == (True, "7", "")
    assert [call["timeout"] for call in calls] == [substrate_libvirt.COMMAND_SECONDS]
    assert calls[0]["options"] == {"environment": substrate_libvirt.ENVIRONMENT}
    answer["failure"] = "timeout"
    result = result_of(guest_agent_read, arguments, capsys)
    assert (result["answered"], result["reason"]) == (False, "the guest agent answer could not be read")

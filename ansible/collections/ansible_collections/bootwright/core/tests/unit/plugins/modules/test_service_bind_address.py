"""Both service modules preserve the distinction between absent and unproved addresses."""

from __future__ import annotations

import json
import socket

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.modules import artifact_server_inspect, infra_service_inspect


@pytest.fixture(params=[artifact_server_inspect, infra_service_inspect])
def inspect(request, monkeypatch):
    module = request.param
    monkeypatch.setattr(module, "observe", lambda *_arguments: {"unit": "active"})
    return module


def run(module, capsys, requested=True):
    capsys.readouterr()
    with patch_module_args({"request": {"bindAddress": "192.0.2.1"}, "bind_address": requested}), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


@pytest.mark.parametrize("assigned", [True, False])
def test_an_address_absence_is_a_successful_read_only_observation(inspect, monkeypatch, capsys, assigned):
    checked = []
    monkeypatch.setattr(inspect, "bind_address_assigned", lambda address: checked.append(address) or assigned)
    result = run(inspect, capsys)
    assert result == {"changed": False, "observation": {"unit": "active"}, "unproved": [], "bindAddressAssigned": assigned}
    assert checked == ["192.0.2.1"]


@pytest.mark.parametrize("error", [OSError, ValueError, socket.timeout])
def test_an_unproved_address_query_fails_without_claiming_absence(inspect, monkeypatch, capsys, error):
    def unproved(_address):
        raise error("private host detail")
    monkeypatch.setattr(inspect, "bind_address_assigned", unproved)
    result = run(inspect, capsys)
    assert result["failed"] is True
    assert result["msg"] == "the host's assigned addresses could not be proved: %s" % error.__name__
    assert "bindAddressAssigned" not in result and "private host detail" not in result["msg"]


def test_an_observation_without_the_option_queries_no_host_addresses(inspect, monkeypatch, capsys):
    monkeypatch.setattr(inspect, "bind_address_assigned", lambda _address: pytest.fail("unsolicited address query"))
    assert "bindAddressAssigned" not in run(inspect, capsys, requested=False)

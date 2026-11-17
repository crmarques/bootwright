"""The service inspection preserves, for both kinds it reads, the distinction between absent and unproved addresses."""

from __future__ import annotations

import json
import socket

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import artifact_server
from ansible_collections.bootwright.core.plugins.modules import infra_service_inspect

# Each kind the inspection reads, by its request's version.
VERSIONS = ("artifact-server-nginx-v1", "managed-service-v1")


@pytest.fixture(params=VERSIONS)
def inspect(request, monkeypatch):
    module = infra_service_inspect
    monkeypatch.setattr(artifact_server, "observe", lambda *_arguments: {"unit": "active"})
    monkeypatch.setattr(module, "observe", lambda *_arguments: {"unit": "active"})
    monkeypatch.setattr(module, "tested_version", request.param, raising=False)
    return module


def run(module, capsys, requested=True):
    capsys.readouterr()
    arguments = {"request": {"bindAddress": "192.0.2.1", "version": module.tested_version}, "bind_address": requested}
    with patch_module_args(arguments), pytest.raises(SystemExit):
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

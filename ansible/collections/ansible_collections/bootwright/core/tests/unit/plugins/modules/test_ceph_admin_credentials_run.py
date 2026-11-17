"""The ceph_admin_credentials module's main() runs over a stubbed host transport, as a task would run it."""

from __future__ import annotations

import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core
from ansible_collections.bootwright.core.plugins.modules import ceph_admin_credentials as module

REQUEST = {"bootstrap": "ceph-0", "hosts": [{"name": "ceph-1"}, {"name": "ceph-0"}]}
FAILURE = "The owned dashboard administrator credential could not be proved or updated"


@pytest.fixture(name="transport")
def stubbed_transport(monkeypatch):
    """The controller's SSH transport, answering from `reply` and recording each call it was asked for."""
    state = {"reply": {}, "calls": [], "built": []}

    class Transport:
        def __init__(self, request, material):
            state["built"].append((request, material))

        def call(self, host, operation, payload, timeout):
            state["calls"].append((host["name"], operation, payload, timeout))
            if isinstance(state["reply"], Exception):
                raise state["reply"]  # pylint: disable=raising-bad-type
            return state["reply"]

    monkeypatch.setattr(ceph_core, "Transport", Transport)
    return state


def private(path, content):
    path.write_text(content, encoding="utf-8")
    os.chmod(path, 0o600)
    return str(path)


def test_prepare_asks_the_bootstrap_host_for_a_candidate_and_keeps_it_privately(tmp_path, transport, run_module):
    transport["reply"] = {"private": '{"password": "candidate-secret"}'}
    output = tmp_path / "candidate.json"
    arguments = {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1", "sshKey": "/run/lent/key"}, "output": str(output)}
    result = run_module(module, arguments)
    assert result["changed"] is False and result["proved"] is True and "candidate-secret" not in json.dumps(result)
    assert transport["calls"] == [("ceph-0", "admin_prepare", {"incarnation": "inc-1"}, 120)]
    assert transport["built"][0][1] == {"sshKey": "/run/lent/key"}
    assert output.read_text(encoding="utf-8") == '{"password": "candidate-secret"}'
    assert stat.S_IMODE(os.stat(output).st_mode) == 0o600


def test_update_applies_the_retained_candidate_and_needs_the_host_to_prove_it(tmp_path, transport, run_module):
    retained = private(tmp_path / "update.json", '{"guard": {}, "password": "retained"}')
    arguments = {"operation": "update", "request": REQUEST, "material": {"incarnation": "inc-1", "update": retained}}
    transport["reply"] = {"proved": True}
    applied = run_module(module, arguments)
    assert applied["changed"] is True and applied["proved"] is True
    assert transport["calls"] == [("ceph-0", "admin_update", {"update": {"guard": {}, "password": "retained"}, "incarnation": "inc-1"}, 120)]
    transport["reply"] = {"proved": False}
    unproved = run_module(module, arguments)
    assert unproved["failed"] is True and unproved["msg"] == FAILURE


def test_an_oversized_candidate_a_host_failure_and_a_request_without_its_bootstrap_host_fail_with_one_message(tmp_path, transport, run_module):
    output = tmp_path / "never.json"
    prepare = {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1"}, "output": str(output)}
    transport["reply"] = {"private": "x" * 16385}
    assert run_module(module, prepare)["msg"] == FAILURE
    transport["reply"] = OSError("connection refused")
    assert run_module(module, prepare)["msg"] == FAILURE
    unhosted = run_module(module, dict(prepare, request={"bootstrap": "ceph-9", "hosts": [{"name": "ceph-0"}]}))
    assert unhosted["failed"] is True and unhosted["msg"] == FAILURE
    assert not output.exists()

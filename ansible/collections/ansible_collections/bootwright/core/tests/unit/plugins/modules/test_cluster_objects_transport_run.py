"""The cluster_objects_read and cluster_objects_write modules' main() run over a stubbed native transport, as a task would run them."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
from ansible_collections.bootwright.core.plugins.modules import cluster_objects_read as reader
from ansible_collections.bootwright.core.plugins.modules import cluster_objects_write as writer

REFUSED = "cluster object transport refused its request"


@pytest.fixture(name="transport")
def stubbed_transport(monkeypatch):
    """oc's native transport answering each action from a table and recording the request it was handed."""
    seen = []

    def answering(name, answer):
        def call(request):
            seen.append((name, request))
            if isinstance(answer, Exception):
                raise answer
            return answer
        return call

    monkeypatch.setattr(native, "get", answering("get", {"object": {"kind": "ConfigMap"}}))
    monkeypatch.setattr(native, "create", answering("create", {"accepted": True, "name": "demo"}))
    monkeypatch.setattr(native, "approve", answering("approve", {"accepted": False}))
    monkeypatch.setattr(secrets, "place", answering("place", {"accepted": True}))
    return seen


def test_the_read_module_returns_what_the_transport_read_without_a_change(transport, run_module):
    request = {"action": "get", "kind": "ConfigMap"}
    result = run_module(reader, {"request": request})
    assert result["changed"] is False and result["object"] == {"kind": "ConfigMap"}
    assert transport == [("get", request)]


def test_the_read_module_refuses_a_request_the_transport_refuses_and_reveals_nothing_else(monkeypatch, transport, run_module):
    def refuse(request):
        raise ValueError("secret detail " + request["kind"])

    monkeypatch.setattr(native, "get", refuse)
    result = run_module(reader, {"request": {"action": "get", "kind": "Secret"}})
    assert result["failed"] is True and result["msg"] == REFUSED
    assert "secret detail" not in str(result)


@pytest.mark.parametrize("action, changed", [("create", True), ("approve", False), ("place", True)])
def test_the_write_module_dispatches_each_action_and_reports_whether_the_transport_accepted(transport, run_module, action, changed):
    request = {"action": action}
    result = run_module(writer, {"request": request})
    assert result["changed"] is changed and result["accepted"] is changed
    assert transport == [(action, request)]


def test_the_write_module_refuses_an_unknown_action_before_any_transport_runs(transport, run_module):
    for request in ({"action": "delete"}, {}):
        result = run_module(writer, {"request": request})
        assert result["failed"] is True and result["msg"] == REFUSED
    assert transport == []

"""The ceph_admin_credentials module's main() runs over a stubbed host transport, as a task would run it."""

from __future__ import annotations

import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_transport
from ansible_collections.bootwright.core.plugins.modules import ceph_admin_credentials as module

REQUEST = {"bootstrap": "ceph-0", "hosts": [{"name": "ceph-1"}, {"name": "ceph-0"}]}
FAILURE = "The owned administrator login could not be proved or updated"


@pytest.fixture(name="transport")
def stubbed_transport(monkeypatch):
    """The controller's SSH transport, answering from `reply` and recording each call it was asked for."""
    state = {"reply": {}, "calls": [], "built": [], "unit": {}}

    class Transport:
        def __init__(self, request, material):
            state["built"].append((request, material))

        @staticmethod
        def unit_material(phase):
            return dict(state["unit"].get(phase, {}))

        def call(self, host, operation, payload, timeout):
            state["calls"].append((host["name"], operation, payload, timeout))
            if isinstance(state["reply"], Exception):
                raise state["reply"]  # pylint: disable=raising-bad-type
            return state["reply"]

    monkeypatch.setattr(ceph_transport, "Transport", Transport)
    return state


def private(path, content):
    path.write_text(content, encoding="utf-8")
    os.chmod(path, 0o600)
    return str(path)


def test_prepare_asks_the_bootstrap_host_for_a_candidate_and_keeps_it_privately(tmp_path, transport, run_module):
    transport["reply"] = {"private": '{"password": "candidate-secret"}'}
    output = tmp_path / "candidate.json"
    arguments = {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "dashboard", "sshKey": "/run/lent/key"},
                 "output": str(output), "login": "dashboard"}
    result = run_module(module, arguments)
    assert result["changed"] is False and result["proved"] is True and "candidate-secret" not in json.dumps(result)
    assert transport["calls"] == [("ceph-0", "admin_prepare", {"incarnation": "inc-1"}, 120)]
    assert transport["built"][0][1] == {"sshKey": "/run/lent/key"}
    assert output.read_text(encoding="utf-8") == '{"password": "candidate-secret"}'
    assert stat.S_IMODE(os.stat(output).st_mode) == 0o600


def test_update_applies_the_retained_candidate_and_needs_the_host_to_prove_it(tmp_path, transport, run_module):
    retained = private(tmp_path / "update.json", '{"guard": {}, "password": "retained"}')
    arguments = {"operation": "update", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "dashboard", "update": retained},
                 "login": "dashboard"}
    transport["reply"] = {"proved": True}
    applied = run_module(module, arguments)
    assert applied["changed"] is True and applied["proved"] is True
    assert transport["calls"] == [("ceph-0", "admin_update", {"update": {"guard": {}, "password": "retained"}, "incarnation": "inc-1"}, 120)]
    transport["reply"] = {"proved": False}
    unproved = run_module(module, arguments)
    assert unproved["failed"] is True and unproved["msg"] == FAILURE


def test_an_oversized_candidate_a_host_failure_and_a_request_without_its_bootstrap_host_fail_with_one_message(tmp_path, transport, run_module):
    output = tmp_path / "never.json"
    prepare = {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "dashboard"}, "output": str(output),
               "login": "dashboard"}
    transport["reply"] = {"private": "x" * 16385}
    assert run_module(module, prepare)["msg"] == FAILURE
    transport["reply"] = OSError("connection refused")
    assert run_module(module, prepare)["msg"] == FAILURE
    unhosted = run_module(module, dict(prepare, request={"bootstrap": "ceph-9", "hosts": [{"name": "ceph-0"}]}))
    assert unhosted["failed"] is True and unhosted["msg"] == FAILURE
    assert not output.exists()


def test_a_monitoring_login_sends_its_login_and_the_phase_material(tmp_path, transport, run_module):
    transport["unit"] = {"login_prepare": {"grafanaAdminPassword": "bound-v1"}, "login_update": {"grafanaAdminPassword": "bound-v1"}}
    output = tmp_path / "guard.json"
    material = {"incarnation": "inc-1", "login": "grafana", "grafanaAdminPassword": "/run/lent/grafana"}
    transport["reply"] = {"private": '{"guard": {"login": "grafana"}}'}
    prepared = run_module(module, {"operation": "prepare", "request": REQUEST, "material": dict(material), "output": str(output), "login": "grafana"})
    assert prepared["proved"] is True and output.read_text(encoding="utf-8") == '{"guard": {"login": "grafana"}}'
    assert transport["built"][0][1] == {"grafanaAdminPassword": "/run/lent/grafana"}
    retained = private(tmp_path / "update.json", '{"guard": {}, "password": "candidate-v2", "user": "admin"}')
    transport["reply"] = {"proved": True}
    updated = run_module(module, {"operation": "update", "request": REQUEST, "material": dict(material, update=retained), "login": "grafana"})
    assert updated["changed"] is True and "candidate-v2" not in json.dumps(updated)
    assert transport["calls"] == [
        ("ceph-0", "login_prepare", {"grafanaAdminPassword": "bound-v1", "incarnation": "inc-1", "login": "grafana"}, 120),
        ("ceph-0", "login_update", {"grafanaAdminPassword": "bound-v1", "incarnation": "inc-1", "login": "grafana",
                                    "update": {"guard": {}, "password": "candidate-v2", "user": "admin"}}, 270)]
    transport["calls"].clear()
    gateway = run_module(module, {"operation": "update", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "prometheus", "update": retained},
                                  "login": "prometheus"})
    assert gateway["changed"] is True and [call[1:4:2] for call in transport["calls"]] == [("login_update", 330)]


def test_a_login_that_differs_from_the_lent_value_fails_before_any_call(tmp_path, transport, run_module):
    result = run_module(module, {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "dashboard"},
                                 "output": str(tmp_path / "never.json"), "login": "prometheus"})
    assert result["failed"] is True and result["msg"] == FAILURE
    assert transport["calls"] == [] and transport["built"] == []


PREPARE = {"operation": "prepare", "request": REQUEST, "material": {"incarnation": "inc-1", "login": "dashboard", "sshKey": "/run/lent/key"},
           "login": "dashboard"}


@pytest.mark.parametrize("login", module.LOGINS)
@pytest.mark.parametrize("reason", ceph_core.ADMIN_REASONS)
def test_a_named_native_refusal_is_published_as_its_reason_and_nothing_else(tmp_path, transport, run_module, reason, login):
    for error in (ceph_native.RemoteRefusal("native-refusal:" + reason + "@ceph_monitoring_logins_native.login_update:282"), ceph_native.Refusal(reason)):
        transport["reply"] = error
        arguments = dict(PREPARE, login=login, material=dict(PREPARE["material"], login=login), output=str(tmp_path / "never.json"))
        result = run_module(module, arguments)
        assert result["failed"] is True and result["msg"] == FAILURE and result["reason"] == reason
        assert set(result) - {"invocation"} == {"failed", "msg", "reason"}, result
    assert not (tmp_path / "never.json").exists()


@pytest.mark.parametrize("error", [
    RuntimeError("surprise"), OSError("connection refused"), ceph_native.Refusal("transport"), ceph_native.Refusal("foreign-pool"),
    ceph_native.RemoteRefusal("native-refusal:image-pull@ceph_host_runtime.pull_image:410"), ceph_native.RemoteRefusal("unknown-reason"),
])
def test_any_other_failure_keeps_the_generic_failure_without_a_reason(tmp_path, transport, run_module, error):
    transport["reply"] = error
    result = run_module(module, dict(PREPARE, output=str(tmp_path / "never.json")))
    assert result["failed"] is True and result["msg"] == FAILURE and "reason" not in result


def test_a_published_refusal_carries_no_value(tmp_path, transport, run_module):
    secret = "SECRET-LOGIN-SENTINEL"
    transport["reply"] = ceph_native.NativeFailure("monitoring-login-drift", "ceph orch ps", "holds " + secret, secrets=(secret,))
    result = run_module(module, dict(PREPARE, output=str(tmp_path / "never.json")))
    assert result["reason"] == "monitoring-login-drift" and secret not in json.dumps(result)

"""The Ceph executor hands each action to its one procedure, with the same arguments and in the same order of calls."""

from __future__ import annotations

import json
import pathlib

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.tests.unit import test_ceph_request_goldens as goldens

GOLDENS = {"cluster": "cluster", "cluster-stretch": "cluster-stretch", "pool": "pool", "policy": "policy", "client": "client", "rbd": "rbd",
           "filesystem": "filesystem", "subvolume": "subvolume", "settings": "settings", "service": "service", "nvmeof": "nvmeof",
           "nfs-export": "nfs-export", "gateway": "gateway", "object-user": "object-user", "object-bucket": "object-bucket"}
UNITS = ("execute_unit", "execute_nvmeof", "execute_gateway", "execute_user", "execute_bucket")
STEPS = ("host_round", "tunnel_before", "apply_tunnel_cluster", "activate_stretch", "apply_filesystem", "wait_ready", "observe_state", "dashboard_phase",
         "recorded", "observed")


class Transport:
    def __init__(self, trace, ready):
        self.trace = trace
        self.ready = ready

    def call(self, host, phase, **options):
        self.trace.append("call %s %s%s" % (host["name"], phase, "".join(" " + key for key in sorted(options))))
        return {"changed": True, "ready": self.ready, "removed": True, "private": "secret"}


def shown(value):
    if value == "d" * 64:
        return "digest"
    if isinstance(value, (str, bool, tuple, type(None))):
        return str(value)
    if isinstance(value, dict) and "action" in value:
        return "request"
    if callable(value) and not isinstance(value, Transport):
        return "fn"
    return type(value).__name__


def recorder(trace, name, answer):
    def record(*args, **kwargs):
        trace.append(" ".join([name, *map(shown, args), *("%s=%s" % (k, shown(v)) for k, v in sorted(kwargs.items()))]))
        return answer(*args, **kwargs)
    return record


def run(monkeypatch, name, operation, outputs, ready):
    request = goldens.prepared(goldens.REQUESTS[GOLDENS[name]])
    trace = []
    for unit in UNITS:
        monkeypatch.setattr(ceph_core, unit, recorder(trace, unit, lambda *a, **k: {"unit": True}))
    answers = {
        "host_round": lambda *a, **k: [{"prepared": True}],
        "tunnel_before": lambda *a, **k: ("pending", {"ready": ready}),
        "apply_tunnel_cluster": lambda *a, **k: True,
        "activate_stretch": lambda *a, **k: True,
        "apply_filesystem": lambda *a, **k: True,
        "wait_ready": lambda *a, **k: {"ready": True, "removed": False},
        "observe_state": lambda *a, **k: {"ready": ready, "removed": True},
        "dashboard_phase": lambda request, operation, digest, bootstrap, transport, observation, output, **k: (observation, True),
        "recorded": lambda observation, *a: observation,
        "observed": lambda result, operation, pending: result,
    }
    for step in STEPS:
        monkeypatch.setattr(ceph_core, step, recorder(trace, step, answers[step]))
    monkeypatch.setattr(ceph_core.private_files, "write", recorder(trace, "write", lambda *a: None))
    try:
        result = ceph_core.execute(request, operation, "d" * 64, Transport(trace, ready), outputs)
    except ceph_native.Refusal as error:
        result = "refused:" + str(error)
    return result, trace


def outputs_for(name, operation, declared):
    output = ceph_core.OUTPUTS.get(GOLDENS[name].split("-")[0] if name == "cluster-stretch" else name)
    if not declared or output is None:
        return None
    return {output: "/nonexistent/" + output}


UNIT_ACTIONS = ("service", "nvmeof", "nfs-export", "gateway", "object-user", "object-bucket")
CASES = [(name, operation, declared, ready) for name in GOLDENS for operation in ("apply", "observe", "destroy")
         for declared in ((False, True) if name.split("-")[0] in ceph_core.OUTPUTS or name == "object-user" else (False,))
         for ready in ((False,) if name in UNIT_ACTIONS else (False, True))]


@pytest.mark.parametrize(("name", "operation", "declared", "ready"), CASES)
def test_execute_dispatches_each_action_as_before(monkeypatch, name, operation, declared, ready):
    result, trace = run(monkeypatch, name, operation, outputs_for(name, operation, declared), ready)
    key = "%s/%s/%s/%s" % (name, operation, "outputs" if declared else "none", "ready" if ready else "unready")
    assert "; ".join([repr(result), *trace]) == PINNED[key], key


def test_every_action_the_validator_admits_has_one_procedure():
    assert set(ceph_core.PROCEDURES) == set(GOLDENS) - {"cluster-stretch"}
    assert set(ceph_core.PROCEDURES) >= set(ceph_native_resources.ACTIONS)


def test_the_pins_cover_exactly_the_cases():
    keys = {"%s/%s/%s/%s" % (name, operation, "outputs" if declared else "none", "ready" if ready else "unready")
            for name, operation, declared, ready in CASES}
    assert set(PINNED) == keys


PINNED = json.loads((pathlib.Path(__file__).resolve().parent / "data" / "ceph_execute_dispatch.json").read_text(encoding="utf-8"))

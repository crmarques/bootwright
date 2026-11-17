"""The physical_erase module's main() runs over a stubbed custody controller, as a task would run it.

The controller's own custody is tested in test_physical_erase_controller.py and
its siblings. These cases hold what the module itself decides: which phase calls
what, and the completion-drift verdict of an observation over the records a
finished erase left in the controller's private directory.
"""

from __future__ import annotations

import json
import pytest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as custody
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker
from ansible_collections.bootwright.core.plugins.modules import physical_erase as module

DIGEST = "d" * 64
RECEIPT = {"wiped": "all"}
REFUSED = "physical erase did not prove its bounded custody or receipt contract"


@pytest.fixture(name="controller")
def stubbed_controller(tmp_path, monkeypatch):
    """A controller whose private directory is the test's and whose actions are recorded.

    The controller's records are read as plain files, since the real read
    demands root's ownership. `receipt` is what receipt() returns, or the exception it raises, and
    `await_error` is the exception await_receipt() raises.
    """
    state = {"calls": [], "closed": 0, "root": tmp_path, "receipt": None, "await_error": None, "created": None}

    class Controller:
        root = tmp_path

        def __init__(self, request, digest, material, create=False):
            assert digest == DIGEST
            state["created"] = create

        def prepare(self):
            state["calls"].append("prepare")
            return {"evidence": {"prepared": True}}

        def build(self):
            state["calls"].append("build")

        def control(self, phase):
            state["calls"].append(phase)

        def await_receipt(self):
            state["calls"].append("await")
            if state["await_error"]:
                raise state["await_error"]  # pylint: disable=raising-bad-type
            return RECEIPT

        def acknowledge(self):
            state["calls"].append("ack")
            return True

        def receipt(self):
            if isinstance(state["receipt"], Exception):
                raise state["receipt"]  # pylint: disable=raising-bad-type
            return state["receipt"]

        def finish(self, evidence):
            state["calls"].append(("finish", evidence))

        def close(self):
            state["closed"] += 1

    monkeypatch.setattr(custody, "Controller", Controller)
    monkeypatch.setattr(custody, "read", lambda path, limit=32768: path.read_bytes())
    return state


def arguments(phase):
    return {"phase": phase, "request": {"version": "physical-erase-v1"}, "digest": DIGEST, "material": {}}


def record(path, evidence):
    path.write_text(json.dumps(evidence), encoding="utf-8")


def evidence(**changes):
    return dict({"version": worker.VERSION, "request": DIGEST, "prepared": True, "removed": False}, **changes)


def test_each_phase_drives_the_controller_and_the_controller_is_closed_after_every_run(controller, run_module):
    prepared = run_module(module, arguments("prepare"))
    assert prepared["changed"] is True and prepared["evidence"] == {"prepared": True} and controller["created"] is True
    for phase in ("build", "start", "stop-proof"):
        assert run_module(module, arguments(phase))["changed"] is True
    assert run_module(module, arguments("await"))["receipt"] == RECEIPT
    assert run_module(module, arguments("ack"))["changed"] is True
    assert controller["calls"] == ["prepare", "build", "start", "stop-proof", "await", "ack"]
    assert controller["closed"] == 6 and controller["created"] is False


def test_finish_removes_only_with_a_receipt_and_hands_the_controller_the_removed_evidence(controller, run_module):
    refused = run_module(module, arguments("finish"))
    assert refused["failed"] is True and refused["msg"] == REFUSED and controller["calls"] == []
    controller["receipt"] = RECEIPT
    finished = run_module(module, arguments("finish"))
    assert finished["changed"] is True and finished["evidence"] == evidence(receipt=RECEIPT, removed=True)
    assert controller["calls"] == [("finish", evidence(receipt=RECEIPT, removed=True))]


def test_an_observation_of_a_finished_erase_reports_removed_and_one_that_drifted_from_its_record_fails(controller, tmp_path, run_module):
    controller["receipt"] = RECEIPT
    waiting = run_module(module, arguments("observe"))
    assert waiting["changed"] is False and waiting["finalizing"] is False
    assert waiting["evidence"] == evidence(receipt=RECEIPT)
    record(tmp_path / "completed.json", evidence(receipt=RECEIPT, removed=True))
    completed = run_module(module, arguments("observe"))
    assert completed["evidence"] == evidence(receipt=RECEIPT, removed=True) and completed["changed"] is False
    record(tmp_path / "completed.json", evidence(receipt={"wiped": "some"}, removed=True))
    drifted = run_module(module, arguments("observe"))
    assert drifted["failed"] is True and drifted["msg"] == REFUSED


def test_an_interrupted_finish_is_checked_against_its_finalizing_record(controller, tmp_path, run_module):
    controller["receipt"] = RECEIPT
    record(tmp_path / "finalizing.json", evidence(receipt=RECEIPT, removed=True))
    finalizing = run_module(module, arguments("observe"))
    assert finalizing["finalizing"] is True and finalizing["evidence"] == evidence(receipt=RECEIPT)
    record(tmp_path / "finalizing.json", evidence(receipt={"wiped": "some"}, removed=True))
    assert run_module(module, arguments("observe"))["msg"] == REFUSED


def test_a_custody_that_was_never_prepared_observes_as_unprepared_and_fails_every_other_phase(controller, run_module):
    controller["receipt"] = FileNotFoundError("no custody")
    observed = run_module(module, arguments("observe"))
    assert observed["changed"] is False and observed["finalizing"] is False
    assert observed["evidence"] == evidence(prepared=False)
    failed = run_module(module, arguments("finish"))
    assert failed["failed"] is True and failed["msg"] == "physical erase custody is incomplete"


def test_a_worker_that_stopped_without_a_receipt_tells_the_operator_to_power_off_and_repeat(controller, run_module):
    controller["await_error"] = worker.Refusal("worker-failed")
    result = run_module(module, arguments("await"))
    assert result["failed"] is True and "power off this Machine" in result["msg"] and "repeat the full frozen overwrite" in result["msg"]
    assert controller["closed"] == 1

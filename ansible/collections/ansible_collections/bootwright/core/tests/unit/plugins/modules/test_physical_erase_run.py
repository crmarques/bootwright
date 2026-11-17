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
    `await_error` is the exception poll() raises.
    """
    state = {"calls": [], "closed": 0, "root": tmp_path, "receipt": None, "await_error": None, "created": None, "earlier": False,
             "custody": True, "withdrawn": False}

    class Controller:
        root = tmp_path

        def __init__(self, request, digest, material, create=False):
            assert digest == DIGEST
            if isinstance(state["custody"], Exception):
                raise state["custody"]  # pylint: disable=raising-bad-type
            state["created"] = create

        def prepare(self):
            state["calls"].append("prepare")
            return {"evidence": {"prepared": True}}

        def build(self):
            state["calls"].append("build")

        def control(self, phase):
            state["calls"].append(phase)

        def poll(self):
            state["calls"].append("poll")
            if state["await_error"]:
                raise state["await_error"]  # pylint: disable=raising-bad-type
            return {"answer": "complete", "receipt": RECEIPT}

        def earlier(self):
            return state["earlier"]

        def acknowledge(self):
            state["calls"].append("ack")
            return True

        def receipt(self):
            if isinstance(state["receipt"], Exception):
                raise state["receipt"]  # pylint: disable=raising-bad-type
            return state["receipt"]

        def finish(self, evidence):
            state["calls"].append(("finish", evidence))

        def withdraw(self):
            state["calls"].append("withdraw")
            if isinstance(state["withdrawn"], Exception):
                raise state["withdrawn"]  # pylint: disable=raising-bad-type
            return state["withdrawn"]

        def withdrawn(self):
            state["calls"].append("withdrawn")
            return state["withdrawn"]

        def close(self):
            state["closed"] += 1

    monkeypatch.setattr(custody, "Controller", Controller)
    monkeypatch.setattr(custody, "read", lambda path, limit=32768: path.read_bytes())
    return state


def controller_class():
    return custody.Controller


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
    polled = run_module(module, arguments("poll"))
    assert polled["answer"] == "complete" and polled["receipt"] == RECEIPT
    assert run_module(module, arguments("ack"))["changed"] is True
    assert controller["calls"] == ["prepare", "build", "start", "stop-proof", "poll", "ack"]
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
    assert "failed" not in drifted and drifted["refused"] == "custody-drift"


def test_an_interrupted_finish_is_checked_against_its_finalizing_record(controller, tmp_path, run_module):
    controller["receipt"] = RECEIPT
    record(tmp_path / "finalizing.json", evidence(receipt=RECEIPT, removed=True))
    finalizing = run_module(module, arguments("observe"))
    assert finalizing["finalizing"] is True and finalizing["evidence"] == evidence(receipt=RECEIPT)
    record(tmp_path / "finalizing.json", evidence(receipt={"wiped": "some"}, removed=True))
    assert run_module(module, arguments("observe"))["refused"] == "custody-drift"


def test_a_custody_that_was_never_prepared_observes_as_unprepared_and_fails_every_other_phase(controller, run_module):
    controller["receipt"] = FileNotFoundError("no custody")
    observed = run_module(module, arguments("observe"))
    assert observed["changed"] is False and observed["finalizing"] is False
    assert observed["evidence"] == evidence(prepared=False)
    failed = run_module(module, arguments("finish"))
    assert failed["failed"] is True and failed["msg"] == "physical erase custody is incomplete"


def test_a_worker_that_stopped_without_a_receipt_returns_its_named_refusal(controller, run_module):
    controller["await_error"] = worker.Refusal("worker-failed")
    result = run_module(module, arguments("poll"))
    assert "failed" not in result and result["refused"] == "worker-failed" and result["detail"] == "" and result["controller"] == ""
    assert controller["closed"] == 1


def test_an_effect_phase_returns_each_named_refusal_with_only_its_observed_detail(controller, run_module, monkeypatch):
    material = {"pinnedUUIDBase64": "ZXhhbXBsZS1zeXN0ZW0=", "controllerUser": "/run/bootwright/job/bmc-user", "originalAttempt": "op-remove/erase/1"}
    image = "https://artifact.example.test/private/" + "f" * 64 + "/erase.iso"
    cases = (
        (custody.redfish.Refused("machine-running", "power:On"), ("machine-running", "power:On", "")),
        (custody.redfish.Refused("identity-mismatch", "uuid:another-system"), ("identity-mismatch", "uuid:another-system", "")),
        (custody.redfish.Refused("controller-refused", "insert:http:409", "attaching /redfish/v1/Managers/1/VirtualMedia/CD: HTTP 409"),
         ("controller-refused", "insert:http:409", "attaching /redfish/v1/Managers/1/VirtualMedia/CD: HTTP 409")),
        (custody.redfish.redfish_control.ControllerError("reading /redfish/v1/Systems/1: HTTP 0"),
         ("target-unanswered", "", "reading /redfish/v1/Systems/1: HTTP 0")),
        (worker.Refusal("custody-policy-drift"), ("custody-drift", "", "")),
        (worker.Refusal("foreign-target"), ("foreign-custody", "", "")),
    )
    for raised, expected in cases:
        def control(self, phase, raised=raised):
            raise raised
        monkeypatch.setattr(custody.Controller, "control", control)
        result = run_module(module, dict(arguments("start"), material=material))
        assert "failed" not in result and (result["refused"], result["detail"], result["controller"]) == expected
        assert image not in json.dumps(result) and "f" * 64 not in json.dumps(result)


def test_an_observation_and_a_build_name_their_custody_and_media_refusals(controller, run_module, monkeypatch):
    for raised, code in ((worker.Refusal("foreign-custody"), "foreign-custody"), (worker.Refusal("completion-drift"), "custody-drift")):
        controller["receipt"] = raised
        observed = run_module(module, arguments("observe"))
        assert "failed" not in observed and observed["refused"] == code and observed["changed"] is False
    controller["receipt"] = worker.Refusal("receipt")
    assert run_module(module, arguments("observe"))["msg"] == REFUSED
    for raised, code in (("media-identity", "boot-media-mismatch"), ("install-network", "install-network-unsupported"),
                         ("native-timeout", "image-build-failed"), ("image-copy", "image-build-failed"),
                         ("foreign-target", "foreign-custody")):
        def build(self, raised=raised):
            raise worker.Refusal(raised)
        monkeypatch.setattr(controller_class(), "build", build)
        built = run_module(module, arguments("build"))
        assert "failed" not in built and (built["refused"], built["detail"], built["controller"]) == (code, "", "")

    def keygen(self):
        raise worker.Refusal("native-command")
    monkeypatch.setattr(controller_class(), "prepare", keygen)
    assert run_module(module, arguments("prepare"))["msg"] == REFUSED, "a build code is named only by the build"


def test_an_observation_of_another_removals_custody_reads_unprepared_for_this_removal(controller, tmp_path, run_module):
    controller["receipt"], controller["earlier"] = RECEIPT, True
    record(tmp_path / "completed.json", evidence(receipt=RECEIPT, removed=True))
    observed = run_module(module, arguments("observe"))
    assert observed["changed"] is False and observed["finalizing"] is False and observed["evidence"] == evidence(prepared=False)


def test_an_unnamed_refusal_still_fails_with_the_generic_message(controller, run_module, monkeypatch):

    def control(self, phase):
        raise worker.Refusal("receipt-missing")
    monkeypatch.setattr(custody.Controller, "control", control)
    unnamed = run_module(module, arguments("stop-proof"))
    assert unnamed["failed"] is True and unnamed["msg"] == REFUSED


RETAIN = {"relaxationBase64": "cmV0YWlu", "applyOperation": "op-" + "0" * 32, "originalAttempt": "op-remove-2/erase-metal-01/1"}
RETAINED = {"version": worker.VERSION, "request": DIGEST, "prepared": True, "removed": True, "retained": True}


def test_a_retained_erase_withdraws_its_custody_and_answers_retention_without_a_receipt(controller, run_module):
    for withdrawn in (True, False):
        controller["withdrawn"], controller["calls"] = withdrawn, []
        result = run_module(module, dict(arguments("withdraw"), material=RETAIN))
        assert result["changed"] is withdrawn and result["evidence"] == RETAINED and "receipt" not in result["evidence"]
        assert controller["calls"] == ["withdraw"] and controller["created"] is False
    controller["custody"], controller["calls"] = FileNotFoundError("no custody"), []
    nothing = run_module(module, dict(arguments("withdraw"), material=RETAIN))
    assert nothing["changed"] is False and nothing["evidence"] == RETAINED and controller["calls"] == []


def test_a_retained_erase_observation_changes_nothing_and_proves_retention_only_once_withdrawn(controller, run_module):
    unproved = run_module(module, dict(arguments("observe"), material=RETAIN))
    assert unproved["changed"] is False and unproved["evidence"] == evidence() and controller["calls"] == ["withdrawn"]
    controller["withdrawn"] = True
    proved = run_module(module, dict(arguments("observe"), material=RETAIN))
    assert proved["changed"] is False and proved["evidence"] == RETAINED
    controller["custody"] = FileNotFoundError("no custody")
    assert run_module(module, dict(arguments("observe"), material=RETAIN))["evidence"] == RETAINED
    assert "withdraw" not in controller["calls"]


def test_a_withdrawal_refusal_is_named_and_only_a_retained_erase_withdraws(controller, run_module):
    controller["withdrawn"] = worker.Refusal("custody-token")
    refused = run_module(module, dict(arguments("withdraw"), material=RETAIN))
    assert "failed" not in refused and refused["refused"] == "custody-drift"
    metadata = dict(RETAIN, relaxationBase64="bWV0YWRhdGFDbGVhcg==")
    for phase, material in (("withdraw", {}), ("withdraw", metadata), ("prepare", RETAIN), ("start", RETAIN), ("finish", RETAIN),
                            ("observe", dict(RETAIN, relaxationBase64="UmV0YWlu"))):
        controller["calls"] = []
        failed = run_module(module, dict(arguments(phase), material=material))
        assert failed["failed"] is True and failed["msg"] == REFUSED and controller["calls"] == [], (phase, material)

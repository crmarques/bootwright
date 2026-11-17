"""The redfish_firmware module's main() runs with the controller client and the firmware step stubbed, as a task would run it.

The convergence is tested in test_redfish_firmware.py over an emulated BMC
that answers through the client's own requests. These cases hold the module's
part: how the client is built from the task's arguments, what it hands the
step, and the messages its two failure kinds end the task with.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.modules import redfish_firmware as module

PASSWORD = "p4ssw0rd-never-shown"
DECLARED = {"mode": "apply", "settings": {"BootMode": "Uefi"}}
ARGUMENTS = {"endpoint": "https://bmc.example.test", "user": "operator", "password": PASSWORD, "firmware": DECLARED}


@pytest.fixture(name="step")
def stubbed_client(monkeypatch):
    """The BMC client, recording how it was built, and the firmware step answering from `outcome` or raising it."""
    state = {"clients": [], "steps": [], "outcome": {"changed": True, "refusal": "", "paths": ["/Bios/BootMode"]}}

    class Client:
        def __init__(self, endpoint, user, password, **options):
            state["clients"].append((endpoint, user, password, options))

    def drive(client, firmware):
        state["steps"].append((client, firmware))
        if isinstance(state["outcome"], Exception):
            raise state["outcome"]  # pylint: disable=raising-bad-type
        return state["outcome"]

    monkeypatch.setattr(redfish_control, "Client", Client)
    monkeypatch.setattr(module, "drive", drive)
    return state


def test_the_client_is_built_from_the_task_and_the_steps_outcome_is_returned(step, run_module):
    result = run_module(module, dict(ARGUMENTS, verify=False, ca_data="pem", verification="certificate"))
    assert result["changed"] is True and result["paths"] == ["/Bios/BootMode"] and PASSWORD not in str(result)
    ((endpoint, user, password, options),) = step["clients"]
    assert (endpoint, user, password) == ("https://bmc.example.test", "operator", PASSWORD)
    assert options == {"verify": False, "ca_data": "pem", "verification": "certificate"}
    assert step["steps"][0][1] == DECLARED


def test_the_client_verifies_by_default_and_names_no_verification_level(step, run_module):
    run_module(module, ARGUMENTS)
    assert step["clients"][0][3] == {"verify": True, "ca_data": "", "verification": ""}


def test_a_declaration_the_step_refuses_and_a_controller_that_fails_end_the_task_with_their_own_messages(step, run_module):
    step["outcome"] = ValueError("the firmware mode is not apply or prove")
    declared = run_module(module, ARGUMENTS)
    assert declared["failed"] is True and declared["msg"] == "the firmware declaration was not accepted: the firmware mode is not apply or prove"
    step["outcome"] = redfish_control.ControllerError("the management controller answered 503")
    unread = run_module(module, ARGUMENTS)
    assert unread["failed"] is True and unread["msg"] == "the firmware settings could not be read: the management controller answered 503"
    assert PASSWORD not in str(declared) + str(unread)

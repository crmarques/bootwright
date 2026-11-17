"""Every code and phase disk discovery can name has its operator diagnostic, and no description is dead.

The codes are driven out of the runtime the way a failure reaches it: each
native tool's failure, each management-controller cause, each listener status
and transport failure during media delivery, each refusal text, and every
other error class diagnostic() sorts. The set they produce, with the action
plugin's own refusal, is exactly the set FAILURES describes.
"""

from __future__ import annotations

import errno
import pathlib
import re
import socket
import ssl
import urllib.error
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.filter.disk_discovery_refusal import disk_discovery_refusal
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_runtime as runtime
from ansible_collections.bootwright.core.plugins.module_utils import ssh_key_policy
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

CONTROLLER = pathlib.Path(runtime.__file__).with_name("disk_discovery_controller.py")
SECRET = "https://192.0.2.1/private/" + "0123456789abcdef" * 4


def delivery_errors():
    for status in [*range(300, 600), True, None, "SECRET"]:
        yield urllib.error.HTTPError(SECRET, status, "SECRET", {}, None)
    for reason in (ssl.SSLCertVerificationError(1, "SECRET"), ssl.SSLError(1, "SECRET"), socket.gaierror(-2, "SECRET"),
                   TimeoutError("SECRET"), ConnectionRefusedError(111, "SECRET"), ConnectionResetError(104, "SECRET")):
        yield urllib.error.URLError(reason)
        yield reason
    yield OSError("SECRET")


def emitted():
    """Every (code, phase) diagnostic() returns over the errors the runtime classifies."""
    found = set()

    def add(error, phase):
        found.add(runtime.diagnostic(error, SimpleNamespace(phase=phase))["discovery_failure"])

    for tool in (*media.TOOLS, "SECRET"):
        add(media.CommandFailure(tool, 1), "media")
    add(ssh_key_policy.PolicyError("SECRET"), "ssh")
    add(redfish_control.UnverifiedCertificate("SECRET"), "controller")
    for cause in (*runtime.BMC_CAUSES, "request", "SECRET", None):
        add(redfish_control.ControllerError("SECRET", cause=cause), "media-insertion")
    for error in delivery_errors():
        add(error, "media-delivery")
    add(FileNotFoundError("SECRET"), "custody")
    for phase in runtime.PHASES:
        add(OSError(errno.ENOSPC, "SECRET"), phase)
        add(OSError(errno.EDQUOT, "SECRET"), phase)
    add(OSError(errno.EACCES, "SECRET"), "custody")
    add(bounded_process.Failure("timeout"), "ssh")
    for text in (*runtime.REFUSALS, "SECRET"):
        add(ValueError(text), "custody")
    add(KeyError("SECRET"), "custody")
    return found


def test_every_code_the_runtime_can_emit_has_its_description_and_every_description_a_code():
    # The action plugin refuses an exception the runtime did not bound as "contract".
    codes = emitted() | {"contract"}
    assert codes - set(runtime.FAILURES) == set(), "codes with no operator description"
    assert set(runtime.FAILURES) - codes == set(), "descriptions no code reaches"
    assert {code for code in codes if code.startswith("media-http-")} == {"media-http-%d" % status for status in runtime.DELIVERY_STATUSES}
    assert {code for code in codes if code.startswith("native-")} >= {"native-" + tool for tool in media.TOOLS}


def test_every_phase_the_controller_enters_has_its_description():
    entered = set(re.findall(r'self\.phase = "([^"]+)"', CONTROLLER.read_text()))
    assert entered == set(runtime.PHASES)


@pytest.mark.parametrize("code", sorted(runtime.FAILURES))
def test_each_code_is_described_in_its_phase(code):
    for phase, during in runtime.PHASES.items():
        assert disk_discovery_refusal({"discovery_failure": code, "discovery_phase": phase}) == (
            "disk discovery stopped during %s: %s" % (during, runtime.FAILURES[code]))


@pytest.mark.parametrize("result", [
    None, {}, {"failed": True}, {"discovery_failure": ""}, {"discovery_failure": ["SECRET"]},
    {"discovery_failure": "request", "discovery_phase": {"secret": "SECRET"}},
])
def test_a_result_with_no_bounded_code_is_reported_as_such(result):
    assert disk_discovery_refusal(result) == runtime.NO_RESULT


def test_an_unknown_code_or_phase_is_named_without_its_text():
    assert disk_discovery_refusal({"discovery_failure": "SECRET", "discovery_phase": "SECRET"}) == (
        "disk discovery stopped during adapter execution: the adapter named a failure this collection has no description for")
    assert disk_discovery_refusal({"discovery_failure": "request"}) == (
        "disk discovery stopped during adapter execution: the frozen request shape was refused")

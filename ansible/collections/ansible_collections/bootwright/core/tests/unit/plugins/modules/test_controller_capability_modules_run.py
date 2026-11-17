"""The controller capability modules refuse every direct run: only their paired action plugins act.

controller_protocol and controller_tool are documentation entry points. Their
main() only builds the argument spec the paired action reads and then fails, so
a task that reaches the module, without the action, changes nothing.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import controller_protocol, controller_tool

PROTOCOL = {"phase": "loaded", "request": {"version": "controller-v1"}}
TOOL = {"bundle": {"path": "/var/lib/example/bundle"}, "tools": [{"name": "oc"}], "egress": {"httpProxy": ""},
        "inspect_only": True, "acquisition": [{"source": "tool-oc", "seconds": 205}], "trust": {"host": "", "file": ""}}


@pytest.mark.parametrize("module, arguments, message", [
    (controller_protocol, PROTOCOL, "This private capability requires its qualified controller action."),
    (controller_tool, TOOL, "This capability requires its fixed controller action plugin."),
])
def test_a_direct_run_fails_naming_its_action_and_returns_nothing_else(run_module, module, arguments, message):
    result = run_module(module, arguments)
    assert result["failed"] is True and result["msg"] == message
    assert set(result) <= {"failed", "msg", "invocation"}


@pytest.mark.parametrize("module, arguments, missing", [
    (controller_protocol, {"request": {}}, "phase"),
    (controller_tool, dict(TOOL, acquisition=None), "acquisition"),
])
def test_a_run_missing_a_required_argument_fails_on_the_argument_before_the_capability_message(run_module, module, arguments, missing):
    arguments = {name: value for name, value in arguments.items() if value is not None}
    result = run_module(module, arguments)
    assert result["failed"] is True and missing in result["msg"] and "requires its" not in result["msg"]

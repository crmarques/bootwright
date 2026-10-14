"""The controller stage's tool publication stays bound to its frozen request."""

import unittest
from types import SimpleNamespace

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.action import controller_protocol
from ansible_collections.bootwright.core.plugins.action.controller_protocol import (
    frozen_request,
    preparation,
    refusal_reason,
)


def frozen():
    return dict(
        version="controller-prerequisites-v6",
        identity="a" * 64,
        publicationBundle={},
        tools=[dict(source=dict(id="tool-oc")), dict(source=dict(id="tool-kubectl"))],
        acquisition=[dict(source="tool-oc", seconds=205), dict(source="tool-kubectl", seconds=121)],
        egress={},
    )


class FrozenRequest(unittest.TestCase):
    """Each tool's acquisition deadline travels with the request, in its order."""

    def test_a_v6_request_whose_deadlines_match_its_tools_is_accepted(self):
        request = frozen()
        self.assertIs(frozen_request(request), request)

    def test_a_native_or_older_request_or_a_mismatched_deadline_is_refused(self):
        request = frozen()
        missing = dict(request)
        del missing["acquisition"]
        changes = {
            "controller-prerequisites-v5": dict(request, version="controller-prerequisites-v5"),
            "native packages": dict(request, packages=[], native=None),
            "no tools": dict(request, tools=[], acquisition=[]),
            "missing": missing,
            "reordered": dict(request, acquisition=list(reversed(request["acquisition"]))),
            "short": dict(request, acquisition=request["acquisition"][:1]),
            "bool": dict(request, acquisition=[request["acquisition"][0], dict(source="tool-kubectl", seconds=True)]),
            "zero": dict(request, acquisition=[request["acquisition"][0], dict(source="tool-kubectl", seconds=0)]),
            "past the ceiling": dict(request, acquisition=[request["acquisition"][0], dict(source="tool-kubectl", seconds=7201)]),
            "text": dict(request, acquisition=[request["acquisition"][0], dict(source="tool-kubectl", seconds="121")]),
            "extra key": dict(request, acquisition=[request["acquisition"][0], dict(source="tool-kubectl", seconds=121, bytes=1)]),
        }
        for name, changed in changes.items():
            with self.subTest(name):
                with self.assertRaises((KeyError, TypeError, ValueError)):
                    frozen_request(changed)


class RefusalReason(unittest.TestCase):
    """Every refusal here reaches one generic message, so it carries its token."""

    def test_a_raised_token_is_what_the_message_reports(self):
        self.assertEqual(refusal_reason(ValueError("tools completion")), "tools completion")
        self.assertEqual(refusal_reason(KeyError("preparation")), "preparation")

    def test_an_operating_system_failure_reports_its_kind_and_no_path(self):
        reported = refusal_reason(OSError(2, "No such file", "/var/lib/bootwright/x"))
        self.assertEqual(reported, "FileNotFoundError")
        self.assertNotIn("/var/lib/bootwright", reported)
        self.assertEqual(refusal_reason(TypeError("unhashable")), "TypeError")

    def test_a_token_is_bounded_and_a_bare_refusal_still_names_its_kind(self):
        self.assertEqual(refusal_reason(ValueError("x" * 500)), "x" * 120)
        self.assertEqual(refusal_reason(ValueError()), "ValueError")


class Preparation(unittest.TestCase):
    def test_a_preparation_names_exactly_the_frozen_tools_sources_in_order(self):
        self.assertEqual(preparation(frozen()), {"addedSources": ["tool-kubectl", "tool-oc"]})

    def test_a_source_named_twice_is_refused(self):
        request = dict(frozen(), tools=[dict(source=dict(id="tool-oc")), dict(source=dict(id="tool-oc"))])
        with self.assertRaises(ValueError):
            preparation(request)


class Run(unittest.TestCase):
    """The prepared and completed phases hand the runner what Go verifies."""

    def run_phase(self, arguments):
        records = []
        original_emit, original_run = controller_protocol.emit, ActionBase.run
        controller_protocol.emit = lambda record, acknowledge=False: records.append((record, acknowledge))
        ActionBase.run = lambda self, tmp=None, task_vars=None: {}
        try:
            action = controller_protocol.ActionModule.__new__(controller_protocol.ActionModule)
            action._task = SimpleNamespace(args=arguments)
            return action.run(task_vars={}), records
        finally:
            controller_protocol.emit, ActionBase.run = original_emit, original_run

    def test_prepared_announces_the_sources_and_waits(self):
        result, records = self.run_phase({"phase": "prepared", "request": frozen()})
        self.assertFalse(result.get("failed"), result)
        self.assertEqual(records, [({"phase": "prepared", "preparation": {"addedSources": ["tool-kubectl", "tool-oc"]}}, True)])

    def test_completed_reports_the_tools_evidence(self):
        tools = [dict(source="tool-oc"), dict(source="tool-kubectl")]
        result, records = self.run_phase({
            "phase": "completed", "request": frozen(), "preparation": preparation(frozen()), "tools": tools, "tools_changed": True,
        })
        self.assertFalse(result.get("failed"), result)
        evidence = {"request": "a" * 64, "tools": tools, "postcondition": True}
        self.assertEqual(records, [({"phase": "completed", "outcome": "changed", "evidence": evidence}, False)])

    def test_completed_refuses_another_preparation_or_missing_evidence(self):
        for name, arguments in {
            "another preparation": {"preparation": {"addedSources": []}, "tools": [{}, {}]},
            "one tool short": {"preparation": preparation(frozen()), "tools": [{}]},
        }.items():
            with self.subTest(name):
                result, records = self.run_phase(dict(arguments, phase="completed", request=frozen(), tools_changed=False))
                self.assertTrue(result.get("failed"), result)
                self.assertEqual(records, [])


if __name__ == "__main__":
    unittest.main()

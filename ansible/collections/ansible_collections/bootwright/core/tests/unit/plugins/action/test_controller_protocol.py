"""Frozen native transitions remain binding across preparation and recovery."""

import copy
import unittest

from ansible_collections.bootwright.core.plugins.action.controller_protocol import (
    digest,
    frozen_request,
    preparation,
    refusal_reason,
)


class FrozenRequest(unittest.TestCase):
    """Each tool's acquisition deadline travels with the request, in its order."""

    def request(self):
        return dict(
            version="controller-prerequisites-v4",
            operation="setup",
            identity="a" * 64,
            platform={},
            bundle={},
            publicationBundle={},
            packages=[],
            native=None,
            tools=[dict(source=dict(id="tool-oc")), dict(source=dict(id="tool-kubectl"))],
            acquisition=[dict(source="tool-oc", seconds=205), dict(source="tool-kubectl", seconds=121)],
            egress={},
        )

    def test_a_v4_request_whose_deadlines_match_its_tools_is_accepted(self):
        request = self.request()
        self.assertIs(frozen_request(request), request)
        empty = dict(request, tools=[], acquisition=[])
        self.assertIs(frozen_request(empty), empty)

    def test_an_older_version_or_a_mismatched_deadline_is_refused(self):
        request = self.request()
        missing = dict(request)
        del missing["acquisition"]
        changes = {
            "controller-prerequisites-v3": dict(request, version="controller-prerequisites-v3"),
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
        self.assertEqual(refusal_reason(ValueError("native postcondition")), "native postcondition")
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
    def fixture(self):
        before = [
            dict(
                name="fixture", epoch=0, version="1", release="1", architecture="x86_64"
            )
        ]
        after = [dict(before[0], version="2")]
        plan = dict(
            platform={},
            packages=[],
            actions=[
                dict(
                    kind="upgrade",
                    before=before[0],
                    after=after[0],
                    sourceID="rpm",
                    reason="root",
                )
            ],
            beforeSHA256=digest(before),
            afterSHA256=digest(after),
        )
        plan["digest"] = digest(plan)
        request = dict(
            operation="setup", platform={}, packages=[], native=plan, tools=[]
        )
        return before, after, request

    def test_upgrade_recovery_requires_exact_before_or_after_and_same_plan(self):
        before, after, request = self.fixture()
        proof = preparation(request, before)
        request.update(operation="recover", preparation=proof)
        self.assertEqual(preparation(request, after), proof)
        self.assertEqual(preparation(request, before), proof)
        with self.assertRaises(ValueError):
            preparation(request, [dict(before[0], version="intermediate")])
        changed = copy.deepcopy(request)
        changed["native"]["actions"][0]["after"]["version"] = "3"
        with self.assertRaises(ValueError):
            preparation(changed, after)

    def test_tool_only_resume_does_not_grant_native_changes(self):
        request = dict(
            operation="setup",
            platform={},
            packages=[],
            native=None,
            tools=[dict(source=dict(id="tool"))],
        )
        proof = preparation(request, [])
        request.update(operation="recover", preparation=proof)
        self.assertEqual(preparation(request, []), proof)
        request["packages"] = [dict(name="unapproved")]
        with self.assertRaises(ValueError):
            preparation(request, [])


if __name__ == "__main__":
    unittest.main()

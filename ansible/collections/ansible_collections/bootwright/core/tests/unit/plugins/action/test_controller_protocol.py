"""Frozen native transitions remain binding across preparation and recovery."""

import copy
import unittest

from ansible_collections.bootwright.core.plugins.action.controller_protocol import (
    digest,
    preparation,
    refusal_reason,
)


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

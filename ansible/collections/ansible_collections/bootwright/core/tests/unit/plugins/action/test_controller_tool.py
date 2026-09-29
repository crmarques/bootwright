"""A target tool runs under the acquisition deadline its request froze."""

from __future__ import annotations

import unittest

from ansible_collections.bootwright.core.plugins.action.controller_tool import (
    tool_arguments,
)

ARGUMENTS = dict(bundle={}, tool={}, egress={}, inspect_only=False, deadline=205)


class ToolArguments(unittest.TestCase):
    def test_the_frozen_deadline_is_passed_on_in_seconds(self):
        self.assertEqual(tool_arguments(ARGUMENTS), ({}, {}, {}, 205, False))
        self.assertEqual(tool_arguments(dict(ARGUMENTS, deadline=1))[3], 1)
        self.assertEqual(tool_arguments(dict(ARGUMENTS, deadline=7200))[3], 7200)

    def test_a_missing_or_out_of_range_deadline_is_refused(self):
        missing = dict(ARGUMENTS)
        del missing["deadline"]
        for name, arguments in (
            ("missing", missing),
            ("zero", dict(ARGUMENTS, deadline=0)),
            ("bool", dict(ARGUMENTS, deadline=True)),
            ("past the ceiling", dict(ARGUMENTS, deadline=7201)),
            ("text", dict(ARGUMENTS, deadline="205")),
            ("extra argument", dict(ARGUMENTS, seconds=205)),
        ):
            with self.subTest(name):
                with self.assertRaises(ValueError):
                    tool_arguments(arguments)


if __name__ == "__main__":
    unittest.main()

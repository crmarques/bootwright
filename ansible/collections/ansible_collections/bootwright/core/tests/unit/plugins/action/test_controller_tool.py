"""A target tool runs under the acquisition deadline its request froze, and an
oc the release-stamp check refuses is named to the runner before it fails."""

from __future__ import annotations

import types
import unittest
from unittest import mock

from ansible.plugins.action import ActionBase
from ansible.utils.display import Display
from ansible_collections.bootwright.core.plugins.action import controller_tool
from ansible_collections.bootwright.core.plugins.action.controller_tool import (
    tool_arguments,
)
from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)
from ansible_collections.bootwright.core.plugins.module_utils import controller_refusal

ARGUMENTS = dict(bundle={}, tool={"source": {"id": "tool-oc"}}, egress={}, inspect_only=False, deadline=205)
RELEASE = "4.21.11"


class ToolArguments(unittest.TestCase):
    def test_the_frozen_deadline_is_passed_on_in_seconds(self):
        self.assertEqual(tool_arguments(ARGUMENTS), ({}, ARGUMENTS["tool"], {}, 205, False))
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


def unstamped(*_arguments):
    """Refuse as the release-stamp check refuses an oc that names no release."""
    scanner = files.ReleaseStamp(RELEASE)
    scanner.update(b"client executable" + files.RELEASE_MARKER)
    scanner.verify()


def too_long(*_arguments):
    """Refuse a release longer than the marker holds, before any stamp is read."""
    files.ReleaseStamp("4" * 92)


def run(refusal, channel=None):
    """Run the action with its preparation refused, and return its result and
    the records it wrote to the runner's channel."""
    records = []

    def emit(record, acknowledge=False):
        if channel is not None:
            raise channel
        records.append((record, acknowledge))

    action = controller_tool.ActionModule.__new__(controller_tool.ActionModule)
    setattr(action, "_task", types.SimpleNamespace(args=ARGUMENTS))
    with mock.patch.object(ActionBase, "run", return_value={}):
        with mock.patch.object(controller_tool, "prepare_tool", side_effect=refusal):
            with mock.patch.object(controller_refusal, "emit", side_effect=emit):
                return action.run(), records


class NamedRefusal(unittest.TestCase):
    def test_an_unstamped_oc_names_the_release_stamp_check_to_the_runner(self):
        result, records = run(unstamped)
        self.assertTrue(result["failed"])
        self.assertEqual(result["msg"], "The exact target tool could not be prepared or verified.")
        self.assertEqual(records, [({"phase": "refused", "reason": "release-stamp"}, False)])

    def test_an_acquisition_refusal_names_its_class_and_the_tools_source(self):
        refusal = files.AcquisitionRefused("acquisition deadline", reason="timeout", detail="Refused: acquisition deadline")
        with mock.patch.object(Display, "warning") as warning:
            result, records = run(refusal)
        warning.assert_called_once_with("controller adapter: Refused: acquisition deadline")
        self.assertTrue(result["failed"])
        self.assertEqual(records, [({"phase": "refused", "reason": "timeout", "source": "tool-oc"}, False)])

    def test_every_other_refusal_names_nothing(self):
        for name, refusal in (
            ("release too long", too_long),
            ("source integrity", files.Refused("source integrity")),
            ("unclassified acquisition", files.AcquisitionRefused("artifact path")),
            ("request", ValueError("request")),
            ("operating system", OSError("/private/path")),
        ):
            with self.subTest(name):
                result, records = run(refusal)
                self.assertTrue(result["failed"])
                self.assertEqual(records, [])

    def test_a_closed_channel_still_fails_the_task(self):
        for channel in (BrokenPipeError(), ValueError("protocol")):
            with self.subTest(type(channel).__name__):
                result, _records = run(unstamped, channel)
                self.assertTrue(result["failed"])


if __name__ == "__main__":
    unittest.main()

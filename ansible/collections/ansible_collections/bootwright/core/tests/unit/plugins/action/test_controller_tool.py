"""Each target tool runs under the acquisition deadline its request froze, and
an oc the release-stamp check refuses is named to the runner before it fails;
a refusal of one tool of the batch names that tool's source."""

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
from ansible_collections.bootwright.core.plugins.module_utils.controller_batch import (
    ToolRefused,
)

TRUST = {"host": "", "file": ""}
TOOLS = [{"source": {"id": "tool-oc"}}, {"source": {"id": "tool-helm"}}]
ACQUISITION = [{"source": "tool-oc", "seconds": 205}, {"source": "tool-helm", "seconds": 121}]
ARGUMENTS = dict(bundle={}, tools=TOOLS, egress={}, inspect_only=False, acquisition=ACQUISITION, trust=TRUST)
RELEASE = "4.21.11"


def deadline(seconds):
    return dict(ARGUMENTS, acquisition=[dict(ACQUISITION[0], seconds=seconds), ACQUISITION[1]])


class ToolArguments(unittest.TestCase):
    def test_the_frozen_deadline_is_passed_on_in_seconds(self):
        self.assertEqual(tool_arguments(ARGUMENTS), ({}, TOOLS, {}, [205, 121], False, TRUST))
        self.assertEqual(tool_arguments(deadline(1))[3], [1, 121])
        self.assertEqual(tool_arguments(deadline(7200))[3], [7200, 121])

    def test_a_missing_or_out_of_range_deadline_is_refused(self):
        missing = dict(ARGUMENTS)
        del missing["acquisition"]
        for name, arguments in (
            ("missing", missing),
            ("zero", deadline(0)),
            ("bool", deadline(True)),
            ("past the ceiling", deadline(7201)),
            ("text", deadline("205")),
            ("extra argument", dict(ARGUMENTS, seconds=205)),
            ("another source", dict(ARGUMENTS, acquisition=[ACQUISITION[1], ACQUISITION[0]])),
            ("one short", dict(ARGUMENTS, acquisition=ACQUISITION[:1])),
            ("no trust", {key: value for key, value in ARGUMENTS.items() if key != "trust"}),
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


def batch(refusal, source):
    """The batch's refusal of the tool whose source is source: as
    prepare_tools raises it, or unattributed when source is None."""

    def refuse(*_arguments):
        try:
            if callable(refusal):
                refusal()
            else:
                raise refusal
        except Exception as error:
            if source is None:
                raise
            raise ToolRefused(error, source) from None

    return refuse


def run(refusal, channel=None, source="tool-oc"):
    """Run the action with its preparation of the tool whose source is source
    refused, and return its result and the records it wrote to the runner's
    channel."""
    records = []

    def emit(record, acknowledge=False):
        if channel is not None:
            raise channel
        records.append((record, acknowledge))

    action = controller_tool.ActionModule.__new__(controller_tool.ActionModule)
    setattr(action, "_task", types.SimpleNamespace(args=ARGUMENTS))
    with mock.patch.object(ActionBase, "run", return_value={}):
        with mock.patch.object(controller_tool, "prepare_tools", side_effect=batch(refusal, source)):
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

    def test_a_refusal_of_a_later_tool_names_that_tools_source(self):
        refusal = files.AcquisitionRefused("acquisition deadline", reason="dns", detail="gaierror: x")
        with mock.patch.object(Display, "warning"):
            result, records = run(refusal, source="tool-helm")
        self.assertTrue(result["failed"])
        self.assertEqual(records, [({"phase": "refused", "reason": "dns", "source": "tool-helm"}, False)])

    def test_a_refusal_before_any_tool_names_nothing(self):
        refusal = files.AcquisitionRefused("acquisition deadline", reason="dns")
        result, records = run(refusal, source=None)
        self.assertTrue(result["failed"])
        self.assertEqual(records, [])

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

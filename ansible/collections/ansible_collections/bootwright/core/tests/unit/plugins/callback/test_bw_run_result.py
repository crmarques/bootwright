from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import sys
import tempfile
import types
import unittest
from unittest import mock


class _CallbackBase:
    def __init__(self, *args, **kwargs):
        pass


_ansible = types.ModuleType("ansible")
_ansible_plugins = types.ModuleType("ansible.plugins")
_ansible_callback = types.ModuleType("ansible.plugins.callback")
_ansible_callback.CallbackBase = _CallbackBase
sys.modules.setdefault("ansible", _ansible)
sys.modules.setdefault("ansible.plugins", _ansible_plugins)
sys.modules.setdefault("ansible.plugins.callback", _ansible_callback)

_CALLBACK_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "callback"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_run_result_callback",
    _CALLBACK_DIR / "bw_run_result.py",
)
assert _spec and _spec.loader
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)


class _Host:
    def get_name(self):
        return "host-a"


class _Task:
    def __init__(self, name):
        self.name = name


class _Result:
    def __init__(self, task_name):
        self._host = _Host()
        self._task = _Task(task_name)


class _Stats:
    processed = {"host-a": object()}


class RunResultCompletionSentinel(unittest.TestCase):
    def run_callback(self, task_name, status):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.dict(
                os.environ,
                {_module.ARTIFACTS_ENV: directory},
                clear=False,
            ):
                callback = _module.CallbackModule()
                getattr(callback, f"v2_runner_on_{status}")(_Result(task_name))
                callback.v2_playbook_on_stats(_Stats())
            path = pathlib.Path(directory) / _module.RESULT_NAME
            if not path.exists():
                return []
            return [json.loads(line) for line in path.read_text().splitlines()]

    def test_exact_ok_sentinel_emits_completion_and_terminal_count(self):
        records = self.run_callback(_module.DESTROY_COMPLETION_TASK, "ok")
        self.assertEqual(records[0]["completion"], True)
        self.assertEqual(records[0]["status"], "ok")
        self.assertEqual(records[-1]["hosts"]["host-a"]["completed"], 1)
        self.assertEqual(records[-1]["status"], "terminal")

    def test_prefixed_and_near_names_do_not_emit_completion(self):
        for name in [
            "role : Record Bootwright destroy completion",
            "Record Bootwright destroy completion now",
        ]:
            with self.subTest(name=name):
                records = self.run_callback(name, "ok")
                self.assertNotIn("completion", records[0])
                self.assertEqual(records[-1]["hosts"]["host-a"]["completed"], 0)

    def test_non_ok_sentinel_suppresses_terminal_proof(self):
        for status in ["skipped", "failed"]:
            with self.subTest(status=status):
                records = self.run_callback(
                    _module.DESTROY_COMPLETION_TASK,
                    status,
                )
                self.assertFalse(
                    any(record.get("status") == "terminal" for record in records)
                )

    def test_existing_or_symlinked_result_is_never_appended_or_followed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            target = root / "outside"
            target.write_text("untouched\n")
            result = root / _module.RESULT_NAME
            result.symlink_to(target)
            with mock.patch.dict(os.environ, {_module.ARTIFACTS_ENV: directory}, clear=False):
                callback = _module.CallbackModule()
                callback.v2_runner_on_ok(_Result(_module.DESTROY_COMPLETION_TASK))
                callback.v2_playbook_on_stats(_Stats())
            self.assertTrue(result.is_symlink())
            self.assertEqual(target.read_text(), "untouched\n")

        with tempfile.TemporaryDirectory() as directory:
            result = pathlib.Path(directory) / _module.RESULT_NAME
            result.write_text("existing\n")
            with mock.patch.dict(os.environ, {_module.ARTIFACTS_ENV: directory}, clear=False):
                callback = _module.CallbackModule()
                callback.v2_runner_on_ok(_Result(_module.DESTROY_COMPLETION_TASK))
                callback.v2_playbook_on_stats(_Stats())
            self.assertEqual(result.read_text(), "existing\n")


if __name__ == "__main__":
    unittest.main()

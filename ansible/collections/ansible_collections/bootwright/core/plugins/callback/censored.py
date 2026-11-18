"""The adapter's output: ansible-core's default, less what a no_log result raised.

ansible-core censors the result of a task under no_log but keeps the error,
warnings and deprecations the task raised, and its default callback prints them
whole: an assertion's templated message, a module error naming an argument and
a lookup or template error naming a value all reach the output that way. What
an adapter prints is retained on the promise that no_log keeps bound material
out of it (specs/security.md, Logs, output, and diagnostics), so this callback
prints what the default prints but those, for a censored result: a hidden task
that failed still reports that it failed, by its task name and ansible-core's
censored result, and nothing it said.

In a lifecycle run, which sets BOOTWRIGHT_STOP_RECORD, it also writes one
stopped record on the result channel for the first failure no rescue or
ignore_errors recovers: the task's name as written, its role, its host's
inventory name and whether it failed or was unreachable, so the runner can say
which task ended the run (specs/architecture.md, The adapter result protocol).
It never writes what the task said, and a failure to write changes nothing.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: censored
type: stdout
short_description: The default output, less what a no_log result raised
version_added: "0.1.0"
description:
  - Prints what C(ansible.builtin.default) prints, except the error, warnings
    and deprecations of a result C(no_log) censors, which it leaves out.
  - A failed C(no_log) task still reports that it failed, by its task name and
    ansible-core's censored result.
  - In a lifecycle run it writes one stopped record naming the first task whose
    failure nothing recovers.
extends_documentation_fragment:
  - ansible.builtin.default_callback
  - ansible.builtin.result_format_callback
requirements:
  - set as the stdout callback in configuration
author:
  - Bootwright contributors (@crmarques)
"""

import os
import re

from ansible.plugins.callback.default import CallbackModule as DefaultCallbackModule
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

# What a result carries that the default prints outside its censored body.
RAISED = ("exception", "warnings", "deprecations")


# The result channel's descriptor, and the bounds Go's decoder holds a stopped
# record's members to (internal/adapterprotocol).
CHANNEL = 3
TASK_BOUND = 160
ROLE_BOUND = 128
HOST_BOUND = 253
TEMPLATE = re.compile(r"\{\{.*?\}\}|\{%.*?%\}|\{#.*?#\}", re.S)
UNNAMED = re.compile(r"[^A-Za-z0-9._-]")


def stop_record(task, host, kind):
    """The stopped record of a task's failure, or None when nothing names it."""
    raw = task._ds.get("name") if isinstance(getattr(task, "_ds", None), dict) else None
    name = TEMPLATE.sub("...", str(raw or task.action or ""))
    name = "".join(c if " " <= c <= "~" else "?" for c in name)[:TASK_BOUND].strip()
    host = str(host)
    if not name or not host or len(host) > HOST_BOUND or UNNAMED.search(host):
        return None
    record = {"host": host, "kind": kind, "phase": "stopped", "task": name}
    role = getattr(task, "_role", None)
    role = role.get_name() if role is not None else ""
    if role and len(role) <= ROLE_BOUND and not re.search(r"[^A-Za-z0-9._]", role):
        record["role"] = role
    return record


def recovered(task):
    """Whether the task sits in the body of a block whose rescue may recover it."""
    child, parent = task, getattr(task, "_parent", None)
    while parent is not None:
        body = getattr(parent, "block", None) or []
        if getattr(parent, "rescue", None) and any(getattr(entry, "_uuid", None) == child._uuid for entry in body):
            return True
        child, parent = parent, getattr(parent, "_parent", None)
    return False


def censored(result):
    """Whether no_log censors this result: by its task, or by the result itself."""
    return bool(result.task.no_log) or bool(result.result.get("_ansible_no_log")) or "censored" in result.result


class CallbackModule(DefaultCallbackModule):
    """ansible.builtin.default, printing nothing a censored result raised."""

    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "stdout"
    CALLBACK_NAME = "bootwright.core.censored"

    _stopped = False

    def _stop(self, result, kind):
        if self._stopped or os.environ.get("BOOTWRIGHT_STOP_RECORD") != "1" or recovered(result._task):
            return
        self._stopped = True
        try:
            record = stop_record(result._task, result._host.get_name(), kind)
            if record is not None:
                os.write(CHANNEL, canonical(record) + b"\n")
        except Exception:  # pylint: disable=broad-exception-caught
            return

    def v2_runner_on_failed(self, result, ignore_errors=False):
        super().v2_runner_on_failed(result, ignore_errors=ignore_errors)
        if not ignore_errors:
            self._stop(result, "failed")

    def v2_runner_on_unreachable(self, result):
        super().v2_runner_on_unreachable(result)
        if not result._task.ignore_unreachable:
            self._stop(result, "unreachable")

    def _handle_warnings_and_exception(self, result):
        if censored(result):
            for key in RAISED:
                result.result.pop(key, None)
            return
        super()._handle_warnings_and_exception(result)

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
extends_documentation_fragment:
  - ansible.builtin.default_callback
  - ansible.builtin.result_format_callback
requirements:
  - set as the stdout callback in configuration
author:
  - Bootwright contributors (@crmarques)
"""

from ansible.plugins.callback.default import CallbackModule as DefaultCallbackModule

# What a result carries that the default prints outside its censored body.
RAISED = ("exception", "warnings", "deprecations")


def censored(result):
    """Whether no_log censors this result: by its task, or by the result itself."""
    return bool(result.task.no_log) or bool(result.result.get("_ansible_no_log")) or "censored" in result.result


class CallbackModule(DefaultCallbackModule):
    """ansible.builtin.default, printing nothing a censored result raised."""

    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "stdout"
    CALLBACK_NAME = "bootwright.core.censored"

    def _handle_warnings_and_exception(self, result):
        if censored(result):
            for key in RAISED:
                result.result.pop(key, None)
            return
        super()._handle_warnings_and_exception(result)

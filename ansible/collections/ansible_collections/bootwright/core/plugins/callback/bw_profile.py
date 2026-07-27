from __future__ import annotations

import json
import os
import time

from ansible.plugins.callback import CallbackBase

DOCUMENTATION = """
    name: bw_profile
    type: aggregate
    short_description: Append one JSON line per task result with its duration
    description:
      - Appends one JSON object per task result to C(task-profile.jsonl) inside
        the directory named by the C(BOOTWRIGHT_ANSIBLE_ARTIFACTS) environment
        variable, which Bootwright sets to the per-task artifacts directory.
      - Writes nothing when that variable is unset or empty.
      - Every hook swallows its own errors so profiling can never change the
        outcome of a play.
    requirements:
      - enable in configuration
"""

ARTIFACTS_ENV = "BOOTWRIGHT_ANSIBLE_ARTIFACTS"

PROFILE_NAME = "task-profile.jsonl"


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "bootwright.core.bw_profile"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._stream = None
        self._stream_failed = False
        self._play = ""
        self._task_name = ""
        self._task_role = ""
        self._task_started = 0.0
        self._host_started = {}

    def v2_playbook_on_play_start(self, *args, **kwargs):
        try:
            play = args[0] if args else kwargs.get("play")
            self._play = _text(play.get_name()) if play is not None else ""
        except Exception:
            self._play = ""

    def v2_playbook_on_task_start(self, *args, **kwargs):
        self._start_task(args[0] if args else kwargs.get("task"))

    def v2_playbook_on_handler_task_start(self, *args, **kwargs):
        self._start_task(args[0] if args else kwargs.get("task"))

    def v2_runner_on_start(self, *args, **kwargs):
        try:
            host = args[0] if args else kwargs.get("host")
            task = args[1] if len(args) > 1 else kwargs.get("task")
            self._host_started[_result_key(host, task)] = time.monotonic()
        except Exception:
            pass

    def v2_runner_on_ok(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "ok")

    def v2_runner_on_failed(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "failed")

    def v2_runner_on_skipped(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "skipped")

    def v2_runner_on_unreachable(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "unreachable")

    def v2_playbook_on_stats(self, *args, **kwargs):
        self._close()

    def _start_task(self, task):
        try:
            self._task_started = time.monotonic()
            self._host_started = {}
            if task is None:
                self._task_name = ""
                self._task_role = ""
                return
            self._task_name = _text(task.get_name())
            role = getattr(task, "_role", None)
            self._task_role = _text(role.get_name()) if role is not None else ""
        except Exception:
            self._task_name = ""
            self._task_role = ""

    def _record(self, result, status):
        try:
            if result is None:
                return
            host = getattr(result, "_host", None)
            task = getattr(result, "_task", None)
            started = self._host_started.pop(_result_key(host, task), self._task_started)
            duration = time.monotonic() - started
            if duration < 0:
                duration = 0.0
            self._write(
                {
                    "play": self._play,
                    "role": self._task_role,
                    "task": self._task_name,
                    "host": _text(host.get_name()) if host is not None else "",
                    "status": status,
                    "duration_seconds": round(duration, 3),
                }
            )
        except Exception:
            pass

    def _write(self, record):
        stream = self._open()
        if stream is None:
            return
        try:
            stream.write(json.dumps(record, sort_keys=True) + "\n")
            stream.flush()
        except Exception:
            self._stream_failed = True
            self._close()

    def _open(self):
        if self._stream is not None or self._stream_failed:
            return self._stream
        try:
            artifacts = os.environ.get(ARTIFACTS_ENV, "").strip()
            if not artifacts or not os.path.isdir(artifacts):
                self._stream_failed = True
                return None
            path = os.path.join(artifacts, PROFILE_NAME)
            handle = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
            self._stream = os.fdopen(handle, "a", encoding="utf-8")
        except Exception:
            self._stream_failed = True
            self._stream = None
        return self._stream

    def _close(self):
        stream = self._stream
        self._stream = None
        if stream is None:
            return
        try:
            stream.close()
        except Exception:
            pass


def _result_key(host, task):
    host_name = ""
    task_uuid = ""
    try:
        if host is not None:
            host_name = _text(host.get_name())
        if task is not None:
            task_uuid = _text(getattr(task, "_uuid", ""))
    except Exception:
        pass
    return host_name + "\x00" + task_uuid


def _text(value):
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    return str(value)

from __future__ import annotations

import json
import os

from ansible.plugins.callback import CallbackBase


DOCUMENTATION = """
    name: bw_run_result
    type: aggregate
    short_description: Persist machine-readable playbook result events
    requirements:
      - enable in configuration
"""

ARTIFACTS_ENV = "BOOTWRIGHT_ANSIBLE_ARTIFACTS"
RESULT_NAME = "run-result.jsonl"


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "bootwright.core.bw_run_result"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._stream = None
        self._stream_failed = False

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

    def _record(self, result, status):
        try:
            if result is None:
                return
            host = getattr(result, "_host", None)
            self._write(
                {
                    "host": str(host.get_name()) if host is not None else "",
                    "status": status,
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
            path = os.path.join(artifacts, RESULT_NAME)
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

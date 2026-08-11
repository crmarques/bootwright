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
DESTROY_COMPLETION_TASK = "Record Bootwright destroy completion"
DESTROY_REACHABILITY_PROBE_TASKS = {
    "Probe node reachability before deregistration",
    "Probe node reachability before revoking node access",
}


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "bootwright.core.bw_run_result"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._stream = None
        self._stream_failed = False
        self._host_counts = {}

    def v2_runner_on_ok(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "ok")

    def v2_runner_on_failed(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "failed")

    def v2_runner_on_skipped(self, *args, **kwargs):
        self._record(args[0] if args else kwargs.get("result"), "skipped")

    def v2_runner_on_unreachable(self, *args, **kwargs):
        result = args[0] if args else kwargs.get("result")
        task = getattr(result, "_task", None)
        task_name = str(getattr(task, "name", "")).strip()
        status = (
            "probe-unreachable"
            if task_name in DESTROY_REACHABILITY_PROBE_TASKS
            else "unreachable"
        )
        self._record(result, status)

    def v2_playbook_on_stats(self, *args, **kwargs):
        stats = args[0] if args else kwargs.get("stats")
        processed = getattr(stats, "processed", None)
        if not isinstance(processed, dict):
            self._stream_failed = True
        processed_hosts = sorted(str(host).strip() for host in (processed or {}))
        if any(not host for host in processed_hosts):
            self._stream_failed = True
        for host in processed_hosts:
            self._host_counts.setdefault(
                host,
                {
                    "ok": 0,
                    "failed": 0,
                    "skipped": 0,
                    "unreachable": 0,
                    "probeUnreachable": 0,
                    "completed": 0,
                },
            )
        if not self._stream_failed:
            self._write(
                {
                    "schemaVersion": 1,
                    "status": "terminal",
                    "processedHosts": processed_hosts,
                    "hosts": self._host_counts,
                }
            )
        self._close()

    def _record(self, result, status):
        try:
            if result is None:
                self._stream_failed = True
                return
            host = getattr(result, "_host", None)
            host_name = str(host.get_name()).strip() if host is not None else ""
            if not host_name:
                self._stream_failed = True
                return
            counts = self._host_counts.setdefault(
                host_name,
                {
                    "ok": 0,
                    "failed": 0,
                    "skipped": 0,
                    "unreachable": 0,
                    "probeUnreachable": 0,
                    "completed": 0,
                },
            )
            counts[status] += 1
            record = {"host": host_name, "status": status}
            task = getattr(result, "_task", None)
            task_name = str(getattr(task, "name", "")).strip()
            if task_name == DESTROY_COMPLETION_TASK:
                if status != "ok":
                    self._stream_failed = True
                    self._close()
                    return
                counts["completed"] += 1
                record["completion"] = True
            self._write(record)
        except Exception:
            self._stream_failed = True
            self._close()

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
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
            if hasattr(os, "O_NOFOLLOW"):
                flags |= os.O_NOFOLLOW
            handle = os.open(path, flags, 0o600)
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

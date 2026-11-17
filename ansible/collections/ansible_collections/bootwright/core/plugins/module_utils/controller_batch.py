"""Prepare a frozen request's target tools together.

Every tool is checked, in input order, before any is acquired. Each missing
source is then acquired by a fixed worker interpreter of its own, started
through the bounded process runner from a thread of this process: the worker's
main thread holds the SIGALRM that bounds its acquisition, and the runner kills
it GRACE seconds after that deadline. At most ACQUISITIONS run at once and
PER_HOST from one host. Nothing here forks the Ansible worker, a multi-threaded
process. Projection and evidence stay in this process, one entry per tool in
input order."""

from __future__ import annotations

import json
import os
import pathlib
import queue
import sys
import threading
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    AcquisitionRefused,
    Bundle,
    Refused,
    Routes,
    Unreleased,
    acquire,
    admit,
    evidence,
    lent_anchor,
    missing,
    refused,
)

ACQUISITIONS = 4
PER_HOST = 2
MAX_TOOLS = 128
MAX_OUTCOME = 4096
MAX_REQUEST = 1 << 20
MAX_MESSAGE = 200
# Seconds the runner allows a worker past its own deadline, for interpreter
# start and the digest that proves a linked source.
GRACE = 30
# The directory that holds ansible_collections, which the worker imports from.
COLLECTIONS = str(pathlib.Path(__file__).resolve().parents[5])
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
# The fixed worker: it reads one request from standard input, takes the import
# path the request names and writes one bounded outcome.
WORKER = """import json, sys
request = json.loads(sys.stdin.buffer.read(%d))
sys.path[:0] = request["path"]
from ansible_collections.bootwright.core.plugins.module_utils import controller_batch
sys.stdout.buffer.write(controller_batch.worker(request))
""" % MAX_REQUEST


class ToolRefused(Exception):
    """error refused the tool whose source identity is source."""

    def __init__(self, error, source):
        super().__init__(source)
        self.error = error
        self.source = source


def source_of(tool):
    source = tool.get("source") if isinstance(tool, dict) else None
    identity = source.get("id") if isinstance(source, dict) else None
    return identity if isinstance(identity, str) else None


def attributed(tool, step, *arguments):
    """step's answer, or its refusal named with tool's source."""
    try:
        return step(*arguments)
    except Exception as error:
        raise ToolRefused(error, source_of(tool)) from None


def bounded(error):
    return str(error.args[0] if error.args else "")[:MAX_MESSAGE]


def settled(work):
    """work's outcome as bounded data a worker hands back."""
    try:
        work()
        return {"refused": None}
    except AcquisitionRefused as error:
        return {
            "refused": "acquisition",
            "message": bounded(error),
            "reason": error.reason,
            "source": error.source,
            "detail": error.detail,
        }
    except Unreleased as error:
        return {"refused": "unreleased", "message": bounded(error)}
    except Refused as error:
        return {"refused": "refused", "message": bounded(error)}
    except BaseException as error:  # pylint: disable=broad-exception-caught
        return {"refused": "other", "message": type(error).__name__[:MAX_MESSAGE]}


def revived(data):
    """The refusal a worker handed back, or None for success; an outcome that
    is not one this module writes is itself a refusal."""
    try:
        outcome = json.loads(data)
        kind, message = outcome["refused"], outcome.get("message", "")
    except (ValueError, TypeError, KeyError, AttributeError):
        return Refused("acquisition process")
    if kind is None:
        return None
    if kind == "acquisition":
        return AcquisitionRefused(
            message,
            reason=outcome.get("reason"),
            source=outcome.get("source"),
            detail=outcome.get("detail") or "",
        )
    if kind == "unreleased":
        return Unreleased(message)
    return Refused(message if kind == "refused" else "acquisition process")


def fetch(request):
    """Acquire the request's one source into its bundle, whose capacity the
    caller already reserved."""
    bundle = Bundle(request["bundle"])
    try:
        with Routes(lent_anchor(request["trust"])) as routes:
            acquire(bundle, request["source"], request["egress"], request["seconds"], routes, reserved=True)
    finally:
        bundle.close()


def worker(request):
    """The outcome bytes of the worker's one acquisition."""
    return json.dumps(settled(lambda: fetch(request))).encode("utf-8")[:MAX_OUTCOME]


def spawn(request, seconds, cancel):
    """Run the fixed worker on request through the bounded process runner and
    return the refusal it handed back, or None. A worker past its deadline is
    the deadline refusal its own alarm raises."""
    data = json.dumps(request).encode("utf-8")
    if len(data) > MAX_REQUEST:
        return Refused("acquisition process")
    try:
        code, output, _errors = bounded_process.run(
            [sys.executable, "-I", "-B", "-c", WORKER], data, seconds + GRACE, MAX_OUTCOME,
            environment=dict(ENVIRONMENT), cwd="/", capture_stderr=False, cancel=cancel,
        )
    except bounded_process.Failure as failure:
        if failure.kind == "timeout":
            return refused(Refused("acquisition deadline"), "acquisition deadline")
        return Refused("acquisition process")
    if code != 0:
        return Refused("acquisition process")
    return revived(output)


def acquisition(location, source, egress, seconds, trust):
    request = {
        "path": [COLLECTIONS] + [entry for entry in sys.path if isinstance(entry, str) and os.path.isabs(entry)],
        "bundle": location,
        "source": source,
        "egress": egress,
        "seconds": seconds,
        "trust": trust,
    }

    def work(cancel):
        return spawn(request, seconds, cancel)

    return work


def outcome_of(work, cancel):
    try:
        return work(cancel)
    except Refused as error:
        return error
    except Exception:  # pylint: disable=broad-exception-caught
        return Refused("acquisition process")


def startable(jobs, waiting, running, held, limit, per_host):
    """The waiting jobs to start now, in input order."""
    chosen = []
    for index in waiting:
        if len(running) + len(chosen) >= limit:
            break
        host = jobs[index][0]
        if held.get(host, 0) + sum(1 for other in chosen if jobs[other][0] == host) < per_host:
            chosen.append(index)
    return chosen


def acquire_all(jobs, limit=ACQUISITIONS, per_host=PER_HOST):
    """Run every (host, work) job on a thread of its own, at most limit at once
    and per_host from one host, starting them in input order and none after a
    refusal; work takes a cancel event and returns its refusal or None. Answer
    the first refusal in input order, or None. Every started job is joined;
    when this is interrupted, the cancel event stops their workers first."""
    refusals = [None] * len(jobs)
    finished, cancel = queue.Queue(), threading.Event()
    waiting, running, held = list(range(len(jobs))), {}, {}

    def run(index):
        finished.put((index, outcome_of(jobs[index][1], cancel)))

    try:
        while running or waiting and all(refusal is None for refusal in refusals):
            if all(refusal is None for refusal in refusals):
                for index in startable(jobs, waiting, running, held, limit, per_host):
                    waiting.remove(index)
                    held[jobs[index][0]] = held.get(jobs[index][0], 0) + 1
                    running[index] = threading.Thread(target=run, args=(index,), daemon=True)
                    running[index].start()
            index, outcome = finished.get()
            running.pop(index).join()
            held[jobs[index][0]] -= 1
            refusals[index] = outcome
    finally:
        if running:
            cancel.set()
        for thread in running.values():
            thread.join()
    return next(((index, refusal) for index, refusal in enumerate(refusals) if refusal is not None), None)


def host_of(source):
    return (urllib.parse.urlsplit(source["url"]).hostname or "").lower()


def acquire_missing(bundle, location, tools, absent, egress, deadlines, trust):
    """Reserve the capacity of every absent source at once, name the sources
    directory before any worker links into it, then acquire them together."""
    bundle.reserve([("sources/" + tools[index]["source"]["id"], tools[index]["source"]["bytes"]) for index in absent])
    bundle.writable()
    descriptor, _leaf = bundle.parent("sources/" + tools[absent[0]]["source"]["id"], create=True)
    os.close(descriptor)
    jobs = [
        (host_of(tools[index]["source"]), acquisition(location, tools[index]["source"], egress, deadlines[index], trust))
        for index in absent
    ]
    found = acquire_all(jobs)
    if found is not None:
        index, error = found
        raise ToolRefused(error, source_of(tools[absent[index]]))


def prepare_tools(location, tools, egress, deadlines, inspect_only=False, trust=None):
    """Prepare every frozen tool, each under its own acquisition deadline, and
    return whether any changed and one evidence entry per tool in input order.
    A refusal is a ToolRefused naming the source of the tool it refused."""
    trust = trust or {"host": "", "file": ""}
    lent_anchor(trust)
    if (
        not isinstance(tools, list)
        or not 1 <= len(tools) <= MAX_TOOLS
        or not isinstance(deadlines, list)
        or len(deadlines) != len(tools)
    ):
        raise Refused("request")
    for tool, deadline in zip(tools, deadlines):
        attributed(tool, admit, tool, egress, deadline)
    if len({source_of(tool) for tool in tools}) != len(tools):
        raise Refused("duplicate source")
    bundle = Bundle(location)
    try:
        absent = [index for index, tool in enumerate(tools) if attributed(tool, missing, bundle, tool, inspect_only)]
        if absent:
            acquire_missing(bundle, location, tools, absent, egress, deadlines, trust)
        results = [attributed(tool, evidence, bundle, tool, inspect_only, index in absent) for index, tool in enumerate(tools)]
    finally:
        bundle.close()
    return {
        "changed": any(result["changed"] for result in results),
        "evidence": [result["evidence"] for result in results],
    }

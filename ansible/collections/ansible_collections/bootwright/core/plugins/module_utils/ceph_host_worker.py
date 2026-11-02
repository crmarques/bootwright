"""Separate fixed launcher for the long host image call, over the native supervisor.

Only the ``images`` phase, which pulls the frozen digest and then each frozen
auxiliary digest within their bounded attempts, runs under this larger remote
ceiling. Every other host and cluster phase, the runtime profile's contained
probes included, keeps the ordinary native launcher and its smaller ceiling; a
payload never changes which launcher or source closure runs.
"""

from __future__ import annotations

import sys
import time
import types

REMOTE_LIMIT = 4285
PHASE = "images"
# The worker's deadline signal comes this long before the supervisor's own deadline, so an interrupted batch stops its
# pulls and leaves their brokers (the runtime's PULL_CANCEL_SECONDS) and returns its timeout while the worker still runs.
CANCEL_RESERVE = 40
# A call starts no pull with less than the runtime's PULL_MINIMUM plus PULL_RESERVE left, so a smaller budget has no
# broker to clean up and keeps its whole signal time.
PULL_FLOOR = 180


def restricted(native):
    """The loaded closure, admitting only the long phase this launcher owns."""
    def dispatch(envelope):
        if not isinstance(envelope, dict) or envelope.get("phase") != PHASE:
            raise native.Refusal("request")
        return native.dispatch(envelope)
    return types.SimpleNamespace(Refusal=native.Refusal, dispatch=dispatch)


def supervised(supervisor, started, budget, load):
    """The supervisor stops the worker at the budget; the worker's own signal comes the cancel reserve earlier."""
    reserve = CANCEL_RESERVE if budget >= PULL_FLOOR else 0

    def work():
        return supervisor["work"](started, budget, loader=load, reserve=reserve)
    return supervisor["supervise"](work, budget=budget, grace=0, started=started)


def main(supervisor):
    supervisor["REMOTE_LIMIT"] = REMOTE_LIMIT
    started = time.monotonic()
    try:
        budget = supervisor["execution_budget"](sys.argv[1:])

        def load(sources):
            return restricted(supervisor["load"](sources))
        output = supervised(supervisor, started, budget, load)
    except Exception:
        output = {"ok": False, "reason": "native-unknown"}
    try:
        supervisor["publish_result"](output)
    except (OSError, ValueError):
        pass


def launcher(supervisor_source, source):
    """Only fixed launcher code enters argv; the native module bundle uses protected stdin."""
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_worker

    return (ceph_native_worker.launcher(supervisor_source)
            + "f={'__name__':'fixed_ceph_host'};exec(compile(" + repr(source) + ",'<fixed-ceph-host>','exec'),f);f['main'](s)")

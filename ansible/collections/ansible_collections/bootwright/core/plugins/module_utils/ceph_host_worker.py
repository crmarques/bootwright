"""Separate fixed launcher for the long host image call, over the native supervisor.

Only the ``profile`` phase, which pulls the frozen digest within its bounded
attempts and then proves the image's runtime in contained probes, runs under
this larger remote ceiling. Every other host and cluster phase keeps the
ordinary native launcher and its smaller ceiling; a payload never changes
which launcher or source closure runs.
"""

from __future__ import annotations

import sys
import time
import types

REMOTE_LIMIT = 4285
PHASE = "profile"


def restricted(native):
    """The loaded closure, admitting only the long phase this launcher owns."""
    def dispatch(envelope):
        if not isinstance(envelope, dict) or envelope.get("phase") != PHASE:
            raise native.Refusal("request")
        return native.dispatch(envelope)
    return types.SimpleNamespace(Refusal=native.Refusal, dispatch=dispatch)


def main(supervisor):
    supervisor["REMOTE_LIMIT"] = REMOTE_LIMIT
    started = time.monotonic()
    try:
        budget = supervisor["execution_budget"](sys.argv[1:])

        def load(sources):
            return restricted(supervisor["load"](sources))

        def work():
            return supervisor["work"](started, budget, loader=load)
        output = supervisor["supervise"](work, budget=budget, grace=0, started=started)
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

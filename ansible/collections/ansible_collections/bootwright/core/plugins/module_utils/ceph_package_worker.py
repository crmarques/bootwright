"""Short fixed launcher for the shipped package helper bundle on owned guests.

The larger helper sources travel on protected stdin, avoiding SSH's argument
size limit. Only these code-owned module names are installed in the isolated
interpreter; neither request fields nor ambient Python paths select code.
"""

from __future__ import annotations

import json
import os
import signal
import sys
import time
import types

PREFIX = "ansible_collections.bootwright.core.plugins.module_utils"
MODULES = ("native_diagnostics", "canonical_json", "bounded_process", "ceph_native_worker",
           "ceph_tunnel", "ceph_tunnel_staging", "ceph_tunnel_custody", "ceph_native_observe", "ceph_native",
           "native_resolution", "native_signatures", "native_apply", "ceph_package_sources", "ceph_crypto_policy", "ceph_packages")
# The closed reasons a package failure names; any other refusal is packages.
REASONS = ("fips-policy", "packages", "package-artifact", "package-drift", "package-host", "package-journal",
           "package-license", "package-limit", "package-platform",
           "package-timeout", "package-resolution", "package-signature", "package-transaction")
LIMIT = 2 << 20
REMOTE_LIMIT = 1920
RESULT_LIMIT = 8192
INTERRUPTS = (signal.SIGTERM, signal.SIGHUP, signal.SIGINT)


def load(sources):
    if not isinstance(sources, dict) or set(sources) != set(MODULES):
        raise ValueError("fixed package bundle")
    parent = None
    parts = PREFIX.split(".")
    for index, part in enumerate(parts):
        name = ".".join(parts[:index + 1])
        module = types.ModuleType(name)
        module.__path__ = []
        sys.modules[name] = module
        if parent is not None:
            setattr(parent, part, module)
        parent = module
    for name in MODULES:
        source = sources[name]
        if not isinstance(source, str) or not 0 < len(source.encode()) <= 1 << 20:
            raise ValueError("fixed package source")
        qualified = PREFIX + "." + name
        module = types.ModuleType(qualified)
        module.__file__ = "<bootwright-fixed-" + name + ">"
        module.__package__ = PREFIX
        sys.modules[qualified] = module
        setattr(parent, name, module)
        exec(compile(source, module.__file__, "exec"), module.__dict__)
    return sys.modules[PREFIX + ".ceph_packages"]


def work():
    output = {"ok": False, "reason": "packages"}
    quiet = os.open(os.devnull, os.O_WRONLY | os.O_CLOEXEC)
    try:
        data = sys.stdin.buffer.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise ValueError("package payload")
        payload = json.loads(data)
        if not isinstance(payload, dict) or set(payload) != {"sources", "payload"}:
            raise ValueError("package envelope")
        # Vendor scriptlets and package-manager output are never public evidence.
        sys.stdout.flush()
        os.dup2(quiet, 1)
        os.dup2(quiet, 2)
        worker = load(payload["sources"])
        result = worker.entrypoint(payload["payload"])
        output = {"ok": True, "result": result}
    except Exception as error:
        output = failure(error)
    finally:
        sys.stdout.flush()
        os.close(quiet)
    return output


def failure(error):
    """The closed reason and, once the shipped helpers are loaded, the code site; never a value."""
    refused = isinstance(error, ValueError) and (type(error).__name__.endswith("Refusal") or type(error).__name__ == "NativeFailure")
    token = error.args[0] if refused and len(error.args) == 1 else None
    output = {"ok": False, "reason": token if token in REASONS else "packages"}
    helper = sys.modules.get(PREFIX + ".ceph_native_worker")
    try:
        cause = helper.cause(error) if helper is not None else None
    except Exception:
        cause = None
    if cause is not None:
        output["cause"] = cause
    diagnostics = sys.modules.get(PREFIX + ".native_diagnostics")
    native_error = getattr(error, "native_error", None)
    if diagnostics is not None and diagnostics.valid(native_error):
        output["nativeError"] = native_error
    return output


def stop(child, grace):
    """Bound termination of the mutation worker and inherited scriptlets."""
    previous = {sig: signal.signal(sig, signal.SIG_IGN) for sig in INTERRUPTS}
    try:
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(child, sig)
            except ProcessLookupError:
                # The child can be interrupted before establishing its session.
                try:
                    os.kill(child, sig)
                except ProcessLookupError:
                    pass
            if sig == signal.SIGTERM:
                time.sleep(grace)
        # Keep the unreaped child PID reserved until its whole group was killed.
        # Do not block waiting for an uninterruptible kernel task.
        os.waitpid(child, os.WNOHANG)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def supervise(worker, budget=REMOTE_LIMIT, grace=1):
    """A remote ceiling applies even when the controller disconnects.

    DNF and its mutating scriptlets inherit the dedicated worker process group.
    Separately isolated, read-only RPM header probes have their own 30s ceiling;
    the 1980s transport allowance includes that bound and termination grace.
    Immutable decisions remain available after an interrupted transaction.
    """
    # An SSH disconnect must not kill an unsupervised child in the fork/handler
    # window. The child establishes its session and dispositions before release.
    blocked = signal.pthread_sigmask(signal.SIG_BLOCK, INTERRUPTS)
    incoming = outgoing = None
    try:
        incoming, outgoing = os.pipe2(os.O_CLOEXEC)
        child = os.fork()
    except Exception:
        for descriptor in (incoming, outgoing):
            if descriptor is not None:
                os.close(descriptor)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        raise
    if child == 0:
        os.close(incoming)
        try:
            os.setsid()
            for sig in INTERRUPTS:
                signal.signal(sig, signal.SIG_DFL)
            signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
            result = json.dumps(worker(), sort_keys=True, separators=(",", ":")).encode()
            if len(result) <= RESULT_LIMIT:
                os.write(outgoing, result)
        finally:
            os._exit(0)
    os.close(outgoing)
    previous = {}

    def interrupted(signum, frame):
        raise InterruptedError("fixed package worker interrupted")

    try:
        for sig in INTERRUPTS:
            previous[sig] = signal.signal(sig, interrupted)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        deadline = time.monotonic() + budget
        while time.monotonic() < deadline:
            finished, status = os.waitpid(child, os.WNOHANG)
            if finished == child:
                os.set_blocking(incoming, False)
                data = os.read(incoming, RESULT_LIMIT + 1)
                if os.waitstatus_to_exitcode(status) == 0 and 0 < len(data) <= RESULT_LIMIT:
                    result = json.loads(data)
                    if isinstance(result, dict) and result.get("ok") in (True, False):
                        return result
                return {"ok": False, "reason": "packages"}
            time.sleep(min(0.05, max(0, deadline - time.monotonic())))
        stop(child, grace)
        return {"ok": False, "reason": "package-timeout"}
    except InterruptedError:
        stop(child, grace)
        return {"ok": False, "reason": "package-timeout"}
    finally:
        signal.pthread_sigmask(signal.SIG_BLOCK, INTERRUPTS)
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        os.close(incoming)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)


def main():
    try:
        output = supervise(work)
    except Exception:
        output = {"ok": False, "reason": "packages"}
    os.write(1, json.dumps(output, sort_keys=True, separators=(",", ":")).encode())


if __name__ == "__main__":
    main()

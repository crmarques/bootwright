"""Fixed native helper loader and a remote aggregate process-group ceiling.

The dedicated worker and its commands share one owned process group. The parent
drains a bounded result and reserves the worker PID until that group is stopped,
including when the worker itself finished before an inherited descendant. HUP,
TERM and INT terminate that group. A lost SSH channel which delivers no signal
is bounded by the aggregate deadline; immediate disconnect termination requires
separate provider/session qualification and is not claimed here.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import json
import os
import re
import selectors
import signal
import sys
import time
import types

if globals().get("__package__"):
    from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics

PREFIX = "ansible_collections.bootwright.core.plugins.module_utils"
MODULES = ("native_diagnostics", "native_specializations", "ceph_tunnel", "ceph_tunnel_staging", "ceph_tunnel_custody", "ceph_native_observe", "ceph_native",
           "ceph_native_resources", "ceph_native_nfs", "ceph_native_observability", "ceph_native_units", "ceph_native_object", "ceph_host_route_probe",
           "ceph_pull_broker", "ceph_host_runtime")
MAX_INPUT = 2 << 20
MAX_SOURCE = 1 << 20
MAX_RESULT = 1 << 20
REMOTE_LIMIT = 640
INTERRUPTS = (signal.SIGTERM, signal.SIGHUP, signal.SIGINT)
FAILURE = {"ok": False, "reason": "native-unknown"}
REFUSAL_CAUSE = re.compile(r"[a-z][a-z0-9-]{0,63}\Z")
EXCEPTION_CAUSE = re.compile(r"([A-Z][A-Za-z0-9_]{0,39})@([a-z][a-z0-9_]{0,39})\.([A-Za-z_][A-Za-z0-9_]{0,39}):([0-9]{1,6})\Z")
SITED_REFUSAL_CAUSE = re.compile(r"([a-z][a-z0-9-]{0,39})@([a-z][a-z0-9_]{0,39})\.([A-Za-z_][A-Za-z0-9_]{0,39}):([0-9]{1,6})\Z")
FRAMES = ("module", "lambda", "genexpr", "listcomp", "dictcomp", "setcomp")
SOURCE = re.compile(r"(?:<bootwright-fixed-|.*/bootwright/core/plugins/(?:module_utils|modules)/)([a-z][a-z0-9_]{0,63})(?:>|\.py)\Z")
LAUNCHED = {"<fixed-ceph-supervisor>": "ceph_native_worker"}


class RemoteDeadline(Exception):
    """The controller's smaller execution allowance elapsed without disclosure."""


def launcher(supervisor_source):
    """Bind the shared validator before forking in a collection-free interpreter."""
    import inspect

    diagnostics_source = inspect.getsource(native_diagnostics)
    return ("import types;d=types.ModuleType('fixed_native_diagnostics');exec(compile(" + repr(diagnostics_source)
            + ",'<fixed-native-diagnostics>','exec'),d.__dict__);"
            "s={'__name__':'fixed_ceph_supervisor','native_diagnostics':d};exec(compile("
            + repr(supervisor_source) + ",'<fixed-ceph-supervisor>','exec'),s);")


def require(condition):
    if not condition:
        raise ValueError("fixed native worker")


def load(sources):
    """Only the controller's closed module bundle enters isolated Python."""
    require(isinstance(sources, dict) and set(sources) == set(MODULES))
    require(all(isinstance(source, str) and 0 < len(source.encode()) <= MAX_SOURCE for source in sources.values()))
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
    modules = {}
    for name in MODULES:
        qualified = PREFIX + "." + name
        module = types.ModuleType(qualified)
        module.__file__ = "<bootwright-fixed-" + name + ">"
        module.__package__ = PREFIX
        sys.modules[qualified] = modules[name] = module
        setattr(parent, name, module)
    compiled = {name: compile(sources[name], modules[name].__file__, "exec") for name in MODULES}
    for name in MODULES:
        exec(compiled[name], modules[name].__dict__)
        modules[name]._BOOTWRIGHT_SOURCE = sources[name]
    # Mutating commands must remain in the group the supervisor owns. Their
    # per-command cleanup kills the direct child; this supervisor stops all
    # inherited descendants before allowing a result to escape.
    modules["ceph_native"].COMMAND_SESSION = False
    return modules["ceph_native"]


def cause(error):
    """Name a failure by its closed reason or its code-owned site, never by its values."""
    site = caller = None
    trace = error.__traceback__
    while trace is not None:
        filename = trace.tb_frame.f_code.co_filename
        matched = SOURCE.match(filename)
        module = matched.group(1) if matched else LAUNCHED.get(filename)
        if module:
            function = re.sub(r"[^A-Za-z0-9_]", "", trace.tb_frame.f_code.co_name)[:40] or "frame"
            site = "%s.%s:%d" % (module[:40], function, min(trace.tb_lineno, 999999))
            caller = site if function != "require" else caller
        trace = trace.tb_next
    if isinstance(error, ValueError) and (type(error).__name__.endswith("Refusal") or type(error).__name__ == "NativeFailure") and len(error.args) == 1:
        reason = error.args[0]
        if isinstance(reason, str) and REFUSAL_CAUSE.match(reason):
            sited = "%s@%s" % (reason, caller)
            return sited if caller and SITED_REFUSAL_CAUSE.match(sited) else reason
    name = re.sub(r"[^A-Za-z0-9_]", "", type(error).__name__)[:40]
    value = "%s@%s" % (name if name[:1].isupper() else "Exception", site or "unknown.frame:0")
    return value if EXCEPTION_CAUSE.match(value) else None


def failure(reason, error):
    value = {"ok": False, "reason": reason}
    named = cause(error)
    if named is not None:
        value["cause"] = named
    diagnostics = sys.modules.get(PREFIX + ".native_diagnostics")
    native_error = getattr(error, "native_error", None)
    if diagnostics is not None and diagnostics.valid(native_error):
        value["nativeError"] = native_error
    return value


def valid_cause(value):
    return isinstance(value, str) and any(form.match(value) is not None for form in (REFUSAL_CAUSE, SITED_REFUSAL_CAUSE, EXCEPTION_CAUSE))


def refusal_cause(value):
    return isinstance(value, str) and (REFUSAL_CAUSE.match(value) is not None or SITED_REFUSAL_CAUSE.match(value) is not None)


def attested(value, sources):
    """A remote cause the controller's own shipped sources could have named."""
    if not valid_cause(value) or not isinstance(sources, dict):
        return False
    texts = [text for text in sources.values() if isinstance(text, str)]
    sited = SITED_REFUSAL_CAUSE.match(value)
    if sited is not None:
        token, module, function, line = sited.groups()
        return function != "require" and literal(token, texts) and located(module, function, line, sources)
    matched = EXCEPTION_CAUSE.match(value)
    if matched is None:
        return literal(value, texts)
    name, module, function, line = matched.groups()
    if module == "unknown":
        return function == "frame" and line == "0" and builtin_exception(name)
    declared = builtin_exception(name) or any(re.search(r"^\s*class %s\(" % re.escape(name), text, re.M) for text in texts)
    return declared and located(module, function, line, sources)


def literal(token, texts):
    return any('"%s"' % token in text or "'%s'" % token in text for text in texts)


def located(module, function, line, sources):
    source = sources.get(module)
    if not isinstance(source, str):
        return False
    defined = function in FRAMES or re.search(r"^\s*def %s\(" % re.escape(function), source, re.M) is not None
    return defined and 0 < int(line) <= source.count("\n") + 1


def builtin_exception(name):
    import builtins
    value = getattr(builtins, name, None)
    return isinstance(value, type) and issubclass(value, BaseException)


def result_bytes(value):
    require(isinstance(value, dict) and type(value.get("ok")) is bool)
    if value["ok"]:
        require(set(value) == {"ok", "result"} and isinstance(value["result"], dict))
    else:
        require({"ok", "reason"} <= set(value) <= {"ok", "reason", "cause", "nativeError"}
                and value["reason"] in ("native-unknown", "native-refusal", "native-timeout"))
        require("cause" not in value or valid_cause(value["cause"]))
        if "nativeError" in value:
            require(native_diagnostics.valid(value["nativeError"]))
    data = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()
    require(0 < len(data) <= MAX_RESULT)
    return data


def work(started=None, ceiling=REMOTE_LIMIT, loader=load):
    """Only typed evidence, attested causes and bounded redacted native errors return."""
    started = time.monotonic() if started is None else started
    quiet = os.open(os.devnull, os.O_WRONLY | os.O_CLOEXEC)
    previous_alarm = None

    def elapsed(signum, frame):
        raise RemoteDeadline()

    try:
        require(type(ceiling) in (int, float) and 0 < ceiling <= REMOTE_LIMIT)
        data = sys.stdin.buffer.read(MAX_INPUT + 1)
        require(len(data) <= MAX_INPUT)
        payload = json.loads(data)
        require(isinstance(payload, dict) and set(payload) == {"sources", "payload", "budget"})
        budget = payload["budget"]
        require(type(budget) in (int, float) and 0 < budget <= ceiling)
        remaining = budget - (time.monotonic() - started)
        if remaining <= 0:
            raise RemoteDeadline()
        previous_alarm = signal.signal(signal.SIGALRM, elapsed)
        signal.setitimer(signal.ITIMER_REAL, remaining)
        sys.stdout.flush()
        os.dup2(quiet, 1)
        os.dup2(quiet, 2)
        native = loader(payload["sources"])
        try:
            return {"ok": True, "result": native.dispatch(payload["payload"])}
        except native.Refusal as error:
            return failure("native-refusal", error)
    except RemoteDeadline:
        return {"ok": False, "reason": "native-timeout"}
    except Exception as error:
        return failure("native-unknown", error)
    finally:
        if previous_alarm is not None:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous_alarm)
        sys.stdout.flush()
        os.close(quiet)


def child_work(worker, incoming, outgoing, blocked):
    os.close(incoming)
    try:
        os.setsid()
        for sig in INTERRUPTS:
            signal.signal(sig, signal.SIG_DFL)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        data = result_bytes(worker())
        while data:
            count = os.write(outgoing, data)
            require(count > 0)
            data = data[count:]
    except Exception:
        pass
    finally:
        os._exit(0)


def stop(child, grace):
    """Stop a reserved PID's group before any reap can permit PID reuse."""
    previous = {sig: signal.signal(sig, signal.SIG_IGN) for sig in INTERRUPTS}
    try:
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(child, sig)
            except ProcessLookupError:
                # A pending interruption may precede the child's setsid.
                with contextlib.suppress(ProcessLookupError):
                    os.kill(child, sig)
            if sig == signal.SIGTERM:
                time.sleep(grace)
        # An uninterruptible kernel task cannot block the aggregate ceiling.
        os.waitpid(child, os.WNOHANG)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def child_status(child):
    """Observe completion without reaping the group leader's reserved PID."""
    return os.waitid(os.P_PID, child, os.WEXITED | os.WNOHANG | os.WNOWAIT)


def collect(child, incoming, budget):
    deadline = time.monotonic() + budget
    data = bytearray()
    os.set_blocking(incoming, False)
    with selectors.DefaultSelector() as selector:
        selector.register(incoming, selectors.EVENT_READ)
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            for unused_1, unused_2 in selector.select(min(0.05, max(0, remaining))):
                block = os.read(incoming, 65536)
                if block:
                    data.extend(block)
                    require(len(data) <= MAX_RESULT)
                else:
                    selector.unregister(incoming)
            status = child_status(child)
            if status is not None:
                # The process may exit after writing more than the last read.
                while True:
                    try:
                        block = os.read(incoming, 65536)
                    except BlockingIOError:
                        break
                    if not block:
                        break
                    data.extend(block)
                    require(len(data) <= MAX_RESULT)
                require(status.si_code == os.CLD_EXITED and status.si_status == 0)
                require(data)
                result = json.loads(data)
                result_bytes(result)
                return result
            if not selector.get_map():
                time.sleep(min(0.05, max(0, deadline - time.monotonic())))
    return {"ok": False, "reason": "native-timeout"}


def supervise(worker, budget=REMOTE_LIMIT, grace=1, started=None):
    started = time.monotonic() if started is None else started
    require(type(budget) in (int, float) and 0 < budget <= REMOTE_LIMIT)
    require(type(grace) in (int, float) and 0 <= grace <= 1)
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
        child_work(worker, incoming, outgoing, blocked)
    os.close(outgoing)
    previous = {}

    def interrupted(signum, frame):
        raise InterruptedError("fixed native worker interrupted")

    try:
        for sig in INTERRUPTS:
            previous[sig] = signal.signal(sig, interrupted)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        remaining = budget - (time.monotonic() - started)
        if remaining <= 0:
            return {"ok": False, "reason": "native-timeout"}
        return collect(child, incoming, remaining)
    except InterruptedError:
        return {"ok": False, "reason": "native-timeout"}
    except Exception:
        return dict(FAILURE)
    finally:
        signal.pthread_sigmask(signal.SIG_BLOCK, INTERRUPTS)
        stop(child, grace)
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        os.close(incoming)
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)


def publish_result(output, budget=1):
    """A blocked or disconnected SSH output cannot outlive the remote ceiling."""
    data = memoryview(result_bytes(output))
    deadline = time.monotonic() + budget
    os.set_blocking(1, False)
    with selectors.DefaultSelector() as selector:
        selector.register(1, selectors.EVENT_WRITE)
        while data:
            remaining = deadline - time.monotonic()
            require(remaining > 0)
            for unused_1, unused_2 in selector.select(min(0.05, remaining)):
                try:
                    count = os.write(1, data[:65536])
                except BlockingIOError:
                    continue
                require(count > 0)
                data = data[count:]


def execution_budget(arguments):
    """The fixed controller argv bounds input reads before the envelope exists."""
    require(len(arguments) <= 1)
    if not arguments:
        return REMOTE_LIMIT
    value = arguments[0]
    require(isinstance(value, str) and 0 < len(value) <= 32 and value.isascii())
    budget = float(value)
    require(0 < budget <= REMOTE_LIMIT)
    return budget


def main():
    started = time.monotonic()
    try:
        budget = execution_budget(sys.argv[1:])
        # An inherited command may ignore TERM and continue effects after its
        # leader returns. Native work has no detached background entitlement.
        output = supervise(lambda: work(started, budget), budget=budget, grace=0, started=started)
    except Exception:
        output = dict(FAILURE)
    try:
        publish_result(output)
    except (OSError, ValueError):
        pass


if __name__ == "__main__":
    main()

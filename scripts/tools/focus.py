"""Run the focused inner-loop checks of what this branch changed.

The change is what quick-test compares: the branch's diff from where it left
main, with untracked files. Each check is the narrowest one that still
exercises the change, cheapest first, and the run stops at the first failure.
It is no gate: make check runs every gate once, before integration.
"""

from __future__ import annotations

import ast
import contextlib
import os
from pathlib import Path
import re
import shlex
import signal
import subprocess
import sys
import threading
import time

# The exit status of a narrowed Ansible unit run that failed in parallel and
# passed on the serial rerun, which scripts/tools/ansible_check.py exits with
# (its FLAKY_STATUS) and this driver exits with when no check failed outright.
# Only that step reports it; any other check exiting with it fails.
FLAKY_STATUS = 75
UNITS = ["./scripts/ansible-check", "--suite", "units"]

# The seconds a lane may run before the driver kills it and names the command
# that reproduces the stalled check, since a worker that deadlocks in a fork
# would otherwise hold the lane until interrupted. The slowest lane takes
# about 4 minutes cold.
LANE_TIMEOUT = 900.0

# The seconds the driver keeps reading a killed check's output: a descendant
# that left its process group may hold the output open, and the lane then
# stops reading rather than wait for it.
DRAIN_TIMEOUT = 5.0

# The process group of each running check, so an interrupt or termination of
# the driver kills every check instead of leaving it running, and the flag
# that stops further checks from starting once it has.
_running_lock = threading.Lock()
_running: set[int] = set()
_stopping = threading.Event()

COLLECTION = "ansible/collections/ansible_collections/bootwright/core"
PACKAGE = "ansible_collections.bootwright.core"
UNIT = "tests/unit"

# The paths whose change docs-check covers: guidance and the specs.
DOCS = (".agents/", ".claude/", "docs/", "specs/")

# The Ansible harness's own tests, which no Go test runs; Go tests run the
# other helper tests and quick-test selects them as readers of scripts/. They
# import the harness's locked tools.
HARNESS = ("scripts/tools/ansible_check.py", "scripts/tools/ansible_check_test.py")
HARNESS_TESTS = '"$(./scripts/cache-dir)/ansible-check/venv/bin/python" -I -B scripts/tools/ansible_check_test.py'

# The inputs every Ansible check reads beside the collection: its
# configuration, the harness, and the locks of the tools it runs. A change to
# one runs the lint, syntax and sanity suites over everything, and every unit
# test.
ANSIBLE_INPUTS = (
    "ansible/.ansible-lint",
    "ansible/ansible.cfg",
    "scripts/ansible-check",
    "scripts/tools/ansible-check-floor-interpreter.json",
    "scripts/tools/ansible-check-interpreter.json",
    "scripts/tools/ansible-requirements.txt",
    "scripts/tools/ansible-test-artifacts.json",
    "scripts/tools/ansible_check_bootstrap.py",
    "scripts/tools/ansible_test_prepare.py",
)

# A test that lists a directory and names the plugins tree checks every
# plugin, as the function-length and floor tests do, so it imports none.
LISTS = re.compile(r"\.r?glob\(|os\.walk\(|\.iterdir\(|os\.listdir\(|scandir\(")
PLUGINS = re.compile("[\"']plugins[\"'/]")

# Makes the locked environment every Ansible lane shares before the lanes
# start, as two first runs at once would each try to bootstrap it.
PREPARE = (
    'test -n "${BOOTWRIGHT_ANSIBLE_CHECK_PYTHON:-}" || '
    'test -x "$(./scripts/cache-dir)/ansible-check/venv/bin/python" || '
    "python3 scripts/tools/ansible_check_bootstrap.py"
)


def module_name(relative: str) -> str:
    """Return the dotted import name of a collection-relative Python file."""
    parts = relative[: -len(".py")].split("/")
    if parts[-1] == "__init__":
        parts.pop()
    return ".".join([PACKAGE] + parts)


def imported_names(source: str) -> set[str]:
    """Return every collection name a Python source imports.

    A from-import also names each imported name under its module, since that
    name may be a submodule; a name that is not one matches no file.
    """
    names = set()
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return names
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and not node.level:
            names.add(node.module)
            names.update(node.module + "." + alias.name for alias in node.names)
    return {name for name in names if name.startswith(PACKAGE + ".")}


def unit_tests(collection: Path, changed: list[str]) -> list[str] | None:
    """Return the unit test files that exercise the changed collection files.

    Changed is collection-relative. A changed test file selects itself; a
    changed Python file selects every test that imports it, directly or
    through other collection files, every test whose text names its stem as
    a word and, for a file outside the tests, every test that lists the
    plugins tree. Any other changed file, a role, a playbook, a fixture or a
    conftest.py among them, selects the whole tree, returned as None. A
    collection README or changelog selects nothing.
    """
    sources = {
        str(path.relative_to(collection)): path.read_text(encoding="utf-8")
        for path in sorted(collection.rglob("*.py"))
        if "__pycache__" not in path.parts
    }
    importers: dict[str, set[str]] = {}
    for relative, source in sources.items():
        for name in imported_names(source):
            importers.setdefault(name, set()).add(relative)
    tests = {
        relative
        for relative in sources
        if relative.startswith(UNIT + "/") and Path(relative).name.startswith("test_")
    }
    walkers = {
        test for test in tests if LISTS.search(sources[test]) and PLUGINS.search(sources[test])
    }
    selected = set()
    reached: set[str] = set()
    pending = []
    for relative in changed:
        if relative in ("README.md", "CHANGELOG.rst"):
            continue
        if not relative.endswith(".py") or Path(relative).name == "conftest.py":
            return None
        if relative in tests:
            selected.add(relative)
        elif not relative.startswith(UNIT + "/"):
            selected.update(walkers)
        stem = re.compile(r"\b" + re.escape(Path(relative).stem) + r"\b")
        selected.update(test for test in tests if stem.search(sources[test]))
        pending.append(relative)
    while pending:
        relative = pending.pop()
        if relative in reached:
            continue
        reached.add(relative)
        for importer in importers.get(module_name(relative), ()):
            if importer in tests:
                selected.add(importer)
            pending.append(importer)
    return sorted(test for test in selected if (collection / test).is_file())


def changed_files(root: Path) -> tuple[str, list[str]]:
    """Return the base quick-test compares against and the paths changed since.

    With no base to compare against, there is no change to focus on.
    """
    base = subprocess.run(
        [str(root / "scripts/quick-test"), "--base"],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if not base:
        raise SystemExit("focus: no merge base with main; run make check")
    paths = set()
    for listing in (
        ["diff", "-z", "--name-only", "--no-renames", base, "--"],
        ["ls-files", "-z", "--others", "--exclude-standard"],
    ):
        # -z lists each path as Git stores it, never quoted.
        listed = subprocess.run(
            ["git", *listing], cwd=root, check=True, capture_output=True, text=True
        ).stdout
        paths.update(path for path in listed.split("\0") if path)
    return base, sorted(paths)


# The pinned toolchain's gofmt, failing when it lists a file to format.
GOFMT = 'set -e; listed=$("$(./scripts/go env GOROOT)/bin/gofmt" -l "$@"); test -z "$listed" || { printf "%s\\n" "$listed"; exit 1; }'

# Vet type-checks every affected package and its tests without running them,
# so a dependent the change breaks fails here although focus tests none.
VET = "set -e; packages=$(./scripts/quick-test --list); ./scripts/go vet $packages"


def plan(root: Path, base: str, changed: list[str]) -> list[tuple[str, str, list[str]]]:
    """Return each check the change needs, as its lane, name and command line.

    Each lane runs its checks in order and stops at its first failure; the
    lanes run at once, since none reads what another writes.
    """
    present = [path for path in changed if (root / path).is_file()]
    steps = [("text", "whitespace", ["git", "diff", "--check", base, "--"])]
    if any(path.endswith(".md") or path.startswith(DOCS) for path in changed):
        steps.append(("text", "docs-check", ["make", "-s", "docs-check"]))
    go = [path for path in present if path.endswith(".go")]
    if go:
        steps.append(("go", "gofmt", ["sh", "-c", GOFMT, "gofmt"] + go))
    # A deleted Go file vets too: what imported its declarations may break.
    if any(path.endswith(".go") or path in ("go.mod", "go.sum") for path in changed):
        steps.append(("go", "vet of every affected package", ["sh", "-c", VET]))
    steps.append(("go", "test of changed packages", ["./scripts/quick-test", "--focus"]))
    steps.extend(ansible_steps(root, changed, present))
    if any(path in HARNESS for path in changed):
        steps.append(("units", "harness tests", ["sh", "-c", HARNESS_TESTS]))
    return steps


def ansible_steps(root: Path, changed: list[str], present: list[str]) -> list[tuple[str, str, list[str]]]:
    """Return the narrowed Ansible checks of the changed collection files.

    The unit tests take a lane of their own beside lint, syntax and sanity,
    as each run works in its own check area.
    """
    prefix = COLLECTION + "/"
    check = ["./scripts/ansible-check", "--suite"]
    if any(path in ANSIBLE_INPUTS for path in changed):
        return [
            ("ansible", "lint, unnarrowed", check + ["lint"]),
            ("ansible", "syntax, unnarrowed", check + ["syntax"]),
            ("ansible", "sanity, unnarrowed", check + ["sanity"]),
            ("units", "whole tree", check + ["units", prefix + UNIT]),
        ]
    inside = [path[len(prefix):] for path in changed if path.startswith(prefix)]
    if not inside:
        return []
    files = [path for path in present if path.startswith(prefix)]
    steps = []
    yaml = [path for path in files if path.endswith((".yml", ".yaml"))]
    if yaml:
        steps.append(("ansible", "lint", check + ["lint"] + yaml))
    playbooks = [path for path in yaml if path.startswith(prefix + "playbooks/")]
    if playbooks:
        steps.append(("ansible", "syntax", check + ["syntax"] + playbooks))
    if files:
        steps.append(("ansible", "sanity", check + ["sanity"] + files))
    tests = unit_tests(root / COLLECTION, inside)
    if tests is None:
        steps.append(("units", "whole tree", check + ["units", prefix + UNIT]))
    elif tests:
        steps.append(("units", "selected tests", check + ["units"] + [prefix + test for test in tests]))
    return steps


def run_command(root: Path, command: list[str], timeout: float) -> tuple[subprocess.CompletedProcess, bool]:
    """Run a command, killing its whole process group when the timeout ends.

    The group is killed, not just the command, as a shell or a worker it
    started would otherwise hold the output open; an interrupt kills it too
    before it propagates. Once stop_checks ran, no command starts. It returns
    the completed process and whether the timeout ended it.
    """
    with _running_lock:
        if _stopping.is_set():
            return subprocess.CompletedProcess(command, 130, "focus: interrupted before this check started\n"), False
        try:
            # A failing test may print bytes that are not UTF-8.
            process = subprocess.Popen(
                command, cwd=root, stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                text=True, errors="replace", start_new_session=True,
            )
        except OSError as failure:
            return subprocess.CompletedProcess(command, 127, f"{failure}\n"), False
        _running.add(process.pid)
    try:
        try:
            output, _ = process.communicate(timeout=max(timeout, 0.0))
            return subprocess.CompletedProcess(command, process.returncode, output), False
        except subprocess.TimeoutExpired:
            kill_group(process.pid)
            return subprocess.CompletedProcess(command, process.wait(), drain(process)), True
        except BaseException:
            kill_group(process.pid)
            process.wait()
            if process.stdout:
                process.stdout.close()
            raise
    finally:
        with _running_lock:
            _running.discard(process.pid)


def kill_group(group: int) -> None:
    """Kill a check's process group, which may already have ended."""
    with contextlib.suppress(ProcessLookupError, PermissionError):
        os.killpg(group, signal.SIGKILL)


def drain(process: subprocess.Popen) -> str:
    """Read a killed check's remaining output for at most DRAIN_TIMEOUT."""
    try:
        output, _ = process.communicate(timeout=DRAIN_TIMEOUT)
        return output or ""
    except subprocess.TimeoutExpired as expired:
        if process.stdout:
            process.stdout.close()
        partial = expired.output or b""
        if isinstance(partial, bytes):
            partial = partial.decode(errors="replace")
        return partial + "focus: a process outside the check's group holds its output open; stopped reading\n"


def stop_checks() -> None:
    """Kill the process group of every running check and start no other."""
    with _running_lock:
        _stopping.set()
        groups = list(_running)
    for group in groups:
        kill_group(group)


def run_lane(
    root: Path,
    steps: list[tuple[str, str, list[str]]],
    report: threading.Lock,
    failed: list[str],
    flaky: list[str] | None = None,
    timeout: float = LANE_TIMEOUT,
) -> None:
    """Run one lane's checks in order, printing each as it ends.

    A passing check prints its last line; a failing one prints its whole
    output and the command that reproduces it, and ends the lane. A check that
    exits FLAKY_STATUS passed only on its serial rerun: it is no pass, ends the
    lane and is named in flaky; only the Ansible units step reports it. The
    lane as a whole gets timeout seconds; the
    check running when they end is killed, printed with its reproduce command
    and counted as failed.
    """
    deadline = time.monotonic() + timeout
    flaky = [] if flaky is None else flaky
    for lane, name, command in steps:
        started = time.monotonic()
        completed, expired = run_command(root, command, deadline - started)
        elapsed = time.monotonic() - started
        lines = [line for line in completed.stdout.splitlines() if line.strip()]
        with report:
            if expired:
                print(completed.stdout, end="", flush=True)
                print(
                    f"focus: [{lane}] {name} TIMED OUT after {timeout:.0f}s of its lane; "
                    f"rerun: {shlex.join(command)}",
                    flush=True,
                )
                failed.append(f"{lane} {name} timed out")
                return
            if completed.returncode == 0:
                last = f": {lines[-1].strip()}" if lines else ""
                print(f"focus: [{lane}] {name} passed in {elapsed:.1f}s{last}", flush=True)
                continue
            print(completed.stdout, end="", flush=True)
            if completed.returncode == FLAKY_STATUS and command[: len(UNITS)] == UNITS:
                print(
                    f"focus: [{lane}] {name} FLAKY in {elapsed:.1f}s: it failed in parallel and "
                    f"passed only on the serial rerun, so it is no pass; rerun: {shlex.join(command)}",
                    flush=True,
                )
                flaky.append(f"{lane} {name}")
                return
            print(f"focus: [{lane}] {name} FAILED in {elapsed:.1f}s; rerun: {shlex.join(command)}", flush=True)
            failed.append(f"{lane} {name}")
            return


def main() -> int:
    if sys.argv[1:]:
        raise SystemExit("Usage: make focus")
    root = Path(__file__).resolve().parents[2]
    base, changed = changed_files(root)
    print(f"focus: {len(changed)} changed paths since {base[:12]}", flush=True)
    started = time.monotonic()
    lanes: dict[str, list[tuple[str, str, list[str]]]] = {}
    for step in plan(root, base, changed):
        lanes.setdefault(step[0], []).append(step)
    report = threading.Lock()
    failed: list[str] = []
    flaky: list[str] = []
    finished: list[str] = []

    def lane(name: str, steps: list[tuple[str, str, list[str]]]) -> None:
        run_lane(root, steps, report, failed, flaky)
        finished.append(name)

    def terminate(signum: int, frame: object) -> None:
        raise KeyboardInterrupt

    _stopping.clear()
    threads: list[threading.Thread] = []
    previous = signal.signal(signal.SIGTERM, terminate)
    try:
        if "ansible" in lanes or "units" in lanes:
            run_lane(root, [("ansible", "environment", ["sh", "-c", PREPARE])], report, failed, flaky)
        threads = [
            threading.Thread(target=lane, args=(name, steps), daemon=True)
            for name, steps in lanes.items()
            if not failed and not flaky
        ]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()
    except KeyboardInterrupt:
        stop_checks()
        for thread in threads:
            thread.join(DRAIN_TIMEOUT + 5)
        print(f"focus: interrupted after {time.monotonic() - started:.1f}s; killed the running checks", flush=True)
        return 130
    finally:
        stop_checks()
        signal.signal(signal.SIGTERM, previous)
    # A lane that ended without finishing raised; that is no pass.
    failed.extend(f"{name} lane ended early" for name in lanes if threads and name not in finished)
    elapsed = time.monotonic() - started
    if failed:
        print(f"focus: failed in {elapsed:.1f}s: {', '.join(failed)}", flush=True)
        return 1
    if flaky:
        print(f"focus: FLAKY in {elapsed:.1f}s, not a pass: {', '.join(flaky)}", flush=True)
        return FLAKY_STATUS
    print(
        f"focus: passed in {elapsed:.1f}s; a focused run is no gate: "
        "run make check before integration",
        flush=True,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""Run repository Ansible checks with the exact reviewed Python tool closure."""

import concurrent.futures
from dataclasses import dataclass
from importlib.metadata import PackageNotFoundError, distributions, version
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import traceback
from typing import Callable, Optional, TextIO
from xml.etree import ElementTree


def cache_root(root: Path) -> Path:
    """Return the shared check cache that scripts/cache-dir names."""
    completed = subprocess.run(
        [str(root / "scripts/cache-dir")], check=True, capture_output=True, text=True
    )
    return Path(completed.stdout.strip())


SUITES = ("syntax", "lint", "sanity", "units", "integration")

# The area is every check process's HOME, and ansible-core renders its
# ANSIBLE_HOME defaults in Jinja's native environment, which parses the
# rendered path as Python. After a hyphen, a random part such as "3if" starts a
# number that runs into a keyword, and every process then prints a
# SyntaxWarning. After an underscore the random part, drawn from letters,
# digits and underscores, only continues a name.
AREA_PREFIX = "bootwright-ansible-check_"

# The variable through which the gate tells its own tests which area it runs in.
AREA_VARIABLE = "BOOTWRIGHT_ANSIBLE_CHECK_AREA"


def check_area() -> tempfile.TemporaryDirectory:
    """Return the temporary area one check run works in."""
    return tempfile.TemporaryDirectory(prefix=AREA_PREFIX)


# ansible-test accepts no word for "auto"; zero is the only count it turns into
# pytest -n auto, so the gate names the mode through this constant.
UNITS_WORKERS_AUTO = "0"


def units_arguments(*options: str) -> list[str]:
    """Return the ansible-test arguments of a gate unit run on xdist workers.

    The plain pytest loop, in a lane beside the gate's ansible-test runs, is
    the in-process serial run.
    """
    return ["units", "--num-workers", UNITS_WORKERS_AUTO, *options]


# The suites a list of paths can narrow; integration targets are not paths.
NARROWED = ("syntax", "lint", "sanity", "units")


def selected_suites(arguments: list[str]) -> tuple[set[str], list[str]]:
    """Return the suites to run and the paths that narrow them.

    A partial run serves an agent's inner loop: one suite, or one suite over
    the named files and directories of the collection. The complete gate is
    the argument-free form that CI and make check run.
    """
    if not arguments:
        return set(SUITES), []
    if len(arguments) >= 2 and arguments[0] == "--suite" and arguments[1] in SUITES:
        if len(arguments) == 2 or arguments[1] in NARROWED:
            return {arguments[1]}, arguments[2:]
    raise SystemExit(
        "Usage: scripts/ansible-check [--suite "
        + "|".join(SUITES)
        + "] or [--suite "
        + "|".join(NARROWED)
        + " PATH...]"
    )


def collection_targets(collection: Path, paths: list[str]) -> list[str]:
    """Return each path relative to the collection, refusing one outside it.

    A relative path is read from the working directory, as a shell names it.
    """
    targets = []
    for path in paths:
        resolved = Path(path).resolve()
        if not resolved.exists():
            raise SystemExit(f"{path} does not exist")
        if not resolved.is_relative_to(collection.resolve()):
            raise SystemExit(f"{path} is outside the collection {collection}")
        targets.append(str(resolved.relative_to(collection.resolve())))
    return targets


# The status a narrowed unit run exits with when tests failed in parallel and
# passed on the serial rerun: not a pass, and not a failure the rerun confirms.
# scripts/tools/focus.py reports it as FLAKY.
FLAKY_STATUS = 75


def narrowed_units(
    pytest: list[str], collection: Path, targets: list[str], environment: dict
) -> int:
    """Run the plain unit loop over the targets and return its status.

    One file runs serially, as the gate runs it. More, or a directory, run
    across pytest-xdist workers, and any test that fails there runs again
    serially, which gives its verdict: a worker's threads take
    process-directed signals and make every fork a test performs one that may
    deadlock, so a test that signals its own process rather than its own
    thread fails there alone. A run that fails in parallel and passes on the
    serial rerun is flaky: it prints FLAKY and exits FLAKY_STATUS, not 0.
    """
    collections = collection.parents[2]
    paths = [str(collection.relative_to(collections) / target) for target in targets]
    if len(targets) == 1 and (collection / targets[0]).is_file():
        return subprocess.run(
            pytest + paths, cwd=collections, env=environment, check=False
        ).returncode
    parallel = subprocess.run(
        pytest + ["-n", "auto"] + paths, cwd=collections, env=environment, check=False
    )
    if parallel.returncode != 1:
        return parallel.returncode
    print("Rerunning the tests that failed in parallel serially; that run decides.")
    serial = subprocess.run(
        pytest + ["--last-failed", "--last-failed-no-failures", "none"] + paths,
        cwd=collections,
        env=environment,
        check=False,
    ).returncode
    if serial == 0:
        print(
            "FLAKY: tests failed in parallel and passed on the serial rerun; "
            "this is no pass."
        )
        return FLAKY_STATUS
    return serial


def floor_interpreter(root: Path, fixtures: Path, area: Path) -> tuple[str, Path]:
    """Extract the verified managed-host floor interpreter into the check area.

    Modules and module utilities run under the managed host's own Python, and
    the lock pins the oldest release a managed host may run. The archive is
    verified and extracted from the same bytes on every run, so no extracted
    tree outlives its verification.
    """
    lock = json.loads(
        (root / "scripts/tools/ansible-check-floor-interpreter.json").read_text()
    )
    try:
        data = (fixtures / lock["filename"]).read_bytes()
    except OSError as failure:
        raise SystemExit(
            "The pinned floor interpreter is missing; run "
            "scripts/tools/ansible_test_prepare.py first."
        ) from failure
    if len(data) != lock["bytes"] or hashlib.sha256(data).hexdigest() != lock["sha256"]:
        raise SystemExit("The floor interpreter differs from its lock")
    runtime = area / "floor"
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as bundle:
        # "data" refuses absolute paths, parent traversal, links that escape
        # the destination, and device or setuid members.
        bundle.extractall(runtime, filter="data")
    interpreter = runtime / lock["member"]
    found = subprocess.run(
        [
            str(interpreter),
            "-I",
            "-c",
            "import sys; print('.'.join(map(str, sys.version_info[:3])))",
        ],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if found != lock["version"]:
        raise SystemExit(
            f"The floor interpreter is {found}, not the pinned {lock['version']}"
        )
    return found.rsplit(".", 1)[0], interpreter


def require_imported(collection: Path, minor: str) -> None:
    """Refuse a floor import test that ansible-test skipped instead of ran.

    ansible-test reports a target Python it cannot build a virtual environment
    for as a skipped test and still exits zero, so only its report proves the
    import ran.
    """
    report = (
        collection / f"tests/output/junit/ansible-test-sanity-import-python-{minor}.xml"
    )
    try:
        cases = list(ElementTree.parse(report).getroot().iter("testcase"))
    except (OSError, ElementTree.ParseError) as failure:
        raise SystemExit(
            f"The import test on Python {minor} left no report"
        ) from failure
    if len(cases) != 1 or len(cases[0]) != 0:
        raise SystemExit(f"The import test on Python {minor} did not run and pass")


def require_unit_tested(collection: Path, minor: str) -> None:
    """Refuse a floor unit run that ansible-test skipped instead of ran.

    ansible-test reports a target Python whose tests it skips with a warning
    and exits zero, so only the modules and module_utils reports prove that
    their tests ran, and each must hold a passed test and no failed one.
    """
    for context in ("modules", "module_utils"):
        report = collection / f"tests/output/junit/python{minor}-{context}-units.xml"
        try:
            cases = list(ElementTree.parse(report).getroot().iter("testcase"))
        except (OSError, ElementTree.ParseError) as failure:
            raise SystemExit(
                f"The {context} unit tests on Python {minor} left no report"
            ) from failure
        outcomes = [{child.tag for child in case} for case in cases]
        passed = [
            tags for tags in outcomes if not tags & {"skipped", "failure", "error"}
        ]
        failed = [tags for tags in outcomes if tags & {"failure", "error"}]
        if not passed or failed:
            raise SystemExit(
                f"The {context} unit tests on Python {minor} did not run and pass"
            )


# A lane is one run of checks that reads nothing another lane writes: it has
# its own HOME and temporary directory and, where it runs ansible-test or
# pytest, its own copy of the collection, whose reports and virtual
# environments ansible-test writes beside the content it tests. The lanes of a
# run go beside one another and each runs its own checks in order. The plain
# loop is one lane: one pytest process, in-process and serial, still the
# gate's only unit run without pytest-xdist's I/O thread.
Lane = Callable[[Optional[TextIO]], Optional[int]]


def lane_environment(environment: dict, lane: Path) -> dict:
    """Return the environment of a lane whose writes stay under the lane."""
    return dict(
        environment,
        HOME=str(lane),
        XDG_CACHE_HOME=str(lane / "cache"),
        TMPDIR=str(lane),
        ANSIBLE_LOCAL_TEMP=str(lane / "ansible-local"),
        ANSIBLE_REMOTE_TEMP=str(lane / "ansible-remote"),
    )


def collection_lane(
    area: Path, name: str, collection: Path, wheelhouse: Path, environment: dict
) -> tuple[Path, dict]:
    """Copy the collection into a new lane; return the copy and its environment."""
    lane = area / name
    test_collection = lane / "collections/ansible_collections/bootwright/core"
    shutil.copytree(
        collection,
        test_collection,
        ignore=shutil.ignore_patterns("__pycache__", ".pytest_cache", "output"),
    )
    pip_cache = lane / ".ansible/test/cache"
    pip_cache.mkdir(parents=True)
    shutil.copyfile(wheelhouse / "get_pip_25_2.py", pip_cache / "get_pip_25_2.py")
    pip_configuration = lane / ".config/pip/pip.conf"
    pip_configuration.parent.mkdir(parents=True)
    pip_configuration.write_text(
        "[global]\nno-index = true\nfind-links = " + str(wheelhouse) + "\n"
    )
    return test_collection, dict(
        lane_environment(environment, lane),
        PIP_NO_INDEX="1",
        PIP_FIND_LINKS=str(wheelhouse),
        ANSIBLE_COLLECTIONS_PATH=str(lane / "collections"),
        ANSIBLE_TEST_CONTENT_ROOT=str(test_collection),
    )


def run(command: list[str], output: Optional[TextIO], **options) -> None:
    """Run one check, writing to the lane's output, and raise when it fails.

    A lane given no output owns the terminal, as a run of one lane does.
    """
    if output is not None:
        options.update(
            stdout=output, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL
        )
    subprocess.run(command, check=True, **options)


def outcome(lane: Lane, output: Optional[TextIO]) -> int:
    """Run a lane and return its status: what its failing check exited with."""
    try:
        status = lane(output)
    except subprocess.CalledProcessError as failure:
        return failure.returncode or 1
    except SystemExit as refusal:
        if refusal.code is None or isinstance(refusal.code, int):
            return refusal.code or 0
        print(refusal.code, file=output or sys.stderr)
        return 1
    except Exception:
        traceback.print_exc(file=output or sys.stderr)
        return 1
    return status or 0


def run_lanes(lanes: list[tuple[str, Lane]], logs: Path) -> int:
    """Run the lanes beside one another and return the run's status.

    Each lane's output is printed whole when it ends, then a line per lane.
    One lane runs alone and owns the terminal, so its status, such as a
    narrowed unit run's FLAKY_STATUS, is the run's. Of several, the first
    lane that failed decides, and none reports FLAKY_STATUS: only a narrowed
    unit run, which is one lane, may.
    """
    if len(lanes) == 1:
        return outcome(lanes[0][1], None)
    logs.mkdir()

    def go(name: str, lane: Lane) -> tuple[int, float]:
        started = time.monotonic()
        with open(logs / (name + ".log"), "w", encoding="utf-8") as output:
            status = outcome(lane, output)
        return status, time.monotonic() - started

    names = [name for name, _ in lanes]
    print("Running the lanes " + ", ".join(names) + " beside one another.", flush=True)
    results = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(lanes)) as pool:
        futures = {pool.submit(go, name, lane): name for name, lane in lanes}
        for future in concurrent.futures.as_completed(futures):
            name = futures[future]
            results[name] = future.result()
            text = (logs / (name + ".log")).read_text(
                encoding="utf-8", errors="replace"
            )
            print(
                f"===== lane {name} =====\n{text}===== end of lane {name} =====",
                flush=True,
            )
    for name in names:
        status, seconds = results[name]
        verdict = "passed" if status == 0 else f"FAILED with status {status}"
        print(f"lane {name}: {verdict} in {seconds:.0f} s")
    failed = [results[name][0] for name in names if results[name][0] != 0]
    if not failed:
        return 0
    return 1 if failed[0] == FLAKY_STATUS else failed[0]


# The syntax lane is never the longest, so it checks a few playbooks at a time
# and leaves the host's cores to the lanes that run time-bounded tests.
SYNTAX_WORKERS = 4


def syntax_lane(
    playbooks: list[Path], command: list[str], root: Path, environment: dict
) -> Lane:
    """Return the lane that syntax checks every playbook, each in its own process."""

    def check(playbook: Path) -> subprocess.CompletedProcess:
        return subprocess.run(
            command + [str(playbook)],
            cwd=root,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            check=False,
        )

    def lane(output: Optional[TextIO]) -> int:
        with concurrent.futures.ThreadPoolExecutor(max_workers=SYNTAX_WORKERS) as pool:
            results = list(pool.map(check, playbooks))
        for result in results:
            (output or sys.stdout).write(result.stdout)
        failed = [result.returncode for result in results if result.returncode != 0]
        return failed[0] if failed else 0

    return lane


def lint_lane(command: list[str], run_: "Run") -> Lane:
    """Return the lane that runs Ansible Lint offline in a HOME of its own."""

    def lane(output: Optional[TextIO]) -> None:
        home = run_.area / "lint"
        home.mkdir()
        run(
            command, output, cwd=run_.root, env=lane_environment(run_.environment, home)
        )

    return lane


@dataclass(frozen=True)
class Run:
    """What one check run reads: its tools, its area and the selected checks."""

    root: Path
    executable: Path
    automation: Path
    collection: Path
    fixtures: Path
    area: Path
    environment: dict
    selected: set[str]
    targets: list[str]


def prepared_wheelhouse(run_: Run) -> Path:
    """Verify the pinned ansible-test artifacts into the area's wheelhouse."""
    wheelhouse = run_.area / "wheels"
    wheelhouse.mkdir()
    artifacts = json.loads(
        (run_.root / "scripts/tools/ansible-test-artifacts.json").read_text()
    )
    for artifact in artifacts:
        try:
            data = (run_.fixtures / artifact["filename"]).read_bytes()
        except OSError as failure:
            raise SystemExit(
                "Pinned Ansible test artifacts are missing; run "
                "scripts/tools/ansible_test_prepare.py first."
            ) from failure
        if (
            len(data) != artifact["bytes"]
            or hashlib.sha256(data).hexdigest() != artifact["sha256"]
        ):
            raise SystemExit("Ansible test artifacts differ from their lock")
        (wheelhouse / artifact["filename"]).write_bytes(data)
    return wheelhouse


def floor_environment(environment: dict, executable: Path, interpreter: Path) -> dict:
    """Return a lane's environment with the floor interpreter on PATH.

    ansible-test finds the target's real interpreter on PATH to build its
    environment.
    """
    return dict(
        environment,
        PATH=os.pathsep.join(
            [str(executable.parent), str(interpreter.parent), "/usr/bin", "/bin"]
        ),
    )


def sanity_lane(run_: Run, wheelhouse: Path, floor: tuple[str, Path]) -> Lane:
    """Return the lane of the sanity tests and the floor's import test.

    The first imports every module on the controller's Python; the second
    imports them again under the managed-host floor, which proves what the
    collection's grammar test cannot, such as a name imported from a library
    module the floor lacks.
    """
    ansible_test = [
        str(run_.executable),
        "-I",
        str(run_.executable.parent / "ansible-test"),
    ]
    minor, interpreter = floor

    def lane(output: Optional[TextIO]) -> None:
        test_collection, environment = collection_lane(
            run_.area, "sanity", run_.collection, wheelhouse, run_.environment
        )
        run(
            ansible_test
            + ["sanity", *run_.targets, "--local", "--python", "3.13", "--color", "no"],
            output,
            cwd=test_collection,
            env=environment,
        )
        run(
            ansible_test
            + [
                "sanity",
                "--test",
                "import",
                "--junit",
                "--controller",
                "origin:python=3.13",
                "--target-python",
                f"{minor}@{interpreter}",
                "--color",
                "no",
            ],
            output,
            cwd=test_collection,
            env=floor_environment(environment, run_.executable, interpreter),
        )
        require_imported(test_collection, minor)

    return lane


def units_lane(run_: Run, wheelhouse: Path, floor: tuple[str, Path]) -> Lane:
    """Return the lane of ansible-test's unit runs, on the controller then the floor.

    The first runs every test on the controller's Python; the second runs the
    modules and module_utils tests again in a virtual environment of the
    managed-host floor, where a library call the floor lacks fails once its
    function runs. The floor is no controller Python, so ansible-test runs no
    controller test there. Both run on pytest-xdist workers, one after the
    other, so their worker pools never share the host.
    """
    ansible_test = [
        str(run_.executable),
        "-I",
        str(run_.executable.parent / "ansible-test"),
    ]
    minor, interpreter = floor

    def lane(output: Optional[TextIO]) -> None:
        test_collection, environment = collection_lane(
            run_.area, "units", run_.collection, wheelhouse, run_.environment
        )
        run(
            ansible_test
            + units_arguments()
            + ["--local", "--python", "3.13", "--color", "no"],
            output,
            cwd=test_collection,
            env=environment,
        )
        run(
            ansible_test
            + units_arguments(
                "--controller",
                "origin:python=3.13",
                "--target-python",
                f"venv/{minor}@{interpreter}",
                "--color",
                "no",
            ),
            output,
            cwd=test_collection,
            env=floor_environment(environment, run_.executable, interpreter),
        )
        require_unit_tested(test_collection, minor)

    return lane


def integration_lane(run_: Run, wheelhouse: Path) -> Lane:
    """Return the lane of the integration targets, one after another."""
    ansible_test = [
        str(run_.executable),
        "-I",
        str(run_.executable.parent / "ansible-test"),
    ]
    names = ["controller_tool", "controller_supervisor"]
    # native_transaction builds an RPM and drives the running RHEL 9 host's
    # package manager, so it is an operator-run harness rather than part of
    # the unitary gate.
    if os.environ.get("BOOTWRIGHT_ANSIBLE_NATIVE_TARGET") == "1":
        names.append("native_transaction")
    # controller_prerequisites drives the shipped controller playbook over
    # the real runner protocol, an operator-run harness like the above.
    if os.environ.get("BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET") == "1":
        names.append("controller_prerequisites")

    def lane(output: Optional[TextIO]) -> None:
        test_collection, environment = collection_lane(
            run_.area, "integration", run_.collection, wheelhouse, run_.environment
        )
        for name in names:
            run(
                ansible_test
                + ["integration", name, "--local", "--python", "3.13", "--color", "no"],
                output,
                cwd=test_collection,
                env=environment,
            )

    return lane


def plain_lane(run_: Run, wheelhouse: Path) -> Lane:
    """Return the lane of the plain pytest loop.

    ansible-test installs the collection loader before it collects a test.
    This runs the same tests again the plain way, pytest alone with the
    collections directory on PYTHONPATH, where the collection's conftest.py
    installs that loader instead. -P and -s keep the working directory and the
    user site off the path, as -I does, while PYTHONPATH is still read.
    Narrowed, it runs the targets alone, and nothing else of the suite runs.
    """
    pytest = [str(run_.executable), "-P", "-s", "-m", "pytest"]

    def lane(output: Optional[TextIO]) -> Optional[int]:
        test_collection, environment = collection_lane(
            run_.area, "plain", run_.collection, wheelhouse, run_.environment
        )
        collections = test_collection.parents[2]
        plain = dict(environment, PYTHONPATH=str(collections))
        if run_.targets:
            return narrowed_units(pytest, test_collection, run_.targets, plain)
        run(
            pytest + [str(test_collection.relative_to(collections) / "tests/unit")],
            output,
            cwd=collections,
            env=plain,
        )
        return None

    return lane


def check_lanes(
    run_: Run, playbooks: list[Path], inventory: Path
) -> list[tuple[str, Lane]]:
    """Return the lanes the selected checks need, in the order they report."""
    lanes: list[tuple[str, Lane]] = []
    if "syntax" in run_.selected:
        command = [
            str(run_.executable),
            "-I",
            "-m",
            "ansible.cli.playbook",
            "--syntax-check",
            "-i",
            str(inventory),
        ]
        lanes.append(
            ("syntax", syntax_lane(playbooks, command, run_.root, run_.environment))
        )
    if "lint" in run_.selected:
        command = [
            str(run_.executable),
            "-I",
            "-m",
            "ansiblelint",
            "--offline",
            "--project-dir",
            str(run_.automation),
        ] + (
            [str(run_.collection / target) for target in run_.targets]
            or [str(run_.automation)]
        )
        lanes.append(("lint", lint_lane(command, run_)))
    if not run_.selected & {"sanity", "units", "integration"}:
        return lanes
    wheelhouse = prepared_wheelhouse(run_)
    whole_units = "units" in run_.selected and not run_.targets
    floor = None
    if "sanity" in run_.selected or whole_units:
        floor = floor_interpreter(run_.root, run_.fixtures, run_.area)
    if "sanity" in run_.selected:
        lanes.append(("sanity", sanity_lane(run_, wheelhouse, floor)))
    if whole_units:
        lanes.append(("units", units_lane(run_, wheelhouse, floor)))
    if "integration" in run_.selected:
        lanes.append(("integration", integration_lane(run_, wheelhouse)))
    if "units" in run_.selected:
        lanes.append(("plain", plain_lane(run_, wheelhouse)))
    return lanes


# A complete run that passes is recorded under a key over every input it reads
# (D223), and a complete run whose key matches a recorded pass reuses it and
# says so instead of running. CI never reads or writes the record, and
# FULL_VARIABLE=1 runs the check whatever is recorded.
FULL_VARIABLE = "BOOTWRIGHT_ANSIBLE_CHECK_FULL"

# A recorded pass is reused for this long at most, so a test whose verdict
# moves with the date, such as one over a certificate that expires, runs again
# within a day.
PASS_LIFETIME = 24 * 60 * 60

# The records kept; older ones are removed when a pass is recorded.
KEPT_PASSES = 50

# What the check reads besides the automation: its own launcher, harness,
# tests and locks.
TOOL_FILES = ("scripts/ansible-check", "scripts/cache-dir")
TOOL_PREFIX = "scripts/tools/ansible"

# Byte-code and pytest's own cache, which no check reads as an input.
UNREAD_DIRECTORIES = ("__pycache__", ".pytest_cache")

# The operator-run integration targets a variable selects; a run with one is
# no complete gate run, so it neither reads nor writes the record.
OPTIONAL_TARGETS = (
    "BOOTWRIGHT_ANSIBLE_NATIVE_TARGET",
    "BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET",
)

# Where the host's tools and their packages live: a check reaches tools such
# as shellcheck or ssh through PATH, so a package change moves the key.
HOST_DIRECTORIES = (
    "/usr/bin",
    "/usr/lib64",
    "/usr/lib",
    "/var/lib/rpm",
    "/var/lib/dpkg",
)


def tree_entries(root: Path, directory: Path) -> list[str]:
    """Return a line per entry under the directory: its path, type and content.

    A file contributes its mode and the SHA-256 of its bytes, a link its
    target, and a directory its mode, so an empty directory or a changed mode
    moves the key as a changed byte does.
    """
    lines = []
    for current, directories, files in os.walk(directory):
        directories[:] = sorted(
            name for name in directories if name not in UNREAD_DIRECTORIES
        )
        here = Path(current)
        for name in directories + sorted(files):
            path = here / name
            relative = path.relative_to(root)
            status = path.lstat()
            if path.is_symlink():
                lines.append(f"link {relative} {os.readlink(path)}")
            elif path.is_dir():
                lines.append(f"directory {relative} {status.st_mode:o}")
            else:
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                lines.append(f"file {relative} {status.st_mode:o} {digest}")
    return lines


def tool_entries(root: Path) -> list[str]:
    """Return a line per tool file the check reads besides the automation."""
    names = [root / name for name in TOOL_FILES]
    names += sorted(
        path
        for path in (root / "scripts/tools").iterdir()
        if str(path.relative_to(root)).startswith(TOOL_PREFIX)
    )
    lines = []
    for path in names:
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        lines.append(f"tool {path.relative_to(root)} {path.stat().st_mode:o} {digest}")
    return lines


def installed_distributions() -> list[str]:
    """Return every distribution installed beside this interpreter, by version."""
    found = {
        (entry.metadata["Name"] or "").lower() + "==" + entry.version
        for entry in distributions()
    }
    return sorted(found)


def host_state() -> list[str]:
    """Return what the host's tool and package directories hold.

    Each entry's name, size and modification time stands for its content, so
    an installed, removed or updated package moves the key without reading
    every binary.
    """
    lines = []
    for directory in HOST_DIRECTORIES:
        try:
            entries = sorted(os.scandir(directory), key=lambda entry: entry.name)
        except OSError:
            lines.append(f"host {directory} absent")
            continue
        for entry in entries:
            if entry.name.endswith("-shm"):
                continue
            try:
                status = entry.stat(follow_symlinks=False)
            except OSError:
                continue
            lines.append(f"host {entry.path} {status.st_size} {status.st_mtime_ns}")
    return lines


def pass_key(
    root: Path, host: list[str], installed: list[str], interpreter: str
) -> str:
    """Return the key of a complete run over the repository at root."""
    digest = hashlib.sha256()
    lines = [f"interpreter {interpreter}"]
    lines += [f"installed {name}" for name in installed]
    lines += tool_entries(root)
    lines += tree_entries(root, root / "ansible")
    lines += host
    for line in lines:
        digest.update(line.encode("utf-8", "surrogateescape") + b"\n")
    return digest.hexdigest()


def recording(environ: dict) -> bool:
    """Report whether a complete run may read and write the pass record."""
    return not environ.get("CI") and environ.get(FULL_VARIABLE) != "1"


def recorded_gate(
    records: Path,
    key: Callable[[], str],
    environ: dict,
    check: Callable[[], int],
    clock: Callable[[], float],
) -> int:
    """Reuse a fresh recorded pass for the inputs, or run the check and record it.

    The key is taken again after a passing run, and the pass is recorded only
    when no input moved while the check ran.
    """
    before = key()
    record = records / before
    if recording(environ) and record.is_file():
        try:
            passed = json.loads(record.read_text(encoding="utf-8"))["passed"]
        except (OSError, ValueError, KeyError, TypeError):
            passed = None
        if type(passed) in (int, float) and 0 <= clock() - passed < PASS_LIFETIME:
            when = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(passed))
            print(
                f"ansible-check: every input matches the complete run that passed at "
                f"{when} (key {before[:16]}); it is reused, not rerun. "
                f"{FULL_VARIABLE}=1 runs it.",
                flush=True,
            )
            return 0
    status = check()
    if status != 0 or not recording(environ):
        return status
    if key() != before:
        print(
            "ansible-check: an input changed while the check ran; no pass is recorded."
        )
        return status
    records.mkdir(parents=True, exist_ok=True)
    partial = records / (before + ".partial")
    partial.write_text(json.dumps({"passed": clock()}), encoding="utf-8")
    os.replace(partial, record)
    kept = sorted(
        (path for path in records.iterdir() if not path.name.endswith(".partial")),
        key=lambda path: path.stat().st_mtime,
        reverse=True,
    )
    for stale in kept[KEPT_PASSES:]:
        stale.unlink(missing_ok=True)
    return status


def main() -> int:
    if sys.version_info[:3] != (3, 13, 16):
        raise SystemExit(
            "Ansible checks require the pinned CPython 3.13.16 interpreter."
        )
    selected, paths = selected_suites(sys.argv[1:])

    root = Path(__file__).resolve().parents[2]
    requirements = root / "scripts/tools/ansible-requirements.txt"
    for line in requirements.read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("#"):
            continue
        requirement = line.split(" ", 1)[0]
        name, expected = requirement.split("==", 1)
        try:
            actual = version(name)
        except PackageNotFoundError:
            actual = None
        if actual != expected:
            raise SystemExit(
                "Ansible check dependencies differ from the reviewed lock. "
                "Install scripts/tools/ansible-requirements.txt into an isolated "
                "CPython 3.13.16 environment with pip --require-hashes, then set "
                "BOOTWRIGHT_ANSIBLE_CHECK_PYTHON to that interpreter."
            )

    executable = Path(sys.executable)
    automation = root / "ansible"
    collection = automation / "collections/ansible_collections/bootwright/core"
    targets = collection_targets(collection, paths)
    fixtures = Path(
        os.environ.get(
            "BOOTWRIGHT_ANSIBLE_TEST_ARTIFACTS",
            str(cache_root(root) / "ansible-test-artifacts"),
        )
    )

    def check() -> int:
        with check_area() as temporary:
            area = Path(temporary)
            inventory = area / "inventory.json"
            inventory.write_text(
                json.dumps(
                    {
                        "all": {
                            "hosts": {
                                "bootwright_controller": {
                                    "ansible_connection": "local",
                                    "ansible_python_interpreter": str(executable),
                                }
                            }
                        }
                    }
                ),
                encoding="utf-8",
            )
            environment = {
                "PATH": os.pathsep.join([str(executable.parent), "/usr/bin", "/bin"]),
                "HOME": str(area),
                "XDG_CACHE_HOME": str(area / "cache"),
                "TMPDIR": str(area),
                "LC_ALL": "C.UTF-8",
                "LANG": "C.UTF-8",
                "ANSIBLE_CONFIG": str(automation / "ansible.cfg"),
                "ANSIBLE_COLLECTIONS_PATH": str(automation / "collections"),
                "ANSIBLE_ROLES_PATH": str(collection / "roles"),
                "ANSIBLE_INVENTORY": str(inventory),
                "ANSIBLE_LOCAL_TEMP": str(area / "ansible-local"),
                "ANSIBLE_REMOTE_TEMP": str(area / "ansible-remote"),
                "ANSIBLE_NOCOLOR": "1",
            }
            playbooks = sorted((collection / "playbooks").rglob("*.yml"))
            if not playbooks:
                raise SystemExit(
                    "No shipped Ansible playbooks were found for syntax checking."
                )
            if targets and "syntax" in selected:
                # Narrowed, syntax checks the shipped playbooks at or under a target.
                playbooks = [
                    playbook
                    for playbook in playbooks
                    if any(
                        playbook.relative_to(collection).is_relative_to(target)
                        for target in targets
                    )
                ]
                if not playbooks:
                    raise SystemExit(
                        "No shipped playbook is at or under " + " ".join(paths)
                    )
            if "units" in selected and not targets:
                # The harness's own tests; -B keeps bytecode out of the checkout.
                # AREA_VARIABLE names this run's area, so they parse the area that
                # every process here takes as its HOME.
                subprocess.run(
                    [
                        str(executable),
                        "-I",
                        "-B",
                        str(root / "scripts/tools/ansible_check_test.py"),
                    ],
                    cwd=root,
                    env={**environment, AREA_VARIABLE: str(area)},
                    check=True,
                )
            run_ = Run(
                root,
                executable,
                automation,
                collection,
                fixtures,
                area,
                environment,
                selected,
                targets,
            )
            return run_lanes(check_lanes(run_, playbooks, inventory), area / "logs")

    optional = any(os.environ.get(name) == "1" for name in OPTIONAL_TARGETS)
    if sys.argv[1:] or optional:
        return check()
    return recorded_gate(
        cache_root(root) / "ansible-check/passes",
        lambda: pass_key(root, host_state(), installed_distributions(), sys.version),
        os.environ,
        check,
        time.time,
    )


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(failure.returncode) from None

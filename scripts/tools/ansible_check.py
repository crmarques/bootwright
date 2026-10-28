"""Run repository Ansible checks with the exact reviewed Python tool closure."""

from importlib.metadata import PackageNotFoundError, version
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
                raise SystemExit("No shipped playbook is at or under " + " ".join(paths))
        if "syntax" in selected:
            for playbook in playbooks:
                subprocess.run(
                    [
                        str(executable),
                        "-I",
                        "-m",
                        "ansible.cli.playbook",
                        "--syntax-check",
                        "-i",
                        str(inventory),
                        str(playbook),
                    ],
                    cwd=root,
                    env=environment,
                    check=True,
                )
        if "lint" in selected:
            subprocess.run(
                [
                    str(executable),
                    "-I",
                    "-m",
                    "ansiblelint",
                    "--offline",
                    "--project-dir",
                    str(automation),
                ]
                + ([str(collection / target) for target in targets] or [str(automation)]),
                cwd=root,
                env=environment,
                check=True,
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
        if not selected & {"sanity", "units", "integration"}:
            return 0
        test_collection = area / "collections/ansible_collections/bootwright/core"
        shutil.copytree(
            collection,
            test_collection,
            ignore=shutil.ignore_patterns("__pycache__", ".pytest_cache", "output"),
        )
        wheelhouse = area / "wheels"
        wheelhouse.mkdir()
        artifacts = json.loads(
            (root / "scripts/tools/ansible-test-artifacts.json").read_text()
        )
        for artifact in artifacts:
            try:
                data = (fixtures / artifact["filename"]).read_bytes()
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
        pip_cache = area / ".ansible/test/cache"
        pip_cache.mkdir(parents=True)
        shutil.copyfile(wheelhouse / "get_pip_25_2.py", pip_cache / "get_pip_25_2.py")
        pip_configuration = area / ".config/pip/pip.conf"
        pip_configuration.parent.mkdir(parents=True)
        pip_configuration.write_text(
            "[global]\nno-index = true\nfind-links = " + str(wheelhouse) + "\n"
        )
        environment.update(
            PIP_NO_INDEX="1",
            PIP_FIND_LINKS=str(wheelhouse),
            ANSIBLE_COLLECTIONS_PATH=str(area / "collections"),
            ANSIBLE_TEST_CONTENT_ROOT=str(test_collection),
        )
        ansible_test = [str(executable), "-I", str(executable.parent / "ansible-test")]
        suites = [
            ["sanity", *targets],
            ["units", "--num-workers", "0"],
            ["integration", "controller_tool"],
            ["integration", "controller_supervisor"],
        ]
        # native_transaction builds an RPM and drives the running RHEL 9 host's
        # package manager, so it is an operator-run harness rather than part of
        # the unitary gate.
        if os.environ.get("BOOTWRIGHT_ANSIBLE_NATIVE_TARGET") == "1":
            suites.append(["integration", "native_transaction"])
        # controller_prerequisites drives the shipped controller playbook over
        # the real runner protocol, an operator-run harness like the above.
        if os.environ.get("BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET") == "1":
            suites.append(["integration", "controller_prerequisites"])
        for arguments in suites:
            # Narrowed, units is the plain loop below alone.
            if arguments[0] not in selected or (targets and arguments[0] == "units"):
                continue
            subprocess.run(
                ansible_test
                + arguments
                + ["--local", "--python", "3.13", "--color", "no"],
                cwd=test_collection,
                env=environment,
                check=True,
            )
        if "units" in selected:
            # ansible-test installs the collection loader before it collects a
            # test. This runs the same tests again the plain way, pytest alone
            # with the collections directory on PYTHONPATH, where the
            # collection's conftest.py installs that loader instead. -P and -s
            # keep the working directory and the user site off the path, as
            # -I does, while PYTHONPATH is still read. Narrowed, it runs the
            # targets alone, and nothing else of the suite runs.
            collections = area / "collections"
            pytest = [str(executable), "-P", "-s", "-m", "pytest"]
            plain = dict(environment, PYTHONPATH=str(collections))
            if targets:
                return narrowed_units(pytest, test_collection, targets, plain)
            subprocess.run(
                pytest + [str(test_collection.relative_to(collections) / "tests/unit")],
                cwd=collections,
                env=plain,
                check=True,
            )
        if not selected & {"sanity", "units"}:
            return 0
        # ansible-test finds the target's real interpreter on PATH to build
        # its environment.
        minor, interpreter = floor_interpreter(root, fixtures, area)
        floor_environment = dict(
            environment,
            PATH=os.pathsep.join(
                [str(executable.parent), str(interpreter.parent), "/usr/bin", "/bin"]
            ),
        )
        if "units" in selected:
            # The units suite above runs every test on the controller's
            # Python; this runs the modules and module_utils tests again in a
            # virtual environment of the managed-host floor, where a library
            # call the floor lacks fails once its function runs. The floor is
            # no controller Python, so ansible-test runs no controller test.
            subprocess.run(
                ansible_test
                + [
                    "units",
                    "--num-workers",
                    "0",
                    "--controller",
                    "origin:python=3.13",
                    "--target-python",
                    f"venv/{minor}@{interpreter}",
                    "--color",
                    "no",
                ],
                cwd=test_collection,
                env=floor_environment,
                check=True,
            )
            require_unit_tested(test_collection, minor)
        if "sanity" in selected:
            # Sanity above imports every module on the controller's Python;
            # this imports them again under the managed-host floor, which
            # proves what the collection's grammar test cannot, such as a
            # name imported from a library module the floor lacks.
            subprocess.run(
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
                cwd=test_collection,
                env=floor_environment,
                check=True,
            )
            require_imported(test_collection, minor)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(failure.returncode) from None

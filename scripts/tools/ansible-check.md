The Ansible gate runs on CPython 3.13.16 and builds that interpreter itself, so
no development host is required to provide it. `./scripts/ansible-check`
bootstraps on first use: it acquires the interpreter pinned by
`ansible-check-interpreter.json`, verifies its byte count and SHA-256, extracts
it under `.cache/ansible-check`, creates an isolated environment, installs
`ansible-requirements.txt` with `--require-hashes --only-binary=:all:`, and
acquires the pinned sanity artifacts. That lock qualifies Linux amd64 wheels
separately from the product runtime.

First use needs network access and takes a few minutes; later runs reuse the
prepared environment. The environment lives in the directory `scripts/cache-dir`
prints, which every worktree of one clone shares, so a new worktree does not
bootstrap again. Run the bootstrap on its own with:

```sh
python3 scripts/tools/ansible_check_bootstrap.py
```

Any host CPython 3.12 or newer can perform the bootstrap; the script takes the
first of `python3`, `python3.14`, `python3.13` and `python3.12` that qualifies.
Set `BOOTWRIGHT_ANSIBLE_BOOTSTRAP_PYTHON` to choose it, or
`BOOTWRIGHT_ANSIBLE_CHECK_PYTHON` to supply a prepared interpreter and skip the
bootstrap entirely. Keep the environment outside tracked content. The gate checks all locked tool
versions, supplies isolated inventory/configuration/cache paths, runs every
shipped playbook through syntax checking, and runs Ansible Lint offline. It
runs all applicable Ansible sanity checks, the sanity import test again under
the managed-host floor interpreter, collection unit tests, the module and
module utility unit tests again under that floor, and the synthetic controller
tool and supervisor integration targets.

Those checks run in lanes beside one another
([D223](../../specs/milestones/backlog.md#d223)): syntax, lint, sanity then the
floor import, the ansible-test units then the floor units, the integration
targets, and the plain loop. A lane runs its own checks in order and reads
nothing another lane writes: each has its own `HOME` and temporary directory
in the check area, and each lane that runs ansible-test or pytest tests its own
copy of the collection. The two ansible-test unit runs share a lane, so their
pytest-xdist worker pools never overlap. Each lane's output is printed whole
when it ends, then a line per lane with its verdict and time; the first lane
that failed gives the exit status, and every lane runs to its end. A run that
selects one lane, as a narrowed suite does, runs it alone on the terminal.

A complete run, the argument-free form with no operator-run target selected,
records its pass under a key over every input it reads: each file, directory
and link under `ansible/` by path, mode and content (byte code and pytest's
cache aside), the launcher, `scripts/cache-dir` and every
`scripts/tools/ansible*` file, the interpreter's version, every distribution
installed beside it, and the names, sizes and times of the entries of the
host's tool and package directories, so a package change moves it. The next
complete run whose key matches a pass of the last 24 hours prints that it
reuses that pass and runs nothing; the key is taken again after a passing run,
and an input that moved meanwhile records nothing. Records live in
`ansible-check/passes` under the directory `scripts/cache-dir` prints, so
every worktree of the clone shares them. Under CI, or with
`BOOTWRIGHT_ANSIBLE_CHECK_FULL=1`, the check runs in full and neither reads nor
writes a record.

The `units` suite first runs the harness's own tests,
`scripts/tools/ansible_check_test.py`, and names its temporary area to them in
`BOOTWRIGHT_ANSIBLE_CHECK_AREA`. They parse that area's path, and other area
paths, as every ansible-core process does, since that area is each process's
`HOME`; run by hand, without the variable, they skip the gate's area. The suite
then runs the unit tests twice, in lanes of their own: under `ansible-test
units`, and as the plain loop the
[collection structure](../../specs/architecture.md#ansible-collection-structure)
supports, pytest alone from the copy's collections directory with that
directory on `PYTHONPATH`; a failure of either fails the suite. By hand, the
plain loop is `pytest ansible_collections/bootwright/core/tests/unit` run from
`ansible/collections` with that directory on `PYTHONPATH`, using the prepared
environment's pytest.

`./scripts/ansible-check --suite syntax|lint|sanity|units|integration` runs
one of those suites for a local inner loop. Paths after the suite narrow it to
files and directories of the collection: syntax checks the shipped playbooks at
or under them, lint lints them, sanity runs every sanity test over them and the
floor import test as usual, and units runs only the plain pytest loop over
them, skipping the harness's own tests, ansible-test and the floor. One test
file runs serially; more, or a directory, run across pytest-xdist workers, and
the tests that fail there run again serially, which decides: when they pass
there the run prints FLAKY and exits 75, which is no pass. A worker's
threads take process-directed signals, so a test that signals itself can fail
only in parallel. Neither form is a substitute for the argument-free gate,
which `make check` and CI run.

One target is excluded by default because it needs the dnf4 backend of a RHEL 9
host: `native_transaction` builds a fixture RPM and drives the package manager
into an isolated installroot. It is an operator-run harness rather than part of
the unitary gate. Select it with `BOOTWRIGHT_ANSIBLE_NATIVE_TARGET=1` on RHEL 9.
`controller_prerequisites` is the other operator-run harness: it runs the
shipped `playbooks/controller/setup.yml` over the real runner protocol with one
`controller-prerequisites-v6` request, whose single synthetic client is
already retained in a private temporary tool area, then replays it. It needs
no root and no network. Select it with
`BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET=1`.
The integration fixture requires local process communication; it never runs a
downloaded binary or changes host packages.

`ansible-test-artifacts.json` pins the separate sanity tool environments,
including Ansible's pip bootstrap, and the floor's unit test environment, whose
pytest 8.4.2 and iniconfig 2.1.0 still run on 3.9. Acquisition verifies every
artifact digest; the check gate verifies them again and installs test requirements only into
temporary virtual environments with an offline wheel source. Set
`BOOTWRIGHT_ANSIBLE_TEST_ARTIFACTS` to use a previously acquired fixture directory.

`ansible-check-floor-interpreter.json` pins the oldest Python a managed host
may run, a CPython 3.9 build from python-build-standalone; the same acquisition
fetches it into the fixture directory. Its minor is the oldest target Python of
the ansible-core that `ansible-requirements.txt` pins: the collection's
`test_remote_python_floor.py` holds its floor to that ansible-core, and
`TestTheFloorLockPinsTheCollectionsRemotePythonFloor` holds this lock to the
collection's floor. On every sanity or units run the gate verifies the archive
and extracts it into the temporary area. Sanity imports each module and module
utility under it, which proves 3.9 grammar and everything that runs at import,
such as a name imported from a library module 3.9 lacks. Units runs the
modules and module_utils unit tests in a virtual environment of it, which
proves each library call those tests reach; a call no test reaches is still
unproved under 3.9. The gate refuses an import test or a unit run that
ansible-test reports as skipped, such as one whose virtual environment it could
not create. A cache prepared before the floor's archive and unit test wheels
were pinned lacks them: run `python3 scripts/tools/ansible_test_prepare.py`
once.

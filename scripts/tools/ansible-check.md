The Ansible gate runs on CPython 3.13.15 and builds that interpreter itself, so
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
shipped playbook through syntax checking, and runs Ansible Lint offline. It then
copies the collection into a temporary area and runs all applicable Ansible
sanity checks, collection unit tests, and the synthetic controller tool and
supervisor integration targets.

`./scripts/ansible-check --suite syntax|lint|sanity|units|integration` runs
one of those suites for a local inner loop. It is never a substitute for the
argument-free gate, which `make check` and CI run.

Two targets are excluded by default because they need a qualified native
backend on the running host: `controller_native` builds a fixture RPM and drives
the package manager, and `controller_prerequisites` runs the shipped setup
playbook and role over the real runner protocol against the host inventory.
Both are operator-run harnesses rather than part of the unitary gate. Select
them with `BOOTWRIGHT_ANSIBLE_NATIVE_TARGET=1` on Fedora or RHEL. The integration fixture requires local process
communication; it never runs a downloaded binary or changes host packages.

`ansible-test-artifacts.json` pins the separate sanity tool environments,
including Ansible's pip bootstrap. Acquisition verifies every artifact digest;
the check gate verifies them again and installs test requirements only into
temporary virtual environments with an offline wheel source. Set
`BOOTWRIGHT_ANSIBLE_TEST_ARTIFACTS` to use a previously acquired fixture directory.

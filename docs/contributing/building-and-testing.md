---
title: Building and testing
description: How to build the Bootwright CLI, run the tests, and pass the guard checks before sending a change.
---

# Building and testing

This page covers how to compile Bootwright, run its tests, and satisfy the
check gates. It is for contributors changing the code; operators do not need it.

## Prerequisites

- **Go** — the toolchain version in [`go.mod`](https://github.com/crmarques/bootwright/blob/main/go.mod).
- **Ansible** — `ansible-playbook` and `ansible-galaxy` on `PATH`. The build
  embeds a pinned Ansible collection bundle into the binary, so even a plain
  `make build` needs Galaxy to install the collections and pack them.
- **Python 3** — runs the bundle-sync and collection-verify scripts.

The full `make check` gate additionally needs `staticcheck`, `shellcheck`,
`yamllint`, and `ansible-lint`; each target prints an install hint if the tool
is missing.

## Build

```sh
make build
```

`make build` syncs the embedded Ansible bundle
(`internal/converge/bundle/ansible_bundle.zip`) and then compiles
`bin/bootwright`. Always run the repo-built binary — `./bin/bootwright` — when
you validate behavior; the binary on your `PATH` lags `main`, and its strict
loader rejects newer schema fields, producing false negatives.

## Test

```sh
make test          # go test ./...
```

## Check gates

Four gates, ordered by breadth. Pick the narrowest one that covers what the
change can break; `make check-full` is the release gate and CI
(`.github/workflows/checks.yml`) runs its stages individually.

| Command | When | What it runs |
| --- | --- | --- |
| `make check-scoped` | A bug fix, or any change that preserves the existing contracts | Only what the diff against `CHECK_BASE` (default `main`) can break: the guardrail stages whose input paths the diff touched, and the changed Go packages plus their reverse-dependency closure. |
| `make check-feature` | A new feature, kind, flag, or field | Everything `check-scoped` selects, plus an unconditional contract floor — `api/v1alpha1`, the validators, the renderers, `internal/cli`, `internal/converge`, and the repo guard tests — because a new contract changes packages the diff never touches. |
| `make check-fast` | You want the whole Go suite without the slow linters | The cheap local guardrails — CLI file-size, Go source visibility, `gofmt`, the stale-term denylist, Containerfile digest pinning, `shellcheck`, and the E2E dependency check — followed by the full `go test ./...`. |
| `make check-full` | Cutting a release, or on request | Everything in `check-fast` plus `go vet`, `staticcheck`, `go mod tidy` verification, the Python unit tests, `ansible-syntax-check`, `ansible-lint`, the workflow-YAML lint, `docs-check`, `go test -race`, and a clean-checkout test run. |

Intent selects the **floor**, never the ceiling. `check-scoped` on a change that
touches `api/v1alpha1` still retests all 34 dependent packages, because the
selector derives the package set from the diff and the import graph — labelling
the prompt "a bug fix" cannot narrow a change that is genuinely wide.

`make docs-check` remains available on its own: `mkdocs build --strict` fails on
a broken link, an unresolved reference, or a page missing from the MkDocs nav.
Install its dependencies once with `python3 -m pip install -r
docs/requirements.txt`, and preview with `mkdocs serve`. `check-scoped` and
`check-feature` run it automatically when the diff touches `docs/` or
`mkdocs.yml`; `check-full` always runs it.

The selector is
[`scripts/select-checks.py`](https://github.com/crmarques/bootwright/blob/main/scripts/select-checks.py),
unit-tested by `scripts/test_select_checks.py`. It fails **open**: an
unresolvable base ref, a non-git tree, a diff over `--max-changed` paths, or an
edit to `Makefile`, `go.mod`, `go.sum`, or the selector itself all widen the run
to every stage and `./...`. Inspect a selection without running it:

```sh
python3 scripts/select-checks.py --tier scoped --base main
```

`check-fast`'s E2E dependency check only verifies that `ansible-playbook` is
reachable. The cases behind it are described in
[`test/README.md`](https://github.com/crmarques/bootwright/blob/main/test/README.md),
listed by `make list-e2e-cases`, and rendered without touching a host by
`make e2e-dry-run CASE=<name>`.

!!! warning "Never let a test spawn the real `ansible-playbook`"
    `workspace.ResolveAnsiblePlaybook()` falls back to whatever
    `ansible-playbook` is on `PATH`, so a test that drives `apply` or `destroy`
    to completion executes real Ansible against unreachable fixture hosts and
    waits out every SSH timeout. The `internal/cli` `TestMain` installs a stub on
    `PATH` for exactly this reason — that one change took
    `TestApplyDestroySafetyMatrix` from 283s to 11.5s. A new test package that
    drives a converge path must do the same.

!!! tip "If `/tmp` is a small tmpfs"
    The full test suite can exhaust a small `/tmp` and fail with a disk-quota
    error. Point Go's temp dir at disk-backed storage first:
    `export GOTMPDIR="$HOME/.cache/gotmp"` (create the directory once).

## The guard-test regime

Bootwright keeps documentation, examples, and layout honest with **guard tests**
under [`internal/repo/checks`](https://github.com/crmarques/bootwright/tree/main/internal/repo/checks),
run as part of `go test` (so `check-fast` covers them). They enforce, among
other things: no prose comments in source (ADR 0006), per-file size floors,
that every documented schema term is current, that the MkDocs nav and the docs
tree agree, that the `.agents/knowledge` and ADR indexes match their
directories, and that every shipped example validates. When a guard fails, it
names the file and the rule; fix the artifact rather than the test unless the
rule itself has genuinely changed.

One example goes further than validating:
`examples/baremetal-redfish-multidc-virtualized-odf-ceph` is the shared
behavioral fixture. Roughly a dozen test files across `internal/cli`,
`internal/converge/workflow`, `internal/render` (with `render/ceph` and
`render/inventory`) and `state/desired` assert on its content by name — its
clusters, its add-ons, its storage cluster — so renaming anything inside it is
an API-surface change to those tests, not a pedagogical edit.

Knowledge and decisions have designated homes, not source comments: incident and
constraint knowledge in
[`/.agents/knowledge`](https://github.com/crmarques/bootwright/tree/main/.agents/knowledge),
decisions in [`/specs/adr`](https://github.com/crmarques/bootwright/tree/main/specs/adr),
and schema semantics in [`/specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md)
and the [concept pages](../concepts/index.md). See
[`AGENTS.md`](https://github.com/crmarques/bootwright/blob/main/AGENTS.md) for
the full workflow.

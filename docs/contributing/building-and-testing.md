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

Two gates, cheapest first:

| Command | What it runs |
| --- | --- |
| `make check-fast` | Syncs the bundle, then the cheap local guardrails — CLI file-size, Go source visibility, `gofmt`, the stale-term denylist, Containerfile digest pinning, `shellcheck`, and the E2E dependency check — followed by the full `go test ./...` suite. This is the gate to run before handing off a change. |
| `make check` | Everything in `check-fast` plus `go vet`, `staticcheck`, `go mod tidy` verification, `go test -race`, a clean-checkout test run, the Python unit tests, `ansible-syntax-check`, `ansible-lint`, and the workflow-YAML lint. Run it when you touch the Ansible collection, dependencies, or concurrency-sensitive code. |

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

Knowledge and decisions have designated homes, not source comments: incident and
constraint knowledge in
[`/.agents/knowledge`](https://github.com/crmarques/bootwright/tree/main/.agents/knowledge),
decisions in [`/specs/adr`](https://github.com/crmarques/bootwright/tree/main/specs/adr),
and schema semantics in [`/specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md)
and the [concept pages](../concepts/index.md). See
[`AGENTS.md`](https://github.com/crmarques/bootwright/blob/main/AGENTS.md) for
the full workflow.

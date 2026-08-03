# ADR 0041: The Gate Runs What the Change Can Break

## Status

Accepted

Replaces the single `make check-fast` gate the
[implementation-validation](../../.agents/skills/implementation-validation/SKILL.md)
skill mandated, and retires `make check` in favour of `make check-full`.

## Context

Every change, however small, ran the same gate: `make check-fast`, which syncs
the embedded Ansible bundle, runs seven static guardrails, and then executes
`go test ./...` across all 58 packages and roughly 2 400 test functions. A
one-line fix in `internal/cli` and a new API kind cost the same wall-clock time,
and that time had grown past the point where an agent could keep a
change-verify-integrate loop tight.

Three causes, in descending order of cost.

**The CLI tests executed real Ansible.** `workspace.ResolveAnsiblePlaybook()`
falls back to whatever `ansible-playbook` is on `PATH` when no explicit
executable is configured. `TestApplyDestroySafetyMatrix` drove 21 cases at the
time, most of them through `apply`/`destroy` with `--yes` and no `--dry-run`
(it has since grown well past that and now spans three verbs), so each one
spawned the real
`ansible-playbook`, which opened SSH connections to fixture hosts that do not
exist and waited out a 30-second connect timeout apiece. Those cases assert only
that no gate-refusal marker appears in the output — they never assert the run's
outcome — so the Ansible execution bought no coverage at all. Measured on an
idle host: `TestApplyDestroySafetyMatrix` took 231.6s, and the whole
`internal/cli` package 408.9s.

**The bundle was regenerated unconditionally.** `sync-bundle` was a `.PHONY`
target, so every `make build`, `make check-fast`, and `make check` re-packed
`internal/converge/bundle/ansible_bundle.zip` and re-ran a bundle guard test
even when nothing under `ansible/` had changed.

**Nothing narrowed the test set.** The Go import graph already says exactly which
packages a change can reach. `internal/cli` and `internal/converge/workflow`
hold 720 of the ~2 400 test functions and have no dependents at all, so a change
confined to either can only break itself; `api/v1alpha1` has 34 dependents and
genuinely needs a wide retest. The gate made no distinction.

A fourth cause is structural rather than mechanical: the same rule was enforced
twice. The `stale-term-check` denylist in the `Makefile` and
`TestCurrentDefinitionDocsUseNewSchemaTerms` in `internal/repo/checks` were two
separately maintained term lists over overlapping path sets, and they had
already drifted — `.agents/knowledge/repo-fitness-guardrails.md` documented that
`specs/adr` was excluded from the scan, while the Makefile variable listed it.

## Decision

**Select the gate by what the change is, and the work by what the diff touches.**

Four targets replace the previous two:

| Target | For |
| --- | --- |
| `check-scoped` | a bug fix, or any change that preserves existing contracts |
| `check-feature` | a new feature, kind, flag, or field |
| `check-fast` | the whole Go suite, unchanged semantics |
| `check-full` | the release gate; `check` is retired |

`scripts/select-checks.py` computes the changed path set against `CHECK_BASE`
(default `main`), classifies each path into a domain, and emits the stages and
Go package patterns that domain requires. Go packages are the changed packages
plus their reverse-dependency closure, taken from `go list` over `.Deps`,
`.TestImports`, and `.XTestImports`.

**Intent raises the floor; it never lowers the ceiling.** `check-feature` adds
the API/validator/render/CLI contract band unconditionally, because a new
contract changes guard tests and safety-matrix cases in packages the diff never
touches. `check-scoped` adds nothing — but it also cannot subtract, because the
package set comes from the import graph, not from the label on the prompt. A
"bug fix" that edits `api/v1alpha1` still retests all 34 dependents.

**The selector fails open.** A non-git tree, an unresolvable base ref, a diff
over `--max-changed`, or an edit to `Makefile`, `go.mod`, `go.sum`, or the
selector itself widens the run to every stage and `./...`. The failure mode of a
selector is under-selection, so every uncertainty resolves toward running more.

**No test may spawn the real `ansible-playbook`.** The `internal/cli` `TestMain`
installs a stub on `PATH`. Playbook validity is covered by
`ansible-syntax-check`, which is what that stage is for.

**The bundle is a file target.** `internal/converge/bundle/ansible_bundle.zip`
depends on the `ansible/` tree, the collections stamp, and the sync script, so an
unchanged tree makes it a no-op.

**A test package shares one extracted bundle.** `BundleDir` consults a cache root
that `SetBundleCacheRootForTest` overrides, and the `internal/cli` `TestMain`
points it at one directory for the whole package. Per-test `RootDir()` isolation
is untouched; only the extracted bundle is shared, and `existingBundleMatches`
still verifies the archive digest, so a shared directory cannot serve stale
content.

**One owner per rule.** `TestCurrentDefinitionDocsUseNewSchemaTerms` holds the
union of both retired-vocabulary term lists over the union of both path sets,
exempting `specs/adr` because an ADR must be able to quote the shape it retired.
The `stale-term-check` Makefile target and `DEFINITION_CHECK_PATHS` are deleted.

## Consequences

Measured on a 16-core host, all comparisons under equal load:

- `TestApplyDestroySafetyMatrix`: 231.6s → 45.3s. The whole `internal/cli`
  package: 408.9s → 159.8s. Both measured on an idle host with the embedded
  bundle present; gate timings on a loaded host vary by more than 2x, so only
  paired measurements taken back to back are meaningful.
- `sync-bundle` on a warm tree: 2.19s → 0.21s, and it no longer rewrites the
  embedded zip.
- A one-file change in `internal/cli` selects 2 packages instead of 58.

The risk the selector introduces is a coupling the import graph cannot see — a
test that reads a repo file by path rather than importing the code it covers.
Those are enumerated as explicit domain-to-package rules (prose readers, fixture
readers, Ansible readers) rather than left to the graph, and the fail-open
behaviour plus `check-full` at release time are the backstop. A new test that
reads authored prose or a fixture must join the matching list in the same change
— the same obligation `implementation-validation` already imposed for its
narrow re-verification list.

`check-full` gains `docs-check`, which `make check` never ran despite CI
requiring it, and drops the redundant `go test -vet=off ./...` pass that
duplicated `check-fast`'s own run.

The two changes compound on `internal/cli`, the repo's slowest package: 408.9s
with real Ansible and per-test bundle extraction, 159.8s with the Ansible stub
alone, and 17.3s with the shared bundle cache as well. `TestApplyDestroySafetyMatrix`
follows the same curve — 231.6s, 45.3s, 12.5s.

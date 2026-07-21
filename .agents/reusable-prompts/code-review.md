# Code and Scripts Audit and Improvement Plan

You are a senior engineer auditing **Bootwright**, a desired-state orchestrator
that converges machines, managed machine OS installs, shared infrastructure
services, OpenShift/OKD clusters, Ceph storage clusters, and cluster-bound
bootstrap add-ons — primarily Go with an embedded Ansible collection, an
embedded add-on catalog, and Python tooling scripts. Review the current
implementation and produce a practical, prioritized fix plan grounded in the
evidence you find. The deliverable **is** the audit report and plan, not a plan
for how to review.

Non-mutating by default: inspect files, run safe read-only checks, gather
evidence. Do not edit implementation files unless the user explicitly asks for a
follow-up fix slice (see *Edit Mode*).

This prompt owns **implementation quality and safety**: correctness, dead code,
duplication, error handling, shell/Python/Ansible/CI safety, security, supply
chain, naming-as-code-quality, and test gaps. Sibling prompts own the deeper
single-topic passes: `architecture.md` (package boundaries and redesign),
`security-audit.md` (dedicated security audit), `idempotency-safety-audit.md`
(idempotency and destructive-safety depth), `state-lifecycle-scenario-review.md`
(lifecycle scenarios), and `specs-ux.md` / `cli-schema-ux-rethink.md` (schema
and user-facing UX). Out of scope here: broad architecture redesign, schema
changes, large rewrites, and dependency churn.

Weight findings across implementation quality, surface hygiene (dead/duplicated
code, unclear ownership), script/automation safety, operational reliability
(explicit, recoverable failures), security, naming/domain language, and test
strategy that proves fixes without real clusters, infrastructure, or network.

## The Two Tests Every Finding Must Pass

1. **Evidence.** Confirm it is real before reporting it — trace usage before
   calling code dead, confirm both sites before calling logic duplicated, cite
   `path:line` (or the directory/role/target and the trace you followed for
   structural findings). Separate proven findings from hypotheses; a hypothesis is
   allowed only with the smallest verification step to confirm or reject it. Invent
   no facts, vulnerabilities, or future requirements.
2. **Aggregation.** State the net gain in one sentence: which correctness,
   security, reliability, or maintenance risk shrinks, and at what change cost.
   Reject churn — a stylistic rename, a mechanical `set -euo pipefail`, or a new
   abstraction that centralizes no real responsibility is noise. Difference is not
   improvement. Prefer the smallest behavior-preserving fix; recommend a broad
   rewrite only when a small local change cannot address the issue.

"No findings in this category" is a valid, useful result. Listing patterns that
looked like findings but cleared on inspection proves the audit was rigorous, not
thin.

## Ground Yourself

Load current state from the repo; trust it if names, taxonomy, substrates, or
layout have changed. Read until the evidence supports concrete findings, then stop
expanding scope:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the task-relevant specs — usually
   `domain.md`, `architecture.md`, `state-model.md`, `security.md`.
3. Project-local skills when they apply: `.agents/skills/code-quality/`,
   `.agents/skills/security-analysis/`, `.agents/skills/repo-stewardship/`.
4. The repo tree and package structure, then representative Go packages
   (`cmd/`, `api/v1alpha1/`, `internal/`), the embedded Ansible collection
   (`ansible/collections/`), the embedded add-on catalog (`add-ons/`), Python
   scripts (`scripts/`), the Makefile, CI workflows (`.github/workflows/`),
   examples, and tests in scope.

```bash
git status --short
rg --files
rg -n '<suspect-symbol>|<domain-rule>|<duplicated-branch>' add-ons api cmd internal ansible scripts specs docs examples test
make check-fast   # repo gate: bundle sync, file sizes, gofmt, stale terms, pins, shellcheck, go test
go vet ./... ; make staticcheck ; make python-test
make ansible-syntax-check ; make ansible-lint-check   # when the tools are available
make build ; bin/bootwright <cmd>   # behavioral probes — PATH binaries lag this tree
```

When a check needs mutation, missing tools, network, or unavailable infrastructure,
record the limitation instead of substituting speculation.

## Guardrails

Apply the Core Invariants in `/AGENTS.md` (scope, provider neutrality, product API,
drive official tools, secrets, output routing, clean-break `v1alpha1`, definitions);
verify their current form in `specs/`. Prompt-specific addition:

- **Executor split.** Go owns CLI, input loading/validation, normalization,
  rendering, storage intent, task planning, locking, ledgers, status, and
  orchestration. Host and bastion mutations execute in Ansible;
  installed-cluster API operations execute in Go through the `oc` boundary
  (`internal/addons/oc`); the `openshift-install` agent run stays in Ansible.
  New cluster-scoped executors follow this split instead of re-deciding it.
  Confirm the exact wording in `specs/architecture.md` before relying on it.

## Priority Order

Triage findings in this order, and let it drive both severity and the plan:

1. Correctness, data loss, security exposure, unsafe side effects.
2. Reliability problems that make failure hard to diagnose or recover from.
3. Test gaps around behavior that can break users.
4. Dead code, duplicated logic or domain rules, unclear ownership, responsibility
   drift that preserves stale behavior.
5. Maintainability issues that cause repeated errors or high change cost.
6. Naming, readability, and local simplification that materially improve
   discoverability, ownership boundaries, or domain consistency.
7. Efficiency improvements with a clear operational payoff.

## What to Look For

Pick the lenses with teeth for the scope; do not run every bullet. Apply
domain-driven discipline throughout: a domain rule, invariant, or decision lives in
the package that owns it, and adapters, CLI commands, renderers, and tests consume
that owner instead of reimplementing it.

**Go.** Unwrapped or vague errors; ignored errors or command results; panics on
normal paths; global mutable state that blocks tests or concurrency; unsafe path
joins, temp-file handling, cleanup, or permissions; shell invocation where direct
`exec.Command` args are safer; missing context/cancellation/timeout; resource
leaks and misplaced `defer`; CLI commands embedding domain logic that belongs in
loaders, renderers, orchestrators, or roles; non-mutating surfaces (`validate`,
`preflight`, `plan`, `diff`, `status`) drifting from the selected graph that
apply/destroy actually load; `apply --converge-drifted` changing more than the
narrow documented safety barrier (with `--confirm-data-loss` gating data loss);
tests needing real infrastructure where a fake would prove the behavior.

**Go↔Ansible drift.** Go mutating hosts or the bastion directly instead of
rendering intent and orchestrating; installed-cluster API calls bypassing the
`internal/addons/oc` boundary; Ansible making CLI, validation,
desired-state-ownership, rendering, storage-intent, planning, locking, or status
decisions. A fact computed in a role that the renderer could have written into vars
is a leak. Per drift: current owner, correct owner, the boundary contract, and the
smallest refactor that moves it back without duplicating logic.

**Scripts, Make, CI.** Names that hide side effects, target host, or workflow
stage; unquoted vars, unsafe globbing, word splitting, path traversal; broad
cleanup (`rm -rf`) without constrained paths; pipelines that swallow failures;
unsafe temp creation/cleanup; commands built by string interpolation where arrays
or fixed args are safer; assumptions about cwd, user, PATH, OS, tools, network, or
writable locations; missing preflight checks; secrets in command traces, logs, or
generated files; non-idempotent scripts that look rerun-safe; hard-coded
environment-specific paths/users/hosts; targets not `.PHONY` or doing more than
their name implies; destructive targets without scoping; unpinned actions, images,
tools, or versions; CI that skips relevant checks or has no offline path. Do not
recommend `set -euo pipefail` mechanically — name the failure mode it prevents and
the conditionals that need care. The `scripts/` tree is Python (bundle sync and
collection verification, unit-tested via `make python-test`); apply the same
lenses there — swallowed exceptions, unsafe temp/path handling, platform
assumptions, drift between script behavior and its tests. Authored shell lives
in Ansible role files and Make recipes; `make shellcheck-check` discovers it by
shebang under `ansible/` and `scripts/`.

**Ansible.** Non-idempotent tasks; shell/command where a module fits, or without
intentional `changed_when`/`failed_when`; missing `no_log` for sensitive values;
weak permissions on credential or runtime-state files; hidden assumptions about
controller or remote-host shape; variable defaults masking missing required input;
handlers that fire unpredictably; roles blurring provider, shared-service,
cluster-infra, or OpenShift responsibilities; unused or duplicated roles/tasks/
vars/templates that should be deleted or centralized.

**Add-on catalog and hooks.** The embedded catalog (`add-ons/catalog.yaml`,
per-add-on `add-on.yaml`, hook playbooks and manifests) ships content that
`add-ons add` snapshots into the root-owned store under `/var/lib/bootwright`
and that hook execution applies to installed clusters. Audit loader strictness
against store snapshots, required/optional input handling, digest coverage of
everything that changes hook behavior (inputs, extra vars, manifests, outputs),
manifest templating boundaries, workspace lifecycle and cleanup on failure, and
parity between catalog entries and the examples that consume them.

**Security and supply chain.** Committed secrets or examples teaching unsafe secret
placement; command injection and unsafe templating; path traversal and unsafe
archive extraction; world-readable sensitive runtime material; TLS verification
disabled without a documented narrow reason; downloads or artifacts without
checksum/version/digest pinning; mutable image tags (`latest`); excessive
privilege, sudo, service-account scope, or BMC access; logs/errors/telemetry
leaking private host data or secret material; stale security-sensitive paths
(privilege, redaction, command exec, TLS, secret handling) that can diverge from
the maintained path. Treat the embedded add-on catalog and its store snapshots as
supply-chain surface: what guarantees integrity between the catalog, the
registered store copy, and the manifests and playbooks applied to clusters.
Report only what code evidence supports.

**State-inspection surfaces.** Audit the non-mutating desired-vs-real surfaces:
`plan` (preview without contacting anything), `preflight` (live, read-only
readiness), `status` (recorded run/ledger view plus next step), and `diff` —
live comparison by default, `--recorded` for the offline last-apply view,
`--adopt` folding live reality back into desired YAML. Each must load the same
selected graph as apply/destroy, read recorded evidence safely, report root
absence succinctly, and report granular drift when roots exist, including
missing declared resources and undeclared live resources such as Ceph pools,
filesystems, gateways, add-ons, VMs, services, endpoints, or storage exports.
Check text and JSON output, drift exit codes, permission/root behavior, behavior
with and without apply's `--converge-drifted` (and `--confirm-data-loss`), and
that no probe or `--adopt` path mutates a live system.

**Duplication and dead code.** One domain rule in multiple packages or roles; one
concept as several types/structs/helpers, or several names; one name reused for
different concepts; adapter-specific code leaking into shared orchestration;
CLI-local copies of validation/normalization/rendering/security behavior; tests
exercising stale helpers; comments/docs/examples preserving abandoned concepts.
Confirm with `rg`, package boundaries, tests, rendered outputs, CLI wiring, and
Ansible consumers before reporting. Default unused code to deletion; default
duplication to one domain-owned implementation the others call — define the
smallest refactor slice and the tests proving callers now share it.

**Naming** (as code quality, not cosmetics). Flag stale vocabulary, ambiguous
ownership, implementation-shaped names, duplicated concepts, and names that no
longer match behavior across packages, types, functions, files, roles, tasks, Make
targets, and domain terms. Propose a rename only with a clear correctness,
discoverability, or ownership gain — give current name, better name, affected
artifacts, benefit, and the smallest migration path. Otherwise keep it.

**Tests.** Missing or weak coverage around desired-state parse/normalize/validate;
rendered installer, Ansible, or lock-file output; command construction and external
process failure; filesystem permissions/cleanup/path validation; secret redaction
and sensitive-output gates; script dry-run/preflight; Ansible idempotency and
generated-variable shape; non-mutating desired-vs-real state checks;
`--converge-drifted`/`--confirm-data-loss` vs. plain-`apply` behavior; objective
drift reports for absent roots and partially
present resources; and a regression test for each high-confidence finding.

## Output Format

Cite concrete files, functions, tasks, targets, workflows, and tests. Keep it
actionable.

# Bootwright Code and Scripts Audit

## 1. Executive Summary
Three to seven bullets ordered by importance; each names the area and the
recommended direction. Lead with the single highest-priority fix.

## 2. Findings
Severity order. Per finding: **Severity** (Critical/High/Medium/Low), **Area** (Go
/ Scripts / Ansible / Integration / Make / CI / Tests / Security / Docs),
**Location** (`path:line`), **Evidence**, **Impact**, **Recommendation** (minimal
in-scope fix), **Validation** (the check that proves it). Say "none" for an empty
category instead of inventing weak issues.

## 3. Deliberately Unchanged
Patterns that looked like findings but cleared on inspection, and churn you
declined (stylistic renames, mechanical hardening, speculative abstractions) — each
with the one-line reason. Proof the findings aggregate rather than pad.

## 4. Test and Validation Gaps
Specific tests or checks to add or improve — no generic "add more tests."

## 5. Improvement Plan
**Now** (high-confidence safety/correctness/reliability/test fixes small enough to
review safely), **Next** (medium refactors or automation cleanup that benefit from
a short design pass), **Later** (larger consolidation or toolchain policy that
should not block immediate fixes). Per item: affected artifacts, smallest safe
approach, validation commands/tests, and **Risk** (Low/Medium/High). Default unused
code to deletion and duplication to one domain-owned implementation. Propose no
shims, flags, or new dependencies unless evidence shows they are necessary. End
with any open question that blocks a safe plan or changes prioritization.

## 6. Checks Run
Commands run and a one-line result each; then useful checks not run, with the
reason.

## Edit Mode (only if the user explicitly requests fixes)

Confirm the selected plan slice and affected files first (unless given). Follow
the `implementation-worktree` skill: edit only in a temporary branch and worktree
created from local `main`. Keep changes small and behavior-preserving unless
fixing a proven bug. Delete confirmed dead code rather than parking it behind
comments; route confirmed duplication through one domain-owned component and
update callers; add or adjust focused tests for changed behavior. Run the repo's
validation skills (including implementation validation) before finishing and
report any that could not run. Summarize only: changes made, files changed,
checks run, remaining follow-ups. Do not invent facts, redesign architecture,
change behavior beyond the fix, add dependencies, remove functionality, hide
errors, weaken security, or print secrets.

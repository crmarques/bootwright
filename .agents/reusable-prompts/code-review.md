# Code and Scripts Audit and Improvement Plan

You are a senior engineer auditing **Bootwright**, a desired-state orchestrator
that provisions fleets of OpenShift and OKD clusters — primarily Go with embedded
Ansible and supporting scripts. Review the current implementation and produce a
practical, prioritized fix plan grounded in the evidence you find. The deliverable
**is** the audit report and plan, not a plan for how to review.

Non-mutating by default: inspect files, run safe read-only checks, gather
evidence. Do not edit implementation files unless the user explicitly asks for a
follow-up fix slice (see *Edit Mode*).

This prompt owns **implementation quality and safety**: correctness, dead code,
duplication, error handling, shell/Ansible/CI safety, security, supply chain,
naming-as-code-quality, and test gaps. For architecture/package-boundary redesign
use `architecture.md`; for schema or user-facing UX use `specs-ux.md` or
`cli-schema-ux-rethink.md`. Out of scope here: broad architecture redesign, schema
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

The repo is the source of truth; load current state instead of relying on memory,
and trust the repo if names, taxonomy, substrates, or layout have changed. Read
until the evidence supports concrete findings, then stop expanding scope:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the task-relevant specs — usually
   `domain.md`, `architecture.md`, `state-model.md`, `security.md`.
3. Project-local skills when they apply: `.agents/skills/code-quality/`,
   `.agents/skills/security-analysis/`, `.agents/skills/repo-stewardship/`.
4. The repo tree and package structure, then representative Go packages, Ansible
   roles/playbooks, scripts, Makefiles, CI workflows, examples, and tests in scope.

```bash
git status --short
rg --files
rg -n '<suspect-symbol>|<domain-rule>|<duplicated-branch>' internal api ansible scripts specs test
go list ./... ; go test ./... ; go vet ./... ; gofmt -l .
shellcheck scripts/* test/**/*.sh        # when available
ansible-lint ; ansible-playbook --syntax-check <playbook>   # when available
```

Do not install tools or fetch dependencies for a review-only audit unless the user
allows it. When a check needs mutation, missing tools, network, or unavailable
infrastructure, record the limitation instead of substituting speculation.

## Durable Guardrails

Verify each in the current repo before relying on it; do not recommend anything
that violates them:

- **Scope.** Stay inside Bootwright's stated scope; Day-2 fleet publication lives
  elsewhere unless the specs say otherwise.
- **Product API.** Desired-state YAML is the user API; generated artifacts are
  outputs, not authored source of truth.
- **Provider neutrality.** Keep abstractions open for supported substrates; do not
  hard-code to the current lab, one vendor, one topology, or one install mode.
- **Secrets.** Credentials, kubeconfigs, pull secrets, private keys, and tokens
  never appear in versioned content, examples, logs, generated docs, or snippets.
- **Output.** CLI human output goes through `internal/cli/output`; raw exceptions
  stay raw (JSON, shell exports, Cobra help, prompts, external process passthrough).
- **Go↔Ansible split.** Go owns CLI, input loading/validation, normalization,
  rendering, storage intent, task planning, locking, ledgers, status, and
  orchestration. Ansible executes configuration and installation on the bastion and
  target hosts/clusters. Confirm the spec's exact wording before relying on it.
- **Clean break.** `v1alpha1` may break cleanly: no migrations, aliases, shims, or
  legacy examples.

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
loaders, renderers, orchestrators, or roles; missing or poorly named non-mutating
desired-vs-real state-check command; `--override` changing more than the narrow
documented safety barrier; tests needing real infrastructure where a fake would
prove the behavior.

**Go↔Ansible drift.** Go directly configuring or installing on hosts/clusters
instead of rendering intent and orchestrating; Ansible making CLI, validation,
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
the conditionals that need care.

**Ansible.** Non-idempotent tasks; shell/command where a module fits, or without
intentional `changed_when`/`failed_when`; missing `no_log` for sensitive values;
weak permissions on credential or runtime-state files; hidden assumptions about
controller or remote-host shape; variable defaults masking missing required input;
handlers that fire unpredictably; roles blurring provider, shared-service,
cluster-infra, or OpenShift responsibilities; unused or duplicated roles/tasks/
vars/templates that should be deleted or centralized.

**Security and supply chain.** Committed secrets or examples teaching unsafe secret
placement; command injection and unsafe templating; path traversal and unsafe
archive extraction; world-readable sensitive runtime material; TLS verification
disabled without a documented narrow reason; downloads or artifacts without
checksum/version/digest pinning; mutable image tags (`latest`); excessive
privilege, sudo, service-account scope, or BMC access; logs/errors/telemetry
leaking private host data or secret material; stale security-sensitive paths
(privilege, redaction, command exec, TLS, secret handling) that can diverge from
the maintained path. Report only what code evidence supports.

**State-check implementation.** Audit whether implementation code gives users a
safe way to ask "does selected desired state match the recorded last apply?" without
mutation. The command must have a clear name, load the same selected graph as
apply/destroy, read the recorded last-apply evidence safely, report root absence succinctly, and report
granular drift when roots exist, including missing declared resources and
undeclared live resources such as Ceph pools, filesystems, gateways, add-ons, VMs,
services, endpoints, or storage exports. Check text and JSON output, exit codes,
permission/root behavior, and behavior with and without `--override`.

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
generated-variable shape; non-mutating desired-vs-real state checks; `--override`
vs. no-override behavior; objective drift reports for absent roots and partially
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

Confirm the selected plan slice and affected files first (unless given). Keep
changes small and behavior-preserving unless fixing a proven bug. Delete confirmed
dead code rather than parking it behind comments; route confirmed duplication
through one domain-owned component and update callers; add or adjust focused tests
for changed behavior. Run the repo's validation skills (including implementation
validation) before finishing and report any that could not run. Summarize only:
changes made, files changed, checks run, remaining follow-ups. Do not invent facts,
redesign architecture, change behavior beyond the fix, add dependencies, remove
functionality, hide errors, weaken security, or print secrets.

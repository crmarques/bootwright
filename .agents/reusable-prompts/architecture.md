# Architecture Audit and Revision Plan

You are an experienced software architect rethinking **Bootwright**, a
desired-state orchestrator that provisions fleets of OpenShift and OKD clusters
from bare or virtualized substrates to installed clusters. It is Go with embedded
Ansible.

Your job is to **rethink the architecture** — make it more coherent, testable,
operationally predictable, easier to evolve, and easier for a newcomer to learn.
Pressure-test current decisions. Where the layout is right, defend it in a
sentence and move on. Where it is wrong, take a position and propose the seam.

This prompt's deliverable **is** the audit and revision plan. If the
collaboration mode requires a plan-only response, the audit below is that plan: do
the read-only grounding, review the architecture, and return the full
Architecture Audit and Revision Plan. Do not return a meta-plan describing how you
would audit, and do not defer findings to a later step.

This prompt owns **internal architecture**: package boundaries, the Go↔Ansible
split, repo/script distribution, role taxonomy, and spec/code drift. For the
user-facing CLI and schema contract use `specs-ux.md` or `cli-schema-ux-rethink.md`.

Out of scope: line-by-line code review, formatting, isolated bugs, and naming
nitpicks. Naming **is** in scope when a package, type, file, directory, role,
workflow, or domain term hides responsibility, preserves a stale concept, or makes
the architecture harder to learn. Do not edit files unless the user explicitly
asks for follow-up implementation.

## The Two Tests Every Proposal Must Pass

1. **Out of the box.** Would a fresh architect reasoning from the system's jobs
   propose this, or are you preserving the current layout because it exists? Don't
   stop at "it compiles" or "tests pass" — ask whether the boundaries are right.
2. **Aggregation.** State the net gain in one sentence: which coupling, ownership
   ambiguity, boundary leak, or newcomer dead-end disappears, and at what cost in
   churn or risk. A move or rename that only regroups things more to your taste,
   with no ownership/testability/navigation gain, is churn — reject it and say so.
   Difference is not improvement.

"No issue here" is a valid position when defended in a sentence. Listing the
boundaries you deliberately leave intact proves the proposals aggregate rather
than thrash a working layout.

## Ground Yourself

The repo is the source of truth; load current state instead of relying on memory
or on this prompt's examples — layout, kind names, role taxonomy, ADRs, and
substrates evolve. Read until you have enough, then stop:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the specs the task touches — start
   with `domain.md`, `architecture.md`, `state-model.md`.
3. `specs/adr/` and its README index — accepted decisions. Note which are
   load-bearing for the current layout and which are historical.
4. Repo tree and Go package inventory: directory layout under `cmd/`, `internal/`,
   `api/`, and the embedded Ansible collection; package names, import direction,
   and the files defining each public responsibility. Map packages to the
   desired-state pipeline before judging whether the layout is learnable.
5. Script, test, fixture, example, generated-output, and tooling directories —
   separate product code, operational automation, dev tooling, test assets,
   generated fixtures, and user-facing examples.
6. `docs/` and root `README.md` — what the project teaches.
7. Sample one or two roles/playbooks per layer to confirm description matches
   reality. Do not bulk-read.

```bash
git status --short
find . -maxdepth 4 -type d | sort
go list ./...
go list -f '{{.ImportPath}} {{.Dir}}' ./...
```

Run validation or tests only if the toolchain is already present.

## Guardrails

Apply the Core Invariants in `/AGENTS.md` (scope, provider neutrality, product API,
drive official tools, secrets, output routing, clean-break `v1alpha1`, definitions);
verify their current form in `specs/`. Architecture-specific addition:

- **Go↔Ansible split.** Go owns the control plane — CLI, input loading,
  validation, normalization, rendering, storage intent, task planning,
  orchestration, status, ledgers. Ansible executes configuration and installation
  on the bastion and target hosts or clusters. Go renders the contract and
  orchestrates; Ansible performs the mutations. Confirm the current spec's exact
  wording of this split rather than assuming it.

## Provocations

Use the ones with teeth for the repo's current state; don't run the whole list.
Where a provocation names a specific file, package, role, or registry, treat it as
a place to *look* — confirm it still exists and matches before relying on it.

**Boundaries and ownership.** For each `internal/` package, name in one sentence
what it owns that nothing else does; if two packages own the same thing, the
boundary is wrong. Walk a recent feature through load → normalize → validate →
render → orchestrate: where does each decision live, where does its *value* live,
and where does behavior leak when they drift? Render writes Ansible inputs;
orchestrate consumes them — a fact computed inside an Ansible role that the
renderer could have written into vars is a leak. Flag any Go code that directly
configures or installs on hosts/clusters, and any Ansible role making CLI,
schema, rendering, storage-intent, scheduling, locking, or status decisions.
Provider-specific logic belongs behind explicit interfaces — find substrate
knowledge in cross-cutting code that has no business knowing the substrate.

**Schema as architecture.** For each kind, ask "what breaks if we collapse this
into the layer above or below?" — if nothing important, propose the collapse. If
the API exposes a single-source-of-truth registry of owned installer fields, trace
whether validator, renderer, docs, and scaffolder all read from it; duplication is
a finding. Pick a new managed service (e.g. NTP, image cache) and count the files
an honest engineer must edit to add it — if more than a handful and the steps are
not orthogonal, the abstraction is wrong; name the missing seam.

**Go layout.** Is each `internal/<pkg>/` a noun with one job or a grab bag? Can a
developer predict where to add a kind, validator rule, renderer field, provider
adapter, CLI command, or runtime workflow from package names alone? Check
dependency direction for upward-leaning imports; the API/types package must be a
leaf. Are `os/exec`, filesystem writes, and network reaches behind contracts tests
can substitute, and is that boundary actually used as one? If the repo enforces a
per-file size guardrail, check whether it still bites or has been quietly relaxed.

**Ansible layout.** Check the embedded collection's `playbooks/`, `roles/`,
`plugins/`, `docs/` against the accepted taxonomy in `specs/adr/`. Does every role
live in the family matching its host scope and side effects? A mis-layered role
means the taxonomy is wrong or the role does two jobs. Do role, task, variable,
template, and inventory names reveal host scope, side effects, generated-input
boundaries, and idempotency? Find one unsafe shell task (missing `changed_when`,
`failed_when`, `no_log`) as the example, not the rule. Pick two roles you suspect
are not safe to re-run unattended and either defend or flag them. Confirm the
runtime-materialized collection bundle is reachable in a disconnected lab and that
dropping in an external collection does not break it.

**Drift and evolution.** Pick two non-trivial validation rules from `specs/` and
check the code enforces them; pick one validator behavior from code and check the
spec describes it — drift in either direction is a finding. Can a contributor
reading `docs/` and one role predict where to add a substrate, a managed service,
or a CLI verb? Find an ADR whose Decision contradicts current `state-model.md` or
`architecture.md`. Name the one place the project will be hardest to change, the
invariant that calcifies it, and the seam that frees it. Distinguish tech debt
that compounds from debt that ages well, and recommend paying down only the former.

## Output Format

Cite real files, directories, packages, roles, and ADRs from the current repo.
Invent nothing. Prefer one strong defended recommendation over three hedged ones.

# Architecture Audit and Revision Plan

## 1. Executive Summary
Three to seven bullets ordered by severity; each names the artifact and the
proposed change. Lead with the single highest-leverage one.

## 2. Architecture Map
The main directories, packages, scripts, roles, tests, fixtures, examples, and
docs as they exist today — what each major area owns, which artifacts are product
code vs. tooling vs. test assets vs. user-facing examples, and where boundaries
blur.

## 3. Strengths
Decisions worth preserving, specifically. Generic compliments are noise.

## 4. Main Problems
Per issue: **Severity** (Critical/High/Medium/Low), **Area** (Go / Ansible / Docs
/ Specs / Integration / Testing / Repo Layout), **Evidence** (paths, packages,
roles, docs), **Problem**, **Why it matters** (cost of leaving it),
**Recommendation** (the fix and the seam to introduce).

## 5. Responsibility and Boundary Review
Walk load → normalize → validate → render → orchestrate. Per transition, name the
owning package(s) and call out leaks. Explicitly flag Go↔Ansible split drift: per
drift, the behavior, current owner, correct owner, boundary contract, and refactor
path.

## 6. Go Package Layout
Map packages into layers (API/types, load, normalize, validate, render,
plan/orchestrate, runtime state, CLI/output, provider adapters, Ansible
integration, utilities). Per family: responsibility, dependency direction,
side-effect isolation, naming, newcomer discoverability. Recommend a better layout
or names only with clear ownership/consistency/learnability gain — otherwise
defend the current one and suggest only package comments or doc fixes. Do not
rewrite code.

## 7. Repository and Script Distribution
Treat directory/file distribution as architecture across the top-level tree and
the important subtrees. Flag placement that hides ownership, workflow order,
generated-vs-authored boundaries, or test intent. Propose a target layout with
directory responsibilities, a move/split/collapse plan, and an incremental
migration path only when it is more than aesthetic.

## 8. Ansible Layout
Role taxonomy vs. host scope and side effects, idempotency, module-vs-shell, and
embedded-bundle integrity — at the architecture level, citing the accepted ADR
taxonomy.

## 9. Docs, Specs, and ADR Drift
Drift between `specs/`, `docs/`, ADRs, and code. Outdated, missing, or misleading
docs; ADRs whose Decision contradicts current specs.

## 10. Deliberately Unchanged
Boundaries, layouts, or names you considered changing and chose to keep, each with
the reason it survived — proof the proposals aggregate rather than churn.

## 11. Recommended Target and Revision Plan
The architecture you would build from this codebase today, inside the guardrails:
directory/package/role organization, naming taxonomy, newcomer package map, import
boundaries, and the migration path. For each rename or move worth doing: current
name/placement, proposed, affected artifacts, benefit, risk, and the validation
that catches stale references. Then phase the work:

- **Phase 1 — Low-risk cleanup:** boundary fixes, doc drift, file/role moves that
  change no behavior or vars.
- **Phase 2 — Structural:** new seams, registries, contracts, reorganizations,
  test-substitution boundaries.
- **Phase 3 — Larger changes:** schema or CLI changes needing a deliberate
  decision (`v1alpha1` allows clean breaks).

Per phase: concrete actions, expected benefit, affected packages/files, suggested
tests, and dependency order. End with the smallest coherent first follow-up change.

## Constraints

- Cite real files; use current project vocabulary; introduce no placeholder names.
- Respect the `/AGENTS.md` Core Invariants; verify their current form in `specs/` first.
- Assume a small team must keep this understandable — reject generality bought at
  the cost of comprehension, and reject churn that buys no ownership or navigation
  gain.
- Every recommendation must pass the Aggregation test. Take a position; "it
  depends" is not a recommendation.

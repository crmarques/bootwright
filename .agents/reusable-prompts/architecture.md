# Architecture Audit and Revision Plan

You are an experienced software architect reviewing **Bootwright**, a
desired-state orchestrator for OpenShift cluster provisioning. The
project is written in Go with embedded Ansible.

Your task is not a checklist — it is to **rethink the architecture**
and propose changes that make the system more coherent, more testable,
more operationally predictable, easier to evolve, and easier for
newcomer developers to understand. Pressure-test current decisions.
Where the existing layout is right, defend it briefly and move on.
Where it is wrong, take a position and propose the change.

Out of scope: line-by-line code review, formatting, isolated naming nitpicks,
and isolated bugs. Naming is in scope when package, type, script, file,
directory, role, workflow, or domain vocabulary hides responsibility, preserves
stale concepts, or makes the architecture harder to learn.

## How to ground yourself

The repository is the source of truth — load current state instead of
relying on what you remember. Read **in this order**, and stop loading
once you have enough:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the specs the task
   touches — start with `domain.md`, `architecture.md`, and
   `state-model.md`.
3. `specs/adr/*` — accepted decisions. Note which decisions are load-
   bearing for the current layout, and which are historical.
4. Repository tree: `go list ./...`, plus the directories under
   `internal/`, `api/`, `ansible/roles/`, `ansible/playbooks/`.
5. Go package inventory: package names, import direction, package
   comments when present, and the files that define public package
   responsibilities. Map packages to the desired-state pipeline before
   judging whether the layout is understandable.
6. Script, test, fixture, example, generated-output, and support-tool
   directories. Identify what belongs to product code, operational
   automation, developer tooling, test-only assets, generated fixtures,
   and user-facing examples.
7. `docs/` and the root `README.md` — what the project teaches users.
8. Sample one or two roles/playbooks per layer to verify the
   description matches reality. Do not bulk-read.

If the layout, kind names, role taxonomy, or supported substrates have
evolved since you last saw them, trust what is in the repo now.

Useful read-only commands:

```bash
git status --short
find . -maxdepth 4 -type d | sort
go list ./...
go list -f '{{.ImportPath}} {{.Dir}}' ./...
go test ./...
```

Run validation only if the toolchain is already present. Do not install
new tools.

## Durable guardrails

These come from `specs/` and operating rules; verify them in-repo each
time. Do **not** propose architectures that violate them:

- Stay within the project's stated scope and out-of-scope list (see
  `specs/domain.md`). Day-2 fleet publication concerns belong to a
  separate project unless the spec says otherwise.
- `v1alpha1` can break cleanly: do not propose migrations, aliases, or
  compatibility shims.
- Do not propose architectures that lock the project to a single
  substrate, topology, or install mode. Provider abstraction must
  remain open to the substrates the spec claims to support.
- Secrets, kubeconfigs, pull secrets, private keys, and tokens must
  never appear in versioned content, examples, or your recommended
  snippets.
- Desired-state YAML is the user API. Generated artifacts are not user
  edit points.
- Ansible inventory and vars are generated; users do not maintain them.
- Go owns the control plane of Bootwright: CLI behavior, desired-state
  input loading, validation, normalization, rendering, Bootwright storage
  intent, task planning, orchestration, status, and runtime ledgers.
- Ansible owns configuration and installation execution on the bastion
  and target hosts or clusters. Go should render the contract and
  orchestrate execution; Ansible should perform the host and cluster
  mutations.

## Provocations

Use these to push on design, not as a checklist. Pick the ones with
teeth for this repo's current state.

**On repository and file distribution:**

- *Code and script placement.* Walk the top-level directories and the main
  subtrees under `cmd/`, `internal/`, `api/`, `ansible/`, `scripts/`,
  `test/`, `examples/`, `docs/`, `specs/`, and `.agents/`. Can a
  newcomer predict what belongs in each directory before reading many files?
  If not, identify the distribution rule that is missing or misleading.
- *File boundaries.* Which files are doing too many jobs, and which related
  files are scattered across directories in a way that hides the workflow?
  Propose splits, moves, or collapses only when they improve ownership,
  navigation, or testability.
- *Scripts vs. product logic.* Are scripts developer tooling, release
  automation, test harnesses, or runtime behavior? If scripts blur those
  roles, propose a clearer directory taxonomy and the boundary that prevents
  product logic from living in ad hoc scripts.
- *Tests, fixtures, and examples.* Do tests and fixtures sit near the code or
  workflow they prove? Are user-facing examples separated from generated or
  test-only inputs? Propose moves when placement hides intent or makes drift
  likely.
- *Generated and embedded content.* Are generated files, embedded bundles, and
  source inputs clearly separated? Flag layouts that make generated output look
  authored or make authored definitions look generated.
- *Better repository layout.* If a different directory/file organization would
  make the architecture easier to understand, propose the target tree,
  responsibilities for each directory, import/reference rules, migration
  steps, and validation checks. Keep the current structure when the proposed
  reorganization is mostly aesthetic or lacks a clear maintenance gain.

**On responsibility boundaries:**

- *Owns vs. orchestrates.* For each `internal/` package, name one
  sentence: what does it own that no one else does? If two packages
  own the same thing, the boundary is wrong.
- *Go ↔ Ansible split.* Pick a recent feature and walk the pipeline:
  load → normalize → validate → render → orchestrate. Where does the
  decision live? Where does the *value* of that decision live? When
  those drift, where does behavior leak?
- *Required execution split.* Verify that Go performs all CLI, input
  validation, rendering, Bootwright storage intent, task planning, and
  orchestration logic. Verify that configuration and installation steps
  in the bastion and target hosts or clusters are executed by Ansible.
  Flag any Go code that directly configures or installs on hosts or
  clusters, and flag any Ansible role that makes CLI policy, schema,
  rendering, storage-intent, scheduling, locking, or status decisions.
- *Render vs. orchestrate.* Render produces Ansible inputs; orchestrate
  consumes them. If a fact is computed by an Ansible role that the
  renderer could have written into vars, that's a leak — flag it.
- *Adapters vs. shared logic.* Provider-specific code should sit behind
  explicit interfaces. Where does substrate-specific logic appear in
  cross-cutting code that has no business knowing the substrate?

**On the schema as architecture:**

- *Does each kind earn its existence?* For every kind in `state-model.md`,
  ask "what fails if we collapse this into the layer above/below?"
  If the answer is "nothing important," propose the collapse.
- *Owned-fields registry.* `api/v1alpha1` exposes a registry of fields
  Bootwright owns in the installer schema. Is the registry actually
  the single source of truth? Trace whether the validator, the renderer,
  the docs, and the scaffolder all read from it — duplication is a
  finding.
- *Sharing semantics.* The schema says "one instance per host" with
  cross-cluster sharing validated at apply time. Is the *discovery* of
  sharing as good as the *validation* of sharing? Pick a fleet of three
  clusters that share two hosts and walk it.
- *Add-on cost.* Pick a new managed service (e.g. NTP, image cache).
  Count the files an honest engineer must edit. If the count is more
  than 5 and the steps are not orchogonal, the abstraction is wrong —
  propose the missing seam.

**On Go layout:**

- *Package cohesion.* Is each `internal/<pkg>/` a noun with a single
  job, or a grab bag? `internal/cli/` files are routinely the worst
  offenders; check whether the Makefile guardrail (max file size) is
  still meaningful or has been silently tuned upward.
- *Newcomer map.* Can a developer new to Bootwright predict where to
  add a new kind, validator rule, renderer field, provider adapter,
  CLI command, or runtime workflow by reading package names alone? If
  not, identify the packages whose names or placement hide the
  pipeline.
- *Dependency direction.* Run `go list -deps` mentally. Are there
  upward-leaning imports (e.g. `infra` importing `cli`)? `api/v1alpha1`
  must be a leaf.
- *Side-effect isolation.* `os.Exec`, filesystem writes, network
  reaches — are they behind contracts the tests can substitute? Where
  is the boundary, and is it actually used as one?
- *Better package layout.* If the current layout makes ownership hard
  to learn, propose a concrete target layout with package names,
  responsibilities, import rules, and migration steps. If the current
  layout is already strong, say so and recommend only package comments,
  README updates, or naming cleanups that would help newcomers.
- *Names as architecture.* Review package, type, struct, interface,
  file, directory, script, Make target, workflow, and domain names. Do
  the names teach where behavior belongs, or do they preserve old terms,
  expose implementation mechanics, duplicate concepts, or point new
  contributors at the wrong layer? Propose better names only when the
  benefit is clearer ownership, cleaner domain language, or easier
  navigation.

**On Ansible layout:**

- *Role taxonomy.* `ansible/roles/{bastion, shared, providers,
  cluster_infra, openshift}/` exists for a reason. Does every role
  live in the layer that matches its hosts? Mis-layered roles imply
  the taxonomy is wrong or the role is doing two jobs.
- *Role and task names.* Do role, task, variable, template, inventory,
  and directory names reveal host scope, side effects, generated-input
  boundaries, and idempotency expectations? If a better taxonomy or
  name would reduce mistakes, propose it with affected files and a
  migration path.
- *Module vs. shell.* Shell tasks should declare `changed_when`,
  `failed_when`, and `no_log` where appropriate. Find an unsafe shell
  task and use it as the example, not the rule.
- *Idempotency.* Pick two roles you suspect are not safe to re-run
  unattended. Read the tasks. Either defend or flag.
- *Embedded bundle.* `internal/embedded` materializes the bundle at
  runtime. Are *all* role search paths under
  `ANSIBLE_ROLES_PATH` reachable in a disconnected lab? When the user
  drops an external collection in, does anything break?

**On documentation and specs alignment:**

- *Spec drift.* Pick two non-trivial validation rules from `specs/`
  and verify whether the code enforces them. Pick one validator
  behavior from code and check whether the spec describes it. Drift in
  either direction is an architectural finding (specs encode the
  contract; code that diverges is wrong).
- *Doc-driven discovery.* A new contributor reads `docs/`, `README.md`,
  and a sample role. Can they predict where to add a new substrate?
  A new managed service? A new CLI verb? Where the prediction fails,
  the docs lag the architecture.
- *ADRs.* Only current decisions are kept; superseded ADRs are
  removed. Check `specs/adr/README.md` against the actual files. Find
  any ADR whose Decision section contradicts the current `state-model.md`
  or `architecture.md` — that's a finding.

**On evolution:**

- *Where will the project be hard to change?* Pick one place. Name the
  invariant that calcifies it. Propose the seam.
- *Tech debt that compounds vs. tech debt that ages well.* Distinguish.
  Recommend paying down only the compounding kind.

You are encouraged to disagree with current decisions inside the
guardrails. Take a position and defend it; "no issue here" is a valid
position if defended in one sentence.

## Output format

Cite real files, directories, packages, scripts, roles, and ADRs from the
current repo state. No invented behaviour. Prefer one strong defended
recommendation over three hedged ones.

# Architecture Audit and Revision Plan

## 1. Executive Summary

Three to seven bullets, ordered by severity. Each bullet names the
artifact and the proposed change.

## 2. Repository Architecture Map

The main directories, packages, scripts, roles, tests, fixtures, examples, and
docs as they exist today. Note what each major area appears to be responsible
for, which artifacts are product code vs. developer tooling vs. test assets
vs. user-facing examples, and where the boundaries are blurred.

## 3. Strengths

Architectural decisions worth preserving. Be specific — generic
compliments are noise.

## 4. Main Architecture Problems

For each issue:

- **Severity:** Critical / High / Medium / Low
- **Area:** Go, Ansible, Docs, Specs, Integration, Testing, Repository
  Layout
- **Evidence:** concrete file paths, packages, roles, or docs
- **Problem:** what is architecturally wrong
- **Why it matters:** the cost of leaving it
- **Recommendation:** practical fix, including the seam to introduce

## 5. Responsibility and Boundary Review

Walk the load → normalize → validate → render → orchestrate pipeline.
For each transition, name the package(s) that own it and call out
boundary leaks.

## 6. Go Package Layout Review

Map the current packages into architectural layers: API/types, load,
normalize, validate, render, plan/orchestrate, runtime state, CLI/output,
provider adapters, Ansible integration, and support utilities. For each
package family, review responsibility, dependency direction,
side-effect isolation, naming, and newcomer discoverability. Recommend a
better package layout or better names if needed; include package, type,
file, directory, script, workflow, or domain names, what each artifact
would own, import rules, and an incremental migration path. Keep current
names when proposed alternatives do not clearly improve ownership,
consistency, or learnability. If no layout or naming change is needed,
defend that position and recommend only the smallest documentation or
package-comment improvements that would make the structure easier to
learn. Do not rewrite code.

## 7. Repository and Script Distribution Review

Review code and script directory/file distribution as architecture. Cover the
top-level tree and the important subtrees under `cmd/`, `internal/`, `api/`,
`ansible/`, `scripts/`, `test/`, `examples/`, `docs/`, `specs/`, and
`.agents/`. Identify directories or files whose placement makes ownership,
workflow order, generated/authored boundaries, or test intent harder to
understand. If the current structure is good enough, defend it. If a better
structure would be clearer, propose the target layout, directory
responsibilities, file move/split/collapse plan, compatibility impact,
incremental migration path, and validation checks. Do not propose churn for
purely aesthetic grouping.

## 8. Ansible Layout Review

Role taxonomy vs. hosts (`bastion` / `shared` / `providers` /
`cluster_infra` / `openshift`), idempotency, module-vs-shell, embedded
bundle integrity. Recommend improvements at the architecture level.

## 9. Go ↔ Ansible Integration Review

Where the rendered contract (inventory.yaml, vars.yaml, installer
files) is healthy, where it leaks. Recommend where contracts,
interfaces, generated files, schemas, or adapters should exist.
Explicitly identify any drift from the required split: Go owns CLI,
input validation, rendering, Bootwright storage intent, task planning,
orchestration, status, and ledgers; Ansible owns bastion and target
host or cluster configuration and installation execution. For each
drift, name the behavior, current owner, correct owner, boundary
contract, and refactor path.

## 10. Docs and Specs Review

Drift between `specs/`, `docs/`, ADRs, and code. Identify outdated,
missing, or misleading documentation. ADRs whose Decision contradicts
current specs are findings.

## 11. Recommended Target Architecture

The architecture you would build if starting from this code base today,
inside the guardrails. Include suggested directory/package/role
organization, code/script distribution, naming taxonomy, newcomer-facing
package map, import/reference boundaries, and the migration path from current
state. For each repository layout change or rename that is worth doing, state
the current placement/name, proposed placement/name, affected artifacts,
expected architectural benefit, risk, and validation needed to avoid stale
references.

## 12. Architecture Revision Plan

- **Phase 1 — Low-risk cleanup:** package boundary fixes, doc drift,
  file moves, script taxonomy fixes, and role moves that don't change
  behavior or vars.
- **Phase 2 — Structural improvements:** new seams, registries,
  contracts, directory reorganizations, and test-substitution boundaries.
- **Phase 3 — Larger architectural changes:** schema or CLI changes
  that require a deliberate decision. Note `v1alpha1` allows clean
  breaks without shims.

Each phase: concrete actions, expected benefits, affected packages or
files, suggested tests, and dependency order.

## 13. Quick Wins

Architecture improvements deliverable in under a day each.

Include rename proposals here only when they are low-risk, evidence-backed,
and more than cosmetic.

Include file or directory moves here only when they are low-risk,
evidence-backed, and make ownership or workflow navigation clearer.

## 14. Open Questions

Decisions you cannot make from the repository alone. Short and
answerable.

## Constraints

- Cite real files from the current repo. No invented behaviour.
- Use the project's current vocabulary. Do not introduce placeholder
  names or commands.
- Respect the durable guardrails above; verify their current form in
  `specs/` before relying on them.
- Be critical but constructive. Defend strong positions; do not hedge
  every recommendation into uselessness.
- Assume this project remains understandable and maintainable by a
  small team. Reject proposals that buy generality at the cost of
  comprehension.
- Take a position. "It depends" is not a recommendation.

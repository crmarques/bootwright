# Architecture Review

You are an experienced software architect reviewing **Bootwright**, a
desired-state orchestrator for OpenShift cluster provisioning. The
project is written in Go with embedded Ansible.

Your task is not a checklist — it is to **rethink the architecture**
and propose changes that make the system more coherent, more testable,
and easier to evolve. Pressure-test current decisions. Where the
existing layout is right, defend it briefly and move on. Where it is
wrong, take a position and propose the change.

Out of scope: line-by-line code review, formatting, naming nitpicks,
isolated bugs.

## How to ground yourself

The repository is the source of truth — load current state instead of
relying on what you remember. Read **in this order**, and stop loading
once you have enough:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the specs the task
   touches — start with `architecture.md` and `state-model.md`.
3. `specs/adr/*` — accepted decisions. Note which decisions are load-
   bearing for the current layout, and which are historical.
4. Repository tree: `go list ./...`, plus the directories under
   `internal/`, `api/`, `ansible/roles/`, `ansible/playbooks/`.
5. `docs/` and the root `README.md` — what the project teaches users.
6. Sample one or two roles/playbooks per layer to verify the
   description matches reality. Do not bulk-read.

If the layout, kind names, role taxonomy, or supported substrates have
evolved since you last saw them, trust what is in the repo now.

Useful read-only commands:

```bash
git status --short
find . -maxdepth 4 -type d | sort
go list ./...
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

## Provocations

Use these to push on design, not as a checklist. Pick the ones with
teeth for this repo's current state.

**On responsibility boundaries:**

- *Owns vs. orchestrates.* For each `internal/` package, name one
  sentence: what does it own that no one else does? If two packages
  own the same thing, the boundary is wrong.
- *Go ↔ Ansible split.* Pick a recent feature and walk the pipeline:
  load → normalize → validate → render → orchestrate. Where does the
  decision live? Where does the *value* of that decision live? When
  those drift, where does behavior leak?
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
- *Extension cost.* Pick a new managed service (e.g. NTP, image cache).
  Count the files an honest engineer must edit. If the count is more
  than 5 and the steps are not orchogonal, the abstraction is wrong —
  propose the missing seam.

**On Go layout:**

- *Package cohesion.* Is each `internal/<pkg>/` a noun with a single
  job, or a grab bag? `internal/cli/` files are routinely the worst
  offenders; check whether the Makefile guardrail (max file size) is
  still meaningful or has been silently tuned upward.
- *Dependency direction.* Run `go list -deps` mentally. Are there
  upward-leaning imports (e.g. `infra` importing `cli`)? `api/v1alpha1`
  must be a leaf.
- *Side-effect isolation.* `os.Exec`, filesystem writes, network
  reaches — are they behind contracts the tests can substitute? Where
  is the boundary, and is it actually used as one?

**On Ansible layout:**

- *Role taxonomy.* `ansible/roles/{bastion, shared, providers,
  cluster_infra, openshift}/` exists for a reason. Does every role
  live in the layer that matches its hosts? Mis-layered roles imply
  the taxonomy is wrong or the role is doing two jobs.
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

Cite real files, packages, roles, and ADRs from the current repo state.
No invented behaviour. Prefer one strong defended recommendation over
three hedged ones.

# Architecture Review

## 1. Executive Summary

Three to seven bullets, ordered by severity. Each bullet names the
artifact and the proposed change.

## 2. Repository Architecture Map

The main directories, packages, roles, and docs as they exist today.
Note what each major area appears to be responsible for and where the
boundaries are blurred.

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

Per-package responsibility, dependency direction, and side-effect
isolation. Recommend a better layout if needed; do not rewrite code.

## 7. Ansible Layout Review

Role taxonomy vs. hosts (`bastion` / `shared` / `providers` /
`cluster_infra` / `openshift`), idempotency, module-vs-shell, embedded
bundle integrity. Recommend improvements at the architecture level.

## 8. Go ↔ Ansible Integration Review

Where the rendered contract (inventory.yaml, vars.yaml, installer
files) is healthy, where it leaks. Recommend where contracts,
interfaces, generated files, schemas, or adapters should exist.

## 9. Docs and Specs Review

Drift between `specs/`, `docs/`, ADRs, and code. Identify outdated,
missing, or misleading documentation. ADRs whose Decision contradicts
current specs are findings.

## 10. Recommended Target Architecture

The architecture you would build if starting from this code base today,
inside the guardrails. Include suggested directory/package/role
organization and the migration path from current state.

## 11. Refactoring Roadmap

- **Phase 1 — Low-risk cleanup:** package boundary fixes, doc drift,
  role moves that don't change vars.
- **Phase 2 — Structural improvements:** new seams, registries,
  contracts, test-substitution boundaries.
- **Phase 3 — Larger architectural changes:** schema or CLI changes
  that require a deliberate decision. Note `v1alpha1` allows clean
  breaks without shims.

Each phase: concrete actions and expected benefits.

## 12. Quick Wins

Architecture improvements deliverable in under a day each.

## 13. Open Questions

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

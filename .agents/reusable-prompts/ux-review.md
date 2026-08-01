# Bootwright UX Review

You are a product-minded principal engineer reviewing **Bootwright**'s
user-facing contract: the desired-state YAML API (the authored kinds) and the
operator CLI.

Start from the problem, not the code. Operators must take fleets of
OpenShift/OKD container clusters and Ceph storage clusters from bare or
virtualized hardware to installed, converged platforms — repeatably, hands-off,
and safely enough for production. Bootwright's answer is an **orchestrator**:
authored desired-state YAML is normalized, validated, rendered into the input
files of the official tools it drives (`openshift-install agent`,
`cephadm`/`ceph orch`, Anaconda kickstart, nmstate, Redfish), and applied
idempotently behind fail-closed safety gates. Restate this problem and solution
in your own words first; every judgment that follows must serve an operator
doing that job.

Your job: pressure-test the contract from the operator's chair and produce a
prioritized improvement plan that maximizes intuitivity, user experience, and
production safety. The plan's deliverables are **definitions only** — docs,
specs, ADRs, examples. Anything that lives in code (CLI help strings,
validation messages, schema fields, command shapes) becomes a **handoff** item:
specify the exact contract or wording the implementation prompts must meet.
This review edits nothing.

## Two Tests Every Proposal Must Pass

1. **Out of the box.** Would this occur to someone reasoning from the
   operator's job rather than from the current code? Don't stop at "docs match
   code" — ask whether the model is the *right* model.
2. **Aggregation.** State the net gain in one sentence: which operator error,
   confusion, duplicated fact, unsafe default, or ambiguous rule disappears,
   and at what cost in explicitness or risk. Swapping one acceptable shape for
   another is churn — reject it and say so. Difference is not improvement.

"No change needed" is a valid, valuable finding. List what you deliberately
leave alone; it proves the survivors earned their place.

## Ground Yourself

The repository is the source of truth, and the verb/flag/token surface changes
over time — verify, never assume. Read until you have enough evidence, then
stop:

1. `AGENTS.md`, then `.agents/README.md`.
2. `specs/README.md` and `specs/index.md`; then `specs/domain.md` (operating
   model, UX principles), `specs/state-model.md` (kind schemas and the CLI
   Contract), `specs/architecture.md`, `specs/security.md`, and the decision
   table in `specs/adr/README.md` — an accepted ADR may already fix a shape or
   record why the obvious alternative was rejected.
3. `README.md` and `docs/` — `getting-started/` teaches the workflow,
   `concepts/` is the per-kind field reference, `advanced/` owns the safety and
   operations story.
4. The smallest example under `examples/` first, then a fleet example and the
   `test/e2e/` fixtures.
5. The CLI as built from this checkout (`make build`, then `bin/bootwright`);
   installed binaries can lag. The safety matrix in
   `internal/cli/apply_destroy_safety_matrix_test.go` enumerates the current
   verb × scope × mode × authorization × starting-state behavior.

## Guardrails

Apply the Core Invariants in `/AGENTS.md` and verify their current form in
`/specs/`. Review-specific anchors:

- **One owner per fact.** Lower-layer objects must not know their upper-layer
  consumers; intent references downward (single accepted exception: ADR 0004).
  Never improve UX by duplicating a fact across layers.
- **Safety axes.** Intent is `apply`/`plan` `--mode`; each named risk is a
  separate `--authorize <token>`; `--yes` answers only the ordinary
  confirmation and never authorizes data loss. Refusals fail closed and name
  the exact command that proceeds intentionally (ADR 0030 and its extensions).
  Proposals must preserve or strengthen this model, never soften it.
- **State checking.** `diff` is the non-mutating desired-vs-real contract
  (live by default, `--recorded` offline, exit 3 on drift); `--adopt` is its
  one mutating opt-in. Keep absence succinct and drift granular.

## Lenses

Work every lens; spend depth where operator pain is highest.

**1. Operator journey.** From an empty machine to a converged cluster and back
to teardown: is the next command discoverable from help, `status`, and docs
alone? Where is the first cliff (secrets, entitlements, BMC, DNS, trust)?
After a partial failure — what does the operator see, rerun, and clean up? Do
automation paths hold (JSON output, exit codes, no hidden prompts)? Are
read-only vs. mutating boundaries predictable across `validate` / `preflight`
/ `plan` / `diff` / `render` / `status` versus `apply` / `destroy` and the
day-2 verbs?

**2. Native-tool alignment (the orchestrator test).** For every kind that
fronts a driven tool, map authored fields to the native input vocabulary —
`ContainerCluster` ↔ `install-config.yaml`/`agent-config.yaml`; the storage
kinds ↔ `cephadm bootstrap` flags, `ceph orch` service specs, and `ceph` CLI
verbs; `MachineInstallProfile` ↔ kickstart directives; `NetworkConfig` ↔
nmstate; `Machine` BMC ↔ the Metal3/Redfish address vocabulary. Classify each
divergence: **renamed**, **relocated** (another kind owns it),
**restructured**, **derived** (not authorable), or **invented** (no native
counterpart). ADR 0008 is the written mirroring precedent; camelCase
respellings per ADR 0014 count as mirrored. Divergence is justified only by
real orchestration value — cross-document references, secret `…Ref`
indirection, fleet-level defaults, multi-cluster composition, safety. An
undefended rename, a shifted value vocabulary (enum spellings), or a native
knob operators know that Bootwright hides is a finding. An operator who knows
the native tool should feel at home; one who pastes a native input file in
should lose as little as possible.

**3. Authoring economy.** What is the smallest complete input, and does the
repo teach it first? For each fact, can the user predict the one owning kind
and file? Which fields expose tool mechanics instead of intent, and which
common intents have no first-class field (forcing overrides, custom playbooks,
or generated-file edits)? Do examples, fixtures, and docs agree on layout and
naming? Classify suspect fields: keep / default / derive / replace / remove.

**4. Production safety.** Walk the destructive paths as an operator: are
refusals understandable, are authorization tokens discoverable before a
refusal is hit, is data loss impossible without its named token, is the blast
radius of a scoped run predictable? Is the safety model taught in the docs as
strongly as the safety matrix enforces it?

**5. Definition quality.** Do the specs give enumerable accept/reject rules
where behavior matters — would two implementers agree on every case? Does each
doc page teach a workflow and link to the normative spec without duplicating
it (the `docs/concepts/` field tables are the sanctioned exception)? Are ADRs
indexed with accurate revision cross-links? Are operator-facing refusal and
error messages specified where operators depend on them?

## Output

Cite real files, kinds, commands, and ADRs; use current vocabulary; invent
nothing. Structure:

# Bootwright UX Review — <date>

## 1. Problem, Solution, Verdict

The problem restated, the solution shape Bootwright gives it, and your single
highest-leverage recommendation.

## 2. Findings by Lens

Per finding: **Evidence** (paths/commands), **Problem**, **Operator impact**,
**Proposal** (or "right as-is" with the reason). Order by user impact.

## 3. Deliberately Unchanged

What you considered and kept, each with the reason it survived.

## 4. Prioritized Plan

**Now** (docs, examples, diagnostics — no contract change), **Next** (spec
tightening and contract clarifications that need agreement), **Later**
(schema/CLI shape changes gated on an ADR). Per item: **Change**, **Why** (the
operator problem), **Evidence**, **Artifacts** (exact docs/specs/ADR/example
paths), **Validation** (which guard tests or gates prove it), **Acceptance
criterion**. Definition artifacts only — mark anything requiring code as
**handoff** for the implementation prompts, stating the contract it must meet.
End with the single first change that buys the most clarity for the least
risk.

## Constraints

- Desired-state YAML is the product API; generated files are outputs, not edit
  points; every snippet must be safe to commit.
- Stay inside Bootwright's scope and provider neutrality; keep secrets out of
  versioned content; keep the safety model fail-closed.
- Prefer fewer, stronger recommendations; every one passes the Aggregation
  test; when the current state is right, say so briefly and move on.

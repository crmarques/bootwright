# Specs, UX, and Desired-State Audit

You are a product-minded staff engineer auditing **Bootwright** — a desired-state
orchestrator that provisions fleets of OpenShift and OKD clusters from bare or
virtualized substrates to installed clusters. Desired-state YAML is the
user-facing API.

Pressure-test the current user-facing contract and turn the findings into a
prioritized improvement plan. Give equal weight to three lenses:

1. **Operator UX.** A real operator goes from a fresh checkout to an installed
   cluster by following the CLI, docs, and examples — without learning repo
   internals.
2. **Authoring.** Input files are complete enough to express real fleet intent,
   simple enough to write by hand, and structured so a user predicts where each
   fact belongs.
3. **Definition quality.** Specs constrain behavior precisely enough that two
   implementers would agree on every accept/reject case, while leaving room for
   future providers, topologies, and install modes.

This third lens is what this prompt owns. For a from-scratch, three-alternatives
*rethink* of the CLI and schema, use `cli-schema-ux-rethink.md` instead; here the
job is to audit the *current* contract and produce an actionable plan.

Out of scope: line-by-line code review, package structure, performance, and
refactors unless they explain a user-visible gap. Do not edit files unless the
user explicitly asks for follow-up changes.

## The Two Tests Every Proposal Must Pass

1. **Out of the box.** Would this occur to someone reasoning from the operator's
   problem rather than from the current code? Don't stop at "docs match code" or
   "the validator accepts it" — ask whether the user-facing model is the *right*
   model.
2. **Aggregation.** State the net gain in one sentence: which operator error,
   confusion, duplicated fact, or ambiguous accept/reject rule disappears, and at
   what cost in explicitness or risk. A tighter spec that makes a fuzzy rule
   enumerable aggregates even when no schema changes. A change that swaps one
   acceptable shape for another, equally acceptable shape does not — reject it and
   say so. Difference is not improvement.

"No change needed" is a valid, valuable finding when the evidence supports it.
Listing what you deliberately leave alone proves the survivors earned their place.

## Ground Yourself

The repository is the source of truth; load current definitions instead of
relying on memory, and do not anchor findings to older vocabulary. Read until you
have enough evidence, then stop:

1. `AGENTS.md` and `.agents/README.md` for load order and operating rules.
2. `specs/README.md` and `specs/index.md`, then the task-relevant specs and ADRs —
   especially the desired-state schema, CLI contract, UX principles, and accepted
   decisions.
3. `README.md` and `docs/` for the taught workflow.
4. User-authored examples, scaffolds, and E2E fixtures under `examples/` and
   `test/` that represent canonical authoring shapes — start from the smallest
   single-node case.
5. Current CLI help, validation output, or rendered tree only to verify what the
   user experiences.

```bash
git status --short
rg --files specs docs test examples .agents
rg -n 'apiVersion:\s*bootwright|^kind:' specs examples test
```

Run validation or tests only if the toolchain is already present. Do not install
dependencies.

## Durable Guardrails

Verify in the current specs before relying on these; do not propose anything that
violates them:

- **Scope.** Direct provisioning to installed clusters plus cluster-bound
  bootstrap add-ons. Day-2 fleet content publication lives elsewhere.
- **Product API.** Desired-state YAML stays declarative, idempotent, typed,
  deterministic, and reproducible. Generated artifacts are readable debugging
  outputs, not user edit points.
- **One owner per fact.** Lower-layer objects (`Machine`, `InfraProvider`,
  `InfraComponent`, provider inventories) own reachability, capabilities,
  services, and substrate facts and must not refer upward to consumers
  (`ContainerCluster`, `StorageCluster`, future cluster types). Consumer intent
  references downward.
- **Provider neutrality.** Keep abstractions open for libvirt, bare metal,
  vSphere, OpenShift Virtualization, and future substrates; hide supplier and BMC
  variation behind capabilities and adapters.
- **Secrets.** Credentials, kubeconfigs, pull secrets, private keys, and tokens
  never appear in versioned content, examples, snippets, or recommendations.
- **Clean break.** While the API is `v1alpha1`, propose clean breaking
  improvements freely — but never migrations, aliases, compatibility shims, or
  legacy examples. State why each break is worth it and what tests or fixtures
  must change.

## Posture

This is a design critique that ends in a plan, not a backlog dump. Take a position
and defend it. Each proposed change carries a clear user-visible outcome, repo
evidence, affected artifacts, a validation approach, and an acceptance criterion.

Ask the hard questions: can an operator author desired state from the problem they
have, or must they understand mechanics first? Does each kind, file, and field
earn its place? Are common changes small, bounded edits? Does validation explain
the model or only reject bad YAML? Are there missing first-class fields that users
will route through overrides or generated files, or fields that expose tool
internals instead of intent? Does the design scale from a lab to a multi-cluster,
multi-provider fleet?

## Provocations

Use these to find the highest-impact gaps, not as a checklist.

**Operator journey.** A new user runs CLI help — what do they naturally run next?
Walk checkout → first converged cluster, counting hidden prerequisites (SSH keys,
pull secrets, provider access, BMC reachability, DNS, state dirs, secret stores);
where is the first cliff? Does help text reveal the task sequence, required
inputs, dry-run behavior, output paths, and destructive actions? Pick three
plausible failures across load/validate/render/apply: for each, what does the user
see, what do they rerun, what state remains, what cleanup is safe? Can CI run
non-interactively with machine-readable failure and no prompts for destructive or
long operations? Inspecting the generated tree, can a user tell "yours to edit"
from "ours to regenerate" by names and paths?

**Authoring.** Count the concepts a user must hold to author a working cluster —
which are real domain concepts and which are tooling artifacts to remove, combine,
rename, or scaffold? For each major fact, name the one owning kind and file, and
flag duplicates, overrides, or hidden defaults. What is the smallest safe input
that installs one cluster, and does the repo teach that shape before advanced
cases? Can a user start compact and expand only for multi-provider, disconnected,
external-service, or special-networking needs? Walk a real provider swap and a
real mode swap (connected/disconnected, proxied/direct, managed/external,
single/multi-node): list the files that change, those that must not, and any edit
that reveals a layer leak.

**Spec and validation.** Pick non-trivial rules: does the spec give enumerable
accept/reject behavior, or describe a behavior without constraining it (a spec
defect)? Prefer explicit structural choices over "field absent means special
behavior." Can users predict how references resolve, where names are scoped, and
what error a missing/ambiguous reference yields? Are overrides and pass-throughs
becoming the normal path for missing schema? Pick one provider, capability,
topology, or install mode the specs want to support — what authoring surfaces must
change, and does it require shotgun edits across unrelated layers? Does validation
name the owning field, the invalid value, the rule, and the smallest fix? Are
secret and trust references usable without committing secret bytes?

## Output Format

Cite real files, kinds, commands, examples, and ADRs. Invent nothing. Use current
project vocabulary.

# Bootwright Specs, UX, and Authoring Audit

## 1. Executive Summary
Three to seven bullets ordered by user impact; each names the artifact, the impact,
and the proposed plan move. Lead with the single highest-leverage change.

## 2. First-Five-Minutes Journey
A new user's path from checkout to first cluster using the current CLI, docs, and
examples. Mark every undocumented prerequisite or out-of-band artifact. End with a
verdict: tractable, painful, or broken.

## 3. Desired-State Mental Model
Per current kind or top-level authoring unit: **Owns** (one sentence),
**Predictability** (would a user know this fact belongs here), **Cognitive cost**,
**Change** (one concrete improvement, or "none — already right"). Then state
whether the layer decomposition is load-bearing or accidental.

## 4. Input File and Schema Design
Per canonical example/scaffold/fixture set: **Path**, **Purpose**, **Strengths**,
**Mixed concerns or mechanics leaks**, **Simplification or completeness change**.
Call out missing minimal examples, overlap, and layouts that hurt review or reuse.

## 5. CLI, Validation, and Safety
Per issue: **Severity** (Critical/High/Medium/Low), **Evidence** (command, help,
spec, doc, example path), **Problem**, **User impact**, **Recommendation**. Cover
help text, dry-run, scoping, destructive-action confirmation, non-interactive use,
output locations, error actionability, secret/trust handling, and generated-output
boundaries.

## 6. Swap and Evolution Invariants
For provider/substrate swap, connected↔disconnected, proxied↔direct,
managed↔external, single↔multi-node: the files that should change, those that must
not, and any edit that reveals a schema leak.

## 7. Spec Design Critique
Step back from coherence: does each kind/file earn its existence; are common tasks
small bounded edits; is missing intent modeled, documented as external, or truly
out of scope; does the schema scale to larger fleets and future providers; where
does the API leak implementation detail; which extension points are clean and
which need reshaping. Show target authoring shape or UX flow only if you recommend
changing file layout, scaffolds, command flow, or examples — keep it concise and
grounded in current vocabulary, marking new commands/flags as proposals.

## 8. Deliberately Unchanged
What you considered changing and chose to keep, each with the reason it survived —
proof the proposals aggregate rather than churn.

## 9. Prioritized Plan
Group into **Now** (docs, examples, help text, naming, diagnostics — no schema/CLI
shape change), **Next** (validation, safer defaults, owned-field defense,
spec/behavior drift — needs agreement, near-term, testable), **Later** (schema,
file-layout, or CLI-model changes blocked on a design decision). Per item:
**Change**, **Type** (Clarify / Tighten / Reshape / Evidence gap), **Why** (the
user-visible problem), **Evidence** (spec/doc/example/command path), **Artifacts**,
**Validation**, **Acceptance criterion**. End with the smallest coherent first
follow-up change.

## Constraints

- Cite current repo evidence; use current vocabulary and kind names; keep
  recommendations aligned with the current specs and operating rules.
- Treat user-authored YAML as the product API and generated artifacts as outputs.
- Prefer schema and CLI improvements that make authoring simpler and more complete
  without coupling to implementation detail; keep every snippet safe to commit.
- Every recommendation must pass the Aggregation test. Prefer fewer, stronger
  recommendations; when something is already right, say so briefly and move on.

# CLI and Input Schema UX Rethink

You are a product-minded principal engineer rethinking **Bootwright** from first
principles. Bootwright automates declarative, idempotent provisioning of fleets
of OpenShift and OKD clusters from bare hardware or virtualized substrates to
installed clusters. Desired-state YAML is the user-facing API; the CLI carries an
operator from a fresh input set to "desired state achieved."

Your job: surface the boldest improvements to the operator experience — the CLI
and the user-authored schemas — that you can actually defend. Think past what
exists. Then gate every idea on whether it *aggregates*: removes cognitive load,
prevents an error class, unlocks a real scenario, or sharpens fact ownership. A
proposal that only trades one acceptable shape for another, equally acceptable
shape is noise — name it and drop it.

This is a design critique and proposal. Do not edit files unless the user
explicitly asks for follow-up implementation.

## The Two Tests Every Proposal Must Pass

1. **Out of the box.** Would this occur to someone who started from the operator's
   job instead of from the current code? If you are only reshuffling existing
   concepts, push harder before writing it down.
2. **Aggregation.** State the net gain in one sentence: *which* operator error,
   confusion, friction, or duplicated fact disappears, and at *what* cost in
   explicitness or risk. If you cannot, the change is non-aggregating — reject it
   and say so, even if the new form is "cleaner." Difference is not improvement.

Reject loudly. Listing what you deliberately did **not** change, and why, is as
valuable as the proposals — it proves the survivors earned their place.

## Ground Yourself

Load current repository facts before judging. Read until you have enough, then stop:

1. `AGENTS.md`, then `.agents/README.md`.
2. `specs/README.md` and `specs/index.md`, then the specs that define objectives,
   ownership, CLI contract, safety, and generated-output boundaries — typically
   `specs/domain.md`, `specs/state-model.md`, `specs/architecture.md`,
   `specs/security.md`.
3. `README.md` and `docs/` for the taught workflow.
4. Canonical examples and E2E fixtures under `examples/` and `test/` that show how
   users actually author inputs — start from the smallest single-node case.
5. Current CLI help, validation output, or rendered tree only to verify what an
   operator literally sees.

```bash
git status --short
rg --files specs docs test examples .agents
rg -n 'apiVersion:\s*bootwright|^kind:' specs examples test
```

## Guardrails

Apply the Core Invariants in `/AGENTS.md` (scope, provider neutrality, product API,
drive official tools, secrets, output routing, clean-break `v1alpha1`, definitions);
verify their current form in `specs/`. Schema/UX-specific additions:

- **One owner per fact.** Never improve UX by duplicating a fact across layers.
  Pressure-test *attribute* ownership, not just kind ownership: lower-layer
  objects (`Machine`, `InfraProvider`, `InfraComponent`, provider inventories)
  expose their own reachability, capabilities, services, and substrate facts and
  must not know, select, or refer to upper-layer consumers (`ContainerCluster`,
  `StorageCluster`, future cluster types). Consumer intent references downward.
- **State checking.** The CLI must include a well-named, non-mutating command for
  comparing selected desired state with the recorded last apply. Do not accept a UX where
  users must infer drift from `apply`, `destroy`, logs, or generated files.
  Evaluate behavior both with and without the destructive-override flags (apply
  `--converge-drifted` / destroy `--force`) for commands that support them; they
  must never make the `diff` command mutate or hide drift.

## Posture

Treat the current CLI and schema as evidence, not as constraints. No kind, field,
command, default, or file earns its place by existing. Start from the operator's
job and the best possible UX, then ask which current definitions deserve to
survive a comparison with credible alternatives. Take a position; prefer one
strong, concrete recommendation over a hedge.

Judge alternatives against: can an operator know what to do next and recover from
mistakes without reading code; can inputs describe desired state in natural
language with predictable ownership; does the model stay declarative, safe,
automatable, and provider-neutral; does it cut concepts and make common edits
small and bounded; can real install scenarios be expressed without generated
files, overrides, or hidden side channels; do commands, flags, kinds, fields,
references, and labels use the words an operator would naturally reach for.

## Provocations

Use these to break out of the current shape, not as a checklist. Spend effort
where the operator pain is highest.

**Operator journey.** From an empty context, what are the first three commands —
and is that sequence discoverable, or does it require knowing Bootwright
internals? Where is the first cliff (SSH keys, pull secrets, provider access, BMC
reachability, DNS, secret stores)? After a partial apply failure: what does the
operator see, what resumes safely, what diagnoses, what cleans up, and what state
remains? Where do the validate / plan / render / apply / status / destroy
mutation boundaries sit, and can a user predict exactly what mutates? Does the
same flow run in automation — JSON output, no prompts, explicit approval for
mutations, stable exit codes? Which command names should be kept, renamed,
collapsed, or split? Which command lets the operator ask "does live state match
this desired state?" without mutation, and is its name better than overloading a
converge or status command? If the whole selected cluster is absent, does it say
that succinctly; if the cluster exists, does it report material drift such as
missing or undeclared Ceph pools, add-ons, VMs, services, endpoints, or storage
exports?

**Authoring.** What is the smallest safe input that installs one useful cluster,
and does the repo teach that shape first? Can users start compact and expand only
for multi-cluster, multi-provider, disconnected, proxied, external-storage, or
managed-service needs? For each major fact, can a user predict the one owning
object and file — or has an upper-layer concern leaked downward? Which fields
expose `openshift-install`, Ansible, Redfish, Ceph, or filesystem mechanics
instead of operator intent? Which common intents have no first-class field and so
become overrides, raw manifests, or generated-file edits? Do the file boundaries
support review, copy/paste reuse, provider swaps, and mode swaps?

**Naming and language.** Treat naming as UX. Is the name the word an operator
would search for or type? Does it describe intent, not mechanics? Is one concept
named identically across CLI, YAML, docs, errors, rendered files, and status? Does
it scale from single-cluster to fleet, storage, add-on, and disconnected
workflows? Rename only when the current name is ambiguous, implementation-shaped,
stale, inconsistent, or likely to push facts to the wrong place — and the clean
break clearly buys discoverability, readability, or consistency.

## Schema Economy

Treat every authored line as a cost. Classify each kind, section, and major field
in scope, grounded in real usage evidence (specs, API types, validation,
rendering, examples, fixtures, docs, CLI behavior). A field present in a type but
not meaningfully validated, rendered, taught, or needed by a core workflow is
suspect until defended.

- **Keep** — expresses intent that cannot be inferred safely.
- **Default** — should vanish from examples behind a clear, *inspectable* default.
- **Derive** — computable deterministically from another owned fact.
- **Replace** — right concept, wrong name, shape, or owner.
- **Remove** — unused, redundant, mechanics-shaped, or not worth its cost.

When you propose a default, say where the operator inspects the effective value
and how validation prevents surprise. Defaults shrink the file set; they must not
hide cross-object bindings or scope selection. Aim for the smallest complete input
set that still teaches the object graph — a new user should see which objects bind
together and which behavior emerges only after normalization.

## Alternatives

Develop three credible alternatives, then choose one:

- **Conservative** — keep the kind model; improve naming, examples, CLI flow,
  validation, scaffolding.
- **Reshaped** — same scope; reorganize commands, files, kind boundaries, or
  defaults around a clearer operator journey.
- **Ambitious** — the cleanest `v1alpha1` model you would actually want, even with
  breaking schema or CLI changes.

For each: what the operator experience becomes, what the user authors, which
artifacts change, what gets simpler, which schema/fields/files become unnecessary,
which names change and why, which defaults enter the model and how users inspect
them, and what gets riskier or less explicit — then accept or reject it, applying
the Aggregation test.

Pick one and defend it as a coherent whole, not a bag of unrelated tweaks. If the
best answer is the current state plus focused refinements, defend *that* by
showing why larger changes do not buy enough UX, safety, clarity, or elegance.

## Evidence Standards

- Cite real specs, docs, examples, fixtures, commands, kinds, and fields.
  Distinguish proven findings from hypotheses. Invent nothing.
- To remove or replace a field, cite evidence it is unused, redundant, surprising,
  mechanics-shaped, or better owned elsewhere.
- To keep a definition, give the clear reason: irreducible intent, safer default,
  provider neutrality, understandable binding, lower risk, or no real gain from
  alternatives.
- To change the CLI, define the mutation boundary, automation behavior, and
  recovery path. Include the destructive-override flags (apply `--converge-drifted`
  / destroy `--force`) vs. their absence where relevant. To add or rename a
  desired-vs-real state-comparison command, define its non-mutating
  contract, report granularity, exit codes, JSON shape, and how it differs from
  `status`, `render`, `apply`, and `destroy`. To change a name, give old → new,
  affected surfaces, the user-facing benefit, and the validation/docs/examples
  that must change.

## Output Format

# Bootwright CLI and Input Schema UX Rethink

## 1. Objective and Verdict
Restate Bootwright's objective, the core operator jobs, and what "desired state
achieved" means. Lead with your single highest-leverage recommendation.

## 2. Current Experience Diagnosis
The current CLI journey and authoring model, with the biggest UX and schema
problems ordered by user impact and backed by evidence.

## 3. From-Scratch Operator Journey
The ideal flow from empty context to achieved desired state — create/import,
validate, materialize secrets, preview effective state and rendered output,
compare desired state with the recorded last apply using non-mutating
`diff --recorded` (or live `diff`), converge infra then clusters (storage and
add-ons included), monitor and
inspect, access, recover from failure, destroy/reset safely. Use current commands
where already right; mark proposals as proposals.

## 4. Authoring Critique
Per kind/file/major field in scope: keep, rename, combine, split, scaffold,
default, derive, replace, or remove — each with the aggregation gain or the reason
to leave it alone. Call out mechanics leaks, missing first-class intents,
dangerous vs. desirable defaults, and the minimal useful file set.

## 5. Alternatives Considered
Conservative, reshaped, ambitious — concrete enough that a maintainer can tell
what changes, each with an accept/reject verdict.

## 6. Recommended Alternative
The chosen design: target CLI flow (mark read-only vs. mutating); target input
layout and schema posture; naming changes; schema keep/default/derive/replace/
remove; key behavior changes; expected user-visible improvements; `v1alpha1` break
implications; risks and mitigations. Explicitly include the desired-vs-real
state-comparison command name and why it is not confused with convergence or cleanup.

## 7. Target Shape
If you change input organization or schema, show a concise, secret-free file tree
and representative YAML using current vocabulary, with defaults shrinking the
authored files and bindings that must stay explicit called out. If you change the
CLI, show the command flow with read-only vs. mutating noted.

## 8. Deliberately Unchanged
What you considered changing and chose to keep, each with the reason it survived —
proof the proposals are aggregating, not cosmetic.

## 9. Implementation Plan
**Now** (docs, examples, help text, diagnostics, scaffolding, validation),
**Next** (schema/CLI changes that need agreement but are near-term and testable),
**Later** (larger model changes blocked on open decisions). Per item: **Change**,
**Why** (user problem), **Evidence** (paths/commands/kinds/fields), **Artifacts**,
**Validation**, **Acceptance criterion**. End with the single first follow-up
change that creates the most clarity for the least risk.

## Constraints

- Stay inside Bootwright's scope; treat desired-state YAML as the product API and
  generated artifacts as outputs, not edit points.
- Preserve one-owner fact ownership; keep provider and BMC variation behind
  capabilities and adapters; keep every snippet safe to commit.
- Prefer lean, meaningful schema over exhaustive knobs; default aggressively but
  keep an inspectable effective state; keep the final file set as small as it can
  be without hiding bindings.
- Require a non-mutating state-comparison UX that reports absence at the right
  level and detailed drift only when live roots exist; do not let the
  destructive-override flags (apply `--converge-drifted` / destroy `--force`)
  change read-only behavior.
- Every recommendation must pass the Aggregation test. Prefer fewer, stronger
  recommendations, and say plainly when the current state should stand.

# Usage Flow Review: Scenarios to Side Effects

You are a senior engineer reviewing **Bootwright**, a desired-state orchestrator
that converges versioned YAML into OpenShift, OKD, and Ceph fleets — machines and
managed machine-OS installs, shared infrastructure services, container clusters,
storage clusters, exported storage surfaces, and cluster-bound bootstrap add-ons.

Start from a realistic advanced environment, generate the scenarios an operator
actually lives through, and trace each one through **all the code** — CLI, Go
packages, generated contracts, Ansible, scripts, Make, CI, examples, and tests —
hunting bugs, unimplemented intent, unhandled cases, missing or bypassable safety
gates, idempotency breaks, unexpected destruction, duplication, stale guidance,
and Go↔Ansible contract mismatches. The deliverable **is** the findings report
and a prioritized fix plan — not a meta-plan describing how you would review.

This prompt combines the end-to-end flow pass (`code-flow-review.md`), the
idempotency and destructive-safety audit (`idempotency-safety-audit.md`), and the
`apply`/`destroy` contract review (`apply-destroy-safety-contract.md`) into one
scenario-driven sweep. For static implementation quality use `code-review.md`;
for a deep security pass `security-audit.md`; for package boundaries
`architecture.md`; for schema/UX `ux-review.md`; for provisioning-graph
efficiency `provisioning-logic-review.md`; for ownership-matrix design
`state-lifecycle-scenario-review.md`.

Review-only by default — do not edit unless the user asks for fixes, and never
run real `apply`, `destroy`, provider, BMC, cluster, storage, disk, or cleanup
commands during the review. Use targeted read-only commands only.

## The Contract

One principle governs every verdict: **a state change happens only when the
operator explicitly asked for it.** Everything else follows.

- Read-only commands (`diff`/`plan`/`status`/`render`/`validate`/`preflight`/
  help/probes — confirm the current set) never mutate a provider, BMC, cluster,
  storage, disk, or durable record beyond a documented narrow runtime write.
- A command that finds drift, foreign ownership, unknown state, a failed probe,
  or any ambiguity it is not authorized to resolve **fails closed** — it mutates
  nothing and stops before the first side effect.
- Bare `apply` is the **safe reconcile** default: create missing, skip proven
  matches without re-running, converge reconcilable drift in place, fail closed
  on structural drift or foreign ownership. A previously-successful run must run
  end to end mutating nothing.
- `apply --mode create` is the **greenfield assert**: additionally fail closed if
  any selected object already exists. `apply --mode rebuild` is the only
  **break-glass** mode: rebuild drifted owned objects, create missing ones, leave
  matches untouched, never touch foreign objects, and bypass no lease,
  validation, secret, or `destroyProtection` gate. Data-loss rebuilds
  (managed-OS reinstall, owned-Ceph wipe) additionally require the
  `--authorize data-loss` gate.
- No matching object is ever destroyed to be recreated. A clean rebuild of a
  matching object is `destroy` then `apply`, never an `apply` flag.
- A destructive path runs only when the command, flags, and selected desired
  state authorize that exact scope. `--yes` skips confirmation only; names,
  labels, context, stale records, or "lab/dev" vocabulary are never
  authorization.
- `diff` is the non-mutating desired-vs-real check: live by default,
  `--recorded` compares against the last recorded apply offline, and only
  `--adopt` writes (folding live state into desired YAML plus history). A
  successful `destroy` drops the component entirely AND resets its convergence,
  install, and ownership records so a later `apply` recreates it.
- Every refusal is **actionable**: it states what was found, why it is unsafe,
  and the exact `bootwright …` command that proceeds intentionally.

When intent is unclear, the safe verdict is no-op, read-only report, safe
convergence, or fail-closed refusal — never mutation. When in doubt, classify a
path as **not authorized to destroy** and audit whether it fails closed.

## The Three Tests Every Finding Must Pass

1. **Trace.** No verdict without a concrete path: scenario input → CLI parse →
   selection/validation/normalization → planning, locks, records → generated
   contract → Ansible/script/external command → final side effect, citing
   `path:line` (or role/playbook/task) at each material step. Separate proven
   findings from hypotheses, and let a hypothesis stand only with the smallest
   step that confirms or rejects it. Invent no bugs, behavior, or requirements.
2. **Intent.** State whether the operator explicitly authorized the side effect.
   A destructive or mutating path without explicit intent is a safety bug even
   when it would usually affect a lab.
3. **Aggregation.** Name the net gain: which data loss, surprise mutation,
   contract break, repeated-work hazard, or recovery gap disappears, at what
   change cost. Difference is not improvement — reject churn and broad rewrites
   where a small guard, probe, record check, idempotency marker, or test proves
   the behavior, and never remove functionality, hide errors, or weaken
   validation to make a trace pass.

"No findings" is a valid outcome when the trace supports it — still list the
scenarios cleared, areas not reviewed, and checks that could not run.

## Ground Yourself

The repo is the source of truth; commands, flags, kinds, and records evolve, so
discover the present contract instead of trusting memory. Read until the
evidence supports concrete findings, then stop expanding scope:

1. `AGENTS.md` and `.agents/README.md` — operating rules and the catalog.
2. `specs/README.md`, `specs/index.md`, then the task-relevant specs — usually
   `domain.md`, `architecture.md`, `state-model.md`, `security.md`.
3. `.agents/knowledge/KNOWLEDGE.md` constraints for this area and the ADR
   decision table in `specs/adr/README.md` — an accepted ADR may already fix the
   shape of a fix or record why the obvious alternative was rejected.
4. The live CLI help (`apply`, `destroy`, `diff`, `preflight`, `status`) and the
   Go that drives it: command setup, flag parsing, root/sudo gating,
   confirmation, selection and scope closure, strict decode, validation,
   normalization, rendering, planning, locks, leases, ledgers,
   ownership/install/convergence records, status, and the apply/destroy runners.
5. The Ansible playbooks, roles, tasks, handlers, templates, vars, generated
   inventory contracts, provider adapters, and scripts the traced flows launch.
6. Make targets, CI workflows, examples, fixtures, golden files, and tests that
   claim the same behavior.

```bash
git status --short ; rg --files AGENTS.md .agents specs internal api ansible scripts test examples
go run ./cmd/bootwright apply --help ; go run ./cmd/bootwright destroy --help ; go run ./cmd/bootwright diff --help
rg -n 'apply|destroy|reset|cleanup|purge|wipe|remove|delete|undefine|format|mkfs|oc delete|ceph|PowerState|ResetType|InsertMedia|EjectMedia|changed_when|failed_when|creates:|removes:|check_mode|--yes|--mode|--authorize|--force|confirm|dry-run|safety|ownership|install-record|ledger|lease|idempot|foreign|drift|fail.?closed' internal cmd api ansible scripts Makefile specs docs examples test .github
go test ./internal/...            # or narrower packages when scope is narrow
ansible-playbook --syntax-check <playbook> ; ansible-lint ; shellcheck scripts/* test/**/*.sh   # when available
```

Report useful checks that could not be run, with the reason.

## Guardrails

Apply the Core Invariants in `/AGENTS.md` (scope, provider neutrality, product
API, drive official tools, secrets, output routing, clean-break `v1alpha1`,
state-change authorization, definitions); verify their current form in `specs/`.
Prompt-specific additions:

- **One owner per fact.** Do not patch drift or safety by duplicating a fact
  across layers; route checks through the owning kind, generated contract, or
  durable runtime record.
- **Go↔Ansible split.** Go owns CLI, input loading/validation, normalization,
  rendering, storage intent, planning, locks, ledgers, status, orchestration;
  Ansible executes mutations from rendered contracts and must not infer
  selection, cleanup scope, or operator authorization from names or filesystem
  layout.
- **Shared comparison primitive.** `apply`'s mode preflight and `diff` must
  classify objects through the same path (per-task converge-safety
  classification aggregated per object) so the drift a state check reports is
  the drift an apply acts on — verify the two cannot disagree.

## The Baseline Environment

Ground every scenario in a representative **advanced** estate, not a toy
single-cluster one — the contract must hold where scope closure, stretch
arbitration, and nested substrates interact. Take as the baseline a two-site
Environment (two data centers declared as sites, machines placed per site) with:

- one **stretched Ceph** storage cluster arbitrated across both DCs, with
  pools, filesystems, object gateways, and exports riding on it,
- two **bare-metal OpenShift** clusters in each DC (four in all), and
- one **virtualized OpenShift** cluster nested inside each bare-metal OCP (four
  in all, each riding on its host cluster's substrate),

plus the supporting graph: machines and images, managed-OS installs, entitlements,
secrets, network configs, infra services, and add-ons. Derive scoped selections —
a single DC, one cluster, one machine, a nested guest, or the stretched Ceph
alone — so scope narrowing, cross-DC blast radius, and host-to-guest ordering
are exercised, never assumed.

The full `examples/` set is the mandatory drift and regression baseline
alongside it: enumerate the example directories, inventory the kinds and flows
each represents, and compare them against each other and the baseline to surface
stale patterns, duplicated flow logic, provider leaks, and missing coverage. If
the user supplies scenario files or input directories, fold them in as primary
scenarios — but never sample one happy path and generalize.

## Step 1 — Map and Cross-Check the Command Contract

Enumerate every flag `apply`, `destroy`, and `diff` currently expose. For each,
in a table: its documented meaning, whether it changes state or only gates or
skips, and whether **specs, docs, CLI help, and code agree**. Reason by role so
the map survives renames, binding each role to the present flag name from
`--help`: the safe-reconcile default, the greenfield assert, the break-glass
drift rebuild, the data-loss gate, the confirmation skip, scope/selection
(stage, through, clusters, machines), ownership/reachability authorization
tokens, `destroy` stages and `destroyProtection`, and the diff modes. A place
where the four sources disagree, a flag's effect is vaguer than its power, or
two flags overlap ambiguously is a finding — as is guidance (specs, docs, help,
reusable prompts, examples, tests) that could train future code toward unsafe
behavior.

## Step 2 — Generate the Scenarios

Cover the lifecycle an operator lives through on the baseline, crossed with the
flag combinations from Step 1 — destructive-override and data-loss flags both
present and absent — and with scoped vs. full selection. This seed list is the
floor, not the ceiling — extend it:

- First apply, full success; first apply failing partway (before records; after
  some side effects; at each distinct stage a failure can land).
- Re-apply unchanged (must be a proven no-op end to end); re-apply with
  reconcilable drift vs. structurally-immutable change vs. data-loss change.
- Apply → destroy → apply again (records cleared so objects recreate, not
  skip-as-matched; `--mode create` no longer refuses).
- Live state drifted or deleted out of band, then apply to reconcile; `diff`
  before and after in live and `--recorded` modes.
- Same-name reuse for a different identity; foreign or shared resource present;
  stale record with a gone or repurposed live object; partially completed
  install; unreachable node.
- Scoped to one DC while the stretched Ceph spans both — apply/destroy of that
  DC's clusters must not touch the cross-DC stretch peer, the arbiter, or the
  other DC's mons and OSDs.
- Destroy or rebuild a bare-metal host OCP while the virtualized OCP nested on
  it is still live — the host-to-guest substrate dependency must gate, not
  silently strand or wipe the guest.
- Read-only flows (`diff`, `plan`, `status`, `render`, `validate`, `preflight`)
  against every state above — including with destructive-override flags
  present, which they must ignore, reject, or strictly no-op.
- Confirmation aborted; non-interactive runs; `--yes` present with each gate.

## Step 3 — Trace Each Scenario Through the Full Flow

No verdict without a trace. Walk each scenario through these checkpoints,
recording the matrix below:

1. **Command classification and parse.** Read-only, convergent, or destructive?
   Flag resolution, root/sudo gating only where needed, local validation before
   sudo prompts or side effects.
2. **Input selection and scope closure.** Which files and resources load — and
   which are excluded? Strict decode rejects unknown fields; each fact lives in
   its owning kind; `--clusters`/`--stage`/context selection bound the mutation
   set.
3. **Normalization and validation.** Defaults visible, deterministic, and valid
   across OpenShift/OKD, connected/disconnected, SNO/multi-node, and provider
   paths; reject cases match the specs; errors name the owning object and field;
   effective state still means what the user authored.
4. **State classification and authorization.** `missing`, `match`, `drift`,
   `foreign`, `undeclared`, `partial`, `stale`, `unknown` — and the
   authorization decision each drives. Probes reliable for every class; desired
   hashes and owner identity derived, non-secret, namespaced by context; stale
   records never authorize deleting a current foreign resource.
5. **Planning, locks, records.** Task-graph ordering and dependencies, leases
   and resource locks acquired before mutation, scoped applies avoiding
   context-wide sweeps, install records and ledgers written and cleared at the
   right moments.
6. **Rendering and the Go↔Ansible contract.** Every var, path, inventory group,
   host target, and role input Ansible consumes: names match role expectations,
   Go values are not recomputed differently, desired-state values are never
   interpolated unquoted into shells, generated paths stay in context-owned
   roots.
7. **Ansible and script execution.** Which tasks actually mutate; read-only
   probes `changed_when: false`; cleanup guarded by explicit authorization,
   ownership, scope, and live state; handlers fire only on real change;
   `no_log`, file modes, and precise `creates`/`removes`.
8. **External commands.** `openshift-install`, `virsh`, `oc`, `kubectl`,
   `ceph`, `cephadm`, `podman`, `systemctl`, `nmstatectl`, disk tools, Redfish
   calls, and file removals use explicit args, safe scopes, context,
   cancellation, and clear errors.
9. **Diff and converge/force pairs.** Trace `diff` (live, `--recorded`,
   `--adopt`) against the scenario: absent selected roots reported succinctly,
   granular drift once the root exists (missing declared pools, filesystems,
   gateways, add-ons, VMs, services; undeclared live resources), and no mode
   but `--adopt` writing. Trace `apply` under all three modes and `destroy`
   with and without `--force`: which refusal disappears, which mutations become
   allowed, and which read-only guarantees remain.
10. **Final state and rerun.** After success, failure, interruption, or aborted
    confirmation: what records exist, and does the same command no-op, resume
    safely, or fail closed? Refusals name the exact command to proceed —
    missing or wrong remedial guidance is itself a finding.
11. **Tests.** Which tests already prove the behavior, and which are missing.

**Trace matrix** (one row per scenario × flag variant): **Scenario** · **Command
intent** · **Environment state** · **Destruction authorized?** · **Go path**
(`path:line`) · **Generated contract** · **Ansible/script path** · **External
commands** · **Final side effect** · **Verdict** (safe / unsafe / unproven /
out of scope).

## What to Look For

Pick the lenses with teeth for each scenario; do not run every bullet
mechanically.

**Unexpected destruction — highest priority.** Any read-only or convergent flow
that can delete, wipe, reset, recreate, or replace provisioned resources without
explicit intent: VM undefine with storage removal, disk partitioning or
formatting, Redfish reset or media eject, `oc delete`, namespace deletion, Ceph
removal, package removal, service disabling, context-wide cleanup, broad file
deletion, install-runtime deletion.

**Missing or bypassable gates.** Unknown state, missing records, stale hashes,
foreign resources, shared services, missing locks, failed probes, or absent
trust material that does not stop before mutation; overrides that broaden scope
silently, suppress reported drift, or turn a state check into a mutation;
destroy scope leaking beyond ownership and selection; storage sub-objects
(pool/filesystem/gateway/export) destroyed-and-recreated for drift that should
reconcile in place, or destructive paths leaving permissive state behind (e.g.
`mon_allow_pool_delete`).

**Correctness bugs and unhandled cases.** Resources not loaded, loaded twice,
or loaded outside the selection; missing or duplicate reference validation;
nondeterministic rendering; wrong machine, MAC, hostname, endpoint, DNS, proxy,
mirror, trust, or certificate data; ordering, resume, install-record, or ledger
mistakes; locks missing shared hosts or BMC targets; errors swallowed, vague, or
reported after side effects; edge states (partial, stale, foreign, absent) with
no code path at all.

**Idempotency and unnecessary work.** Tasks that mutate on every apply when
state matches; probes marked changed; restarts from false changes; repeated
network, firewall, DNS, BMC, VM, storage, or cluster commands without a
desired-state change; artifact rewrites triggering downstream mutation; broad
roles hiding independent no-op work.

**Intent drift.** Spec rules unenforced in code; code behavior absent from
specs, docs, examples, or help; defaults that change meaning silently; stale
examples or tests; provider swaps requiring edits above the provider-owned
layer; connected/disconnected/SNO/multi-node paths diverging without a
documented reason.

**Duplication and quality.** Duplicated validation, normalization, rendering,
planning, command construction, path, redaction, or status logic; a domain rule
reimplemented in CLI/renderer/planner/tests instead of one owner;
responsibilities on the wrong side of the Go/Ansible boundary; unused code,
fixtures, or examples touched by no current flow (default to deletion); secret
bytes in versioned input, output, logs, or tests; missing `no_log` or
`--sensitive` gates; unsafe sudo, interpolation, path traversal, temp files, or
cleanup.

**Scripts, Make, CI, tests.** Targets named `check`/`validate`/`test`/`prepare`
hiding destructive cleanup or mutation; unconstrained cleanup roots; CI teaching
unsafe flags or leaking secrets; missing tests proving: second identical apply
is a no-op, read-only commands do not mutate, ambiguous state fails closed,
destroy scope is bounded, `--yes` is not an override, confirmation abort is a
no-op, records cannot authorize foreign deletion, roles rerun safely.

## Durable Guardrails

The contract must hold for flags, kinds, and providers not yet written. Propose
the smallest mechanisms that make future unsafe code *fail a check*, not merely
violate a convention:

- **Test matrix.** Extend the table-driven safety suite keyed on (command, flag
  combination, starting state, selected scope) asserting the expected verdict —
  no-op, fail-closed refusal, or authorized mutation — for every case traced
  above, built on the advanced baseline so stretch arbitration, cross-DC scope
  closure, and host-to-guest ordering are exercised.
- **Fail-closed default.** Where a registry or classifier routes mutations, a
  meta-test asserting every registered mutating path has an authorization
  classification and an actionable refusal — so a new path that forgets one
  fails the build.
- **Agent instructions.** Record any new invariant where future implementers
  will read it: `AGENTS.md`/specs for the rule, a constraint row in
  `.agents/knowledge/KNOWLEDGE.md`, and the checklist expectation that new
  state-changing surfaces extend the matrix. Specs stay the source of truth;
  docs link, not copy.

## Output Format

Cite concrete files, packages, functions, roles, tasks, commands, tests, specs,
and generated artifacts. Use current project vocabulary. Keep secrets out of
every snippet.

# Bootwright Usage Flow Review

## 1. Verdict
Three to seven bullets ordered by risk. Lead with any path that mutates or
destroys without explicit intent, or state clearly that none was found and name
the top residual risk.

## 2. Reviewed Scope and Assumptions
Baseline and scenarios generated; user-supplied inputs folded in; examples
enumerated; packages, roles, scripts, and tests reviewed; areas intentionally
not reviewed; missing facts and whether they block a verdict.

## 3. Contract Map
The per-command, per-flag table from Step 1, marking spec/docs/help/code
agreement and every incoherence or guidance drift.

## 4. Scenario Traces
The trace matrix from Step 3, including diff behavior and each
flag-present-vs-absent pair, with verdicts and error-message quality.

## 5. Findings
Severity order. Per finding: **Severity** (Critical/High/Medium/Low), **Type**
(Unexpected destruction / Fail-closed gap / Idempotency bug / Unnecessary
mutation / Bug / Unhandled case / Intent drift / Contract incoherence /
Go-Ansible contract / Destroy scope / Duplication / Script safety / Guidance
drift / Test gap), **Location** (`path:line` or role/task), **Scenario trace**,
**Evidence**, **Impact**, **Minimal fix**, **Validation**, **Fix readiness**
(ready now / needs user decision / needs more evidence).

## 6. Cleared on Trace
Behaviors that looked wrong but the trace confirmed correct, and nits declined
as below the aggregation bar — each with a one-line reason. Proof the findings
are real and a map of the surface already verified.

## 7. Durable Guardrails
The test-matrix, fail-closed-default, and guidance mechanisms proposed, and what
future regression each catches.

## 8. Fix Plan
**Now** (high-confidence correctness/safety fixes and regression tests small
enough to implement immediately), **Next** (hardening that needs a coherent
slice or short design pass), **Later** (larger design or UX changes needing
agreement). Per item: affected artifacts, smallest safe approach, aggregation
gain, validation, **Risk** (Low/Medium/High). End with the smallest coherent
first implementation slice and any open question that blocks a safe fix or
changes prioritization.

## 9. Checks Run
Commands run with a one-line result each; useful checks not run, with the
reason — missing tools, network, credentials, root, or destructive side
effects.

## Fix Mode (only if the user explicitly requests implementation)

Confirm selected findings only if the request is ambiguous; otherwise implement
the requested slice per the repo's `implementation-worktree` and
`implementation-validation` skills, following `definition-stewardship` for any
spec, doc, ADR, or guidance change. Keep each change scoped to the traced defect
and existing architecture, add the test that proves the verdict before or with
the fix, preserve generated-output and secret boundaries, and run the gate
`AGENTS.md` selects for the change intent after the edit set and any rebase.
Report per the repo handoff rules.

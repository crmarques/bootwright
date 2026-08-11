# Apply/Destroy State-Change Safety Contract

Review **Bootwright**'s `apply` and `destroy` commands and every flag they expose.
Confirm the specs, docs, CLI help, and code agree on what each command and flag
means; generate the lifecycle scenarios an operator will actually hit; trace each
one through the code to prove behavior matches the documented contract; then close
every gap and make the safe behavior durable for code that does not exist yet.

Deliver the review and the fixes, not a plan to review. Ground in the current repo
first — commands and flags evolve, so discover the present contract instead of
trusting memory.

## The Contract

One principle governs every verdict: **a state change happens only when the
operator explicitly asked for it.** Everything else follows.

- Read-only commands (`diff`/`plan`/`status`/`render`/`validate`/help/probes —
  confirm the current set) never mutate a provider, BMC, cluster, storage, disk,
  or any durable record.
- A command that finds drift, foreign ownership, unknown state, a failed probe, or
  any ambiguity it is not explicitly authorized to resolve **fails closed**. The
  complete records preflight stops before any run side effect; a task-local live
  gate stops before changing that target or consequence. Independent authorized
  branches may already have completed and are not rolled back.
- No matching object is destroyed to be recreated. Forcing a clean rebuild of a
  matching object is `destroy` then `apply`, never an `apply` flag.
- A destructive path runs only when the command, its flags, and the selected
  desired state authorize that exact scope. Names, labels, context, stale records,
  or "lab/dev" vocabulary are never authorization.
- Every refusal is **actionable**: it states what was found, why it is unsafe, and
  the exact command (with the flag) the operator runs to proceed intentionally.

When intent is unclear, the safe verdict is no-op, read-only report, safe
convergence, or fail-closed refusal — never mutation.

## Scope

This prompt owns the **`apply`/`destroy` command-and-flag contract**: that each
flag's documented meaning is coherent across specs/docs/help/code, that lifecycle
scenarios behave as specified, that refusals are actionable, and that the safety
contract survives future code. For scenario-file-driven idempotency auditing use
`idempotency-safety-audit.md`; for broad ownership/state pressure-testing and
safety-lock design use `state-lifecycle-scenario-review.md`; for end-to-end output
drift use `code-flow-review.md`. Prefer extending those over duplicating them.

## Ground Yourself

The repo is the source of truth. Read the current contract before judging it:

1. `AGENTS.md` and `.agents/README.md` for operating rules and the prompt catalog.
2. `specs/README.md`, `specs/index.md`, then `specs/state-model.md`,
   `specs/domain.md`, `specs/architecture.md`, `specs/security.md`.
3. Operator docs for `apply`/`destroy`/`diff` and the state lifecycle under `docs/`.
4. `.agents/knowledge/KNOWLEDGE.md` (constraints/semantics for this area) and the
   ADR decision table in `specs/adr/README.md`.
5. The live CLI help and the Go it drives: command setup, flag parsing, pre-run
   validation, root/sudo gating, confirmation, selection/scope, validation,
   planning, locks, ownership/install/convergence records, and the apply/destroy
   runners plus the Ansible/provider paths they launch.

```bash
git -C <repo> status --short
go -C <repo> run ./cmd/bootwright apply --help
go -C <repo> run ./cmd/bootwright destroy --help
go -C <repo> run ./cmd/bootwright diff --help
rg -n 'apply|destroy|converge-drifted|confirm-data-loss|expect-new|force|include-unowned|skip-unreachable|--yes|destroyProtection|foreign|drift|ownership|convergence|fail.?closed|record' specs docs cmd internal api ansible test
```

Do not run real `apply`, `destroy`, provider, BMC, cluster, storage, disk, or
cleanup commands during a review-only pass.

## Step 1 — Map And Cross-Check The Contract

Enumerate every flag `apply` and `destroy` currently expose. For each, in a table:
its documented meaning, whether it changes state or only gates/skips, and whether
**specs, docs, CLI help, and code agree**. Reason about flags by role so this
survives renames — bind each role to the present flag name from `--help`:

- The **safe-reconcile default** (bare `apply`): create missing, skip proven
  matches where a concrete probe supports it, let other tasks re-run
  idempotently, and fail closed on drift and foreign ownership without changing
  the affected target.
- The **greenfield assert** (e.g. `--mode create`): additionally fail closed if any
  selected object already exists or shows ownership evidence.
- The **break-glass drift rebuild** (e.g. `--mode rebuild`): rebuild only
  drifted owned objects, skip matches, never touch foreign; bypasses no
  validation, lease, secret, or protection gate.
- The **data-loss gate** (e.g. `--authorize data-loss`): required on top of a rebuild
  that discards data; never implied by a confirmation-skip flag.
- The **confirmation skip** (e.g. `--yes`): skips the prompt only; grants no
  override, scope widening, or ownership relaxation.
- **Scope/selection** (stage, through, cluster, machine selectors) and any
  ownership/reachability modifiers (e.g. `--authorize unowned-vms`, `--authorize unreachable-nodes`):
  confirm each narrows or widens exactly as documented and cannot broaden silently.
- `destroy` scope and `destroyProtection`: what each stage removes, what records it
  clears, and what protection refuses.

Flag any place where the four sources disagree, a flag's effect is undocumented or
vaguer than its power, or two flags overlap ambiguously — that is a finding.

## Step 2 — Generate The Scenarios

Ground every scenario in a representative **advanced** environment, not a toy
single-cluster one — the safety contract must hold at the topology operators
actually run, where scope closure, stretch arbitration, and nested substrates
interact. Take as the baseline a two-data-center Environment with:

- one **stretched Ceph** storage cluster arbitrated across both DCs,
- two **bare-metal OpenShift** clusters in each DC (four in all), and
- one **virtualized OpenShift** cluster nested inside each bare-metal OCP (four in
  all, each riding on its host cluster's substrate).

Derive scoped selections from this baseline — a single DC, one cluster, one
machine, a nested guest cluster, or the stretched Ceph alone — so scope narrowing,
cross-DC blast radius, and the ordering between a host cluster and the virtualized
cluster running on it are exercised, never assumed. Then cover the lifecycle an
operator lives through, combined with flags and state. This seed list is the floor,
not the ceiling — extend it:

- First apply, full success.
- First apply that fails partway (before records written; after some side effects;
  at each distinct stage a failure can land).
- Re-apply with unchanged desired state (must be a proven no-op).
- Re-apply with changed desired state (reconcilable drift vs. structurally-immutable
  change vs. data-loss change).
- Apply, then `destroy`, then apply again (records cleared so the object recreates,
  not skipped-as-matched).
- Real/live state drifted or deleted out of band, then a new apply to reconcile.
- Same-name reuse for a different identity; foreign or shared resource present;
  stale record with a gone or repurposed live object.
- Scoped to one DC while the stretched Ceph spans both — apply/destroy of that DC's
  clusters must not touch the cross-DC stretch peer, the arbiter, or the other DC's
  mons and OSDs.
- Destroy or rebuild a bare-metal host OCP while the virtualized OCP nested on it is
  still live — the host-to-guest substrate dependency must gate, not silently
  strand or wipe the guest.

Cross each state with the relevant flag combinations from Step 1 — including the
destructive-override and data-loss flags both present and absent — and with scoped
vs. full selection (single DC, single cluster, nested guest, and stretched-Ceph-only
scopes drawn from the baseline).

## Step 3 — Trace Each Scenario Against The Contract

No verdict without a trace. For each scenario walk:

1. CLI parse, flag resolution, root/sudo gate, pre-run validation.
2. Selected roots and scope closure (what is in and out of the mutation set).
3. State classification: `missing`, `match`, `drift`, `foreign`, `undeclared`,
   `partial`, `stale`, `unknown` — and the authorization decision it drives.
4. Plan, locks/leases, generated contract, Ansible/external side effects, and the
   records written or cleared.
5. Operator-visible output, exit code, and the recovery guidance on refusal.

Cite `path:line` (or role/task) for each material step. Judge every scenario
against the Contract above and mark it **safe**, **unsafe**, **unproven**, or
**out of scope**. Confirm each refusal names the exact command to proceed —
missing or wrong remedial guidance is itself a finding.

## Durable Guardrails

The contract must hold for flags, kinds, and providers not yet written. Propose and
install the smallest mechanisms that make future unsafe code *fail a check*, not
merely violate a convention — efficiency over breadth:

- **Test matrix.** A table-driven safety suite keyed on (command, flag combination,
  starting state, selected scope) asserting the expected verdict — no-op,
  fail-closed refusal, or authorized mutation — for every case traced above. Build
  it on the advanced baseline from Step 2 (stretched Ceph across two DCs, two
  bare-metal OCP per DC, one nested virtualized OCP each), not a single-cluster
  fixture, so it exercises stretch arbitration, cross-DC scope closure, and
  host-to-guest substrate ordering. New flags, kinds, and topologies extend the
  table; every `apply`/`destroy` flag needs a real matrix verdict and there is no
  exemption list. The suite is the regression net for the whole contract.
- **Provider mutation closure.** Require an exact machine-readable registration
  for every state-capable provider/boot/media/ownership task: path, task name,
  action, class, named safety surface, and ordered gate/mutation/evidence anchor.
  Treat every include or import as a state-capable delegated boundary unless its
  target is exhaustively registered. Meta-tests reject new, missing, dead, or
  unordered entries and unsupported providers.
- **Fail-closed default.** Every mutating operation classifies its authorization
  and defaults to refusal until explicitly authorized. Where a registry or
  classifier already routes mutations, add a meta-test asserting every registered
  mutating path has an authorization classification and an actionable refusal — so a
  new path that forgets one fails the build.
- **Agent instructions.** Record the contract where future implementers and agents
  will read it: the relevant invariant in `AGENTS.md`/specs, a constraint row in
  `.agents/knowledge/KNOWLEDGE.md`, and a checklist item that any new state-changing
  command, flag, kind, or provider must fail closed, be actionable, and extend the
  test matrix. Keep specs the source of truth; docs and guidance link, not copy.

## Findings And Plan

Order by safety impact. Per finding: **Severity** (Critical/High/Medium/Low),
**Type** (Unexpected mutation / Fail-closed gap / Contract incoherence / Unclear
error / Destroy scope / Test gap / Guidance drift), **Location**, **Scenario
trace**, **Evidence**, **Impact**, **Minimal fix**, **Validation**. Reject churn
where a small guard, record check, error-message fix, or test proves the behavior.

Group the plan into **Now** (safety fixes and regression tests to do immediately),
**Next** (guardrail and record/role hardening needing a coherent slice), and
**Later** (contract or UX changes needing agreement). End with the smallest first
implementation slice.

## Output

# Bootwright Apply/Destroy Safety Contract Review

## 1. Verdict
Three to seven bullets ordered by risk. Lead with any path that mutates without
explicit intent, or state clearly that none was found and name the top residual
risk.

## 2. Contract Map
The per-command, per-flag table from Step 1, marking spec/docs/help/code agreement
and every incoherence.

## 3. Scenario Traces
The scenarios from Step 2 with their traces and verdicts (safe/unsafe/unproven/out
of scope), including flag-present-vs-absent pairs and error-message quality.

## 4. Findings
Severity order, in the format above.

## 5. Durable Guardrails
The test-matrix, fail-closed-default, and agent-instruction mechanisms proposed or
installed, and what future regression each one catches.

## 6. Implementation Plan
Now / Next / Later, then the first slice.

## 7. Checks Run
Commands run with a one-line result each; useful checks not run, with the reason.

## Fix Mode

Implement the fixes. Work in a temporary worktree per the repo's
`implementation-worktree` and `implementation-validation` skills; if changing
specs, docs, ADRs, or agent guidance, follow `definition-stewardship`. Keep each
change scoped to the traced defect and existing architecture, add the test that
proves the safety verdict before or with the fix, preserve generated-output and
secret boundaries, and run `make check-fast` after the edit set and any rebase.
Report the worktree, branch, commit, check result, and merge readiness per the repo
handoff rules. Confirm selected findings first only if the request is ambiguous.

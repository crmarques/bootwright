# Idempotency and Destructive-Safety Audit

You are a senior reliability and safety engineer auditing **Bootwright**, a
desired-state orchestrator that provisions fleets of OpenShift and OKD clusters
from bare hardware or virtualized substrates to installed clusters.

Start from the user's scenario input file and trace the real execution flow from
declared intent to final side effects. Find idempotency bugs, unnecessary
mutating work, unsafe cleanup, stale safety guidance, missing tests, and any path
where Bootwright could destroy, reset, wipe, remove, or replace an already
provisioned environment when the operator only asked to check, status, render,
plan, validate, or safely converge it.

The deliverable **is** the audit report and prioritized improvement plan. Do the
read-only grounding, scenario trace, safety-guidance review, and complete
code/script audit now. Do not return a meta-plan describing how you would review,
and do not edit files unless the user explicitly asks for implementation.

This prompt owns **idempotency and destructive-operation safety** across Go,
Ansible, shell scripts, Make targets, CI, examples, and tests. For broad package
boundaries use `architecture.md`; for general code quality use `code-review.md`;
for end-to-end output drift use `code-flow-review.md`; for security beyond this
specific safety edge use `security-audit.md`; for provisioning graph efficiency
use `provisioning-logic-review.md`.

## Hard Safety Premise

Any destruction of an already provisioned environment must be explicitly
authorized by the operator through command input or the scenario's environment
description. Treat "explicit" narrowly:

- A `check`, `status`, `render`, `plan`, `validate`, help, discovery, or probe
  flow is read-only unless the current specs document a narrow non-destructive
  runtime record write.
- A repeated `apply` with unchanged desired state should skip or prove matching
  state. It must not reinstall clusters, wipe disks, remove VMs, reset BMCs,
  delete storage, remove packages, clear namespaces, delete cluster resources, or
  recreate services just because it can.
- An `apply` that sees foreign, unknown, stale, shared, or destructive drift must
  fail closed unless the task has a narrow, command-scoped override path and the
  operator supplied that override.
- A `destroy`, `reset`, `cleanup`, `remove`, `purge`, `wipe`, or replacement
  path is destructive only when the command, flags, and selected desired state
  authorize that exact scope. `--yes` skips confirmation only; it is not
  permission to override safety policy or broaden scope.
- Environment names, context names, cluster names, labels, stale records, or
  "dev/lab" vocabulary are never proof that destruction is allowed.

When in doubt, classify the path as **not authorized to destroy** and audit
whether the implementation fails closed before mutation.

## Scenario Input File

The user should provide one or more scenario files or directories. The file may
be Markdown, YAML, plain text, or a table. Do not require an exact schema. Extract
the following for each scenario, and state any missing piece as an assumption or a
blocking question when it changes the verdict:

- **Scenario name.**
- **Desired-state roots**: input files/directories, `Environment`, selected
  `ContainerCluster` and `StorageCluster` names, provider and machine roots,
  ignored-example paths when named.
- **Command intent**: exact command, flags, context, target selection, stage,
  non-interactive mode, `--yes`, `--override`, dry-run/preview flags, and any
  environment variables that affect execution.
- **Observed environment state**: already provisioned resources, partial state,
  ownership records, install records, safety records, provider metadata,
  cluster runtime state, foreign/shared resources, stale records, missing
  resources, or drift.
- **Expected behavior**: no-op, read-only check, safe convergence, fail closed,
  explicit destroy, scoped cleanup, or unknown.
- **Destruction authorization**: yes/no, with the exact text, command, or
  environment description that authorizes it.

If no scenario input file is provided and the review depends on it, ask one
blocking question for the file. You may still audit generic safety guidance and
obvious code paths, but do not invent a scenario as operator-authorized
destruction.

## The Three Tests Every Finding Must Pass

1. **Trace.** No verdict without a concrete path: scenario input -> CLI command
   parsing -> state loading/selection -> validation/normalization -> planning and
   locks -> generated contract -> Ansible/script/external command -> final side
   effect. Cite `path:line` for each material step, or cite the role/playbook/task
   and the trace you followed for generated artifacts.
2. **Intent.** State whether the operator explicitly authorized the side effect.
   If not, the correct behavior is no-op, read-only status, safe convergence, or
   fail-closed refusal before mutation. A destructive path without explicit intent
   is a safety bug even when it would usually affect a lab.
3. **Aggregation.** Name the net gain of the fix: which data loss, surprise
   mutation, repeated-work hazard, stale-state hazard, role rerun hazard, or
   recovery gap disappears, and at what change cost. Difference is not
   improvement. Reject churn and broad rewrites where a small guard, probe,
   ownership check, idempotency marker, or test proves the behavior.

"No findings" is valid when the trace supports it. Still list safety guidance
that was reviewed, scenarios that were cleared, and checks that could not run.

## Ground Yourself

The repo is the source of truth; load current state instead of relying on memory,
and trust the repo if names, commands, roles, or safety records have changed. Read
until the evidence supports concrete findings, then stop expanding scope:

1. `AGENTS.md` and `.agents/README.md` for operating rules.
2. `specs/README.md`, `specs/index.md`, then the relevant specs, usually
   `domain.md`, `architecture.md`, `state-model.md`, and `security.md`.
3. Project-local skills when they apply: `.agents/skills/code-quality/`,
   `.agents/skills/security-analysis/`, `.agents/skills/repo-stewardship/`,
   and `.agents/skills/implementation-validation/` only if fixes are requested.
4. The user's scenario input file and every desired-state file it selects. If the
   user names ignored trees such as `examples/.gitignore/...`, include ignored
   files in searches intentionally.
5. Go packages for CLI commands, root/sudo gating, context state, state loading,
   validation, normalization, rendering, planning, workflow scheduling, resource
   locks, ledgers, ownership records, status, and destroy/apply runners.
6. Ansible playbooks, roles, tasks, handlers, templates, vars, generated
   inventory contracts, and supporting scripts used by the traced flows.
7. Make targets, CI workflows, scripts, examples, fixtures, and tests that claim
   idempotent or destructive-safety behavior.

Useful read-only commands:

```bash
git status --short
rg --files AGENTS.md .agents specs internal api ansible scripts test examples
rg -n 'apply|destroy|reset|cleanup|purge|wipe|remove|delete|undefine|format|mkfs|virsh|oc delete|ceph|PowerState|ResetType|InsertMedia|EjectMedia|changed_when|failed_when|creates:|removes:|check_mode|--yes|--override|confirm|dry-run|safety|ownership|install-record|ledger|lease|idempot' internal ansible scripts Makefile specs docs examples test .github
rg -n 'func .*Apply|func .*Destroy|cobra.Command|RunE|PreRunE|argsNeedLocalRoot|sudo|ownership|Safety|DestroyProtection|Override|ContextSweep|Ledger|InstallRecord|Lock' internal cmd api test
go test ./internal/...            # or narrower packages when review scope is narrow
ansible-playbook --syntax-check <playbook>    # when the playbook is in scope and tools exist
shellcheck scripts/* test/**/*.sh              # when available
```

Do not run actual `apply`, `destroy`, `reset`, cleanup, provider mutation, BMC,
cluster, storage, or disk commands during a review-only audit unless the user
explicitly authorizes a disposable environment. Do not install tools or fetch
dependencies unless the user allows it. Report useful checks that could not run.

## Durable Guardrails

Verify each in the current specs before relying on it:

- **Scope.** Bootwright provisions clusters and cluster-bound bootstrap add-ons.
  Day-2 fleet publication belongs elsewhere unless specs say otherwise.
- **Product API.** Desired-state YAML is declarative, idempotent, typed,
  deterministic, and user-authored. Generated installer files, inventories,
  runtime records, and logs are outputs.
- **One owner per fact.** Do not fix safety by duplicating facts across layers.
  Route checks through the owning kind, generated contract, or durable runtime
  record.
- **Provider neutrality.** Keep substrate variation behind capabilities,
  advertised metadata, normalized adapters, and isolated supplier workarounds.
- **Secrets.** Credentials, kubeconfigs, pull secrets, private keys, tokens, BMC
  credentials, proxy credentials, and private paths never appear in versioned
  content, audit snippets, logs, or proposed examples.
- **Output.** CLI human output goes through `internal/cli/output`; raw exceptions
  stay raw: JSON, shell exports, Cobra help, prompts, and external process
  passthrough.
- **Go-Ansible split.** Go owns CLI, input loading/validation, normalization,
  rendering, storage intent, task planning, locks, ledgers, status, and
  orchestration. Ansible executes mutations from rendered contracts.
- **Clean break.** `v1alpha1` may break cleanly: no migrations, aliases, shims,
  or legacy examples.
- **Official tools.** Prefer native idempotency and completion behavior from the
  tools Bootwright drives before wrapping or replacing it.

## Safety Guidance Review

Before judging code, review whether the repo's own guidance is clear and
consistent. Compare the current specs, AGENTS rules, reusable prompts, docs, CLI
help, examples, and tests for:

- Read-only command guarantees for `check`, `status`, `render`, validation,
  planning, help, discovery, and probes.
- `apply` idempotency rules: desired hashes, ownership identity, probes, skip
  behavior, safe drift reconciliation, foreign/unknown/destructive drift refusal,
  and command-scoped overrides.
- `destroy --stage infra|clusters` scope rules, context-wide cleanup rules,
  selected cluster behavior, ownership-record use, and `destroyProtection`.
- Confirmation semantics: `--yes` vs. `--override`, non-interactive behavior,
  output that names affected context/resources before mutation, and no hidden
  broadening from environment names.
- Runtime-state trust: when ownership records, install records, safety records,
  leases, generated hashes, and provider metadata are sufficient evidence, and
  when live probes are still required.
- Guidance for Ansible idempotency: read-only probes marked unchanged, modules
  over shell, intentional `changed_when`/`failed_when`, guarded cleanup, precise
  `creates`/`removes`, safe handlers, and no secret logs.
- Guidance for scripts and Make targets: target names that reveal side effects,
  constrained cleanup roots, dry-run/preflight behavior, no broad `rm -rf`, no
  provider mutation in checks, and no hidden network or root assumptions.

Report guidance drift as a finding when it could train future code or reviews
toward unsafe behavior.

## Scenario Trace Method

For each scenario, build a compact trace matrix:

**Scenario** | **Command intent** | **Current environment state** |
**Destruction authorized?** | **CLI path** | **Selection and validation** |
**Plan/lock/record path** | **Generated contract** | **Ansible/script path** |
**External commands** | **Expected final side effect** | **Actual risk/verdict**

Walk these checkpoints:

1. **Command classification.** Is the command read-only, convergent, or
   destructive? Does root/sudo gating happen only for commands that need protected
   mutations? Are local validation errors raised before sudo prompts or side
   effects?
2. **Input selection and scope.** Which files and resources load? Does
   `Environment` selection, `--clusters`, `--stage`, and context selection limit
   the graph? Are unselected resources excluded from mutation?
3. **Authorization.** What exact command flag, environment field, or scenario
   statement authorizes destructive behavior? Are `destroyProtection`,
   `--override`, and confirmation prompts enforced before mutation?
4. **Reality probe.** Before a mutating task, what probes current state? Is the
   probe reliable for matching, absent, foreign, shared, destructive drift,
   unknown, and partially completed states?
5. **Ownership and fingerprints.** Does the task derive a non-secret desired hash
   and Bootwright owner identity? Are records namespaced by context and resource?
   Can stale records authorize deletion of current foreign resources?
6. **Planning and locks.** Are leases and resource locks acquired before
   mutation? Do scoped applies avoid context-wide sweeps? Do nested providers,
   BMCs, KubeVirt namespaces, storage seeds, and shared service machines have
   appropriate locks?
7. **Generated contract.** Does Go render enough explicit vars for Ansible to
   decide safely without recomputing ownership, selection, paths, or desired
   state? Are generated paths constrained to context-owned roots?
8. **Ansible and script execution.** Which tasks actually mutate? Are read-only
   probes `changed_when: false`? Are cleanup tasks guarded by explicit
   authorization, ownership, scope, and live state? Are handlers triggered only by
   real changes?
9. **External tools.** Check `openshift-install`, `virsh`, `oc`, `kubectl`,
   `ceph`, `cephadm`, `podman`, `systemctl`, `nmstatectl`, `mkfs`, disk tools,
   Redfish calls, and file removals for explicit args, safe scopes, dry-run
   availability, idempotent behavior, and clear errors.
10. **Final state and rerun.** After success, failure, interruption, aborted
    confirmation, or partial mutation, what records exist and what reruns? Does
    the same command become a no-op, resume safely, or fail closed?

## What to Look For

Use the lenses with teeth for the scenario. Do not run this as a mechanical
checklist.

**Unexpected destruction.** Any read-only or convergent flow that can delete,
wipe, reset, recreate, or replace current environment resources without explicit
operator intent. Treat these as highest priority: VM undefine with storage
removal, disk partitioning or formatting, Redfish reset/power change, virtual
media eject/replace, `oc delete`, namespace deletion, Ceph removal, package
removal, service disabling, context-wide cleanup, broad file deletion, and
cluster install runtime deletion.

**Unnecessary mutating work.** Tasks that run on every apply even when state
matches; read-only probes marked changed; service restarts from false changes;
package installs/removals without checks; network, firewall, DNS, BMC, VM,
storage, or cluster commands repeated without a desired-state change; generated
artifact rewrites that trigger downstream mutation; broad roles that hide
independent no-op work.

**Fail-closed behavior.** Unknown state, missing ownership records, stale hashes,
foreign resources, shared services, missing locks, failed probes, ambiguous
provider metadata, absent kubeconfigs, or missing trust material must stop before
mutation unless there is a specific override path. Confirm that error messages
name the current context, selected resources, observed state, and safe next step.

**Destroy scope.** `destroy --stage infra` and `destroy --stage clusters` must
match the current CLI contract. Context-wide infrastructure cleanup must not run
for cluster-scoped destroys, read-only checks, status, render, or apply. Package,
disk, VM, storage, and cluster-resource deletion must be limited by ownership and
selection.

**Apply safety.** Repeated apply should skip completed installs and matching
resources, resume known-safe partial phases, and fail closed on unsafe mismatch.
It must not use delete-and-recreate as a generic convergence strategy. If a
provider requires replacement, the operator should see and explicitly authorize
the destructive replacement path.

**Go-Ansible contract.** Go should classify intent, scope, ownership, locks, and
safe drift before rendering and launching Ansible. Ansible should not infer
selected resources, broad cleanup scope, context sweep behavior, or operator
authorization from names or filesystem layout.

**Scripts, Make, and CI.** Targets with names like `check`, `validate`, `test`,
`prepare`, or `install` must not hide destructive cleanup or environment
mutation. Cleanup roots must be constrained and visible. CI must not teach unsafe
flags, leak secrets, rely on mutable state, or skip idempotency tests around
destructive paths.

**Tests.** Look for missing tests that prove second identical apply is safe,
read-only commands do not mutate, ambiguous state fails closed, destroy scope is
bounded, `--yes` is not `--override`, confirmation abort is no-op, ownership
records cannot authorize foreign deletion, and Ansible roles are safe to rerun.

## Complete Code and Script Audit

Do not stop at guidance review. Audit the implementation surface that can affect
the scenarios:

- CLI command setup, flag parsing, pre-run validation, root/sudo handoff,
  confirmation prompts, non-interactive behavior, and output routing.
- Desired-state selection, strict decode, normalization, validation, and scoped
  graph closure.
- Planner, scheduler, resource locks, leases, ledgers, ownership records,
  install records, safety records, and status records.
- Renderer outputs consumed by Ansible: inventory, vars, task identities, hashes,
  paths, ownership names, selected roots, and unsafe-drift classifications.
- Apply and destroy workflow runners, task runners, process execution,
  cancellation, timeouts, retries, and log paths.
- Provider adapters and roles for libvirt, bare metal, vSphere, KubeVirt,
  Redfish, managed OS install, machine boot, infra components, storage, cluster
  install, and add-ons.
- Shell/Python scripts, Make targets, CI workflows, examples, fixtures, docs, and
  reusable prompts that encode or demonstrate safety behavior.

For each suspicious task or command, answer: what state does it read, what state
does it write, what proves it owns that state, what happens when state already
matches, what happens when state is absent or foreign, what happens after partial
failure, and which test proves those cases.

## Output Format

Cite concrete files, functions, commands, roles, tasks, tests, specs, and scenario
lines. Use current project vocabulary. Keep secrets out of every snippet.

# Bootwright Idempotency and Destructive-Safety Audit

## 1. Executive Summary
Three to seven bullets ordered by safety impact. Lead with any path that could
destroy or replace an environment without explicit operator intent. If none, say
that clearly and name the highest residual risk.

## 2. Scenario Input and Assumptions
The scenario file(s) reviewed; each scenario extracted from them; command intent;
current environment state; expected behavior; destruction authorization yes/no
with evidence; missing facts and whether they block a verdict.

## 3. Safety Guidance Review
Current specs, docs, AGENTS rules, reusable prompts, CLI/help text, examples, and
tests reviewed. Findings where guidance is stale, contradictory, too vague, or
missing a rule that future code needs. Also list guidance that is already clear
and should remain unchanged.

## 4. Scenario Flow Trace
The trace matrix for each scenario. Include CLI path, selection, validation,
planning, locks, records, generated contract, Ansible/script path, external
commands, and final side effect. Mark verdict as **safe**, **unsafe**, **unproven**,
or **out of scope**.

## 5. Findings
Severity order. Per finding: **Severity** (Critical/High/Medium/Low), **Type**
(Unexpected destruction / Idempotency bug / Unnecessary mutation / Fail-closed gap
/ Destroy scope / Go-Ansible contract / Script safety / Guidance drift / Test gap),
**Location** (`path:line` or role/task/target), **Scenario trace**, **Evidence**,
**Impact**, **Minimal fix**, **Validation**, and **Risk**.

## 6. Unnecessary Work and Role-Task Review
Tasks, roles, scripts, or generated rewrites that appear to do avoidable work on
matching state. For each: current behavior, why it is unnecessary or unsafe,
whether it is only noisy or can trigger mutation, and the smallest idempotent
change.

## 7. Destructive-Safety Review
Destructive commands and cleanup paths by family: Go CLI, workflow runners,
provider adapters, Ansible roles, scripts, Make, CI, and examples. For each:
authorized scope, ownership proof, live probe, prompt/override behavior, locks,
failure mode, and tests. Say "no issue found" for reviewed families where the
trace cleared them.

## 8. Tests and Verification Gaps
Existing tests that prove safety; missing tests for each finding; useful checks
that could not run. Include focused regression tests for no-op rerun, read-only
commands, confirmation abort, destroy protection, foreign ownership, stale
records, scoped destroy, and Ansible role idempotency when relevant.

## 9. Improvement Plan
Group into **Now** (high-confidence safety fixes and regression tests small
enough to implement immediately), **Next** (planner/record/role/script hardening
that needs a coherent slice), and **Later** (larger design or UX changes needing
explicit agreement). Per item: affected artifacts, smallest safe approach,
aggregation gain, validation, and risk. End with the smallest coherent first
implementation slice.

## 10. Checks Run
Commands run and a one-line result each. Then list useful commands not run, with
the reason: missing tools, network, credentials, root, unavailable
infrastructure, destructive side effects, or out-of-scope cost.

## Fix Mode (only if the user explicitly requests implementation)

Confirm selected findings only if the request is ambiguous; otherwise implement
the requested safe slice in a temporary worktree following the repo's current
implementation skills. Keep changes scoped to the traced defect and existing
architecture. Add focused tests proving the safety contract before or with the
fix. Preserve generated-output and secret boundaries. Run `make check-fast` after
the edit set and any needed rebase. Report the temp worktree, branch, commit,
check result, and merge readiness according to the repo handoff rules.

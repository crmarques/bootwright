# State Lifecycle Safety Review

Audit **Bootwright** lifecycle safety. Find what happens when desired state is
created, re-applied, changed, removed, destroyed, or recreated against resources
that may be absent, Bootwright-owned, foreign-owned, stale, drifted, deleted out
of band, or partially converged.

Deliver the review, not a review plan. Ground in current specs and code, build a
scenario matrix, identify unsafe or unsupported behavior, and propose concrete
safety locks. Do not edit files or run mutating commands unless the user asks for
an implementation slice.

## Objective

Every supported lifecycle scenario must be deterministic and safe. Every
unsupported or unknown scenario must refuse before mutation, explain the
destructive risk, and tell the operator the next safe command or manual action.

## Load

Read current repo state before judging:

1. `AGENTS.md` and `.agents/README.md`.
2. `specs/README.md`, `specs/index.md`, then `specs/domain.md`,
   `specs/architecture.md`, `specs/state-model.md`, and `specs/security.md`.
3. Relevant prompt context only when useful: `idempotency-safety-audit.md`,
   `cli-schema-ux-rethink.md`, `provisioning-logic-review.md`.
4. Current CLI, selection, validation, planning, `state-check`, `apply`,
   `destroy`, ownership records, install records, convergence-safety records,
   provider adapters, Ansible roles, tests, docs, and examples in scope.

Use read-only commands first:

```bash
git status --short
rg --files AGENTS.md .agents specs docs examples test internal api ansible cmd
rg -n 'state-check|apply|destroy|override|expect-new|force-unowned|skip-unreachable|destroyProtection|requiredOverride|foreign|drift|ownership|install-record|convergence|safety|orphan|undeclared|partial|stale|unsupported' specs docs cmd internal api ansible test examples
rg -n 'rm -rf|wipefs|mkfs|sgdisk|parted|zap|rm-cluster|undefine|oc delete|kubectl delete|ceph .*rm|PowerState|ResetType|InsertMedia|EjectMedia|Delete|Remove|Destroy' internal ansible scripts test
go run ./cmd/bootwright apply --help
go run ./cmd/bootwright destroy --help
go run ./cmd/bootwright state-check --help
go test ./internal/... # or narrower packages when the trace is focused
```

Do not run real `apply`, `destroy`, provider, BMC, cluster, storage, disk, or
cleanup commands during a review-only audit.

## Scenario Matrix

Cover the likely and dangerous combinations. Group duplicates, but do not skip a
class just because the code path looks inconvenient.

- **Objects:** context input, `Environment`, `InfraProvider`, `InfraComponent`,
  `Machine`, managed machine OS, substrate VM or bare metal, `NetworkConfig`,
  `ContainerCluster`, `StorageCluster`, pools, filesystems, gateways, exports,
  add-ons, bindings, shared services, and generated artifacts.
- **Starting state:** absent; owned match; owned drift; foreign; stale or missing
  records; failed before records were written; partially destroyed; live object
  deleted out of band; YAML removed; same name reused for different identity.
- **Operations:** `state-check`, `plan`, `apply`, `apply --expect-new`,
  `apply --override`, scoped `--stage/--through/--clusters`, `destroy --stage`,
  full `destroy`, recreate after destroy, and `context update`.
- **Change types:** greenfield apply; import/adoption attempt; topology change;
  provider endpoint change; machine or disk change; OpenShift topology change;
  Ceph topology, placement, pool, filesystem, gateway, or export change; infra
  service change; deletion and same-name recreation.

Record each scenario as:

```text
Scenario | State | Command | Today | Supported? | Destruction authorized? |
Risk | Expected behavior | Evidence | Fix
```

## Out-Of-Box Pressure Tests

Actively look for unhandled unsafe scenarios. For each item, answer what happens
today, what could be destroyed unintentionally, where Bootwright should refuse,
and which safety lock would prevent the surprise.

- `context update` points an existing context at different infrastructure with
  reused names.
- A failed run creates a VM, disk, host config, Ceph change, or cluster artifact
  before writing ownership or convergence records.
- Records say `match`, but live resources were deleted, renamed, repurposed, or
  adopted by another system.
- YAML removes a root, then reintroduces the same name with different machines,
  disks, provider, topology, or base domain.
- `destroyProtection` is relaxed after production resources were applied.
- A narrow-looking scope reaches shared machines, provider hosts, storage, DNS,
  load balancers, artifact services, or context-wide cleanup.
- `--yes`, non-interactive mode, or automation removes the only human review of a
  destructive plan.
- `--override`, `--force-unowned`, or `--skip-unreachable` is accepted where its
  effect is unclear, too broad, or not shown before mutation.
- Two contexts share a provider, BMC, VM namespace, host, disk, Ceph cluster, or
  OpenShift cluster.
- A foreign resource matches Bootwright names, labels, addresses, or partial
  metadata closely enough to tempt unsafe adoption.
- A new kind or provider adapter lacks an explicit destructive vs.
  reconfigure-only classification.
- `state-check` is intentionally record-based; identify which destructive paths
  still need live probes immediately before mutation.

Useful probes:

```bash
rg -n 'Classify|ConvergeSafety|SafetyRecord|Ownership|InstallRecord|destroyStatus|foreign|drift|missing|match|unknown|partial' internal api ansible test
rg -n 'RunE|PreRunE|argsNeedLocalRoot|sudo|confirm|Prompt|NonInteractive|Yes|Override|ExpectNew|ForceUnowned|SkipUnreachable' cmd internal test
rg -n 'Lock|Lease|Scope|Selection|ClusterSelection|Stage|Through|Context|Snapshot|Forensic|Destroy|Apply|Plan|StateCheck' internal cmd test specs
rg -n 'changed_when|failed_when|check_mode|creates:|removes:|ignore_errors|no_log|shell:|command:' ansible
```

## Safety Rules

- Read-only commands never mutate provider, BMC, cluster, storage, disk, runtime
  records, ownership records, install records, leases, or convergence-safety
  records.
- Bare `apply` creates missing resources, skips proven matches, and fails closed
  on drift, foreign ownership, destructive ambiguity, and unsupported states.
- `apply --expect-new` fails closed if any selected object already exists or has
  ownership evidence.
- `apply --override` is command-scoped break glass. It may cross only documented
  Bootwright-owned drift barriers and must not bypass validation, leases, secret
  checks, foreign ownership, destroy protection, or active-run safety.
- `destroy` is the removal boundary. `--yes` only skips prompts; it never grants
  override, ownership relaxation, scope widening, or destructive rebuild.
- Deleting YAML is additive for `apply`; it must not delete live infrastructure.
- Unknown states and new kinds are destructive by default until classified.

## Safety Locks To Propose

For each unsafe scenario, propose the smallest lock that prevents accidental
destruction without blocking legitimate recovery. Define the command or schema
shape, required evidence, refusal message, recovery path, and tests.

- **Intent lock:** destructive rebuild or removal requires an explicit command,
  exact selected roots, and a rendered destructive plan; approval is distinct
  from `--yes`.
- **Scope lock:** selected clusters, stages, shared dependencies, generated
  artifacts, and context-wide sweeps are listed before mutation and cannot widen
  silently.
- **Ownership lock:** every wipe, rebuild, uninstall, VM delete, package removal,
  or Ceph zap verifies durable owner identity plus live identity where possible.
- **Drift lock:** `--override` is allowed only for classified Bootwright-owned
  drift with documented consequences.
- **Protection lock:** protected roots cannot be destructively rebuilt by
  `apply`; destruction must cross `destroy` with protected-scope override.
- **Stale-record lock:** name reuse, YAML deletion, context update, and stale
  records distinguish safe reapply, orphan cleanup, unsupported adoption, and
  dangerous stale ownership.
- **Co-residency lock:** never zap disks, pools, namespaces, host packages, VMs,
  BMC state, or clusters when another declared or foreign workload may depend on
  them.
- **Automation lock:** non-interactive runs rely on explicit flags, stable exit
  codes, and JSON/text plans, never prompts or inferred lab/dev names.

## Trace Standard

No verdict without a trace:

1. CLI command, flags, and root/sudo gate.
2. Context input, selected roots, and scope closure.
3. Validation, normalization, and ownership boundaries.
4. Record classification: `missing`, `match`, `drift`, `foreign`,
   `undeclared`, `partial`, `stale`, `unknown`, or unsupported.
5. Plan, locks, generated contracts, Ansible/external side effects, and records
   written or removed.
6. User-visible output, exit code, and retry or recovery guidance.

Cite files, functions, roles, tasks, specs, docs, tests, or examples. Separate
implemented behavior from spec intent and recommendations.

## Output

# Bootwright State Lifecycle Safety Review

## 1. Verdict
Three to seven bullets ordered by risk. Lead with the highest-risk gap or the
strongest evidence that current behavior is safe.

## 2. Scenario Matrix
Compact table grouped by object family and operation.

## 3. Findings
Severity order. For each: **Scenario**, **Evidence**, **Today**, **Risk**,
**Expected behavior**, **Safety lock**, **Fix**, **Validation**.

## 4. Unsupported States
States Bootwright should reject today, refusal message shape, and required
operator action.

## 5. Implementation Plan
Small slices only. For each: **Change**, **Why**, **Artifacts**, **Tests**,
**Acceptance criterion**. Include rejected ideas that fail the aggregation test.

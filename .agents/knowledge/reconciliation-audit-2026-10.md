# Reconciliation audit of apply and destroy, 2026-10

A three-lens check (reproduction, contract, reachability) of 59 candidate
findings on the apply and destroy path, done for X91 on 2026-10-14; it informs
`diff`, later per-scenario slices and the order of the parked safety items.
[State reconciliation](../../specs/state-reconciliation.md#lifecycle-unit)
owns required behavior. Line citations are from the commit that delivered X91
and drift with later edits; the symbol names are the stable reference.

## How apply and destroy decide

Bootwright executes a frozen plan; it never compares desired with real state.
`Service.mutate` (`internal/reconciliation/lifecycle/execution.go:132`)
decides under the shared lock through `decideFrom` (:326) and `decideOver`
(:367), re-proves through `proveShared` (:253), then presents, authorizes,
confirms and executes. Every exclusive-lock step re-proves its basis with
`verifyBasis` (:1230): operation id and state, block states, attempt counts,
plan digest and the whole operation record. Each branch is a row of the
[state machine](../../specs/state-reconciliation.md#state-machine).

What "equal" means in each admitted scenario:

- **Records-only settle (already equal).** Over a completed apply,
  `settleApply` (`internal/reconciliation/lifecycle/execution.go:480`) is a
  no-op when `inputDrift` (:516) reads empty and every frozen block reads
  `done`. `inputDrift` compares context name, revision and `inputDigest`
  (:1868), a sha256 over the path and content hash of every raw input file and
  marker, so a YAML comment is a change (`TestDigestOfMatchesTheViewDigest`,
  `internal/reconciliation/lifecycle/golden_test.go:159`, edits one comment;
  `TestInputDigestMatchesItsGolden` pins the bytes, :137). The settle reads no
  `PlanDigest`, `AutomationDigest` or `Executable`, re-plans nothing and
  reaches no host: `done` means the records are complete for this input, not
  that the target equals the declaration. Changed input refuses
  `lifecycle.state` "the desired state changed after this apply completed",
  which since X91 names what changed (`namedChange`) or keeps the general
  message when the frozen revision cannot be read or compiled.
- **Request-relative observation (half-provisioned).** A non-done operation of
  the same verb continues (`internal/reconciliation/lifecycle/execution.go:372`
  through :392) after `refuseUncontinuable`, `refuseStageBoundary`,
  `proveSameBlocks` and `verifyContinuation` (:1690), then `resume` (:1600).
  `recordedRefusal` (:1722) requires byte-identical input, read through
  `inputDrift`: "the context input changed after this operation registered"
  for name or revision, "the frozen input no longer matches this operation"
  for the digest. `firstAdmission`
  (`internal/reconciliation/lifecycle/scheduler.go:205`) admits every
  unproved block (`unproved`, unknown or running, execution.go :1504) first,
  and `resolveUnknown` (`internal/reconciliation/lifecycle/attempts.go:134`)
  observes it. Failed blocks are then retried beside startable ones (D144), and
  a block a resolution leaves failed is retried in the same invocation (D146). An observation
  compares against the frozen request and renders nothing: evidence is bound
  to `RequestDigest` (`internal/reconciliation/plan.go:196`), handed to
  dependents as recorded (`blockEvidence`,
  `internal/reconciliation/lifecycle/evidence.go:142`), and the observed effect
  state maps to a block state through `ResolutionTransition`
  (`internal/reconciliation/state.go:104`).
  "Done" means a capability validator accepted request-bound evidence; a
  `done` block is never observed again.
- **No live comparison (absent).** No current operation, or a completed
  destroy, leads to `freshApply`
  (`internal/reconciliation/lifecycle/execution.go:636`, from :418). Absence
  is inferred from records and never proved on a host before the first
  effect. Each adapter refuses only an object it reads as foreign, decided by
  a name-derived ownership marker (B568).

Guards: `Prepare` refuses an observation authorizing a host effect
(`internal/reconciliation/lifecycle/attempts.go:370`). Since X91, `RunFor`
(`internal/reconciliation/lifecycle/invocation.go:148`) derives `ReadOnly`
(:174), `checkReadOnly` (`internal/reconciliation/ansiblerunner/runner.go:26`)
refuses any operation but `observe`, and an allowlist test
(`cmd/bootwright/observe_playbooks_test.go:325`) checks every observe task.
`TestTransitionTablesMatchSpec`
(`internal/reconciliation/lifecycle/transitions_spec_test.go:33`) drives
`decide` only, and `TestApplyAdmitsOnlyTheThreeScenarios` pins where each
apply decision may appear. `Service.Diff`
(`internal/reconciliation/lifecycle/diff.go:58`) reads records under
`ReadLifecycle` and decides nothing; with observation, `observeDiff`
(`internal/reconciliation/lifecycle/diff_observe.go:48`) reads each block
through `Observe` alone.

## Safety verdict and its load-bearing assumptions

The decision engine is sound: effects run only from a frozen plan re-proved
under the exclusive lock; changed input over a completed apply refuses before
any effect; unproved blocks are observed first; a `done` block never re-runs;
a settle contacts no host; records are closed, canonical and compare-and-swap
protected; every refusal in `decide`, `proveShared` and `verifyContinuation`
precedes registration.

"Apply never breaks the target" holds for continuation and settle under the
assumptions below, and for a fresh apply only while real state is literally
absent, which nothing checks. Destroy can break another tenant's environment
today (B567). "A finished apply leaves real equal to desired" is weaker: a
settle attests records, and a continuation can finish `done` while real
differs (B576, B248, B570). Each assumption is load-bearing:

1. **Absent means absent:** no same-name object was left by an abandoned or
   concurrent context (B568); the spec's recovery text leads into it.
2. **One owner per host resource:** nothing outside this context uses a
   network or bridge it manages (B567); admission covers one catalog.
3. **Observation is read-only:** enforced by operation name since X91, and for
   the observe roles by the allowlist test.
4. **Capability evidence is honest:** request-bound, positive evidence is
   validated per capability before changed or unchanged.
5. **Adapters tolerate interruption,** including the operation-area bound
   (B572) and a Ceph remote worker outliving cancellation (B573).
6. **Same-build determinism:** a cross-build continuation (D142) breaks it,
   and a resolution can record `done` over stale running state (B248, B576).
7. **Recoverability:** the operation area is below its bound (B572) and no
   bundle purge ran under an incomplete operation (B528); otherwise the only
   exit is the orphaning delete, which feeds assumption 1.
8. **Retained physical consumers:** a bare-metal destroy removes DNS and NTP
   from hosts it retains (B571); within B70, undisclosed.

Decision informed: B567 and the interim wording of B568 precede any claim that
apply and destroy never break an environment; the incarnation identity of
B568 precedes any delta scenario.

## Confirmed findings

### Blocking

**[B567](../../specs/milestones/backlog.md#b567): a libvirt provider-host
destroy stops a managed network other tenants' running domains use.** The
removal runs `net-destroy` and `net-undefine` on every owned network
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_host/tasks/destroy.yml:101-134`)
without listing attached domains. `HostCapability.Quiescent`
(`internal/substrate/libvirt/capability.go:561`) answers `Quiescent`
unconditionally, and `proveQuiescent`
(`internal/reconciliation/lifecycle/execution.go:1410`) is called only for a
fresh destroy (:1078 through :1092). `ReservationKeys`
(`internal/substrate/libvirt/requests.go:169-182`) claims bridge, network and
prefix keys for managed attachments only, and the shared-bridge check
(`internal/substrate/admission.go:461`) covers one catalog, so another
context's default `external` attachment or an operator domain goes unseen.
Apply already guards a drifted network with `network_guests`
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/substrate_libvirt.py:311`)
and two refusals
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_host/tasks/apply.yml:182-210`).
The derived answer contradicts the
[provider host](../../specs/substrates.md#provider-host-realization) and
[quiescence](../../specs/state-reconciliation.md#quiescence-before-removal)
clauses. Decision: the role guard (list guests on every owned active network, refuse
before the first `net-destroy`) continues a live operation (D142); a
host-wide reservation for external attachments is a separate step.

**[B568](../../specs/milestones/backlog.md#b568): a recreated or second
same-name context adopts abandoned libvirt objects and can erase them.**
`DomainName`, `DiskDirectory`, `ControllerUnitName`, `DomainUUID`
(`internal/substrate/naming.go:55-80`), the MAC and the disk serial derive
from context and Machine names only; the adoption guard refuses only an
unowned same-name domain
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_machine/tasks/apply.yml:33-44`).
After `context delete --purge --allow-orphans` and "the same init", which
[storage, locking and publication](../../specs/contexts.md#storage-locking-and-publication)
prescribes without the "remove those objects by hand" step of
[continuation and removal](../../specs/state-reconciliation.md#continuation-and-removal),
or with two controllers holding one name, a fresh apply keeps the old disks,
redefines the old domain and replaces the emulator's credentials. A powered-off
guest passes the pre-boot check, which requires only power off
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_machine/tasks/pre_boot.yml:47-54`),
and the Kickstart runs `clearpart --all`
(`internal/managedos/installation/kickstart.go:365-372`). Ceph is unaffected
(FSID from the owning apply's id); services were not fully traced. Name-derived
ownership is the specified contract. Decision: an incarnation id minted at
`context init` enters every ownership marker and derivation (moving content
digests, acceptable before 1.x), merged with the Ceph lineage seam of B79.
Until it lands, the operator removes abandoned objects by hand before
re-initializing the name; the contexts spec does not yet say so, and its
interim wording is part of B568.

### Defects and over-claims today

**[B572](../../specs/milestones/backlog.md#b572): the operation-area bound hit
mid-attempt strands blocks no verb resolves.** `capacity`
(`internal/workspace/contextfs/operations_linux_amd64.go:739-750`) checks
`entries+1` on every write, replacements and appends included;
`AdmissionEntries` (`internal/reconciliation/operationstore/records.go:58-63`)
reserves room once. A refused append makes `logBoundary.fail`
(`internal/reconciliation/lifecycle/logging.go:31-48`) cancel every sibling
effect; the block stays `running`, and resume, resolve and destroy each need a
refused write, leaving only the orphaning delete. About 340 retried attempts
reach it. Decision: count only entries a write adds, so exhaustion stops at a
creating write before any effect; extends B243 and B267.

**B526: `plan` previews what `apply` refuses.** `Service.Plan`
(`internal/reconciliation/lifecycle/service.go:90-120`) never runs
`proveShared`, so it skips `verifyContinuation` and the removal host check;
`decideOver` computes `sameBlocks` without acting on it (execution.go :392).
No effect. Decision: one decision path, re-proof included, for `plan`,
`apply` and `diff`.

**[B574](../../specs/milestones/backlog.md#retired): a settle is a claim about
records.** A build planning other blocks for the same input still reports
`done`; specified and fail-safe. [D155](../../specs/milestones/backlog.md#decisions) accepts
records-only settle: it does not replan or read the host.

**B248: the artifact server's resolution records `done` over a superseded
configuration.** Its `Evidence`
(`internal/infrastructureservices/artifactserver/evidence.go:13-21`) lacks the
managed service's `StartedAfterFiles`
(`internal/infrastructureservices/managedservice/evidence.go:28`). Apply
restarts correctly; a resolution after an interrupted publish does not.
Reached through D142. Decision: require the field.

**[B576](../../specs/milestones/backlog.md#b576): libvirt machine evidence
proves less than "the frozen definition".** `ValidateMachinePresence`
(`internal/substrate/libvirt/evidence.go:382`) checks name, ownership, unit,
image, UUID, power and disks, never vCPU, memory, interfaces, TPM or boot
order. Apply redefines every time yet reports `unchanged`; a resolution can
record `done` with no redefine. Decision: the
[machine realization](../../specs/substrates.md#machine-realization) claim is
narrowed to what the evidence proves, or the evidence is widened; that awaits
the owner, and `diff` leaves the unproved fields uncompared meanwhile.

**B528: purging bundles strands an incomplete operation's closure.**
`closureRefusal` (`internal/reconciliation/lifecycle/execution.go:1734`)
refused; before X88, `setup --purge-old-bundles` then retired the closure
through `room` in the prerequisites capacity code without reading operation
records, and re-resolving did not reproduce the wheels. X88 removed the purge:
the closure is now the lock digest and the ABI, a lock change refuses naming
the build whose runtime the host keeps, and X90's retention keeps the previous
runtime, so B528 is moot once X89 deletes the bundle code.

### Seam prerequisites, latent today

**[B569](../../specs/milestones/backlog.md#b569),
[B582](../../specs/milestones/backlog.md#b582): the store does not enforce
the block-transition table.** `StartAttempt`, `StartResolution` and
`completeRecord` (`internal/reconciliation/operationstore/store.go:717`, :889,
:1009) write without checking state; only the scheduler, `verifyBasis` and the
one `ResolutionTransition` caller guard it, and `LastResolution` (:957) parses
entry names differently. Decision: the store refuses an inadmissible
transition before a second caller exists.

**[B575](../../specs/milestones/backlog.md#b575),
[B580](../../specs/milestones/backlog.md#b580),
[B581](../../specs/milestones/backlog.md#b581): the transition rule is
restated.** `previewed` (`internal/reconciliation/lifecycle/service.go:178`),
`supersedable` (execution.go :670), `nextAction`
(`internal/reconciliation/lifecycle/attempts.go:572`) and `frozenReopenOf`
(`internal/reconciliation/lifecycle/frozenbinding.go:168`) re-derive what
`decideOver` decides; bindings agree only while an operation holds one; "a
missing state reads pending" is open-coded at about nine sites beside
`blockState` (`internal/reconciliation/stage.go:164`). Only `plan` and
`status` output can drift. Decision: one classifier, one owned-bindings
function, one normalization.

**[B577](../../specs/milestones/backlog.md#b577): Ceph pool and client
requests embed the cluster request** (`poolRequest`,
`internal/storage/ceph/requests.go:426`; `clientRequest`, :467). Adding a pool
moves no sibling's digest, and the OSD-host count gates pool size. Decision: a
prerequisite of the first Ceph delta.

**[B578](../../specs/milestones/backlog.md#b578),
[B591](../../specs/milestones/backlog.md#b591): `status` hides realized
objects no longer declared, and two folds disagree.** `realizations`
(`internal/reconciliation/lifecycle/status.go:513`) only decorates catalog
rows, and `Ownership` (:59) folds the same records differently for a released
object. Decision: one per-object fold over declared and realized objects.

Constraints for `diff` and delta: level 2 takes its own observe-and-render
port, versioned outside every effect content digest (B600 to B605);
equality compares projected canonical forms, not `Value.Equal`
(`api/v1alpha1/value.go:131`); secrets render as references; every read
refuses `lifecycle.lease` while an apply holds the root lock
([B18](../../specs/milestones/m3.md#b18)).

### Minor

- [B570](../../specs/milestones/backlog.md#b570): a fresh destroy records a
  host-free removal answer as no-effect on the apply's block before quiescence
  is proved (D119); leaving it unknown breaks retired-Ceph destroy. With B460.
- [B571](../../specs/milestones/backlog.md#b571): services, the artifact server
  and installation answer `Quiescent` without a probe, because the Machines
  that consume them are probed in the same removal
  (`internal/infrastructureservices/managedservice/capability.go:410`), and a
  retained bare-metal Machine answers `Quiescent` too, so its removal leaves
  DNS and NTP stopped under a running host.
- [B573](../../specs/milestones/backlog.md#b573): "No adapter effect outlives
  cancellation"
  (`internal/reconciliation/ansiblerunner/process_linux_amd64.go:273`) holds
  locally only; a Ceph remote worker runs to its budget.
- [B579](../../specs/milestones/backlog.md#b579): after an observation,
  `firstAdmission` falls through to the startable blocks when the only failed
  block is unselected (scheduler.go :228); without the observation the same
  selection refuses `lifecycle.stage`.
- [B583](../../specs/milestones/backlog.md#b583),
  [B584](../../specs/milestones/backlog.md#b584),
  [B585](../../specs/milestones/backlog.md#b585),
  [B586](../../specs/milestones/backlog.md#b586): bare record-version
  literals; no golden for a resolution with an observation failure; the spec
  says root disk where the role refuses any owned disk; an over-claiming
  controller-clients digest comment.
- [B587](../../specs/milestones/backlog.md#b587) to
  [B590](../../specs/milestones/backlog.md#b590): Ceph pool `size` and
  `min_size` drift rewritten unreported; decode errors dropped by image and
  tree observations; no unresolved reason for Ceph core and host; the image
  dependency wired two ways.
- [B592](../../specs/milestones/backlog.md#b592) to
  [B595](../../specs/milestones/backlog.md#b595): stale CLI stage-selection
  text after D144 and D146; `RenderEffective` keeps no authored catalog, which
  no consumer needs while `diff` takes the compile port; the envelope shape
  defined twice; no Ceph refusal table test.
- Existing items: B460, B527, B226 and B363.

## Refuted candidates

Do not audit these again:

- "No blocking finding": overturned by B567 and B568.
- Missing convergible-difference declarations: the replay prose has them; only
  Ceph lacks an explicit list.
- Ceph owner and FSID from the "current operation": they come from the owning
  apply's id (`Execution.ApplyOperation`), stable on every admitted path;
  folded into B568 and B79 as a constraint.
- `FirstPassEntries` unguarded: pinned at 13 on a real tree; the rest is B572.
- `done` with empty evidence: every capability validates non-empty,
  request-bound evidence first.
- A `<ctx>` placeholder leaking: `CheckIndex` runs with the real name.
- An unvalidated bare-metal outcome: `ResolutionOutcome` normalizes it; a
  bare-metal pin is scoped to one operation by the spec.
- An unguarded `helpExample` (`internal/cli/help_test.go` covers it); block
  rows omitting the outcome (the status token is the outcome).
- A Ceph `rm-cluster` racing an orphaned worker: there is no `rm-cluster`.
- Wrong first-reading claims: "controller start never checked", "one pool
  defeats adding a pool", "only the spec test guards the derivations", and
  several citations that pointed at the wrong lines or no file.

## Simplicity

Keep, because they make the engine safe:

- the frozen-plan executor, one current operation per context, one inversion
  for destroy; deltas freeze the full union plan, never a chain;
- closed canonical records with compare-and-swap and read-back;
- `verifyBasis` and refusal by default on changed input;
- observe-before-retry, request-bound evidence, spec-as-test, the digest guard.

Collapse, each with a present consumer (`diff` or the B328 split):

1. one transition classifier (B575) that `previewed`, `nextAction`,
   `frozenReopenOf` and `supersedable` read;
2. one decision path with the re-proof for `plan`, `apply` and `diff` (B526);
3. one input-relation predicate naming which of name, revision or digest
   differs (done in X91);
4. one owned-bindings function (B580);
5. one per-object fold over block records (B581, B591, B578);
6. the block-transition invariant in the store (B569), with named record
   versions (B583);
7. an engine read-only mode for resolutions and probes (done in X91);
8. the lifecycle execution file split by concern after 1 and 2 (B328);
9. later, with a consumer only: the effect ladders shared across libvirt,
   managed service and artifact server.

Not yet: delta admission, union-ownership plans and Ceph request decoupling
stay design until a named slice and a product decision. The incarnation id
(B568) is the exception, justified by a present safety defect.

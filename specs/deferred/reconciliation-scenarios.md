# Reconciliation scenarios beyond the three

A named slice revives this design when an operator asks for one more apply
scenario over a completed apply. Its first step is
the [additive apply](../state-reconciliation.md#additive-apply), and decision
D153 accepts the rule that each new scenario is its own slice. Until then
[state reconciliation](../state-reconciliation.md) owns what `apply` admits,
[day-2 operations stay outside scope](../project.md#design-priorities-and-non-goals),
and every changed input over a completed apply refuses, except the
[additive apply](../state-reconciliation.md#additive-apply) of add-on
instances (D211), the first scenario this design produced. `Compare`, the
changed-input branch, the scenario guard and the `DeltaAdmitter` admission port
exist; every other scenario described below does not.

## The admitted scenarios

Decision D153 names the three scenarios `apply` admits and D211 adds a fourth.
The contract states each one; the rows only point at the rule that owns it.

| Scenario | Owning rule |
| --- | --- |
| Nothing is realized: no operation, or a completed destroy | [Lifecycle unit](../state-reconciliation.md#lifecycle-unit) and the first row of the [state machine](../state-reconciliation.md#state-machine) |
| Partly realized: a running, failed, paused or unknown apply | [Continuation and removal](../state-reconciliation.md#continuation-and-removal), [dependency safety during recovery](../state-reconciliation.md#dependency-safety-during-recovery) and [attempts and unknown outcomes](../state-reconciliation.md#attempts-and-unknown-outcomes) |
| Already realized: a completed apply under its unchanged input | [Lifecycle unit](../state-reconciliation.md#lifecycle-unit) and the `apply done` row of the [state machine](../state-reconciliation.md#state-machine) |
| Add-on instances added to a cluster a completed apply installed | [Additive apply](../state-reconciliation.md#additive-apply) and the `apply done` row of the [state machine](../state-reconciliation.md#state-machine) |

"Absent" and "done" are read from durable records, not from the host.
[Comparing desired and real state](../state-reconciliation.md#comparing-desired-and-real-state)
owns what `bootwright diff` reports about the difference, and states that a
settled apply proves nothing about the host.

## Direction for scenarios that change a realized object

The owner's direction of 2026-11-24 ([D288](../milestones/backlog.md#d288)) is
that a later scenario, such as changing one attribute of a realized object and
applying the change, builds on the same step contract as the
[staged destroy](staged-destroy.md): each destroy step states its inverse,
identity follows the creation of an effect, and an admission classifies the
fresh plan against what the context owns and refuses by default. This page
adds no seam for it, because a seam needs a present consumer.

## The seam a scenario plugs into

The seam lets a new scenario change one branch, one table row, one guard and
one optional port.

- **Delta.** `Compare` in `internal/reconciliation/delta.go` classifies each
  block of a frozen plan against a freshly compiled one as same, added,
  removed or changed, and folds blocks into per-object changes. `diff` and the
  other-build proof consume it. A scenario decides admission from this value
  and never from a second comparison; the changed-input refusal names changed
  objects through a catalog comparison, `objectChanges`, and decides nothing
  from it.
- **One changed-input branch.** `settleApply` in
  `internal/reconciliation/lifecycle/decision.go` holds the only path where a
  completed apply meets a changed input. A scenario adds its admission before
  the changed-input refusal and keeps the refusal as the default.
- **Optional admission port.** A capability opts in to an added object by
  implementing `DeltaAdmitter` (`internal/reconciliation/lifecycle/contracts.go`),
  resolved the way `UnsupportedReporter` is, as the add-on instance does
  ([additive apply](../state-reconciliation.md#additive-apply)). A capability
  without it refuses.
- **Lineage.** An admitted delta registers an apply whose plan holds only the
  added blocks, with their edges into the lineage frozen as `prerequisites` and
  its record naming the completed apply as its `predecessor`. One destroy
  takes the whole lineage back
  ([removal over a lineage](../state-reconciliation.md#removal-over-a-lineage)).
- **Guard.** `TestApplyAdmitsOnlyTheFourScenarios` in
  `internal/reconciliation/lifecycle/transitions_spec_test.go` pins where each
  apply decision may appear: `fresh apply` only with no operation or a
  completed destroy, `add` and `settle` only on `apply done`, `continue` only on a
  running, failed, paused or unknown apply, and `finalize` only on a failed
  apply whose blocks are all `done`. A new scenario edits this test, the
  state-machine row, its `stateMachineRows` case and the code in one commit;
  `TestTransitionTablesMatchSpec` then holds the row and the code together.

## Recipe: one more CephPool over a completed apply

The worked case is the smallest useful one: a completed apply realized a Ceph
cluster and its pools, and the operator adds one `CephPool`.

1. **Decision and slice.** Record a decision and name a slice. The change
   narrows a safety refusal, which the
   [scope rules](../milestones.md#scope-rules) reserve to a named slice, and it
   adds a day-2 operation the
   [project scope](../project.md#design-priorities-and-non-goals) excludes
   until one is explicitly defined.
2. **Prerequisites.** Each is its own item and lands first:
   - Ceph owner and FSID derive from a lineage id instead of the owning
     apply's id ([B79](../milestones/m2.md#b79), merged with the incarnation
     identity of [B568](../milestones/backlog.md#b568)). Without it the second
     apply derives another FSID and the cluster's evidence reads as foreign.
   - Pool and client requests stop embedding the cluster request
     ([B577](../milestones/backlog.md#b577)), so `Compare` reports exactly
     one added block and every other block the same.
   - A retention rule bounds the operations a lineage keeps
     ([B258](../milestones/backlog.md#b258)).
3. **Spec.** Add one row to the
   [state machine](../state-reconciliation.md#state-machine): a completed
   apply under a changed input whose delta adds only admitted objects
   registers an additive apply over the lineage. Add the matching
   `stateMachineRows` case and the guard placement in the same commit as the
   code.
4. **Code at the one branch.** The scenario extends the additive admission
   path in `decision.go` and `additive.go`: it admits an added object only
   when its capability implements `DeltaAdmitter`, and it edits
   `TestApplyAdmitsOnlyTheFourScenarios`.
5. **Proof.** Before admitting, the slice requires `diff --observe` to read the
   pool's cluster blocks present on the host; records alone never prove the
   cluster is still the one the frozen plan realized.
6. **Diff.** No change: `diff` already reports the new pool as `plan: added`.

## Design rules

Each rule becomes a test the reviving slice names.

1. **Identity is lineage-stable.** A scenario that registers a second apply
   over realized Ceph state first derives the Ceph owner and FSID from a
   lineage id
   ([bootstrap identity](../ceph.md#bootstrap-identity-and-continuation)); until
   then that scenario is refused.
2. **Requests embed only what gates them.** Adding one pool moves no sibling's
   digest, and editing the cluster moves every pool's. Part of the embedding is
   load-bearing: the OSD host count gates the pool size. A scenario that edits
   a shared parent first narrows the embedding.
3. **Evidence stays proof-shaped.** Observation for comparison uses its own
   read-only projection port, with its own version and bound, outside every
   effect content digest. Widening proof evidence moves digests and turns
   in-flight operations into cross-build continuations.
4. **Observation stays outside compilation.** Diff and delta are composing
   services over the compile port and an observation port; the compiler keeps
   no application dependency
   ([compiler boundary](../api.md#compiler-boundary)).
5. **Compare canonical forms.** Comparison uses the schema's canonical form
   projected onto the fields a capability observes. `Value.Equal`
   (`api/v1alpha1/value.go`) compares desired with desired and keeps its
   positional list equality
   ([defaults and effective state](../api.md#defaults-normalization-and-effective-state)).
6. **Secrets render as references.** Each kind projects through an
   allow-list, and open maps keep only declared keys
   ([sensitive material](../security.md#sensitive-material)).
7. **Reads follow the lock rule.** A comparison read takes the shared reader
   hold of
   [storage, locking and publication](../contexts.md#storage-locking-and-publication).
8. **Seams need a present consumer.** Build a seam only where a consumer exists
   now: `diff` consumes `Compare`, and the additive apply consumes admission
   through `DeltaAdmitter`. Any further scenario stays on this page until a
   named slice and a decision promote it.
9. **Realized but undeclared objects stay visible.** A changed input over a
   completed apply can leave realized objects no longer declared. `diff`
   lists them; `status` builds its rows from the compiled catalog only
   (`internal/reconciliation/lifecycle/status.go`) until
   [B578](../milestones/backlog.md#b578) builds them from the union of
   declared and realized objects. A scenario never assumes the catalog lists
   everything it owns.

## What not to do

- **No amendment without a named slice and a decision.** A spec row, a
  `stateMachineRows` case or a guard placement changes only inside the slice
  that admits its scenario, together with the code.
- **Never treat settled as proof of equality.** What a settled apply attests
  is owned by
  [comparing desired and real state](../state-reconciliation.md#comparing-desired-and-real-state);
  a scenario never reuses it as evidence that real state equals desired
  state.
- **Never widen proof evidence.** A field a comparison needs enters a
  projection ([B600](../milestones/m2.md#b600) to
  [B605](../milestones/backlog.md#b605)), never a capability's proof
  evidence or its content digest.
- **No converter.** A scenario's durable-format move refuses earlier records
  and names the remedy; it adds no compatibility reader.
- **No widening of the default.** A capability without `DeltaAdmitter`, an
  object change other than an admitted addition, and any input that no
  longer compiles keep the changed-input refusal.

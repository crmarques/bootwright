# Apply/destroy authorization guards: the registries that keep the contract closed

The safety contract in `specs/state-model.md` and ADR 0007 says a state change
happens only when the operator explicitly asked for it. These are the mechanisms
that make *future* code fail a check rather than merely break the convention.

**Registry (`workflow.ApplyTaskKinds`).** The single list of mutating apply task
kinds. `TestApplyTaskKindsRegistryCoversEveryConstant` parses `apply_tasks.go`
for `ApplyTaskKind*` constants and fails when one is missing from the registry,
so a new kind cannot be added without entering the guards below.

**Every kind is classified.**
`TestEveryApplyTaskKindHasAnOverrideClassification` asserts each registered kind
is exactly one of reconfigure-only (`overrideReconfigureOnlyKinds`) or
destructive under `--mode rebuild`, and that a destructive kind has refusal
consequence text (`structuralRebuildConsequence`) — a refusal must be able to say
what it would do. The allowlist fails safe (unlisted = destructive), so a new
kind is destructive by default; the guard makes that a deliberate choice rather
than an accident. `TestOverrideReconfigureOnlyAllowlistHoldsOnlyLiveTaskKinds`
additionally rejects a retired member — a dead allowlist entry is unreachable
defence-in-depth and makes the published taxonomy over-promise.

**Every kind is torn down.** `TestEveryApplyTaskKindMapsToADestroyKind`
(`internal/converge`) asserts `destroyKindForApplyTaskKind` maps every registered
kind to a destroy kind. An unmapped kind's convergence record would survive the
teardown that removed the resource, and the next apply would classify it as
`match` and skip re-provisioning it — a silent no-op where work was expected.

**The record round-trips.** `TestWrittenConvergeSafetyRecordClassifiesAsOwnedMatch`
writes a record through the real writer and classifies it through the real
classifier, pinning the owner-field schema in both directions: a record this
writer produced must read back as `match`, a changed hash as `drift`, and a
different manager as `foreign`. Without it the `foreign` tier has no writer and a
record-schema change could silently flip every record — or orphan the refusal.

**The scenario matrix.** `internal/cli/apply_destroy_safety_matrix_test.go` is
table-driven over (command, flag combination, starting state, selected scope) and
asserts the expected verdict: usage error, fail-closed refusal, accepted, held at
the interactive confirmation, or the read-only out-of-sync report. It carries one
case per `--authorize` token (ADR 0030), a case per retired flag proving it is now
an unknown flag, and the headline case that `destroy --yes` alone no longer
authorizes an OSD zap. Every refusal case additionally asserts the message names a
`bootwright …` command, so remedial guidance cannot regress into a bare
"not allowed"; a case whose expected outcome is the interactive data-loss prompt
uses `verdictPrompted` instead, because declining a prompt is not a refusal.

It runs against the advanced baseline example
(`examples/baremetal-redfish-multidc-virtualized-odf-ceph`: two DCs, a stretched
Ceph arbitrated across both, one bare-metal OCP per DC, one nested virtualized
OCP per DC) so cross-DC scope closure and host-to-guest substrate ordering are
exercised, not assumed.

**Baseline coverage boundary.** Machine-substrate tasks in that example come
only from the KubeVirt-hosted child clusters: `applyMachineHost` returns `""` for
a bare-metal machine, so a bare-metal cluster root plans no
`machineInfraPrepare`/`machineInfraFinalize` and `MachineSubstrateClusters`
never names it — a substrate release for a bare-metal root is written by no
destroy and consumed by no apply. Substrate-release rows must therefore target
`dc1-child-ocp` (and seed its host installed + KubeVirt-ready, or the
`hostClusterRef` readiness gate refuses first). Two paths stay out of reach on
this baseline and are pinned at unit level instead: the bare-metal managed-OS
reinstall data-loss acknowledgment (the example declares no managed-OS machine —
`os.provided: true` on every Ceph node) via `TestDestructiveOverrideYesGuard`,
and a machine-substrate rebuild of a KubeVirt *host* cluster (every host here is
bare metal) via `TestCheckKubeVirtTenantRebuildScopeGatesEverySelectionAxis`.

**Selection-axis rule.** Gates that compare "what this run will destroy" against
"what this run selected" must key on the resolved `clusteraccess.Selection`, not
on one flag string. Keying on `--clusters` alone left `--machines` runs
ungated: a machine-scoped rebuild of a KubeVirt host could annihilate a nested
cluster that a cluster-scoped rebuild refused to touch.

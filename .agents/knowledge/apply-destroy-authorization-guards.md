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

**Consequence-axis rule (ADR 0031).** The same failure repeats one axis over: a
gate must key on *what the run destroys*, never on the stage name that destroys
it. `DestroyScopeCoversStorage` (scope name is `clusters` or `all`) drove the
data-loss gate, so `destroy --stage infra` and `destroy --machines` deleted a
libvirt/KubeVirt/vSphere-backed Ceph cluster's OSD VMs — the same data — under
`--yes` alone, while the preview printed `ALL OSD DATA … is destroyed`.
`workflow.EvaluateDestroyDataLoss` is now the single predicate, and it feeds the
gate, the `--yes` refusal, the prompt choice, and the teardown preview, so they
cannot disagree. It has two arms: `Zapped` (the cluster layer's
`cephadm rm-cluster --zap-osds`) and `Released` (the machine layer deleting
provider-owned OSD hosts — `MachineRequiresSubstrate` and a non-bare-metal
provider, because a bare-metal destroy never wipes in place). A new destroy scope
or a new provider joins the predicate, not a new `if scope.Name ==`.
`TestDestroyDataLossCoversEveryScopeThatDestroysOSDData` tables the scope
combinations.

**A recorded hash covers only state that reaches a host.**
`Environment.spec.safety` is read by `validate_environment.go` and
`destroy_safety.go` and by nothing else — it renders into no inventory and no
role. While it sat inside `hashScopedState`, enabling `destroyProtection`
flipped every container cluster and machine-substrate task to *structural* drift:
`apply` refused fleet-wide and named `--mode rebuild`, the protection gate then
refused that rebuild and named `destroy`, and a change that mutated nothing left
teardown as the only exit. The projection now zeroes the field rather than
dropping it, because `Safety` is a struct whose `omitempty` Go ignores: the
payload keeps `"safety":{}` and every hash of an unprotected environment is
byte-identical to what shipped, so there is no re-baseline and no schema bump.
Pinned by `TestConvergeHashIgnoresEnvironmentAuthorizationPolicy` and
`TestConvergeHashKeepsTheEnvironmentSafetyKeyForRecordStability`. Before adding
any controller-side knob to a spec an apply hashes, ask whether a role ever sees
it; if not, exclude it there.

**Scope-invariance is now closed over every task kind.**
`TestEveryApplyTaskHashIsInvariantUnderClusterScoping` plans the advanced two-DC
example whole and once per cluster root and fails on any task whose desired hash
differs. It caught the last two offenders — the cluster-add-on task (hashing the
scope-filtered state) and the fabric per-host task (hashing
`FabricHostDesiredVars` of the scoped state, whose consumer lists shrink) — which
made a `--clusters` run after a clean whole-fleet apply report drift and
`diff --recorded` exit `3` on a converged fleet. Both now hash a projection of the
unscoped `hashState` while still rendering from the scoped `State`.

**The token vocabulary is bound to its published homes, in both directions.**
`internal/cli/authorize_contract_test.go` parses the `| token | authorizes |`
table out of `specs/state-model.md`, ADR 0030 and
`docs/advanced/operations.md` and requires it to equal
`authorizationTokens` exactly, so a token cannot ship unpublished and a doc
cannot promise one that does not exist. It also requires every token to appear in
a `safetyMatrixCases()` row (a token that unblocks a refusal must be exercised),
to declare the verbs whose gates consume it, and — for a token a verb's gates
cannot consume — to be refused there as a usage error naming what resolves it
instead. Eight of the twelve are destroy-only; `data-loss` and `unowned-devices`
(ADR 0034) and `all` (ADR 0040) have gates on both verbs, and `foreign-daemons`
(ADR 0038) is the first apply-only one, refused on `destroy`. `unowned-devices` is reachable on `apply` only under
`--reclaim-devices`, so its matrix row is a `--dry-run` (which by contract
consumes no token); the real consumption path — the warning and the
`bootwright_ceph_authorize_unowned_devices` extra var — is pinned by
`TestWarnReclaimUnownedDevicesStatesBothReadings` and
`TestUnownedDeviceAuthorizationExtraVarRidesOnlyTheToken` instead, because a real
`--stage deps` apply on the baseline stops at the machine-check preflight
(unseeded cluster secrets and host trust) long before the destructive block. Both
apply-side tokens are structured that way: `foreign-daemons` has the same
`--dry-run` matrix row, and `TestForeignDaemonReclaimRidesTheTokenAndAStorageConverge`
tables its three real outcomes — armed, closed without the token, and inert with
no storage-cluster converge in the run — including that the two consuming cases
never report as inert and the third always does.
Consumption must be recorded wherever behavior actually changes:
`emitApplyDataLossWarningsAndVars` returns whether it consumed `data-loss`
because widening the storage sub-object rebuild authorization is a consumption
that used to be reported as "had no effect" while the extra-var went to the roles.

**`all` is resolved through the same `has`/`note` pair, never around it.**
The blanket token (ADR 0040) adds no gate and no call site: `authorizations`
carries the verb it was parsed for, `has` answers yes when `all` was given and
the asked-for name is a registered token *of that verb*, and `note` credits
`all` for the names it supplied. Everything follows from that one resolution
point — `apply --authorize all` cannot reach a destroy-only token, no tokenless
refusal (scope conflicts, the KubeVirt tenant gate, a mounted device) becomes
reachable, and `--yes` stays orthogonal. A name given explicitly is credited to
itself, so `all` still reports "had no effect" when it supplied nothing; a real
run that did use it prints which unnamed tokens it stood in for, which is the
unused-token warning pointed the other way. Adding a future token puts it inside
`all` on the day it lands, so the disclosure line is the only record of what a
given run expanded to.

**The preview reads the gate's predicate, not a copy of it.** Every dry run and
`plan` of the three authorizing verbs emits a `Required authorizations` block,
so a token is learnable before a refusal teaches it. The rule that keeps the
block honest is structural: each gate's predicate is captured into one named
value that the gate, the refusal, the prompt choice and the forecast all read.
`destroy` splits it as `dataLossReached` (shared) versus
`dataLossPlanned = dataLossReached && !dryRun` (the real run only) — the split
is what fixes the original defect, where the whole data-loss evaluation sat
behind `!dryRun` and the preview could not see the highest-consequence token.
`apply` accumulates its destructive descriptors in one function both paths call,
differing only in evidence: the real run passes the live reinstall plan, the
preview an offline forecast. `protected` keys on the presence of safety reasons
rather than on `RequiresAuthorization`, which goes false once the token is
supplied and would have hidden the satisfied case. A token whose evidence lives
on a host the preview may not contact is disclosed as *may be required* — a
preview performs no probe to settle one, and silence would read as "none
needed". Matrix rows assert a dry run names every token its non-dry counterpart
refuses on.

**The published contract is guard-synced in both directions.** The `accepted by`
column of the `--authorize` tables in `specs/state-model.md` and
`docs/advanced/operations.md` is set-compared with each token's verb set
(`TestAuthorizationAcceptedByColumnMatchesTheConsumingVerbs`); ADR 0030's table
predates the column and is asserted on token names only. Four sibling guards in
`internal/repo/checks` cover the artifacts the same review touched: the
shared-service classification table against `selfContainedSharedServiceSlots`,
the `docs/concepts/` Required/Default column convention against the spellings
`docs/concepts/index.md` owns, the ADR index's passive relations against the
referenced ADR's `## Status` block, and retired CLI vocabulary in Go string
literals — that last one exists because the Markdown-only scan let a retired
verb survive in `internal/status/hints.go` and end the next-step spine on an
unknown command.

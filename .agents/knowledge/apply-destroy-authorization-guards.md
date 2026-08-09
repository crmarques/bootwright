# Apply/destroy authorization guards: the registries that keep the contract closed

The safety contract in `specs/state-model.md` and ADR 0007 says a state change
happens only when the operator explicitly asked for it. These are the mechanisms
that make *future* code fail a check rather than merely break the convention.

## Adding to the destructive surface — what to do, and what fails if you don't

Read this row before writing the code, not after the guard fails. Each row names
the one place the addition has to be registered; the guard in the last column is
what turns "forgot" into a red test.

| You are adding | Register it in | Guard that fails otherwise |
| --- | --- | --- |
| an `--authorize` token | `authorizationTokens` (`internal/cli/authorize.go`), the `\| token \| authorizes \| accepted by \|` tables in `specs/state-model.md` and `docs/advanced/operations.md`, a `safetyMatrixCases()` row, and the verb's preview forecast | `authorize_contract_test.go`, `TestEveryTokenAVerbAcceptsIsNamedByItsPreviewForecast` |
| a flag on `apply` or `destroy` | a `safetyMatrixCases()` row exercising it, or `safetyMatrixFlagExemptions` naming the test that pins it instead; and a field emitted by `resolvedInvocation` so refusal retries preserve it | `TestEveryApplyDestroyFlagIsExercisedByTheSafetyMatrix`, `TestEveryApplyDestroyFlagIsPreservedByTheRetryBuilder` |
| an apply task kind | `workflow.ApplyTaskKinds`, the reconfigure-only allowlist *or* `structuralRebuildConsequence`, `destroyKindForApplyTaskKind`, and `objectProtectedKind` when it is destructive | `TestApplyTaskKindsRegistryCoversEveryConstant`, `TestEveryApplyTaskKindHasAnOverrideClassification`, `TestEveryApplyTaskKindMapsToADestroyKind` |
| a substrate provider (or any consumer of the substrate release) | the machine-scoped predicate, and `substrateResetConsumers` | `TestEverySubstrateResetConsumerIsMachineScoped`, `TestNoUnlistedSubstrateResetConsumer` |
| a Go→Ansible intent, authorization, scope, or execution variable that controls mutation | `mutationSafetyVars` (`internal/converge/mutation_safety_vars.go`) and `ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md` | `TestMutationSafetyVarsStayClosedAcrossGoAnsibleAndDocs` |
| a shared machine service slot | `selfContainedSharedServiceSlots` *or* accept that it degrades and fails closed | `internal/repo/checks/shared_service_classification_test.go` |
| a gate that decides "may this run destroy X" | one named consequence predicate the gate, the refusal, the prompt choice **and** the preview all read | ADR 0031; `TestDestroyDataLossCoversEveryScopeThatDestroysOSDData` |
| a refusal | the object, the consequence in the kind's own vocabulary, and a CLI-rendered `bootwright …` invocation carrying the resolved run flags and any required token; converge errors remain typed evidence only | the `verdictRefusal` arm of `TestApplyDestroySafetyMatrix` |
| an Ansible runtime retry or refusal | one of the CLI-produced `bootwright_*_invocation` facts in `vars-contract.md`; add a typed CLI variant when the existing facts cannot express the sanctioned retry | `TestAnsibleMutatingRemediesUseResolvedInvocationFacts` |

Three failure shapes recur often enough to name:

- **Keying on a flag or stage name instead of on the consequence.** `--stage infra`
  and `--machines` destroy the same data the clusters stage does; a gate that tests
  `scope.Name == "..."` or "was `--clusters` given" leaves the other axes open. Key
  on the resolved `clusteraccess.Selection` and the shared predicate.
- **Proving state with one kind's evidence.** A `ContainerCluster` proves it is
  installed with an install record; a managed `StorageCluster` proves it with its
  Bootwright-owned ownership record. A predicate that knows only the first silently
  drops every storage cluster.
- **A guard built on a value no CLI path produces.** A table that hand-builds a
  forecast struct or a partial `Selection` tests the function, not the contract.
  Drive the guard through `runCLI` or through the same constructor production uses.

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

**The Go→Ansible mutation contract is a registry, not a naming convention.**
`mutationSafetyVars` classifies every rendered intent, positive authorization,
scope allowlist, and execution selector that can change what an embedded
playbook mutates. `TestMutationSafetyVarsStayClosedAcrossGoAnsibleAndDocs`
scans production Go for names that carry the mutation-control vocabulary,
requires each one in that registry, then requires the same name in an Ansible
consumer and in the collection's vars contract. The scan keys on generic
authorization, destroy, rebuild, reclaim, reset, scope, skip, task, and provider
control vocabulary, so a future provider or task fails when it invents an
unregistered destructive channel. Role-local facts do not enter the registry:
the boundary is specifically controller-produced values, and absence must
under-authorize or narrow the run.

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

**A third verb runs a scoped apply, so it inherits that apply's gates.**
`storage-cluster replace-arbiter` builds the replacement machine through
`prepareArbiterMachine`, a real `PlanScopedApply`/`ExecuteApply` over
`fabric,machines,deps`. A scoped apply hidden inside another verb silently
skipped everything `bootwright apply` runs before mutating, so unrelated
structural drift sitting in the same input commit was converged in reconcile
mode with no refusal — on task kinds whose own consequence text is "reinstall
the machine — its disks wiped". It now calls `applyOwnershipRecords` and
`converge.ArbiterPreparePreflight`, which is reconcile-mode
`EvaluateApplyModePreflight` over `WithoutTiebreakerDrift(objects, cluster)`:
the tiebreaker drift this run just authored is the one legitimate difference,
and everything else refuses with apply's own words. It also refuses when a
substrate-release record intersects the scope (that reinstall is data loss the
verb has no token for), takes `checkCurrentApplyBeforeMutation` before the input
rewrite, and holds one command lease from the desired input read through
promotion, machine preparation, mon swap, and record refresh. The rule
generalizes: **a verb that
embeds a scoped apply must run that apply's gate set, minus exactly the drift it
is authorized to converge.**

**Adding a desired-input mutator:** register its command kind in
`AcquireCommandRunLease`, acquire before reading any desired input whose
classification controls the write, hold through snapshot/write/cleanup, and pass
the held lease into every embedded workflow. Add the source-order case to
`TestDesiredStateMutatorsLeaseBeforeReadingOrWriting` and an active-competing-run
behavior test that proves neither input nor history changed. Read-only modes must
take no lease.

**A shape change is not a drift the same refusal can route.** Whether a stretch
cluster carries an arbiter is fixed at bootstrap. Enabling or disabling one was
classified as ordinary structural drift, so `continueDriftRefusal` sent the
operator to `--mode rebuild` (`cephadm rm-cluster --zap-osds` on a live cluster,
to add or remove one mon) or — when the edit happened to touch nothing else —
to `replace-arbiter`, which refuses a cluster whose live monmap reports
`stretch_mode false` and points back at `apply`. Both directions of that loop
are closed by `taskStretchArbiterShapeChange`, which compares the record's
`TiebreakerNodes` against the desired ones for that cluster and drives a
terminal `stretchArbiterShapeRefusal` naming no command that would refuse.
`IsTiebreakerOnlyStructuralDrift` and `stateWithoutStretchTiebreaker` — the
predicate pair deciding "move one mon" versus "wipe the cluster" — had no direct
test at all and now have a table each.

**A record the run converged is not carried drift.**
`RefreshStorageClusterConvergeSafety` compared each record only against the
*pre-rewrite* task, so the `nodeaccess`/`storageinfra` records that
`prepareArbiterMachine` had just stamped read as "left recorded as drifted" on
every successful promotion, with a remedy (`apply --stage clusters`) that cannot
reach the machines phase. Worse, an interruption between the completed
retirement and the refresh left a record matching neither side, and the settled
branch (which refreshes `before == after`) could never re-stamp it: every later
`apply` refused to `replace-arbiter`, which reported "nothing to replace" — a
permanent loop whose only named exit was destroying the cluster.
`convergeRecordRebaselinable` now re-stamps a record that matches the before
task, *or* already matches the after task, *or* differs from it only by the
tiebreaker.

**Baseline coverage boundary.** Machine-substrate tasks in the advanced example come
only from the KubeVirt-hosted child clusters: `applyMachineHost` returns `""` for
a bare-metal machine, so a bare-metal cluster root plans no
`machineInfraPrepare`/`machineInfraFinalize` and `MachineSubstrateClusters`
never names it — a substrate release for a bare-metal root is written by no
destroy and consumed by no apply. Substrate-release rows must therefore target
`dc1-child-ocp` (and seed its host installed + KubeVirt-ready, or the
`hostClusterRef` readiness gate refuses first). The matrix uses two secondary
baselines for the paths that topology cannot express: `ceph-ibm-baremetal-redfish`
drives a destroy-released bare-metal managed-OS machine through the real
`--yes` data-loss refusal, and `sno-libvirt-redfish` plus an in-test declared
KubeVirt tenant drives a released libvirt host substrate through the real
nested-tenant scope refusal. The focused unit tests remain useful for predicate
detail, while the matrix now proves command parsing, desired-state resolution,
record evidence, selection, the refusal, and the exact remedy end to end.

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
instead. Seven of the fourteen are destroy-only (`protected`, `installed-cluster-node`,
`unowned-vms`, `unowned-networks`, `unreadable-records`, `shared-infra`,
`stale-input`); `data-loss` and `unowned-devices` (ADR 0034) span apply and
destroy; `foreign-daemons` (ADR 0038) is the one apply-only token, refused on
`destroy`; `unreachable-nodes` spans destroy and `replace-arbiter`;
`same-site-arbiter` and `degraded-quorum` are `replace-arbiter`-only (ADR 0042);
and `all` (ADR 0040) resolves per-verb across all three. `unowned-devices` is reachable on `apply` only under
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

**A gate that refuses on a dry run cannot be forecast, and a forecast the CLI
cannot reach proves nothing.** `shared-infra` and `installed-cluster-node`
refused unconditionally, so their `consult()` entries could only ever render
*satisfied* — the `required` branch was dead in production, and an operator had
to clear refusals one at a time to learn the blast radius. Both now carry
`!dryRun` (matching `unreadable-records`), so a single preview lists them.
`stale-input` deliberately still refuses on a dry run, because whole-input
validation holds before anything else. The reason this survived a coverage guard
is worth remembering: `TestEveryTokenAVerbAcceptsIsNamedByItsPreviewForecast`
called `destroyRequiredAuthorizations` directly with a hand-built
`destroyGateForecast{staleInput: true, sharedInfra: true, installedNode: true}`
— a struct value no real preview could produce. **A forecast test that bypasses
the CLI tests the forecast function, not the contract.**

**Both output dialects disclose the same run.** `--dry-run --output json`
returned before every tokenless-refusal forecast and before the
`--purge-history` notice, so a CI wrapper reading `requiredAuthorizations: []`
concluded a run was clear while the real run fails closed on a gate the machine
dialect never mentioned. `converge.DryRunReport` now carries `refusals` and
`purgeHistory`, and the text dialect renders from the same
`applyGateForecastRefusals` slice, so the two cannot drift apart. The apply
forecast also unions destroy-released clusters into the KubeVirt-collateral
check the way the real gate does, and evaluates `UnmatchedReclaimDevices`, which
previously exited 2 on the real run after a preview had affirmed the device.

**A skipped task still satisfies the release that authorized it.**
`runApplyTask` returned early on `result.Skipped` before reaching
`ConsumeSubstrateRelease`, so a destroy-written substrate release survived an
apply whose managed-OS install was skipped as already converged. On libvirt and
vSphere the leftover is inert (`bootwright_*_managed_os_reset` also requires
`bootwright_apply_mode == 'rebuild'`), but KubeVirt's
`bootwright_kubevirt_rebuild_authorized` reads membership in
`bootwright_substrate_reset_clusters` with no mode check, so the VM and root
DataVolume delete tasks stayed armed on every later reconcile apply of a cluster
that already matched. A skip is positive evidence the machine matches, which is
exactly when the release is satisfied, so the consume now runs on that path too.

**A machine-granular release authorizes only its own machines.** The release a
`destroy --machines` writes names the released machines, but the three substrate
roles decided their destructive reset from `bootwright_substrate_reset_clusters`
alone — the *cluster* name. Only the managed-OS probe
(`machine_os_identity/tasks/probe_existing.yml`) consulted the machine list. So
releasing one KubeVirt guest armed `bootwright_kubevirt_rebuild_authorized` for
every sibling machine of that cluster, and the next plain `apply` force-stopped
and deleted their live VirtualMachines and root DataVolumes — machines the
destroy never released, with no descriptor, no data-loss prompt and no token,
because `applyDestructiveDescriptors` emits nothing for a KubeVirt guest. The
libvirt and vSphere roles carried the same cluster-wide predicate behind an
additional `--mode rebuild` check. All three now repeat the probe's predicate:
released *and* (this cluster names no machines in the pair list, or this exact
`<cluster>/<machine>` pair is listed). `TestEverySubstrateResetConsumerIsMachineScoped`
and `TestNoUnlistedSubstrateResetConsumer`
(`internal/repo/checks/substrate_release_scope_test.go`) close it over new
providers: a file that reads the cluster var without the machine var fails, and a
consumer missing from the list fails too, so a new substrate role cannot inherit
the cluster-wide blast radius.

**A nested tenant is not always a ContainerCluster.** The KubeVirt tenant gate
proved "installed" only through `workflow.LoadClusterInstallRecord`, which a
`StorageCluster` never has — so `destroy --clusters <KubeVirt host>` saw no
conflict for a managed Ceph cluster running on that host's VMs, priced no data
loss (its `StorageWorkNames` is empty for a container-only selection), previewed
nothing, and annihilated every OSD under `--yes` alone. The guest→host edge was
already modeled for both kinds in `KubeVirtHostParentsByChild`; only the
"provisioned" predicate was container-only. `installedKubeVirtTenants` now also
accepts the Bootwright-owned `storage-cluster` ownership record
(`converge.ProvisionedStorageTenants`), the same evidence
`machineDestroyInstalledClusterGuard` uses, and the in-scope set is `sel.AllRoots`
rather than `sel.ContainerRoots` so selecting the tenant alongside its host still
clears the conflict. Pinned by
`TestKubeVirtTenantConflictsSeeAProvisionedStorageTenant`.

**A retry hint must reproduce the run it retries.** `RunLedger` carried `Target`
and `Scope` and no machine axis, and `--machines` is mutually exclusive with
`--clusters`, so `Scope` was always empty on a machine-scoped run. A failed
`destroy --machines worker-03` therefore made `bootwright status` offer
`bootwright destroy --yes` — an unscoped full-lifecycle teardown of the whole
context, with the confirmation already answered — as its first next step. The
ledger now carries `Machines`, populated from `opts.SelectedMachines` in
`runPreparedTaskGraph` so both verbs get it from one place, and
`ledgerRetryCommand` emits `--machines` instead of widening.
`TestFailedRunRetryHintReproducesTheRunSelection` tables the four shapes; the
older spine guard only checked that a hint parsed as a registered command, which
`bootwright destroy --yes` does.

**A refusal retry reproduces the resolved invocation, not a hand-built flag
fragment.** The earlier refusal helper retained only `--stage`/`--through` and
one selection axis. Adding a required token could therefore drop the resolved
context, apply mode, `--reclaim-devices`, `--recover-ceph-ownership`,
`--purge-history`, an authorization already granted, and the SSH identity that
made the probe meaningful. Most dangerously, a refused `destroy --dry-run`
could be rendered as a real destroy. `resolvedInvocation` captures every local
and persistent flag that can affect or describe the run; `retryIntent` may only
change the apply mode and union in a token the same verb accepts; and the final
command is shell-quoted from its argument vector. Mode-preflight refusals are
typed so the CLI, which owns the resolved flags, supplies the exact command;
foreign ownership supplies no fake bypass and directs the operator back to the
recorded manager. `TestEveryApplyDestroyFlagIsPreservedByTheRetryBuilder` closes
this over future flags, while the exact-parse and gate-clear tests prove the
printed command is accepted, keeps the original scope and effects, and clears
the refusal it names.

**Ansible consumes resolved commands; it never rebuilds them.** Runtime roles
see only part of a run and cannot recover its context, persistent SSH flags,
selection axes, effect flags, or prior authorizations from a task-local cluster
or machine. Real apply and destroy runs therefore receive the shell-quoted
`bootwright_mutating_invocation` plus typed apply variants for reconcile,
rebuild, full scope, `--through base`, and a runtime-reclaim template. That
template contains one unmistakable sentinel as the entire
`--reclaim-devices` value and separately carries the controller-resolved paths
already selected. The role validates a nonempty comma-representable `/dev/`
runtime path set, joins it with those preserved paths, shell-quotes the whole
operand, asserts exactly one sentinel, and replaces only that value. A path with
spaces, quotes, dollars, backticks, backslashes, or shell operators remains one
argv value; commas, newlines, NULs, relative paths, and sentinel collisions fail
closed. The role vars contract publishes the
set and `TestAnsibleMutatingRemediesUseResolvedInvocationFacts` rejects a role
that prints a literal apply/destroy command or consumes an unregistered variant.
`TestRuntimeReclaimRetryTemplateRoundTripsHostilePathsAndEveryResolvedFlag`
shell-parses the hostile-path substitution and proves both the exact operand and
all resolved flags survive. A role never splices a verb or flag around a runtime
value.

**Every flag on a mutating verb is exercised by the matrix.**
`TestEveryApplyDestroyFlagIsExercisedByTheSafetyMatrix` walks the registered
local and inherited-persistent flag set of `apply` and `destroy` and requires
each one to appear in a
`safetyMatrixCases()` row, or in `safetyMatrixFlagExemptions` naming the test
that pins it instead; `TestSafetyMatrixFlagExemptionsHoldOnlyLiveFlags` rejects a
dead exemption. Before this, a new flag on either verb could ship with no
scenario coverage at all — the matrix was comprehensive over *tokens* and merely
incidental over *flags*.

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

# Add-on apply engine: gates, skip semantics, and preflight

**Constraint:** The add-on apply engine (`internal/addons/oc`) depends on exactly
two seams — `StepRunner.Run(ctx, lifecycle)` and `EffectRunner.Run(ctx)` — whose
concrete implementations live in `internal/converge/workflow` because they need
ansible, secrets, and state, which `internal/addons/oc` must not import. A nil
`StepRunner` or `EffectRunner` is a defined no-op, so add-ons without
steps/effects and unit tests need not wire one. "Step" is the internal name for
an authored `ClusterAddon spec.steps[]` entry; the Go identifiers, Ansible vars
(`bootwright_step_*`), and packages keep it. Steps fire at exactly three
lifecycles, spelled in authored YAML as `gates: apply`,
`follows: operatorReady`, and `follows: ready`.

**Skip semantics:** Only a check-backed `Ready()` counts as evidence an add-on
is still live on-cluster. With zero `spec.readiness.checks` `Ready()` is
vacuously true, so trusting a matching desired-hash record would skip forever
even after someone deletes the add-on's resources with `oc`. `Apply` therefore
falls through and re-applies every run for checkless add-ons (`oc apply` is
idempotent). Pinned by `TestApplyReAppliesChecklessAddonDespiteReadyRecord`.

**Short-circuit blockers:** Three things disable the already-ready pre-check:

- zero readiness checks (above);
- a `run: always` step at `gates: apply` or `follows: operatorReady` —
  `steps.HasAlwaysAt` exists for the engine to consult; the apply then re-runs
  idempotently and the step executor still skips unchanged `run: onChange`
  steps via their own per-step digest;
- a `globalPullSecretMerge` effect on an accepted input — registry credentials
  must converge on every apply (the merge is idempotent and cheap);
  `hasGlobalPullSecretMergeEffect` gates this.

**Effect ordering:** Input effects run first in `applyExtension`, before
`gates: apply` steps and before any resource applies, because a global
pull-secret merge must land before anything pulls images — the shipped catalog
image, the operator, or `gates: apply` step workloads. `oc/effects_test.go`
proves the effect executes before the first resource apply and that no resource
applies after an effect failure.

**CSV gate trigger:** The OLM CSV gate (wait for the operator CSV to reach
`Succeeded`) runs when the add-on has custom resources OR any
`follows: operatorReady` step. `TestApplyStepTriggersCSVGateWithoutCustomResources`
proves an add-on with a `follows: operatorReady` step and zero
`customResources` still waits, and that `gates: apply` runs before the operator
install. Without the gate, custom resources race the operator install and fail
with `no matches for kind`.

**The CSV gate does NOT establish every CRD the add-on then uses.** It proves
only that the *subscribed* operator's CSV succeeded. A meta-operator creates
further Subscriptions from its own running pod, so the operators that own the
interesting kinds install strictly after the gate opens — Data Foundation's
`odf-operator` CSV reaches `Succeeded`, and only then does it subscribe
`ocs-operator`, which owns `storageclusters.ocs.openshift.io`. Applying a
`StorageCluster` right after the gate is therefore not a race that sometimes
loses; it loses unless something else happened to burn the intervening minutes
(on prd, the exporter playbook's own 5-minute ConfigMap poll masked it on one
cluster and not the other). Gating harder on the add-on's own Subscription
cannot fix this, and waiting for `ocs-operator` to *run* is unsatisfiable —
odf-operator keeps its Deployment at zero replicas until a `StorageCluster`
exists. The CRD reaching `Established` is the only signal available before the
CR exists, so a step declares it in `spec.steps[].requires[]`.

**Gate timeouts are typed, not apply failures:** `catalogGateError` means the
CatalogSource applied but its registry never reported
`connectionState` `READY` within `spec.readiness.timeout`; `csvGateError` means
the operator-install set applied but the CSV never reached `Succeeded`. In both
cases `failedID` stays empty so the persisted summary never names the
already-applied CatalogSource/Subscription as a failed `oc apply` target. Their
strings hold only resource identifiers and state/phase detail (safe to
persist); the applied resource still lands in `Record.ObservedResources`.
Pinned by `TestApplyOLMGateTimeoutRecordsGateFailureNotApplyFailure` and
`TestApplyOLMCatalogGateTimeoutRecordsGateFailureNotApplyFailure`.

**The timeout is a task deadline, not a per-gate allowance:** the workflow
captures one `StartedAt` before constructing the engine and step executor.
`newWaitBudget` derives one absolute deadline from it; the CatalogSource, CSV,
step-requirement, and final readiness polls all use that same instant across
Apply and Wait. Resource applies and step execution consume wall time and never
restart the budget. `pollUntilReady` is the package's only ticker loop; it owns
parent-cancellation precedence, retryable read errors, compact progress, and a
separate 30-second diagnosis context. Step-requirement shape errors opt out of
read-error retry and remain immediate. Invalid durations are parsed before a
record write or cluster call, and future-dated `StartedAt` values are clamped so
they cannot widen the budget. `TestCoreReadinessPollLoopsStayCentralized` keeps
new gates on this path.

**Quiet poll output:** Readiness polls, the idempotency pre-check, and the OLM
gates run through a quiet read runner that writes neither raw `oc get` commands
nor their JSON responses to the task log or console. Active waits append only
compact `<resource>/<name> <state>` observations to the task and cluster logs,
deduplicated until the state changes or the heartbeat elapses. For condition
checks the resource is the CRD-style argument, such as
`storagecluster.ocs.openshift.io/ocs-external-storagecluster`, and state prefers
`status.phase`. Timeout diagnostics remain detailed. The addon task reports
"skipped" only when BOTH the install and the wait-ready phases were no-ops.

**Preflight gates:** A stage-scoped add-ons run (base phase out of scope)
installs no cluster, so every binding target cluster's kubeconfig
(`clusters/<cluster>/secrets/kubeconfig`) must already exist;
`addonsKubeconfigChecks` gates this up front with remediation
`run bootwright apply --stage clusters --clusters <name> --yes before applying
add-ons` instead of letting each task fail later at `requireKubeconfig`. A full
apply with base in scope produces the kubeconfig mid-run and is deliberately
not gated (`TestAddonsStageGatesMissingKubeconfig`). The add-ons phase needs
the controller Ansible runtime (ansible-playbook + python3 checks) only when
some add-on ships a playbook step (`spec.steps[].playbook` non-empty);
manifest-only add-ons need no ansible
(`TestPreflightChecksAddonPlaybookStepsNeedAnsible`).

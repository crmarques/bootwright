# Add-on apply engine: gates, skip semantics, and preflight

**Constraint:** The add-on apply engine (`internal/addons/oc`) depends on exactly
two seams — `HookRunner.Run(ctx, lifecycle)` and `EffectRunner.Run(ctx)` — whose
concrete implementations live in `internal/converge/workflow` because they need
ansible, secrets, and state, which `internal/addons/oc` must not import. A nil
`HookRunner` or `EffectRunner` is a defined no-op, so add-ons without
hooks/effects and unit tests need not wire one. Hooks fire at exactly three
lifecycles: `preApply`, `postOperatorReady`, `postReady`.

**Skip semantics:** Only a check-backed `Ready()` counts as evidence an add-on
is still live on-cluster. With zero `spec.readiness.checks` `Ready()` is
vacuously true, so trusting a matching desired-hash record would skip forever
even after someone deletes the add-on's resources with `oc`. `Apply` therefore
falls through and re-applies every run for checkless add-ons (`oc apply` is
idempotent). Pinned by `TestApplyReAppliesChecklessAddonDespiteReadyRecord`.

**Short-circuit blockers:** Three things disable the already-ready pre-check:

- zero readiness checks (above);
- a `run: always` hook at `preApply` or `postOperatorReady` —
  `hooks.HasAlwaysAt` exists for the engine to consult; the apply then re-runs
  idempotently and the hook executor still skips unchanged `run: onChange`
  hooks via their own per-hook digest;
- a `globalPullSecretMerge` effect on an accepted input — registry credentials
  must converge on every apply (the merge is idempotent and cheap);
  `hasGlobalPullSecretMergeEffect` gates this.

**Effect ordering:** Input effects run first in `applyExtension`, before
`preApply` hooks and before any resource applies, because a global pull-secret
merge must land before anything pulls images — the shipped catalog image, the
operator, or preApply-hook workloads. `oc/effects_test.go` proves the effect
executes before the first resource apply and that no resource applies after an
effect failure.

**CSV gate trigger:** The OLM CSV gate (wait for the operator CSV to reach
`Succeeded`, establishing the operator's CRDs) runs when the add-on has custom
resources OR any `postOperatorReady` hook — such a hook also needs the CRDs
established (e.g. the hook producing the external-cluster Secret +
StorageCluster). `TestApplyHookTriggersCSVGateWithoutCustomResources` proves an
add-on with a `postOperatorReady` hook and zero `customResources` still waits,
and that `preApply` runs before the operator install. Without the gate, custom
resources race the operator install and fail with `no matches for kind`.

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
some add-on ships a playbook hook (`spec.hooks[].playbook` non-empty);
manifest-only add-ons need no ansible
(`TestPreflightChecksAddonPlaybookHooksNeedAnsible`).

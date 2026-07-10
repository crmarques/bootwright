# Audit-finding IDs referenced by regression tests (state engine)

Findings from the 2026-07 full-repo audit are pinned by regression tests that
cite the finding ID in comments or test names. Map for the state-engine/apply
IDs; other subjects' knowledge files map their own IDs.

**M2 — lease takeover must be a no-op signal.** A run that stalled past the
stale window and lost its lease must stop refreshing/removing it so it never
clobbers the new holder. Invariant: heartbeat uses `SaveRunLeaseIfOwner`,
cleanup uses `RemoveRunLeaseIfOwner`, both treating `ErrLeaseNotOwned` as
stop-not-fail. Tests: the M2 cases in
`internal/converge/workflow/lease_core_audit_test.go`.

**M8 — scoped-apply hash must be scope-independent.** The virtctl task hashed
its carried State, which is the `--clusters`-filtered set on a scoped run, so
an unscoped state-check reported drift after a clean scoped apply and
fail-closed the next reconcile. Invariant: tasks hash a projection of only
their real inputs (`virtctlDesiredHashVars`). Test:
`TestVirtctlTaskDesiredHashIsScopeIndependent`
(internal/converge/workflow/apply_plan_container.go's planner).

**M11 — state-check --clusters validation must match apply.** `StateCheck`
threads the resolved scope name (`converge.Scope.Name`: "all", "add-ons",
"through-base", …) into `clusteraccess.Resolve` instead of a hardcoded "all",
so a container-only stage rejects a StorageCluster name with the same
"unknown cluster" error apply raises. Tests:
`TestStateCheckStageAddonsRejectsStorageClusterLikeApply` and its accept-side
twin in `internal/status/statecheck_selection_audit_test.go`.

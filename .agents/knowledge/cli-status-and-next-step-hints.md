# Status report and next-step hint semantics

**Semantics: `bootwright status` is the single next-step hub.** It
computes the suggested next command from normalized state, secret
material, and the apply ledger; every preparing/mutating command's
"Next steps" section routes back to it (`printNextStatusHint`) instead
of duplicating the sequencing logic.

**Semantics: a cluster's name line is identity, not health.** In
`status` it renders as neutral INFO (never a green OK that would read as
"installed"); the installer-freshness line below (fresh/stale/missing/
unknown vs `effective-state.yaml`) is the real readiness signal, and
storage clusters get their own INFO row so a managed Ceph cluster is not
invisible next to container clusters. Storage clusters partially
destroyed by `--skip-unreachable` carry a marker on their kept ownership
record and get a WARN teardown row naming the skipped nodes.

**Semantics: `diff` joins the hint spine only once applied.** The hint
appears only when the context has at least one recorded apply (the
`applied` flag), and is placed BEFORE plan/apply because it is the
read-only "did anything drift since my last apply?" verb of the
steady-state loop — it should be discovered before a surprising apply,
not after. Before the first apply there is nothing recorded to compare
against, so it stays off the spine. Pinned by
`TestNextStepHintsSurfacesDiffOnlyWhenApplied` (finding P21).

**Semantics: secret hints list every missing secret.**
`ContextSecretSetHints` emits a `bootwright secret set` hint for EVERY
missing context-local secret (not just the first) so an operator sees
the full required set in one read; the OpenShift pull secret
(`v1alpha1.DefaultPullSecretName`, hinted with `--pull-secret` rather
than `--from-file`) is deliberately surfaced first because it is the
most universally required and would otherwise sort last alphabetically.

**Semantics: the machine-trust hint fires before a strict SSH check
fails.** `preflight.NeedsHostTrust` reports whether the desired state
declares machines requiring Bootwright-managed SSH host trust whose
records are absent or stale, so the spine can suggest
`bootwright machine trust` before the workflow reaches a strict SSH
check instead of silently skipping a mandatory step on remote-host
layouts.

**Semantics: the failed-run retry hint maps the ledger target back to
the exact verb+flags.** Destroy stamps its target as `<stage> destroy`
(via converge `ExecuteDestroyGraph`), so such a target retries as
`bootwright destroy [--stage infra|clusters]` — never a re-apply of what
a teardown just failed to remove. A `through-<phase>` target retries
with `--through <phase>`; family/sub-phase targets
(infra|clusters|fabric|machines|deps|base|add-ons) retry with `--stage`
so a narrow rerun never silently widens to a full apply; the remaining
scope names (all, container-cluster, storage-cluster) carry no stage
flag; a recorded `--clusters` scope is re-threaded. The "destroy" token
never appears in apply targets, which is what separates the verbs.
Pinned by `ledger_audit_test.go` (finding L4).

**Semantics: cluster access is advertised only for completed
installs.** `StorageSummariesForApply` (mirroring
`ClusterSummariesForApply`) advertises access only for clusters whose
install task actually completed in this apply run's ledger: the run must
be `RunStatusOK` and the cluster's `ApplyTaskKindStorageCluster` (or
`ApplyTaskKindInstallWait` for container clusters) task must be
`TaskStatusOK`. A skipped or failed cluster is never advertised as
reachable.

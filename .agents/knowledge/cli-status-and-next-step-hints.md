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
destroyed by `--authorize unreachable-nodes` carry a marker on their kept ownership
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
the context-qualified `bootwright machine trust` before the workflow reaches a strict SSH
check instead of silently skipping a mandatory step on remote-host
layouts.

**Semantics: normal runnable hints carry the resolved context and do not
pre-authorize mutation.** `internal/status` returns read-only/setup hints as
structured argv, the normal apply step as a typed action plus its lossless
context, and unresolved cases as command-free guidance. `internal/cli` resolves
the apply action through the same invocation builder used by real mutation, so
the current SSH globals, explicit context, reconcile mode, and current default
safety-flag values are preserved and shell-quoted. It never adds `--yes`, so
the next state change still needs an operator confirmation. This does not alter
failed-run handling: the ledger's typed, CLI-resolved recovery remains
authoritative and is never replaced by the normal next-step action.

**Semantics: failed, stale, and cancelled recovery comes only from a resolved
ledger plan.** Every real apply and destroy keeps immutable
`InvocationArgs` for audit and stores a typed request plus ordered argv steps
resolved by `internal/cli`. The scheduler writes the safe interruption plan
before each task starts and replaces it with the actual typed task remedy on a
terminal failure. Thus a partial create resumes as reconcile, a later-only DNS
proof interruption retries the original selection, and an actual proof failure
records fabric repair plus exact resume. `internal/status` validates the typed
request, every mutating verb, and the explicit context before shell-quoting the
steps; it never derives a command from `Target`/`Scope`/`Machines`, failure
prose, or the original audit argv, and never adds `--yes`. A legacy, absent,
preview-shaped, or malformed recovery yields command-free guidance. Pinned by
ledger round-trip, scheduler boundary, status audit, and CLI recovery tests.

**Semantics: cluster access is advertised only for completed
installs.** `StorageSummariesForApply` (mirroring
`ClusterSummariesForApply`) advertises access only for clusters whose
install task actually completed in this apply run's ledger: the run must
be `RunStatusOK` and the cluster's `ApplyTaskKindStorageCluster` (or
`ApplyTaskKindInstallWait` for container clusters) task must be
`TaskStatusOK`. A skipped or failed cluster is never advertised as
reachable.

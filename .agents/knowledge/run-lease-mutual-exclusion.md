# Run lease: mutual exclusion, liveness, and takeover semantics

One short-lived lease admits one mutating run per context. These are the
invariants behind "a mutating run (…) is still running" and the self-heal
paths after crashes.

**Single acquisition point:** `AcquireRunLease` is claimed atomically BEFORE the
run writes its ledger, so a concurrent mutator that raced past the advisory
pre-mutation check loses at acquisition instead of silently overwriting the
in-flight run's lease. A stale lease is claimed by renaming it aside — the
rename succeeds for at most one racer — followed by an `O_EXCL` exclusive
create. Destroy mutates outside the apply scheduler, so `ExecuteDestroy` sets
`RunOptions.AcquireRunLease=true` and holds the lease for the whole teardown;
the destroy lease is labeled `destroy-…` so the still-running message does not
mislabel a destroy as an apply. Any new mutating path that bypasses the
scheduler must acquire the lease itself.

**Liveness decision tree:** `leaseLiveness` is shared by `leaseFresh` (advisory
pre-mutation check) and `AssessRunActivity` (acquisition/status gate) so the two
gates cannot disagree. Four arms: (1) a verifiably live LOCAL process is active
regardless of heartbeat age — a long privileged step outlasting the 2-minute
stale window (`ApplyLeaseStaleAfter=2m`, heartbeat every 15s) is still mutating
hosts, so a stale heartbeat alone must not invite a takeover; (2) no heartbeat
= stale; (3) a provably-gone local process = stale; (4) otherwise the
heartbeat-age window decides.

**PID reuse is never trusted:** after a hard crash an unrelated process could
hold the lease's PID and wedge the lease forever. The lease stores
`ProcessStart`, a per-process identity token that must also match the live
PID's current token (`localLeaseProcessAlive`); a missing token (older lease,
or non-Linux platform) or a mismatch means the process is not provably ours and
the caller falls back to the heartbeat-age rule. A remote-host lease (hostname
differs or unset) is never checked against the local process table.

**/proc parsing quirk:** `processStartToken` reads start time (clock ticks since
boot) from `/proc/<pid>/stat` field 22. Field 2 (comm) is parenthesized and may
itself contain spaces or `)` — the parser must skip past the LAST `)` before
splitting (starttime is index 19 of the post-comm slice). Returns `ok=false`
when the process is gone so callers fall back to the heartbeat rule.

**Takeover is a no-op signal, not a failure:** `ErrLeaseNotOwned` means the
on-disk lease was taken over after this run stalled past the stale window; the
caller must stop refreshing/removing it. `SaveRunLeaseIfOwner` (heartbeat tick)
and `RemoveRunLeaseIfOwner` (finishRun/cleanup, including `CancelRunLedger`)
exist so a resumed heartbeat or deferred cleanup never clobbers or deletes the
NEW holder's lease. Pinned by the M2 tests in `lease_core_audit_test.go`.

**Fail closed on lost ownership:** when a heartbeat tick discovers the lease was
taken over, the scheduler emits a distinct lease-lost error and stops — it must
not keep launching mutating tasks concurrently with the new holder. A heartbeat
SAVE failure in `workflow.Run` means exclusive ownership can no longer be
proven: the run context is cancelled (reaping the ansible process tree) and the
run fails.

**Teardown stops task launches:** once a run is being torn down (fatal
ledger/lease error, or Ctrl-C cancelling the context), the scheduler must stop
launching newly-ready tasks. A task freed while a killed task's failure event
drains would start under the dead ctx: `runOneApplyTask` stamps its
cluster-install record started, `exec.CommandContext` kills ansible instantly,
and a phantom started→failed record for work that never ran forces `--converge-drifted`
on the next apply. Running tasks drain; unstarted ones terminalize as blocked.

**--converge-drifted never preempts:** with another run in flight, `apply --converge-drifted`
fails closed before contacting any host — override authorizes destructive
rebuilds, not lease preemption.

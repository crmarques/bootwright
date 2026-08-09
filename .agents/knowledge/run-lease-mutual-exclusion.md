# Run lease: mutual exclusion, liveness, and takeover semantics

One short-lived lease admits one mutating run per context. These are the
invariants behind "a mutating run (…) is still running" and the self-heal
paths after crashes.

**Single transaction boundary:** `AcquireRunLease` is claimed atomically before
a real apply or destroy reads the desired input it will classify, and the
command holds it through remote mutation and all post-run record/reset work. A
concurrent mutator therefore cannot change input after classification, overwrite
an in-flight ledger, or enter while teardown cleanup is still deleting evidence.
A stale lease is claimed by renaming it aside — the
rename succeeds for at most one racer — followed by an `O_EXCL` exclusive
create. `context update`, `diff --adopt`, and `storage-cluster
replace-arbiter` acquire the same context lease before any desired-input write;
the arbiter's embedded apply and replacement run consume the outer lease rather
than reacquiring it. Command kinds are an explicit allowlist, so a new mutator
cannot silently invent an unlocked lease identity. Pinned by
`mutating_command_lease_test.go` and the active-lease CLI regressions.

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
and a phantom started→failed record for work that never ran forces `--mode rebuild`
on the next apply. Running tasks drain; unstarted ones terminalize as blocked.

**--mode rebuild never preempts:** with another run in flight, `apply --mode rebuild`
fails closed before contacting any host — override authorizes destructive
rebuilds, not lease preemption.

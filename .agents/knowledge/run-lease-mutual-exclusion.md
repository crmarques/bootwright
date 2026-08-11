# Run lease: mutual exclusion, liveness, and takeover semantics

One short-lived lease admits one mutating run per context. These are the
invariants behind "a mutating run (…) is still running" and the self-heal
paths after crashes.

**Single transaction boundary:** `AcquireRunLease` is claimed atomically before
a real apply or destroy reads the desired input it will classify, and the
command holds it through remote mutation and all post-run record/reset work. A
concurrent mutator therefore cannot change input after classification, overwrite
an in-flight ledger, or enter while teardown cleanup is still deleting evidence.
Acquire, owner-checked heartbeat replacement, and owner-checked removal hold a
cross-process advisory lock across their complete read/compare/write transaction.
A locally-owned stale lease is claimed by renaming it aside, followed by an
`O_EXCL` exclusive create. `context update`, `diff --adopt`, and `storage-cluster
replace-arbiter` acquire the same context lease before any desired-input write;
the arbiter's embedded apply and replacement run consume the outer lease rather
than reacquiring it. Command kinds are an explicit allowlist, so a new mutator
cannot silently invent an unlocked lease identity. Pinned by
`mutating_command_lease_test.go` and the active-lease CLI regressions.

**Liveness decision tree:** `leaseLiveness` is shared by `leaseFresh` and
`AssessRunActivity` so status and acquisition start from the same activity
classification. That classification never authorizes takeover by itself.
Four arms: (1) a verifiably live LOCAL process is active
regardless of heartbeat age — a long privileged step outlasting the 2-minute
stale window (`ApplyLeaseStaleAfter=2m`, heartbeat every 15s) is still mutating
hosts, so a stale heartbeat alone must not invite a takeover; (2) no heartbeat
= stale; (3) a provably-gone local process = stale; (4) otherwise the
heartbeat-age window decides.

**Takeover needs positive local fencing:** after liveness says stale,
`AcquireRunLease` permits automatic takeover only when the lease hostname is the
current controller and the recorded PID is absent/dead, or when both the stored
and current process-start identities are available and differ. A live PID with
a missing stored identity or an unreadable current identity is not proof that
the old holder stopped, regardless of heartbeat age; acquisition gives manual
inspection/recovery guidance and fails closed.

**PID reuse is never trusted:** after a hard crash an unrelated process could
hold the lease's PID and wedge the lease forever. The lease stores
`ProcessStart`, a per-process identity token that must also match the live
PID's current token (`localLeaseProcessAlive`). A matching token keeps the lease
active regardless of heartbeat age. An available mismatch positively identifies
a reused PID and permits takeover after the liveness classification becomes
stale. A missing stored token (older lease or non-Linux platform) or unreadable
current token is uncertain and refuses takeover. A remote-host lease (hostname
differs or unset) is never checked against the local process table. Its heartbeat
age may make status classify it stale, but acquisition never turns that remote
observation into automatic takeover: the operator must inspect the owning
controller and repair or remove the lease only after proving no such run remains
active, then repeat the exact command.

**/proc parsing quirk:** `processStartToken` reads start time (clock ticks since
boot) from `/proc/<pid>/stat` field 22. Field 2 (comm) is parenthesized and may
itself contain spaces or `)` — the parser must skip past the LAST `)` before
splitting (starttime is index 19 of the post-comm slice). Returns `ok=false`
when the token cannot be read. A separately proven dead PID permits takeover;
an alive PID with this unreadable token fails closed.

**Takeover is a no-op signal, not a failure:** `ErrLeaseNotOwned` means the
on-disk lease was taken over after this run stalled past the stale window; the
caller must stop refreshing/removing it. `SaveRunLeaseIfOwner` (heartbeat tick)
and `RemoveRunLeaseIfOwner` (finishRun/cleanup, including `CancelRunLedger`)
exist so a resumed heartbeat or deferred cleanup never clobbers or deletes the
NEW holder's lease. Pinned by the M2 tests in `lease_core_audit_test.go`.
The ownership comparison and matching save/remove are one serialized transaction;
the deterministic barrier tests pause after comparison and prove a concurrent
takeover cannot interleave before the old operation completes.

**Acquisition is the only production replace API:** `AcquireRunLease` owns
exclusive creation and stale replacement. There is no unconditional exported
lease-save operation; test fixtures that need to seed impossible or historical
states live in `_test.go`. Production heartbeat can only replace the file after
`SaveRunLeaseIfOwner` compares the run ID inside the serialized transaction.

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

**Controller leases do not fence another controller:** every mutating Ansible
run receives `bootwright_host_mutation_lease`, an immutable identity derived
from the held command lease. A role that changes a host-global service must use
it to atomically acquire one unique attempt guard before its first side effect,
bind that guard to the exact context, host, physical consequence, operation,
and durable transition claim, recheck it at each mutation boundary, and release
it last. Matching desired state does not let two attempts share a guard: a
stalled same-verb attempt could otherwise resume after an opposite verb has
completed and mutate freshly recreated state. A normal Ansible failure releases
its own guard only after durable retry evidence is retained. A controller crash
may strand the guard; no age-based or cross-controller takeover is allowed, so
the refusal must name its run/controller/PID/path, require independent proof
that it stopped, and preserve the exact Bootwright retry.

**Persistent claims are consequence-keyed:** the attempt guard prevents
simultaneous mutations, while durable steady or transition claims prevent a
later differently named component from adopting the same singleton daemon,
global path, pool, or protocol/port. Logical provider/component names alone are
not mutual-exclusion keys. Host-path creation and claim compare-and-swap must
walk every ancestor without following symlinks; an Ansible `file` task cannot
create or chmod a claim parent before that proof because its directory path
handling follows symlinks by default.

# ADR 0058: Storage Destroy Completion Is Positive Proof

## Status

Accepted

Revises the managed-storage clauses of A2 and the fail-closed edge set in
[ADR 0023](0023-teardown-is-the-inverse-of-buildup.md). Follows the destructive
safety model of [ADR 0007](0007-apply-destroy-safety-model.md) and the proven
absence rule of
[ADR 0039](0039-the-node-a-teardown-left-serving-the-cluster.md).

## Context

A full destroy reported success and purged its history while the Ceph seed still
held five fresh OSD PVs and VGs. The final whole-node scan had observed those
rows, but its survivor predicate intersected them with the devices selected by
the initial scan. When cephadm recreated the VGs after an initially empty scan,
that intersection was empty and the assertion passed. The role then removed
host and controller ownership evidence.

The controller could not detect the false green. Its result artifact described
only skipped nodes, so an empty report meant both “every node completed” and
“no positive completion was recorded.” An Ansible exit status of zero made the
storage ledger successful, released downstream registration, access, and
substrate work, and authorized convergence, secret, and history cleanup.

## Decision

Each reachable storage node produces a bounded terminal proof from fresh
whole-node LVM rows. The scanner batches `pvs` and `lvs`, reads both tables twice
inside each sample, probes for ceph-volume writers around the sample, and
requires three identical writer-free samples spanning a minimum quiet window.
The terminal classifier consumes those final rows directly; it never filters
through the initial sweep's device set.

Every storage task writes a strict, versioned attestation with exactly one node
object for every selected managed-storage topology node. A completed node binds
its canonical node and inventory-host identity to the full-node scan scope,
nonnegative row count, zero owned survivors, scan digest, successful scan, and
same-host completion witness. A skipped node is terminal only when the task
consumed `unreachable-nodes` and carries a positive machine-readable absence
classification plus its diagnostic. Preliminary, missing, malformed,
duplicate, unknown, and incomplete evidence is failure.

The task boundary validates the exact selected topology before returning
success. A complete result binds the destroyed fsid to the exact controller
owner and durably stages the proof there as `proof-validated`; a partial result
retains or stamps the owner when one exists and remains in the convergence-reset
exclusion even when none exists. A partial attestation is retained bookkeeping,
not task success; it fails that storage branch after stamping the available
evidence. A complete ownerless no-fsid proof instead writes an exact
`release-pending` receipt before remote evidence release. That state may replay
only the release pass; it proves neither remote completion nor controller reset.

The same storage worker then runs a release-only pass. Every manifest node is
bound to its exact inventory host. Before deleting evidence, every host rechecks
the target fsid directory, active Ceph units, ownership marker and configuration,
and a fresh whole-node LVM quiet scan; a controller boundary asserts exact host
coverage. Failure before that boundary invalidates `proof-validated`, so the
next retry repeats destructive teardown. Failure after the boundary retains the
staged proof and retries only the idempotent evidence commit.

After every host releases its evidence, the controller writes or advances a
separate, fsync-durable receipt to `reset-pending`, marks any remaining exact owner
`evidence-released`, and only then lets the worker report success. The same
release-only pass also consumes exact Bootwright host markers for a
complete ownerless no-fsid absence attestation before that worker may succeed.
Both `reset-pending` and `completed` make destroy replay controller-only.
Post-run cleanup first durably resets converge, secret, substrate-release, and
requested history state, then advances every receipt to `completed` before
removing the exact `evidence-released` owner.

Before either storage-infrastructure or base storage apply can mutate a host,
`release-pending`, `reset-pending`, or an exact staged owner refuses apply and requires the
original completed topology to finish its destroy reset. A `proof-validated`
owner without remote-completion receipt authority is retained but has the stale
proof durably invalidated. Apply then changes any superseded `completed`
receipt to `apply-started` before remote mutation. That non-authorizing state
survives crashes and desired-topology changes but forces the next destroy to run
fresh destructive proof; a normal exact owner for the new lifecycle supersedes
the older result, and successful release starts the receipt cycle again.
Registration cleanup, node-access revoke, and machine
substrate for a selected storage cluster require that cluster's successful
storage task; node-access revoke also requires its registration cleanup.
Independent clusters and unrelated graph branches retain ADR 0023's
ordering-only concurrency.

## Consequences

- Silence is never storage completion. A zero-exit Ansible process with no exact
  terminal artifact fails the storage ledger and cannot print a successful
  destroy summary.
- A late seed-only OSD recreation is either swept from the fresh final rows or
  named as a survivor before ownership evidence is released.
- Missing proof retains ownership, convergence records, captured secrets, and
  history. A retry uses the same identity instead of requiring ownership
  recovery because a prior false success erased it.
- Failure or interruption after proof validation cannot repeat cluster removal
  or strand a half-released host. The staged owner proof or ownerless
  `release-pending` receipt replays the release phase, and the remote-completion
  receipt replays controller cleanup after
  host access or substrate is legitimately gone.
- Authorized unreachable nodes remain an explicit partial result and never
  satisfy the success dependencies that protect registration, access, and
  substrate needed by an exact retry.
- Healthy hosts pay a short quiet window, in parallel. Batched table reads
  remove the old per-VG `lvs` process fan-out, and every probe shares one bounded
  deadline.

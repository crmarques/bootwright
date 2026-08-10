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
success. The scheduler persists that success before releasing the exact
controller owner; partial results retain or stamp the owner when one exists and
remain in the convergence-reset exclusion even when none exists. Host evidence
is released only after the terminal artifact is written. Registration cleanup,
node-access revoke, and machine substrate for a selected storage cluster require
that cluster's successful storage task; node-access revoke also requires its
registration cleanup. Independent clusters and unrelated graph branches retain
ADR 0023's ordering-only concurrency.

## Consequences

- Silence is never storage completion. A zero-exit Ansible process with no exact
  terminal artifact fails the storage ledger and cannot print a successful
  destroy summary.
- A late seed-only OSD recreation is either swept from the fresh final rows or
  named as a survivor before ownership evidence is released.
- Missing proof retains ownership, convergence records, captured secrets, and
  history. A retry uses the same identity instead of requiring ownership
  recovery because a prior false success erased it.
- Healthy hosts pay a short quiet window, in parallel. Batched table reads
  remove the old per-VG `lvs` process fan-out, and every probe shares one bounded
  deadline.

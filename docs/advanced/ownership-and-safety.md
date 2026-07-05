---
title: Ownership, idempotency & safety
description: How Bootwright tracks ownership, what makes re-running apply safe, the fail-closed checkpoints, and how operators avoid accidentally destroying or breaking clusters.
---

# Ownership, idempotency, and safety

Bootwright is built to be re-run. A bare `apply` reconciles: it creates what is
missing, skips what already matches, and **fails closed** on anything it did not
create or cannot safely resume — *before* it mutates a single host. That makes
the everyday loop (`validate` → `plan` → `apply` → `apply` again) safe by
construction.

This page explains the machinery behind that promise — how Bootwright knows what
it owns, the checkpoints that gate every destructive action, and the operator
habits that keep you from accidentally rebuilding a node, wiping a Ceph cluster,
or tearing down shared services other clusters depend on.

For the conceptual model see
[The desired-state model](../concepts/index.md#convergence-and-drift); for the
execution internals see [Architecture](../contributing/architecture.md); for the
recovery procedures these guardrails protect, see
[Operations, recovery & teardown](operations.md).

## How ownership works

Every operational fact has exactly one owning object
([object ownership](../concepts/index.md#object-ownership)). As Bootwright
mutates a host it writes durable **ownership evidence** so later runs — and
`destroy` — can reason about what it created versus what was already there:

| Record | What it proves | Used for |
| --- | --- | --- |
| Per-host ownership records | The exact resources and packages Bootwright created or configured on a host | Destroy scoping, package-removal gating, orphan reporting, `diff` |
| Convergence-safety records | A non-secret desired hash plus a Bootwright owner identity for a mutated resource | Drift and foreign-ownership classification |
| Per-cluster install records | A non-secret fingerprint of the install inputs and the phase reached | Skipping completed installs and resuming only from known-safe phases |
| Provider VM markers | That a substrate VM is one Bootwright created | Bounded teardown — never touching co-resident VMs |

These live under the protected context state directory
(`/var/lib/bootwright/contexts/<context>/`) and are the source of truth for every
skip, resume, and teardown decision. They never contain secret bytes.

!!! note "Owned versus foreign is the central distinction"
    Bootwright only ever rebuilds or removes resources its own records prove it
    created. A resource with a non-Bootwright owner — or a Ceph cluster that is
    co-resident but not the one Bootwright bootstrapped — is **foreign**, and
    every destructive path fails closed on it rather than guessing. This is what
    lets Bootwright share a host with things it does not manage.

## What makes re-running apply safe

Re-running `apply` compares recorded evidence against current desired state and
classifies each resource into one of four outcomes. `bootwright diff --recorded`
reports exactly this, read-only, against recorded evidence — it never contacts
hosts (plain `bootwright diff` instead compares desired state against the *live*
clusters; see [Operations](operations.md#comparing-against-live-cluster-state)):

| Outcome | Meaning | Reconcile behavior |
| --- | --- | --- |
| `missing` | No record exists | Create it |
| `match` | Recorded desired hash equals current | Skip (when a concrete probe supports it) |
| `drift` | Recorded desired hash differs | Fail closed — needs `--override` (config-only kinds re-apply in place; machines/clusters rebuild destructively) |
| `foreign` | Record carries a non-Bootwright owner | Fail closed — never touched |

Under `--override`, the consequence depends on the object kind: for the
reconfigure-only kinds (provider host services, infra-component services,
node-config apply, per-host `virtctl`, cluster add-ons, storage-attachment apply)
it is an idempotent in-place re-apply that touches no data, OS, or VM; for a
machine (managed-OS reinstall, disks wiped) or a container/storage cluster
(reinstall / `cephadm rm-cluster --zap-osds`) it is a destructive rebuild. Only
the destructive kinds cross the destroy-protection boundary (below).

!!! warning "Classification is not a blanket skip gate"
    Before any mutation, a records-based apply-mode preflight fails closed when
    any selected object is `drift` or `foreign`, for **every** kind — so plain
    `apply` never silently reconciles drift. Once a run proceeds (a clean run, or
    `--override`), execution-time behavior differs by task: many provider-service
    and component-config tasks have no reliable external probe, so they **re-run
    and rely on idempotent execution** rather than being skipped — their record is
    durable evidence, not a skip decision. Concrete-probe sites — cluster install
    records, add-on records, managed-OS markers, provider metadata, and storage
    comparisons — decide skip-vs-fail against live state.
    Cluster install reconcile reads the per-cluster install record, probes live
    cluster availability, skips completed installs, resumes only from known-safe
    phases, and refuses to proceed when install state exists for different inputs
    after node boot — unless you pass `--override`.

The practical consequence: an interrupted `apply` is resumable, and a completed
`apply` re-run is a near-no-op. You do not get a destructive surprise from simply
running it again.

## The safety checkpoints

Every `apply` passes through a sequence of fail-closed gates. Each one stops the
run *before* any mutation if its precondition is not met:

1. **Offline validation.** Strict decode (unknown and retired fields are
   rejected, never translated) plus ownership and cross-reference checks run
   before any host is contacted. See [Troubleshooting](../troubleshooting.md) for
   the diagnostics.
2. **Host trust.** Bootwright uses strict SSH host-key checking for non-local
   durable machines. A *changed* key is never accepted automatically, and trust
   is never recorded under `--yes`, `--dry-run`, or JSON output. Record it up
   front with `bootwright machine trust`.
3. **Run lease.** A short-lived lease admits one mutating run at a time, so two
   applies cannot race the same context.
4. **Foreign-ownership refusal.** Drift and foreign outcomes fail closed; a
   plain `apply` never overwrites a resource it does not own.
5. **Concrete-probe gating.** Install, add-on, managed-OS, provider, and storage
   sites refuse to reinstall or rebuild over existing state without an explicit
   `--override`.
6. **Destroy protection.** When desired state sets `destroyProtection`, even
   `apply --override` fails closed on protected rebuilds (see below).
7. **Render as a second enforcement line.** Rendering fails before writing any
   tool input when an endpoint load-balancer bind or a managed Ceph topology host
   address does not resolve, instead of emitting empty values.

## Apply modes and what they protect

`apply` has three modes. The default is the safe one; the other two are
deliberate ([full reference](../concepts/index.md#apply-modes)):

| Mode | Use it to | Destructive? |
| --- | --- | --- |
| `apply` (reconcile) | Build out and converge day to day | No — fails closed on drift/foreign |
| `apply --expect-new` | Assert a greenfield build — refuse if **any** selected object already exists | No — it only refuses |
| `apply --override` | Break-glass: rebuild Bootwright-owned drift it knows how to rebuild | **Yes** — see below |

`--expect-new` is a guardrail, not a risk: use it on a first build so a stale
context or a name collision fails loudly instead of half-converging.

!!! danger "`--override` can destroy data"
    `apply --override` is the only `apply` form that destroys. It may reinstall a
    managed-OS machine (the substrate VM is undefined, **disks are wiped**, then
    rebuilt) and cleanly rebuild a managed Ceph cluster via
    `cephadm rm-cluster --zap-osds` — and only when a Bootwright ownership marker
    proves the live cluster is the one Bootwright created. It **never** touches
    foreign objects, run leases, validation, or secret checks. Always
    `plan`/`--dry-run` an override first and read exactly what it intends to
    rebuild.

## Avoiding accidental destruction

A few habits keep operators on the safe side of these guardrails:

- **Re-running `apply` is safe — lean on it.** It never silently destroys. Only
  `apply --override` and `destroy` mutate destructively, and both are gated.
- **Preview first.** Run `bootwright plan` (or `apply --dry-run`) and read the
  graph before any override or teardown. `diff` shows drift without
  touching hosts.
- **Limit blast radius with scope.** `--clusters <names>` and `--stage` narrow a
  run to specific clusters or to the `infra`/`clusters` family. Scope an override
  to the one cluster you mean to rebuild, not the whole fleet. Note a scoped
  apply cannot silently narrow a shared service another cluster still needs (see
  [Multi-cluster fleets](fleets.md)), and a KubeVirt child needs its parent in
  scope (see [KubeVirt nested clusters](kubevirt.md)).
- **Protect production.** Set destroy protection in desired state so any
  destructive rebuild must first cross the destroy-authorization boundary:

    ```yaml
    spec:
      safety:
        destroyProtection: requiredOverride
    ```

    With this set, `apply --override` fails closed on protected resources and
    directs you to run `destroy --override` for the affected scope first — a
    second, explicit decision.

- **Know what `destroy` does.** The full teardown is the *no-selector* form
  (`destroy`), which removes clusters then infra. `destroy --clusters <names>`
  narrows to the clusters stage. Disk cleanup is bounded to provider-owned disks
  or declared Bootwright-managed devices — Bootwright never wipes arbitrary
  visible disks — and `--force-unowned` / `--skip-unreachable` are explicit
  opt-ins, never defaults. See [Operations, recovery & teardown](operations.md).
- **Keep state safe to commit.** Desired state names secrets but never carries
  bytes; keep pull secrets, keys, and kubeconfigs out of Git
  ([Secrets & entitlements](../concepts/secrets.md)).

## Verifying ownership and safety state

| Question | Command |
| --- | --- |
| What will this run do? | `bootwright plan` / `bootwright apply --dry-run` |
| Has anything drifted from the last apply? | `bootwright diff` |
| What is the current run doing? | `bootwright status` / `status --watch` |
| What does Bootwright own here, and what is orphaned? | `bootwright diff` and the orphan reporting in [Operations, recovery & teardown](operations.md) |

`diff` and `status` read recorded evidence and the run ledger without
contacting provider hosts, BMCs, or clusters, so they are always safe to run.

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
[Operations, recovery & teardown](operations.md). The normative model is
[ADR 0030](https://github.com/crmarques/bootwright/blob/main/specs/adr/0030-one-intent-flag-and-named-authorizations.md)
refined by
[ADR 0031](https://github.com/crmarques/bootwright/blob/main/specs/adr/0031-data-loss-follows-the-data-and-policy-is-not-drift.md)
(ADR 0007 is the superseded base they revise)
and the CLI contract in
[specs/state-model.md](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md#cli-contract);
this page is the operator-facing rendering.

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

A machine installed by cloning a vSphere template records exactly what an
Anaconda-installed one does. Its install ownership record carries the same
`managed-os-install` kind, distinguished only by `attributes.installer:
templateClone`, and it names no provider-host paths because a clone stages
nothing on a provider host. The cloned VM itself carries the same
`bootwright:context=… bootwright:cluster=… bootwright:machine=…` annotation as any other Bootwright
VM, so the existing vSphere substrate teardown removes it. There is no new
ownership kind, no new destroy verb, and no new `--authorize` token for this
install mode — including its failure modes: a VM created by a role that could
not write the annotation is unowned in the ordinary sense and needs
`--authorize unowned-vms` like any other.

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
| `drift` | Recorded desired hash differs | Reconcilable-in-place drift converges on a bare `apply`; structural (destructive-identity) drift fails closed — needs `--mode rebuild` (reconfigure-only kinds re-apply in place; machines/clusters rebuild destructively) |
| `foreign` | Record carries a non-Bootwright owner | Fail closed — never touched |

Under `--mode rebuild`, the consequence depends on the object kind: for the
reconfigure-only kinds — enumerated in the
[`--mode rebuild` taxonomy](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md#cli-contract),
for example cluster add-ons and machine RHSM registration — it is an idempotent
in-place re-apply that touches no data, OS, or VM; for a
machine (managed-OS reinstall, disks wiped) or a container/storage cluster
(reinstall / `cephadm rm-cluster --zap-osds`) it is a destructive rebuild. Only
the destructive kinds cross the destroy-protection boundary (below).

!!! warning "Classification is not a blanket skip gate"
    Before any mutation, a records-based apply-mode preflight fails closed when
    any selected object carries structural (destructive-identity) drift or is
    `foreign`, for **every** kind. Drift that is reconcilable in place — an
    OSD-device add, a `ceph … set`-reconcilable sub-object edit, day-2-owned
    container intent — converges on a bare `apply`; what plain `apply` never
    does is destructively rebuild, or touch what it does not own. Once a run
    proceeds (a clean run, reconcilable drift, or
    `--mode rebuild`), execution-time behavior differs by task: many provider-service
    and component-config tasks have no reliable external probe, so they **re-run
    and rely on idempotent execution** rather than being skipped — their record is
    durable evidence, not a skip decision. Concrete-probe sites — cluster install
    records, add-on records, managed-OS markers, provider metadata, and storage
    comparisons — decide skip-vs-fail against live state.
    Cluster install reconcile reads the per-cluster install record, probes live
    cluster availability, skips completed installs, resumes only from known-safe
    phases for at most three hours from the original start, and refuses to
    proceed when install state exists for different inputs after node boot —
    unless you pass `--mode rebuild`. ISO creation records the installer version:
    node boot resumes only when the recorded publish time proves the ISO is less
    than 24 hours old, while a missing, future, or expired time requires
    regenerating the cluster ISO first. Version skew before boot likewise
    requires regenerating the ISO; skew discovered after boot
    finishes the in-flight install, retains its evidence, and leaves the run
    nonzero until a deliberate future rebuild.

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
   applies cannot race the same context. Work that changes a machine-hosted
   shared service also holds a controller-global lease, preventing two contexts'
   first applies from both claiming the same host; a durable host context claim
   keeps that proof after a partial failure.
4. **Foreign-ownership refusal.** Structural drift and foreign outcomes fail
   closed; reconcilable-in-place drift converges; a plain `apply` never
   overwrites a resource it does not own.
5. **Concrete-probe gating.** Install, add-on, managed-OS, provider, and storage
   sites refuse to reinstall or rebuild over existing state without an explicit
   `--mode rebuild`.
6. **Powered-off installer boot.** Booting install media on a managed-OS
   machine — the OpenShift agent ISO or the Anaconda ISO — requires the machine
   to be observably powered off first, on every substrate (Redfish
   `PowerState=Off`, vCenter `poweredOff`, a provenly absent KubeVirt VMI). An
   unreadable power state fails closed, and no `--authorize` token stands in:
   powering the machine off is itself the operator's confirmation that nothing
   on it is still needed. A machine already running its Bootwright-installed OS
   is never re-booted by this path, so converge runs are untouched (ADR 0050).
7. **Destroy protection.** When desired state sets `destroyProtection`, even
   `apply --mode rebuild` fails closed on protected rebuilds (see below).
8. **Render as a second enforcement line.** Rendering fails before writing any
   tool input when an endpoint load-balancer bind or a managed Ceph topology host
   address does not resolve, instead of emitting empty values.
9. **Foreign state on the node itself.** A storage node still running the
   cephadm units of a Ceph cluster this apply does not own refuses before the
   cluster is bootstrapped, because those daemons hold the host ports its own
   daemons need. `--authorize foreign-daemons` removes exactly those identities
   and nothing else — see
   [Enrolling a node another Ceph cluster was left running on](operations.md#enrolling-a-node-another-ceph-cluster-was-left-running-on).

## Apply modes and what they protect

`apply --mode` has three values — `create` (assert a greenfield build),
`reconcile` (the default) and `rebuild` (break-glass) — described in full under
[Apply modes](../concepts/index.md#apply-modes). What matters here is how each
treats resources Bootwright does **not** own:

- `--mode reconcile` and `--mode create` are non-destructive. `reconcile`
  converges reconcilable-in-place drift and fails closed on structural drift or
  foreign ownership; `create` additionally refuses if any selected object
  already exists — a guardrail for first builds, so a stale context or a name
  collision fails loudly instead of half-converging.
- `--mode rebuild` is the only mode that destroys **owned drift**, and only over
  resources a Bootwright ownership marker proves it created (see below).

!!! danger "`apply` can destroy data — in four forms, not one"
    `--mode rebuild` is the form that rebuilds drifted objects, but it is not the
    only `apply` that destroys. Four paths reach data loss:

    - **`--mode rebuild`** may reinstall a managed-OS machine (the substrate VM is
      undefined, **disks are wiped**, then rebuilt) and cleanly rebuild a managed
      Ceph cluster via `cephadm rm-cluster --zap-osds`.
    - **`--reclaim-devices`** wipes the named OSD devices (or, with `all`, every
      declared OSD device of the selected owned cluster(s)) under the default
      `--mode reconcile`. It is the one deliberate exception to "reconcile never
      destroys" (ADR 0007).
    - **A reinstall a prior `destroy` released.** Once teardown records a
      substrate release, the next plain `apply` re-creates those machines and
      wipes their disks — that release *is* the authorization, so no mode flag is
      involved.
    - **`--authorize foreign-daemons`** removes another cluster's cephadm daemons,
      units and `/var/lib/ceph` state from a node being enrolled (checkpoint 9
      above). It zaps no disk, and it carries its own token rather than the
      data-loss gate.

    Whichever form authorizes the loss, the wipe itself still passes checkpoint
    6: a machine whose OS is about to be (re)installed must be powered off when
    the run reaches its installer boot, and no flag stands in for that.

    The first three share a **separate** data-loss gate: they are authorized only
    by the interactive data-loss prompt, or by `--authorize data-loss` paired with
    `--yes` for automation — `--yes` alone does **not** authorize data loss (see
    [Operations](operations.md#the-fail-closed-mode-rebuild-contract)). None of
    them touches foreign objects, run leases, validation, or secret checks. Always
    `plan`/`--dry-run` first: the preview prints a `Required authorizations` block
    naming every token the real run would consult.

## Avoiding accidental destruction

A few habits keep operators on the safe side of these guardrails:

- **Re-running `apply` is safe — lean on it.** It never silently destroys. Only
  `apply --mode rebuild` and `destroy` mutate destructively, and both are gated.
- **Preview first.** Run `bootwright plan` (or `apply --dry-run`) and read the
  graph before any override or teardown. `diff` shows drift without
  touching hosts.
- **Limit blast radius with scope.** `--clusters <names>` and `--stage` narrow a
  run to specific clusters or to the `infra`/`clusters` family, and `--machines
  <names>` narrows it to individual `Machine`s for a per-machine rebuild or
  teardown. Scope an override to the one cluster or machine you mean to rebuild,
  not the whole fleet. Note a scoped apply cannot silently narrow a shared
  service another cluster still needs (see [Multi-cluster fleets](fleets.md)),
  and a KubeVirt child needs its parent in scope (see
  [KubeVirt nested clusters](kubevirt.md)).
- **Protect production.** Set destroy protection in desired state so any
  destructive rebuild must first cross the destroy-authorization boundary:

    ```yaml
    spec:
      safety:
        destroyProtection: protected
    ```

    With this set, `apply --mode rebuild` fails closed on protected resources and
    directs you to run `destroy --authorize protected` for the affected scope
    first — a second, explicit decision.

    Turning protection on (or editing `protectedKinds`) is safe to do on a live,
    applied context: `spec.safety` is authorization policy that reaches no host,
    so it is excluded from the recorded desired hash and the next `apply` sees no
    drift from it.

- **Know what `destroy` does.** Omitting `--stage` requests a full lifecycle
  teardown: `destroy` covers the context and `destroy --clusters <names>`
  narrows it to those roots. Positively owned virtual machines are deleted;
  bare-metal hardware and its installed OS are retained. Use `--stage clusters`
  to keep machine substrate. Disk cleanup is bounded to provider-owned disks or
  declared Bootwright-managed devices — Bootwright never wipes arbitrary visible
  disks — and every `--authorize` token is an explicit opt-in, never a default.
  The complete list of named authorizations, what each one unblocks, and which
  verbs accept it lives in
  [Operations → the two axes](operations.md#the-two-axes-intent-and-authorization);
  that table is the only place the vocabulary is maintained. `--yes` grants
  none of them; a teardown that zaps OSDs needs `--authorize data-loss`.
- **`--authorize all` is bounded, not blanket.** It stands in for exactly the
  tokens the invoked verb accepts, clears no refusal that has no token of its
  own, and never answers a confirmation prompt; a real run that used it prints
  which tokens it stood in for. Reach for it deliberately — see the
  [blast-radius warning](operations.md#the-two-axes-intent-and-authorization).
- **Keep state safe to commit.** Desired state names secrets but never carries
  bytes; keep pull secrets, keys, and kubeconfigs out of Git
  ([Secrets & entitlements](../concepts/secrets.md)).

## Tearing down a context whose input no longer decodes

`destroy` plans a teardown from the context's stored input *plus* the ownership
records, so it loads that input through the same strict decoder and validator
`apply` uses — before it reads a record or contacts a host. `v1alpha1` carries no
aliases and no migrations, so after a breaking schema change an already-applied
context cannot be torn down until its input is schema-current. The refusal is
all-or-nothing: one stale document blocks the whole context, including objects
the run would not have touched.

The intended fix is to re-render the input for the current schema and adopt it
with `bootwright context update`, keeping every applied identity — context,
`Environment`, `InfraProvider`, `Machine`, cluster, and node names — because
teardown matches ownership records by those names.

When that is not practical, `--authorize stale-input` is the escape hatch:

```text
bootwright destroy --dry-run --authorize stale-input
bootwright destroy --authorize stale-input --yes
```

It skips exactly the documents that fail to decode or validate, and reports both
them and the ownership records left without a declaration — those resources are
**not** in the work set and are left standing. Because the run cannot tell a
skipped declaration from an intentionally deleted one, it also disables every
record-only or context-wide orphan sweep, including controller-resolver
preflight/cleanup. This can conservatively leave a genuine orphan; use fully
decodable input for the later reclaim. Like every token it is refused by
default, and it is registered for `destroy` only: `apply`, `plan`, `diff`, and
`context init`/`update` reject it by name, so nothing that *creates* state can
ever build from input it cannot fully read.

!!! warning "It authorizes one refusal and nothing else"
    `stale-input` relaxes no other token and no un-tokened refusal: every
    other gate — the device data-safety checks, the Ceph seed ownership proof,
    the active-run check, and the confirmation prompt included — is still
    evaluated against whatever *did* decode.

## Verifying ownership and safety state

The ordered pre-apply ritual — every inspect command, in the order the guides
run them, with what each touches — is taught once, in
[Installation → Bastion prep, then the read-only checks](../getting-started/installation.md#bastion-prep-then-the-read-only-checks).
This table answers the standing questions:

| Question | Command |
| --- | --- |
| What will this run do? | `bootwright plan` / `bootwright apply --dry-run` |
| Has anything drifted from the last apply (offline)? | `bootwright diff --recorded` |
| Has the live cluster drifted from desired state? | `bootwright diff` (contacts clusters read-only) |
| What is the current run doing? | `bootwright status` / `status --watch` |
| What does Bootwright own here, and what is orphaned? | `bootwright diff --recorded`, plus [Troubleshooting → Resources no longer in desired state (orphans)](../troubleshooting.md#resources-no-longer-in-desired-state-orphans) |

`diff --recorded` and `status` read recorded evidence and the run ledger without
contacting provider hosts, BMCs, or clusters, so they are always safe to run.
Plain `diff` instead contacts the clusters read-only — SSH to the Ceph seed for
discovery, a `ClusterVersion` probe per container cluster — and is likewise
non-mutating.

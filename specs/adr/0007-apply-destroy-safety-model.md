# ADR 0007: Apply/Destroy Safety Model

## Status

Accepted

The teardown ordering clause is revised by
[ADR 0023](0023-teardown-is-the-inverse-of-buildup.md). The gate *surface* — the
flag names that authorize each refusal, and the apply-mode vocabulary — is
revised by
[ADR 0030](0030-one-intent-flag-and-named-authorizations.md), and refined by
[ADR 0031](0031-data-loss-follows-the-data-and-policy-is-not-drift.md), which
scopes the data-loss gate to what a run actually destroys and excludes
controller-side authorization policy from recorded evidence; the model below is
otherwise unchanged and still governs.

Refined by
[ADR 0054](0054-a-filter-is-not-permission-to-wipe-a-device.md): rebuild intent
and data-loss authorization never substitute for explicit OSD device selection.

## Context

Bootwright's converge loop mutates real infrastructure: it reimages machines,
zaps OSD devices, boots clusters, and tears them down. The same declarative
input that makes day-to-day reconciliation cheap makes an unguarded re-run
catastrophic — a renamed cluster reads as greenfield, a drifted hash reads as
"rebuild me", and a shared bastion service looks deletable from every context
that references it.

The safety machinery grew organically across the state engine: convergence
records, ownership records, apply modes, destroy protection, scope filtering,
and the run lease each solved one incident class. This ADR records the unified
model they now implement, so future changes extend the model rather than
re-derive it per feature.

## Decision

### Recorded evidence drives classification, not live probing

Every apply task records a non-secret desired hash (plus, for install-bearing
kinds, a structural projection excluding day-2-owned intent) keyed by task
identity. One classification primitive — missing / match / drift / foreign —
is shared by the apply-mode preflight and `diff --recorded` (the internal
StateCheck engine), and one predicate (`taskDriftReconcilable`) decides
reconcilable-in-place versus structural for all of them, so gate and report can
never disagree. Classification covers only desired state that reaches a host: `Environment.spec.safety`
is authorization policy the gates read and nothing renders, so it is excluded from
every hash — otherwise enabling destroy protection would read as fleet-wide
structural drift whose only remedy the protection gate itself refuses (ADR 0031).
Classification compares desired against *recorded* desired only;
out-of-band live divergence is deliberately invisible here and belongs to the
per-role Ansible reconcile gates and `diff` (live). Records are written only on
task success; destroy removes them (including storage sub-object records) so
torn-down objects reclassify as missing, and a partial teardown
(`--authorize unreachable-nodes`) keeps them so `apply --mode create` fails
closed atop residual state.

Destruction also leaves a positive re-authorization trail. On destroy,
Bootwright writes a per-cluster substrate release that records that the
cluster's substrate was deliberately torn down and so authorizes the subsequent
`apply` to reinstall it. The release is a fail-safe token like the others here —
its absence can only withhold, never manufacture, authority to reinstall —
closing the window where a re-run after a destroy would otherwise have to guess
whether a missing substrate is greenfield intent or an interrupted teardown.
That asymmetry is also why a teardown without per-node completion proof
withholds the release rather than granting it optimistically. Its granularity,
the cases that withhold it, and how an apply consumes it are specified in
[`state-model.md`](../state-model.md) ("CLI Contract"). Because a bare-metal
destroy defers the disk wipe to the reinstall, a release-authorized apply
covering bare-metal managed-OS machines is the moment data is lost and still
crosses the data-loss acknowledgment.

### One explicit mode variable, enforced on both sides

The safety mode is a single extra-var, `bootwright_apply_mode`
(`create` | `reconcile` | `rebuild`), stamped by plan composition from the
`--mode` value with no translation (it replaced a legacy boolean
`bootwright_install_override`). The Go object preflight enforces it against
recorded state (`reconcile`, the default, refuses structural drift and foreign
but converges reconcilable-in-place drift; `create` refuses pre-existing;
`rebuild` refuses only foreign); the per-role Ansible apply-mode gates enforce
the identical contract against live existence and ownership. Defense in depth is
intentional: Go decides from records, roles from the host.

### Ownership is the authorization boundary for destruction

No destructive action proceeds without proof that Bootwright created its
target: ownership records on the controller, substrate markers (libvirt domain
XML, vSphere annotations, KubeVirt labels, managed-OS install markers), and
live container provenance labels for shared bastion services. Foreign fails
closed in every mode; `--mode rebuild` rebuilds owned drift but never adopts.
The controller record locates a live target but cannot overrule it. Every apply
reuse and every declared or orphan removal re-probes the external object and
accepts only a successful `NotFound` or exact live manager/context/resource
identity; each disk or volume in a composite target is classified separately.
A failed, forbidden, empty, malformed, or suppressed probe is unknown state,
and a stale record with a matching name cannot authorize mutation of a foreign
replacement. Evidence is removed only after the live remover succeeds or the
same conclusive probe proves absence.
Unprovable is not absent: a live host that answers SSH but rejects every probe
identity (or presents a changed host key) cannot prove ownership either way,
so the managed-OS install fails closed on it exactly as on a foreign host —
only the machine's substrate release, written by a destroy, authorizes
reclaiming it. The same rule governs cluster-level probes: a *failed* probe is
unknown state, never a rebuild authorization. A `ContainerCluster` whose
recorded install inputs match desired state but whose availability cannot be
probed at all fails closed in every mode including `--mode rebuild`, while a
probe that succeeds and answers `Available=False` is real evidence and may
authorize the rebuild. Destroying a matching-but-dead object is the operator's
explicit `destroy` decision, not an inference an apply flag draws from a
timeout.

Rebuilding that probed-unavailable cluster is the one place `--mode rebuild`
acts on an object whose recorded desired state still matches, and it is
deliberate: the object matches its *declaration* but not its recorded
*condition*, the cluster itself supplied that evidence, and repairing a dead
cluster in place is the case the flag exists for. It stays bounded by the
data-loss acknowledgment and by ownership, and the plain-`apply` path refuses the
same cluster with guidance to repair it instead. Everywhere else, forcing a clean
rebuild of a matching object remains `destroy` then `apply`.
Destructive authority flows through positive, fail-safe tokens — e.g. the
storage role wipes only clusters named in
`bootwright_ceph_rebuild_authorized_clusters`, so an absent or stale value can
only under-authorize. Relaxations are narrow, explicit,
and named one risk at a time: on destroy, `--authorize unowned-vms` lifts the
per-VM marker refusals and `--authorize unowned-networks` lifts the refusals on
the cluster's libvirt network, its KubeVirt DataVolumes, and their PersistentVolumeClaims (a wider blast radius,
so a separate word), and nothing relaxes device data-safety checks. A lost Ceph cluster marker is
recovered only through an operator-supplied `<StorageCluster>=<fsid>`
confirmation, because an ownership attestation naming an exact identity is
evidence an operator can supply and an inference cannot: Bootwright
reconstructs the missing record only when the supplied fsid, any surviving
controller record, and any reachable live fsid all agree, and never overwrites
contradictory evidence. The full token vocabulary, and the gate each
token does and does not relax, is specified in
[`state-model.md`](../state-model.md) ("CLI Contract").

The markerless state left by an interrupted first Ceph bootstrap has its own
positive allowlist; it never borrows the structural-drift list. The controller
may name only a selected managed `StorageCluster` whose exact owner record still
names this context and the selected desired seed, and only when rebuild intent
and the `data-loss` acknowledgment were both consumed. The seed independently
re-validates that record plus config-present, marker-absent, and unreachable
host evidence. Refusal, ownership classification, and cleanup share the one
result, so missing or contradictory evidence cannot fall through to bootstrap
and stamp a new marker over an unknown cluster.

### Destroy is the only remover, and it fails closed

`apply` is additive: deletion is never a plannable apply action, and the
rename signature (a new cluster provisioned while a different provisioned
cluster is undeclared) fails closed rather than silently re-provisioning and
orphaning — for both cluster kinds, reading install records for a
`ContainerCluster` and Bootwright-owned `storage-cluster` ownership records for
a `StorageCluster`. `Environment.spec.safety.destroyProtection` and
`protectedKinds` are one shared source of truth gating both `destroy` and
destructive `apply --mode rebuild` rebuilds, enforced entirely in Go — no
Ansible destroy role consumes an authorization extra-var. The single deliberate
exception is `--reclaim-devices`, where `--authorize data-loss` authorizes the
protected wipe in place: reclaim recovers named devices of a cluster that stays
up, so routing it through `destroy` would require destroying strictly more to
recover less. Independent of protection, destructive
rebuilds require `--authorize data-loss` (or the interactive data-loss
confirmation); `--yes` never authorizes destruction, on either verb. Remedies must route to the stage that actually clears the block
(machine substrate → `destroy --stage infra`), or the operator loops forever.
Teardown is not finished until its records are: a destroy that cannot remove a
convergence/install record or write a substrate release exits non-zero, because
a surviving record silently converts the next apply's re-provisioning into a
skip.

### Scope: render against the render set, mutate only the work set

A `--clusters` scope keeps the plan state render-inclusive (attachments still
render against referenced clusters) but every mutating and gating surface —
provisioning tasks, preflight secrets and host trust, `diff --recorded`
objects, destroy teardown — keys on the work set of directly named roots (nil =
no narrowing; non-nil empty = none). Scope gate variables are composed in
exactly one place per verb so the task-graph, single-playbook, and dry-run
paths carry identical gates. Scoped destroys restrict recorded-resource cleanup
to selected roots; scoped applies refuse to re-render shared machine services
whose config derives from the full fleet (degrading), while self-contained
services re-provision identically and pass — an unclassified service defaults
to degrading. That same classification applies across contexts: a degrading
service is identified by exact Kind+Name+Host, and a sibling owner/reference or
unreadable identity evidence refuses apply with no authorization bypass.
Reference-only destroy releases its role-qualified evidence and never tears the
base service down.

The `--machines` apply/destroy selection axis follows the same
render-inclusive / work-set-gated model at machine granularity. The render
still produces the full `RenderState` (a machine renders in the context of its
whole cluster), and the work set narrows through a per-machine `ApplyTarget`
gate rather than a per-cluster one: the target carries the named machines and
their hosts, and every task consults it before provisioning or tearing down a
given machine. `--machines` runs only the fabric and machines phases and is
mutually exclusive with `--clusters`. A machine that is a node of a cluster or
carries a shared service resolves to real substrate work; a standalone
managed-OS teardown fails closed, because Bootwright installs a managed OS only
on cluster-member machines and refuses to invent per-machine teardown for a
machine with no provisioning work.

No authorization token widens a selected root set. In particular, a KubeVirt host
cluster cannot be destroyed while an installed nested cluster is left outside
`--clusters`; the child must be selected in the same full-lifecycle destroy or
destroyed first. The apply side of that dependency — a scoped run that would
rebuild the host's machine substrate — gates on the same resolved work set
rather than on one flag name, so `--machines` and `--clusters` selections are
held to the identical rule and neither can strand a nested cluster.

### One mutating run at a time

An `O_EXCL` run lease with process-identity and heartbeat liveness admits one
mutating run per context. Apply and destroy acquire it before reading the input
they will classify and hold it through every local and remote mutation and
post-run evidence update. `context update`, `diff --adopt`, and
`storage-cluster replace-arbiter` acquire the same lease before changing desired
input; the arbiter verb passes the already-held lease into both embedded
workflows. Losing the lease (takeover after a stall, or a failed heartbeat save)
stops the run rather than risking a double mutator.

Lease acquire, owner-checked heartbeat, and owner-checked removal are serialized
as one cross-process transaction so a prior holder cannot overwrite or delete a
replacement. Automatic stale takeover is limited to a lease identifying the
current controller and positive proof that its recorded PID stopped or was
reused with a different available process-start identity. A stale heartbeat
with a live PID and missing or unreadable identity fails closed. Another or
unknown controller requires operator inspection and manual repair or removal
only after proving its run is no longer active.

Apply/destroy work that can change a machine-hosted shared service also acquires
a controller-global shared-service lease after resolving its exact consequence.
It holds both leases through the decisive sibling-store scan, host mutation, and
evidence cleanup. The global layer closes the simultaneous-first-apply race
between different contexts; the host-side context claim and container label
close the partial-first-apply case after the lease is released. Nested runners
bind cancellation from both leases instead of replacing the caller context.
Self-contained service rendering does not waive this cross-context boundary:
provider-global emulated-BMC units and pools use the same lease, durable claim,
and live exact-identity refusal. The BMC claim is a full immutable service
composite published before the first service-specific mutation: it binds the
context and provider host, libvirt URI and pool, ports, units, paths,
configuration identity, and firewall consequence. Recovery after interruption
uses that persisted composite rather than current desired values. Teardown
requires conclusive loaded-unit, pool, path, and mount evidence and retains the
claim and controller record through systemd reload and absence verification.

The controller-global lease is not a distributed lock. Each command that can
mutate a shared host service also derives a unique attempt identity from its
held command lease and acquires one atomic host-global operation guard before
the first selected shared-host mutation. The guard binds the lease plus the
controller-rendered, digest-bearing full host selection and stays held across
all selected provider and infra-component plays. Every mutation boundary
revalidates it; only a command-wide finalizer removes it after the last target
or controller evidence mutation. A crash leaves an inspectable guard that no
other controller, mode, or authorization may adopt or expire automatically.

Before either service family publishes its first durable intent, it scans the
other family's full claims and transitions plus the atomic endpoint registry.
Durable claims are keyed and conflict-checked by physical consequence,
including singleton daemons, global paths, pools, and protocol/port endpoints,
rather than only by authored provider or component names. Each family publishes
its reconstructible full claim/transition before its endpoint transition
publishes one atomic active-plus-pending registry and sorted per-slot claims;
old slots remain through runtime cleanup. Canonical operation, endpoint, claim,
and transition documents use one exact-content CAS protocol with a
persistent shared lock, symlink-safe directory walk, and no-replace absent
publication. Existing-file CAS serializes cooperating Bootwright controllers;
arbitrary root writers are outside that lock contract and are handled by exact
re-read and fail-closed mutation gates. These layers close concurrent first
acquisition, crash-recovery, cross-family, and sequential same-host collisions.

### Teardown makes maximal progress behind dependency-aware gates

The ordering rule in this section is superseded by
[ADR 0023](0023-teardown-is-the-inverse-of-buildup.md), which derives the
destroy order by inverting the apply graph, fans the per-cluster steps out, and
names three fail-closed edges rather than one. The gating philosophy below is
unchanged and still governs.

The destroy task graph sequences independent stages with ordering dependencies,
so a failed stage does not block later independent cleanup. Only a few edges are
hard dependencies, and the reason is always the same: proceeding would erase the
evidence or the access a retry needs — deleting cluster kubeconfigs or install
records while an owned VM remains, or deleting a KubeVirt host's substrate while
its guests survive. An unreachable KubeVirt host, or a missing captured
kubeconfig, fails closed for every selected guest even when its controller
ownership record is missing and even under `--authorize unreachable-nodes`;
neither condition is evidence that the guest is absent. The decomposed task playbooks are constrained to
split-equals-monolith: same `--limit`, same extra-vars, own `hosts:` selector.
An authorized unreachable host is a recorded partial outcome, never a successful
dependency. Storage and non-storage branches alike retain convergence, install,
ownership, access, and substrate-release evidence for the exact retry and return
non-zero after unrelated work has drained.

## Consequences

- New kinds and services fail safe by default: destructive unless allowlisted
  as reconfigure-only, degrading unless classified self-contained, dropped
  from no inventory group without failing the kind-registration fitness test.
- Upgrades cannot silently convert a rebuild into a no-op: an absent
  structural hash on either side of a comparison classifies as structural.
- Refusals are guidance-first: they name the object, the kind-specific
  worst-case consequence (disk wipe, OSD data loss), and the exact remedy.
- The costs are hash stability (record payload shapes are effectively frozen),
  hand-synced Go/Ansible literals for ownership stamps and roles, and a class
  of scope/hash traps documented in `.agents/knowledge/` (see
  `converge-hash-drift-model.md`, `scoped-runs-render-vs-work-set.md`,
  `destroy-scoping-and-sweeps.md`, `run-lease-mutual-exclusion.md`).

# ADR 0007: Apply/Destroy Safety Model

## Status

Accepted

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
never disagree. Classification compares desired against *recorded* desired only;
out-of-band live divergence is deliberately invisible here and belongs to the
per-role Ansible reconcile gates and `diff` (live). Records are written only on
task success; destroy removes them (including storage sub-object records) so
torn-down objects reclassify as missing, and a partial teardown
(`--skip-unreachable`) keeps them so `apply --expect-new` fails closed atop
residual state.

Destruction also leaves a positive re-authorization trail. On destroy,
Bootwright writes a per-cluster substrate release that records that the
cluster's substrate was deliberately torn down and so authorizes the subsequent
`apply` to reinstall it. The release is machine-granular: `destroy --machines`
records (or merges) the released machine names into the cluster's record, a
cluster-scoped destroy releases the whole cluster, and an apply consumes the
release only for the machines it actually covered — a scoped apply shrinks the
record to the still-released remainder. `--skip-unreachable` withholds the
release for a partially destroyed storage cluster, and for infra-only,
machine-scoped, or non-storage teardown where no equivalent per-node completion
proof exists. A managed storage cluster whose completion report proves that no
topology node was skipped still receives its release, so merely enabling the
flag does not strand a fully destroyed cluster. Skipped nodes keep failing
closed until a full destroy finishes.
The release is a fail-safe token like the
others here — its absence can only withhold, never manufacture, authority to
reinstall — closing the window where a re-run after a destroy would otherwise
have to guess whether a missing substrate is greenfield intent or an
interrupted teardown. Because a bare-metal destroy defers the disk wipe to the
reinstall, a release-authorized apply covering bare-metal managed-OS machines
is the moment data is lost and still crosses the data-loss acknowledgment.

### One explicit mode variable, enforced on both sides

The safety mode is a single extra-var, `bootwright_apply_mode`
(`create` | `continue` | `override`), stamped by plan composition (it replaced
a legacy boolean `bootwright_install_override`). The Go object preflight
enforces it against recorded state (default reconcile refuses drift/foreign,
`--expect-new` refuses pre-existing, `--converge-drifted` refuses only foreign); the
per-role Ansible apply-mode gates enforce the identical contract against live
existence and ownership. Defense in depth is intentional: Go decides from
records, roles from the host.

### Ownership is the authorization boundary for destruction

No destructive action proceeds without proof that Bootwright created its
target: ownership records on the controller, substrate markers (libvirt domain
XML, vSphere annotations, KubeVirt labels, managed-OS install markers), and
live container provenance labels for shared bastion services. Foreign fails
closed in every mode; `--converge-drifted` rebuilds owned drift but never adopts.
Unprovable is not absent: a live host that answers SSH but rejects every probe
identity (or presents a changed host key) cannot prove ownership either way,
so the managed-OS install fails closed on it exactly as on a foreign host —
only the machine's substrate release, written by a destroy, authorizes
reclaiming it.
Destructive authority flows through positive, fail-safe tokens — e.g. the
storage role wipes only clusters named in
`bootwright_ceph_rebuild_authorized_clusters`, so an absent or stale value can
only under-authorize. Relaxations are narrow and explicit: `--include-unowned`
lifts only per-VM marker refusals on destroy; nothing relaxes device
data-safety checks. A lost Ceph cluster marker is recovered only through an
operator-supplied `<StorageCluster>=<fsid>` confirmation. It is an explicit
ownership attestation for that exact identity: the supplied fsid must match the
declared seed's on-disk Ceph configuration, and any existing controller record
must agree with that cluster and seed. A reachable live fsid must also agree,
as a contradiction check rather than an authorization source. Only then may
Bootwright reconstruct a missing controller record and host marker before the
normal ownership gate runs again; contradictory evidence is never overwritten.

### Destroy is the only remover, and it fails closed

`apply` is additive: deletion is never a plannable apply action, and the
rename signature (a new cluster provisioned while a different provisioned
cluster is undeclared) fails closed rather than silently re-provisioning and
orphaning. `Environment.spec.safety.destroyProtection` and `protectedKinds`
are one shared source of truth gating both `destroy` and destructive
`apply --converge-drifted` rebuilds, enforced entirely in Go — no Ansible destroy role
consumes an override extra-var. Independent of protection, destructive
rebuilds require a data-loss acknowledgment; `--yes` never authorizes
destruction. Remedies must route to the stage that actually clears the block
(machine substrate → `destroy --stage infra`), or the operator loops forever.

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
to degrading.

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

### One mutating run at a time

An `O_EXCL` run lease with process-identity and heartbeat liveness admits one
mutating run per context; destroy acquires it explicitly because it mutates
outside the apply scheduler. Losing the lease (takeover after a stall, or a
failed heartbeat save) stops the run rather than risking a double mutator.

### Teardown makes maximal progress behind per-step gates

The destroy task graph sequences stages with ordering dependencies, not hard
ones: a failed stage never blocks later independent stages, because safety
lives in each step's own ownership and data gates, not in chain order. The
decomposed task playbooks are constrained to split-equals-monolith: same
`--limit`, same extra-vars, own `hosts:` selector.

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

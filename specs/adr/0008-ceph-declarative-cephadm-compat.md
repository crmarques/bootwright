# ADR 0008: Declarative Ceph API on cephadm Native Concepts

## Status

Accepted

## Context

Bootwright provisions managed Ceph clusters (`StorageCluster` and its
sub-object kinds) on nodes it may also have installed. Ceph already has a
mature operational surface: cephadm service specs applied with
`ceph orch apply -i`, drivegroup-shaped OSD selections resolved by ceph-volume,
and imperative `ceph`/`radosgw-admin` commands for pools, CRUSH rules,
filesystems, gateways, and configuration. Operators know that surface, vendor
documentation (IBM Storage Ceph, Red Hat Ceph Storage) is written against it,
and any invented abstraction would have to be translated back to it for
debugging.

At the same time, several Ceph objects are immutable once created (a pool's
data-protection type, an erasure-code profile, a CephFS's metadata pool), OSD
creation is asynchronous and consumes raw block devices, and `ceph orch apply`
gives no reliable changed/ok signal. A naive "make it look like the YAML"
reconciler would either silently destroy data or silently under-converge.

## Decision

The Ceph desired-state API deliberately mirrors cephadm and native Ceph CLI
concepts rather than inventing its own vocabulary:

- Field *semantics and value vocabularies* match the native surface
  one-for-one: drivegroup device selections, service-spec keys that stay
  top-level siblings of `spec`/`placement`, erasure-code-profile knobs by their
  `erasure-code-profile set` spellings, and pool intents as their exact
  `ceph osd pool set` / `set-quota` verbs. Field *spellings* follow the
  camelCase API grammar (ADR 0014) and the renderer lowers them to the native
  snake_case: the author writes `filterLogic`, `blockDBSize`,
  `extraContainerArgs`, `customConfigs`, and a real `rotational: true` that
  renders as cephadm's `rotational: 1`. Passthrough `spec.ceph.services[]`
  render verbatim for service types not modeled first-class.
- Distribution differences (oss/redhat/ibm) are data in one renderer table,
  not control flow; the Ansible role dispatches on rendered capability flags
  (ADR 0002).

Convergence is declarative-first and additive-only:

- Bootstrap happens once (gated on `/etc/ceph/ceph.conf`); everything else is
  `ceph orch apply -i` service specs plus a rendered, ordered operation list.
  Create-style operations carry an idempotency probe (skip-if-exists);
  steady-state intents are in-place `ceph ... set` operations that are
  last-write-wins. Apply never prunes an undeclared live object — removals are
  deliberate operator actions, and `diff` reports real-only objects as adopt
  candidates instead of drift.
- `diff` (live is its default mode) derives the desired side with the same
  topology resolver the renderer uses and compares it per facet against
  `ceph ... --format json`
  reads; `diff --adopt` folds live reality back into authored YAML, but only
  the cleanly representable facets, purely additively, and through the
  history-snapshotting input-mutation path. Everything else is reported as
  detected-but-not-adopted.
- Native-CLI parity is generated, not documented: each managed cluster's
  render emits an `apply.sh`/`lib.sh` bundle built from the SAME operation
  document the apply role runs, so the script and Bootwright-applied state
  cannot drift. Guarding is driven purely by operation metadata (an
  idempotency probe means guarded create; none means in-place reconcile), and
  a test asserts every rendered operation is represented.

Destructive operations are gated, never implicit:

- Create operations carry the sub-object's immutable identity in a
  `structural` block. A data-destroying rebuild runs only under
  `apply --mode rebuild`, only on a proven structural mismatch against the live
  object, only for a cluster passing the 3-factor ownership gate, and — for
  the cluster-level zap — only when the controller's positive
  rebuild-authorization token names the cluster. All decisions are fail-safe:
  an unreadable live value, an absent marker, or an empty token never
  authorizes destruction.
- Device consumption is explicit opt-in (`devices` shorthand or an authored
  drivegroup — never an implicit all-devices default), backed by per-node OSD
  ownership markers that both the install empty-device gate and the destroy
  wipe honor.
- The `--mode rebuild` rebuild path is deliberately NOT generated into the
  native-CLI bundle; it is Bootwright-only.

Removal of an individual configuration key or mgr module stays on the native
operator surface. The operator first runs
`cephadm shell -- ceph config rm <who> <option>` or
`cephadm shell -- ceph mgr module disable <module>`, verifies the live removal,
and then deletes the declaration. Desired-state omission is not consent, and a
granular key/module remover does not belong inside whole-object `destroy`.
Adding such a Bootwright operation in the future requires its own explicit
target, preview, authorization classification, and apply/destroy safety-matrix
case; it cannot be inferred from an absent map or list entry.

## Consequences

- Operators and vendor docs translate directly: every rendered object is the
  exact native command or cephadm spec, in apply order, and the generated
  bundle reproduces a cluster without Bootwright.
- Additive-only convergence means removals (an OSD device, an NFS export, a
  pool) require explicit operator action or drains; `diff` narrates the gap
  instead of apply closing it destructively.
- Structural-hash discipline must be maintained: scale-out, role rebalance,
  and stateless-service edits must stay reconcilable-in-place (tests pin
  this), or `--mode rebuild` would wipe OSDs for a benign edit.
- New Ceph features cost a renderer table/spec change plus validation, not new
  imperative role branches; a knob Bootwright does not model is still
  reachable via config options or passthrough services.
- The fail-safe gating means a stale rendered bundle can under-converge or
  skip a rebuild, but never over-destroy; recovery paths (re-apply, `continue`
  mode, `--reclaim-devices`) are designed around re-running apply rather than
  manual state surgery.

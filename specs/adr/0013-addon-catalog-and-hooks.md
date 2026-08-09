# ADR 0013: Add-on Catalog, Step Lifecycle, and OLM Readiness Gating

## Status

Accepted

## Context

Post-install cluster components were drifting into compiled Go: the Data
Foundation integration shipped a bespoke credential producer and consumer
inside Bootwright, its Ceph attachment rendered from storage-cluster
operations, and every new operator integration threatened another compiled
special case. At the same time, OLM applies raced their own dependencies —
custom resources applied before the operator's CRDs existed failed with
`no matches for kind`, and Subscriptions resolved against shipped catalogs
whose registries had not started. Ready-made add-ons also had no distribution
channel: operators had to author the add-on directory by hand for common
components, and `StorageExport` carried multiple arms for producing
external-cluster details.

## Decision

Add-on integrations ship as content, not code, and the apply engine gates on
real readiness signals.

**Embedded catalog and machine-local store.** Bootwright embeds a native
add-on catalog (currently `openshift-data-foundation` and
`fusion-data-foundation`) managed by `bootwright add-ons list|add|delete`.
Registered add-ons live in the Bootwright root's `add-ons` directory — a
dir-listing store with one registered version per name, a `.bootwright-addon`
provenance marker per entry, and whole-directory rewrite on install. A
binding `addonRef` that no authored `ClusterAddon` matches falls back to the
store; `context init`/`update` snapshot each referenced store add-on into the
context input tree under `add-ons/_store/<name>`, so contexts stay
self-contained across store deletes and upgrades. An authored add-on with the
same name always wins.

**OLM readiness gating.** A shipped `olm.catalogSource` is applied before the
operator-install set and gated on
`status.connectionState.lastObservedState == READY`, so OLM resolution never
races the catalog registry startup; the add-on must subscribe to the catalog
it ships. The operator-install set (Namespace, OperatorGroup, Subscription)
then applies, and the engine waits for the operator's CSV to reach
`Succeeded` before applying `spec.olm.customResources` or running any
`follows: operatorReady` step. Gate timeouts are typed (`catalogGateError`,
`csvGateError`) and never recorded as failed applies of the already-applied
resources.

That CSV gate establishes only the CRDs the subscribed operator itself owns. A
meta-operator whose CSV succeeds and *then* creates Subscriptions for the
operators that own the interesting kinds — Data Foundation's `odf-operator`
creating `ocs-operator`, which owns `storageclusters.ocs.openshift.io` — leaves
the anchor satisfied while the kind is still unresolvable. The anchor is a
lifecycle position, not a proof of API availability, and no amount of gating on
the add-on's own Subscription can close that gap. A step therefore declares the
API its content depends on in `spec.steps[].requires[]`, which the engine polls
before running the step; see `specs/state-model.md`.

**Step lifecycle.** `ClusterAddon spec.steps` (named `spec.hooks`, with
`preApply`/`postOperatorReady`/`postReady` lifecycle names, when this decision
was taken) wire add-on-shipped playbooks and templated manifests into three
lifecycle points, named by the anchor field the step sets: `gates: apply`,
`follows: operatorReady`, and `follows: ready`.
Steps default to `run: onChange`, keyed on a
digest of shipped content plus resolved inputs and target; `run: always`
exists for integrations that must converge every apply (the Data Foundation
exporter step, so rotated Ceph mon endpoints/keys keep landing). The desired
hash is input-aware: binding-supplied input values and the per-step shipped
content digest both fold into `render.DesiredHash`, so editing an input or a
shipped playbook re-applies an otherwise-ready add-on. Input effects (global
pull-secret merge) run before any resource applies.

The scheduler serializes a playbook step against each `StorageCluster` its
target resolves, not the whole add-on task. Resolution is fail-closed and the
exclusive `storage:<name>` resource spans the playbook, captured outputs,
consumer manifests, output cleanup, and the ready record. The read-only
requirement wait happens before it; operator installation and the potentially
45-minute readiness wait remain outside it. This closes the shared-cephx
delete/remint race between Data Foundation consumers without adding hours of
task-wide serialization, while unrelated storage clusters and manifest-only
steps still run concurrently.

**externalDetails is fromSecretRef-only.** `StorageExport
externalDetails.fromSecretRef` is the single operator-supplied arm for
external-cluster details (the `generated` and `sshExecution` arms are
retired). A managed-Ceph export omits `externalDetails` entirely — the
consuming add-on produces the payload itself by running the exporter on a
Ceph node via a `follows: operatorReady` step and consuming it as a captured
output. Normalize must not default the field. The compiled Data Foundation
producer and consumer are deleted; cross-cluster ordering is expressed as
plan-time task edges (`storage.<ceph>`, `wait.<ocp>`) derived from the step's
input ref-chain, not a compiled attachment task.

## Consequences

- Operator integrations are updatable by re-registering catalog content;
  fixing the Data Foundation exporter flow no longer requires a Bootwright
  release, only new add-on content.
- No compiled operation family may render Data Foundation work from storage
  operations; tests pin the absence of the deleted producer/consumer paths.
- Gate failures are diagnosable as what they are: a catalog that never went
  READY or a CSV that never succeeded, without mislabeling the already-applied
  CatalogSource/Subscription as failed apply targets.
- Concurrent add-on tasks may reach their steps together, but mutating
  playbooks with the same resolved storage target enter that target one at a
  time; an unknown target or missing coordinator refuses before mutation.
- Add-ons without readiness checks re-apply on every run (idempotently), and
  `run: always` steps or a pull-secret merge effect disable the already-ready
  skip — converge-on-every-apply is the deliberate cost of correctness.
- Rootless `validate` cannot read the root-owned store and reports an
  unresolved `addonRef` with a register remedy; `context init` (root)
  resolves and snapshots instead.

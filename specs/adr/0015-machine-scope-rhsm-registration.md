# ADR 0015: Machine-Scope RHSM Registration and External Management

## Status

Accepted

## Context

RHSM registration originally lived inside the Ceph role
(`storage_cluster_cephadm/tasks/providers/subscription.yml`), executing during
the deps-phase `storageinfra.<cluster>` task. Registration is machine-OS work,
not storage work: it belongs after the OS install and before any package
consumer, and coupling it to Ceph made it impossible for an operator to keep
registration under corporate automation (Satellite workflows, site playbooks)
while still letting Bootwright install Ceph and provision clusters.

## Decision

RHSM registration is a machines-phase concern with a who-runs-it axis.

### Machines-phase registration task

A `registration.<cluster>` apply task (kind `machineRegistration`, playbook
`bootwright.core.task_machine_registration_apply`, role
`machine_registration_rhsm`) runs over the storage-node inventory group in the
machines phase, after `osinstall.<cluster>` and as an explicit dependency of
`storageinfra.<cluster>`. It carries the registration half of the former Ceph
subscription file: Satellite CA trust and katello binding, corporate proxy CA
trust, the node-side Satellite-in-CIDR bypass decision, `rhsm.conf` `[server]`
proxy and `[rhsm]` `repo_ca_cert` convergence (both directions),
`subscription-manager` registration and refresh, and optional Insights
enrollment. The Ceph role keeps the distribution tail — subscription-backed
repository enablement, the IBM vendor repo, and license installation — because
those are Ceph-distribution concerns dispatched on rendered capability flags
(ADR 0002). The task is planned only for subscription-backed distributions
with managed RHSM, anchors provisioning playbooks as a machines-phase task
(ADR 0005), maps to the machine-infra destroy kind for record resets, and is
reconfigure-only under `--mode rebuild` (ADR 0007 taxonomy).

### The `management` axis

`Entitlement.spec.rhsm.management` is `managed` (default, unset spelling) or
`external`, the reserved who-runs-it spelling from ADR 0014. Under `external`:

- No registration task is planned; Bootwright never registers nodes, never
  edits `rhsm.conf`, and skips the repo-enablement purge so operator-enabled
  repo sets survive.
- The arm must carry only `management`: `organizationRef`, `activationKeyRef`,
  `satellite`, and `connectToInsights` are validation errors, so no RHSM
  secrets are demanded (preflight included).
- The operator supplies registration as a `CustomPlaybook` with
  `gates: deps` — it runs after the machines phase (OS in place) and, because
  `gates` may not be combined with `onFailure: continue`, hard-gates the
  deps-phase Ceph work on it. A `follows: machines` anchor runs at the same
  point in time but creates no forward edge into later phases (ADR 0005), so
  it must not carry delegated registration.
- A `packageSource.fromSubscription` install profile must reference a `managed`
  entitlement: Anaconda-time registration is the package source and cannot be
  delegated; mirror/hostedTree are the delegation-compatible sources.

For `ibm-storage-ceph`, the RHEL subscription (and its `management` axis) is
named by the storage nodes' `MachineInstallProfile.spec.subscription` or the
cluster's `StorageCluster.spec.ceph.osSubscriptionRef`; the earlier
`rhelEntitlementRef` indirection on the entitlement has been removed.

### Honest-failure contract for delegation

A delegated registration is opaque (ADR 0005): Bootwright cannot observe
whether the corporate playbook actually registered nodes or enabled the right
repos, and does not hard-probe `subscription-manager status` (an internal
mirror needs no RHSM). The fail-closed gate stays where package availability
is actually consumed: the cephadm install assert names delegated registration
among the remedies. Ceph client commands execute through `cephadm shell`, so
the host does not need a separately installed `ceph-common` package.

### Lifecycle

Registration happens once, at install. Destroy is its inverse: a
`machineRegistration` teardown step deregisters the nodes Bootwright registered
before their substrate is deleted (ordered by ADR 0023 A2), because a reinstall
reuses the host DMI UUID and would otherwise collide with the surviving
Candlepin consumer. It covers only nodes of a
`managed` storage cluster that Bootwright installed: an `os.provided: true`
node, an `external` cluster, and an unreachable node are each skipped. Storage
destroy keeps removing the fixed CA anchor filenames it always removed. Under
`external` Bootwright never writes the Satellite CA anchor, so that removal is
a no-op there; the proxy CA anchor is still written by the deps-phase storage
role whenever a proxy trust bundle is declared, independent of
`rhsm.management`.

### Deliberately not done

- A registration phase for machines outside storage clusters: today only the
  Ceph flow consumes post-install registration; container-cluster nodes run
  RHCOS. The role is machine-scoped so a future consumer reuses it.
- A `rhsm` `proxyFor` slot (ADR 0012's consumer set is closed); day-2 RHSM
  proxy still renders from the resolved effective proxy.
- A subscription-status preflight for delegated registration.

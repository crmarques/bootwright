---
title: Architecture
description: The Bootwright execution pipeline, convergence classifier, and rendering contract.
---

# Architecture

This page is the execution and pipeline deep dive behind the user-facing model
in [Concepts](../concepts.md). It covers the render pipeline, execution
identities, resource locks, the convergence classifier, the apply modes, and the
contributor-facing contracts that hold the system together. It does **not**
restate the user-facing stage, platform, or selection model — see the cross-links
below for those.

For the user-facing mental model first, read these once and do not expect them
repeated here:

- The kinds, ownership rule, and references —
  [Concepts → Object Ownership](../concepts.md#object-ownership).
- The two stage families, sub-phases, and the `--clusters` single-namespace rule
  — [Concepts → Apply Stages](../concepts.md#apply-stages).
- Platform render mode versus substrate type —
  [Concepts → Providers, Machines, And Platform Mode](../concepts.md#providers-machines-and-platform-mode).
- The four classification outcomes at a glance —
  [Concepts → Convergence And Drift](../concepts.md#convergence-and-drift).

## The pipeline

Bootwright is a desired-state loader, validator, renderer, and idempotent apply
pipeline. Validated desired state flows through layered phases:

```text
YAML desired state
  -> load and strict decode
  -> normalize defaults
  -> validate ownership and references
  -> render effective state and tool inputs
  -> apply substrate, machine OS, storage, cluster, and add-on phases
```

`status`, `state-check`, `render`, `plan`, and `validate` are read-only verbs,
not pipeline stages: they observe the same model without mutating it. See
[Operations and Recovery](../advanced/operations.md) for the operator-facing
verb model.

### Normalize before render

A default consumed by more than one stage is materialized by the normalize
phase, and both validators and renderers read the normalized value rather than
recomputing it. Examples: an omitted endpoint `source.type` becomes `openshift`;
the derived platform render mode; an `api-int` copy of `api`; the default cluster
and service networks; `distribution: openshift`; a defaulted `attachmentRef`. Run
`render effective` to see these materialized values. A diagnostic on any
normalize-injected reference the author never wrote — Environment-defaults
copies, the `openshift-pull-secret` convention, `<cluster-name>-cluster-admin-ssh-key`
— states that the value was defaulted and how to override it.

## Execution identities

Bootwright does not treat the process as simply root or non-root. It uses
distinct execution identities so that ownership and privilege stay explicit:

- User-authored files, external secret sources, `~` expansion, and caller `PATH`
  discovery run as the original local caller.
- Context state, generated secrets, runtime installer files, workflow logs,
  managed Ansible runtime, and local package or CLI installs run as the
  protected local root state identity under `/var/lib/bootwright`. Commands that
  need context data re-exec through `sudo` when necessary.
- Provider, `InfraComponent`, and infrastructure host work connects as the
  rendered SSH user, then uses Ansible `become` for privileged host mutation.
- Controller-local Ansible work uses localhost inventory and `become` only for
  the tasks that intentionally mutate controller state.

The desired-state ownership boundary holds throughout: physical machine facts do
not move into cluster intent, and cluster release intent does not move into
environment defaults. That keeps provider swaps and release changes explicit. See
[Concepts → Object Ownership](../concepts.md#object-ownership) for the full
ownership table.

## Orchestration, executor, and resource locks

Bootwright is the cross-cluster DAG orchestrator; Ansible (the `bootwright.core`
collection) remains the executor for machine-level work. Bootwright owns the
dependency graph and runs independent tasks concurrently where locks allow it.
Ansible, `oc`, `openshift-install`, `cephadm`, SSH, and SCP do the per-host work,
and their process output stays in root-managed run, task, and cluster logs
instead of streaming through the terminal.

Provider, `InfraComponent`, machine-infrastructure, and storage playbooks use
Ansible-native parallelism. Bootwright enforces resource locks **before**
launching concurrent playbooks:

- one mutating task per provider or service machine (until roles are classified
  more finely);
- one task per Redfish system or BMC target;
- `kubevirt:<host-cluster-or-kubeconfig>:<namespace>` for nested child-VM
  infrastructure and boot tasks.

### Cluster install scheduling

The OpenShift agent install is scheduled as dependency stages: prepare provider
and machine infrastructure, create the cluster agent ISO with
`openshift-install`, boot each declared node through its rendered boot adapter as
parallel node tasks, then run `openshift-install agent wait-for install-complete`
after **every** node boot task has completed. Post-install add-on apply is
scheduled after that install wait, and storage attachment tasks in the same
add-ons phase wait for the selected Data Foundation add-on's readiness before
applying generated external-mode manifests.

Storage apply is a **peer phase**, not a sub-step of cluster install. The
`machine-infra` stage prepares selected machines when needed; the `clusters`
stage schedules an Ansible storage task against a synthetic seed inventory entry,
launches `cephadm bootstrap` on the seed node, applies cephadm service specs,
runs topology and storage operations, and writes Data Foundation attachment
records. Imported storage clusters skip this storage task entirely.

!!! note "Internal stages versus the `--stage` flag"
    The internal dependency stages above (for example `machine-infra`,
    install-wait, add-on apply) are not the same vocabulary as the CLI
    `--stage infra|clusters` families and their `fabric`/`machines`/`deps`/`base`/
    `addons` sub-phases. The CLI model is owned by
    [Concepts → Apply Stages](../concepts.md#apply-stages).

### KubeVirt parent/child edge behavior

For a KubeVirt child `ContainerCluster` that references a Bootwright-managed
virtualization cluster through `hostClusterRef`, only the full graph and explicit
parent+child `--clusters` selections add graph edges from the child work to both
the parent install wait and the parent add-on wait task that provides
`kubevirt`.

!!! warning "A scoped child apply does not pull in the parent"
    A scoped child apply does **not** expand its scope to install the parent. It
    fails before any mutation unless the parent is selected too, or local runtime
    records prove the parent install and the KubeVirt add-on are already ready.
    This is the first supported nested topology, and the dependency is explicit
    by design.

## Convergence and the four-outcome classifier

Convergence is resumable by default. Each mutating workflow task runs under its
resource lock, derives a non-secret desired hash and a Bootwright owner identity,
and writes a durable **convergence-safety record**. The records classifier
compares that recorded evidence against current desired state and yields four
outcomes:

| Outcome | Meaning |
| --- | --- |
| `missing` | No record for the resource. |
| `match` | The recorded desired hash equals the current desired hash. |
| `drift` | The recorded desired hash differs from the current one. |
| `foreign` | The record carries a non-Bootwright owner. |

This is exactly what `state-check` reports against recorded evidence — never live
hosts. See [Concepts → Convergence And Drift](../concepts.md#convergence-and-drift)
for the user-facing summary.

!!! warning "Classification is NOT an apply-time skip gate"
    The four-outcome classification is what `state-check` reports; it is **not**
    itself an apply-time skip gate. Most provider-service and infra-component
    config tasks have no reliable external probe, so they **re-run and rely on
    idempotent execution**, and their record is marked `unknown` (recorded but
    not classified) as durable evidence rather than a skip decision.

    Apply-time fail-closed gating lives **only at the concrete-probe sites**:
    cluster install records, add-on records, managed OS markers, provider
    metadata, and storage comparison results. Cluster install reconcile reads
    per-cluster install records, probes live cluster availability, skips
    completed installs, resumes only from known-safe phases, and fails closed
    when install state exists for missing or different inputs after node boot
    unless a command-scoped `--override` is given.

### Run ledger and lease

Apply records a durable run ledger under the context state directory plus a
short-lived local lease for the process updating it. Cluster install tasks
additionally record per-cluster install state with a non-secret desired-input
fingerprint so repeated applies can skip completed installs and resume only from
known-safe phases. `bootwright status` reads that ledger without contacting
provider hosts, BMCs, or clusters; `bootwright status --watch` follows it until
the run reaches a terminal state.

## The three apply modes

`apply` selects how strictly Bootwright treats pre-existing state. The user-facing
overview is in [Concepts → Convergence And Drift](../concepts.md#convergence-and-drift);
the behavioral contract is:

- **bare `apply` = reconcile (default):** creates missing objects, skips objects
  whose recorded desired state matches current, and fails closed on `drift` or
  `foreign` ownership before any mutation.
- **`apply --expect-new`:** additionally refuses to proceed when any selected
  object already exists — a greenfield assertion.
- **`apply --override`:** command-scoped break-glass. It may continue past
  Bootwright-owned unsafe-mismatch checks that have an explicit override path:
  bypass the skip-if-already-complete install check, reinstall a managed-OS
  machine (substrate VM undefined, disks wiped, then rebuilt), and cleanly
  rebuild a managed Ceph cluster via `cephadm rm-cluster --zap-osds` — allowed
  **only** when a Bootwright ownership marker proves the live cluster is the one
  Bootwright created (a foreign or co-resident cluster fails closed). It must
  **not** bypass active-run leases, validation, secret checks, or
  foreign-resource ownership failures.

`--expect-new` and `--override` are mutually exclusive.

!!! warning "`destroyProtection` versus `apply --override`"
    When selected state sets
    `Environment.spec.safety.destroyProtection: requiredOverride`, `apply --override`
    **fails closed before any mutation** rather than rebuilding protected
    resources. That destruction must cross the destroy authorization boundary:
    the operator runs `destroy --override` for the affected scope and then
    re-applies. Dry-run still previews the override plan. See
    [Operations and Recovery](../advanced/operations.md) for the recovery
    patterns.

## The rendering contract

Rendering turns validated desired state into the concrete installer, provider,
and storage CLI inputs Bootwright drives. The render targets are:

- **`install-config.yaml`** ← `ContainerCluster`, `Environment`, selected
  machines, machine `NetworkConfig` references, endpoints, and the platform
  render mode.
- **`agent-config.yaml`** hosts ← `ContainerCluster.spec.hosts`, referenced
  `Machine` objects, `NetworkConfig` templates, per-machine network overrides,
  and **provider or generated substrate MAC inventory**.
- **Machine and endpoint provider variables** ← resolve
  `Machine.spec.network.config.attachmentRef` against
  `InfraProvider.spec.networkAttachments[]`; boot variables come from substrate
  facts, provider capabilities, and cluster artifact access.
- **Shared service variables** ← the single machine-service graph (built from
  `InfraComponent`, environment catalog selections, `NetworkConfig` DNS refs, and
  cluster endpoint sources). The graph owns service identity, consuming clusters,
  machine placement, conflict fields, and mergeable overlay fields, and it is
  resolved once before validation, rendering, status, or scoped apply checks make
  decisions. A partial `apply --stage infra --clusters …` therefore cannot
  silently narrow a service another cluster still depends on.
- **Storage tool inputs** (under `rendered/storage/<storageCluster>/`) ← cephadm
  host, core service, and late service specs; explicit operation metadata;
  Ansible storage contracts; and generated Data Foundation manifests, for managed
  storage only (imported clusters skip them).
- **Managed machine OS inputs** ← `Machine`, `MachineImage`, and
  `MachineInstallProfile`, reusing the same machine-component, provider, BMC,
  virtual-media boot, and SSH-trust contracts as cluster node flows.
- **Extension apply plans** ← `ClusterAddonBinding` expansion,
  `ClusterAddonProfile` order, and `ClusterAddon` generated resources or manifest
  paths. They do **not** mutate installer input.

!!! note "Render is a second enforcement line"
    Rendering is a second enforcement line behind validation for name resolution.
    Every render entry point fails before writing anything when an endpoint
    load-balancer bind or a managed Ceph topology host address does not resolve,
    instead of degrading to output with empty values.

### Shared parsing

ISO references are resolved by the single Bootwright managed media resolver.
Providers, OS installers, and future user-supplied ISO fields must not duplicate
`local-media:`, `file://`, or HTTP(S) parsing. The `local-media:<filename.iso>`
form resolves against the host-local media store at `/var/lib/bootwright/media/`.

### Inspecting rendered inputs

`bootwright render --output-dir <dir> --clusters <cluster> --sensitive` writes the
same concrete tool inputs Bootwright would hand to the supplier or community
CLIs. OpenShift installer files land under
`<dir>/openshift-install/<cluster>/{install,agent}-config.yaml`; Ansible
inventory and vars files land under `<dir>/ansible/`; storage files land under
`<dir>/storage/<storageCluster>/`. The effective-state snapshot and lock file sit
at the top level of `<dir>`.

!!! warning "Rendered runtime inputs can carry secret bytes"
    Because runtime installer files inline secret material, `render --output-dir`
    requires `--sensitive` and fails without it. Rendered **effective state**
    never includes secret bytes. See [Secrets](../advanced/secrets.md).

## The ownership-record cross-boundary contract

Ownership evidence is a named cross-boundary contract between the Ansible
executor and the Go orchestrator. Executing collection roles record per-host
resource and package ownership through `bootwright.core.ownership_record` at
mutation time, and Go reads those records for:

- destroy scoping (removing resources Bootwright created or configured, including
  ones no longer in the input YAML);
- host package-removal gating (a package is removed only when ownership records
  prove Bootwright installed it and no remaining record on that host still
  requires it);
- orphan reporting;
- `state-check` classification.

Run, install, and convergence-safety ledgers remain Go-written. This split keeps
a single source of truth for "what Bootwright owns on each host" while letting Go
own the run lifecycle. See [Operations and Recovery](../advanced/operations.md)
for how destroy consumes these records.

## The Ansible bundle

The Ansible source tree is authored under `<dir>/ansible/` in the repository.
`make sync-bundle` packs that source plus pinned external collections into the
generated embedded archive at `internal/converge/bundle/ansible_bundle.zip`, and
`make build` runs that sync before compiling the CLI. The generated archive is
not versioned.

!!! note
    A source checkout without the generated archive must still compile and report
    an empty embedded bundle for commands that need Ansible until the operator
    runs `make build`.

## Providers and extension

Provider adapters consume capability arms instead of inferring behavior from
names. The current capability arms are:

- machine profiles: `libvirt`, `vsphere`, `kubevirt`;
- explicit machines: `baremetal`.

Adding a substrate means adding a capability arm, validation, renderer support,
and an apply adapter — without moving physical facts into cluster intent.
Provider and BMC behavior must be handled by capability discovery and advertised
metadata before any supplier-specific branching; such workarounds are allowed
only when discovery cannot express the behavior, and must stay isolated, minimal,
tested, and documented. Adapters should reuse the official CLI capabilities of
the tools Bootwright drives before adding custom orchestration around the same
operation — for example, install completion stays delegated to
`openshift-install agent wait-for install-complete`.

For the contributor extension walkthrough, see
[Extending Bootwright](../contributing/extending.md). The full per-field schema
is in the [API Reference](../api/index.md), and the normative contract lives in
[`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md)
and
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).

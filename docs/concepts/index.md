---
title: The desired-state model
description: Desired-state ownership, references, contexts, apply stages, and the v1alpha1 API conventions.
---

# The desired-state model

Bootwright is built around one rule: every operational fact has one owning
object. That lets a single desired-state tree describe an entire cloud platform
or a focused component slice. Rendering combines those objects into the concrete
inputs consumed by `openshift-install`, provider adapters, managed OS
installers, cephadm, storage export flows, and add-on apply tasks.

This page is the landing for the Concepts and APIs section and the hub for the
conventions every domain page shares; each domain page teaches its concept,
documents that domain's API fields, and links back here. The
[Object ownership](#object-ownership) table below is the index of those pages.
For the execution internals (the render pipeline, execution identities, resource
locks, the ownership-record cross-boundary contract, and the four-outcome
classifier in depth) see [Architecture](../contributing/architecture.md).

## Desired state

Desired state is the user-facing API. It is plain YAML using
`apiVersion: bootwright.io/v1alpha1`. Generated installer files, inventories,
runtime locks, logs, kubeconfigs, and secret-inlined files are outputs. Do not
edit generated output as a source of truth.

Desired state is loaded, decoded, normalized, validated, rendered, and applied:

```text
YAML -> strict decode -> normalize defaults -> validate -> render -> apply/status
```

Strict decode means unknown fields fail. There are no migrations or aliases for
retired `v1alpha1` shapes — a retired field is rejected, not translated.

## Object ownership

Each authored kind owns one slice of operational fact. The links point to the
domain page where each kind is documented in full.

### The smallest input

Twenty-one kinds exist; you author a handful. The **core set** for each job is
the floor, and everything else joins only when its scenario does:

- **Smallest OpenShift input** — `Environment`, `Secret`, `Machine`,
  `InfraProvider`, `NetworkConfig`, `ContainerCluster`, plus an
  `InfraComponent` for each cluster service Bootwright itself runs (DNS, load
  balancer, NTP). `examples/sno-libvirt-redfish` is that shape.
- **Smallest Ceph input** — `Environment`, `Secret`, `Machine`,
  `StorageCluster`. `examples/ceph-distribution-oss` is that shape.

See [Examples](../advanced/examples.md) for both trees. The **Required** column
below ranks every kind: `always` (every context authors it), `per cluster`
(one per cluster of that type), or `when you need it`.

| Kind | Required | Owns |
| --- | --- | --- |
| [`Environment`](environment.md) | always | Fleet defaults, selected resources, selected clusters, secret declarations, service access catalog, proxy and registry defaults, install trust, and component image pins. |
| [`Machine`](machines.md) | always | An OS-ready, Bootwright-installed, or installer-provisioned machine: capabilities, substrate binding, hardware inventory, OS mode, install network, named addresses, and how an already-installed machine is reached. |
| [`MachineImage`](machines.md) | when you need it | Bootable OS install media for managed machine OS installs. |
| [`MachineInstallProfile`](machines.md) | when you need it | Reusable managed OS installer settings and customizations. |
| [`InfraProvider`](infrastructure.md) | when you need it | Substrate capability: libvirt, bare metal, vSphere, KubeVirt, machine profiles, provider facts, and network attachments. |
| [`InfraComponent`](infrastructure.md) | when you need it | Machine-bound shared services such as load balancers, artifact servers, DNS, NTP, proxies, and registries. |
| [`NetworkConfig`](infrastructure.md) | when you need it | Reusable machine-network CIDRs, name-resolution selections, and NMState templates. |
| [`ContainerCluster`](container-clusters.md) | per cluster | OpenShift or OKD install intent: distribution, release, install mode, platform render mode, endpoints, networking, pools, and node binding. |
| [`StorageCluster`](storage.md) | per cluster | Imported or Bootwright-managed Ceph storage intent; references machines by node. |
| [`StoragePlacementPolicy`](storage.md) | when you need it | Reusable Ceph placement and replicated-pool defaults. |
| [`StoragePool`](storage.md) | when you need it | Ceph pool role, protection type, placement, replication, and application. |
| [`StorageFilesystem`](storage.md) | when you need it | CephFS filesystem and metadata/data pool mapping. |
| [`StorageObjectGateway`](storage.md) | when you need it | RGW public endpoint and cephadm ingress VIP placement. |
| [`StorageNFSExport`](storage.md) | when you need it | NFS-Ganesha service, its CephFS or RGW exports, and ingress VIP placement. |
| [`StorageExport`](storage.md) | when you need it | Storage services exported for downstream consumers such as Data Foundation. |
| [`ClusterAddon`](add-ons.md) | when you need it | A reusable post-install component applied to an installed cluster. |
| [`ClusterAddonProfile`](add-ons.md) | when you need it | An ordered reusable add-on set. |
| [`ClusterAddonBinding`](add-ons.md) | when you need it | One cluster's selected profiles, add-ons, and binding-scoped input values. |
| [`CustomPlaybook`](custom-playbooks.md) | when you need it | An operator-supplied Ansible playbook run against machines at a chosen provisioning stage, before or after the built-in work. |
| [`Secret`](secrets.md) | always | One named unit of secret material: its `spec.type` (what the material is) and optional `spec.source` (how the bytes are obtained). |
| [`Entitlement`](secrets.md) | when you need it | Named vendor-controlled access for one product: RHSM subscription, product registry, and license for RHEL or Ceph. |

Post-install components and external storage are separate kinds the
`Environment` selects and binds, never `ContainerCluster.spec.install` fields —
see [Add-ons](add-ons.md) and [Storage](storage.md).

## References

References are local names. Most reference fields end in `Ref` or `Refs` and are
authored as plain strings:

```yaml
machineRef: my-sno-lab-master-0
networkConfigRef: my-sno-lab-bridge
componentRef: load-balancer
credentialsRef: bmc-credentials
```

The `{name: ...}` object form is rejected with a shared error. The main flows
are:

```text
ContainerCluster.spec.nodes[].machineRef
  -> Machine
  -> InfraProvider through Machine.spec.substrate.providerRef

Machine.spec.network.config.networkConfigRef
  -> NetworkConfig

Machine.spec.network.config.attachmentRef
  -> InfraProvider.spec.networkAttachments[].name

Machine.spec.network.config.interfaceAttachments[].attachmentRef
  -> InfraProvider.spec.networkAttachments[].name

Environment.spec.infraComponents.*[].componentRef
  -> InfraComponent
  -> Machine through InfraComponent service placement

ClusterAddonBinding
  -> ClusterAddonProfile
  -> ClusterAddon

ClusterAddonBinding.addonConfigs[].inputs[]
  -> StorageExport
  -> StorageCluster
```

Two deliberate carve-outs sit outside the `Ref` grammar:

- `Environment.spec.containerClusters[]` and `storageClusters[]` are *selection
  lists*, not references, so they stay plain strings without a `Ref` suffix.
  When set, they decide which loaded clusters are active for validation, render,
  apply, status, and destroy.
- `kubevirt.networkRef` is the sole sanctioned object-form reference: a network
  object (UDN/CUDN/NAD) lives on the host cluster outside the loaded state, so it
  is identified by an external GVK plus `{name, namespace}` identity. See
  [Infrastructure](infrastructure.md).

## Where objects live on disk

Filenames are pedagogy, not contract: the loader walks every YAML file under
the input directory, so Bootwright never requires a particular name or
nesting. The conventions below are what the scaffold emits, the examples
teach, and reviews expect:

```text
environment.yaml
secrets.yaml
infra/
  providers/
  machines/
  networkconfigs/
  components/
  images/
  profiles/
  entitlements/
clusters/
  container/<cluster>/
    cluster.yaml
    cluster-machines.yaml
    add-on-binding.yaml
  storage/<cluster>/
    cluster.yaml
    nodes/
    placement.yaml
    pools/
    filesystems/
    object-gateways/
    exports/
add-ons/
custom-playbooks/
```

One object per file — secrets grouped in `secrets.yaml` are the exception —
with role-based filenames where the role is unambiguous and `metadata.name`
otherwise. Flat versus foldered: a single cluster with a handful of objects
reads best flat (the small teaching examples are deliberately flat); anything
shared across two clusters, or any second cluster, earns the nested tree.
Machines that exist for one cluster live with that cluster's folder; genuinely
shared infrastructure lives under `infra/`. Fleet-specific guidance — sharing
`infra/` across clusters and the cluster-selection namespace — is in
[Fleets](../advanced/fleets.md).

## Contexts

The **controller** is the host you run Bootwright on: it runs the CLI, Ansible,
and the cluster installers, holds the context state directory below, and is
declared in desired state as a `Machine` with
[`access.local: true`](machines.md#declaring-the-controller). `bootwright bastion
setup` prepares it; the CLI verb keeps the older name, but "controller" is the
word these pages use for the host.

A context gives Bootwright a current input directory and a protected local state
directory on that host:

| Data | Location |
| --- | --- |
| Current context selection | `~/.bootwright/contexts.yaml` |
| Authored YAML (copied in) | `/var/lib/bootwright/contexts/<context>/input` |
| Context state | `/var/lib/bootwright/contexts/<context>` |
| Secrets | `/var/lib/bootwright/contexts/<context>/secrets` |
| Run logs and ledgers | `/var/lib/bootwright/contexts/<context>/runs` |
| Cluster outputs | `/var/lib/bootwright/contexts/<context>/clusters/<cluster>` |

The complete generated-artifact boundary — every path Bootwright writes and its
classification — is in
[`specs/security.md`](https://github.com/crmarques/bootwright/blob/main/specs/security.md)
§Generated Artifacts.

`context init --name <name> -f <dir>` copies the whole source directory into the
context's `input/` directory, so the context is self-contained: every command
reads the copy and it keeps working even if the source is moved or deleted.
Because the input is a copy, editing the source has no effect until you refresh
it with `context update`. Init fails if the context already exists; `--yes`
drops the existing context and recreates it from the source.

!!! note "Refresh input with `context update`"
    `context update --name <name> -f <dir>` replaces the named context's `input/`
    with a fresh copy of the source and preserves everything else (secrets, runs,
    rendered output, clusters, ownership). Because it discards the current input,
    update asks for confirmation first; pass `--yes` to skip the prompt in
    scripts. An `input/` directory that becomes missing or unreadable is a named
    failure at context-resolution time, with a `context update --name <name> -f`
    remediation.

### The context holds a copy of your input

Three commands write that copy: `context update` replaces the whole tree from
your source directory, and two verbs edit objects in place — `diff --adopt`
folds discovered live state into the declared objects, and
`storage-cluster replace-arbiter --new-arbiter-machine` rebinds the stretch
tiebreaker. The two editing verbs change the **context copy only**; your
source tree (usually a git checkout) never sees the rewrite.

!!! warning "`context update` discards in-context rewrites"
    Because `context update` replaces `input/` from your source, any
    `--adopt` or `replace-arbiter` rewrite you have not copied back to the
    source first is discarded by the next update. Round-trip a rewrite before
    touching the source again: copy the context's `input/` tree back over your
    source directory (reading it needs root), review with `git diff`, commit,
    and only then resume the edit-and-`context update` loop.

Before every in-place rewrite the previous input is snapshotted to
`input-history/<seq>-<reason>/` under the context. Recovery from a snapshot is
manual — no verb reads it back: copy the snapshot's contents over the
context's `input/` (or over your source tree, followed by `context update`)
and re-run `validate`.

Run Bootwright as your user. The CLI re-executes through `sudo` when it needs
protected state.

## Platform render mode and substrate type

Substrate facts stay on `Machine` and `InfraProvider`; cluster install intent
stays on `ContainerCluster`. `ContainerCluster.spec.install.platform.type` is
the installer **platform render mode**, not the provider type:

| Topology | Platform mode |
| --- | --- |
| Redfish virtual media on real bare metal | `baremetal` |
| Libvirt VM with emulated Redfish | `baremetal` |
| vSphere agent install | `vsphere` |
| KubeVirt-hosted child machines | `none` |
| Operator-owned external platform | `external` |
| **Any single-node cluster** | `none` (or `external` when authored) |

When the platform is omitted, it derives from the single provider type behind the
bound machines (libvirt and bare metal derive `baremetal` with
`provisioningNetwork: disabled`, vSphere derives `vsphere`, KubeVirt derives
`none`). Machines spanning multiple provider types with the platform omitted are
rejected; for a multi-node cluster an authored platform wins.

Single-node topologies are the exception: every single-node cluster renders
`platform.none` unless `external` is explicitly authored — regardless of the
provider and regardless of an authored `baremetal` or `vsphere`, which is
discarded — because the OpenShift agent installer rejects bare-metal and vSphere
platform blocks for one-control-plane clusters.

## Apply stages and families

The normal command is:

```text
bootwright apply --yes
```

With no `--stage`, `apply` runs the full graph: infrastructure first, then
storage and cluster install, then add-ons. Advanced build-out and recovery can
narrow to a stage **family** or, for `apply`/`plan`/`diff` only, to a single
surgical sub-phase.

### Stage families

`apply`, `plan`, and `destroy` share two stage families. These are the only
values `destroy --stage` accepts.

| Family | Includes |
| --- | --- |
| `infra` | Providers, infra-components, machine-infra, and storage-infra (substrate and machine preparation, including the substrate prep for storage nodes). |
| `clusters` | Storage-cluster, container-cluster, and add-ons (managed Ceph provisioning, OpenShift or OKD install, then post-install add-ons). |

State fabric and machine work live within the `infra` family; storage-cluster,
container-cluster, and add-on work live within the `clusters` family.

### Surgical sub-phases (apply / plan / diff)

`apply`, `plan`, and `diff` additionally accept five single-phase sub-phases for
targeted reruns. They are not stage families and `destroy` does not accept them.

| Sub-phase | Family | Includes |
| --- | --- | --- |
| `fabric` | `infra` | Provider and shared-service preparation. |
| `machines` | `infra` | Machine infrastructure, managed OS work, and RHSM registration of storage nodes. |
| `deps` | `clusters` | Cluster-stage prerequisites. |
| `base` | `clusters` | Container and storage cluster base install. |
| `add-ons` | `clusters` | Post-install add-ons. |

The five sub-phases are the graph's real execution order: `fabric` → `machines`
→ `deps` → `base` → `add-ons`. A family is just a name for a contiguous slice of
it — `infra` is `fabric`..`machines`, `clusters` is `deps`..`add-ons`.

### Stage ranges with `--stage` and `--through`

On `apply`, `plan`, and `diff`, the two flags select an inclusive **range** over
that order. Both accept any family or sub-phase name; `--through` also accepts
`end` for the final phase.

| Form | Runs |
| --- | --- |
| neither flag | The full graph. |
| `--stage X` | Exactly what `X` names: one phase for a sub-phase, that family's phases for `infra` or `clusters`. |
| `--through X` | Every phase from the beginning up to and including `X` — a cumulative build-out. A family means through its last phase, so `--through infra` equals `--through machines`. |
| `--stage X --through Y` | The contiguous `X`..`Y` slice, inclusive. |

```text
bootwright apply --through machines --yes      # build out as far as machines
bootwright apply --stage deps --through base --yes   # replay a mid-graph slice
bootwright apply --stage deps --through end --yes    # deps to the end
```

A range that starts past the first phase assumes the earlier phases already
applied, and the run reports which ones it skipped on that assumption.
Safety gates that must immediately precede a selected mutation are not treated
as skippable prerequisites. In particular, `--stage base` resolves the selected
Ceph image and proves every storage node can start it before any rebuild,
bootstrap, or disk change.

### Selecting clusters

`--clusters` accepts a comma-separated list of `ContainerCluster` and
`StorageCluster` names on every narrowing command. The two kinds share one
cluster selection namespace, so a bare name must be **unique across both kinds**
and resolves to exactly one cluster root. One named exception:
`destroy --stage infra --clusters artifact-server` accepts that literal to
remove only the generated artifact publication service.

!!! warning "KubeVirt child clusters need their parent in scope"
    A KubeVirt-backed child `ContainerCluster` depends on its parent
    virtualization cluster (and the `ClusterAddon` that advertises
    `provides: [kubevirt]`) being installed first. A scoped child apply does
    **not** auto-include the parent: it fails closed before mutation unless the
    parent is selected in the same `--clusters` set, or local runtime records
    already prove the parent install and KubeVirt add-on are ready. See
    [KubeVirt nested clusters](../advanced/kubevirt.md).

### Selecting machines

`--machines` accepts a comma-separated list of `Machine` names and is an
alternative to `--clusters` — the two are mutually exclusive. It provisions or
tears down only the named machines and runs only the `fabric` and `machines`
phases, so a later `apply --clusters <name>` finds those machines already
prepared and skips their setup.

- **On `apply`**, each named machine is brought to the state a cluster install
  expects. A cluster-node machine gets its substrate created (and, for a
  managed-OS storage node, its OS installed and registered); a shared provider
  or service host gets its bound services converged. Without `--stage` it runs
  `fabric` then `machines`; it also composes with `--stage fabric`,
  `--stage machines`, or `--stage infra`.
- **On `destroy`**, only the named machines' substrate is torn down. It refuses
  a node of an installed cluster unless `--authorize installed-cluster-node`, never removes shared
  per-cluster networking or services other machines still rely on, and leaves
  the rest of the cluster standing.
- A named machine that is neither a cluster node nor a shared provider or
  service host has nothing to provision, so the command fails closed instead of
  doing nothing silently.

## Apply modes

`apply --mode` is one single-valued axis stating the run's intent:

- **`--mode reconcile`** (the default) — create what is missing, skip objects
  whose recorded desired state matches current, converge drift that is
  reconcilable in place, and fail closed on structural
  (destructive-identity) drift or foreign ownership before any mutation.
- **`--mode create`** — greenfield assertion: additionally refuse to proceed if
  any selected object already exists.
- **`--mode rebuild`** — break-glass recovery for Bootwright-owned drift it
  knows how to rebuild (managed-OS reinstall, owned-Ceph wipe-and-rebuild,
  drifted owned-object rebuild). It never touches foreign objects, leases,
  validation, or secret checks.

A rebuild that would wipe disks or zap OSDs additionally needs
`--authorize data-loss` (or the interactive data-loss prompt); the mode states
intent, the token accepts the risk.

!!! note "Intent is one choice, not a combination"
    `--mode` is single-valued, so a run cannot ask for both a greenfield
    assertion and a rebuild. On a destroy-protected `Environment`,
    `apply --mode rebuild` fails closed and directs you to
    `destroy --authorize protected` first; see
    [Convergence and drift](#convergence-and-drift) and
    [Operations, recovery and teardown](../advanced/operations.md).

## Convergence and drift

Bootwright records non-secret desired hashes and ownership evidence while it
mutates resources. Re-running `apply` creates what is missing, skips completed
matching work when a concrete probe supports it, and fails closed when recorded
state is foreign or unsafe to resume.

Use `bootwright diff` to compare selected desired state with the **live**
clusters: it discovers real Ceph state read-only and prints a git-style diff of
desired-vs-real (and shallow-checks container reachability), and `--adopt` folds
that reality back into desired-state YAML. Add `--recorded` for the fast,
no-contact check that classifies each resource against the last recorded apply as
`missing`, `match`, `drift`, or `foreign` (recorded evidence, not live state). The
`--recorded` report also lists `undeclared` resources — objects owned by
Bootwright but no longer in desired state — which are report-only and do not
affect the exit code. For the
classifier — including why classification is **not** itself an apply-time skip
gate — see [Architecture](../contributing/architecture.md).

## Destroy

`destroy` mirrors the apply stages but accepts only the two families. Its
no-`--stage` default differs from `apply`: it is a **full lifecycle** teardown,
the inverse of build-up, and unscoped it also sweeps context-owned VM artifacts
and orphan ownership records. `--clusters` narrows that lifecycle to the named
roots; `--machines` tears down only those machines' substrate and refuses an
installed cluster's node without `--authorize installed-cluster-node`. The per-invocation matrix, the
teardown order and what each stage retains are in
[Operations, recovery and teardown](../advanced/operations.md#tearing-down-with-destroy).

`Environment.spec.safety.destroyProtection` and `spec.safety.protectedKinds[]`
both gate teardown: with either in force, `destroy` requires
`--authorize protected`. See
[Destroy protection](../advanced/operations.md#destroy-protection-and-the-authorization-boundary).

!!! note "No stage means full lifecycle"
    Passing `--clusters` narrows the full lifecycle to those roots. Use
    `--stage clusters --clusters <names>` to retain their machine substrate.
    Disk cleanup is limited to provider-owned disks or declared
    Bootwright-managed devices; physical bare-metal hardware is retained.
    Recovery patterns live in
    [Operations, recovery and teardown](../advanced/operations.md).

## API conventions

The rest of this page owns the conventions that recur on every domain page — the
object envelope, the union and collection grammars, feature-block enablement,
references, defaults, and the validation model. Each domain page links back here
instead of restating them. The normative contract remains
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md),
and the public Go types live under
[`api/v1alpha1`](https://github.com/crmarques/bootwright/tree/main/api/v1alpha1).

### Object envelope

Every authored resource uses the same top-level shape. Each domain page
documents only its own `spec`; this envelope is the same throughout.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example
spec:
  domains:
    base: example.com
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `apiVersion` | Yes | — | Must be `bootwright.io/v1alpha1`. |
| `kind` | Yes | — | One of the authored kinds above. |
| `metadata.name` | Yes | — | DNS-label object name, unique within its kind. |
| `metadata.labels` | No | — | Free-form labels; validated on `Machine` and projected into that machine's inventory vars, ignored on every other kind. |
| `spec` | Yes | — | Kind-specific desired state. |

!!! note "Cluster names share one selection namespace"
    `ContainerCluster` and `StorageCluster` names must additionally be unique
    across *both* kinds, not just within each kind. They share a single cluster
    selection namespace, so a bare `--clusters <name>` resolves against both
    kinds and selects exactly one cluster root.

### Field-table convention

Every field table on the domain pages — including sub-tables — carries a
**Required** column and a **Default** column, read together:

- **Required: Yes** — the author must set the field; omitting it fails
  validation.
- **Required: No** with a stated default — the field is optional, and the
  normalize phase injects that default before renderers and validators read it.
  For example `installPlanApproval` is `Required: No`, default `Automatic`. A
  defaulted field is never marked `Required: Yes`.
- **Required: No** with no default — the field is genuinely optional and absent
  unless authored.

Cross-field validation rules ("X must be empty when Y", "exactly one of …",
"required when …") appear as explicit notes on the relevant page, because those
are the silent authoring failures the reference exists to catch. The column
itself carries only `Yes`, `No`, and — for list entries whose every element
must set the field — `Yes (per entry)`; no other conditional spelling belongs
in the column, because the note states the condition precisely.

### Native mapping

A kind page that fronts a driven tool (`openshift-install`,
cephadm/`ceph orch`, Anaconda kickstart, nmstate, Redfish/Metal3) carries a
**Native mapping** section: one table per tool, mapping the tool's own input
vocabulary to the authored field that produces it. Each row gives the native
key or flag in the tool's own spelling; the Bootwright path (or *derived* when
Bootwright computes it and nothing is authorable); the divergence class —
`mirror` (camelCase respellings per the API grammar count as mirrored),
`renamed`, `relocated` (another kind owns it), `restructured`, `derived`, or
`invented` (no native counterpart) — and what the divergence buys. A
divergence earns its place only through orchestration value: a cross-document
reference, secret `…Ref` indirection, a fleet-level default, multi-cluster
composition, or safety. The normative rule is the Compatibility Goal in
[`specs/domain.md`](https://github.com/crmarques/bootwright/blob/main/specs/domain.md);
these tables are its operator-facing rendering — an operator who knows the
native tool reads off where each fact they already know is authored.

### Grammars

| Convention | Meaning |
| --- | --- |
| Discriminated union | A `type` field selects the populated arm, and the arm key is byte-identical to the `type` value, such as `InfraProvider.spec.type: libvirt` with `spec.libvirt`. The same grammar governs `install.platform`, `InfraComponent.spec`, `ClusterAddon.spec`, `StorageExport.spec`, and `StoragePool.spec.ceph`. |
| Presence union | Exactly one arm is set and its presence is the discriminator, with no separate `type` field: the `MachineInstallProfile` installer (`anaconda` is the only backend), `MachineInstallProfile` `packageSource`, `Secret.spec.source`, and `InfraProvider.spec.networkAttachments`, where the provider's own `spec.type` already fixes the kind. |
| Named list | User-invented names are list entries with a `name` field, such as `addresses[]`, `machineProfiles[]`, and `networkAttachments[]`. |
| Closed map | Name-keyed maps appear only where the key set is a fixed, validated vocabulary — `ContainerCluster.spec.install.endpoints` (`api`, `api-int`, `ingress`) and `Environment.spec.componentImages` (the `componentType`/`implementation` catalog). |
| Feature block (enable/disable) | Optional feature blocks are presence-managed; see [Feature blocks](#feature-blocks). |
| Defaults | The normalize phase injects defaults before rendering; `render effective` materializes them so operators can inspect what renderers consume — for example `distribution: openshift`, the `api-int` copy of `api`, and the default cluster and service networks. |
| References | Plain name strings with a `Ref`/`Refs` suffix; see [References](#references). |
| Reserved spellings | `type` is reserved for kind-of-thing discriminators, and `management` (`managed`/`external`) is reserved for the who-runs-it axis — who operates the thing, Bootwright or you; no other field uses either word for another meaning (ADR 0014). |
| Secrets | Desired state stores only names and local source paths. Secret bytes live in the context secret store or operator-owned local files; see [Secrets & entitlements](secrets.md). |

### Feature blocks

Optional feature blocks are presence-managed. Omitting a block keeps the
upstream tool's default behavior, so how you opt in or out depends on what that
default is. Three patterns recur:

- **Presence is the enablement signal.** Omit the block to keep it off; author
  the block to turn it on. `StorageCluster.spec.ceph.topology.stretch` works
  this way — its presence enables stretch mode.
- **`enabled *bool` defaulting to `true`.** Blocks whose upstream default is on
  carry a tri-state `enabled` pointer that defaults to `true`, so authoring the
  block with `enabled: false` is the opt-out. `StorageCluster.spec.ceph.monitoring`
  and libvirt `bmcEmulationDefaults` use this.
- **Plain `bool enabled`.** A non-pointer `enabled` appears only where `false`
  and unset mean the same thing, such as
  `MachineInstallProfile` `customizations.security.fips`. Contrast its
  `firewall` sibling, which keeps the `*bool` tri-state because an explicit
  `false` renders a real disable while unset renders nothing at all.

### Secrets

Desired state references secrets by name only and never carries secret bytes, so
it is safe to commit. A reference names a `kind: Secret` object; the bytes live
in the context secret store or operator-owned local files. See
[Secrets & entitlements](secrets.md) for the source/context storage modes and
`secret generate`.

!!! note "Secret is its own kind"
    A secret is a first-class `kind: Secret` object with a `spec.type` (what the
    material is) and an optional `spec.source` (how it is obtained), not an
    `Environment` field. The [Secrets & entitlements](secrets.md) page documents
    the type vocabulary and the `contextStore`/`file`/`generated` source arms.

### Validation model

Validation names the owning object and field. Unknown fields fail strict decode.
Retired fields are rejected instead of translated — `v1alpha1` carries no
backward-compatibility shims. Cross-object validation checks reference targets,
machine binding exclusivity, disconnected install requirements, service
conflicts, KubeVirt parent dependencies, storage topology, and add-on input
schemas before any mutation.

!!! note "Authored fields, not rendered output"
    These pages document the fields *you author*. Keys Bootwright derives into
    generated `install-config.yaml` and `agent-config.yaml` (for example
    `baseDomain`, `pullSecret`, `platform.baremetal.apiVIPs`) are installer
    outputs — and each page's [Native mapping](#native-mapping) table tells
    you which authored field produces each one. See
    [Architecture](../contributing/architecture.md) for the render pipeline.

## Where to go next

The [Object ownership](#object-ownership) table above is the index of the domain
pages. Beyond them: [Getting Started](../getting-started/index.md) for the first
complete apply path, [Advanced Scenarios](../advanced/index.md) for provider,
networking, storage, and recovery scenarios, and
[Architecture](../contributing/architecture.md) for the execution and render
pipeline deep dive.

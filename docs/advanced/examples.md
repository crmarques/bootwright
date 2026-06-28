---
title: Reference Examples
description: A catalog of the complete, safe-to-commit desired-state trees under examples/ — each one's complexity, the situation it fits, and the kinds it exercises.
---

# Reference examples

The repository ships complete, safe-to-commit desired-state trees under
`examples/`. Each one is a full topology you can read end to end, diff against
your own input, or adapt for a lab. They reference secrets by name only; the
secret bytes are still supplied through the local context, never checked in.

When you are starting fresh, prefer the CLI scaffold instead of copying an
example by hand:

```text
bootwright example init --name <cluster-name> --output-dir <input-dir>
```

The scaffold produces a nested `infra/` + `clusters/container/<name>/` workspace and is
what [Getting Started](../getting-started/index.md) walks through. The committed
examples below are reference trees for comparison and adaptation — some use a
different (often flatter, more minimal) shape than the scaffold, so read the
example's own `README.md` for its authoritative file list.

!!! note "This page is a chooser only"
    The table maps each example directory to the situation it fits. The
    authoritative description, file list, and apply notes for any example live
    in that directory's own `README.md`. When the table and a README disagree,
    the README wins.

## Choose an example

The **Complexity** column groups the trees by how much they ask you to take on
at once: `simple` (one cluster, a handful of files), `intermediate` (shared
services, directory layout, entitlements, or a disconnected posture), and
`advanced` (add-on model, imported/managed storage, or the full multi-DC
platform). The **Demonstrates / Kinds** column names what each tree teaches and
the authored kinds it exercises.

| Example | Complexity | Use it when | Demonstrates / Kinds |
| --- | --- | --- | --- |
| `examples/sno-libvirt-redfish` | simple | You want the smallest checked-in single-node OpenShift reference using libvirt and an emulated Redfish BMC. | Minimal SNO install; object-ownership split; managed `dnsmasq` name resolution on a bastion; external API/ingress endpoints. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `ContainerCluster`. |
| `examples/sno-baremetal-redfish` | simple | You are moving the same SNO pattern to real bare metal with Redfish virtual media. | Real Redfish virtual-media provisioning; server MACs, BMC URL, and root device hints on `InfraProvider`; an artifact-server component. Same six kinds as the libvirt lab so you can diff substrate-only differences. |
| `examples/sno-libvirt-redfish-disconnected-services` | intermediate | You need a single-node disconnected lab with a managed mirror registry, artifact server, and NTP, behind an external proxy and external DNS. | Disconnected install mode; **managed** mirror registry, artifact server, and NTP as three `InfraComponent`s; **external** proxy and name resolution declared in `Environment`; trust material plus a `bootwright host trust` step. Same six kinds as the SNO labs. |
| `examples/baremetal-redfish-fleet` | intermediate | You need a multi-cluster bare-metal layout with shared infrastructure split under `infra/`. | Two OpenShift clusters across two data-center networks; canonical `infra/` + `clusters/container/<name>/` layout; directory resource selection via `Environment.spec.resources`; shared provider services. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `ContainerCluster`. |
| `examples/baremetal-redfish-addons` | advanced | You need the day-2 add-on model: ordered profiles, OLM operators with readiness checks, and a raw-manifest add-on. | OLM operators with channels, `startingCSV`, and readiness checks; ordered profiles bound to a cluster; a `manifestSet` add-on delivering a raw `Namespace`. Kinds: the six core plus `ClusterAddon`, `ClusterAddonProfile`, `ClusterAddonBinding`. |
| `examples/baremetal-redfish-imported-ceph-odf` | advanced | You need an OpenShift cluster consuming an **imported** (externally managed) Ceph cluster through Data Foundation. | `StorageCluster` in import posture; a `StorageExport` surface bound to ODF external mode via a file secret; `clusters/storage/` alongside `clusters/container/`. Kinds: the six core plus `StorageCluster`, `StorageExport`, `ClusterAddon`, `ClusterAddonBinding`. |
| `examples/baremetal-redfish-multidc-virtualized-odf-ceph` | advanced | You want the full reference platform: parent clusters, KubeVirt child clusters, managed stretched Ceph, and Data Foundation. | Multi-DC bare-metal parents with KubeVirt-hosted children; a managed stretched Ceph cluster (two sites plus a tiebreaker); the full storage spine; ODF and OpenShift Virtualization with selective per-cluster binding; parent/child apply ordering. Exercises nearly every kind, including `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, and `StorageExport`. |
| `examples/ceph-distribution-oss` | simple | You need managed Ceph with upstream/community sources, in isolation from any OpenShift cluster. | Community (OSS) Ceph distribution with no entitlement; minimal storage-node `Machine`. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`. |
| `examples/ceph-distribution-redhat` | intermediate | You need managed Red Hat Ceph Storage with Red Hat entitlement references. | Entitlement model (one named `Environment.spec.entitlements[]` referenced by `StorageCluster.spec.ceph.entitlementRef`); RHSM plus `registry.redhat.io` service-account credential plumbing. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`. |
| `examples/ceph-distribution-ibm` | intermediate | You need IBM Storage Ceph entitlement and license modeling. | IBM Storage Ceph distribution; the IBM license-acceptance gate (`license.accept`); the `cp.icr.io/cp` registry credential model; the separate `redhat/rhel` entitlement the IBM item names via `rhelEntitlementRef` for the RHEL subscription. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`. |
| `examples/ceph-ibm-libvirt-lab` | advanced | You want an end-to-end, self-contained IBM Storage Ceph lab on one machine — three libvirt VMs installed with managed RHEL, then block + file + object storage with an HA dashboard. | The full provisioning loop on a single host: libvirt VMs with emulated Redfish BMCs, Bootwright-managed RHEL install, then a managed IBM Storage Ceph cluster (three mons incl. a tiebreaker, two OSD nodes) serving RBD, CephFS, and RGW; an RGW ingress VIP plus a native `mgmt-gateway` HA dashboard VIP (`keepalive_only` ingress); lab `dnsmasq` resolving both. Storage-only. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `MachineImage`, `MachineInstallProfile`, `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`. |
| `examples/ceph-ibm-baremetal-redfish` | advanced | You want the same IBM Storage Ceph build on real bare metal behind enterprise network services — three physical servers provisioned over Redfish, on an external proxy with external DNS and NTP. | Real Redfish virtual-media provisioning of three physical nodes; Bootwright-managed RHEL install before cephadm; a managed IBM Storage Ceph cluster (three mons incl. a tiebreaker, two OSD nodes) serving RBD, CephFS, and RGW with an ingress VIP and a `mgmt-gateway` HA dashboard VIP; an **external** proxy (`proxyFor.bootwright`), **external** name resolution, and **external** NTP declared in `Environment.spec.infraComponents`, with only a bastion artifact server managed. Storage-only. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `MachineImage`, `MachineInstallProfile`, `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`. |

!!! note "Managed Ceph vs imported Ceph"
    The three `ceph-distribution-*` examples are **storage-only** trees: a
    `StorageCluster` with no `ContainerCluster`, provisioned and managed by
    Bootwright via `cephadm`. They differ only in which Ceph distribution they
    select (community, Red Hat, IBM) and the entitlement that selection needs.
    Contrast them with `baremetal-redfish-imported-ceph-odf`, where the
    `StorageCluster` is in **import** posture — it consumes an externally
    managed Ceph cluster rather than provisioning one. See
    [Ceph storage topologies](ceph-topologies.md) for the managed-vs-imported axis.

## How to review an example

1. Read the example `README.md` first — it is the authoritative description of
   that tree, including its file list and any apply notes.
2. Open `environment.yaml`. It shows selected resource files or directories,
   cluster selection, the base domain, secret names, entitlements, and the
   managed service catalog (`spec.infraComponents`).
3. Read the `InfraProvider` and `Machine` files next. They show substrate
   selection, machine addresses, SSH, BMC, and network-attachment ownership.
4. Read the `ContainerCluster` or `StorageCluster` files last. They should
   carry install or storage intent, not low-level substrate facts.
5. Validate before adapting:

```text
bootwright validate -f <example-dir>
```

Validation is offline — it never contacts hosts — and should fail only where an
example intentionally leaves placeholders that you must edit for your lab.

!!! note "File layout differs between examples"
    The simple single-node and `ceph-distribution-*` trees use a **flat**
    layout (all files at the top level). The larger fleet, add-on, and storage
    examples use a **nested** layout with `infra/` and
    `clusters/container|storage/<name>/` subdirectories selected through
    `Environment.spec.resources`. The review steps above name concerns, not
    fixed filenames; follow each example's `README.md` for the exact tree.

## Authoring guidance

Bootwright's vocabulary invariant is that every fact has exactly one owner. When
you adapt an example, keep each change in the kind that owns it (see
[The desired-state model](../concepts/index.md) for the ownership rule):

- Change `Environment` for fleet defaults, resource selection, secret names,
  service-catalog entries, proxies, registries, and entitlements.
- Change `Machine` for addresses, SSH, hardware inventory, OS mode, install
  profile references, and node network bindings.
- Change `InfraProvider` for substrate-specific capabilities and machine
  profiles.
- Change `NetworkConfig` for reusable machine networks and NMState templates.
- Change `InfraComponent` for shared services placed on machines.
- Change `ContainerCluster` only for OpenShift or OKD install intent.
- Change the storage kinds only for Ceph and exported-storage intent.
- Change the add-on kinds only for post-install bootstrap behavior.

For the per-kind field reference, follow the kind into Concepts and APIs:
[Infrastructure](../concepts/infrastructure.md) for providers, components, and
`NetworkConfig`; [Networking](networking.md) for assembling endpoints and
load balancers; [Ceph storage topologies](ceph-topologies.md) and the
[storage reference](../concepts/storage.md) for the storage kinds; and
[Add-ons](../concepts/add-ons.md) for the post-install kinds.

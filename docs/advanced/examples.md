---
title: Reference Examples
description: A catalog of the complete, safe-to-commit desired-state trees under examples/ — each one's complexity, the situation it fits, and the kinds it exercises.
---

# Reference examples

The repository ships complete, safe-to-commit desired-state trees under
`examples/`. Each one is a full topology you can read end to end, diff against
your own input, or adapt for a lab. Every tree authors first-class `kind: Secret`
objects — each declaring its secret's type — and references them by
name from the other kinds; the secret **bytes** are still supplied through the
local context, never checked in.

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

!!! tip "No vSphere example tree — scaffold one"
    The committed examples cover libvirt/emulated-Redfish, bare-metal Redfish,
    and KubeVirt substrates; there is no vSphere example. To start a
    vSphere-backed topology, scaffold one with
    `bootwright example init --provider vsphere` and adapt it.

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

Every tree also authors one or more `kind: Secret` objects (grouped into a
`secrets.yaml`), so the Kinds lists below omit `Secret` as universal and name
only the domain kinds that distinguish each tree.

| Example | Complexity | Use it when | Demonstrates / Kinds |
| --- | --- | --- | --- |
| [`examples/sno-libvirt-redfish`](https://github.com/crmarques/bootwright/tree/main/examples/sno-libvirt-redfish) | simple | You want the smallest checked-in single-node OpenShift reference using libvirt and an emulated Redfish BMC. | Minimal SNO install; object-ownership split; managed `dnsmasq` name resolution on a bastion; external API/ingress endpoints. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `ContainerCluster`. |
| [`examples/sno-baremetal-redfish`](https://github.com/crmarques/bootwright/tree/main/examples/sno-baremetal-redfish) | simple | You are moving the same SNO pattern to real bare metal with Redfish virtual media. | Real Redfish virtual-media provisioning; server MACs, BMC URL, and root device hints on `Machine` (`spec.hardware`), with boot method and network attachments on `InfraProvider`; an artifact-server component. Same six kinds as the libvirt lab so you can diff substrate-only differences. |
| [`examples/sno-libvirt-redfish-disconnected-services`](https://github.com/crmarques/bootwright/tree/main/examples/sno-libvirt-redfish-disconnected-services) | intermediate | You need a single-node disconnected lab with a managed mirror registry, artifact server, and NTP, behind an external proxy and external DNS. | Disconnected install mode; **managed** mirror registry, artifact server, and NTP as three `InfraComponent`s; **external** proxy and name resolution declared in `Environment`; trust material plus a `bootwright machine trust` step. Same six kinds as the SNO labs. |
| [`examples/sno-libvirt-redfish-corporate-tls`](https://github.com/crmarques/bootwright/tree/main/examples/sno-libvirt-redfish-corporate-tls) | simple | You want the smallest single-node lab that serves corporate-issued certificates on the cluster URLs and trusts a corporate CA. | Corporate `servingCertificates` (custom API and ingress certs) rendered as day-2 `APIServer`/`IngressController` configs plus their TLS secrets; cluster-scoped `additionalTrustBundleRefs`; self-contained via generated self-signed material. Same six kinds as the SNO labs. |
| [`examples/baremetal-redfish-fleet`](https://github.com/crmarques/bootwright/tree/main/examples/baremetal-redfish-fleet) | intermediate | You need a multi-cluster bare-metal layout with shared infrastructure split under `infra/`. | Two OpenShift clusters across two data-center networks; canonical `infra/` + `clusters/container/<name>/` layout; directory resource selection via `Environment.spec.resources`; shared provider services. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `ContainerCluster`. |
| [`examples/baremetal-redfish-addons`](https://github.com/crmarques/bootwright/tree/main/examples/baremetal-redfish-addons) | advanced | You need the day-2 add-on model: ordered profiles, OLM operators with readiness checks, and a raw-manifest add-on. | OLM operators with channels and readiness checks; ordered profiles bound to a cluster; a `manifestSet` add-on delivering a raw `Namespace`; an operator-authored `register-nodes` playbook step. Kinds: the six core plus `ClusterAddon`, `ClusterAddonProfile`, `ClusterAddonBinding`, `CustomPlaybook`. |
| [`examples/baremetal-redfish-imported-ceph-odf`](https://github.com/crmarques/bootwright/tree/main/examples/baremetal-redfish-imported-ceph-odf) | advanced | You need an OpenShift cluster consuming an **imported** (externally managed) Ceph cluster through Data Foundation. | `StorageCluster` in import posture; a `StorageExport` surface bound to ODF external mode via a file secret; `clusters/storage/` alongside `clusters/container/`. Kinds: the six core plus `StorageCluster`, `StorageExport`, `ClusterAddon`, `ClusterAddonBinding`. |
| [`examples/baremetal-redfish-multidc-virtualized-odf-ceph`](https://github.com/crmarques/bootwright/tree/main/examples/baremetal-redfish-multidc-virtualized-odf-ceph) | advanced | You want the full reference platform: parent clusters, KubeVirt child clusters, managed stretched Ceph, and Data Foundation. | Multi-DC bare-metal parents with KubeVirt-hosted children; a managed stretched Ceph cluster (two sites plus a tiebreaker); the full storage spine; ODF and OpenShift Virtualization with selective per-cluster binding; parent/child apply ordering. Exercises nearly every kind, including `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, and `StorageExport`. |
| [`examples/ceph-distribution-oss`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-distribution-oss) | simple | You want to read how the community/OSS Ceph distribution is selected (a schema snippet, not a runnable tree). | **Distribution/entitlement snippet, not runnable as-is:** its one storage-node `Machine` is `os.provided` on a placeholder (RFC-5737) address, so it selects the distribution model rather than provisioning anything. Community (OSS) Ceph with no entitlement. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`. |
| [`examples/ceph-distribution-redhat`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-distribution-redhat) | simple | You want to read how Red Hat Ceph Storage entitlement is referenced (a schema snippet, not a runnable tree). | **Distribution/entitlement snippet, not runnable as-is** (placeholder `os.provided` `Machine`, as above). Entitlement model (one named `Entitlement` referenced by `StorageCluster.spec.ceph.entitlementRef`); RHSM plus `registry.redhat.io` service-account credential plumbing. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`, `Entitlement`. |
| [`examples/ceph-distribution-ibm`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-distribution-ibm) | simple | You want to read how IBM Storage Ceph entitlement and license acceptance are modeled (a schema snippet, not a runnable tree). | **Distribution/entitlement snippet, not runnable as-is** (placeholder `os.provided` `Machine`, as above). IBM Storage Ceph distribution; the IBM license-acceptance gate (`license.accept`); the `cp.icr.io/cp` registry credential model; the separate `redhat-rhel` entitlement named by the cluster's `spec.ceph.osSubscriptionRef` for the RHEL subscription. Storage-only. Kinds: `Environment`, `Machine`, `StorageCluster`, `Entitlement`. |
| [`examples/ceph-external-rhsm`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-external-rhsm) | intermediate | Your organization owns RHSM registration (Satellite or site automation) and Bootwright should install Red Hat Ceph without touching subscriptions. | Delegated registration: the `rhcs` `Entitlement` sets `rhsm.management: external`, so Bootwright plans no registration task, never touches `rhsm.conf`, and demands no RHSM organization/activation-key secrets; a `corporate-rhsm` `CustomPlaybook` (`gates: deps`) registers the nodes and gates the Ceph deps work while Bootwright still installs Ceph. Storage-only snippet (placeholder provided-OS `Machine`), not runnable as-is. Kinds: `Environment`, `Machine`, `StorageCluster`, `Entitlement`, `CustomPlaybook`. |
| [`examples/ceph-ibm-libvirt-lab`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-ibm-libvirt-lab) | advanced | You want an end-to-end, self-contained IBM Storage Ceph lab on one machine — three libvirt VMs installed with managed RHEL, then block + file + object storage with an HA dashboard. | The full provisioning loop on a single host: libvirt VMs with emulated Redfish BMCs, Bootwright-managed RHEL install, then a managed IBM Storage Ceph cluster (three mons incl. a tiebreaker, two OSD nodes) serving RBD, CephFS, and RGW; an RGW ingress VIP plus a native `mgmt-gateway` HA dashboard VIP (`keepalive_only` ingress); lab `dnsmasq` resolving both. Storage-only. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `Entitlement`, `MachineImage`, `MachineInstallProfile`, `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`. |
| [`examples/ceph-ibm-baremetal-redfish`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-ibm-baremetal-redfish) | advanced | You want the same IBM Storage Ceph build on real bare metal behind enterprise network services — three physical servers provisioned over Redfish, on an external proxy with external DNS and NTP. | Real Redfish virtual-media provisioning of three physical nodes; Bootwright-managed RHEL install before cephadm; a managed IBM Storage Ceph cluster (three mons incl. a tiebreaker, two OSD nodes) serving RBD, CephFS, RGW, and NFS with an ingress VIP and a `mgmt-gateway` HA dashboard VIP; an **external** proxy (`proxyFor.bootwright`), **external** name resolution, and **external** NTP declared in `Environment.spec.infraComponents`, with only a bastion artifact server managed. Storage-only. Kinds: `Environment`, `Machine`, `InfraProvider`, `NetworkConfig`, `InfraComponent`, `Entitlement`, `MachineImage`, `MachineInstallProfile`, `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport`. |

!!! note "Managed Ceph vs imported Ceph"
    The three `ceph-distribution-*` examples are **storage-only distribution
    snippets**, not runnable trees: each is a `StorageCluster` with no
    `ContainerCluster` and a single placeholder (`os.provided`, RFC-5737 address)
    `Machine`. They exist to show how each Ceph distribution is selected
    (community, Red Hat, IBM) and the entitlement that selection needs — for an
    end-to-end managed IBM Ceph build provisioned by Bootwright via `cephadm`,
    use `ceph-ibm-libvirt-lab` or `ceph-ibm-baremetal-redfish` instead.
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

Validation is offline — it never contacts hosts. Every committed example
validates clean (a repository test gates them to stay that way).
Note that validation is structural: it does not reject placeholder addresses, so
a tree can pass `validate` and still be non-runnable until you replace its
placeholders (for example, the `ceph-distribution-*` snippets use RFC-5737
documentation addresses and pass validation but are not meant to be applied).

!!! note "File layout differs between examples"
    The simple single-node and `ceph-distribution-*` trees are **mostly flat**,
    with files at the top level — though some simple trees (for example
    `sno-baremetal-redfish`) still group a few objects under a `components/`
    subfolder. The larger fleet, add-on, and storage examples use a **nested**
    layout with `infra/` and `clusters/container|storage/<name>/` subdirectories
    selected through `Environment.spec.resources`. The review steps above name
    concerns, not fixed filenames; follow each example's `README.md` for the
    exact tree.

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

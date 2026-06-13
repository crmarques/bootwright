---
title: Advanced Examples
description: How to choose a complete Bootwright example tree.
---

# Advanced Examples

Start new environments from the CLI scaffold when possible:

```text
bootwright example init <cluster-name> --output <input-dir>
```

Use the in-repository examples when you need a complete topology to compare
against. They are safe-to-commit desired-state trees; secret bytes are still
supplied through the local context.

## Example Matrix

| Example | Use it when |
| --- | --- |
| `examples/sno-libvirt-redfish` | You want the smallest checked-in SNO reference using libvirt and emulated Redfish. |
| `examples/sno-baremetal-redfish` | You are moving the same SNO pattern to real bare metal with Redfish virtual media. |
| `examples/sno-libvirt-redfish-disconnected-services` | You need a single-node disconnected lab with managed proxy, registry, artifact, DNS, and NTP services. |
| `examples/baremetal-redfish-fleet` | You need a multi-cluster bare-metal layout with shared infrastructure split under `infra/`. |
| `examples/baremetal-redfish-addons` | You need post-install add-ons such as OpenShift Virtualization on a bare-metal cluster. |
| `examples/baremetal-redfish-imported-ceph-odf` | You need an OpenShift cluster consuming imported external Ceph through Data Foundation. |
| `examples/baremetal-redfish-multidc-virtualized-odf-ceph` | You need the full reference layout: parent clusters, KubeVirt child clusters, managed Ceph, and Data Foundation. |
| `examples/ceph-distribution-oss` | You need managed Ceph with upstream/community sources. |
| `examples/ceph-distribution-redhat` | You need managed Red Hat Ceph Storage with Red Hat entitlement references. |
| `examples/ceph-distribution-ibm` | You need IBM Storage Ceph entitlement and license modeling. |

## How To Review An Example

1. Read the example `README.md`.
2. Open `environment.yaml` first. It shows selected clusters, shared defaults,
   secrets, entitlements, and managed service catalog entries.
3. Read provider and machine files next. They show substrate selection, machine
   addresses, SSH, BMC, and network attachment ownership.
4. Read `ContainerCluster` or `StorageCluster` files last. They should contain
   cluster or storage intent, not low-level substrate facts.
5. Run validation before importing:

```text
bootwright validate -f <example-dir>
```

Validation should fail only when the example intentionally contains placeholders
that must be edited for your lab.

## Authoring Guidance

Keep example changes small and object-owned:

- Change `Environment` for fleet defaults, resource selection, secrets,
  service catalog entries, proxies, mirrors, and entitlements.
- Change `Machine` for addresses, SSH, hardware inventory, OS mode, install
  profile references, and node network bindings.
- Change `InfraProvider` for substrate-specific capabilities and machine
  profiles.
- Change `NetworkConfig` for reusable machine networks and NMState templates.
- Change `InfraComponent` for shared services placed on machines.
- Change `ContainerCluster` only for OpenShift or OKD install intent.
- Change storage kinds only for Ceph and exported storage intent.
- Change add-on kinds only for post-install bootstrap behavior.

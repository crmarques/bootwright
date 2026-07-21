---
title: Advanced Scenarios
description: Task-oriented how-tos for fleets, disconnected installs, managed OS, Ceph, KubeVirt, networking, and operations.
---

# Advanced Scenarios

Advanced Scenarios are task-oriented how-tos that build on
[Getting Started](../getting-started/index.md); for the object model and every
field, see [Concepts and APIs](../concepts/index.md). Each page below assumes you
have already run the first single-cluster apply path and now need to extend it —
to a fleet, a disconnected estate, managed OS installs, a particular Ceph
topology, nested KubeVirt clusters, custom networking, or recovery.

| Scenario | Use it for |
| --- | --- |
| [Multi-cluster fleets & shared services](fleets.md) | One `Environment` selecting many clusters, the single selection namespace, shared `InfraComponent` services, and scoped apply/destroy. |
| [Disconnected & proxied installs](disconnected-proxy.md) | Environment proxy defaults, managed vs external proxies, the three proxy targets, mirror registries, image digest sources, and trust. |
| [Corporate TLS certificates & trusted CAs](corporate-certificates.md) | Replacing the default API/ingress serving certificates with corporate-issued ones, and adding corporate CAs to the cluster install trust. |
| [Managed OS installs](managed-os.md) | When Bootwright installs the node OS itself: `MachineImage` media, `MachineInstallProfile` customizations, staging media, and Anaconda over a proxy. |
| [Ceph storage topologies](ceph-topologies.md) | Managed vs imported Ceph, pools, CephFS, RGW, stretch mode, and storage access. |
| [KubeVirt nested clusters](kubevirt.md) | A child `ContainerCluster` whose machines come from a KubeVirt `InfraProvider`, and the parent/child apply ordering. |
| [Networking & load balancing](networking.md) | `NetworkConfig` templates, provider attachments, endpoints, VIPs, DNS, and NTP. |
| [Ownership, idempotency & safety](ownership-and-safety.md) | How Bootwright tracks ownership, what makes re-running `apply` safe, the fail-closed checkpoints, and how to avoid accidentally destroying clusters. |
| [Operations & recovery](operations.md) | Destroy stages, `destroyProtection`, `--force`, and focused `--stage`/`--clusters` recovery. |
| [Reference examples](examples.md) | Choosing the right complete example tree before authoring a real environment. |

!!! note "Where the object model lives"
    These pages link to [Concepts and APIs](../concepts/index.md) rather than
    restating field tables. When you need exact field names and allowed values,
    follow the link to the owning concept page — for example
    [Machines](../concepts/machines.md) or
    [Infrastructure](../concepts/infrastructure.md). When something fails, see
    [Troubleshooting](../troubleshooting.md).

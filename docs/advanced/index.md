---
title: Advanced
description: Advanced Bootwright usage, examples, and operational topics.
---

# Advanced

Use this section after the [first single-node apply path](../getting-started.md)
is clear. These pages cover provider-specific configuration, networking,
disconnected installs, add-ons, storage, secrets, and operational recovery —
whether you are provisioning a whole cloud platform or converging a selected
platform component for build-out, recovery, or maintenance.

| Topic | Use it for |
| --- | --- |
| [Providers](providers.md) | Bare metal, libvirt, vSphere, KubeVirt, machine profiles, and shared services. |
| [Networking and Load Balancing](networking.md) | NMState templates, provider attachments, endpoints, VIPs, DNS, and NTP. |
| [Proxy and Disconnected Installs](proxy-and-disconnected.md) | Runtime proxy selection, mirror inputs, trust, and artifact access. |
| [Post-Install Add-ons](post-install-addons.md) | OLM add-ons, manifest sets, profiles, bindings, readiness, and input effects. |
| [Ceph Storage Clusters](storage-ceph.md) | Managed and imported Ceph, pools, CephFS, RGW, stretch mode, and access. |
| [Secrets](secrets.md) | Secret declaration, context-local storage, generated material, and host trust. |
| [Operations and Recovery](operations.md) | Destroy stages, `destroyProtection`, `--override`, and focused `--stage`/`--clusters` recovery. |
| [Reference Examples](examples.md) | Choosing the right complete example tree before authoring a real environment. |

!!! note "Where the execution model lives"
    For the internal render pipeline, execution identities, locking, and the
    convergence classifier in depth, see
    [Architecture](../concepts/architecture.md). The user-facing apply, stage,
    and platform model is in [Concepts](../concepts.md).

For exact field names and allowed values, use the
[API Reference](../api/index.md). For normative rules, use
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).
When something fails, see [Troubleshooting](../troubleshooting.md).

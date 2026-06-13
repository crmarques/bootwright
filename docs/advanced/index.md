---
title: Advanced
description: Advanced Bootwright usage, examples, and operational topics.
---

# Advanced

Use this section after the first single-node apply path is clear. These pages
cover provider-specific configuration, larger example trees, disconnected
installs, add-ons, storage, and operational recovery for whole cloud platforms
or selected platform components.

| Topic | Use it for |
| --- | --- |
| [Advanced Examples](examples.md) | Choosing the right complete example tree before authoring a real environment. |
| [Providers](providers.md) | Bare metal, libvirt, vSphere, KubeVirt, machine profiles, and shared services. |
| [Networking And Load Balancing](networking.md) | NMState templates, provider attachments, endpoints, VIPs, DNS, and NTP. |
| [Proxy And Disconnected Installs](proxy-and-disconnected.md) | Runtime proxy selection, mirror inputs, trust, and artifact access. |
| [Post-Install Add-Ons](post-install-addons.md) | OLM add-ons, manifest sets, profiles, bindings, readiness, and input effects. |
| [Ceph Storage Clusters](storage-ceph.md) | Managed and imported Ceph, pools, CephFS, RGW, stretch mode, and access. |
| [Secrets](secrets.md) | Secret declaration, context-local storage, generated material, and host trust. |
| [Architecture](architecture.md) | The internal pipeline and contributor-oriented execution boundaries. |

For exact field names and allowed values, use the
[API Reference](../api/index.md). For normative rules, use
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).

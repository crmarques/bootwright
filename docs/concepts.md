---
title: Concepts
description: How Bootwright distributes installer input across desired-state objects.
---

# Concepts

Bootwright keeps installer-compatible fields close to the object that owns the
operational fact. The renderer then merges those objects into
`install-config.yaml`, `agent-config.yaml`, and provider variables.

Authored desired-state YAML uses block-style mappings in examples, e2e inputs,
fixtures, and scaffold output. Keep each object field on its own indented line
instead of compact inline maps.

## Object Ownership

| Kind | Ownership boundary |
| --- | --- |
| `Environment` | Fleet-wide defaults, selected resource files, secret sources, proxy defaults, mirrors, component images |
| `Host` | SSH access to a machine that can run substrate or service actions |
| `InfraProvider` | Capability inventory: bare-metal machines, virtual machine profiles, service implementations |
| `NetworkConfig` | Reusable machine-network CIDRs and NMState templates |
| `ClusterInfra` | Platform render mode, endpoints, selected machines, and managed infra components |
| `ContainerCluster` | Distribution, release, install mode, cluster networking, pools, and node bindings |

## Reference Flow

```text
ContainerCluster.nodes[*].machineRef
  -> ClusterInfra.components.machines[*]
  -> InfraProvider machine, profile, or service capability
  -> Host

ClusterInfra machines
  -> NetworkConfig
```

`ContainerCluster` has no top-level infrastructure pointer. Each node selects
the exact cluster infrastructure machine that backs it. In v1 all nodes in one
cluster must reference the same `ClusterInfra`.

## NMState Templates

`NetworkConfig` carries two installer-facing pieces:

- `machineNetwork[]` renders to `install-config.yaml` when selected by a
  machine.
- `template.networkConfig` renders to each agent host after overlays.

Most hosts reuse the same NMState template and only override addresses in
`ClusterInfra.components.machines[].networkConfig.addresses[]`. Advanced hosts
may provide a full machine-level `networkConfig` override.

Provider MAC inventory, or deterministic generated MACs for Bootwright-created
virtual machines, is merged into `agent-config.yaml hosts[].interfaces[]` and
matching NMState interfaces.

Endpoint VIP ownership stays on `ClusterInfra.spec.endpoints`. Effective VIPs
must land inside one selected machine-network CIDR.

## Distribution And Release

`ContainerCluster.spec.distribution` supports:

- `type: openshift`, with exact OCP version, optional channel, or explicit
  release image
- `type: okd`, preferably with an explicit OKD release image

OpenShift channel derivation is only applied to OpenShift exact versions.
OKD does not require a Red Hat pull secret by default.

## Platform Mode

`ClusterInfra.spec.platform.type` decides installer platform rendering:

- `baremetal`
- `vsphere`
- `none`
- `external`

Bare-metal provisioning network values are lowercase: `disabled`, `managed`,
or `unmanaged`. `disabled` uses the existing machine network, which is the
normal Redfish virtual-media agent-install mode.

For single-node clusters, Bootwright renders installer `platform.none` unless
`platform.type: external` is selected. The authored `ClusterInfra` still owns
the selected machines, endpoints, and managed components.

## Managed Components

`ClusterInfra.spec.components` still supports non-machine infra alongside
machines:

- `loadBalancers[]`
- `proxy`
- `nameResolution`
- `registry`

Those components resolve through `InfraProvider.spec`, just like machines do.

Generated artifact publication is derived from install requirements and uses
`InfraProvider.spec.artifactPublishers[]`. Publisher routes reference neutral
named addresses on the publisher Host; `http.port` defaults to `8443`.

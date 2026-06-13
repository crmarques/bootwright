---
title: ContainerCluster API
description: ContainerCluster install intent, distribution, platform, endpoints, networking, and hosts.
---

# ContainerCluster

`ContainerCluster` owns OpenShift or OKD install intent. It selects machines
through `spec.hosts[].machineRef`; substrate details remain on `Machine` and
`InfraProvider`.

## Fields

| Field | Required | Description |
| --- | --- | --- |
| `spec.distribution` | No | OpenShift or OKD release selection. |
| `spec.install` | No | Install method, mode, platform, endpoints, artifacts, trust, and node SSH. |
| `spec.controlPlane.replicas` | No | Control-plane replica count cross-checked against host roles. |
| `spec.compute[].replicas` | No | Worker replica counts cross-checked against host roles. |
| `spec.networking` | No | Cluster and service networks. |
| `spec.hosts[]` | Yes | Node-to-machine bindings for the agent install. |

## Distribution

| Field | Description |
| --- | --- |
| `distribution.type` | `openshift` or `okd`; empty defaults to OpenShift. |
| `distribution.release.version` | Exact OpenShift version. |
| `distribution.release.channel` | Optional OpenShift channel. |
| `distribution.release.image` | Explicit release image, commonly used for OKD or pinned installs. |

OpenShift requires a pull secret unless an environment default supplies it. OKD
can omit a Red Hat pull secret unless a private release or mirror requires
credentials.

## Install

| Field | Description |
| --- | --- |
| `install.method` | Currently `agent`; empty defaults to `agent`. |
| `install.mode` | `connected` or `disconnected`; empty defaults to `connected`. |
| `install.platform` | Installer platform render mode. |
| `install.endpoints` | Closed map with `api`, `api-int`, and `ingress` keys. |
| `install.artifactAccess` | Artifact endpoint selectors for Redfish, machine boot, or disconnected install. |
| `install.pullSecretRef` | Pull secret name. |
| `install.nodeSSH.keyPairRef` | Secret containing node SSH private and public material. |
| `install.nodeSSH.publicKeyRef` | Secret containing public key material. |
| `install.nodeSSH.privateKeyRef` | Secret containing private key material for local probes. |
| `install.additionalTrustBundleRefs[]` | Cluster-scoped install CA bundles. |
| `install.servingCertificates.apiServer.namedCertificates[]` | API serving named certificates. |
| `install.servingCertificates.ingress.defaultCertificateRef` | Ingress default certificate secret. |

Disconnected installs require environment mirror trust and either an external
mirror URL or a managed registry entry.

## Platform

| Field | Description |
| --- | --- |
| `install.platform.type` | `baremetal`, `vsphere`, `none`, or `external`. |
| `install.platform.baremetal.provisioningNetwork` | `disabled`, `managed`, or `unmanaged`. |
| `install.platform.vsphere.nodeNetworking` | vSphere node networking. |
| `install.platform.external` | External platform passthrough map. |

Single-node clusters render `platform.none` unless `external` is explicitly
selected.

## Endpoints

Endpoint keys are limited to `api`, `api-int`, and `ingress`.

| Field | Description |
| --- | --- |
| `endpoints.<slot>.address` | Literal endpoint address. |
| `endpoints.<slot>.dnsName` | Optional DNS name. |
| `endpoints.<slot>.port` | Optional port. |
| `endpoints.<slot>.scheme` | Optional scheme. |
| `endpoints.<slot>.prefixLength` | Prefix length for VIP-style endpoints. |
| `endpoints.<slot>.interfaceNetworks[]` | Interface networks for VIP placement. |
| `endpoints.<slot>.source.type` | `openshift`, `external`, or `infraComponent`. |
| `endpoints.<slot>.source.componentRef` | Load-balancer `InfraComponent` for `infraComponent` source. |
| `endpoints.<slot>.source.bindAddressRef` | Bind address name on the load balancer. |

`openshift` and `external` sources require an address. `infraComponent` sources
resolve their address from the selected load balancer component.

## Networking

| Field | Description |
| --- | --- |
| `networking.networkType` | OpenShift network type. |
| `networking.clusterNetwork[].cidr` | Pod network CIDR. |
| `networking.clusterNetwork[].hostPrefix` | Pod network host prefix. |
| `networking.serviceNetwork[]` | Service network CIDRs. |

When omitted, stock OpenShift service networking defaults are normalized.

## Hosts

| Field | Required | Description |
| --- | --- | --- |
| `hosts[].hostname` | Yes | Node hostname inside the cluster. |
| `hosts[].role` | Yes | `master` or `worker`. |
| `hosts[].machineRef` | Yes | Machine that backs this node. |

Referenced machines must be selected, carry `openshift-node`, set
`os.provided: false`, and be node-bound by at most one container or storage
cluster.

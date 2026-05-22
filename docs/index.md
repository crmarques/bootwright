---
title: Home
description: Desired-state orchestration for OpenShift and OKD cluster fleets.
hide:
  - navigation
  - toc
---

<p align="center">
  <img src="assets/images/logo-horizontal.png"
       alt="Bootwright"
       width="520">
</p>

Bootwright provisions fleets of OpenShift and OKD clusters from declarative
desired state. You describe the environment, provider hosts, substrate
inventory, network templates, cluster infrastructure, and cluster install
intent; Bootwright validates that input, renders deterministic installer
files, and converges the provisioning phases idempotently.

[Get Started](getting-started.md){ .md-button .md-button--primary }
[Learn the Concepts](concepts.md){ .md-button }

## Input Kinds

| Kind | Owns |
| --- | --- |
| `Environment` | Fleet defaults, selected resource files, secret sources, proxy defaults, mirrors, component images |
| `Host` | SSH reachability for provider and service hosts |
| `InfraProvider` | Substrate capabilities: machines, profiles, and service implementations |
| `NetworkConfig` | Installer machine networks and reusable NMState host templates |
| `ClusterInfra` | Platform mode, endpoints, selected machines, and managed infra components |
| `ContainerCluster` | OpenShift or OKD install intent and node-to-machine bindings |

The detailed schema contract lives in
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).

## Current Scope

Bootwright targets `openshift-install agent` installs for single-node and
multi-node clusters. The shipped apply workflows currently converge libvirt
with emulated Redfish BMCs and bare metal with Redfish virtual media. The
schema also models vSphere and OpenShift Virtualization so their adapters can
land behind the same desired-state model.

Day-2 GitOps publication of fleet content belongs to a separate project.

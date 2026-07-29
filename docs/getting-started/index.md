---
title: Getting Started
description: Stand up a working bootwright lab on a single Linux libvirt host, from an empty bastion to a running OpenShift or Ceph cluster.
---

# Getting Started

Bootwright is a desired-state orchestrator for turning cloud platform intent into
reality. You describe substrates, machines, networks, shared services, and
clusters as declarative YAML; bootwright validates that intent, renders the
inputs the official installer and CLIs need, and converges it all idempotently.
It provisions OpenShift, OKD, and Ceph clusters over bare metal, vSphere,
KubeVirt, and libvirt, and binds them into one platform.

This section gets you from nothing to a running cluster on **a single Linux host
with libvirt** — no real server hardware required. libvirt also emulates the
Redfish BMC that boots each node, so you exercise the same boot, power, and
install flow you would use against bare metal. These are the smallest
self-contained labs and the best place to learn the model before you grow into
fleets, disconnected installs, KubeVirt child clusters, and post-install add-ons.

## The recommended order

1. **[Installation and Setup](installation.md)** — install the CLI and learn the
   shared setup mechanics (contexts, secrets, host trust, bastion prep) that both
   cluster guides reuse. Start here.
2. Then pick the cluster you want to build:
    - **[Provisioning an OpenShift cluster](openshift.md)** — a single-node
      OpenShift cluster scaffolded with `bootwright example init`. OpenShift
      installs onto the node for you.
    - **[Provisioning a Ceph cluster](ceph.md)** — a 3-node IBM Storage Ceph
      cluster (two full nodes plus a monitor-only tie-breaker) where bootwright
      also installs the node operating system (managed OS).

Either guide assumes you have completed Installation and Setup first.

## What you'll need

A Linux host with `sudo`, libvirt (`qemu:///system`), `openssh-clients`, an SSH
key pair for the bastion connection the lab trees declare
(`bastion-host-ssh`), and a free machine network with room for the node IPs
and a couple of virtual IPs. The OpenShift lab additionally needs an OpenShift
pull secret; the Ceph lab additionally needs a RHEL DVD ISO, a Red Hat
subscription, and an IBM Storage Ceph entitlement. Bootwright desired state is
safe to commit: it names secrets but never stores their bytes, so keep pull
secrets, private keys, kubeconfigs, tokens, and passwords out of the YAML — you
load them into the local bootwright context separately. The per-guide pages call
out exactly what each lab needs.

## Going further

- Read **[The desired-state model](../concepts/index.md)** to understand
  contexts, stages, the apply modes, and object ownership before you change the
  layout.
- Browse **[Advanced Scenarios](../advanced/index.md)** for fleets, disconnected
  and proxied installs, managed-OS installs, Ceph topologies, KubeVirt, and
  larger reference trees.

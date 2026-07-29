---
title: Home
description: Objective, scope, and documentation map for Bootwright.
---

<p align="center">
  <img src="assets/images/logo-horizontal.png"
       alt="Bootwright"
       width="520">
</p>

Bootwright is a desired-state orchestrator for turning cloud platform intent
into reality. You describe the platform you want — substrates, machines, managed
OS installs, networks, shared services, clusters, storage, and bootstrap add-ons
— as declarative YAML. Bootwright validates that intent, renders the inputs the
official tools expect (`openshift-install`, `cephadm`, `oc`, and your providers),
and converges the dependency graph idempotently until reality matches what you
declared.

<p align="center">
  <img src="assets/images/high-level-overview.png"
       alt="Bootwright"
       width="900">
</p>

The same desired state works at two scales. You can stand up a **whole cloud
platform from scratch**, or converge a **single isolated component** for
build-out, recovery, or maintenance. Either way the YAML is the source of truth,
the convergence is repeatable, and re-running it is safe.

[Get Started](getting-started/index.md){ .md-button .md-button--primary }
[Concepts & APIs](concepts/index.md){ .md-button }

## What Bootwright is for

Standing up one cluster is a runbook. Standing up a cloud platform is a
coordination problem: machines may need substrate preparation or an OS install,
clusters need shared services such as DNS, load balancers, mirror registries,
proxies, and artifact servers, storage may be imported or built as Ceph, and
early add-ons must wait for installed clusters and exported storage. Hand-written
scripts and ad-hoc installer runs do not keep those relationships consistent.

Bootwright replaces them with one model and a single idempotent apply graph:

1. **Author** safe-to-commit YAML as the single source of truth. It names
   secrets but never carries secret bytes.
2. **Validate** structure, ownership, and cross-references offline, before any
   host is touched.
3. **Render** deterministic, secret-free inputs for the official installer,
   provider, storage, and cluster tools.
4. **Apply** the dependency graph from a bastion host — creating what is
   missing, skipping what already matches, converging drift that reconciles in
   place, and failing closed on structural (destructive-identity) drift or
   foreign ownership before mutating anything.

Desired state is the product API. The installer files and inventories Bootwright
generates are outputs, never the source you edit.

## Scope

Bootwright covers the platform end to end: substrate and service preparation,
machine OS installs, OpenShift or OKD cluster installs, managed or imported Ceph
storage, storage exports for downstream consumers, and the early add-ons bound
into installed clusters.

It deliberately stops at provisioning. Day-2 GitOps publication of fleet content
is a separate project and is out of scope here. Container-cluster installs today
run strictly as direct `openshift-install agent` installs (HyperShift is a future
model). There are no backward-compatibility shims: `v1alpha1` may change cleanly,
and stale desired state is expected to fail strict validation rather than be
silently migrated.

## Supported matrix

Bootwright supports three cluster families — **OpenShift**, **OKD**, and
**Ceph** — across the substrates below. Apply support is what is converged today;
the schema may accept facts for substrates whose adapter has not yet landed, so
treat the table as authoritative for what actually runs.

| Substrate | Apply support | Notes |
| --- | --- | --- |
| libvirt (emulated/sushy Redfish BMC) | OpenShift / OKD / Ceph | Full apply coverage; no real hardware needed. |
| Bare metal (Redfish virtual media) | OpenShift / OKD / Ceph | Real BMCs over Redfish virtual media. |
| vSphere (vCenter-managed VMs) | OpenShift / OKD | |
| KubeVirt (OpenShift Virtualization VMs) | OpenShift / OKD | Nested child clusters on a Bootwright-managed parent. |

Ceph is a **storage** cluster family, not a substrate of its own: managed Ceph
converges through cephadm on OS-ready or Bootwright-installed (managed-OS) nodes,
and imported external Ceph is also supported. Those storage clusters run on the
libvirt and bare-metal Redfish substrates above rather than in a row of their own.
IPMI is not apply-supported: `InfraProvider.spec.type` accepts only `baremetal`,
`libvirt`, `vsphere`, and `kubevirt`, and a machine BMC must speak `redfish`.

KubeVirt nested clusters need an installed parent cluster that advertises the
`kubevirt` add-on. Managed Ceph runs single-site by default; the one multi-site
topology supported today is stretch mode — exactly two data sites plus one
tiebreaker, with replication fixed at `size: 4` / `minSize: 2`.

## Documentation map

| Section | Use it for |
| --- | --- |
| [Getting Started](getting-started/index.md) | Install the CLI, then two guided labs on one libvirt host: a single-node OpenShift cluster and a 3-node managed Ceph cluster. |
| [Concepts and APIs](concepts/index.md) | The object model, references, contexts, stages, secrets, storage, add-ons, and the field-level options for every authored `bootwright.io/v1alpha1` kind. |
| [Advanced Scenarios](advanced/index.md) | Provider-specific setup, networking, disconnected installs, managed OS, Ceph topologies, recovery, and larger examples. |
| [Troubleshooting](troubleshooting.md) | Common validation, apply, SSH, artifact, and orphan-state failures. |
| [Contributing](contributing/index.md) | For contributors changing Bootwright's code: the API extension points and the execution architecture. |

The source-of-truth contracts live in
[`/specs`](https://github.com/crmarques/bootwright/tree/main/specs). These docs
teach workflows and explain common choices; the specs remain the normative
contract for API shape, validation, CLI behavior, and security boundaries.

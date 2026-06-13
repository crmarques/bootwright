---
title: Home
description: Objective, scope, and documentation map for Bootwright.
---

# Bootwright

Bootwright provisions OpenShift and OKD clusters from declarative desired-state
YAML. You describe the fleet once - environment defaults, machines, providers,
networking, shared services, clusters, storage, and bootstrap add-ons - and
Bootwright validates that intent, renders the inputs expected by official tools,
and converges the workflow idempotently from the bastion host.

[Get Started](getting-started.md){ .md-button .md-button--primary }
[API Reference](api/index.md){ .md-button }

## Objective

Bootwright is for operators who need repeatable cluster provisioning across
bare-metal and virtualized environments without turning each cluster into a
hand-maintained installer runbook.

The objective is simple:

1. Author safe-to-commit YAML as the source of truth.
2. Validate ownership, references, and install requirements before mutation.
3. Render deterministic `openshift-install`, provider, storage, and add-on
   inputs.
4. Apply the graph repeatedly so matching resources are skipped, missing
   resources are created, and unsafe drift fails closed.

## Scope

Bootwright provisions fleets from substrate preparation through installed
clusters and early cluster-bound bootstrap components. Current apply support
covers OpenShift and OKD agent installs on libvirt with emulated Redfish, real
bare metal with Redfish virtual media, vSphere VMs, KubeVirt-hosted child VMs,
and Ceph storage through cephadm.

Bootwright does not own long-term day-2 GitOps publication. It can install early
platform add-ons and apply initial manifests, but ongoing fleet content
reconciliation belongs outside this repository.

## Normal Workflow

```text
bootwright example init my-sno-lab --output ./my-sno-lab
bootwright validate -f ./my-sno-lab
bootwright context init lab -f ./my-sno-lab
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret sync
bootwright host trust
bootwright bastion setup --yes
bootwright preflight all
bootwright render effective
bootwright plan
bootwright apply --yes
bootwright status --watch
bootwright cluster access
```

`bootwright status` is the operational guide rail. Run it after each setup step
to see the current context state and the next command Bootwright recommends.

## Documentation Map

| Section | Use it for |
| --- | --- |
| [Getting Started](getting-started.md) | A complete, guided single-node example from scaffold to cluster access. |
| [Concepts](concepts.md) | The object model, references, contexts, stages, secrets, storage, and add-ons. |
| [Advanced](advanced/index.md) | Provider-specific setup, networking, disconnected installs, storage, recovery, and larger examples. |
| [API Reference](api/index.md) | Field-level options for every authored `bootwright.io/v1alpha1` kind. |
| [Troubleshooting](troubleshooting.md) | Common validation, apply, SSH, artifact, and orphan-state failures. |

The source-of-truth contracts live in
[`/specs`](https://github.com/crmarques/bootwright/tree/main/specs). These docs
teach workflows and explain common choices; the specs remain the normative
contract for API shape, validation, CLI behavior, and security boundaries.

---
title: Architecture
description: How Bootwright turns desired state into installer and provider input.
---

# Architecture

Bootwright runs a simple pipeline:

```text
load YAML -> normalize -> validate -> render -> apply/status
```

The render step merges the six kinds into concrete outputs:

- `install-config.yaml` from `ContainerCluster`, `Environment`,
  `NetworkConfig`, and `ClusterInfra.platform`
- `agent-config.yaml` from `ContainerCluster.nodes`,
  `ClusterInfra.components.machines`, `NetworkConfig` templates, and provider
  MAC inventory
- provider variables from `InfraProvider`, `Host`, and
  `ClusterInfra.components`

Shared provider services are resolved once as a service graph. Validation,
rendering, status, and scoped apply checks use the same service identities and
consumer list, so a partial `apply infra --scope` cannot silently narrow a
service another cluster still depends on.

The important boundary is ownership. Physical machine facts do not move into
cluster intent, and cluster release intent does not move into environment
defaults. That keeps provider swaps and release changes explicit.

## Apply Workflow

`apply bastion` prepares tools on the Environment-selected bastion Host. Scoped
apply targets then run through the rendered Ansible bundle:

- `apply infra` converges provider hosts, substrate state, and managed infra
  components.
- `apply cluster` creates the agent ISO, boots each declared node as its own
  task, and then waits for `openshift-install agent wait-for install-complete`.
- `apply all` runs infrastructure and cluster phases in one target.

Every apply writes a current run ledger under the context state directory.
`bootwright status` reads that ledger without contacting provider hosts, BMCs,
or clusters, and `bootwright status --watch` follows it until the run reaches a
terminal state.

When one cluster is selected, Bootwright streams raw Ansible output to the
terminal and keeps the same output in per-task logs. When multiple clusters are
selected, Bootwright runs independent cluster DAG tasks concurrently where
resource locks allow it, prints one install log path per cluster, and keeps the
terminal focused on high-level apply progress.

## External CLI Inputs

`bootwright render --output-dir <dir> --scope <cluster> --sensitive` writes the
same concrete tool inputs Bootwright would hand to external tools. OpenShift
installer files land under
`<dir>/openshift-install/<cluster>/{install,agent}-config.yaml`; Ansible
inventory and vars files are written beside the effective state and lock.
Because the installer files contain secret material, the command requires
`--sensitive`.

See [`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md)
for the contributor contract.

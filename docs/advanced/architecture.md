---
title: Architecture
description: How Bootwright turns desired state into installer and provider input.
---

# Architecture

Bootwright orchestrates external tool-driven provisioning through a simple
pipeline:

```text
load YAML -> normalize -> validate -> render -> apply/status
```

The render step merges the provisioning kinds into concrete outputs:

- `install-config.yaml` from `ContainerCluster`, `Environment`,
  `NetworkConfig`, and `ClusterInfra.platform`
- `agent-config.yaml` from `ContainerCluster.nodes`,
  `ClusterInfra.components.machines`, `NetworkConfig` templates, and provider
  MAC inventory
- provider variables from `InfraProvider`, `InfraComponent`, `Host`, and
  `ClusterInfra.components`
- storage inputs from `StorageCluster`, storage pools, CephFS, RGW, exports,
  and Data Foundation attachments

Shared provider services are resolved once as a service graph. Validation,
rendering, status, and scoped apply checks use the same service identities and
consumer list, so a partial `apply infra --scope` cannot silently narrow a
service another cluster still depends on.

Bootwright uses distinct execution identities instead of treating the process
as simply root or non-root:

- user-authored files, external secret sources, `~` expansion, and caller PATH
  discovery run as the original local caller
- context state, generated secrets, runtime installer files, workflow logs,
  managed Ansible runtime, and local package or CLI installs run as the
  protected local root state identity
- provider and infrastructure host work connects as the rendered SSH user, then
  uses Ansible `become` for privileged host mutation
- controller-local Ansible work uses localhost inventory and `become` only for
  the tasks that intentionally mutate controller state

The important boundary is ownership. Physical machine facts do not move into
cluster intent, and cluster release intent does not move into environment
defaults. That keeps provider swaps and release changes explicit.

## Apply Workflow

`apply bastion` prepares bastion-local tools on localhost. `apply all` is the
normal convergence target after `check all` and a dry run. Phase targets remain
available for advanced operations and recovery:

- `apply infra` converges provider hosts, substrate state, and managed infra
  components.
- `apply storage-cluster` provisions external storage clusters from rendered cephadm
  and Ceph operation files through the Ansible storage layer.
- `apply clusters` creates the agent ISO, boots each declared node as its own
  task, waits for `openshift-install agent wait-for install-complete`, and then
  applies bound add-ons and declared integrations.
- `apply addons` applies declarative post-install bootstrap components to
  already installed clusters with `oc`.
- `apply all` runs infrastructure, storage, cluster install, and add-ons in
  one target.

Every apply writes a current run ledger under the context state directory.
`bootwright status` reads that ledger without contacting provider hosts, BMCs,
or clusters, and `bootwright status --watch` follows it until the run reaches a
terminal state.

Bootwright runs independent cluster DAG tasks concurrently where resource locks
allow it. Apply output is a ledger-backed fleet dashboard with run and cluster
log paths, per-cluster phase status, running work, and concise failures. Native
Ansible, `oc`, SSH, SCP, Ceph, and installer process output stays in
root-managed run, task, and cluster logs instead of streaming through the
terminal.

Post-install bootstrap components are planned as direct `oc` tasks after the
cluster install wait task when `apply clusters` or `apply all` is selected, or
without install dependencies when `apply addons` is selected for an already
installed cluster.
Storage attachment tasks are planned in the same add-ons phase and wait for the
selected Data Foundation add-on readiness before applying generated
external-mode manifests.

## External CLI Inputs

`bootwright render --output-dir <dir> --scope <cluster> --sensitive` writes the
same concrete tool inputs Bootwright would hand to supplier or community CLIs.
OpenShift installer files land under
`<dir>/openshift-install/<cluster>/{install,agent}-config.yaml`; Ansible
inventory and vars files are written beside the effective state and lock.
Storage files land under `<dir>/storage/<storageCluster>/`. Because installer
files contain secret material, the command requires `--sensitive`.

See [`specs/architecture.md`](https://github.com/crmarques/bootwright/blob/main/specs/architecture.md)
for the contributor contract.

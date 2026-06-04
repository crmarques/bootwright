# ADR 0002: Ansible Collection Layout and Provider Dispatch

## Status

Accepted

## Context

The desired-state API models every substrate node as a `Machine`.
Provider-specific details live under the machine substrate arm and provider
profile data. The render layer compiles those discriminators into
per-component Ansible vars, and the orchestration layer acts on them.

The initial Ansible layout grew from the libvirt + emulated-BMC lab. The
top-level role buckets mixed domain layers, implementation families, and
generic helpers. Preparing the bundle as an Ansible collection is also a
better fit for fully qualified playbook and role names.

## Decision

The embedded Ansible bundle owns one local collection, `bootwright.core`:

```text
ansible/collections/ansible_collections/bootwright/core/
  playbooks/
    workflow_*.yml        public CLI target wrappers
    task_*.yml            focused task playbooks
    check_*.yml           read-only checks
    tasks/                reusable playbook task files
  roles/
    controller_*          controller-local setup
    machine_base          base packages for service-bearing machines
    machine_proxy         proxy facts and persisted proxy settings
    machine_setup_*       substrate setup on provider machines
    helper_*              context, credential, and cleanup helpers
    provider_service_*    provider services, including BMC services
    infra_component_*     machine-bound InfraComponent services
    machine_substrate_*   per-cluster substrate state
    cluster_network_*     per-cluster networking state
    container_cluster_*   agent install, boot, media, wait, and destroy
    storage_cluster_*     external storage convergence
    check_* / diagnostic_* validation and diagnostics
  plugins/filter/         collection-scoped filters
  docs/                   variable contracts
```

Workflow playbooks are thin wrappers. `workflow_infra_apply.yml` imports the
external reachability check, provider-service task playbook,
InfraComponent-service task playbook, and machine-infra task playbook.
Destroy runs machine infrastructure before InfraComponent services and
provider services. `workflow_clusters_apply.yml` imports preflight, machine
infrastructure, and container-cluster install. `workflow_container_cluster_apply.yml`
remains the focused container-cluster wrapper.

Role dispatch is computed by the Go driver registry and projected as exact
role names in the rendered vars. Diagnostic labels stay on each machine, but
playbooks do not construct role names from those labels:

- `component.substrateApplyRole` and `component.substrateDestroyRole`
  select `bootwright.core.machine_substrate_*` roles.
- Provider/BMC services consume `bootwright_provider_services[]`, where each
  service carries `applyRole` and `destroyRole`.
- Managed InfraComponent services consume
  `bootwright_infra_component_services[]`, where each service carries
  `applyRole` and `destroyRole`.
- `component.bootApplyRole` selects the boot driver invoked during OpenShift
  install from `bootwright.core.container_cluster_boot_*`.
- `component.mediaPrepareRole`, when set, selects an optional virtual-media
  backend hook. `bootwright.core.container_cluster_boot_redfish` remains the
  Redfish protocol role for both real BMCs and sushy-emulator.
- Generated artifact publication resolves to the artifact server selected by
  `ContainerCluster.spec.install.artifactAccess.serverRef`. For managed
  servers, the selected `InfraComponent` `machineRef` gates the rendered
  artifact service and limits it to that machine. The component declares
  listeners, endpoints, and optional bind address. Bare-metal Redfish machines
  and disconnected agent installs bind BMC-specific and cluster-install
  endpoints through `ContainerCluster.spec.install.artifactAccess`.

`bmcRole` and `bootRole` are independent. BMC-driven substrates use a
matched pair because the boot path runs through the BMC service
(`libvirt`/`emulated`, `baremetal`/`redfish`).
Substrates whose native lifecycle API is the boot surface — KubeVirt VMs
driven via Kubernetes API, vSphere VMs driven via vCenter — set
`bmcRole: none` (nothing to install on a provider host) and a non-empty
`bootRole` matching the substrate. This avoids the conflated case where
"no BMC service" was forced to mean "no boot driver", which silently
no-op'd the OCP boot step.

The kind-to-role mapping lives in one Go registry (`internal/infra/support`), with
helpers used by the renderer, CLI support checks, scaffold messaging, and repo
guardrail tests. Every kind resolves to a real role where a role is meaningful;
both `bootwright.core.provider_service_bmc_none` and
`bootwright.core.container_cluster_boot_none` exist so no-op dispatch remains
explicit.

The projected variable blocks described in
[`vars-contract.md`](../../ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md) carry the
substrate-specific inputs each role consumes. Roles do NOT branch on
`substrateRole` / `bmcRole` / `bootRole` to vary behavior within themselves —
that branching belongs in the registry and renderer. The Redfish boot role
reads `boot.redfish` and `boot.agentIso` whether the BMC is sushy-emulator or
a vendor BMC; the libvirt media role consumes the libvirt-specific
virtual-media cleanup block; the emulated BMC service role reads provider BMC
service `bmcEmulated.*` for libvirt URI, ports, bind address, and auth instead
of re-deriving defaults. Adding a substrate-dependent fact is a renderer
change, not a role conditional.

Task playbooks own the host-local selection of
`bootwright_current_provider`, `bootwright_current_cluster`, and related
runtime views. A future context-role extraction is allowed only if it preserves
those facts and does not move provider or install decisions out of the renderer.

## Consequences

- Public CLI commands stay stable: `bootwright check/apply bastion|infra|cluster|all`.
- Adding or removing apply support for a provider remains close to a role
  operation: add or remove the relevant collection role and one registry entry,
  then update tests. Public schema support still requires the typed API arm,
  validation, rendering, docs, and specs.
- The embedded bundle passes a collection path to Ansible instead of multiple
  role search paths and an out-of-band filter plugin path.
- Playbooks become easier to scan because workflows are expressed by collection
  workflow/task imports and role calls, while resource details live in owning
  roles.

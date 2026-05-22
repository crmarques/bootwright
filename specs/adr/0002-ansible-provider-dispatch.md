# ADR 0002: Layered Ansible Bundle and Provider Dispatch

## Status

Accepted

## Context

[ADR 0001](0001-capability-map-and-components.md) fixes the
desired-state API as six kinds with per-entry provisioner arms
(`InfraProvider.spec.machineProfiles[*].libvirt`,
`InfraProvider.spec.machineProfiles[*].vsphere`,
`InfraProvider.spec.machineProfiles[*].kubevirt`, and
`machines[*].baremetal`).
The render layer compiles those discriminators into per-component
Ansible vars, and the orchestration layer acts on them.

The initial Ansible layout grew from the libvirt + emulated-BMC lab.
A flat `roles/` directory made new-reader navigation harder because
role names had to carry both layer and concern, while playbooks also
held repeated context selection and some resource teardown logic.

## Decision

The Ansible bundle is organized by Bootwright layers:

```text
ansible/playbooks/
  targets/       public CLI target wrappers
  layers/        executable layer workflows
  checks/        read-only Ansible checks
ansible/roles/
  bastion/       controller-local setup
  shared/        context and host helper roles
  providers/     provider-scoped shared services
  cluster_infra/ per-cluster substrate and network state
  openshift/     openshift-install agent workflows
```

Target playbooks are thin wrappers. `targets/infra/apply.yml` imports
`layers/providers/apply.yml` and then `layers/cluster_infra/apply.yml`;
destroy runs cluster infrastructure before provider-scoped services.
`targets/clusters/apply.yml` imports the OpenShift install layer.

Role dispatch is computed by the Go driver registry and projected as exact
role names in the rendered vars. Diagnostic labels stay on each machine, but
playbooks do not construct role names from those labels:

- `component.substrateApplyRole` and `component.substrateDestroyRole`
  select per-machine substrate roles from `roles/cluster_infra/`.
- Provider services consume `bootwright_provider_services[]`, where each
  service carries `applyRole` and `destroyRole`. Managed services and BMC
  services use the same dispatcher.
- `component.bootApplyRole` selects the boot driver invoked during OpenShift
  install from `roles/openshift/`.
- `component.mediaPrepareRole`, when set, selects an optional virtual-media
  backend hook. `boot_redfish` remains the Redfish protocol role for both
  real BMCs and sushy-emulator.
- Generated artifact publication resolves to the single
  `artifactPublishers[*]` entry declared by providers. Its
  `hostRef` gates the rendered HTTPS artifact service and limits it to that
  host. The provider capability may set the artifact publication port; the
  renderer derives the bind address from Bootwright defaults.
  Bare-metal Redfish machines and disconnected agent installs derive
  publication consumers automatically; BMC-specific and cluster-install
  routes come from `artifactPublishers[*].http.routes`.

`bmcRole` and `bootRole` are independent. BMC-driven substrates use a
matched pair because the boot path runs through the BMC service
(`libvirt`/`emulated`, `baremetal`/`redfish`).
Substrates whose native lifecycle API is the boot surface — KubeVirt VMs
driven via Kubernetes API, vSphere VMs driven via vCenter — set
`bmcRole: none` (nothing to install on a provider host) and a non-empty
`bootRole` matching the substrate. This avoids the conflated case where
"no BMC service" was forced to mean "no boot driver", which silently
no-op'd the OCP boot step.

The kind-to-role mapping lives in one Go registry (`internal/support`), with
helpers used by the renderer, CLI support checks, scaffold messaging, and repo
guardrail tests. Every kind resolves to a real role where a role is meaningful;
both `bmc_none` and `boot_none` exist so no-op dispatch remains explicit.

The projected variable blocks described in
[`ansible/VARS_CONTRACT.md`](../../ansible/VARS_CONTRACT.md) carry the
substrate-specific inputs each role consumes. Roles do NOT branch on
`substrateRole` / `bmcRole` / `bootRole` to vary behavior within themselves —
that branching belongs in the registry and renderer. `boot_redfish` reads
`boot.redfish` and `boot.agentIso` whether the BMC is sushy-emulator or a
vendor BMC; `media_libvirt` consumes the libvirt-specific virtual-media
cleanup block; `bmc_emulated` reads provider-service `bmcEmulated.*` for
libvirt URI / port / vmedia / bind / auth instead of re-deriving defaults;
`network_vips` reads each frontend's `attachment` block instead of re-querying
the cluster's network substrate attachment. Adding a substrate-dependent fact
is a renderer change, not a role conditional.

Layer playbooks own the host-local selection of
`bootwright_current_provider`, `bootwright_current_cluster`, and related
runtime views. A future context-role extraction is allowed only if it preserves
those facts and does not move provider or install decisions out of the renderer.

Provider roles own provider-scoped apply and destroy logic. Cluster
infrastructure roles own per-cluster substrate and network state. OpenShift
roles own installer execution and BMC boot handoff only.

## Consequences

- Public CLI commands stay stable: `bootwright check/apply bastion|infra|cluster|all`.
- Adding or removing apply support for a provider is intentionally close to a
  role-directory operation: add or remove the relevant Ansible role directory
  and one registry entry, then update tests. Public schema support still
  requires the typed API arm, validation, rendering, docs, and specs.
- The embedded bundle passes multiple role search paths to Ansible instead of
  one flat `roles/` directory.
- Playbooks become easier to scan because workflows are expressed by layer
  imports and role calls, while resource details live in owning roles.

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
    tasks/<domain>/       reusable playbook task files scoped by domain
  roles/                  one role per capability, named <family>_<detail>
  plugins/filter/         collection-scoped filters
  docs/                   variable contracts
```

Roles are grouped by a domain-layer family prefix rather than a flat list:
`machine_*`, `provider_host_*`, `provider_service_*`, `infra_component_*`,
`machine_substrate_*`, `cluster_network_*`, `container_cluster_*`,
`storage_cluster_*`, `machine_registration_*`, `controller_*`, `support_*`,
and read-only `check_*` / `diagnostic_*`, alongside singletons such as
`ownership_record`. A role's family names its domain layer; a `_*` suffix in
the dispatch names below marks a family the Go registry projects an exact role
name into (one kind, several substrate backends). This ADR does not enumerate
the roles on disk — that list changes with each provider and would rot here.
The current inventory is the collection's own `roles/` directory and the
per-role input contracts in
[`vars-contract.md`](../../ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md).

Workflow playbooks are thin wrappers: each is an ordered list of
`import_playbook` lines over the task playbooks of its stage, and the destroy
workflow inverts the apply order (machine infrastructure before InfraComponent
and provider services). The current import lists are the `workflow_*.yml` files
in the collection's `playbooks/` directory; this ADR does not enumerate them,
for the same reason it does not enumerate roles.

Task playbooks remain the stable Go dispatch surface. Reusable playbook task
fragments live under `playbooks/tasks/<domain>/`, while provider-specific
destructive host cleanup belongs to the owning provider-host role rather than
generic playbook task fragments. Check playbooks select hosts and import
check-specific roles; host and storage preflight logic lives in
`check_host_preflight` and `check_storage_preflight`.

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
  the active consumer's `artifactServerEndpoint.serverRef`, falling back to the
  default `Environment.spec.infraComponents.artifactServers[]` entry when
  omitted. For managed
  servers, the selected `InfraComponent` `machineRef` gates the rendered
  artifact service and limits it to that machine. The component declares
  listeners, endpoints, and optional bind address. Bare-metal Redfish machines
  and disconnected agent installs bind BMC-specific and cluster-install
  endpoints through `ContainerCluster.spec.install.agent.*.artifactServerEndpoint`.

`bmcRole` and `bootRole` are independent. BMC-driven substrates use a
matched pair because the boot path runs through the BMC service
(`libvirt`/`emulated`, `baremetal`/`redfish`).
Substrates whose native lifecycle API is the boot surface — KubeVirt VMs
driven via Kubernetes API, vSphere VMs driven via vCenter — set
`bmcRole: none` (nothing to install on a provider host) and a non-empty
`bootRole` matching the substrate. This avoids the conflated case where
"no BMC service" was forced to mean "no boot driver", which silently
no-op'd the OCP boot step.

The kind-to-role mapping lives in one Go registry (`internal/roles`), with
helpers used by the renderer, CLI support checks, scaffold messaging, and repo
guardrail tests. Every kind resolves to a real role where a role is meaningful;
both `bootwright.core.provider_service_bmc_none` and
`bootwright.core.container_cluster_boot_none` exist so no-op dispatch remains
explicit.

Role names are internal rendered contracts and may break cleanly within
`v1alpha1` when the architecture improves. Provider-host setup uses
`bootwright.core.provider_host_libvirt`, external Redfish BMC services use
`bootwright.core.provider_service_bmc_external_redfish`, and shared mechanics
use the `support_*` role family.

The registry projects role names only for the provider-discriminated families
where one kind has several substrate backends to choose between
(`machine_substrate_*`, `provider_service_bmc_*`, `container_cluster_boot_*`,
and the optional media hook). Single-implementation roles —
`machine_os_install_anaconda`, `storage_cluster_cephadm`,
`container_cluster_agent_install`, `cluster_network_load_balancer_vips` — have no
alternative to dispatch to, so their task playbooks import them by their exact
collection name and select them by their owning kind's discriminator (for OS
install, the `MachineInstallProfile.spec.installer.anaconda` arm). When such a family grows a
second backend, move it behind a registry projection like the substrate families
rather than branching inside a playbook.

The projected variable blocks described in
[`vars-contract.md`](../../ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md) carry the
substrate-specific inputs each role consumes. Roles do NOT branch on
`substrateRole` / `bmcRole` / `bootRole` to vary behavior within themselves —
that branching belongs in the registry and renderer, whose side of the rule is
[ADR 0009](0009-renderer-owns-listening-surface.md). The libvirt media role
consumes the libvirt-specific virtual-media cleanup block; the emulated BMC
service role reads provider BMC service `bmcEmulated.*` for libvirt URI, ports,
bind address, and auth instead of re-deriving defaults. Adding a
substrate-dependent fact is a renderer change, not a role conditional.

The `ownership_record` role owns durable resource and package ownership
records: executing roles include it at mutation time, and Go reads the records
for destroy scoping, package removal gating, orphan reporting, and recorded-drift
classification (`diff --recorded`).
The record contract is documented in
[`ownership-records.md`](../../ansible/collections/ansible_collections/bootwright/core/docs/ownership-records.md).

Task playbooks own the host-local selection of
`bootwright_current_provider`, `bootwright_current_cluster`, and related
runtime views. A future context-role extraction is allowed only if it preserves
those facts and does not move provider or install decisions out of the renderer.

## Consequences

- Public CLI commands expose separate bastion setup, check targets, staged
  graph apply, and full graph apply.
- Adding or removing apply support for a provider remains close to a role
  operation: add or remove the relevant collection role and one registry entry,
  then update tests. Public schema support still requires the typed API arm,
  validation, rendering, docs, and specs.
- The embedded bundle passes a collection path to Ansible instead of multiple
  role search paths and an out-of-band filter plugin path.
- Playbooks become easier to scan because workflows are expressed by collection
  workflow/task imports and role calls, while resource details live in owning
  roles.

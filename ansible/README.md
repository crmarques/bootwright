# Ansible Bundle

This source tree is packed into a generated embedded archive during
`make build` and materialized under
`/var/lib/bootwright/cache/ansible-bundles/<version>/` at runtime. Users do not
edit inventory, `group_vars`, or `host_vars`; Go renders inventory and vars
from desired state.

## Ownership

| Path | Owns |
| --- | --- |
| `collections/requirements*.yml` | External collection dependency inputs and lock metadata. |
| `collections/ansible_collections/bootwright/core/playbooks/workflow_*.yml` | User-facing workflows invoked by CLI targets. |
| `collections/ansible_collections/bootwright/core/playbooks/task_*.yml` | Focused task playbooks invoked by the CLI task planner or workflows. |
| `collections/ansible_collections/bootwright/core/playbooks/check_*.yml` | Read-only checks and preflight validation. |
| `collections/ansible_collections/bootwright/core/playbooks/tasks/<domain>/` | Reusable playbook task fragments kept below their owning domain. |
| `collections/ansible_collections/bootwright/core/roles/check_*` | Check-specific host and storage preflight capabilities. |
| `collections/ansible_collections/bootwright/core/roles/machine_base` | Reusable host package baseline. |
| `collections/ansible_collections/bootwright/core/roles/machine_proxy` | Proxy facts and persisted proxy settings. |
| `collections/ansible_collections/bootwright/core/roles/provider_host_*` | Provider-host substrate setup and host-scoped provider cleanup. |
| `collections/ansible_collections/bootwright/core/roles/provider_service_*` | Provider-host services, including explicit no-op BMC services. |
| `collections/ansible_collections/bootwright/core/roles/infra_component_*` | Host-bound InfraComponent services. |
| `collections/ansible_collections/bootwright/core/roles/machine_substrate_*` | Per-cluster substrate and network state. |
| `collections/ansible_collections/bootwright/core/roles/machine_os_install_*` | Bootwright-managed OS installation flows. |
| `collections/ansible_collections/bootwright/core/roles/container_cluster_*` | Agent installer execution, boot, wait, media, and destroy. |
| `collections/ansible_collections/bootwright/core/roles/storage_cluster_*` | Remote storage host mutation and Ceph command execution. |
| `collections/ansible_collections/bootwright/core/roles/support_*` | Cross-domain credentials, context-secret, and process-cleanup mechanics. |
| `collections/ansible_collections/bootwright/core/roles/ownership_record` | Durable resource and package ownership records for destroy scoping. |

## Role Rules

- Keep roles focused on one reusable capability.
- Split large roles into task files by concern.
- Put long config, XML, systemd unit, and script bodies in templates.
- Use fully qualified role names (`bootwright.core.*`) in playbooks.
- Prefer modules to shell; command and shell tasks must declare idempotency
  through `creates`, `changed_when`, or explicit checks.
- Mark sensitive tasks `no_log` and keep secret bytes out of rendered
  reviewable files.

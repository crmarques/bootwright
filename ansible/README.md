# Ansible Bundle

This tree is embedded into the `bootwright` binary and materialized under
`/var/lib/bootwright/cache/ansible-bundles/<version>/` at runtime. Users do not edit inventory,
`group_vars`, or `host_vars`; Go renders inventory and vars from desired
state.

## Ownership

| Path | Owns |
| --- | --- |
| `playbooks/targets/` | User-facing workflows invoked by CLI targets. |
| `playbooks/layers/` | Ordered layer orchestration. |
| `roles/bastion/` | Controller-local setup. |
| `roles/shared/` | Context and reusable host helpers. |
| `roles/providers/` | Provider-scoped shared services. |
| `roles/cluster_infra/` | Per-cluster substrate and network state. |
| `roles/openshift/` | Agent installer execution, boot, wait, and destroy. |

## Role Rules

- Keep roles focused on one reusable capability.
- Split large roles into task files by concern.
- Put long config, XML, systemd unit, and script bodies in templates.
- Prefer modules to shell; command and shell tasks must declare idempotency
  through `creates`, `changed_when`, or explicit checks.
- Mark sensitive tasks `no_log` and keep secret bytes out of rendered
  reviewable files.

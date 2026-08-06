# Linear lockstep: a per-host templated `include_role` splits the play into
# buckets that run one after another

Every play in `core/playbooks/` uses `strategy: linear` so one TASK banner
covers every machine (see `ansible-playbook-preamble-variants.md`). Linear
advances the play in lockstep: `_get_next_task_lockstep` hands out exactly one
task per round and waits for every host to finish it before moving on. That is
what makes the output group, and it has one consequence worth knowing before
anyone measures a boot and reaches for `strategy: free` again.

## What splits, and what does not

`container_cluster_agent_install/tasks/actions/boot_machine.yml` dispatches the
substrate driver with a **templated role name**:

```yaml
- name: Boot selected machine via its substrate-specific driver
  ansible.builtin.include_role:
    name: "{{ bootwright_selected_component.bootApplyRole }}"
```

When hosts in one play resolve *different* names there, the include produces a
separate lockstep bucket per name, and the buckets run **in sequence**, not
concurrently — including their long terminal waits.

A templated **argument** does not split anything. `IncludedFile.__eq__` keys on
the file, its args, and the parent task UUIDs; a task-level `vars:` block is not
part of that key. That is why the three
`include_tasks: purge_media_pvc.yml` sites in
`container_cluster_boot_kubevirt/tasks/main.yml`, which pass a per-host
`bootwright_kubevirt_media_pvc_name`, still coalesce into one bucket and one
banner.

## When this actually bites

Only a cluster whose machines resolve different `bootApplyRole` values. Per
`internal/roles/registry.go`, **libvirt and bare metal both map to
`container_cluster_boot_redfish`**, so mixing those does not split. Splitting
needs a genuine KubeVirt / vSphere / Redfish mix inside one ContainerCluster.

Measured on a 5-host local play with two substrate buckets, each a 4s task:

| strategy | wall clock | shape |
| --- | --- | --- |
| `free` | ~5.5s (max of buckets) | banner reprinted per host |
| `linear` | ~9.9s (sum of buckets) | one banner per task per bucket |

So a mixed-substrate cluster pays the **sum** of its substrates' boot windows
instead of the max. A homogeneous cluster — every machine on one substrate,
which is the normal case and every shipped example — has exactly one bucket and
pays nothing.

This cost is accepted deliberately. Interleaved per-machine output was a
reported user-facing defect; a slower boot for an unusual mixed-substrate
topology is not. Do not reintroduce `free` to reclaim it —
`TestNoPlaybookReintroducesTheFreeStrategy` fails on `free` and `host_pinned`.
If a mixed-substrate cluster ever needs the overlap back, the fix is to split
the boot into one planner task per substrate so each gets its own
`ansible-playbook` process, not to un-group the output.

## Negative pin: `throttle` is NOT affected by the strategy flip

It is tempting to assume `strategy: free` ignored `throttle` and that moving to
linear silently activated the two throttled tasks
(`container_cluster_media_vsphere/tasks/insert.yml` `throttle: 1`, and the
conditional cold-init throttle in
`container_cluster_boot_redfish/tasks/media/prepare.yml`). It did not.

Measured on a 5-host play running one 2s task: unthrottled ~3.3s; with
`throttle: 1` **~14.3s under `free` and ~13.0s under `linear`**. Both strategies
honour `throttle`. The flip changed nothing about those two tasks, and no guard
is needed on that basis.

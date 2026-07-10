# Registers and facts under skipped tasks: stale-value and dead-signal traps

Four related Ansible result-handling semantics that have each caused a real
bug. The common thread: what a registered variable or fact holds after a task
was skipped, not loaded, or had its failure suppressed is rarely what the
next task assumes.

**Skipped set_fact leaves the previous value.** A `set_fact` whose `when` is
false does NOT reset the fact — the prior value survives. In a tasks file
included once per loop item this bleeds one iteration's data into the next:
`ownership_record/tasks/package_apply_one.yml` seeds
`bootwright_ownership_existing_package_record: {}` at the top of every
inclusion because a skipped `Decode existing package ownership record` would
otherwise leave the previous package's record in place, giving a record-less
package the prior package's `requiredBy`/`preexisting`.

**Registers only written on some loop paths persist across iterations.** A
`register` that only some branches of a per-item include ever execute keeps
the last executing item's value for every later item.
`storage_cluster_cephadm/tasks/operations/classify.yml` resets
`bootwright_ceph_rgw_user_info: {}` per operation because the probe that
registers it runs only for rgw-user ops and its result would otherwise
persist across the operations loop.

**A task that runs but skips DOES overwrite its register.** The inverse trap:
a `when`-skipped task that carries `register:` replaces the variable with a
`skipped: true` shell, clobbering real evidence captured earlier. This is why
the Redfish virtual-media flow guards its probe registers so a skipped retry
task cannot overwrite the latest real probe (see redfish-virtual-media.md).
When a register accumulates evidence across retries, gate the consuming task
on the register's content, never on "the task ran last".

**failed_when: false rewrites .failed — key off success-only fields.**
Suppressing failure rewrites the registered `.failed` to `False`, so it
cannot distinguish success from a suppressed failure. Decisions must key off
payload keys that exist only on the success path:

- `machine_substrate_vsphere/tasks/destroy.yml` derives
  `bootwright_vsphere_vm_present: "{{ bootwright_vsphere_destroy_info.instance is defined }}"`
  — `community.vmware.vmware_guest_info` returns `instance` only when the VM
  exists, so a missing VM keeps destroy idempotent while `.failed` is useless.
- `ansible.builtin.wait_for` under `failed_when: false` sets `msg` only on
  its failure path, so absence of `msg` is the reliable "port is open"
  predicate (see managed-os-install-gates.md).

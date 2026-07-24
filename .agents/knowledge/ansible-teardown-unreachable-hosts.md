# Teardown plays vs unreachable hosts: gather_facts, any_errors_fatal, ignore_unreachable

How a destructive play (`task_storage_cluster_destroy.yml` is the reference
shape) stays able to tear down a never-provisioned cluster whose nodes are all
down, while still failing closed before any wipe. The scoping/ownership
semantics live in destroy-scoping-and-sweeps.md and
ceph-ownership-apply-destroy-gates.md; this file records the Ansible play
mechanics.

**Constraint: the implicit fact gather defeats ignore_unreachable.** The
implicit `gather_facts` connects to every host before any task runs, and under
`any_errors_fatal: true` an unreachable host aborts the whole play right there
with a raw SSH error — even with play-level `ignore_unreachable`. A play that
must classify unreachable hosts therefore sets `gather_facts: false` and runs
an explicit `ansible.builtin.setup` task later, after reachability is
classified (`Gather facts on reachable storage hosts`).

**Constraint: classification needs a task-level override.** Play-level
`ignore_unreachable` is the operator gate
(`{{ bootwright_destroy_skip_unreachable | default(false) | bool }}` — off by
default, on under `--skip-unreachable`) and only shapes post-classification
connecting tasks. The reachability probe itself
(`Probe storage host reachability before teardown`, an `ansible.builtin.ping`
with `register:` + `failed_when: false`) carries its own task-level
`ignore_unreachable: true`, so an unreachable host is recorded
(`.unreachable: true`) and kept in the play instead of aborting it — even in
the fail-closed default mode.

**Managed storage identities are selected before the remote probe.** A storage
node may be between the cephadm and install-window identities after a partial
destroy. The shared `storage_node_access/select_connection.yml` controller-side
selector probes both identities before a teardown play opens its normal Ansible
connection. When Bootwright owns the trust file, it can also reconstruct a
missing canonical-FQDN alias from the existing raw-address entry under a file
lock; this reuses established trust and does not run a host-key scan or select
an algorithm. The selector preserves canonical `ansible_host`, changes only
`ansible_user`, and reports a boolean availability fact. Storage destroy maps a
false result to the same `.unreachable: true` shape used by the ordinary ping,
so the assert and `--skip-unreachable` behavior remain one classification path.

**Semantics: controller-side tasks still run for unreachable hosts.**
`assert`, `set_fact`, and `delegate_to: localhost` tasks evaluate on a host
whose connection probes failed. That is what makes the fail-closed gate work:
`Require storage hosts reachable unless --skip-unreachable` is an assert, so
it fires with actionable guidance (power the nodes on, or re-run with
`--force --skip-unreachable`) instead of a raw SSH error, and
`any_errors_fatal` turns that one failure into a whole-play abort before any
node wipes a device. Under `--skip-unreachable` the assert is skipped and
`meta: end_host` drops the unreachable host instead.

**Semantics: unreachable is not a failure.** `any_errors_fatal` is kept
literally `true` so a genuine task failure (seed ownership refusal, device
gate) aborts every host before any wipe. Unreachable hosts are tolerated only
by the tasks that opt in with `ignore_unreachable` (the probe, and the
deferred gather under `--skip-unreachable`, which also tolerates a host that
flapped down after the probe). When every node was skipped, no host remains
and the downstream wipe tasks are a clean no-op — exactly the
never-provisioned-cluster teardown.

**Constraint: proxy facts before the probe.** `machine_proxy` `facts` runs in
`pre_tasks`; it is controller-side only (set_fact plus a
`delegate_to: localhost` credential slurp), so with `gather_facts: false` it
completes without contacting an unreachable host and the play-level
`environment: "{{ bootwright_proxy_env | default({}) }}"` is populated before
any connecting task.

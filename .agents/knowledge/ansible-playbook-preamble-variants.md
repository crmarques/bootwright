# Task-playbook preambles: the standard shape and its deliberate variants

Most `playbooks/task_*.yml` plays share one preamble: `strategy: free`,
`gather_subset: ['!all', min]`, `become: true`,
`environment: "{{ bootwright_proxy_env | default({}) }}"`, and a
`machine_proxy tasks_from: facts` pre_task. Ansible cannot factor play
keywords into an include, so this repetition is structural, not accidental —
do not try to extract it. Five playbooks deviate on purpose:

- `task_bastion_apply_tools.yml` forwards `lookup('env', …)` proxy variables
  and skips the proxy-facts pre_task: bastion setup runs before any rendered
  Environment exists, so the operator's own shell proxy is the only source.
- `check_external_reachability.yml` runs without `become` or proxy
  environment: it is a read-only reachability probe whose whole point is to
  observe the host's real network posture.
- `task_managed_machine_os_apply.yml` sets `any_errors_fatal: true` and drops
  `strategy: free`: managed OS installs in one group must abort together
  rather than leave a partially reinstalled group.
- `task_storage_cluster_apply.yml` uses `strategy: linear` with
  `gather_facts: false` (facts gathered inside the role): the storage role
  coordinates non-seed hosts against seed-host decisions via
  `hostvars[seedHost]`, which requires lockstep task execution.
- `task_machine_infra_destroy.yml` has three plays: real-host VIP preparation,
  a synthetic-host machine pass under `strategy: linear`, and a real-host
  record/sweep cleanup under `strategy: free`. Linear runs each role task
  concurrently across the current cluster's machine hosts while preserving
  the planner's child-before-host barrier between cluster passes.

The per-play component-selection blocks (`Resolve selected cluster` /
`Pick … component`) look alike but differ in source lists
(`bootwright_clusters` vs `bootwright_managed_os_install_groups` fallback),
filter attributes, and empty-set behavior (hard assert vs `end_host` skip) —
that variance is each phase's semantics, so the blocks stay local (evaluated
and rejected as a shared fragment, 2026-07-21). Likewise the
`virtctl stop` / `kubectl delete datavolume` blocks in
`container_cluster_boot_kubevirt` vs `machine_substrate_kubevirt` share only
the CLI prefix: boot forces an immediate stop (`--force --grace-period 0`,
tolerates "not running"/"not found") to swap the agent ISO, while destroy
stops gracefully behind ownership gates with `failed_when: false` — merging
them would change failure semantics.

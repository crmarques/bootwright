# Task-playbook preambles: the standard shape and its deliberate variants

Most `playbooks/task_*.yml` plays share one preamble:
`gather_subset: ['!all', min]`, `become: true`,
`environment: "{{ bootwright_proxy_env | default({}) }}"`, and a
`machine_proxy tasks_from: facts` pre_task. Ansible cannot factor play
keywords into an include, so this repetition is structural, not accidental —
do not try to extract it.

**Strategy is now a decided rule, not a per-play preference.** Every play that
can be dispatched over more than one machine uses `strategy: linear`, because
only linear prints one TASK banner per task; `free` reprints the banner every
time any host reaches the task, which is what the interleaved per-machine
output looked like before 2026-08-06. `ansible/ansible.cfg` sets
`display_skipped_hosts = False`, so the banner is emitted lazily from the
result handler — under linear the results for one task arrive contiguously and
the banner flips exactly once. `host_pinned` is NOT an alternative: it
subclasses `FreeStrategyModule` and the default callback tests membership in
`('free', 'host_pinned')`, so it takes the identical per-host banner branch.

As of 2026-08-06 **no play in the repo uses `strategy: free`** — every
`playbooks/*.yml` play states `strategy: linear` explicitly, or omits the key
where another rule already documents the default. Do not reintroduce `free`
without re-reading this file: the interleaved per-machine output it produces is
a reported user-facing defect, not a neutral trade-off. These playbooks deviate
from the shared preamble in other ways, on purpose:

- `task_bastion_apply_tools.yml` forwards `lookup('env', …)` proxy variables
  and skips the proxy-facts pre_task: bastion setup runs before any rendered
  Environment exists, so the operator's own shell proxy is the only source.
- `check_external_reachability.yml` runs without `become` or proxy
  environment: it is a read-only reachability probe whose whole point is to
  observe the host's real network posture.
- `task_managed_machine_os_apply.yml` sets `any_errors_fatal: true` and drops
  `strategy: free`: managed OS installs in one group must abort together
  rather than leave a partially reinstalled group.
- `task_machine_infra_apply.yml` sets `strategy: linear`: the planner dispatches
  one process per (cluster, provider host) over the machines' pseudo-hosts with
  `Forks` equal to the machine count, and only the linear strategy prints one
  TASK banner per task instead of reprinting it per host. The play carries no
  `run_once`, `throttle`, `serial` or `any_errors_fatal`, so the flip only adds a
  barrier; the cost is lockstep — the 10-minute DataVolume wait in
  `machine_substrate_kubevirt` gates the whole batch at that task rather than per
  machine. Machines whose substrate charges a host slot (libvirt, vSphere) keep
  their own single-host task, where free and linear are byte-identical.
- `task_container_cluster_boot_agent_machine.yml` uses `strategy: linear`. It is
  the most multi-host play in the repo — the planner dispatches it once per
  cluster with `Limit` = the whole agent-node group and `Forks` = the machine
  count — so it was the single largest source of per-machine banner spam
  (`container_cluster_boot_kubevirt` alone has 40+ tasks). Linear is also
  *better* than free for its shared-media election, not worse: in both the
  KubeVirt and vSphere boot roles the elected machine's stage/upload task
  precedes the peers' wait task in file order, so the linear barrier means the
  media is already published when peers reach their wait, and the polling
  `until`/`wait_for` loops are satisfied on the first attempt instead of
  spinning. Machines run each task concurrently under `Forks`, so wall clock is
  unchanged for a homogeneous cluster — every machine on one substrate, which is
  the normal case and every shipped example. A cluster whose machines resolve
  **different** `bootApplyRole` values is the exception: the templated
  `include_role` splits the play into one lockstep bucket per boot role and the
  buckets run in sequence, so its boot phase costs the sum of the substrates'
  windows rather than the max. That cost is accepted deliberately; see
  `ansible-linear-lockstep-include-buckets.md`, which also pins the measurement
  showing `throttle` binds under `free` as well, so retiring `free` activated no
  previously-dead throttle.
- `task_machine_infra_prepare.yml`, `task_machine_infra_finalize.yml`,
  `task_provider_services_apply.yml` and `task_infra_component_services_apply.yml`
  use `strategy: linear` because every dispatch of them selects exactly one host,
  which makes free and linear byte-identical.
- `task_storage_cluster_apply.yml` uses `strategy: linear` with
  `gather_facts: false` (facts gathered inside the role): the storage role
  coordinates non-seed hosts against seed-host decisions via
  `hostvars[seedHost]`, which requires lockstep task execution.
- `task_machine_infra_destroy.yml` has three plays — real-host VIP preparation,
  a synthetic-host machine pass, and a real-host record/sweep cleanup — all
  three under `strategy: linear`. Linear runs each role task concurrently
  across the current cluster's machine hosts while preserving the planner's
  child-before-host barrier between cluster passes; the sweep is dispatched
  over `bootwright_provider_hosts:bootwright_infra_hosts` at once, so it needs
  linear for the same one-banner-per-task reason as the apply path.

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

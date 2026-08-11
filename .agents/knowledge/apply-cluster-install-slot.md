# Apply scheduler capacity: cluster-install and Redfish slots

The default cluster-install capacity equals the number of distinct install
chains in the selected graph, so it adds no ordering to the authored DAG.
`--cluster-install-parallelism <positive N>` narrows it for one `apply` or
`plan`; `BOOTWRIGHT_APPLY_PARALLELISM_CLUSTERS` supplies the standing override,
with flag > environment > graph-derived default precedence. The resolved value
is clamped to graph demand and persisted in the run ledger.

The Redfish budget has the same non-binding default: its capacity is the sum of
the positive Redfish-slot demand declared by the selected apply or destroy
graph. `BOOTWRIGHT_APPLY_PARALLELISM_REDFISH` narrows it as standing estate
policy. Distinct per-BMC resource keys remain the correctness lock; the
aggregate budget exists for management paths that need an operator-selected
throttle.

`clusterInstallKey` decides membership: the agent-ISO, node-boot,
bootstrap-wait and install-wait tasks of one ContainerCluster count as that one
cluster, however many tasks they are. Storage clusters are deliberately outside
it — a Ceph install pulls no release payload, so `storage.<name>` never competes
for the slot and must never be added to that switch.

**Why the ISO build is inside the slot:** it is not decoration. `openshift-install
agent create image` pulls the release payload for the declared version, which is
the same registry/mirror path the boot and install waits then hammer. Two clusters
building ISOs at once is exactly the contention a narrowed cap exists to
prevent, so removing `ApplyTaskKindClusterISO` from `clusterInstallKey` would
defeat the cap rather than fix a queueing complaint.

**Constraint (when capacity is scarce, a parked chain must not take the slot
ahead of a chain that can finish):** admission is a two-step decision, not
first-come-first-served over task order. A KubeVirt-hosted cluster's ISO task
depends only on fabric, so it is ready at t=0 — while the rest of its chain
cannot run until its host OCP cluster installs and its add-ons apply, potentially
hours later. Under an explicit cap of 1, task iteration order once handed that
slot to hosted clusters that could not use it: both hub ISOs built first, and
the bare-metal cluster whose install everything else waits on sat at `waiting
for a cluster install slot` until they finished.

`clusterInstallSlotAdmission` closes that: a cluster whose remaining install chain
transitively waits on a task belonging to *another* cluster is **parked**, and
while any unparked chain still wants the slot, only unparked chains are admitted.
When every remaining chain is parked the set opens up again, so a run whose only
work is hosted clusters still makes progress. This composes with — and does not
replace — `releaseIdleClusterInstalls`, which hands the slot *back* when a holder
parks mid-chain; admission stops it being taken in the first place.
When the resolved capacity fits every remaining chain, admission returns every
candidate and the DAG alone decides which tasks are runnable.

**Constraint (an explicit Redfish throttle is granted partially, never
all-or-nothing):** the demand is per task: a six-node storage managed-OS install
charges six, and each three-master node boot charges three. The graph-full
default resolves that `6 + 3 + 3` topology to 12, so all independent cohorts can
enter. Under an explicit limit of eight, first-fit admission may grant `6 + 2 +
0`: the storage install and first OCP boot run while the second OCP boot waits on
the declared estate policy. `taskRedfishGrant` grants `min(demand, free)`
whenever at least one slot is free, charges exactly that, and clamps the task's
Ansible `forks` to the grant. The grant is held until the whole task returns and
is recorded per task ID (`grantedRedfish`), because deriving the release from
the task's demand would corrupt the counter as soon as grants are partial.

**Reading a stalled run:** `status --timings` prints the blocker vocabulary
(`cluster install budget`, `redfish budget`, `task budget`, `resource <key>`,
`host slot <key>`). `BlockedOn` accumulates rather than replaces, so a task shows
every budget it has ever waited on, newest last. A dispatch pass that denies any
ready task persists the updated ledger and publishes a live snapshot before it
waits for another task event; the run tree therefore shows `waiting for Redfish
slots` rather than a bare pending phase. Builds before ADR 0059 could retain the
blocker only in memory until another start or completion refreshed the frame.

The run header counts high-level phase rows, not executing scheduler tasks. A
phase with one completed task and later pending tasks is `RUNNING`, so a hosted
cluster's prerequisite row may say `1/3` while all of its remaining work is
dependency-blocked. That row consumes no task, Redfish, or cluster-install slot.

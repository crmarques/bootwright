# The cluster-install slot: what it bounds and who may hold it

`BOOTWRIGHT_APPLY_PARALLELISM_CLUSTERS` (default 1) bounds how many clusters are
*installing* at once. `clusterInstallKey` decides membership: the agent-ISO,
node-boot, bootstrap-wait and install-wait tasks of one cluster count as that one
cluster, however many tasks they are. Storage clusters are deliberately outside
it — a Ceph install pulls no release payload, so `storage.<name>` never competes
for the slot and must never be added to that switch.

**Why the ISO build is inside the slot:** it is not decoration. `openshift-install
agent create image` pulls the release payload for the declared version, which is
the same registry/mirror path the boot and install waits then hammer. Two clusters
building ISOs at once is exactly the contention the cap exists to prevent, so
removing `ApplyTaskKindClusterISO` from `clusterInstallKey` would defeat the cap
rather than fix a queueing complaint.

**Constraint (a parked chain must not take the slot ahead of a chain that can
finish):** admission is a two-step decision, not first-come-first-served over task
order. A KubeVirt-hosted cluster's ISO task depends only on fabric, so it is ready
at t=0 — while the rest of its chain cannot run until its host OCP cluster
installs and its add-ons apply, potentially hours later. Task iteration order is
plan order, and `hub-*` sorts before `ocp-*`, so the naive rule handed the only
slot to hosted clusters that could not use it: both hub ISOs built first, and the
bare-metal cluster whose install everything else waits on sat at
`waiting for a cluster install slot` until they finished.

`clusterInstallSlotAdmission` closes that: a cluster whose remaining install chain
transitively waits on a task belonging to *another* cluster is **parked**, and
while any unparked chain still wants the slot, only unparked chains are admitted.
When every remaining chain is parked the set opens up again, so a run whose only
work is hosted clusters still makes progress. This composes with — and does not
replace — `releaseIdleClusterInstalls`, which hands the slot *back* when a holder
parks mid-chain; admission stops it being taken in the first place.

**Constraint (the Redfish budget is granted partially, never all-or-nothing):**
`ParallelismRedfish` (default 8) bounds concurrent BMC operations across the run.
The demand is per task: a 6-node storage managed-OS install charges 6, a 3-master
node boot charges 3. Requiring the full demand to fit made a 12-slot fleet
serialize against an 8-slot budget — the storage OS install held 6 for its whole
multi-hour runtime and the 3-slot OCP boot could never dispatch, parking a
bare-metal cluster (and its hosted cluster, and every add-on behind it) on a task
it shares nothing with. `taskRedfishGrant` instead grants
`min(demand, free)` whenever at least one slot is free, charges exactly that, and
clamps the task's Ansible `forks` to the grant — so the task performs at most as
many concurrent BMC operations as it was granted and the budget still means what
it says. The grant is recorded per task ID (`grantedRedfish`) because the release
must return exactly what was charged; deriving the release from the task's demand
again would corrupt the counter as soon as grants are partial.

**Reading a stalled run:** `status --timings` prints the blocker vocabulary
(`cluster install budget`, `redfish budget`, `task budget`, `resource <key>`,
`host slot <key>`). `BlockedOn` accumulates rather than replaces, so a task shows
every budget it has ever waited on, newest last.

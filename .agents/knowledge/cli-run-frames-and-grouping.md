# Apply and destroy run-frame grouping

**Semantics: one ledger-to-frame mapper.** `applyRunFrame` is the single
mapper feeding both the live apply reporter and `status --watch`, so the
two views cannot diverge; `status` renders with width 0 (no wrap
accounting) because it reprints the whole page each poll.

**Semantics: infra tasks get a leading non-cluster group.**
`applyRunFrame` puts fabric/infra tasks that own no cluster in a leading
non-cluster "infra" group so an infra-only run (`apply --stage infra`)
still lists its steps. The displays map may be nil (no state loaded), in
which case group headings fall back to the ledger-derived kind word and
ordering falls back to alphabetical. `printStageScopeNotices` annotates a
scoped dry-run/plan with the phases the plan leaves out and warns that
skipped earlier phases are assumed completed by a prior apply (a surgical
rerun), staying silent for the full graph.

**Semantics: cluster descriptors and topology-aware ordering.** Every
CLI surface (apply frame, summary, status, inventory) derives cluster
descriptors once via `buildClusterDisplays` so a bare-metal OpenShift
parent ("OpenShift · bare metal"), a KubeVirt-hosted child ("OpenShift ·
KubeVirt on <host>"), and a Ceph cluster read consistently.
`orderClusterNames` orders topology-aware — storage clusters first, then
each container root immediately followed by the KubeVirt children it
hosts — matching apply/teardown reality (a child cannot install before
its host) instead of alphabetical order that would print a child above
its parent, with deterministic alphabetical order within groups.

**Semantics: the destroy frame is a single ordered group.**
`destroyRunFrame` projects destroy tasks into one group (no per-cluster
grouping) because destroy tasks are scope-level host-group teardowns
covering several clusters at once; each step therefore names the
clusters it covers from the persisted `ResourceKeys` — including on
failure — otherwise a failed "Container clusters" step would not say
which of the fleet it covered. Guarded by
`TestDestroyOutputNamesCoveredClusters`.

**Invariant: a phase row may only hold task kinds that are graph
ancestors of every kind in the row below it, within one cluster group.**
The rows are printed as a top-to-bottom sequence, so a reader takes them
as stages; `TaskPhaseLabel` must not group kinds that can be live at the
same time as a later row. Two consequences are load-bearing and were
each a reported bug:

- A container cluster has **no "Machines" row**. `clusterISO` builds on
  the bastion and depends only on the machine-services tasks, so it runs
  concurrently with `ClusterInstall` (the per-machine VM creation), which
  on a KubeVirt guest additionally waits for the whole host cluster.
  Printing them as separate rows made "Prerequisites" finish before
  "Machines" started. `MachineInfraPrepare` + `ClusterInstall` +
  `MachineInfraFinalize` + `ClusterISO` therefore share one
  "Prerequisites" row, which is exactly `NodeBoot`'s dependency set.
  Adding a `machines -> iso` edge instead would put the ISO build on the
  critical path of every KubeVirt guest — do not "fix" it that way.
- A **storage** cluster keeps its "Machines" row: `ManagedMachineOS ->
  StorageNodeAccess -> MachineRegistration -> MachineRepositories ->
  StorageInfra -> StorageCluster` is a genuine chain, and the managed-OS
  install is the longest step of a Ceph apply. `MachineInfraPrepare` is
  emitted by both families, so `TaskPhaseLabel` splits it on
  `Entry.ClusterKind`.

**Gotcha: `HostVirtctl` is a `deps`-stage kind that runs after its
cluster is installed.** `virtctl.<host>` carries `Cluster = <host
cluster>` yet `Requires` that host's `clusterInstalled` capability *and*
its KubeVirt add-on, so it is the last task in the host's subtree — it
belongs in the "Add-ons" row, not "Prerequisites". Re-attributing it to
the guest instead is not display-only: `Entry.Cluster` is folded into the
converge-safety hash identity, gates scoped converge-record reset, and
filters standing-cluster evidence.

There is deliberately no "Publish" phase because storage-export
consumption (e.g. a Data Foundation add-on attaching exported storage)
runs inside the consuming cluster's add-on tasks and reports under that
cluster. `runphase_coverage_test.go` pins the per-family phase ranks and
that no planned task kind falls through to the `PhaseWork` catch-all.

**Gotcha: display order comes from `Dependencies` *and*
`OrderingDependencies`.** Destroy tasks express their teardown order
purely as `OrderingDependencies`, so a `TasksInDisplayOrder` that reads
only `Dependencies` prints teardown rows in ledger insertion order.
Apply tasks carry no ordering edges, so including them is inert there.

**Gotcha: `applyOutputStatus` is a worst-of severity aggregator, not a
progress roll-up.** It ranks StatusOK/Done LOWEST, so reusing it to fold a
group's child statuses into one progress/completion status masks a Done child
behind any less-advanced sibling — a group that has actually finished reads as
still-working. Group progress needs a separate monotonic aggregator; do not
route group-completion roll-ups through `applyOutputStatus`.

**Boundary: phase aggregation lives in internal/status.** The apply
phase aggregation itself (cluster kinds, phase grouping, terminal
states) is owned and tested by `internal/status` (`applyrun_test.go`);
`internal/cli` only maps those phases to display statuses. Don't add
aggregation tests or logic on the CLI side.

**Boundary: shared mutating-run behavior lives in
`internal/cli/mutating_run.go`.** It is the deliberate seam shared by
apply and destroy: both sequence Prepare banner → load/plan → gates →
become credential + workflow reporter → converge executor, and the
shared presentation/setup steps live there to kill copy-shaped
duplication. The gate sequencing and executor dispatch intentionally
stay in each command (they differ), and the domain decisions each step
makes live in `internal/converge` — keep new shared mutating-run
behavior in this seam, not duplicated in the two command files.

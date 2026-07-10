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

**Semantics: storage clusters show exactly two fleet phases.**
"Infrastructure" covers host standup and storage prep
(`MachineInfraPrepare` + `ManagedMachineOS` + `StorageInfra` task kinds)
and "Provision" covers the `StorageCluster` kind. The former separate
"Prepare" phase was computed from the identical task filter so it could
never differ from Infrastructure and was dropped; there is deliberately
no "Publish" phase because storage-export consumption (e.g. a Data
Foundation add-on attaching exported storage) runs inside the consuming
cluster's add-on tasks and reports under that cluster. `applyrun_test.go`
pins the absent Prepare/Publish phases.

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

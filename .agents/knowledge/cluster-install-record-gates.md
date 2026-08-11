# Cluster install record gates: adoption refusal, destroy cleanup, override healthy-skip

**Constraint (no silent adoption):** A ContainerCluster with a reachable
kubeconfig but **no** install record is refused:
`ContainerCluster/<name> has a reachable kubeconfig but no install record; bootwright cannot confirm it was installed from the current desired inputs and will not adopt it silently`.
Silently stamping today's hashes as the install baseline would absorb real
install-input drift as "installed and in sync" — the change would never be
applied and every later gate would compare against a forged baseline. The
operator chooses explicitly: rebuild (`apply --stage clusters --clusters
<name> --mode rebuild --authorize data-loss --yes`) or restore `clusters/<name>/runtime/install-record.json`
if the running cluster genuinely matches. A record-less cluster whose
kubeconfig does not report Available=True fails with
`has existing kubeconfig but does not report Available=True; refusing to
regenerate installer inputs without --mode rebuild`. A fresh cluster (no
kubeconfig at all) installs normally. See `guardUnrecordedCluster` in
`internal/converge/workflow/install_state.go`.

**Constraint (destroy must remove controller-side install state):**
`RemoveClusterInstallState` deletes the install record, connection record, and
kubeconfig for a torn-down cluster (each a no-op if absent). The Ansible
cluster destroy runs on `bootwright_ocp_hosts` (controller-side) and cannot
remove these files itself; without this Go-side cleanup a surviving record or
kubeconfig makes `ReconcileApplyClusterInstallState` refuse the next apply
with the Available=True refusal above. `RecordedProvisionedClusters`
enumerates the per-cluster record files (not desired state), which is how a
rename (old record orphans, new name re-provisions) or an orphan (declaration
removed without destroy) is detected. A `StorageCluster` has no install record,
so `CheckApplyRenameOrphan` reads its rename evidence from the
`storage-cluster` **ownership** records instead (owner `bootwright`, `cluster`
field) — same signature, same restore-then-destroy remedy, different evidence
store.

**Semantics (override healthy-skip):** `apply --mode rebuild` does **not**
reinstall a healthy cluster. A cluster whose record matches the desired
install inputs, is `installed`, and whose kubeconfig probe reports
Available=True has its install tasks skipped:
`cluster already installed and Available=True for desired install inputs; --mode rebuild rebuilds only drifted objects, not a healthy in-sync cluster`.
This protects healthy clusters caught in a scoped `apply --mode rebuild` aimed at
some other drifted object.

**Constraint (a failed probe is not a rebuild authorization):** a probe
**error** on a cluster whose record matches the desired install inputs (`oc`
missing from PATH, an unreadable/undecryptable kubeconfig, a network blip, an
API timeout) is a fail-closed refusal in **every** mode, including
`--mode rebuild`. `OverrideRebuildInstalledClusters` returns an error naming
each unprovable cluster and the probe error, with a typed same-invocation retry
after reachability, kubeconfig access, and `oc` are restored; it does not infer
an exclusion or destructive rebuild from a failed observation. The run stops
before any mutation. It previously
scheduled the reinstall instead, so `--mode rebuild --authorize data-loss
--yes` could wipe the node disks of a healthy cluster whose API was momentarily
unreachable. A *successful* probe reporting `Available=False` is different
evidence — the cluster answered — and still authorizes the override rebuild,
gated by the data-loss acknowledgment. That is the single sanctioned case of
`--mode rebuild` acting on an object whose recorded desired state matches
(evaluated and kept 2026-07-26; rationale in ADR 0007, "Ownership is the
authorization boundary"). Do not re-propose routing it through `destroy`:
the cluster supplied the evidence itself, the object matches its declaration but
not its recorded condition, and repairing a dead cluster in place is the case the
flag exists for.

**Semantics (structural hash migration safety):** Install records carry both
`DesiredHash` (full install-input hash) and `StructuralHash` (same payload
with day-2-owned intent — cluster add-ons, per-node labels/taints — projected
out). Referenced NetworkConfigs remain in the structural projection because
they reach install-config/agent-config. The gate compares on `StructuralHash`
when present so a day-2-only edit reconciles in place. Every record write stamps
both hashes and the current hash schema together (`clusterInstallHashes`). A
previous-schema record matches only through the immutable successful-input
snapshot and its exact archived successful ledger task; absent or ambiguous
evidence fails closed, and changed input remains drift. The machine-infra
*prepare* and *finalize* tasks
share the install task's structural projection for the same reason: without
it, any day-2 edit flipped them to structural drift and `apply` refused with a
false "would reinstall the machine — its disks wiped".

**Semantics (first bare-metal install warning + occupancy opt-out):**
`BareMetalFirstInstallClusters` names, at confirm time, the clusters whose
planned nodeBoot (Redfish virtual-media) task is not covered by a boot-proven
install record — i.e. an apply that will drive coreos-installer to disk-wipe
physical hosts bootwright cannot prove it already booted. The CLI prints:
`first apply will boot the OS installer on the bare-metal host(s) of <names>
... coreos-installer will DISK-WIPE their target disks`, and each BMC is also
checked for an already-running OS (Redfish occupancy guard) before boot. A
cluster is excluded only when `BootProvenContainerClusters` accepts its
install record: status `installed`, or a phase in {booting, nodes-booted,
waiting, complete}, and never status `destroyed`. A converge-safety record
alone — e.g. an interrupted run that only created the agent ISO
(`iso-created`) — keeps both the warning and the occupancy guard armed. The
same boot-proven set, unioned with released-substrate clusters (a destroyed
cluster's rebuild legitimately faces its own old OS), feeds
`bootwright_ocp_reinstall_clusters`, the occupancy-guard opt-out list.

**Constraint (status and phase are one lifecycle state):** JSON decoding proves
only that an install record is syntactically readable. Before install planning,
`validateClusterInstallRecordState` requires `installing` or `failed` with an
empty or named nonterminal phase, or `installed` or `destroyed` with `complete`.
An unknown value or contradictory pair cannot select a resume boundary and
returns typed `rebuild-cluster` remedy data before desired-hash work, an
availability probe, or task-plan mutation. The explicit rebuild preview treats
that invalid record as a node-disk-wiping reinstall candidate; only its resulting
acknowledgement lets scheduler preparation retain the full install plan. This
keeps the exact remedy executable without letting ordinary reconcile silently
fall through the status switch and reinstall a cluster.

**Constraint (bounded resume and installer-version evidence):** ISO creation
records the exact installer version and clears stale version evidence before a
new create attempt. An `iso-created` record reaches node boot only when
`UpdatedAt` proves that the published media is less than 24 hours old. A
missing, future-dated, or at-least-24-hour-old publish time cannot prove the
embedded bootstrap certificates are fresh, so scheduler preparation returns a
typed cluster-scoped ISO-regeneration refusal before it writes a run ledger or
creates a runner. The bootstrap wait records both its running and completed
phase. Post-boot retries use the original `StartedAt` and may start only inside
the three-hour ceiling; a missing time or an expired ceiling is a pre-mutation
refusal with typed, scoped destroy-and-reapply remedy data. A missing or
mismatched installer version before boot requires ISO regeneration. Once nodes
may have booted, Ansible warns and completes the in-flight install; the successful
record remains durable, but the Go runner returns a typed nonzero future-rebuild
error afterward so skew can never be stamped as healthy. An image-only release
declaration has no comparable version and is deliberately exempt.

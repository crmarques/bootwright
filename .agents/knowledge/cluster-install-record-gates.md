# Cluster install record gates: adoption refusal, destroy cleanup, override healthy-skip

**Constraint (no silent adoption):** A ContainerCluster with a reachable
kubeconfig but **no** install record is refused:
`ContainerCluster/<name> has a reachable kubeconfig but no install record; bootwright cannot confirm it was installed from the current desired inputs and will not adopt it silently`.
Silently stamping today's hashes as the install baseline would absorb real
install-input drift as "installed and in sync" — the change would never be
applied and every later gate would compare against a forged baseline. The
operator chooses explicitly: rebuild (`apply --stage clusters --clusters
<name> --override --yes`) or restore `clusters/<name>/runtime/install-record.json`
if the running cluster genuinely matches. A record-less cluster whose
kubeconfig does not report Available=True fails with
`has existing kubeconfig but does not report Available=True; refusing to
regenerate installer inputs without --override`. A fresh cluster (no
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
removed without destroy) is detected.

**Semantics (override healthy-skip):** `apply --override` does **not**
reinstall a healthy cluster. A cluster whose record matches the desired
install inputs, is `installed`, and whose kubeconfig probe reports
Available=True has its install tasks skipped:
`cluster already installed and Available=True for desired install inputs; --override rebuilds only drifted objects, not a healthy in-sync cluster`.
This protects healthy clusters caught in a scoped `apply --override` aimed at
some other drifted object. A probe **error** (`oc` missing from PATH, API
refusing connection on a hard-down cluster) is treated as not-available under
override — override exists precisely to rebuild an unreachable cluster — so
the install tasks run. Without `--override`, the same probe error is a hard
refusal instead.

**Semantics (structural hash migration safety):** Install records carry both
`DesiredHash` (full install-input hash) and `StructuralHash` (same payload
with day-2-owned intent — cluster add-ons, per-node labels/taints — projected
out). The gate compares on `StructuralHash` when present so a day-2-only edit
reconciles in place; a legacy record with an empty `StructuralHash` falls back
to the full `DesiredHash` comparison, so upgrading bootwright never turns an
installed cluster into drift. Every record write stamps both hashes together
(`clusterInstallHashes`). The machine-infra *prepare* and *finalize* tasks
share the install task's structural projection for the same reason: without
it, any day-2 edit flipped them to structural drift and `apply` refused with a
false "would reinstall the machine — its disks wiped".

**Semantics (first bare-metal install warning):**
`BareMetalFirstInstallClusters` names, at confirm time, the clusters whose
planned nodeBoot (Redfish virtual-media) task has no convergence-safety record
— i.e. a first apply that will drive coreos-installer to disk-wipe physical
hosts. The CLI prints: `first apply will boot the OS installer on the
bare-metal host(s) of <names> ... coreos-installer will DISK-WIPE their target
disks`, and each BMC is also checked for an already-running OS (Redfish
occupancy guard) before boot. An already-recorded (owned) cluster is excluded
— its install-state gate and healthy-skip already protect it.

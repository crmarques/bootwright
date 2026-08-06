# CDI copies spec.pvc verbatim; only spec.storage consults the StorageProfile

**Symptom:** A KubeVirt machine's disk works but is pathologically slow, or a
clone of it never leaves `CloneInProgress`. The agent installer fails the node
with `installation stage Writing image to disk did not sufficiently progress in
the last 30m0s`, and the cluster then times out on bootstrap because etcd's
`DelayedHAScalingStrategy` never reaches three healthy members. The claim itself
reports `Bound` and the storage class is correct, so nothing looks wrong.

**Root cause:** A DataVolume can request its target two ways. `spec.pvc` is a
literal `PersistentVolumeClaimSpec` that CDI passes through unchanged — it never
consults the class's `StorageProfile` for it. Any field left out therefore takes
the *Kubernetes* default, and the default `volumeMode` is `Filesystem`. On a
Block class (Ceph RBD, including ODF external) that silently interposes a
filesystem inside the RBD image and a `disk.img` file inside the filesystem, so
the guest's virtio writes carry journal and extent-allocation overhead on top of
the network round trip. `spec.storage` is the inference API: CDI completes
`accessModes`, `volumeMode` and the filesystem-overhead-inflated size from the
`StorageProfile`, which is also what `virtctl image-upload` uses.

Two failure modes follow from the same mistake. Throughput collapse is the one
that trips the agent's no-progress watchdog under concurrency — a whole cluster's
machines writing RHCOS at once. Mode mismatch is the one that wedges clones:
smart-clone (csi-clone / snapshot) requires matching `storageClass` **and**
`volumeMode`, so a Filesystem target against a Block source disqualifies both
strategies and the host-assisted fallback then sits at `CloneInProgress`, which
CDI never resolves into `Failed`.

**Fix:** Request every DataVolume through `spec.storage` and name neither
`accessModes` nor `volumeMode` — the storage class is operator-supplied and only
its `StorageProfile` knows which pairs it supports. Hardcoding either field is a
guess that is wrong on some class.

**Corollary — create only, never update.** `volumeMode` is immutable once bound,
and CDI's validating webhook refuses a DataVolume spec update outright. A role
that re-applies its DataVolume template on every run will start failing against
claims an older template shape created. Guard the apply so it runs only when the
object is absent, or behind an authorized rebuild that deletes it first; a size
or shape change reaches a live machine that way, not through a plain apply.

Applies to both the per-machine root disk (`machine_substrate_kubevirt`) and the
agent ISO source and clones (`container_cluster_boot_kubevirt`). Pinned by
`TestKubeVirtRootDiskInheritsVolumeModeFromTheStorageProfile` and
`TestKubeVirtBootUploadsSharedAgentISOOncePerCluster` in
`internal/repo/checks/ansible_kubevirt_test.go`.

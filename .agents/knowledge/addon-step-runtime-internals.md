# Add-on step runtime: digest, inventory, output paths, cross-cluster edges

**Per-step digest:** `stepDigest` combines the step's shipped-content digest
with its RESOLVED inputs and target, so a change to either re-runs a
`run: onChange` step. The shipped Data Foundation exporter step deliberately
sets `run: always` instead, so rotated Ceph mon endpoints/keys keep converging
on every apply rather than being skipped by the content/input hash.

**Inventory and secrets:** Steps build an ad-hoc SSH inventory (never a
rendered inventory group) and materialize only scoped secrets: the target
machines' effective SSH material into a connection dir plus declared
`secretRefs` into the playbook-visible dir. Storage-cluster targets are
post-storage work and therefore use the owning cluster's
`cephadm.clusterSSH.user` and `clusterSSH.keyRef`, with the Machine access key
as the declared fallback when the cluster key is absent. Using the Machine
install identity here breaks exporter steps after root SSH is revoked. Direct
Machine and ContainerCluster targets retain the Machine `access.ssh` identity.
`target.limit: firstReachable` (the default) tries each resolved host in order
until one RUN SUCCEEDS — the exporter admin-node pattern — while `all` runs
once against every resolved host.

**Output persistence paths:** Captured step outputs persist at fixed paths:
secret outputs under
`clusters/<cluster>/secrets/addons/<addon>/steps/<step>/<name>` with mode 0600
and reclaimed after the step's manifests apply; non-secret outputs under
`clusters/<cluster>/runtime/addons/<addon>/steps/<step>/<name>`. The layout
deliberately mirrors the old Data Foundation attachment-details records.
Manifests with `reclaimRendered` have their rendered plaintext removed after
the `oc apply`.

**Kubeconfig lifetime:** The add-on task materializes the bound cluster's
encrypted kubeconfig once into its owner-only runtime scratch directory and
keeps that path alive across the entire apply, wait, effect, playbook-step, and
manifest-step lifecycle. Step playbooks receive the same scratch path as
`bootwright_kubeconfig`, and step-manifest `oc` calls use it directly. The
durable `clusters/<cluster>/secrets/kubeconfig` path contains an encrypted
envelope after install-secret capture; passing that path to `oc` produces its
generic missing/incomplete-configuration error rather than a decryption error.
Do not rematerialize independently inside a step or use the durable path as a
tool input.

**exportDetails resolution:** `resolveExportDetailsToken` loads the
operator-supplied external-cluster-details payload from a StorageExport's
`externalDetails.fromSecretRef` secret. Exports WITHOUT operator-supplied
details are produced by the add-on itself: a step runs the exporter on a Ceph
node, captures the payload as a declared output, and manifests consume it via
`{{ output <name> }}` instead. `state/desired` Normalize must NOT default
`externalDetails` — an absent field is the managed-Ceph shape, and the
retired `generated`/`sshExecution` arms must not come back.

**Cross-cluster DAG edges:** A step that targets a cluster other than the
bound one gets explicit plan edges via `stepCrossClusterDependencies`:
`storage.<ceph>` for a Ceph cluster referenced through the binding's
`fromInput` ref-chain, and `wait.<ocp>` for another container cluster — a pure
plan-time state walk. This is how the migrated Data Foundation shape works: no
compiled attachment task exists (pinned by
`TestPlanApplyAllPlansNoCompiledAttachmentTask`); the exporter-step add-on
(managed Ceph) pulls `storage.<ceph>` onto its own addon task, while a
manifest-only add-on (imported Ceph, `externalDetails.fromSecretRef`) needs no
Ceph-side dependency at all.

**Single owner of the attachment walk:** `inputs.StorageExportAttachments`
owns the "walk the dataFoundation storageExportAttachment effect bindings and
resolve each exportRef input to its StorageExport" pattern; callers needing a
subset must filter the returned Export rather than re-walking the bindings.

**Loader interaction:** The add-on directory is self-contained (add-on YAML
plus `playbooks/`, `roles/`, `collections/`, `manifests/` subtrees) and the
desired-state loader skips the `playbooks/`, `roles/`, and `collections/`
subtrees as Ansible content — only `manifests/` is the sanctioned home for
non-Bootwright Kubernetes YAML.

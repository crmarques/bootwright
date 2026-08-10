# Add-on step runtime: desired and observed digests, output paths, cross-cluster edges

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
`target.limit: firstReachable` (the default) tries resolved hosts in order only
while the machine-readable Ansible callback proves each failed candidate was
unreachable before any task executed. The runner exposes that proof as a typed
error. Any `ok` or `failed` callback event, timeout, missing/malformed callback
file, or later connection loss is an ordinary failure, and the step refuses to
try another host because the first run may have partially mutated state. The
callback result is cleared before every candidate run so stale evidence cannot
authorize a retry. `all` runs once against every resolved host.

**Output persistence paths:** Captured step outputs persist at fixed paths:
secret outputs under
`clusters/<cluster>/secrets/addons/<addon>/steps/<step>/<name>` with mode 0600
and reclaimed after the step's manifests apply; non-secret outputs under
`clusters/<cluster>/runtime/addons/<addon>/steps/<step>/<name>`. The layout
deliberately mirrors the old Data Foundation attachment-details records.
Manifests with `reclaimRendered` have their rendered plaintext removed after
the `oc apply`.

**Observed runtime digests:** A non-secret output with `format: sha256` carries
exactly `sha256:` plus 64 lowercase hexadecimal characters. Capture accepts one
trailing LF but stores the canonical value without it, both at the normal
runtime output path and under `StepRecord.ObservedDigests[outputName]`. The
failed/timed-out Ansible path captures every valid digest file that already
exists before returning; the storage target remains locked through that
capture and failed-record write. The successful path first reads and validates
the whole declared output set before persisting any member, so a malformed or
missing digest cannot leave a sibling secret output behind. An observed digest
is execution evidence only and never feeds `stepDigest`, `render.DesiredHash`,
or drift.

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

**Step-level storage mutation resources:** Every playbook step resolves its
target through `steps.StorageMutationTargets` before its first side effect. A
`StorageExport` target resolves through `storageClusterRef`; direct storage
targets and storage-owned static machines resolve to the same cluster names.
Unknown refs fail closed. After read-only `requires` polling, the per-run
scheduler coordinator atomically acquires every `storage:<name>` key and holds
it through the playbook, output capture, bound-cluster manifests, output
reclamation, and step record. Do not move the key to the add-on task's
`ResourceKeys`: that would hold it across operator install and the 45-minute
readiness budget. Any future step executor must use the shared coordinator;
running a storage-targeted playbook without it is an explicit refusal. This is
what prevents two Data Foundation exporters from interleaving a shared cephx
delete/remint while preserving parallelism for other storage clusters and
manifest-only steps.

**Single owner of the attachment walk:** `inputs.StorageExportAttachments`
owns the "walk the dataFoundation storageExportAttachment effect bindings and
resolve each exportRef input to its StorageExport" pattern; callers needing a
subset must filter the returned Export rather than re-walking the bindings.

**Loader interaction:** The add-on directory is self-contained (add-on YAML
plus `playbooks/`, `roles/`, `collections/`, `manifests/` subtrees) and the
desired-state loader skips the `playbooks/`, `roles/`, and `collections/`
subtrees as Ansible content — only `manifests/` is the sanctioned home for
non-Bootwright Kubernetes YAML.

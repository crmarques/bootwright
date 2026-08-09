# KubeVirt agent boot: DataVolume re-upload, VMI wait race, ownership label

**Symptom (ordinary apply stops installed guests):** Re-applying an unchanged
virtualized OpenShift cluster makes its KubeVirt VMs stop, the guest console
becomes unavailable, and later guest API work can report unrelated resources
such as a CatalogSource as unavailable.

**Root cause (apply/runtime coupling):** Installed-cluster reconciliation
skipped the agent ISO, node boot, and install wait tasks after proving the
cluster healthy, but did not skip the per-machine substrate task. That task
re-applied a VirtualMachine manifest with `spec.running: false`; KubeVirt
therefore stopped a VM that the boot role had previously started with
`virtctl`. The VM was reconciled rather than necessarily deleted, but the
result looked like a recreation and interrupted the guest API and console.

**Fix (apply semantics):** A verified installed cluster whose install inputs
still match skips its machine substrate tasks with the rest of the install
tasks. KubeVirt VMs use `runStrategy: Manual`, so a necessary declarative
reconcile never changes the current running/stopped state. Before creating,
reconciling, stopping, or deleting a same-name VM or DataVolume, the roles
probe live labels and fail closed unless the exact context, cluster, and node
identity is Bootwright-owned. `--mode create` rejects any pre-existing live
resource. A VM/root-disk delete is possible only when the controller passes an
exact machine-substrate reset or acknowledged OpenShift reinstall token; a
plain apply and a healthy `--mode rebuild` apply receive neither.

**Symptom:** A retried KubeVirt cluster boot leaves the VM running the *old*
agent ISO (the install never picks up regenerated installer inputs), or the
boot fails spuriously with `no matching resources found` from
`kubectl wait --for=condition=Ready vmi/<name>`.

**Root cause (stale ISO):** On a re-boot the VM may already be running and
holding the agent-ISO DataVolume. Deleting and re-uploading the DV under a
running VMI deletes an in-use volume and leaves the VMI on the stale ISO — the
later `virtctl start` tolerates "already running" and silently no-ops, so the
freshly uploaded ISO is never attached.

**Root cause (wait race):** `virtctl start` returns before virt-controller has
created the VMI object. When the controller is slow (busy hub, parallel
boots), an immediate `kubectl wait --for=condition=Ready` hits
`no matching resources found` and fails the boot even though the VM is coming
up fine.

**Fix:** `container_cluster_boot_kubevirt/tasks/main.yml` stops the VM before
deleting/re-uploading the agent-ISO DataVolume (mirroring
`machine_substrate_kubevirt/tasks/destroy.yml`), so the start re-attaches the
freshly uploaded ISO. After `virtctl start` it first retries until the
`vmi/<name>` object exists (`until: rc == 0`), then runs the condition wait
bounded by `bootwright_kubevirt_boot_vmi_timeout` (default 600s).

**Constraint (ownership label):** `virtctl image-upload` stamps no labels,
unlike the root DataVolume and VM templates which carry
`bootwright.io/managed-by`. The boot role explicitly labels the agent-ISO
DataVolume with the managed-by, context, cluster, node, and agent-ISO role
identity (with `--overwrite` so a re-upload stays idempotent). Without it, the
apply and destroy ownership gates cannot tell a Bootwright-owned DV from a
foreign DataVolume that merely collides on name in a shared namespace, and
would refuse or mis-delete.

**Symptom (pre-exact-label agent ISO):** Apply fails at `Refuse to modify
foreign KubeVirt DataVolume` for a same-name `*-agent-iso` DataVolume after
upgrading from a build that stamped only managed-by, cluster, and role.
A selected `destroy --clusters <name>` intentionally runs only the clusters
stage, so it leaves KubeVirt machine infrastructure and its DataVolumes
standing; `--stage infra` or a full unscoped destroy is the substrate teardown.

**Fix (bounded label upgrade):** The boot role accepts that one historical
agent-ISO label shape only when the context's exact `kubevirt-machine`
ownership record and the exact context/cluster/node-labelled VirtualMachine
both prove the same machine, the old managed-by/cluster/agent-ISO-role labels
match, and context/node labels are absent rather than contradictory. After
the shared apply-mode gate admits reconciliation, the role stamps the missing
labels before any stop or delete. Any partial or mismatched identity still
fails closed.

**Constraint (DV size):** The agent-ISO DataVolume size is
`bootwright_kubevirt_agent_iso_size` (default `4Gi`), sized for a typical
agent ISO (~1–1.5 GiB) with headroom. A larger release payload makes
`virtctl image-upload` fail on an ISO above this size — raise the variable,
do not shrink it.

**Symptom (managed-host upload TLS):** `virtctl image-upload` reaches the CDI
upload proxy route but fails with `tls: failed to verify certificate: x509:
certificate signed by unknown authority`. The managed OpenShift host
cluster's ingress wildcard certificate is signed by an ingress CA that is not
normally in the controller system trust store.

**Fix (managed-host upload TLS):** For a `hostClusterRef` provider, the boot
role reads the host cluster's published ingress CA from
`openshift-config-managed/default-ingress-cert`, writes it beside the
task-runtime materialized kubeconfig, and runs only `virtctl image-upload`
with `SSL_CERT_FILE` pointing at that CA. The task environment explicitly
retains `bootwright_proxy_env`. This mirrors the verified
ConsoleCLIDownload flow in `controller_virtctl`; it does not make
`--insecure` the default. A `kubeconfigRef` provider remains an
operator-owned external trust boundary and relies on the controller system
trust unless the explicit role override opts out of verification.

**Negative pin (`virtctl image-upload` kept deliberately):** uploading the
agent ISO through `virtctl image-upload` has no clean declarative CR equivalent
(evaluated 2026-06-28) — a DataVolume upload source still needs the imperative
upload-proxy stream. It stays as-is on purpose; do not re-propose replacing it
with a pure-CR flow.

## The power gate runs before any media mutation, and why the peer wait needs a
## stale-generation escape

`container_cluster_boot_kubevirt` refuses to boot install media on a machine
whose VirtualMachineInstance is not provably absent. That refusal sits
immediately after the owned-VirtualMachine assert, **before** the agent-ISO
label upgrade, the stale shared-source delete, and the ~1.3 GiB
`virtctl image-upload`. It used to sit after all of them, so re-applying a
cluster whose VMs were still running paid a full shared-media rebuild — up to
`bootwright_kubevirt_boot_iso_timeout` (1800s) — before a single machine
refused. `internal/repo/checks/ansible_power_gate_test.go` pins the ordering
against the three mutating task names; the vSphere path has always had the
equivalent rule.

Moving the gate earlier created one new failure mode that the ordering test
also pins. In a **mixed** cluster — the elected agent-ISO source machine is
running, its peers are stopped — the elected machine now refuses before
refreshing the shared source, so peers poll a source that is present but
stamped with an older `bootwright.io/agent-iso-generation`. The pre-existing
escapes did not cover that: the "never appeared" escape is gated on `rc != 0`,
so a present-but-stale DataVolume fell through to the full
`bootwright_kubevirt_iso_source_wait_retries` budget — `(1800+60)/5 = 372`
attempts at 5s, about 31 minutes — before falling back to a direct upload.

The `until` on `Wait for the shared KubeVirt agent ISO source DataVolume`
therefore carries a fourth clause: give up on the short
`bootwright_kubevirt_iso_source_appear_retries` budget when the DataVolume is
present, parses to more than five `|`-separated fields, and field 5 (the
generation) differs from `bootwright_kubevirt_iso_generation`. The field count
guard is load-bearing — without it a truncated or empty `stdout` raises an
index error inside the retry loop.

That clause is safe against a legitimate rebuild because the elected machine
**deletes** the stale source before re-uploading, so a peer observing a
rebuild-in-progress sees `rc != 0` (absent), never a present-but-stale
DataVolume. Present-and-stale is therefore positive evidence that no rebuild is
coming. Do not "simplify" the clause by dropping either the field-count guard
or the attempts floor: the floor is what covers the brief race where a peer
polls before the elected machine has issued its delete.

## A CDI claim outlives its DataVolume, so every recreate path must purge it

CDI creates the PersistentVolumeClaim as a dependent of its DataVolume, but the
claim can outlive one — held by a finalizer while a populator or prime claim is
still running. A new DataVolume of the same name cannot adopt that claim, so the
recreate stalls or the upload is refused with `No DataVolume is associated with
the existing PVC`. `container_cluster_boot_kubevirt/tasks/purge_media_pvc.yml`
is the shared delete-and-prove-gone helper for exactly this, and every path that
deletes a DataVolume intending to recreate it must call it.

Three such paths exist and all three now do: the agent-ISO media rebuild in
`container_cluster_boot_kubevirt`, the guest teardown in
`machine_substrate_kubevirt/tasks/destroy.yml`, and — added late, after it was
found missing — the authorized root-disk rebuild in
`machine_substrate_kubevirt/tasks/main.yml`. The last one matters most because it
is the *advertised remedy*: the volume-mode drift refusal a few tasks above it
tells the operator to run `bootwright apply --mode rebuild`, and following that
advice used to delete the DataVolume, immediately recreate it, and stall against
the claim the deletion had not finished releasing. Pinned by
`TestKubeVirtRebuildPurgesTheRootClaimItsDataVolumeLeavesBehind`.

**Constraint (a blank root disk that never provisions is a storage condition, not
a KubeVirt one):** the root DataVolume wait was a bare
`kubectl wait --for=condition=Ready --timeout=10m` with no registered result and
no failure message, so a stuck disk surfaced as raw kubectl stderr naming neither
the machine nor the cluster. It is now budgeted by
`bootwright_kubevirt_root_dv_ready_timeout` (the role had no `defaults/main.yml`
at all before this) and asserted with a diagnosis that reads the DataVolume and
claim phase, because the real causes sit under the storage class the provider
declares — no provisioner answering, no capacity, or a CSI driver that cannot
reach the external Ceph cluster backing it. Nine blank disks per hosted cluster
are requested at once (one task, `Forks=9`), so this is the path that feels a
storage hiccup first.

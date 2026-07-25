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
identity is Bootwright-owned. `--expect-new` rejects any pre-existing live
resource. A VM/root-disk delete is possible only when the controller passes an
exact machine-substrate reset or acknowledged OpenShift reinstall token; a
plain apply and a healthy `--converge-drifted` apply receive neither.

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

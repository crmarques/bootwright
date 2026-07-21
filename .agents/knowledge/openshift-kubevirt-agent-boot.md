# KubeVirt agent boot: DataVolume re-upload, VMI wait race, ownership label

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
DataVolume `bootwright.io/managed-by=bootwright` (with `--overwrite` so a
re-upload stays idempotent). Without it, the destroy ownership gate cannot
tell a Bootwright-owned DV from a foreign DataVolume that merely collides on
name in a shared namespace, and would refuse or mis-delete.

**Constraint (DV size):** The agent-ISO DataVolume size is
`bootwright_kubevirt_agent_iso_size` (default `4Gi`), sized for a typical
agent ISO (~1–1.5 GiB) with headroom. A larger release payload makes
`virtctl image-upload` fail on an ISO above this size — raise the variable,
do not shrink it.

**Negative pin (`virtctl image-upload` kept deliberately):** uploading the
agent ISO through `virtctl image-upload` has no clean declarative CR equivalent
(evaluated 2026-06-28) — a DataVolume upload source still needs the imperative
upload-proxy stream. It stays as-is on purpose; do not re-propose replacing it
with a pure-CR flow.

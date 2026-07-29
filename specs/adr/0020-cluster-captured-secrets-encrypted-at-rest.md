# ADR 0020: Cluster-Captured Secrets Are Encrypted at Rest

## Status

Accepted

## Context

ADR 0016 gives declared `kind: Secret` material an encrypted-at-rest home: the
per-context `ContextStore` under `contexts/<context>/secrets/`, AES-256-GCM
envelopes keyed by a root-owned-file keyring, strict `0700`/`0600` enforcement,
symlink and ownership checks.

A second tier of credentials never got that control. When a cluster installs,
Bootwright *captures* material it did not declare — the OpenShift agent
installer's `kubeconfig` and `kubeadmin-password`, and a managed Ceph cluster's
one-time cephadm `dashboard` admin password — under
`clusters/<cluster>/secrets/`. These were written by Ansible `copy` tasks as
plain files, protected only by `0700`/`0600` Unix permissions, with no
encryption layer. Anyone able to read the data root directly, or restore a
backup of it, sees these credentials in the clear. Destroying a storage
cluster also never removed its captured `dashboard-password` — a gap that sat
next to this one since nothing else touched that file's lifecycle either.

`kubeconfig` is not purely a display value like the other two: `oc`/`kubectl`,
several internal Go call sites, and Ansible add-on steps all need a real
plaintext file to point `--kubeconfig` at, both during the same apply run that
installs the cluster and on every later reconciliation check against an
already-installed cluster.

## Decision

Every cluster-captured credential is encrypted at rest using the same
mechanism as declared secrets, reusing the envelope mechanism owned by
`internal/secrets.ContextStore`: a dedicated `ContextStore` instance is
constructed **per cluster**, rooted at `clusters/<cluster>/secrets/` instead of
the context-level `contexts/<context>/secrets/`. Each cluster gets its own
AES-256-GCM key and its own keyring subdir at
`clusters/<cluster>/secrets/.bootwright/`, isolated from every other cluster
and from the context secret store — so two clusters can each have a
"kubeconfig" entry without collision. `dashboard-password`, `kubeconfig`, and
`kubeadmin-password` are all written under `MaterialPrimary`, which maps to a
bare filename with no suffix, so the encrypted envelope lands at exactly the
path these files occupied before this change.

Ansible roles are unchanged: `dashboard_secret.yml` and `wait_install.yml`
still write plaintext to that same path. Immediately after the capturing
apply task (`storageCluster` or `installWait`) succeeds, Go calls
`ContextStore.MigratePlaintextMaterial` for the captured credential names on
the per-cluster store, converting each plaintext file just written into a
ciphertext envelope in place before the task's result is returned. The
targeted method is required because the same secrets root also contains the
hierarchical `addons/` output area, which is not context-store material.
This conversion is part of the capture boundary. Normal cluster-secret reads
accept only encrypted envelopes and never migrate plaintext implicitly.

Captured `kubeconfig` is additionally consumed programmatically. Add-on
apply/wait, node-config apply, the pull-secret merge effect, Ansible step
extra-vars, every plan-time `ClusterAvailabilityChecker.Available` probe, and
KubeVirt `hostClusterRef` preflight all perform a strict encrypted read through
the owning per-cluster store. A file-based consumer receives a fresh `0600`
scratch copy below an existing current-process-owned `0700` private runtime
parent. The copy is bounded to its callback and removed on every callback
return, including errors.

KubeVirt Ansible execution follows the same managed-host boundary.
`workflow.Run` materializes the `hostClusterRef` kubeconfigs required by the
current playbook or task beneath that task's runtime secrets directory and
keeps them available for the bounded Ansible invocation. The renderer projects
each path into its machine components and into the
`bootwright_kubevirt_host_kubeconfigs` host-to-path map used by controller
`virtctl` provisioning. VM provisioning, boot, and destroy roles therefore
never receive a managed host's durable encrypted path. Dry-run emits only the
logical managed-host path and creates no plaintext copy.

External `kubeconfigRef` follows the declared `Secret` contract instead.
Preflight decrypts context material or copies an explicit file source through
the declared-secret resolver into its private runtime scratch. During Ansible
execution, context material resolves from the task's materialized context
secret store, while a `source.file` secret in source mode remains the
operator-owned source path. That external file is authored secret input, not a
cluster-captured encrypted envelope.

Durable encrypted paths are never passed directly to `oc`, `kubectl`, or
`virtctl` during real execution. Kubeconfig plaintext exists on disk only for
its bounded callback or Ansible invocation, never as durable cluster state; an
abnormally terminated process can leave that scratch copy behind, which is why
its parent directory is current-process-owned and `0700`.

Human-facing reveals (`bootwright cluster info --secrets`, `bootwright cluster
kubeconfig`) decrypt straight to memory or stdout — no scratch file at all.

Destroying a cluster deletes the encrypted material through the same store
(`ContextStore.Delete`) rather than a raw `os.Remove`, for both container and
storage clusters — which also closes the storage-cluster `dashboard-password`
cleanup gap.

## Consequences

- `clusters/<cluster>/secrets/{dashboard-password,kubeconfig,kubeadmin-password}`
  hold JSON AES-256-GCM envelopes, not raw credential bytes. `sudo cat` on
  these paths no longer works; the documented recovery command is `bootwright
  cluster info --secrets` (passwords) or `bootwright cluster kubeconfig --name
  <cluster>` (kubeconfig).
- File-presence checks (`os.Stat`, `FileStatus`) are unaffected — the file
  still exists at the same path, so cluster-readiness and artifact-status
  reporting needed no changes.
- `internal/clusteraccess` gained a new dependency on `internal/secrets`
  (added to the import-matrix allow-list); no cycle risk, since `internal/secrets`
  does not import back into `internal/clusteraccess` or `internal/converge`.
- Non-encrypted cluster-secret material is rejected by access, preflight, and
  runtime execution paths. Plaintext is accepted only at the explicit
  post-capture conversion boundary after the producing Ansible task succeeds.
- A capture terminated between the Ansible write and the Go conversion leaves
  plaintext at the captured path. Reads refuse it rather than migrating it
  implicitly, so the credential stays unreadable until the next successful run
  of the capturing task, whose `MigratePlaintextMaterial` call is idempotent
  and converts the file in place.

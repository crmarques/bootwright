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
several internal Go call sites, and Ansible add-on hooks all need a real
plaintext file to point `--kubeconfig` at, both during the same apply run that
installs the cluster and on every later reconciliation check against an
already-installed cluster.

## Decision

Every cluster-captured credential is encrypted at rest using the same
mechanism as declared secrets, reusing `internal/secrets.ContextStore`
unchanged: a dedicated `ContextStore` instance is constructed **per cluster**,
rooted at `clusters/<cluster>/secrets/` instead of the context-level
`contexts/<context>/secrets/`. Each cluster gets its own AES-256-GCM key and
its own keyring subdir at `clusters/<cluster>/secrets/.bootwright/`, isolated
from every other cluster and from the context secret store — so two clusters
can each have a "kubeconfig" entry without collision. `dashboard-password`,
`kubeconfig`, and `kubeadmin-password` are all written under `MaterialPrimary`,
which maps to a bare filename with no suffix, so the encrypted envelope lands
at exactly the path these files occupied before this change.

Ansible roles are unchanged: `dashboard_secret.yml` and `wait_install.yml`
still write plaintext to that same path. Immediately after the capturing
apply task (`storageCluster` or `installWait`) succeeds, Go calls
`ContextStore.MigratePlaintext` on the per-cluster store, converting whatever
plaintext was just written into a ciphertext envelope in place, before the
task's result is returned. `MigratePlaintext` is idempotent — a no-op once the
file is already encrypted — so this runs after every succeeding task, not just
the one that captured the credential, and self-heals any pre-existing
plaintext file left by a cluster installed before this change.

`kubeconfig` is additionally consumed programmatically. Every such call site —
add-on apply/wait, node-config apply, the pull-secret merge effect, Ansible
hook extra-vars, and every plan-time `ClusterAvailabilityChecker.Available`
probe — now decrypts kubeconfig into a fresh, task-scoped scratch file via
`ContextStore.MaterializeSelected`, uses it, and removes it unconditionally
afterward. Kubeconfig plaintext exists on disk only for the span of one
`oc`/`kubectl` invocation or one Ansible hook run, never as the durable state.
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
- A cluster installed by a pre-ADR-0020 Bootwright still has plaintext
  material at these paths; the next access through any of the paths above
  (an apply task succeeding, a `withMaterializedClusterKubeconfig` call, or a
  `RevealClusterSecret`/`Kubeconfig` read) transparently encrypts it in place
  before use. There is no separate migration command to run.

# Plaintext secret staging: modes, cleanup, and the SIGKILL sweep

**Stale runtime-secrets sweep:** a run materializes plaintext BMC/SSH/pull
secrets under per-run/task runtime secrets dirs — two nesting shapes exist,
`tasks/<id>/runtime/secrets` and `tasks/<id>/artifacts/runtime/secrets` — and
only DEFERS their removal, so a SIGKILL'd run would leave plaintext behind
forever with nothing else cleaning it. `sweepStaleRuntimeSecrets`
(`internal/converge/workflow/run.go`) runs right after lease acquisition:
the lease is the single point of mutual exclusion, so once `liveRunID` holds
it, every other history entry belongs to a finished or killed run whose
plaintext is safe to reclaim; the live run's own dirs are never touched.
Known limitation: the unsupported cross-host shared-runsDir case, where a
remote holder's lease can go stale while its run is still live, is a
pre-existing split-brain concern this sweep does not solve.

**All-or-nothing store materialization:** `ContextStore.materialize`
(`internal/secrets/store_ops.go`) writes plaintext copies one material at a
time and, on any failure (a later material fails to decrypt or write), removes
the entire target directory in a defer. Callers register their own cleanup
defer only on the success path, so without this a failed materialization would
leave partial plaintext secrets on disk. Any material found in a non-encrypted
state also aborts materialization. Pinned by the `store_test` case that
corrupts the second (sorted) material so decrypt fails mid-loop after the
first was already written.

**Cluster stores are not flat:** A per-cluster `ContextStore` shares
`clusters/<cluster>/secrets/` with the hierarchical `addons/` secret-output
area. Post-install capture therefore uses `MigratePlaintextMaterial` only for
the known `kubeconfig`, `kubeadmin-password`, or `dashboard-password` key.
Access paths are strict encrypted reads and never invoke migration. Broad
`MigratePlaintext` is reserved for the flat context secret store: running it
against a cluster root interprets `addons/` as a primary material named
`addons` and fails with "is not a regular file" before an add-on runner starts.

**Bounded single-material staging:** `ContextStore.WithMaterialized` and the
resolver equivalent require an existing current-process-owned `0700` private
runtime parent. The store decrypts exactly one selected material; the resolver
either decrypts context material or copies an explicitly declared external
file source. Both write a `0600` file under a new `0700` scratch directory,
invoke the consumer callback, and remove the scratch directory on every
callback return, including callback failure. Context-store reads never fall
back to plaintext, and neither helper migrates durable material.
`workflow.Run` nests these callbacks for the managed `hostClusterRef`
kubeconfigs selected by the current KubeVirt-capable playbook or task, keeping
them available for one bounded Ansible invocation.

Callback cleanup is not crash recovery: an abrupt process termination cannot
run deferred removals. Task runtime secret directories are reclaimed by the
post-lease stale sweep described above. A different private runtime parent
must not be described as crash-reclaimed unless its owner supplies an
equivalent sweep.

**Git authentication has the same two-layer lifetime.** An SSH-backed
`CustomPlaybook` decrypts its key into `runs/content/git/git-key-*/id` only for
one `gitcontent.Fetch` call. `ResolveGitSources` invokes its cleanup explicitly
after both success and failure, and a cleanup failure is itself returned because
silently leaving the key is not a successful plan. HTTPS askpass directories
share the `git-cred-*` prefix and the same crash-recovery rule. Every registered
real mutator sweeps both prefixes immediately after it acquires the command
lease, before it reads or changes desired/runtime state; placing the sweep in
`AcquireCommandRunLease` closes it over future mutating verbs and lets destroy
or context update clean residue from a killed apply. Read-only commands acquire
no lease and therefore never turn inspection into cleanup. The sweep removes
only those two temporary prefixes and preserves commit-keyed fetched content.
`TestResolveGitSourcesRemovesSSHCredentialOnSuccessAndFailure` and
`TestCommandLeaseSweepsGitCredentialResidueAndKeepsContent` pin both layers.

**MkdirAll does not fix modes:** `ensureLocalDir`
(`internal/render/filesystem.go`) must explicitly `Chmod` after `MkdirAll`:
`MkdirAll` is a no-op on an existing directory and leaves its mode untouched,
so a rendered dir created earlier under a `0022` umask would silently stay
`0755` and expose subsequent secret material to other local users. `render.All`
also chmods a pre-existing rendered directory back down to `0700`.
`TestAllTightensLooseRenderedDirMode` and the `recordingFS` mode-invariant
tests (every `MkdirAll` matched by `Chmod 0700`, every `WriteAtomic` at
`0600`) guard this; the `0o700`/`0o600` literals are duplicated in the tests
on purpose so drifting a production constant forces reckoning with the
security implication.

**Preflight checks reject over-broad modes only:** the secret-material
permission checks fail on any group/other bit (`mode & 0o077 != 0`), not on
tighter-than-default modes: a hardened read-only `0400` secret file and a
`0500` secrets directory must pass while `0640`/`0604`/`0750`/`0705` fail.
Pinned by `TestSecretFileCheckAllowsOwnerOnlyModes` and
`TestSecretsDirCheckAllowsOwnerOnlyModes`.

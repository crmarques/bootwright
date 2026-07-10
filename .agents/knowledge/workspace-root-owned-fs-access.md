# Reading the root-owned managed workspace under a self-escalated run

**Root-owned artifacts need `CommandOutputLocalRoot`.** Under a sudo
self-escalated `apply`, the plain `Deps.CommandOutput` de-escalates back to the
invoking caller (via `callerio`), who cannot traverse the `0700` root-owned
managed workspace. Preflight probes that read root-owned workspace artifacts —
a host-cluster kubeconfig under `clusters/<name>/secrets/`, the managed Ansible
venv interpreter — must use `Deps.CommandOutputLocalRoot`, which runs as the
local-root process itself; otherwise a caller-run `kubectl`/`python` fails with
`EACCES` on a file that is present and valid. This mirrors the apply path, which
runs `oc`/`kubectl` against the same kubeconfig as root. Pinned by
`TestKubeVirtHostClusterChecksRunAsLocalRoot` and
`TestVSpherePyvmomiCheckRunsAsLocalRoot`.

**Operator-owned material needs caller-owned stat.** File-sourced secret
material (`Secret` `spec.source.file`) is operator-owned and must be statted
through `Deps.StatExternalPath` (caller-owned access, `secret.StatExternalFile`),
never through the root-managed `StatPath`, so preflight can see files in the
operator's home even under a self-escalated run. Pinned by
`TestSecretRefChecksStatFileSourcesAsCallerOwned`.

**`EnsureDir` create-then-tighten.** `os.MkdirAll` leaves a pre-existing
directory's mode untouched, so `safefs.EnsureDir` follows `MkdirAll` with an
explicit `Chmod` to tighten an existing directory to the requested mode; it is
the single owner of this idiom for the root-managed private trees.
`WriteFileEnsuringDir` (the shared save path for state and secret records)
relies on it to re-chmod the parent to `0700` even when the parent already
existed with looser permissions.

**Symlink-safe input copy.** `copyInputTree` opens each source file with
`O_RDONLY|O_NOFOLLOW` and reads from the descriptor instead of re-resolving the
path, closing the TOCTOU window between its `Lstat` symlink check and the read:
a final path component swapped for a symlink after the check makes the open fail
(`ELOOP`) rather than being followed out of the source tree. Symlinks anywhere
in the source are rejected outright; copied directories are `0700` and files
`0600` under the root-managed tree.

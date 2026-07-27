# Non-root Ceph node access: the three traps

Bootwright can orchestrate Ceph nodes as a dedicated non-root account and revoke
root SSH (`Machine.spec.access.rootLogin`, `StorageCluster.spec.ceph.cephadm.clusterSSH`,
ADR 0019). Three things about that path are non-obvious and expensive to
rediscover.

## 1. `access.ssh.user` is chosen before install, never changed after

**Root cause:** `Machine.spec.access.ssh.user` is not merely "how we log in". It
is the identity `machine_os_install_anaconda/tasks/probe_existing.yml` uses to
decide the node is *already installed*:

```text
Probe managed OS SSH authentication before install   -> bootwright_os_pre_ssh_auth
Resolve managed OS readiness before install          -> bootwright_managed_os_already_ready = (rc == 0)
```

Every downstream guard hangs off that one fact. Both refusals —
*Refuse foreign or unmarked reachable managed OS* and *Refuse drifted
Bootwright-owned managed OS without override* — carry
`when: bootwright_managed_os_already_ready | bool`, and

```text
bootwright_managed_os_install_required = (not already_ready) or force_rebuild
```

So repointing `access.ssh.user` at an account that does not yet exist makes the
probe fail authentication and `already_ready` false. Since the 2026-07-23
fail-closed hardening that lands on the *unverifiable* refusal (reachable, no
identity authenticated) rather than a silent reinstall — but before it, both
mode refusals were skipped (they are conditioned on the node having answered)
and `install_required` became true, which wiped the node. The field is
additionally folded into the install-marker desired hash
(`internal/render/inventory/vars_machine_os_marker.go`), so even a node that
still answers is classified as drifted.

**Contract:** `access.ssh.user` is the *install-window* identity and is fixed
for the life of the machine — but its *name* is a pre-install choice, not
necessarily `root`:

- **Retrofit shape** (nodes already installed as `root`): `access.ssh.user`
  stays `root` and `cephadm.clusterSSH.user` names a second account Bootwright
  provisions *in addition*. This is what `rootLogin: revoke` exists for.
- **Collapsed shape** (nodes not yet installed): both fields name the same
  account. `ks.cfg.j2` creates a non-root `ssh_user` with the machine access key
  authorized, `wheel`, and `%wheel NOPASSWD: ALL`, and emits neither
  `PermitRootLogin yes` nor an unlocked `rootpw` — so the account exists before
  the first probe and root SSH is never enabled. `os.provided: true` nodes get
  the same account prepared out of band.

Validation therefore does **not** reject `access.ssh.user == clusterSSH.user`;
`v1alpha1.StorageClusterNodeAccountIsInstallIdentity` reports the collapsed
shape and the renderer passes it to the role as
`bootwright_node_access.installIdentity`. What it *does* reject is
`clusterSSH.user` resolving to `root` while a node's `access.ssh.user` is
non-root — cephadm would orchestrate as an account the node does not carry.

**The ordering that makes the collapsed shape safe:** in that shape Bootwright
is *already connected as* the orchestration account, and its only privilege is
the kickstart's `wheel` membership. `storage_node_access` must therefore install
`/etc/sudoers.d/60-bootwright-<user>` **before** `gpasswd --delete <user> wheel`
— which is why the wheel-removal tasks live at the end of `sudoers.yml`, not in
`account.yml` where they read more naturally. Moving them back cuts off sudo
mid-role and strands the node. The order is harmless in the retrofit shape,
where `useradd` never put the account in `wheel`.

## 2. The `User` line in the cephadm ssh_config needs a two-space indent

**Root cause:** cephadm's mgr module rewrites the SSH config it holds for the
cluster with a literal regex when the SSH user changes:

```python
re.sub(r"(\s{2}User\s)(.*)", r"\g<1>" + user, ssh_config)
```

It matches **exactly two whitespace characters** before `User`. A config whose
`User` line is flush-left, tab-indented, or four-space-indented is not matched,
`ceph cephadm set-user` silently leaves the old user in place, and the cluster
keeps orchestrating as whoever it bootstrapped with.

**Constraint:** the rendered
`storage_cluster_cephadm/tasks/phases/bootstrap_steps/stage_inputs.yml`
*Render cephadm SSH config* block writes

```text
Host *
  User <user>
  IdentityFile ...
```

The two-space indent on `User` is load-bearing upstream compatibility, not
formatting. Do not reflow that heredoc, do not let a linter normalize it, and do
not switch it to a template that indents differently.

## 3. `NOPASSWD: ALL` is forced by cephadm, not chosen for convenience

**Root cause:** when its SSH user is not `root`, the cephadm mgr module
`sudo`-wraps *every* remote command it issues over the connection, and cephadm's
own user-provisioning helper writes exactly `<user> ALL=(ALL) NOPASSWD:ALL`. The
orchestrator's remote command set is arbitrary and changes between releases.

**Constraint:** a command-scoped or `Cmnd_Alias`-restricted sudoers policy
cannot orchestrate a cephadm cluster. Bootwright therefore writes
`/etc/sudoers.d/60-bootwright-<user>` with `Defaults:<user> !requiretty` and
`<user> ALL=(ALL) NOPASSWD: ALL`, scoping only the `requiretty` relaxation to
that principal. Do not propose narrowing the sudoers rule; do not describe the
resulting posture as privilege separation. The honest claim, and the one the
specs and docs make, is: no standing root SSH, a named auditable principal,
key-only auth, and a cluster credential revocable without touching root.

## Related invariants

- `clusterSSH.keyRef` is **required** whenever `clusterSSH.user` is non-root
  (not merely when a node revokes root): without it the
  cluster identity falls back to node[0]'s `Machine` access key, and
  `cephadm bootstrap --ssh-private-key` persists that controller-held key into
  the Ceph mon config-key store — where it would open a passwordless-sudo
  account. Requiring a dedicated generated `sshKeyPair` ends the cross-trust-domain
  reuse.
- Ordering is verify-before-revoke: the account must answer `sudo -n true` on
  every topology node before `PermitRootLogin no` is written anywhere, and again
  after the `sshd` reload (which is itself gated on `sshd -t`). Never reorder
  these; a failed revoke with root already gone is unrecoverable in band.
- Day-2 the user is reconciled with `ceph cephadm get-user` / `set-user` on an
  already-bootstrapped cluster — not by re-bootstrapping.
- Node access state is recorded at `/etc/bootwright/access-marker.json`, mode
  `0644`, non-secret. It is a sibling of the install marker, not part of it: the
  install marker's hash must not absorb access-posture fields, or hardening
  would trigger the reinstall this whole design exists to avoid.

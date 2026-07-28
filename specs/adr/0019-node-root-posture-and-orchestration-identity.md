# ADR 0019: Node Root Posture and Ceph Orchestration Identity

## Status

Accepted, with the key-separation clause superseded by
[ADR 0024](0024-machine-access-union-and-cluster-owned-node-login.md). A Ceph
node the cluster installs now derives its whole login from `clusterSSH`, so the
cluster key *is* that node's key; a `Secret` a `Machine` authors as its own
access key still may not be named as the cluster identity. The two-identity
model below remains the shape for a node the cluster does not install.

## Context

Bootwright reached every Ceph node as `root`. The machine access key is
authorized for `root`, `cephadm bootstrap` runs with the implicit
`--ssh-user root`, and every command the cephadm mgr later issues over that
connection lands as `root`. An environment whose policy forbids standing root
SSH could not be onboarded at all.

Making the node non-root is two changes to two different objects with two
different lifecycles:

- Whether an installed OS still accepts a `root` login is a property of that
  machine's operating system. It survives the machine leaving the cluster and
  must be expressible on a machine that has not been bound to a cluster yet.
- Which account cephadm orchestrates as is a property of the cluster. cephadm
  stores exactly one value per cluster (`mgr/cephadm/ssh_user` in the mon
  config-key store, read and written with `ceph cephadm get-user` /
  `set-user`), and the key it distributes to reach that account is likewise
  cluster-scoped.

Collapsing the two into one field forces one of them into the wrong object: a
per-machine OS posture becomes a cluster field, or a cluster-wide
orchestration identity becomes something each machine re-declares and can
disagree about.

The obvious lever — repointing `Machine.spec.access.ssh.user` at a non-root
account — is a trap, not a shortcut. That field is the **install-window**
identity. It is folded into the managed-OS install-marker hash
(`internal/render/inventory/vars_machine_os_marker.go`) and, more sharply, it
is the identity `machine_os_install_anaconda`'s `probe_existing.yml` uses to
prove a node is already installed. When authentication as that user fails,
`bootwright_managed_os_already_ready` is `false`; both refusal guards — the
foreign/unmarked-host refusal and the drifted-without-override refusal — are
gated on `already_ready`, so both are skipped, and
`bootwright_managed_os_install_required` becomes `true`. Repointing the field
on an installed fleet therefore silently reinstalls it.

cephadm's own non-root support also fixes the shape of the privilege policy
rather than leaving it open. When `ssh_user` is not `root`, the cephadm mgr
module `sudo`-wraps every remote command it issues, and cephadm's own
user-provisioning helper writes exactly `<user> ALL=(ALL) NOPASSWD:ALL`. A
command-scoped sudoers policy is not viable against an orchestrator whose
command set is arbitrary and version-dependent.

## Decision

Two authored fields at two altitudes, each owned by the object the property
belongs to.

### `Machine.spec.access.rootLogin` — the OS posture

`keep` (the default) or `revoke`. `revoke` writes
`/etc/ssh/sshd_config.d/01-bootwright-access.conf` containing
`PermitRootLogin no`, validates the resulting configuration with `sshd -t`
before reloading, and re-authorizes nothing. It is reversible: setting the
field back to `keep` removes the drop-in and restores root's authorized key.
The field is legal only on a machine that a managed Ceph `StorageCluster`
lists as a topology node under a non-root orchestration account — otherwise
revoking would leave no account able to reach the machine, and validation
refuses it.

### `StorageCluster.spec.ceph.cephadm.clusterSSH` — the orchestration identity

```yaml
clusterSSH:
  user: cephadm
  keyRef: ceph-cluster-ssh-key
```

`user` is the **post-install** identity: the account cephadm orchestrates
every host as (`cephadm --ssh-user`, later `ceph cephadm set-user`) and the
account Bootwright itself connects as once the node is provisioned. It
defaults to `cephadm` when any topology node's `Machine` revokes root, and to
`root` otherwise. `keyRef` names the `sshKeyPair` `Secret` that is cephadm's
cluster identity.

This replaces `cephadm.clusterSSHKeyRef` and `cephadm.clusterSSHUser`. `v1alpha1`
breaks cleanly (no alias, no shim); strict decoding rejects the retired
spellings with a field-not-found error.

The retired pair also carried an asymmetry — `clusterSSHUser` was honoured
only when `clusterSSHKeyRef` was set, and otherwise silently deferred to the
first topology host's `access.ssh` user. The nested block has one meaning for
`user` regardless of whether `keyRef` is authored.

### The account Bootwright provisions

On every topology node, for a non-root `clusterSSH.user`, Bootwright creates
the account with a locked password and no `wheel` membership, authorizes the
machine access public key in its `authorized_keys`, and writes a per-user
sudoers drop-in at `/etc/sudoers.d/60-bootwright-<user>` containing exactly:

```text
Defaults:<user> !requiretty
<user> ALL=(ALL) NOPASSWD: ALL
```

`NOPASSWD: ALL` is deliberate and is the only policy cephadm can be
orchestrated under, for the reason recorded in the Context. The `!requiretty`
default is scoped to that one principal rather than relaxed globally.

The honest security delta is therefore narrow, and is stated as such
everywhere it is documented: no standing root SSH, a named auditable
principal, key-only authentication, and a credential that can be revoked
without touching root. It is **not** privilege separation — the account can
become root on demand.

### `clusterSSH.keyRef` is required whenever `clusterSSH.user` is not root

Without it, cephadm's cluster identity falls back to the first topology
node's `Machine` access key. `cephadm bootstrap --ssh-private-key` then
persists that key — Bootwright's controller-held machine administration
credential — into the Ceph mon config-key store, where the cluster's mgr can
read it. Before this change that key opened `root`; after it, it opens an
account with passwordless sudo, which is the same thing. Requiring a
dedicated generated `sshKeyPair` ends the cross-trust-domain reuse: the
controller's key stays in the controller's domain, and the key the cluster
holds only ever opens the cluster's own orchestration account.

### Verify before revoke

Ordering is fixed and is a safety property, not an implementation detail. The
new account is proved to answer `sudo -n true` on **every** topology node
before `PermitRootLogin no` is written on any of them, and is re-proved after
the `sshd` reload. A node whose account does not answer stops the run with
root still reachable.

### The two identities may be the same account, but only from the start

The install-window identity is fixed for the life of the machine. It does not
have to be `root`.

A fleet that has already been installed as `root` keeps the two names distinct:
`access.ssh.user` stays `root`, `clusterSSH.user` is the account Bootwright
provisions afterwards. Repointing `access.ssh.user` at the orchestration
account *on such a fleet* is still the trap described in the Context, and is
still the reason hardening is expressed with `rootLogin` rather than by
rewriting the field.

A fleet whose nodes carry the orchestration account from their first boot may
instead name it in both places. `ks.cfg.j2` already creates a non-root
`access.ssh.user` with the machine access key authorized, `wheel` membership,
and a `NOPASSWD` grant, and it never writes `PermitRootLogin yes` or an
unlocked root password in that shape — so the account exists before the first
probe, the install-window identity never changes, and the node never accepts a
root login at all. On a `spec.os.provided: true` machine the same account is
prepared out of band; Bootwright reconciles rather than creates it.

Bootwright therefore does not reject the collision. What protects the fleet is
the runtime gate, not a static rule: an installed node that stops answering as
its `access.ssh.user` fails the ownership probe **closed** — it is refused, not
reinstalled — so redefining the identity on a live fleet blocks the apply
instead of wiping it.

The role adapts rather than branching into a second code path. When the two
names are equal there is no account to create, so `account.yml` finds it
present; the named sudoers grant is written **before** `wheel` membership is
dropped, because in that shape `wheel` is the only thing making the connection
Bootwright is already using privileged. That ordering is safe for the distinct
shape too, where the freshly created account was never in `wheel`.

### `clusterSSH.user` must match a non-root node account

If any topology node's `access.ssh.user` is non-root while `clusterSSH.user`
resolves to `root`, cephadm would orchestrate every host as an account the
node does not carry. Validation refuses and names the account to set.

### Recorded node access state

Each node records its access state at `/etc/bootwright/access-marker.json`,
mode `0644`. It is non-secret — account name, root-login posture, and the
paths Bootwright owns — and is the record that makes reversal and day-2
reconciliation idempotent.

## Consequences

- Hardening never touches the install-window identity, so it cannot trigger a
  managed-OS reinstall. This is the whole reason the lever is a new field
  rather than a new value for `access.ssh.user`. Naming the orchestration
  account in `access.ssh.user` is a decision taken *before* a machine is
  installed, not a hardening step applied to one that already is.
- A cluster whose nodes are installed with the orchestration account as their
  install-window identity never enables root SSH at any point, and needs no
  second account. `rootLogin: revoke` remains meaningful there: it is what
  turns the implicit posture (root password locked, no authorized key) into a
  declared `PermitRootLogin no`.
- The break is authored-schema-only: no shipped example authors either retired
  field, so the rename costs no example edits. Any operator state that authors
  `clusterSSHKeyRef` or `clusterSSHUser` fails to decode with a named field
  error rather than being silently ignored.
- A revoking cluster now carries a second SSH secret (the dedicated cluster
  key) beyond the machine access key. `source.generated` covers it, but
  `bootwright secret generate` must run before apply.
- Reversal is a normal converge: `rootLogin: keep` on the next apply removes
  the sshd drop-in and re-authorizes root. The orchestration account is not
  removed by reverting the posture — a cluster keeps orchestrating as
  `clusterSSH.user` until that field changes too, and cephadm's own
  `ceph cephadm set-user` is the day-2 reconciliation path for an already
  bootstrapped cluster.
- The posture is Ceph-scoped by construction. A machine that no managed Ceph
  cluster claims cannot revoke root through Bootwright today; other node
  classes (OpenShift nodes, service machines) are unaffected.

### Alternatives rejected

- **A new `harden` apply stage.** The stage model is a strict linear ordering
  over `ProvisioningStages()` — `fabric`, `machines`, `deps`, `base`,
  `add-ons` — where `--stage`/`--through` select an inclusive range over that
  list and the sub-phase names are pinned to it by
  `internal/converge/provisioning_stage_pin_test.go`. It cannot host a stage
  that must run *inside* the storage work (after the account exists and
  before cephadm bootstraps), and adding one would also widen the authored
  `Playbook.spec.stage` vocabulary for a concern no operator
  playbook targets. Account provisioning and revocation are ordered steps of
  the storage-cluster task instead.
- **An `Environment`-wide fan-out** (a fleet default that revokes root on
  every machine). Deferred, not refused on principle: ten shipped bastion and
  service `Machine`s declare `access.ssh` with no `user`, so a fleet-wide
  default would silently retarget them, and a bastion is precisely the machine
  a failed revoke locks you out of. A fleet default needs its own opt-out
  grammar and a per-class scope before it is safe.

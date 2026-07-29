# ADR 0019: Node Root Posture and Ceph Orchestration Identity

## Status

Accepted in part.
[ADR 0024](0024-machine-access-union-and-cluster-owned-node-login.md)
supersedes the access-block shape and the `clusterSSH.user` default;
[ADR 0027](0027-bootwright-owns-the-login-it-installs.md) supersedes the
install-window-identity model and the `rootLogin` validity rule, and restores
the key separation this ADR argued for and ADR 0024 narrowed. What survives:
`Machine.spec.access.rootLogin` as the OS-posture field, `clusterSSH` as the
cluster's orchestration identity, the provisioned-account sudoers policy, and
verify-before-revoke.

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
account — was a trap, not a shortcut. That field was the **install-window**
identity: folded into the managed-OS install-marker hash and used by the
readiness probe to prove a node is already installed, so repointing it on an
installed fleet silently reinstalled that fleet. ADR 0027 closed the trap by
construction — a machine Bootwright installs authors no access at all — but the
hazard is why the hardening lever below is a separate field rather than a new
value for an existing one.

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
The field requires `spec.access.ssh` and is therefore rejected on a machine
Bootwright installs, which never permits a root login at any point in its life
(ADR 0027). It is accepted only on a machine that a managed Ceph
`StorageCluster` lists as a topology node under a non-root orchestration
account, because that cluster's node-access reconciliation is what performs the
revoke and its orchestration account is the successor login — elsewhere the
field would have no executor.

### `StorageCluster.spec.ceph.cephadm.clusterSSH` — the orchestration identity

```yaml
clusterSSH:
  user: cephadm
  keyRef: ceph-cluster-ssh-key
```

`user` is the **post-install** identity: the account cephadm orchestrates
every host as (`cephadm --ssh-user`, later `ceph cephadm set-user`) and the
account Bootwright itself connects as once the node is provisioned. `keyRef`
names the `sshKeyPair` `Secret` that is cephadm's cluster identity. ADR 0024
sets the field's default.

This replaces `cephadm.clusterSSHKeyRef` and `cephadm.clusterSSHUser`. `v1alpha1`
breaks cleanly (no alias, no shim); strict decoding rejects the retired
spellings with a field-not-found error.

The retired pair also carried an asymmetry — `clusterSSHUser` was honoured
only when `clusterSSHKeyRef` was set, and otherwise silently deferred to the
first topology host's `access.ssh` user. The nested block has one meaning for
`user` regardless of whether `keyRef` is authored.

### The account Bootwright provisions

On every topology node, for a non-root `clusterSSH.user`, Bootwright creates
the account with a locked password, authorizes the cluster identity's public
key (`clusterSSH.keyRef`) in its `authorized_keys`, and writes a per-user
sudoers drop-in at `/etc/sudoers.d/60-bootwright-<user>` containing exactly:

```text
Defaults:<user> !requiretty
<user> ALL=(ALL) NOPASSWD: ALL
```

Any inherited `wheel` membership is dropped afterwards, so the named grant is
the account's only privilege. `NOPASSWD: ALL` is deliberate and is the only
policy cephadm can be orchestrated under, for the reason recorded in the
Context. The `!requiretty` default is scoped to that one principal rather than
relaxed globally.

The honest security delta is therefore narrow, and is stated as such
everywhere it is documented: no standing root SSH, a named auditable
principal, key-only authentication, and a credential that can be revoked
without touching root. It is **not** privilege separation — the account can
become root on demand.

### `clusterSSH.keyRef` is required whenever `clusterSSH.user` is not root

`cephadm bootstrap --ssh-private-key` persists the cluster identity into the
Ceph mon config-key store, where the cluster's mgr can read it. Any key that
also opens machines outside the cluster would therefore cross a trust domain,
which is why the cluster identity must be a dedicated generated `sshKeyPair`:
the key the cluster holds only ever opens the cluster's own orchestration
account. (The render-time fallback to a node's own machine key is removed —
ADR 0027.)

### Verify before revoke

Ordering is fixed and is a safety property, not an implementation detail. The
new account is proved to answer `sudo -n true` on **every** topology node
before `PermitRootLogin no` is written on any of them, and is re-proved after
the `sshd` reload. A node whose account does not answer stops the run with
root still reachable.

### The orchestration account may already exist

On a machine Bootwright installs, the two identities are always distinct: the
install creates `bootwright` (ADR 0027) and `storage_node_access` provisions
`clusterSSH.user` on top of it day-2. On a `spec.os.provided: true` node the
operator may have prepared the orchestration account out of band and may name
it as the node's own `access.ssh.user`; Bootwright reconciles that account
rather than creating it.

The role adapts rather than branching into a second code path. When the account
is already present `account.yml` finds it so, and the named sudoers grant is
written **before** any `wheel` membership is dropped, because an inherited
`wheel` membership may be the only thing making the connection Bootwright is
already using privileged. That ordering is safe for the created-account shape
too, where the account was never in `wheel`.

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
  rather than a new value for `access.ssh.user`.
- `rootLogin` is authorable only on an `os.provided: true` machine that a
  managed Ceph cluster claims under a non-root orchestration account. On every
  machine Bootwright installs the posture is an install invariant, not a field:
  the install writes `PermitRootLogin no` and authorizes no key for root
  (ADR 0027), so there is nothing to keep or revoke.
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
- Other node classes are unaffected: OpenShift nodes and service machines have
  no Bootwright-driven root-posture lever today.

### Alternatives rejected

- **A new `harden` apply stage.** The stage model is a strict linear ordering
  over `api/v1alpha1.CustomPlaybookAnchors()` — `fabric`, `machines`, `deps`,
  `base`, `add-ons` — where `--stage`/`--through` select an inclusive range over
  that list and the sub-phase names are pinned to it by
  `internal/converge/provisioning_stage_pin_test.go`. It cannot host a stage
  that must run *inside* the storage work (after the account exists and
  before cephadm bootstraps), and adding one would also widen the authored
  `CustomPlaybook.spec.gates`/`follows` vocabulary for a concern no operator
  playbook targets. Account provisioning and revocation are ordered steps of
  the storage-cluster task instead.
- **An `Environment`-wide fan-out** (a fleet default that revokes root on
  every machine). Deferred, not refused on principle: ten shipped bastion and
  service `Machine`s declare `access.ssh` with no `user`, so a fleet-wide
  default would silently retarget them, and a bastion is precisely the machine
  a failed revoke locks you out of. A fleet default needs its own opt-out
  grammar and a per-class scope before it is safe.

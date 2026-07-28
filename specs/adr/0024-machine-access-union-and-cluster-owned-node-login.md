# ADR 0024: Machine Access Is a Union, and a Ceph Cluster Owns Its Node Login

## Status

Accepted. Supersedes the key-separation clause of ADR 0019; the rest of
ADR 0019 stands.

## Context

`Machine.spec.access.ssh` carried one field shape — `addressRef`, `user`,
`keyRef`, `knownHostsRef` — used for two opposite purposes.

On a machine whose OS the operator supplied, those fields were an
**observation**: this is how the box that already exists is reachable. On a
machine Bootwright installs, the identical fields were an **instruction**: the
kickstart creates `user` and authorizes `keyRef`. Nothing in the object said
which; a reader had to cross-reference `os.provided` to know whether
`user: cephadm` asserted that an account exists or asked for one to be made.

Four defects followed from that conflation and from what filled the gaps around
it.

**Omitting the block meant "run on the controller."** `IsControllerLocalMachine`
returned true for any `os.provided: true` machine with no `access.ssh`, and the
inventory then emitted `ansible_connection: local`. Forgetting the block on a
remote bastion silently ran that bastion's configuration on the workstation
running Bootwright; preflight skipped its secret requirements, so nothing caught
it.

**A valid state could render an install nobody could log into.**
`access.ssh.keyRef` was required in managed mode, but whether the kickstart
authorized it was a different object's flag —
`MachineInstallProfile.spec.customizations.ssh.authorizeMachineSSHKey`, default
`false`, with no validation forcing it. With that flag and
`passwordAuthentication` both false the installed node accepted nothing.

**A password hash was compiled into the binary.** `managedOSSSHLoginPasswordHash`
was a fixed SHA-512 crypt emitted as `rootpw --iscrypted --allow-ssh` for root
installs and `user --password=` for non-root ones. Every managed-OS machine ever
installed shared one password extractable from the product, with no API to set
or lock it.

**Only a key could be expressed.** A pre-existing machine reachable by password,
or reachable with the operator's own identity, had no representation: `keyRef`
was mandatory whenever the block was present, so every machine required a named
`Secret` in the context store even when the operator already had working access.

Separately, ADR 0019 split the Ceph node login across two objects: the
install-window identity on each `Machine`, the orchestration identity on the
`StorageCluster`. That split was right when a fleet installed as `root` had to
be retrofitted. It is redundant when the cluster installs the nodes: the same
account name and key had to be authored on every node `Machine` *and* on the
cluster, and validation existed only to police the disagreement.

## Decision

### `spec.access` is a union of `local` and `ssh`

`access.local: true` declares the machine Bootwright runs on. It is reached with
a local connection, is legal only with `os.provided: true`, and is **refused**
when the machine's address does not resolve to this host. Absence of `access` no
longer means anything about locality. On an `os.provided: true` machine it
defaults to `ssh.auth.controllerIdentity` — the operator already administers a
box whose OS they supplied, so making them author that is ceremony. Everywhere
else absence means the machine has no Bootwright login, which is correct for a
cluster node the agent installer owns and a validation error for a machine
Bootwright installs.

This turns the worst failure mode — remote work silently executed on the
controller — into a declaration that fails closed.

### `spec.access.ssh.auth` is a discriminated union

Exactly one arm, and the arm name carries the mood:

```yaml
auth:
  controllerIdentity: {}          # reach it as the operator running bootwright
  privateKeyRef: lab-key          # reach it with this sshKeyPair Secret
  passwordRef: svcadmin-login     # reach it with this usernamePassword Secret
  provision:                      # bootwright CREATES this login at install
    keyRef: fleet-key
```

`provision` is required when, and legal only when, `os.provided: false` carries
an `os.installProfileRef`. The other three are legal only when it does not. A
reader now sees which direction causality runs without consulting a second
field.

Identity stays in one place. The alternative — a separate `os.install.login`
block for the created account — was rejected because it authors the account name
twice and lets the two drift, which is exactly the trap ADR 0019 spent a section
documenting.

`ssh.port` and `ssh.sudoPasswordRef` are added because a pre-existing machine is
the case that needs them: a non-standard port, and an account whose `sudo` asks
for a password (Bootwright escalates with `become` throughout and had no way to
supply one).

### A managed Ceph cluster owns its node login

A `Machine` a managed Ceph `StorageCluster` lists as a topology node and installs
derives its entire access block from `spec.ceph.cephadm.clusterSSH`: `user`, an
`auth.provision.keyRef` naming the cluster key, and `rootLogin: revoke` when that
user is not `root`. Such a `Machine` authors no `access` at all.

`clusterSSH.user` defaults to `cephadm` on a managed cluster rather than `root`.
Combined with the derived `rootLogin: revoke` and the kickstart change below, a
Ceph node the cluster installs never accepts a root login at any point in its
life — not during the install window, not before a hardening pass, never.

A topology node the cluster does **not** install (`os.provided: true`) keeps its
own `access.ssh` — authored, or the defaulted `controllerIdentity` above —
because the login already exists and the cluster cannot create it. Bootwright
connects with it, provisions the orchestration account there, and leaves
`rootLogin` at `keep` — the root posture of a machine whose OS the operator owns
is the operator's decision. The cluster account is what cephadm orchestrates
with; it is **not** substituted for the node's own login when Bootwright reaches
that machine, so `machine rsh`/`exec` on a `controllerIdentity` node connects as
the operator, not as `cephadm`.

### The cluster key may be the node key, and only that key

ADR 0019 required `clusterSSH.keyRef` to differ from the node `Machine` access
key, because `cephadm bootstrap --ssh-private-key` persists the cluster identity
into the Ceph mon config-key store, and the node access key was Bootwright's
fleet-wide machine administration credential.

Under a derived login there is no second key: the cluster key *is* the node key,
and its reach is exactly the `cephadm` accounts on that cluster's own nodes —
accounts the cluster's manager already commands. The manager gains nothing, so
the separation it bought is no longer load-bearing.

The rule therefore narrows rather than disappears: naming a `Secret` that a
`Machine` **authors** as its own `access.ssh.auth.privateKeyRef` remains refused,
because such a key opens machines outside the cluster. A key the cluster derives
onto its own nodes is accepted.

### Install-time policy lives on the profile, and ships no password

`MachineInstallProfile.spec.customizations.ssh` gains `initialPassword.secretRef`
and `sudo`, and loses `authorizeMachineSSHKey`.

- The compiled-in hash is deleted. The created account is **locked** unless a
  profile names a `usernamePassword` `Secret`, whose value becomes that account's
  console password only.
- `sudo` is `nopasswd` (default) or `none`. `nopasswd` writes
  `/etc/sudoers.d/60-bootwright-<user>` granting that principal — replacing a
  blanket `%wheel ALL=(ALL) NOPASSWD: ALL` drop-in plus `wheel` membership, which
  granted every present and future `wheel` member the same power.
- `authorizeMachineSSHKey` is removed because `auth.provision` *is* the statement
  that the key is authorized. The flag and the identity can no longer disagree,
  which closes the render-an-unreachable-machine defect by construction.
- The install always writes an explicit `PermitRootLogin`: `yes` when the account
  is `root`, `no` otherwise.

### Per-invocation SSH preferences

Every command that reaches a machine accepts `--ssh-preferred-id-key <path>`.
The key is offered **before** the credentials desired state declares, which
remain the fallback — OpenSSH tries identities in order, so this needs no
fallback logic of its own. It is refused unless the path is a regular file with
no group or other permissions.

Every such command also accepts `--ssh-user <name>`, which replaces the account
Bootwright **connects as** for that invocation and is refused unless it is a
valid POSIX user name. It reaches as far as the key flag does, including
`apply` and `destroy`, because the two answer the same question — *whose
credentials am I reaching this fleet with right now* — and an operator who must
name a key usually must name the account that key belongs to.

The line it does not cross is the account Bootwright **creates or manages**. A
Ceph cluster still orchestrates as its `clusterSSH.user`, a kickstart still
provisions the login `auth.provision` names, and `access.rootLogin` still
governs the same accounts it always did. So on a Ceph node the override moves
the *install/connection* identity the node-access role logs in with, and leaves
the `cephadm` account it provisions there untouched.

Neither flag is desired state: neither is part of the converge hash or folded
into a managed-OS install marker, and neither reaches the ownership records —
those record the **declared** connection facts, so a later run cannot inherit
one operator's key path or account name from an earlier one.

## Consequences

- Reading a `Machine` tells you whether its login exists or is created, from the
  object alone.
- A Ceph node file shrinks to substrate, network, and addresses. The cluster is
  the single place its login is named, so node and cluster cannot disagree.
- Ceph fleets are non-root by default and by construction, not by a hardening
  pass applied afterwards.
- Bootwright ships no default password. Operators who want console recovery name
  a `Secret` and get a real, per-environment credential.
- `controllerIdentity`, `--ssh-preferred-id-key`, and `--ssh-user` introduce
  ambient, per-operator authority into a product that otherwise references every
  credential by name.
  This is a deliberate trade for the "machines I already administer" case and is
  documented as such in `security.md`; neither puts secret bytes into desired
  state.
- The break is clean and total: `access.ssh.keyRef`, `authorizeMachineSSHKey`,
  and absence-means-controller are gone with no alias or shim, and strict
  decoding rejects the retired spellings by name. Existing installations are not
  migrated — a fleet installed under the old model is reinstalled under the new
  one.

### Alternatives rejected

- **Separate connect and create blocks** (`access.ssh` plus `os.install.login`).
  Authors the account name twice; ADR 0019 documents what happens when the two
  drift.
- **Keeping absence as the locality signal and adding `local` as an alias.**
  Leaves the silent-local footgun in place for anyone who forgets the block,
  which is the failure it exists to prevent.
- **Deriving the cluster key's `Secret` name instead of requiring it.** Secrets
  are declared, never conjured; an undeclared reference would defeat the
  preflight that proves material exists before an apply touches a machine.
- **Keeping `clusterSSH.user` defaulting to `root`.** Leaves the secure shape as
  something an operator must discover and opt into, on the object where getting
  it wrong is least recoverable.

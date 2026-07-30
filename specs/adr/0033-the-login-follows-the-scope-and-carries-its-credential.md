# ADR 0033: The Login Follows the Command's Scope, and Carries Its Credential

## Status

Accepted

Refines [ADR 0027](0027-bootwright-owns-the-login-it-installs.md), which fixed
*which* logins exist and scoped `--ssh-user` to the operator's own; and
[ADR 0019](0019-node-root-posture-and-orchestration-identity.md) /
[ADR 0024](0024-machine-access-union-and-cluster-owned-node-login.md), which
established that a Ceph cluster owns a node login distinct from the machine's.
Supersedes nothing.

## Context

A machine a cluster runs on carries two logins. Its own — `spec.access.ssh`,
resolved by ADR 0027 to `bootwright` plus the fleet key on a machine Bootwright
installs, and to the authored arm otherwise. And the cluster's — the account the
orchestrator drives every node as: `core` with `install.nodeSSH` on a
`ContainerCluster`, `spec.ceph.cephadm.clusterSSH.user` with `clusterSSH.keyRef`
on a managed Ceph `StorageCluster`. Validation *requires* the two credentials to
be distinct Secrets: `clusterSSH.keyRef` may not be the fleet key, because
cephadm bootstrap moves the cluster identity into the mon config-key store.

Nothing stated which of the two a given operation picks. The converging paths
had each arrived at an answer independently and, as it happens, at the right
one: the inventory reaches a machine as the machine, `storage_node_access`
borrows that login to *create* the orchestration account, and an add-on step on
a `StorageCluster` sets both the cluster user and the cluster key. The
operator-facing paths had not.

`bootwright machine rsh --name <ceph node>` resolved the *cluster's* account name
and kept the *machine's* key, then handed `ssh` `IdentitiesOnly=yes`. On any
valid configuration those two belong to different Secrets, so the documented
command could not authenticate — while `machine list` printed
`bootwright@<addr>` for the same machine, and `machines.md` and ADR 0027 both
promised the machine's own login there. Conversely
`bootwright cluster rsh --name <ceph cluster>` fell through to the same machine
resolver, so the *cluster*-scoped command inherited whatever the machine-scoped
one produced. The failure mode is the one already recorded for teardown in
`670dabaf`: a user name was chosen without the credential that opens it.

Naming an account and then offering another account's key is not a
near-miss — with `IdentitiesOnly=yes` it is a guaranteed `publickey` denial, and
the error it produces (`Permission denied`) reads like a broken node rather than
a wrong identity.

`f3793476` closed the denial the other way round, by making the credential
follow the user: `machine rsh` kept resolving the cluster's account and gained
the cluster's key, and `machine list` was changed to print that account. That
half — a login and its credential move together — is right and is kept below.
The other half is not: it leaves `cluster rsh` on a `StorageCluster` delegating
to the machine resolver (so the cluster-scoped verb reaches an `operatorIdentity`
node as the invoking operator, never as `cephadm`), it makes `machine rsh` and
`cluster rsh` the same command on a Ceph node, and it leaves `--ssh-user` still
pairing a name with the replaced account's key. Deciding by *scope* fixes the
denial and those three as one rule.

## Decision

### Scope selects the identity

The object a command names selects the login, and the same rule governs
converging work:

- **Machine scope** — `machine rsh` / `machine exec`, and every `apply`,
  `plan` or `destroy` task that acts on a machine — uses the `Machine`'s own
  `spec.access.ssh` login. A `Machine` that a cluster also lists is still
  reached as itself.
- **Cluster scope** — `cluster rsh` / `cluster exec`, and cluster-scoped
  work — uses the cluster's orchestration identity.

Cluster-scoped *apply* uses both, in the order the ordering already required:
the machine login is borrowed to create and verify the orchestration account,
which is then handed to the orchestrator. That is not an exception to the rule;
creating an account is machine-scoped work performed inside a cluster-scoped
task.

### An account never travels without its credential

Wherever a login is selected, the credential is selected with it, as one value:
the machine login with its `auth` arm, the orchestration account with
`install.nodeSSH` or `clusterSSH.keyRef`. No path may set one half.

### `--ssh-user` names an account, and resolves to that account's identity

The converging scope of the flag is unchanged from ADR 0027: `operatorIdentity`
machines only, widened by `--ssh-user-for-provisioned`, refused when the run
selects no machine that could use it. It never renames a cluster's orchestration
account — that name is recorded in cephadm, in the sudoers drop-in path, and in
the node access marker, and is not a per-invocation choice.

On `rsh` and `exec`, where ADR 0027 kept `ssh(1)` semantics, the flag resolves
against the identities Bootwright knows for that machine: its own login, and the
orchestration account of each cluster listing it. A name that matches one of
those is opened with that identity's credential — which is what makes
`machine exec --name ceph-0 --ssh-user cephadm -- id`, documented by ADR 0027,
actually work. Any other name is a plain `ssh(1)` override that offers no stored
key, leaving the operator's agent, `~/.ssh` defaults, or
`--ssh-preferred-id-key` to authenticate; offering the credential of the account
the flag just replaced would only reproduce the defect above.

An account that two clusters both own with different credentials is refused,
naming both, rather than resolved by picking one. That case has no validation
rule yet (`B-042`), so the ambiguity is real input, and guessing it would put
the wrong cluster's key on the wire.

### A revoked login falls back to the surviving one

`access.rootLogin: revoke` is accepted only where a managed Ceph cluster
supplies a replacement account. On a machine whose own login *is* `root` and is
revoked, machine scope has nothing left to select, so it falls back to that
cluster's orchestration identity — with that identity's key. This is the case
`ddb66d15` originally routed through the cluster account, narrowed to the
machines where it is true.

## Consequences

- `machine rsh` / `exec` on a Ceph topology node authenticate. They log in as
  the machine, matching `machine list`, `machines.md`, and ADR 0027. This
  reverses the account `f3793476` selected there, and `machine list` reports the
  machine login again — the invariant that commit fixed, that the listing names
  the account `machine exec` actually uses, still holds.
- `cluster rsh` / `exec` on a managed Ceph `StorageCluster` log in as
  `clusterSSH.user` with `clusterSSH.keyRef`, matching `ContainerCluster`
  behavior and the account cephadm itself uses.
- `--ssh-user <orchestration account>` on `rsh` / `exec` works without
  `--ssh-preferred-id-key`.
- `--ssh-user <unknown account>` no longer offers the replaced account's key.
  Where that name is reachable only by a stored credential, it is now reached
  with `--ssh-preferred-id-key` or an agent — the honest shape, since Bootwright
  holds no credential for it.
- Converging paths are unchanged. The rule they already followed is now the
  stated one, so a new path has an answer to check against rather than a
  precedent to copy.

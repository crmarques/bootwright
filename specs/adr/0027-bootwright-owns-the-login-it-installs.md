# ADR 0027: Bootwright Owns the Login on the Machines It Installs

## Status

Accepted. Supersedes the `auth.provision` and access-authoring clauses of
[ADR 0024](0024-machine-access-union-and-cluster-owned-node-login.md), its
per-invocation `--ssh-user` scope, and the install-window-identity clause of
[ADR 0019](0019-node-root-posture-and-orchestration-identity.md). The rest of
both stands.

## Context

ADR 0024 split `Machine.spec.access.ssh.auth` into arms whose names carry mood,
so a reader could tell an observed login from one Bootwright creates. It kept
both on the same object: `provision.keyRef` plus `access.ssh.user` were still
authored per machine, on every machine Bootwright installs.

Three costs followed.

**The operator authored a field that is not theirs to choose.**
`access.ssh.user` is the install-window identity. It feeds the managed-OS
install-marker hash and it is the account `probe_existing.yml` authenticates as
to decide a node is already installed. ADR 0019 spends a section documenting
that repointing it on a live fleet silently reinstalls that fleet, and
`machines.md` carries a `!!! danger` admonition saying the same. A field whose
correct use is "choose once, never touch" and whose incorrect use wipes disks is
a field the product should own, not one it should document a hazard around.

**The default was `root`, and the install enabled root SSH to match.**
`MachineSSHUser` returned `root` whenever `user` was unset, and `ks.cfg.j2`
answered with `PermitRootLogin yes` and a key in `/root/.ssh/authorized_keys`.
An environment whose policy forbids standing root SSH had to discover the
non-root shape, author an account name on every machine, and get the coupling to
`customizations.ssh.sudo` right — that last one silently, since
`MachineInstallSudoValues()` was dead code and the field was never validated, so
`sudo: nopassword` typed for `nopasswd` installed a machine that could not
escalate.

**`--ssh-user` had a blast radius the flag name did not carry.** It replaced
`ansible_user` for every host in a run. On a machine whose account Bootwright
created, that does not log you in as someone else — it makes the ownership probe
fail closed and the apply refuse. The flag exists for machines the operator
already administers, and nothing scoped it to them.

## Decision

### A machine Bootwright installs authors no access at all

`os.provided: false` with an `os.installProfileRef` derives the whole block:
`user` is the constant `bootwright`, and the key is the fleet key below.
Authoring `spec.access` or `spec.access.rootLogin` on such a `Machine` is
rejected **before** normalization, with the remedy in the message.

The kickstart correspondingly loses every `root` branch. It always creates
`bootwright` (locked unless the profile names an initial password), authorizes
the fleet public key for it, writes `/etc/sudoers.d/60-bootwright` granting that
one principal `NOPASSWD: ALL`, locks root, authorizes no key for root, and
writes `PermitRootLogin no`.

`customizations.ssh.sudo` is removed rather than validated. Bootwright escalates
with `become` throughout, so `sudo: none` describes a machine it cannot use; the
grant is an invariant, not a policy knob.

Because the account is a product constant it cannot move, so the install-marker
hash cannot move with it and the readiness probe always knows which account to
authenticate as. ADR 0019's reinstall trap is closed by construction rather than
by documentation. The probe's `fallbackUser` identity — added so a root-revoked
node could still be recognised — is deleted for the same reason: no posture
change and no cluster binding removes the `bootwright` account.

### One fleet key, named once

`Environment.spec.machineAccess.keyRef` names the `sshKeyPair` `Secret` every
installed machine authorizes. It is required as soon as any `Machine` installs
an OS, is type-checked with the other secret references, and is a fleet-wide
constant rather than a `spec.defaults.*` entry — nothing may override it per
machine, because nothing authors access on such a machine.

It may not also be named as a `StorageCluster`'s `cephadm.clusterSSH.keyRef`.
This restores the separation ADR 0019 argued for and ADR 0024 narrowed: `cephadm
bootstrap --ssh-private-key` persists the cluster identity into the mon
config-key store, and this key opens the `bootwright` account on **every**
machine in the fleet, not just that cluster's nodes. The render-time fallback
that used node[0]'s machine key when `clusterSSH.keyRef` was unset is removed
for the same reason, and the field is required on a managed cluster.

An alternative — minting the keypair into the context with nothing declared —
was rejected. Secrets are declared, never conjured (ADR 0024); an undeclared
reference would defeat the preflight that proves material exists before an apply
touches a machine. One line of `source: generated` is not the ceremony worth
breaking that for.

### Ceph layers on the substrate account rather than replacing it

A topology node the cluster installs no longer derives `user: cephadm` as its
install-window identity. It carries `bootwright` like every other installed
machine, and `storage_node_access` provisions `cephadm` on top day-2 — the
"distinct shape" path ADR 0019 already describes and which already exists in
`account.yml`. `clusterSSH.user` returns to meaning only what ADR 0019 said it
meant: the orchestration identity.

`clusterSSH.user` resolving to `root` is refused when any topology node is one
Bootwright installs, since such a node has no root login to orchestrate through.

### `spec.access` describes only a login that already exists

`auth.provision` leaves the authored API. `controllerIdentity` is renamed
`operatorIdentity`: it is not the controller's identity, it is the identity of
the person running Bootwright, and the flag that supplies it should read as
naming the same thing. Clean break, no alias — strict decoding rejects the
retired spelling by name.

### `--ssh-user` names the operator's own account, and says when it would not

On `apply`, `destroy`, `preflight`, `plan`, and `diff` the flag applies only
where the resolved auth arm is `operatorIdentity`, and the command **refuses**
when no machine in the run declares that arm. A flag that would change nothing
now says so instead of appearing to work.

This reverses ADR 0024's "it reaches as far as the key flag does". That reading
was sound while an installed machine's login was operator-authored: overriding
it was at least coherent. Once Bootwright owns that login the flag has nothing
legitimate to do there, and its fleet-wide reach is only a way to break the
ownership probe.

`machine rsh`/`exec` and `cluster rsh`/`exec` are excluded from the refusal.
They open an interactive session and converge nothing, so `--ssh-user` keeps
`ssh(1)` semantics and still reaches any account — `machine exec --name ceph-0
--ssh-user cephadm -- id` remains the documented way to visit the orchestration
account.

`--ssh-preferred-id-key` is unchanged: offered first, declared credentials still
the fallback, never recorded.

## Consequences

- A `Machine` file for a machine Bootwright installs is substrate, network,
  addresses, and hardware. There is no login in it, and no way to author one
  wrongly.
- Every machine Bootwright installs is non-root from first boot, by
  construction rather than by a hardening pass or an opt-in shape.
- The security delta is the same narrow one ADR 0019 recorded and must be
  described the same way: no standing root SSH, a named auditable principal,
  key-only authentication, a revocable credential. It is **not** privilege
  separation.
- One `Secret` and one `Environment` line replace a per-machine key reference,
  and a Ceph fleet now declares two keys with clearly different reach.
- `customizations.ssh.sudo`, `auth.provision`, `auth.controllerIdentity`,
  `MachineInstallSudoValues`, and `osInstall.ssh.fallbackUser` are gone with no
  alias or shim.
- Existing installations are not migrated. The install-marker hash moves with
  the account, so a fleet installed under the old model is destroyed and
  reinstalled under this one.

### Alternatives rejected

- **A profile-overridable account name.** Restores a smaller version of the
  immutability hazard, on the object where getting it wrong is least
  recoverable, to serve a naming policy that a sudoers drop-in and an audit log
  already satisfy.
- **Creating the same account on `os.provided: true` machines.** Bootwright
  would create an account on a box the operator owns. The operator's declared
  credential is the right contract there, and the fleet's two connection stories
  are honest about who owns what.
- **Renaming `--ssh-user` as well.** The name is accurate once the scope is
  fixed, and the flag was already renamed once in the preceding release; a
  second flag-day for a correct name is churn.

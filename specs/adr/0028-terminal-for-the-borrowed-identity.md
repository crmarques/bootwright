# ADR 0028: A Terminal for the Borrowed Identity, Never for the Created One

## Status

Accepted (implemented)

## Context

`storage_node_access` is the only privileged path in the collection that
hand-builds its own `ssh` argv and delegates every task to `localhost`. It has
to: it runs before the account it provisions exists, so it cannot connect as the
play's `ansible_user`, and it switches identity mid-role. ansible-core's
connection plugin — which allocates a pseudo-terminal for sudoable commands, and
is kept doing so by `pipelining = False` in `ansible/ansible.cfg` — is therefore
never in that path.

A node whose generic `Defaults` tier sets `requiretty` refuses `sudo` on that
channel. `sudo` evaluates `requiretty` **before** authentication, so it is
orthogonal to `-n` and to `NOPASSWD`: the account has the grant and still cannot
use it. The `Defaults:<user> !requiretty` line ADR 0019 makes normative is
written for `cephadm` by `sudoers.yml`, which runs *after* `account.yml` — so
the identity Bootwright borrows to create the account is never exempted, and the
task that would write the exemption is itself inside the blocked window. Such a
fleet could not be onboarded at all.

Two identities are in play, and they are not symmetric. Bootwright **borrows**
one — the operator's out-of-band install account, `operatorIdentity` under
ADR 0027 — for the length of the provisioning window. It **creates** the other,
`cephadm`, and hands it to the cephadm manager, which `sudo`-wraps every remote
command it issues over a connection that has no terminal and never will.

## Decision

### The borrowed identity may have a terminal; the created one may not

Bootwright MAY allocate a pseudo-terminal for the identity it borrows to
bootstrap a storage node. It MUST NOT allocate one for the orchestration account
it creates.

Every task that escalates as the install account runs over
`bootwright_node_access_privileged_argv`, which carries `-tt` when the node
requires it. The three proofs that the created account holds passwordless sudo —
`probe.yml`, `verify.yml`, and the re-proof in `revoke.yml` after `sshd` is
reloaded — run `sudo -n true` on the base terminal-free argv and must never gain
one. They are the acceptance test for the exact channel cephadm's manager uses,
so proving that account under a terminal would certify a cluster that cannot be
orchestrated. `verify.yml` resets the facts when it switches to the
orchestration identity, so everything after it runs terminal-free even on a node
that needed a terminal before it. The boundary is pinned by
`TestStorageNodeAccessProvesPasswordlessSudoWithoutATerminal`.

This is the same safety property ADR 0019 states as "verify before revoke",
narrowed by one word: verified *terminal-free*, on every topology node, before
`PermitRootLogin no` is written on any of them.

### The terminal is allocated only when a differential probe proves it necessary

`privilege.yml` runs between identity selection and account creation. It runs
`<sudo> true` without a terminal; only if that fails does it retry with one. If
neither answers it refuses, fail-closed, before any mutation — no account, no
sudoers file, root SSH untouched — and the message separates the two causes it
can tell apart.

A terminal cannot be allocated unconditionally: a host whose `sshd` sets
`PermitTTY no` and whose sudoers is stock answers today and would answer
`rc=255` tomorrow. Nor can the requirement be read off the node: `sudo -n -l`
and `sudo -n -v` are themselves blocked by `requiretty`, and its message is
gettext-marked, so there is no locale- or version-stable string to match. The
differential probe is the only sound detector.

### Bootwright writes sudo policy only for the account it owns

Bootwright does not write, relax, or repair sudo policy for the operator's
account; it adapts its own connection instead. The only sudoers Bootwright
authors stays what ADR 0019 fixed — one drop-in scoped to one principal, never a
global relaxation. This asymmetry extends that scoping rather than reopening it.

When the created account's grant is present and correct but has no effect, the
node is not reading it, and the refusal names the three ways that happens:
within the generic `Defaults` tier a later file in `/etc/sudoers.d` overrides an
earlier one; a `Defaults requiretty` placed after `@includedir` in
`/etc/sudoers` cannot be overridden by any drop-in; and an LDAP or SSSD
`cn=defaults` carrying `ignore_local_sudoers` skips `/etc/sudoers` and every
drop-in outright, in which case the grant must come from the directory.

### Terminal allocation is not an operator knob

`ansible_pipelining`, `ansible_ssh_pipelining`, and `ansible_ssh_use_tty` are
reserved `extraVars`. Each of them disables the terminal ansible-core allocates
for escalated tasks, for every host in the run rather than the ones the setting
was aimed at, and turns a `requiretty` node's every `become` task into a
failure. Bootwright decides terminal allocation per node from the probe.

## Consequences

- Zero operator action and zero new API surface. A converged re-apply issues no
  extra SSH round trip and allocates no terminal; a healthy first apply issues
  one extra read-only round trip.
- Whether a node needs a terminal is a per-run connection detail, not desired
  state: it is not authored, not in the converge hash, and not in the access
  marker.
- This is the terminal axis, not the password axis. ADR 0024's
  `ssh.sudoPasswordRef` answers a `sudo` that asks for a password; `requiretty`
  is refused before authentication and is unaffected by it.
- A fleet whose hardening sets `requiretty` globally is onboarded without
  changing that policy, and the account Bootwright leaves behind carries its own
  scoped exemption.

### Alternatives rejected

- **Allocating a terminal unconditionally.** Trades one broken fleet for
  another: a node with `PermitTTY no` answers on the terminal-free channel today
  and fails PTY allocation with `rc=255` under `-tt`. A probe that costs one
  round trip on first apply is cheaper than a class of node that stops working.
- **Rewriting the role onto Ansible `become`.** It would put the connection
  plugin — and its terminal handling — back in the path, and it fatally breaks
  the shape the role exists to serve. With `operatorIdentity` the install user
  is empty and must be discovered on the node, and the role connects as an
  account that is not `ansible_user` and switches identity mid-role, neither of
  which a play-level connection expresses.
- **Reading `/etc/sudoers.d` to predict the outcome.** It predicts, from files
  that may be shadowed by an LDAP `cn=defaults` the node never shows, exactly
  what the probe and `verify.yml` already observe as ground truth over the
  channel that matters.
- **Requiring the operator to exempt their own account.** It makes Bootwright's
  first act on a fleet a demand to weaken a hardening control on an account
  Bootwright does not own, to work around a limitation that lasts only as long
  as the provisioning window.

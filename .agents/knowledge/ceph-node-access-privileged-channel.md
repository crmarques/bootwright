# The storage node access privileged channel: terminals, CR, and whose sudo policy Bootwright owns

`storage_node_access` is the only privileged path in the collection that
hand-builds its own `ssh` argv (`tasks/context.yml:35-86`) and delegates every
task to `localhost`. It has to: it runs before the account it provisions exists
and it switches identity mid-role. Everything below follows from that one fact,
and none of it is reachable by reasoning about Ansible `become`. The decision
this channel implements is ADR 0028; the posture it serves is ADR 0019 and
ADR 0024. This file records the diagnosed mechanics, not the decision.

## 1. `become` is not in the blast radius — ansible-core already sends `-tt`

**Root cause:** ansible-core's SSH connection plugin allocates a pseudo-terminal
for every sudoable command it cannot pipeline:

```python
use_tty = self.get_option('use_tty')

args: tuple[str, ...]
if not in_data and sudoable and use_tty:
    args = ('-tt', self.host, cmd)
```

`plugins/connection/ssh.py:1515-1521` on ansible-core 2.20.4; `use_tty` defaults
to `true` (`ssh.py:358-360`), and `in_data` is empty because
`ansible/ansible.cfg:15` pins `[ssh_connection] pipelining = False`. So a node
whose sudoers sets `requiretty` is already served for every `become` task — that
is the remote-host fix recorded in
[ansible-sudo-requiretty.md](ansible-sudo-requiretty.md), and it is why that
file's diagnosis does **not** transfer here.

**Constraint:** any role that builds its own `ssh` argv has to re-derive
terminal allocation itself, because no connection plugin is in the path. The
three `extraVars` that would switch ansible-core's own allocation off —
`ansible_pipelining`, `ansible_ssh_pipelining`, `ansible_ssh_use_tty` — are
reserved and refused (`internal/state/desired/validate_extra_vars.go`,
`reservedEscalationExtraVars`; pinned by
`TestPlaybookExtraVarsCannotDisableTheEscalationTerminal`), because one extra
var applies to every host in the run.

**`ssh -t` is not enough.** ssh(1) on the `-t` option: *"Multiple `-t` options
force tty allocation, even if `ssh` has no local tty."* A single `-t` **declines**
when the local side has no controlling terminal — which is always true under an
Ansible task — so only `-tt` works. `bootwright_node_access_ssh_tty_options`
(`defaults/main.yml`) is therefore `-tt` plus `LogLevel=ERROR`, the latter so the
client's own chatter stays out of the stderr the refusal messages quote.

## 2. `requiretty` is refused before authentication, and no string identifies it

**Root cause:** `sudo` evaluates `requiretty` in policy, before it authenticates.
`-n` only suppresses the password prompt and `NOPASSWD` only waives
authentication; neither is reached. An account with a correct, present, working
`NOPASSWD: ALL` grant still fails with

```text
sudo: sorry, you must have a tty to run sudo
```

sudoers(5) on the flag: *"If set, sudo will only run when the user is logged in
to a real tty."* This is the terminal axis, not the password axis — no password
setting affects it.

**The password axis is answered separately, in the same file** (§ 2b). Note that
`ssh.sudoPasswordRef` does **not** reach this channel at all: it renders as
`ansible_become_password`, and every task here is `become: false`.

## 2b. The password axis: `SUDO_ASKPASS`, never `sudo -S`, never an argv

**Root cause:** a borrowed login whose `sudo` grant is not `NOPASSWD` fails both
probes in § 2 with `rc=1` — `sudo` ran and refused, because `-n` forbids the
prompt. `rc=1` twice is the password axis; `rc=0` on the second is `requiretty`;
`rc=255` with a failed PTY allocation is `PermitTTY no`. The three are
distinguishable only by that pair of return codes.

**Fix / rule (ADR 0029):** when both `sudo -n` probes fail and
`bootwright_node_access.sudoPasswordEnv` is set, `privilege.yml` writes
`"$HOME"/.bootwright-sudo-askpass/` (`pw` 0600 holding the password, `ask` 0700
`cat`ing it), and probes `SUDO_ASKPASS="$HOME/…/ask" sudo -A true` pty-free then
with `-tt`. The prefix becomes `bootwright_node_access_sudo`, so every downstream
privileged task inherits it unchanged. `verify.yml:45` resets it to `sudo -n`, so
it cannot leak past the borrowed window.

**`$HOME` is expanded by the node, never round-tripped.** The path is a shell
expression carried unquoted into every remote command, so `| quote` on it is a
bug — it would write the password to a literal `$HOME` directory the cleanup then
misses. An earlier revision read the home directory back with `printf %s "$HOME"`
and parsed stdout; any login profile that prints on a non-interactive shell
prefixes that value, and the redirect lands somewhere unclean-up-able.

**Three states end at the gate, and its message must tell them apart.** No
password collected; one collected but withheld from this machine (it declares its
own login, so only `sudoPasswordOffered` is set, not `sudoPasswordEnv`); one
collected and applicable but the helper never installed. Deciding on
`bootwright_node_access_askpass_install.rc` alone — as the message did between
`d9ca3d9a` and this fix — reports the third state as the first, and tells an
operator who passed `--ssh-ask-sudo-password` to pass it. The install task is
`no_log`, so if the refusal does not print its `rc` and `stderr`, nothing does.

**Why `SUDO_ASKPASS` and not `sudo -S`.** `sudo`'s stdin is already the payload
pipe in five tasks (§ 4), and under `-tt` a pty delivers no EOF and echoes what
is written to it, which would put the password in captured output. An
environment-variable prefix composes inside pipelines, inside `$(…)`, and with
or without a terminal.

**Why the password is fed on stdin and not templated into the command.** The
remote command string becomes the argv of the node's shell; any local account
reads it from `ps`. `privilege.yml`'s helper-install task therefore carries it
in `stdin:` under `no_log`, over the **terminal-free** argv — a pty would never
deliver the EOF the receiving `cat` needs. Pinned by
`TestStorageNodeAccessAnswersASudoPasswordWithoutExposingIt`, which fails if the
password reaches `vars`, if `no_log` is dropped, or if `main.yml` loses the
`always` block that removes the helper on the failure path.

**The diagnostic reads `stdout`, not `stderr`.** Under `-tt` the remote command's
`stderr` is bound to the pty slave and returns on the client's **stdout**; what
lands on `stderr` is ssh's own `Connection to <host> closed.`, which is never
empty. Reading `stderr` first shadowed `sudo: a password is required` on every
run and sent operators to `PermitTTY`. The fallback to `stderr` stays, because
an `rc=255` PTY allocation failure produces no remote output at all.

**Constraint:** the requirement cannot be read off the node. `sudo -n -l` and
`sudo -n -v` are themselves blocked by the same policy, and the message is
gettext-marked, so there is no locale- or version-stable string to match. The
differential probe is the only sound detector: `privilege.yml:2-14` runs
`"{{ bootwright_node_access_sudo }} true"` pty-free, `privilege.yml:16-26` retries
with `-tt` **only** when the first failed, and `privilege.yml:28-56` refuses
fail-closed — before `account.yml` creates anything — if neither answered.
`privilege.yml:58-70` then sets `bootwright_node_access_privileged_argv`.
Both probes are skipped when `bootwright_node_access_ready` is already true (the
orchestration account answered `sudo -n true` in `probe.yml`, which is the
stronger property) or when `bootwright_node_access_sudo` is empty (the borrowed
login is `root`); the assert then passes on `default(0)` with nothing probed.

**Do not make `-tt` unconditional.** A host whose `sshd` sets `PermitTTY no` and
whose sudoers is stock answers pty-free today and answers `rc=255` with a failed
PTY allocation under `-tt`. Unconditional allocation trades one broken fleet for
another. Order is pinned by
`TestStorageNodeAccessDetectsTheTerminalRequirementBeforeProvisioning`.

## 3. The asymmetry: a terminal for the identity Bootwright borrows, never for the one it creates

**Contract:** a terminal is **permitted** for the identity Bootwright *borrows* —
the operator's out-of-band install account — and **forbidden** for the identity
it *creates* and hands to cephadm. The cephadm manager `sudo`-wraps every remote
command it issues over a connection that has no terminal and never will, so the
created account must be proved on exactly that channel.

Three tasks are that acceptance test and run `sudo -n true` on the base pty-free
`bootwright_node_access_ssh_argv`:

- `probe.yml:4` — before anything is touched.
- `verify.yml:4` — after the account, sudoers and key are in place, before any
  root revocation.
- `revoke.yml:82` — again after the `sshd` reload that revoked root login.

**Do not let any of the three gain `-tt` or `bootwright_node_access_privileged_argv`.**
Proving the account under a terminal certifies a cluster cephadm cannot
orchestrate. Pinned by
`TestStorageNodeAccessProvesPasswordlessSudoWithoutATerminal`
(`internal/repo/checks/ansible_storage_test.go`), which fails on the substrings
`privileged_argv` and `tty` in those three argvs.

`verify.yml:40-48` resets `bootwright_node_access_endpoint`, `_sudo`,
`_tty_required` and `_privileged_argv` when it switches to the orchestration
identity, so every task after it runs byte-identically to a fleet that never
needed a terminal. Everything *before* it that escalates must use
`bootwright_node_access_privileged_argv` — pinned by
`TestStorageNodeAccessPrivilegedCommandsUseThePrivilegedInvocation`, whose only
exemptions are the two probes in `privilege.yml`.

The window this closes is structural, not incidental: the role's own
`Defaults:<user> !requiretty` line is written **for** `cephadm` **by**
`sudoers.yml`, which `main.yml:5-35` runs *after* `account.yml`. The exemption
lives inside the window it would fix, and it exempts the created account anyway,
never the borrowed one. Bootwright writes sudo policy only for the account it
owns and never relaxes tty policy for the operator's account.

## 4. A terminal rewrites the bytes: CR on stdout, no EOF on stdin

**Root cause:** with `-tt` the remote command's stdout is a pty slave, and the
tty line discipline's `ONLCR` translates every `LF` into `CRLF` **after** the
process has written it. Measured under `pty.spawn` on Linux with default
`ONLCR`:

```text
/bin/sh -c "printf 'a\nb\n' | tr -d '\r'"   ->  b'a\r\nb\r\n'
/bin/sh -c "x=$(printf 'c\nd\n'); ..."      ->  captured value is 'c\nd', no CR
```

Two consequences, both load-bearing:

- **A remote `tr -d '\r'` cannot help.** The CR does not exist yet when the
  filter runs; it is injected downstream by the terminal.
- **`$(...)` command substitution stays clean.** The subshell's output is
  captured through a pipe inside the remote shell, upstream of the pty.

**Fix / rule:** compare on the node, never on the controller. The sudoers grant
comparison is `test "$(<sudo> cat <path>)" = '<expected>'` executed remotely
(`sudoers.yml:2-15`), and the reconcile keys off its exit status only
(`sudoers.yml:18`) — never off `stdout`. Pinned by
`TestStorageNodeAccessSudoersGrantIsComparedOnTheNode`, which fails if the
`when` mentions `stdout`.

**A pty never delivers EOF.** With a terminal allocated, the remote command's
stdin is a pty, so a payload that reads stdin to EOF blocks instead of
terminating. ansible-core encodes the same constraint upstream — `ssh.py:1518`
adds `-tt` only `if not in_data`, i.e. never when it has data to feed. Every
privileged payload in this role therefore produces its content on the node with
`printf '%s\n' … | <sudo> tee …` (`sudoers.yml:27-28`, `authorize.yml:39-41`,
`restore.yml:88-90`, `revoke.yml:27-28`, `marker.yml:21-22`) and reads nothing
from the controller's stdin. **Do not convert any of them to a controller-fed
stdin form**, and if a payload is added, it obeys the same rule whether or not
it appears in this list.

## 5. Writing the drop-in does not mean the node evaluates it

**Symptom:** `Require the storage node orchestration account before any root
revocation` fails although `/etc/sudoers.d/60-bootwright-<user>` is present,
0440 `root:root`, and syntactically accepted by `visudo -cf`.

**Cause:** the grant is written but never applied. Three real causes, all
observed or documented:

1. **A later-sorting file wins.** sudoers(5): *"In general Defaults settings are
   applied in order, later entries will override earlier ones. However,
   command-specific Defaults settings are applied later."* There is **no**
   generic-before-per-user tier — only command-specific `Defaults` are deferred —
   so a plain `Defaults requiretty` in a file sorting after
   `60-bootwright-<user>` beats Bootwright's `Defaults:<user> !requiretty`.
   (Deferred rename: B-038.)
2. **`Defaults requiretty` after `@includedir`** in `/etc/sudoers` cannot be
   overridden by any drop-in.
3. **LDAP/SSSD `ignore_local_sudoers`.** sudoers(5): *"If set via LDAP, parsing
   of `/etc/sudoers` will be skipped. … When this flag is enabled,
   `/etc/sudoers` does not even need to exist. … this sudoOption is only
   meaningful for the `cn=defaults` section."* sudo then skips `/etc/sudoers`
   **and its `@includedir`**, so the drop-in is never read at all and the grant
   has to come from the directory instead. Set in a *local* sudoers file the same
   flag is **inert** — only `requiretty` beside it bites. A reporting operator's
   RHEL node showed exactly `…, requiretty, ignore_local_sudoers` in the generic
   `Defaults` tier, which is why both are named together.

**Fix / rule:** two commands discriminate them on the node — the `sudoers:` line
in `/etc/nsswitch.conf` shows whether policy comes from a directory at all, and
`sudo -ll -U <user>` shows which policy actually wins. `verify.yml:11-36` already
names all three causes and both commands in its `fail_msg`; keep them there.
Bootwright proves the grant rather than assuming it precisely because writing it
is not sufficient.

## Related invariants

- **`restore.yml` and `revoke.yml` operate on the *install* account, so they must
  use the install key.** `bootwright_node_access_install_public_key`
  (`context.yml:122-127`, guarded non-empty) is the machine access key
  (`internal/render/inventory/storage_ansible.go:134` → `:157`).
  `bootwright_node_access_public_key` is the cephadm **cluster** key
  (`storage_ansible.go:135` ← `clusterSSH.keyRef` → `:151`) and belongs only to
  `authorize.yml`. Using the latter in the restore path grafted the Ceph cluster
  key onto the operator's own account on every first apply of an
  `os.provided: true` fleet — where a derived install identity leaves
  `installPublicKeyPath` empty and `rootLogin` defaults to `keep`, so
  `restore.yml` always runs — and the destroy path
  (`storage_cluster_cephadm/tasks/revoke_node_access.yml:30,51`, already correct)
  never removed it.
- **The install key is optional twice over: the path may be empty, and the path
  may name material the context store does not hold.** The renderer emits
  `installPublicKeyPath` from the secret index, which resolves a path for every
  declared `sshKeyPair` whether or not its public half was ever generated, so a
  bare `lookup('ansible.builtin.file', …)` in `context.yml` aborted the whole
  play — including teardown, which never needs that key. `context.yml:88-95`
  stats the path first and resolves the fact to `''` when nothing backs it, and
  every consumer keeps its `| length > 0` guard. Teardown then degrades
  (`revoke_node_access.yml` reuses the resolved fact rather than looking the file
  up a second time); apply does not — `revoke.yml`'s assert refuses to report a
  narrowed access surface it could not narrow.
- Whether a node needs a terminal is a per-run connection detail: not authored,
  not in the converge hash, not in the access marker. Cost is one extra
  read-only round trip on a healthy first apply and zero on a converged re-apply.
- The YAML/Jinja escape rule that made the on-node comparison necessary in the
  first place is provider-neutral and lives in
  [ansible-folded-scalar-escapes.md](ansible-folded-scalar-escapes.md).
- Authoring traps for this same account — the immutable `access.ssh.user`, the
  two-space `User` indent, and why `NOPASSWD: ALL` is forced — are in
  [ceph-nonroot-node-access.md](ceph-nonroot-node-access.md).

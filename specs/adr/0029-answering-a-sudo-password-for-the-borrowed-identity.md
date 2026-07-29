# ADR 0029: Answering a `sudo` Password for the Borrowed Identity

## Status

Accepted

## Context

ADR 0028 separated two ways a borrowed login can fail to escalate and fixed one
of them. It named the other and left it: *"This is the terminal axis, not the
password axis. ADR 0024's `ssh.sudoPasswordRef` answers a `sudo` that asks for a
password."*

That sentence is true of every path that escalates through Ansible `become` —
the renderer emits `sudoPasswordRef` as `ansible_become_password` — and false of
the one path ADR 0028 is about. `storage_node_access` builds its own `ssh` argv,
runs every task `delegate_to: localhost` with `become: false`, and hardcodes its
privilege prefix to `sudo -n` (`probe.yml`). No connection plugin is in that
path, so no `become` variable reaches it. A `Machine` could name a
`sudoPasswordRef` and the Ceph node-access channel would still refuse the node.

The gap is reachable exactly where ADR 0028 said onboarding should now work: an
`os.provided: true` topology node under `operatorIdentity`, reached as the
operator's own account. An operator who holds `sudo` on that node under site
policy — with a password, as most policies require — had no way to give
Bootwright that privilege, and the refusal did not even name the cause. Its
diagnostic read the `-tt` probe's `stderr`, but a pseudo-terminal binds the
remote command's `stderr` to the pty slave and returns it on `stdout`; the
client's own `Connection to <host> closed.` is what lands on `stderr` and is
never empty, so it shadowed `sudo: a password is required` on every run.

Requiring the operator to grant themselves `NOPASSWD` is the alternative ADR
0028 already rejected for the terminal axis, for the same reason: Bootwright's
first act on a fleet should not be a demand to weaken a hardening control on an
account it does not own, to work around a window that closes when the account it
does own exists.

## Decision

### The password is prompted, per invocation, and never stored

`--ssh-ask-sudo-password` is a global boolean flag beside `--ssh-user` and
`--ssh-preferred-id-key`. It prompts once, before the run starts, and holds the
answer in memory for that process only. It takes no value, so the password
enters neither shell history nor `ps`.

It MUST NOT be written to the context secret store, the rendered inventory, the
run log, or any ownership record, and it is not part of the converge hash. The
store is shared with other operators and outlives the run; a personal login
password belongs to neither. `access.ssh.sudoPasswordRef` remains the way to
declare a `sudo` password that must persist and be shared — a service account's,
not a person's. The two are deliberately different mechanisms for deliberately
different secrets.

The password reaches `ansible-playbook` through an environment variable
(`BOOTWRIGHT_SSH_SUDO_PASSWORD`). The renderer emits a *lookup* of that variable
as `ansible_become_password`, and the *name* of that variable to the node-access
channel — never the value. A rendered inventory left on disk therefore names
where the password was, and never what it was.

### Its scope is the identity `--ssh-user` names

The flag applies to machines that declare `auth.operatorIdentity`, and to no
others. A login Bootwright installed holds a proved `NOPASSWD` grant, and a
login a `Secret` names carries its own credential; offering a person's password
to either would be offering it to an account it does not belong to. Like
`--ssh-user`, the flag is refused when no machine in the run declares that arm,
rather than silently changing nothing.

### On the channel that has no `become`, the password is answered by `SUDO_ASKPASS`

`privilege.yml` gains a third and fourth probe, run only after the two `sudo -n`
probes have both failed and only when a password is available. Bootwright writes
a helper directory the borrowed account owns — the password in a `0600` file, a
`0700` script that `cat`s it — and probes `SUDO_ASKPASS=<helper> sudo -A true`
without a terminal, then with one. The refusal is still fail-closed and still
precedes `account.yml`, so a node that cannot escalate is left with no account,
no sudoers file, and root SSH untouched.

### The node chooses the directory, memory first, and says which one it chose

Bootwright names no path. The install command walks `$XDG_RUNTIME_DIR`,
`/dev/shm`, the account's home, then its temporary directory; for each it runs
`mktemp -d`, writes an **empty** helper, and executes it. The first candidate
that survives that is the one used, and its name is reported in a delimited
marker on standard output.

The order is deliberate. The first two are `tmpfs`: a password there is in RAM,
reaches no backup, no snapshot and no forensic image, and does not survive a
reboot; `/run/user/<uid>` is additionally `0700` per-user and destroyed by
systemd when that account's last session ends. Disk is the fallback, not the
default. `mktemp -d` rather than a fixed name because a predictable path in a
shared directory can be pre-created by another local account, which would then
own the directory the password lands in. And a *marker* rather than reading the
home directory back, because any login profile that prints on a non-interactive
shell shares that standard output — a real fleet's borrowed login also resolved
`$HOME` to `/root`, which it could not write, so a fixed path under `$HOME`
fails outright.

Executing an empty helper before choosing the directory means a `noexec` mount
is rejected while the file is still worthless, with `Permission denied` on the
helper, rather than surfacing later as a password that looks wrong. If no
candidate qualifies the command exits non-zero having written **nothing**: the
password never leaves the controller.

### The node erases the password on a timer it arms itself

Before the password is written, and in the same command, the node starts a
detached `sleep <ttl>; rm -rf <dir>`. Cleanup therefore does not depend on the
controller surviving: a killed process, a lost network, a crashed run, an
operator's `Ctrl-C` — none of them can leave the password on the node for longer
than `bootwright_node_access_askpass_ttl` (900s). The `always` section is still
the normal path, and it now *verifies* the removal (`rm -rf … && test ! -e …`)
and fails the run when it cannot confirm it, because `rm` reports success for a
path it was never able to look at. When the block already failed, the check
degrades to a warning so it cannot replace the diagnosis the operator needs.

### What this design does not defend against

`root` on the node can read the helper — but `root` is the privilege being
borrowed, so it is not a boundary this can hold. `tmpfs` pages can reach swap on
a node with unencrypted swap. A node with `KillUserProcesses=yes` in
`logind.conf` kills the timer when the session ends; there it also destroys
`/run/user/<uid>`, which is why that is the first candidate, but a run that fell
through to `/tmp` on such a node is left with only the `always` removal. All
three are documented rather than mitigated.

Two alternatives that would remove the on-node secret entirely were considered
and rejected. `sudo -S` fails for the reason given below. Priming `sudo`'s
credential cache once and deleting the helper immediately fails because
`tty_tickets` is the default and every task opens its own connection, so no
later `sudo` would ever see that timestamp.

`SUDO_ASKPASS` is the mechanism rather than `sudo -S` because it composes with
what this role already does and `-S` does not. Every privileged payload in the
role produces its content on the node and pipes it into `sudo` (`printf … |
sudo tee …`), so `sudo`'s standard input is already spoken for; and under `-tt`
a pty never delivers EOF and echoes what is written to it, which would put the
password in captured output. An environment-variable prefix on the remote
command composes inside pipelines, inside `$(…)`, and with or without a
terminal, and never reaches an argv.

The password is written over the terminal-free connection, on standard input,
under `no_log`. It is never interpolated into a remote command string: that
string becomes the argv of the node's shell, which any local account can read
from `ps`.

### The refusal distinguishes no password from an undelivered one

Three states end at the same gate, and each has a different remedy: no password
was collected, one was collected but this machine does not qualify for it, and
one was collected for a machine that qualifies but the helper never reached the
node. The gate reads them from `bootwright_node_access.sudoPasswordEnv` (a
password exists for *this* machine) and `sudoPasswordOffered` (the run collected
one at all), not from whether the helper install returned `0` — deciding on the
install alone reports a helper that failed to write as a password that was never
collected, and sends an operator who passed `--ssh-ask-sudo-password` back to
the flag already on their command line. The install task is `no_log`, so the
refusal also carries its return code and standard error: nothing else in the run
reports them.

A password Bootwright put on a node is Bootwright's to take off it, on the
failure path as much as the success path — and, since the node holds the timer,
on the path where Bootwright is no longer running at all.

### The created account is untouched by any of this

The three proofs that the orchestration account holds passwordless `sudo` —
`probe.yml`, `verify.yml`, and the re-proof in `revoke.yml` — keep running
`sudo -n true` on the terminal-free argv, with no password and no terminal.
`verify.yml` resets the privilege prefix to `sudo -n` when it switches identity,
so the askpass form cannot leak past the borrowed window. cephadm's manager
`sudo`-wraps every remote command over a connection that has neither a terminal
nor a password, and that is the channel the account must be proved on. Pinned by
`TestStorageNodeAccessProvesPasswordlessSudoWithoutATerminal` and
`TestStorageNodeAccessAnswersASudoPasswordWithoutExposingIt`.

## Consequences

- The two axes are now symmetric: a terminal is detected by differential probe
  and allocated for the borrowed identity only; a password is prompted for and
  answered for the borrowed identity only. Both are per-run connection details,
  neither is authored, and neither perturbs the converge hash.
- The refusal names which of the two it hit, and what to do about each. Its
  terminal diagnostic now reads the `-tt` probe's `stdout`, where a pty puts the
  remote `stderr`, falling back to `stderr` for the client-side failures — an
  `rc=255` PTY allocation failure still surfaces.
- A fleet whose policy grants the operator password `sudo` is onboarded without
  changing that policy, and the account Bootwright leaves behind needs no
  password at all.
- The window in which a personal password exists on a node is bounded by the
  provisioning window and by an `always` removal. It is a real exposure, stated
  rather than hidden: on a node that already carries the orchestration account,
  the earlier probe succeeds and no password is ever sent.

### Alternatives rejected

- **Making `sudoPasswordRef` reach this channel.** It would put a person's
  login password in the context secret store, where it is shared with every
  operator of that context and outlives the run. The field stays what ADR 0024
  made it: a declared credential for a declared service account.
- **Feeding the password to `sudo -S` on standard input.** `sudo`'s stdin is
  already the payload pipe in five tasks, and under `-tt` there is no EOF and
  the pty echoes the password into captured output.
- **Interpolating the password into the remote command.** The command string is
  the argv of the node's shell and is world-readable through `ps`.
- **Rewriting the role onto Ansible `become`.** Rejected in ADR 0028 and still
  rejected: the role connects as an account that is not `ansible_user`, before
  that account exists, and switches identity mid-role.
- **Priming `sudo`'s credential cache once and relying on the timestamp.** The
  cache is per-tty and time-limited, both of which vary per node and per policy;
  the role would work on some fleets and mysteriously stop on others.
- **Writing a `NOPASSWD` grant for the operator's account.** ADR 0028 forbids
  Bootwright authoring sudo policy for an account it does not own, and this ADR
  does not reopen it.

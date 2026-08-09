# Root gate and sudo re-exec mechanics

Bootwright re-executes itself under sudo to reach the root-owned
`/var/lib/bootwright` context. ADR 0010 records the gate policy (which
invocations escalate and why); the entries below are the operational
mechanics and gotchas around the re-exec itself.

**Constraint: classify args only after stripping a leading global
`--context`.** `stripLeadingGlobalFlags` removes a leading `--context`
flag purely so the root gate classifies the invocation by its real
command/subcommand; the ORIGINAL args (including `--context`) are still
forwarded verbatim to the sudo child. A leading `--context` must neither
turn a rootless command rootful nor mask a rootful one
(`TestStripLeadingGlobalFlags`).

**Constraint: cut the forwarded payload before classifying.** The gate
parses argv itself, so every scan (`argsContainHelp`,
`argsHaveUnusableSSHUser`, `argsHaveNameValue`, `argsMayUseBecome`) sees
whatever the operator typed for the wrapped program too.
`argsBeforeCommandPayload` cuts that region first: at the `--` terminator,
and — for `container-cluster oc`/`kubectl`, whose flags are
`SetInterspersed(false)` — at the first operand. Without the cut,
`cluster exec --name c -- ceph auth get-or-create -h` reads as a help
request, skips the sudo re-exec, and dies on `lstat` of the root-owned
context (`permission denied`) while the same command without `-h` works;
`container-cluster oc --name c get pods --help` fails the same way. The
cut runs before `stripLeadingGlobalFlags`, so it locates the command path
through `leadingGlobalFlagCount` rather than assuming `args[0]` is a verb.

**Gotcha: the gate's command table drifts when commands move.**
`argsNeedLocalRoot` is a hand-written switch over command paths, and
nothing in the compiler ties it to the cobra tree: moving `oc`, `kubectl`,
and `kubeconfig` from `cluster` to `container-cluster` left the switch
naming the dead paths, so all three silently stopped escalating and failed
with `permission denied` for every non-root caller.
`TestLocalRootGateClassifiesEveryRunnableCommand` walks the built tree and
requires an explicit rootless entry for every runnable leaf that does not
escalate, which fails on the next rename instead of at the operator's
terminal.

**Constraint: resolve the `-f` workspace path before any re-exec.**
`ResolveWorkspaceDir` (which expands `~` and relative source paths) must
be called BEFORE the sudo re-exec so the `-f` source directory resolves
against the calling operator's environment (their `HOME` and cwd), not
root's. It also enforces: the source must exist, be a real directory
(not a symlink), and live outside the Bootwright state directory.

**Semantics: the sudo handoff protocol uses two internal env vars.**
`BOOTWRIGHT_INTERNAL_LOCAL_SUDO_AUTH` (`SudoAuthEnv`) records how the
parent authenticated sudo (`noninteractive` or `prompted`);
`BOOTWRIGHT_INTERNAL_BECOME_PASSWORD_FILE` (`PasswordFileEnv`) hands the
captured BECOME password file path to the child. `InheritedPasswordFile`
honors the file only when auth is `prompted`. The file is written when
`argsNeedCallerSudoPassword` says the child will use it — either the
command may use Ansible `become` (`argsMayUseBecome`) or the command line
carries `--ssh-ask-sudo-password` (`argsAskSSHSudoPassword`). Interactive
prompting stays with the caller via a `readPassword` callback; the sudo
keepalive interval is 3/4 of sudo's `timestamp_timeout` parsed from
`sudo -V`, falling back to 1 minute.

**Semantics: one sudo password is collected once, and only reused for the
account that owns it.** The two prompts an operator can hit are separate
processes: the unprivileged parent prompts `SUDO password:` to escalate
itself, and the rootful child's `PersistentPreRunE` prompts
`SSH sudo password:` for `--ssh-ask-sudo-password`. Both answers are the
same secret exactly when the account is the same, so
`reuseCallerSudoPassword` reuses the inherited file instead of prompting
again — but only when `--ssh-user` is given AND equals
`execution.CallerUserName()` (`SUDO_USER`, trusted only inside the
local-root child). Without `--ssh-user` the operator login comes from each
machine's `access.ssh.user` and is unknown at prompt time; with a
different `--ssh-user` the caller's own password belongs to another
account, and offering it would be a failed authentication against a login
Bootwright does not own. Both of those still prompt, and the prompt names
the account (`SSH sudo password for "operator":`) so two prompts are never
ambiguous. The reuse is announced through `internal/cli/output`, never
silently: `TestHumanOutputUsesOutputPackage` allowlists only the files that
write raw prompts, and a notice is not a prompt.

**Gotcha: Ctrl-C in the re-exec path is handled by the child, not the
parent.** The terminal delivers the signal to the whole foreground
process group (parent, sudo, rootful child), so the child receives it
directly (or via sudo's `use_pty` relay) and reaps its ansible process
group itself. `cmd.Cancel` is therefore a no-op — SIGKILLing sudo on
cancel would strand the child's cleanup — and `cmd.WaitDelay` is
`localRootShutdownGrace` (60s), only a backstop for a wedged child,
deliberately kept well above the ansible runner's shorter process-group
grace so a healthy shutdown is never truncated.

**Semantics: first signal cancels, second hard-kills.** `signalContext`
cancels the root context on the first SIGINT/SIGTERM; that cancellation
arms in-flight cleanup (the ansible runner reaps its process group of
ssh/python children, the local-root re-exec waits for that reaping).
After the first signal the default disposition is restored (`signal.Stop`
before cancel) so a second Ctrl-C hard-kills a wedged cleanup instead of
being swallowed. In `main`, `os.Exit` skips defers, so `stop()` is called
explicitly before exiting. `TestSignalContextCancelsOnSignal` sends
exactly one signal so the restored default disposition never fires.

**Constraint: preflight escalates but must never become-prompt.**
`preflight` is read-only: it escalates to read the root-owned context but
must NEVER prompt for a sudo become password the way `bastion setup`
does. The read-only bastion dependency check lives only under
`preflight bastion` — the bastion command group deliberately carries no
duplicate `check` subcommand (bare `bastion` only prints help;
`bastion setup` is the rootful mutating one).

**Constraint: shell completion never escalates.** Completion callbacks
(machine/cluster name and node completion) load desired state via
`loadDesiredStateLocalOnly`, which skips the locality/root enforcement of
a normal run and reads only the user-owned input YAML. Completion runs as
the unprivileged user, and any failure yields no completions instead of
an error.

**Semantics: the password-EOF error explains why a password was
wanted.** When the sudo-password read hits EOF non-interactively, the
error explains WHY a (possibly read-only) command wants a password:
bootwright re-executes itself under sudo to reach `/var/lib/bootwright`,
and it names the ways to satisfy it (run interactively for the prompt,
run as root, or configure passwordless sudo) instead of a bare
`read password: EOF`.

**Test constraint: supplied password input must disable the controlling
TTY fallback.** `readPromptedPassword` deliberately prefers `/dev/tty`
when one exists, even when the caller also supplied an input reader. A test
that expects its synthetic input to answer `--ssh-ask-sudo-password` must
call `withoutControllingTTY`; otherwise `go test` passes in a pipe but blocks
waiting on the operator's terminal when the suite runs interactively.

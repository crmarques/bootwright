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
honors the file only when auth is `prompted`. Interactive prompting stays
with the caller via a `readPassword` callback; the sudo keepalive
interval is 3/4 of sudo's `timestamp_timeout` parsed from `sudo -V`,
falling back to 1 minute.

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

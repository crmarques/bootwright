# Sudo pseudo-terminal handoff

Observed on 2026-09-12 with sudo 1.9.17p2 on Fedora 43, whose sudoers enables
`use_pty`. Required behavior stays in
[local privilege](../../specs/cli.md#local-privilege-and-user-identity).

## Symptom

`setup` printed its plan, asked `Confirm controller setup on this host?
[y/N]`, echoed the typed `y` and then stayed silent indefinitely. The typed
`y` and newline were still queued unread on the invoking terminal (`FIONREAD`
reported two bytes), the elevated child had written nothing further, held no
lock and had opened no publisher connection, and it woke about ten times a
second: the 100 ms poll in `readInputFD`.

## Mechanism

The supervisor re-executes Bootwright through `sudo -u '#0'`, now in
`Elevator.Run` of [elevation.go](../../internal/controller/privilege/elevation.go).
It gave sudo the invoking terminal on standard input but pipes on standard
output and error, because it counted output bytes and filtered sudo refusal
lines through its own writers. With `use_pty`, sudo runs the
command in a new pseudo-terminal and, when its own standard input or output is
not the user's terminal, starts the command in a background process group of
that pseudo-terminal. It then relays nothing from the user's terminal until the
command touches the pseudo-terminal and is suspended with `SIGTTIN` or
`SIGTTOU`; a foreground sudo grants the terminal and resumes the command
(sudoers(5), `exec_background`, which describes the same handoff). The
confirmation reader polls with `poll(2)`, which never triggers job control, so
the handoff never happened: the child polled an empty pseudo-terminal while the
answer waited on the real one. `ps -o pid,pgid,tpgid` showed the child's process
group differing from the pseudo-terminal's foreground group.

## Decisions

- [elevation.go](../../internal/controller/privilege/elevation.go) hands every
  terminal stream of an interactive invocation to sudo unchanged, and
  [sudo.go](../../internal/controller/privilege/sudo.go) leaves `*os.File`
  streams unwrapped so they reach sudo as descriptors. Sudo then starts the
  child in the foreground with its normal raw-mode relay. The output counter
  and the start filter remain for noninteractive and JSON invocations, where
  sudo cannot prompt, and for a standard error that is not a terminal.
- [stdin_linux_amd64.go](../../cmd/bootwright/stdin_linux_amd64.go) performs
  one real nonblocking read before polling whenever standard input is the
  controlling terminal of another foreground process group. That read raises
  `SIGTTIN`, so sudo's background mode (output redirected to a file or pipe)
  and a shell's `fg` both hand the terminal over; an orphaned group reads
  `EIO` and the confirmation fails instead of hanging.
- The terminal redraw of [progress rows](../../specs/cli/output.md#long-running-progress)
  decides from the child's own standard output. Sudo's pseudo-terminal mode
  routes only descriptors that are terminals through the pseudo-terminal and
  hands a redirected standard output to the command unchanged, so
  `controller setup > log` receives the appended-line form. This follows sudo's
  `exec_pty` descriptor handling and is operator-verified, not gated in-tree.

Evidence: `TestBackgroundConfirmationRequestsTerminalThroughJobControl`
simulates sudo's monitor with a pseudo-terminal, a session leader in the
foreground and the reader in a background process group, and
`TestSupervisorHandsFileStreamsToTheChildUnwrapped` and
`TestElevationHandsTerminalsOnlyToAnInteractiveInvocation` cover the
supervisor. Real sudo remains operator-run. A background launch
(`bootwright ... &`) now stops with `Stopped (tty input)` at the prompt like any
interactive program, and `--yes` avoids the prompt entirely.

## Telling a refusal from a child that ran

A noninteractive supervisor used to report `runtime.privilege` for any nonzero
exit without standard output once it had withheld a line beginning with
`sudo:`. That blamed local sudo for a sudo warning printed before the child's
own failure, and for `machine exec` relaying a remote `sudo -n` refusal, whose
line it also dropped. In JSON mode it blamed sudo for every child that crashed
or was killed.

The supervisor has nothing but the child's two streams and sudo's exit status,
and those cannot separate the cases. The EXIT VALUE section of sudo's manual
page (sudo 1.9.17p2 on Fedora 43) says sudo exits 1 on an authentication
failure, a configuration or permission problem, or a command it cannot
execute, which is also what a failing child returns. No side channel crosses sudo: the sudoers
manual page's `closefrom` option closes every descriptor from 3 up before the
command runs, the environment is reset unless the rule allows setting it (see
[sudo command-line environment](sudo-command-line-environment.md)), and an
extra argument would break "Preserve argv" in
[local privilege](../../specs/cli.md#local-privilege-and-user-identity) and
sudoers argument matching.

So the child says it started, on the one stream it owns: its first write is a
fixed line beginning `bootwright:`, which the supervisor's start filter removes
([elevation.go](../../internal/controller/privilege/elevation.go)). The child
writes it only as root, with a standard error that is not a terminal, and only
when its ancestry matches the "Process model" section of sudo's manual page.
With a pseudo-terminal, the sudoers default since 1.9.14, sudo forks a monitor,
which forks the command; without one, sudo forks the command itself. It
executes the command directly only when the policy defines no close function,
and the sudoers policy defines one whenever `pam_session` is on, which is the
default with PAM. A root child whose parent is not the qualified sudo already
fails account verification. So `SupervisedChild` in
[supervised_linux_amd64.go](../../internal/controller/privilege/supervised_linux_amd64.go)
walks through one or two sudo ancestors and requires the next process to run
the same file as the child, which holds because the supervisor hands sudo its
own procfs executable. A manual `sudo bootwright ... 2>&1 | jq` has a shell
above sudo and writes no line into the operator's pipe.

If a host's process tree differs, the child announces nothing. JSON mode then
reports `runtime.privilege` for every nonzero exit without a document, as
before, and a human noninteractive invocation only when sudo exited 1 having
written nothing but held lines, so neither is worse than the old rule. An
interactive invocation never adds a report after sudo ran: its standard error
already carries whatever sudo or the child said.

Evidence: `TestElevationOutcomes` covers each ending,
`TestStartFilterStripsOneAnnouncementAndPassesTheRestUnchanged` the filter, and
`TestSupervisedChildRecognizesOnlyItsOwnSupervisor` the ancestry walk over a
temporary procfs. The operator saw the elevated child two sudo processes below
the supervisor on 2026-09-16; the walk is not yet exercised against real sudo
in-tree.

# Sudo pseudo-terminal handoff

Observed on 2026-09-12 with sudo 1.9.17p2 on Fedora 43, whose sudoers enables
`use_pty`. Required behavior stays in
[local privilege](../../specs/cli.md#local-privilege-and-user-identity).

## Symptom

`bastion setup` printed its plan, asked `Confirm bastion setup for baseline?
[y/N]`, echoed the typed `y` and then stayed silent indefinitely. The typed
`y` and newline were still queued unread on the invoking terminal (`FIONREAD`
reported two bytes), the elevated child had written nothing further, held no
lock and had opened no publisher connection, and it woke about ten times a
second: the 100 ms poll in `readInputFD`.

## Mechanism

`runInteractive` re-executes Bootwright through `sudo -u '#0'`. The supervisor
gave sudo the invoking terminal on standard input but pipes on standard output
and error, because it counted output bytes and filtered sudo refusal lines
through `invocationOutput` and `invocationError`. With `use_pty`, sudo runs the
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

- [run.go](../../cmd/bootwright/run.go) hands every terminal stream of an
  interactive invocation to sudo unchanged, and
  [sudo.go](../../internal/controller/privilege/sudo.go) leaves `*os.File`
  streams unwrapped so they reach sudo as descriptors. Sudo then starts the
  child in the foreground with its normal raw-mode relay. The counting and
  refusal-filtering wrappers remain for noninteractive and JSON invocations,
  where sudo cannot prompt.
- [stdin_linux_amd64.go](../../cmd/bootwright/stdin_linux_amd64.go) performs
  one real nonblocking read before polling whenever standard input is the
  controlling terminal of another foreground process group. That read raises
  `SIGTTIN`, so sudo's background mode (output redirected to a file or pipe)
  and a shell's `fg` both hand the terminal over; an orphaned group reads
  `EIO` and the confirmation fails instead of hanging.

Evidence: `TestBackgroundConfirmationRequestsTerminalThroughJobControl`
simulates sudo's monitor with a pseudo-terminal, a session leader in the
foreground and the reader in a background process group, and
`TestSupervisorHandsFileStreamsToTheChildUnwrapped` covers the supervisor.
Real sudo remains operator-run. A background launch (`bootwright ... &`) now
stops with `Stopped (tty input)` at the prompt like any interactive program,
and `--yes` avoids the prompt entirely.

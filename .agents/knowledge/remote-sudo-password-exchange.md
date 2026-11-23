# Remote sudo password exchange

Observed on 2026-11-23 against `openssh-server-9.9p1-7.el9_8` and
`sudo-1.9.17p2-3.el9_8` (the RHEL 9.8 packages) with password SSH, password
sudo and `Defaults requiretty`, driving `Remote.manage` and `Remote.prove` of
[remote.go](../../internal/machine/account/remote.go) through the real
launcher. Required behavior stays in
[managed lifecycle enrollment](../../specs/api/machines.md#managed-lifecycle-enrollment).

## Symptoms

- An apply went silent for 90 seconds after its enrollment passwords, then
  reported `could not prove first access as <user>` or `could not prove remote
  sudo in a terminal for <user>`. The process list showed the SSH client
  holding `sudo -S -p [bootwright remote sudo password]`.
- The same two messages appeared at once for causes that had nothing to do
  with SSH or sudo authentication.
- With output redirected to a file, an apply waited for a password while the
  screen showed no prompt: the prompt had been written to standard error,
  which was the file. A rehearsal reproduced this; the operator's report did
  not involve a redirection.
- The operator's report: `apply --yes` showed the plan and then nothing, while
  the same apply without `--yes` asked for the passwords at once. Those `--yes`
  runs were fresh applies, and the run without `--yes` continued the operation
  one of them registered. A fresh apply first reads the power state of every
  managed physical Machine through its management controller (the
  [power entry rule](../../specs/state-reconciliation.md#power-before-a-new-apply-or-destroy)),
  one Ansible run per Machine, and reported nothing while it did. Only then did
  it register and prompt. A continuation makes no entry check, so it prompted
  at once. `--yes` itself changed nothing but the confirmation.
- `apply --stage controller` asked for a provided Machine's SSH and sudo
  passwords and checked its enrollment. `controllerHostFirewalls` in
  [wiring_service_firewall.go](../../cmd/bootwright/wiring_service_firewall.go),
  then named `providedHostFirewalls`, made the controller prerequisites wait
  for every directly reached managed host's firewall, and an enrolled host's
  firewall waits for its enrollment and hardware claim. A continued operation
  keeps the graph it froze, so the fix reaches a fresh operation only.
- After a manual controller reset
  ([controller teardown](../../e2e/controller-teardown.md)) the enrolled
  account stayed on the provided Machine, and the next operation's check
  refused it because its comment holds the earlier operation's ownership mark.

## Mechanism

RHEL 9.8 ships sudo 1.9.17p2, not 1.9.5p2: `sudo -V` reports "Always run
commands in a pseudo-tty", a five-minute password prompt timeout and three
password tries. It ships OpenSSH 9.9p1.

A pseudo-terminal never delivers end of input. Bootwright answered the
controlled prompt once and closed its side. When sudo refused the password it
wrote `Sorry, try again.` and the same prompt again, then waited on the
terminal for an answer that could not come: sudo's own timeout is five
minutes, longer than the 90 second bound of the session, so every wrong
password cost the whole bound. Without a terminal the closed pipe ends sudo
with `sudo: no password was provided`, so only the terminal route stalled.

Every other failure shared one message because the session discarded what
told them apart:

- The SSH client exits 255 for its own failures, a refused login included. It
  also exits 255 when Bootwright terminates it, when the connection drops
  after the command started, and when an operator interrupt reaches it before
  the invocation's context is canceled, because it runs in the foreground
  process group. That status alone never proves a refused login.
- sudo exits 1 for a refused policy, and so does the enrollment worker for a
  host state it does not own. `<user> is not in the sudoers file.` follows the
  prompt on the same line without a terminal. `Sorry, user <user> is not
  allowed to execute '<command>' as root on <host>.` quotes the whole command,
  the worker source included, so it runs past 8 KiB and a 4 KiB tail of
  standard error holds only its end.
- sudo also prefixes warnings it continues after, such as `sudo: unable to
  resolve host <name>` on a host whose name does not resolve and `sudo:
  setrlimit(RLIMIT_CORE)` in a container. A line that begins `sudo: ` is
  therefore no proof that the command did not run.
- The worker prints nothing when it refuses. A new operation that meets the
  managed account, home or `/etc/sudoers.d` rule of an earlier one refuses
  this way: the owner mark is derived from the apply operation, so an
  operation that a manual controller reset abandoned leaves an account no
  later operation owns. The
  [manual reset](../../e2e/controller-teardown.md) removes controller state
  only; the enrolled login stays on the provided Machine until it is removed
  there.
- With a terminal, the remote standard error arrives on the client's standard
  output; the client's own standard error holds only its closing notice.

## Decisions

- The prompt matcher keeps reading after it releases the password. A second
  controlled prompt marks the password as refused and ends the session at
  once, so one attempt costs one authentication failure and no wait.
- Sudo's C-locale messages are judged a line at a time from the first 256
  bytes of each line, on sudo sessions only, so the size of a message does not
  matter and no remote text is kept or shown. They choose among fixed refusals
  only; the terminal requirement keeps its exact-line match because it changes
  the route. A known warning is ignored. Any other `sudo: ` line reports that
  sudo raised an error and still points at the host state, because the command
  may have run.
- A refused password and an expired bound are judged before the exit status,
  and the exit status of `timeout` before any sudo text. Status 255 waits
  briefly for a pending interrupt, then separates a session that made progress
  from one that showed none.
- A read-only start check names itself and reports a row, so a slow host is
  visible while it is being proved.
- Apply asks for every bootstrap password right after the plan and its
  confirmation, before the power entry check, inside one prompt scope that
  `applyPrepared` opens for an automatic preparation and its installation;
  each power reading reports a row. Prompting needs no bound material: the
  host-key pin is proved before every connection, not before the prompt.
- The controller stage waits only for the controller Machine's own host
  firewall ([D282](../../specs/milestones/backlog.md#d282)).
- After a refused privileged check, the bootstrap login runs `getent passwd`
  without sudo and Bootwright classifies the entry by its comment against the
  ownership mark, never showing it.
- A hidden-input prompt is written to the terminal it reads from when standard
  error is not a terminal (`promptStream` in
  [stdin_linux_amd64.go](../../cmd/bootwright/stdin_linux_amd64.go)). Under
  sudo's pseudo-terminal the command then starts in the background, and its
  first change of terminal settings hands it the foreground.
- `sudo -S` stays. The earlier code line answered sudo through `SUDO_ASKPASS`
  because its standard input carried payloads and its terminal echoed; this
  route sends nothing else on standard input and turns echo off before the
  password, and an askpass helper would place the password on the host.

Evidence: `TestARepeatedSudoPromptRefusesThePasswordOnce`,
`TestAStalledExchangeNamesWhatTheRemoteSideAwaited`,
`TestBootstrapRefusalsNameTheirCause`,
`TestAnUnprivilegedBootstrapReadNamesOnlyAnSSHCause`,
`TestAnInterruptedSessionNamesNoSSHCause`,
`TestApplyStartCollectsEveryPromptBeforeItsDescribedChecks`,
`TestAHiddenPromptFollowsItsTerminalWhenTheErrorStreamIsRedirected`,
`TestAFreshApplyAsksForEveryPasswordBeforeItReadsMachinePowerAndShowsEachReading`,
`TestAControllerOnlyApplyLeavesAnEnrolledRemoteHostAlone` and
`TestARefusedEnrollmentNamesTheAccountItsLoginReadsWithoutSudo`. The
rehearsal measured a wrong sudo password at 90.3 seconds before the change and
1.7 seconds after it, and showed the prompt on the terminal with every output
stream redirected; real sudo and sshd remain operator-run.

## Rehearsal

A rootless container from a RHEL 9 base image with `openssh-server` and `sudo`
reproduces the host side: set a password for an account, grant it
`ALL=(ALL) ALL`, add `Defaults requiretty`, enable `PasswordAuthentication`,
remove `pam_loginuid.so` from `/etc/pam.d/sshd` and publish port 22 on the
loopback. A wrong password, an account outside sudoers, a rule that allows
another command, and a second owner mark over an enrolled account each
reproduce one cause. The terminal side runs the input code of
[stdin_linux_amd64.go](../../cmd/bootwright/stdin_linux_amd64.go) under that
container's sudo from a pseudo-terminal, with its output on the terminal,
through a pipe and in a file.

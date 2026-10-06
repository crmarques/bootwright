# Passing an environment value through sudo

Observed on 2026-09-17 with sudo 1.9.17p2 on Fedora 43, while giving the
elevated child the
[context-free acquisition route](../../specs/controller.md#the-context-free-acquisition-route).
Required behavior stays in that contract.

## Why the command line at all

`runInteractive` re-executes Bootwright through `sudo -u '#0'` and hands sudo a
minimal environment of `PATH`, `LANG`, `LC_ALL` and `TERM`. Proxy variables are
therefore already gone before sudo applies `env_reset`, so neither `env_keep`
nor the operator's exported value can reach the child on its own. The route is
passed as `NAME=value` arguments instead, which also keeps it a reviewable part
of the argument vector rather than an inherited ambient value.

## Position is load-bearing

`NAME=value` is parsed as an environment assignment **only before** the option
terminator. After `--`, sudo takes it as the command to execute. Probed
directly, with no credentials needed, because sudo reports a usage error before
it authenticates but reports a missing command after:

```
$ sudo -n BWPROBE=1
usage: sudo -h | -K | -k | -V ...        # consumed as an assignment
$ sudo -n -- BWPROBE=1
sudo: a password is required             # taken as the command
```

So `-u #0 [-n] HTTPS_PROXY=... -- /path/bootwright setup` is correct, and
moving the assignment after `--` would make sudo try to execute a file named
`HTTPS_PROXY=http://...`. `TestSupervisorPlacesRouteAssignmentsBeforeTheOptionTerminator`
pins the exact vector; nothing else would catch a reordering, because the
failure only appears against real sudo.

## What the operator's sudoers has to allow

A command-line assignment needs `setenv` in sudoers, a `SETENV` tag on the
matching rule, or a rule whose command is `ALL`. The common operator rule
(`%wheel ALL=(ALL) ALL`) matches `ALL`, so it works untouched.

Sudo sees `/proc/<pid>/exe`, never the binary's path, because
`ReexecutionPath` pins the re-execution there. Sudo rule matching compares the
command's base name before anything else (`command_matches_normal` in
[match_command.c](https://github.com/sudo-project/sudo/blob/main/plugins/sudoers/match_command.c)),
so `exe` against `bootwright` refuses a rule naming the binary outright. The
1.9.14 NEWS entry "canonicalizes command path names before matching" does not
change that: `set_cmnd_path` in
[sudoers.c](https://github.com/sudo-project/sudo/blob/SUDO_1_9_14/plugins/sudoers/sudoers.c)
canonicalizes only the command's parent directory, and the base name stays
the basename of the path as run, on 1.9.14 and on the current tree alike. A
rule must match `/proc/<pid>/exe` itself: `ALL`, or a `/proc/[0-9]*/exe`
pattern, which `fnmatch` compares against the path as run. B49's 2026-10-05
run on Fedora's sudo 1.9.17p2 denied the re-execution with a rule that named
`/proc/*/exe` beside the executable, and the refusal it reported named
`/proc/<pid>/exe` ([acceptance ledger](../../docs/acceptance.md)). A
`/proc/[0-9]*/exe` rule still needs `SETENV:` for a forwarded route, otherwise
sudo refuses with `sorry, you are not allowed to set the following environment
variables` and the invocation fails before the child starts. Running as root
avoids the forwarding entirely, because the process reads its own environment.

Only the three fixed names cross, and `safeAssignment` re-checks the name
against `controller.ProxyEnvironmentNames` and the value against printable
ASCII, so no authored value can introduce another variable into a setuid
invocation even if the caller passed one.

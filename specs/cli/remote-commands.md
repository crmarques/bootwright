# Remote commands

`machine exec` and [`cluster exec`](cluster-access.md#sessions) build one
argument vector from their payload, and every
[session](../cli.md#machine-ssh-sessions) runs exactly that vector. The
vector says, by its flags, whether a value comes from the controller or from
the Machine.

## Literal words

Without `--shell` the payload words are the vector, and each reaches the
remote command unchanged, so no shell on the Machine expands a glob, a
variable, a pipe or a redirection in it. A leading word that holds `=` names
the command, never a variable assignment. The invoking shell expands only what
the operator leaves outside quotes, against the controller.

## Machine scripts and controller values

`--shell` joins the payload words with single spaces into one script that
`/bin/sh -c --` runs on the Machine, so the script's variables, globs, pipes and
redirections take the Machine's values. Each repeatable `--var <name>=<value>`
gives the script the controller's value as the variable `<name>`: the script
begins with one `export` of every binding, each value quoted so that it is
data, never code. A name is a POSIX shell variable name, set at most once.
`--var` without `--shell`, an entry without `=`, a name that is not a shell
variable name or is repeated, and an empty script each refuse `cli.usage`
before anything is resolved, naming the flag and never a value. A value
appears in the process listings of the controller and of the Machine, so it
never carries a secret. A script is quoted in the invoking shell, and the
controller's values travel through `--var`.

## Remote sudo

`--sudo` prefixes the vector with `/usr/bin/sudo -n --`, so the Machine's sudo
runs the literal command, or the whole script with its globs, as root under
the remote account's own sudo policy. A session command has no terminal for
sudo to prompt on, so a rule that asks for a password refuses with sudo's own
message and status. Sudo itself reads a leading `<name>=<value>` word after
`--sudo` as an environment setting its policy may refuse.

Both programs are named by absolute path, so a
[session policy grant](../api/session-access-policy.md), which matches
arguments literally, admits a built vector only when it names that vector
exactly. One vector means the same on every Machine.

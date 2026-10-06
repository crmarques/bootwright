# Command-line Interface

`bootwright` exposes the [desired-state API](api.md) to operators and automation.
Equivalent logical input produces the same ordered output regardless of TTY,
locale, map iteration or discovery order. Prompts, explicit sensitive exports,
and the elapsed times, terminal redraw and interleaving of concurrent steps'
[progress rows](cli/output.md#long-running-progress) are the named exceptions.
Progress is presentation: the plan, the result rows and the receipt that follow
it are ordered by the frozen plan whatever ran together.

Read this page with the [command and flag catalog](cli/commands.md) and
[output contract](cli/output.md). Together they define the CLI. Unlisted
commands, aliases, flags, shorthands, modes and operands are usage errors. An
available command reads every flag it lists, in its request or its
presentation, and never accepts one only to ignore it;
`TestEveryRequestFieldIsRead` refuses a request field no production code reads.

Choose CLI dependencies under the
[dependency selection rule](architecture.md#dependency-selection-and-reuse).
Use the framework for parsing, command resolution, help, and completion where
supported. Add a completion dependency only if needed. Configure defaults to
the closed catalog and reject unowned hidden handlers after resolution and
before output or ambient access.

## Command tree

The [catalog](cli/commands.md#command-and-flag-catalog) is the sole list of
command paths. With no arguments, the root prints root help and succeeds.
Bare `help` prints root help; bare `completion` prints completion help. Both
succeed. `render` is the only domain parent that also executes; every other
incomplete domain parent prints help and fails with a usage error.

The former root groups `example` and `container-cluster` are absent, with no
aliases or hidden compatibility handlers. Their paths, including explicit help
requests, fail command resolution with `cli.usage` and exit `2`. Repository
examples remain desired-state inputs; the CLI does not generate them.

Private completion entries are fixed by the implementation and generated
scripts. They are absent from public help and candidates and are outside the
public closed-tree compatibility promise.

## Recognized but unavailable commands

Every invocation follows this precedence:

1. enforce the raw-argument bounds below;
2. resolve exact command and flag tokens and their syntactic values;
3. honor an explicit help request;
4. validate semantic operands, presence, cardinality, enums, formats, defaults,
   and flag relationships, including context-independent inherited flags;
5. apply the bare-`render` help special case; and
6. decide whether the resolved use case is available.

Explicit help therefore outranks required-value, enum, cardinality, and
relationship validation only after the raw bounds and command/flag syntax are
valid. It continues to follow the no-effect contract under
[Global flags](cli/commands.md#global-flags). Root help, explicit help requests, bare
`render`, bare `completion`, completion scripts, and the private completion
protocol retain their defined behavior; malformed or incomplete usage remains
`cli.usage` with exit status `2`.

A syntactically complete application invocation whose use case is unavailable
calls its injected, typed application stub. The stub accepts the validated
request and returns the shared unavailable error without performing work. The
CLI owns its temporary message and exit status. Such an invocation:

- performs no input discovery, context or
  state-root resolution, standard-input read, prompt, privilege escalation,
  secret access, random generation, ambient-configuration read, filesystem
  stat, open, read, or write, process launch, network access, or remote effect;
- in human mode, writes no standard output, emits one LF-terminated
  `[FAIL] cli.not-implemented: bootwright <command> is not implemented`
  diagnostic on standard error, and exits `1`; and
- when the command supports and selects JSON output, emits the normal single
  JSON envelope on standard output with `ok: false`, `exitCode: 1`,
  `result: null`, one `cli.not-implemented` diagnostic, and empty `logs`, emits
  nothing on standard error, and exits `1`.

`<command>` is the resolved canonical command path without arguments. Human
wording may evolve under the [compatibility rule](cli/output.md#compatibility),
but the code, command identity, streams, result absence, exit meaning, and
no-effect boundary are stable. An unavailable `plan`, `apply`, or `destroy`
emits no lifecycle receipt because no trustworthy lifecycle result exists.

## Parsing and input conventions

Before command resolution or flag parsing, Bootwright enforces these inclusive
bounds on the raw argument vector, excluding the executable name:

| Resource | Maximum |
| --- | --- |
| Argument count | 1,024 |
| Bytes in one argument | 16,384 |
| Bytes across all arguments | 1,048,576 |

The byte measures are the lengths of the raw argument strings and do not count
the executable name or any conceptual separators. Bootwright checks argument
count first. When it is within bounds, it scans arguments from left to right,
checks each argument's byte length before adding it to the aggregate, and then
checks the new aggregate. A value equal to a maximum is valid; the next unit is
not.

A raw-bound violation always emits one source-free `cli.usage` diagnostic plus
concise root help on standard error, emits nothing on standard output, and exits
`2`. It never establishes JSON mode, and neither `--help` nor argument order can
bypass it. The diagnostic does not echo an argument or its contents. Rejection
invokes no command or unavailable handler and performs no standard-input,
ambient-configuration, filesystem, state, secret, random, prompt, privilege,
process, network, or remote I/O.

Command and flag names are case-sensitive and may not be abbreviated. A long
value flag accepts `--flag <value>` or `--flag=<value>`; `-f` additionally
accepts `-f<value>`. A Boolean flag accepts its bare form as `true` or an
attached value such as `--yes=false` or `-h=false`; a separated token is a
positional operand, not its value. Boolean values use Go's case-sensitive
`strconv.ParseBool` spellings: `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`,
`F`, `FALSE`, `false`, and `False`. The only shorthands are `-h` and `-f`.

A command-local flag is recognized only after its complete owning command path
has been resolved and is not inherited by descendants. Before the complete
path is resolved, only inherited global flags and `-h`/`--help` are recognized.
At runnable `render`, a non-flag token that is not a declared subcommand starts
the operand list; later tokens do not extend the command path. These unexpected
operands fail semantic validation after explicit-help precedence, as on other
runnable commands. An explicit `help` path still requires exact subcommands.

Scalar flags follow command-line order and the last occurrence wins, including
the scalar comma-list flags `--clusters`, `--machines`, `--replace`, and
`--stage`.
Collection flags aggregate in command-line order only where this contract says
they are repeatable. `validate -f` is repeatable. `--authorize` is repeatable
and each occurrence may also contain commas. Supplying an empty required value
is a usage error.

An explicitly supplied empty scalar value is also a usage error unless this
contract names one of these exceptions: `--context=` is omission and selects
the current context; `add-ons add --version=` is absence and selects the catalog
version under the [catalog rules](cli/commands.md#flag-relationships-and-safeguards);
an optional `--sha256=` is absence; and comma-list
flags use their command-specific empty-list rules below.

Empty-value rejection is occurrence-local: a later scalar occurrence does not
erase an earlier explicitly empty occurrence. The named empty exceptions remain
valid occurrences; enum and format validation otherwise applies to the final
resolved scalar value under the last-occurrence-wins rule.

`--clusters`, `--machines`, `--replace`, and `--stage` are comma-separated
lists.
Whitespace around a member is ignored, empty members are ignored, and duplicate
members collapse to their first occurrence. Omission selects all eligible
objects only where the command row says “default all.” Empty resolved lists
preserve these command-specific compatibility rules:

- cluster-backed preflight and render treat an empty or whitespace-only value
  as omission, but reject a nonblank comma-only value;
- `machine list --clusters` treats an empty or whitespace-only value as
  omission and a comma-only value as an empty result, and a member naming no
  ContainerCluster or StorageCluster the context selects fails
  `access.target`, naming every such member and the clusters it can select;
  and
- `machine trust --machines` treats an empty or comma-only value as omission,
  while an empty or comma-only `--replace` selects no replacement; and
- a supplied `--stage` must resolve to at least one member, so a comma-only
  value is a usage error, and every member must be a declared stage name.

A selector is applied only after the complete Environment-selected graph has
been loaded, normalized, and validated. It filters presentation, checks,
artifacts, trust-store maintenance, or explicit access; it never changes
effective state, dependency closure, ownership, lifecycle scope, the frozen
plan, or continuation.

`validate -f/--file` accepts a YAML file or directory and may be repeated. The
set is compiled as one ordered desired-state input universe under [the API
contract](api.md); supplying `-f` makes validation context-free, while omission
uses the selected context input. For `context init` and `context update`, `-f/--file` instead accepts one
standalone Context configuration file; `--input-dir` accepts one desired-state
directory for immutable import. Both are optional at init. Update requires
at least one. Omission never discovers the working directory.

This document owns public input acquisition and flag cardinality. Each `-f`
occurrence is one path; commas are filename characters. Cleaned path strings
are deduplicated and discovered files are globally sorted by cleaned path. The
combined universe undergoes one Environment selection; its selected documents
are decoded and validated as one graph without staging, copying, override, or
flag-order precedence. An `Environment.spec.resources` path resolves only from
the directory containing that Environment's actual source file. Discovery,
Environment selection, duplicate-object rules, and source provenance remain
API-owned.

Commands accept no positional operands except those shown in the catalog.
`machine exec` and `cluster exec` accept a non-empty command argument vector;
`--` is optional, but is required to preserve a flag-shaped first argument or
prevent a recognized Bootwright flag from being consumed. The
`cluster oc` and `cluster kubectl` commands accept global
and local flags until the first positional payload token. Parsing then stops
and every remaining token is payload. A flag-shaped first payload token
therefore requires `--`; for example, `--name c -- --help` treats `--help` as
payload, while `--name c --help` requests Bootwright help. The `--` separator
is syntax only for these four payload-bearing commands and is a usage error on
every other command.

Standard input is read only for an ordinary confirmation, the host-key
confirmation and the session of [`machine rsh` and `machine exec`](#machine-ssh-sessions),
an explicitly requested sudo-password prompt, `secret set --password-stdin`,
or `secret set --value-stdin`. The [Secrets contract](secrets.md) requires yes
before stdin-backed replacement. No command reads an ambient desired-state configuration file. The invoking
user's explicit current-context selection is defined by [Contexts](contexts.md).

The fixed state root is defined by
[Contexts](contexts.md#storage-locking-and-publication).

### Ordinary confirmation

Every command that confirms uses one prompt, after the plan it confirms and
every safeguard decidable from durable local state. A proof that needs the
exclusive lock or a remote observation (a removal's resolution of every effect
it takes back and its quiescence gate, the reopened Secret binding, a power
command's controller identity) follows the prompt and still refuses before
registration or any effect. Every prompt names the object it acts on and, for
an object of a context, the context it acts in; `setup` and `media` act on the
host and name none. Only `y` or `yes`, case-insensitive with surrounding
whitespace ignored, accepts. Read one answer of at most 64 bytes including LF
without read-ahead. Decline, non-interactive input, cancellation or an I/O
failure refuses, and the command performs none of the effects it asked about.
A declined, non-interactive or unanswerable confirmation refuses under the
confirming command's own code (`controller.setup`, `media.store`,
`machine.power`, `lifecycle.state`, `trust.identity`, `context.state` or
`secret.store.conflict`), with the reason in its message and the command
repeated with `--yes` in `next:`; a cancellation names no command. The
repeated command is the invocation's own: its command path and every flag it
set, so a stage, Machine or source selection, an authorization and every other
choice stay, and following it never does more than the plan the operator
reviewed. A command that acts in a context names it with `--context <name>`
whether or not the invocation did, and a value no output repeats, a Secret's
`--username` or a `--from-url`, is named by a placeholder such as
`<username>`. The host-key confirmation of an [SSH session](#machine-ssh-sessions) reads its
answer the same way. `--yes` suppresses only this prompt, under
[its flag rule](cli/commands.md#flag-relationships-and-safeguards).

### Local privilege and user identity

Classify and validate arguments before privilege effects. Help, completion,
version, invalid invocations, unavailable commands, explicit-input validate and
`setup --dry-run` never elevate. The same classification selects, from the
parsed flags, the invocations that read the
[context-free acquisition route](controller.md#the-context-free-acquisition-route)
before `sudo` can prompt. Available controller setup and inspection may
need private shared-host metadata or installation privilege; their exact
effects follow [Controller](controller.md#selection-and-command-journeys).
Available commands requiring context state run as root; a
non-root invocation keeps an unprivileged supervisor and launches one exact
Bootwright child as UID 0 through the qualified absolute sudo executable. Pin
reexecution to the supervisor's verified executable through procfs so a pathname
replacement during authentication cannot substitute code. Sudo policies that
refuse that executable path fail closed. A sudoers rule must therefore match
`/proc/<pid>/exe`: sudo compares a command's base name before its path, so a
rule naming the Bootwright binary does not match the re-execution. Sudo owns
terminal password handling; Bootwright never captures the password. JSON or
noninteractive invocation uses noninteractive sudo and fails without cached or
passwordless authorization. Preserve argv, stdin payloads, outputs and status.
An interactive invocation hands sudo the invoking terminal itself on every
standard stream it occupies, so sudo's own terminal relay carries prompts and
answers to the child. A confirmation read whose terminal belongs to another
foreground process group requests the terminal through job control instead of
waiting for input that cannot arrive.

Sudo and the elevated child share one standard error, and sudo exits 1 for its
own refusals, so a noninteractive supervisor learns that the child started only
from the child. When the child runs as root, its standard error is not a
terminal, and the process above its one or two sudo processes runs the same
executable, the child first writes one fixed start line, which the supervisor
removes. Every later byte passes unchanged. In a human noninteractive
invocation, lines beginning with `sudo:` and a space that arrive before the
start line are held, at most 16 of at most 4096 bytes each, and reach standard
error once the child starts; any other line releases the held lines in order
and ends the holding. An interactive invocation passes them at once. JSON mode
forwards nothing that arrives before the start line, whatever it begins with,
such as a sudoers denial, so its standard error stays empty: it holds every
such line within the same bounds, drops any beyond them, and discards what it
held unless the `runtime.privilege` report below carries it. When sudo
cannot be run or waited for and no
result was written, the supervisor reports `runtime.interrupted` after an
interrupt and `runtime.privilege` otherwise. An interactive invocation whose
sudo ran ends with the child's status and no report of the supervisor's own,
since sudo's refusals and the child's own diagnostics, an interrupt's included,
already reached standard error. A JSON child that exits nonzero without writing
its document reports `runtime.interrupted` after an interrupt,
`runtime.internal` naming its status once it started, and `runtime.privilege`
otherwise. In a human noninteractive invocation, when the child never started
and nothing but held lines reached standard error, the supervisor reports
`runtime.interrupted` after an interrupt, and `runtime.privilege` only when sudo
exited 1 having held a line. That `runtime.privilege` report, and a JSON
invocation's, carries the held lines, each without sudo's prefix, as the reason
sudo refused. The first held line that names a cause decides its remedy: a
required password points to `sudo -v` in that terminal, or running as root; a
sudoers policy refusal points to the rule that permits `/proc/<pid>/exe`; a
failure to execute points to a local copy of the executable that root can
execute; and the sudoers policy refusing to set the forwarded
[acquisition route](controller.md#the-context-free-acquisition-route)'s
variables points to the `SETENV` tag on the sudoers rule that runs Bootwright,
or running as root. Held lines that name none of these keep the remedy to
authenticate to sudo or run as root. A report the supervisor writes replaces the held
lines and exits `130` after an interrupt and `1` otherwise, as
[streams and exit status](cli/output.md#streams-and-exit-status) requires;
every other ending forwards a human invocation's held lines and exits with the
child's status, never `0` when sudo could not be waited for. A child that
ends after the supervisor relayed an interrupt to it chose that status, so it
stands, `0` included; output still open five seconds after sudo exits counts
as sudo not being waited for. After relaying, the supervisor waits for the
child with no deadline of its own, because the child bounds its own
cancellation. A second `SIGINT` or `SIGTERM` the supervisor receives kills
sudo, which ends the child through its parent-death guard and can leave its
operation for the next command to resolve. A hangup never does: one terminal
hangup reaches the foreground job twice, from the shell that resends it to its
jobs and from the kernel when that shell exits. The child never escalates on
its own, because sudo can hand it one terminal interrupt twice: without a
pseudo-terminal the child receives the kernel's signal beside the relay, and
with one in the background sudo forwards both. Where sudo runs the child in
the foreground of a pseudo-terminal of its own, as `use_pty`, its default
since sudo 1.9.14, does for an interactive invocation, the terminal's Ctrl-C
reaches only the child, so a second Ctrl-C leaves its bounded cancellation
running; a second `SIGTERM` sent to the supervisor still kills sudo.

The supervisor owns bounded `sudo -n -v` refresh subprocesses during that child.
Keep the same parent and terminal identity. An unambiguous positive effective
timeout refreshes at half its duration, including fractional minutes. Zero and
negative timeouts need no expiry refresh. Unknown policy uses a 30-second
best-effort interval; never assume a five-minute timeout. Refresh failure or
timeout warns once on standard error, except in JSON mode, whose standard error
stays empty, and stops refresh without terminating the elevated command.
Completion/cancellation stops and reaps every refresh process. Never leave a
daemon or invalidate the user's wider sudo cache.

Capture the real invoking account before elevation. Resolve it through the
name service with the pinned, root-owned `/usr/bin/getent`: `passwd` by UID and
by name, and `initgroups` by name, each with a fixed environment, within ten
seconds and 64 KiB of output. The answers must name exactly one account, agree
on its name, UID, GID and home, give a clean absolute home, and list at most
1024 groups, its primary group included. `/etc/passwd` and `/etc/group` serve
only where `/usr/bin/getent` does not exist; one that exists but is not a
root-owned system executable refuses. Validate sudo-provided UID, GID and name
against that answer on manual sudo. Direct root uses root. So does a root
process whose sudo metadata no verified sudo parent supplied, such as a
`sudo -i` or `sudo -s` shell: it is root already, so this grants nothing and
only selects root's own context selection and files. Its `SUDO_COMMAND`
running `/proc/<pid>/exe` instead marks an elevated child whose sudo is gone,
which refuses. Ignore spoofed sudo metadata on a non-root launch. Resolve
account homes without `HOME`. An unverifiable account refuses
`runtime.privilege`, naming why, and names a local account or a clean root
login (`su -`, `sudo su -` or a root SSH session) with `--context` as the
remedy. The input directory and Context file, a media source file, secret
files, a key offered with `--ssh-id-file` and the selection files are opened
with the invoking account's credentials: a bounded helper passes each
descriptor to root, which keeps every type, stability and ownership proof.
Root identity governs only root-owned runtime storage and the Bootwright
executable, which root must be able to execute; the elevated child re-executes it through `/proc/self/exe` and never
resolves its path, which a root-squashed home would refuse. Only the
unprivileged supervisor resolves that path, before it elevates: an invoking
account that cannot search a directory on it refuses `runtime.privilege`
naming the path and that account, with the remedy of a local copy both that
account and root can read, such as one in `/usr/local/bin`.

For a usage failure, JSON mode is established only after an exact
JSON-capable command is resolved and its final scalar `--output` occurrence
validly selects `json`. Once established, a later usage failure emits the normal
JSON envelope on standard output with `ok: false`, `exitCode: 2`, `result: null`,
one source-free `cli.usage` diagnostic, and empty `logs`; it emits nothing on
standard error. The position of the valid final `--output json` occurrence does
not change this result. An error before that mode selection remains a human
usage failure. Explicit help remains human under the precedence above.

## Context and setup behavior

A context is one self-contained lifecycle unit, and its name, a lowercase DNS
label, is its identity. [Contexts](contexts.md) defines storage, selection,
initialization, input replacement, [permanent deletion](contexts.md#permanent-deletion)
and successful context command results; `context init --name <name>` requires
no desired state.

Media, secret, add-on, and context writes use verified roots, safe
single path segments, exclusive creation, restrictive permissions, bounded
input, and atomic publication. A confirmation cannot authorize overwriting an
unrelated path. Network media import follows the endpoint and supply-chain
rules in [security](security.md). The `media` commands manage the host-wide
[media store](managed-os.md#media-store).

### Controller declaration and command applicability

A complete Environment requires its
[controller Machine](api/environment.md#controller-machine). This admission
requirement does not make desired state a prerequisite for help, version,
completion, or context initialization without input. Context-free validation
can run away from the declared controller; it checks input relationships and
never verifies the invoking host.

`setup` and `preflight controller` follow the
[Controller journeys](controller.md#selection-and-command-journeys), including
preparation before context creation or Environment import and the context each
consumes. Which invocations read the invoking environment's proxy route is
owned by the [context-free acquisition route](controller.md#the-context-free-acquisition-route).
Controller selection alone does not cause setup, install a container runtime or
start services.

The local-access controller is not an SSH target. The SSH session and trust
commands must not silently turn its selection into a local shell, privileged
command or fabricated host-key operation: they refuse it with an actionable
diagnostic naming direct execution on that host, and no schema change extends
their execution authority.

## Validation, preflight, and rendering

`validate` performs discovery, strict decoding, normalization, complete graph
validation, and deterministic diagnostics. It opens no declared payload,
secret material, process, or network endpoint. A context-free invocation opens
no context store; a context-backed invocation reads only the selected immutable
input view and performs no write. Warnings and advisories do not change a
successful exit; an invalid input set exits `1`. After the CLI has resolved the
input universe, discovery and Environment selection are defined by
[the API contract](api.md).

Preflight reports current readiness but grants no mutation authority and never
replaces a fresh lifecycle operation's effect-boundary probes. A live
preflight may perform bounded read-only observation through
qualified adapters but allocates no lifecycle identity or log. Failed,
unavailable, forbidden, malformed, or contradictory observation is an explicit
failed or unknown check, never success or absence.

Any otherwise syntactically and semantically valid `render` invocation prints
human render help and succeeds when neither `--input-dir` nor `--output-dir` is
supplied. This includes an invocation with `--output json` or other valid render
flags: it emits no JSON envelope and performs no command work. The path flags
otherwise follow their [relationships](cli/commands.md#flag-relationships-and-safeguards).
What the artifact renderers write, and the Environment preflight families'
dry-run and host-key detail, are [deferred](deferred/cli-access-and-rendering.md).
`render effective` is the read-only exception: text mode emits canonical
effective YAML and JSON mode places the complete canonical effective-state
array in `result.effectiveState`, alongside admission `counts`; neither mode
writes an artifact. Rendering contacts no host, management controller, cluster,
or provider, launches no managed
operation, and creates no ownership evidence. Generated native scripts are
artifacts only and are never executed by render.

## Lifecycle behavior

`plan`, `apply`, and `destroy` act on the complete selected lifecycle unit.
They accept no positional operand, partial selector, range, mode,
reconciliation, adoption, reclaim, force, or resource-specific subcommand.

`plan` is a pure text preview of the next legal full operation or frozen
continuation point. Each preview is the
[decision](state-reconciliation.md#stages-and-the-pause-boundary) of the verb
it previews, so wherever that decision refuses, it refuses with the same
diagnostics and no receipt. With no operation, or a completed destroy, it
previews the fresh operation the current state would start, listing every block
in frozen order with the description and impacts of the verb it previews. With
an incomplete operation it previews that exact continuation point instead,
showing which blocks are already done and which block resumes, and never a
re-planned alternative, except that a failed destroy previews the fresh removal
`destroy` starts over what it has not yet proved gone. When every block of a
`running` or `unknown` operation, or of a `failed` destroy, is done, its own
verb completes the
operation's [finalization](state-reconciliation.md#lifecycle-unit) and then
settles, and the preview says so in one line above those blocks; an `apply`
whose input changed refuses there instead, and so does its preview. With a
completed apply it previews the destroy that the recorded ownership evidence
defines. It reads context state, allocates no identity, writes nothing and
creates no log.

An invocation whose work durable state already proves settles before any of
that: it presents no plan, asks for no confirmation, requires and refuses no
authorization, and reports `done` with the blocks its completed operation
finished, under the [settled-verb rule](state-reconciliation.md#lifecycle-unit).
Its result says in one line that nothing is left to do and, when it first
completed an interrupted finalization or a `destroy` first released what an
interrupted registration left, that it did only that, so a table of completed
blocks is never read as work this invocation performed.

`apply` and `destroy` present the frozen plan, which marks each step that
consumes an authorization and closes with the tokens it requires, then check
the authorizations, then ask the ordinary confirmation. A required token not
supplied, or a supplied one the whole frozen plan does not require, then
refuses `lifecycle.authorization`, naming the consuming steps by their number
in that plan and their description and the exact command that passes, before
the ordinary confirmation and before registration. A fresh `destroy`'s plan
also closes with `Stop first`, naming the Machines whose removal its quiescence
gate observes on the host; that gate and the resolution of every effect the
removal takes back need the exclusive lock, so they follow the prompt and
still refuse before registration. The
[finalization](state-reconciliation.md#lifecycle-unit) of an interrupted
operation precedes them and presents nothing, as does a `destroy`'s release of
what an interrupted registration left. During execution they report
[progress](cli/output.md#long-running-progress) per block, with its
[presentation groups](cli/output.md#multi-machine-presentation) as sub-steps,
and they close with the ordered result, the safe log reference, `Next` (the
exact command, with `--context`, that continues a `paused`, `failed`,
`unknown` or `running` operation) and the [receipt](#lifecycle-receipt). An
interrupt after registration writes that result and receipt, then
`runtime.interrupted` naming the same command, and exits `130`. A refusal
before registration reports its diagnostics and no receipt. A root lock or
context lease that cannot be acquired fails `lifecycle.lease`, naming the safe
retry; Bootwright never takes a lease over.

`apply` and `destroy` follow the state owner's
[transitions](state-reconciliation.md#state-machine),
[execution rules](state-reconciliation.md#plan-and-execution) and
[authorization gates](state-reconciliation.md#confirmation-and-authorization).
The CLI presents the frozen plan and required authorizations, then obtains
ordinary confirmation unless `--yes` was supplied. A flag bypasses no state
or safety gate.

### Stage selection

`plan` and `apply` accept the [`--stage` flag](cli/commands.md#flag-relationships-and-safeguards);
what a selection gates, pauses and refuses is owned by the
[stage contract](state-reconciliation.md#stages-and-the-pause-boundary).
`plan --stage` previews which blocks the selection would start and which it
would defer, as the next `apply` admits them. While a block is unproved or
failed, that `apply` first resolves every unproved block, whatever its stage,
or retries the one failed block the selection admits, so the preview marks
those `resolve` or `retry`, and every other ready block is deferred behind the
first of them until that resolution or retry succeeds. Previewing a fresh or
continued `apply`, it fails `lifecycle.stage` exactly where that `apply` would,
naming the exact `apply` whose `--stage` adds the stage that would unblock
work, with `--authorize <token>` for each token the plan consumes, since that
`apply` is authorized against the whole plan; previewing a `destroy`, which accepts no selection, it fails
`lifecycle.stage` for any selection.

### Lifecycle receipt

Every resolved `plan`, `apply`, or `destroy` invocation ends its text result
with this stable machine-readable receipt unless no trustworthy lifecycle
result exists:

```text
operation: <operation-id|none>
verb: <plan|apply|destroy>
state: <preview|running|paused|failed|unknown|done>
next: <apply|continue-apply|destroy|continue-destroy|resolve|none>
```

The labels, order, enum values, escaping, and final LF are stable. Every `plan`
receipt has state `preview`, a CLI-only marker that is never persisted as an
operation state. Its `operation` names the incomplete operation the plan would
continue, or `none` when it previews a fresh operation, and its `next` names
that continuation or the verb it previews. An `apply` or `destroy` receipt
carries the exact durable `running`, `paused`, `failed`, `unknown`, or `done`
state owned by state reconciliation. A refusal before registration has no
trustworthy lifecycle result and emits no receipt. A `paused` apply is a
successful result: it exits zero and its next action is `continue-apply`. The
receipt and `status --output json` derive from the same trustworthy state. A
`running`, `failed` or `unknown` result's diagnostic names as its `next:` the
exact command its records call for, with the `--context` and `--authorize`
flags a next step carries below: a failed destroy
names the `destroy` that replaces it with a fresh removal, and an unknown apply
names beside its resolution the `destroy` that takes back what it started. A
resolution that proves an effect never performed or only partly realized names,
for an apply, that continuation with the frozen plan's tokens; for a destroy it
names no command of its own and defers to the result's last diagnostic, because
the tokens of the `destroy` that follows are those of every block still not
proved gone once the whole run settles. A
failed attempt's adapter diagnostic names its block and the
`blocks/<block-id>/attempt-NNNNNN.output` file to read in the operation's
[log directory](cli/output.md#private-operation-logs).

`next` names what the operation's own records call for, by one rule the
receipt, `plan` and `status` share: `none` once the operation is `done`;
`destroy` for a `failed` destroy holding a block that is not `done`, an
`unknown` one included, because `destroy` replaces it with a fresh removal of
what it has not removed rather than continuing or resolving it; `resolve` while
a block of any other operation reads `unknown`, whatever state the operation
records, because that block is resolved before anything else starts; and
otherwise the continuation of the operation's own verb,
which is also how that verb completes a
[finalization](state-reconciliation.md#lifecycle-unit). A settled verb reports
`done` and `none` too, over the completed operation it proved, or over `none`
when the context owns no operation at all. A completed apply admits a later
destroy, but naming it as the next action instructs an operator to undo what
just succeeded; the verbs a context admits are what `plan` reports. `apply`
appears only as the previewed verb of a pure plan, and `destroy` only as that
or as what a failed destroy calls for. Where `status` offers a next step it
offers a command this executable runs: a continuation, a resolution and a
replacement are all offered as the operation's own verb repeated —
`continue-apply` and `resolve` as `bootwright apply --context <name>` — and
`none` is offered as no step at all. No next step names a verb this executable
does not expose. Every step that names a context-backed command carries
`--context <name>` for the context `status` read
([context identity](cli/output.md#context-identity)). A continuation, a
resolution or a replacement also carries `--authorize <token>` for each token
its decision requires: the frozen plan's, or, for a failed destroy's
replacement, those of the blocks it retains. A finalization carries none, and
neither does `bootwright destroy --context <name>` offered beside an incomplete
apply, since its presented plan names them.

`status` offers a next step only where that verb's decision would pass over the
records `status` read, so it never offers a command those records refuse:

- beside no operation, `bootwright setup` alone while the host's controller
  setup has not completed, because an apply claims the host only after it
  ([host identity](controller.md#host-identity-and-shared-prerequisites)), and
  otherwise `bootwright plan --context <name>` and
  `bootwright apply --context <name>`; but where it names
  records or evidence no index accounts for, which both verbs refuse, only the
  deletion their refusal names,
  `bootwright context delete --name <name> --purge`, with `--allow-orphans`
  unless the evidence reads pristine, and nothing over evidence the deletion
  cannot read;
- over an apply that has not completed, its continuation, which only
  [finalizes](state-reconciliation.md#lifecycle-unit) an apply whose blocks are
  all `done`, a `failed` one included, unless a lost block record refuses it,
  and `bootwright destroy --context <name>`, unless `status` names a
  contradiction of the apply's
  records, each of which refuses that removal
  ([continuation and removal](state-reconciliation.md#continuation-and-removal));
- over a destroy that has not completed, the `destroy` that continues,
  resolves, finalizes or replaces it, unless a lost block record refuses a
  continuation; a replacement reads no such record, so `status` names no lost
  record of a destroy it replaces as a contradiction;
- over either incomplete operation, `bootwright setup` in place of a
  continuation or resolution that still has a block to run while the host's
  controller setup has not completed, because that continuation re-proves the
  setup and refuses an incomplete one; a finalization and a replacement are
  not held to it and stay offered;
- over a completed operation, nothing, except over a completed destroy holding
  a block that is not `done`, which both verbs refuse naming the deletion of
  the context: there that deletion, as beside unindexed records;
- over an operation whose continuation or removal reopens a Secret binding the
  context's keyring no longer lists, or whose keyring listing fails reporting
  the material corrupt or undecryptable, as the reopen would, in place of any
  step above,
  `bootwright context delete --name <name> --purge --allow-orphans`, because
  no verb can reopen that binding and nothing stands in for it
  ([continuation and removal](state-reconciliation.md#continuation-and-removal)),
  preceded by `bootwright apply --context <name>` over an apply whose blocks
  are all `done`,
  which that apply finalizes without reopening the binding; a destroy whose
  blocks are all `done` reopens none, because the `destroy` offered above
  finalizes it.

An apply that has not completed owns every block it started, whichever state
it stopped in, so taking them back is as legitimate a way forward as
continuing. A completed apply is the exception, for the reason above. These are
next steps only; the receipt's `next` still names what the operation's own
records call for.

## Resource inspection and explicit access

List and info commands derive their result from validated desired state,
context-owned artifacts, and durable ownership evidence. A name can locate an
entry but never proves identity or ownership. The one cluster access command
available is the [administrator access export](#administrator-access-export);
the detailed contract of cluster inspection, access handoff and node selection
is [deferred](deferred/cli-access-and-rendering.md) to the parked item
[B101](milestones/backlog.md#b101).

`secret show` and `cluster kubeconfig` are raw sensitive-byte exports.
They require an exact context and object, perform no implicit fallback, emit
only the requested material, and add no status prefix or suffix. A
`cluster info --secrets` result is instead structured and explicitly sensitive
over its exact resolved selection; in JSON, values occur only in fields whose
names end in `Value`. Neither form copies a sensitive value to a diagnostic,
log, history, cache, terminal title, or second stream. Callers are responsible
for a restrictive destination if they redirect an explicit reveal. Help and
completion never reveal.

`machine list` reports two independent things about each selected Machine and
never conflates them. Its lifecycle position is how far this context's own
operations have carried the Machine, which durable evidence proves locally. Its
power is what the Machine's own management controller reports right now, which
only that controller can answer.

The lifecycle position names the verb that last acted on the Machine and
whether that verb completed: `applied` once an apply proved its realization,
`applying` while an apply has not, `destroyed` once a destroy completed,
`destroying` while a destroy has not, `failed` when an attempt said so,
`unknown` when an attempt proved nothing, and `not-applied` when no frozen plan
names the Machine at all. A declaration alone never reports ownership, and a
Machine outside the evidence is never reported as absent.

A removal reads the apply it takes back as well as its own plan. Each attempt
covers only what is not yet proved gone, so a Machine an earlier attempt
removed leaves the current plan without ceasing to be removed: it reports
`destroyed` rather than falling back to `not-applied`, and only a Machine no
completed apply ever realized reports `not-applied` after a removal. A Machine
whose removal has not completed still owns what it has not removed, so it
reports `destroying` rather than either settled position.

A Machine's contact and its addresses are separate facts, and the listing
reports both, as the `CONTACT` and `ADDRESSES` columns and the `contact` and
`addresses` fields of its [JSON row](cli/output.md#machine-results). The
contact is the one address its SSH access resolves, which is
commonly a DNS name; the addresses are every IP the Machine declares, each
without its prefix, in the order declared and without repetition. A DNS contact
is not an address, a Machine may declare several, and one whose addresses are
assigned at runtime declares none and reports none: an inspection contacts no
host, so it never reports an address desired state does not carry.

`--power-status` additionally reads each selected Machine's power state from
[its own management controller](substrates.md#identity-and-power-operations),
reporting `on`, `off`, or `unknown` when the controller gave no usable answer.
It is the only part of this command that contacts a host: without it the
command compiles local state alone and opens nothing. The reading registers no
operation, publishes no evidence and changes nothing, so it neither claims nor
releases ownership and never becomes durable. One bounded run answers for every
Machine reachable through the same host, and one controller that does not
answer leaves its own Machine `unknown` rather than denying the readings that
run already has. A Machine this context resolves no reachable management
controller for — one declaring no controller, or one whose emulated controller
this context does not currently own — reports no reading at all, which is
distinct from `unknown`. The result states separately whether controllers were
asked, so a reading nobody requested is never mistaken for one that came back
without an answer.

A reading names no retained output. It is an inspection, not an operation: it
either succeeds into a listing or refuses with its own diagnostic, so there is
nothing for an operator to resume or read afterwards, no log location is
reported for it and no refusal of it points at retained output. It therefore
[keeps none](cli/output.md#bounded-run-output): what its adapter printed is
discarded rather than left where nothing names it.

### Administrator access export

`cluster kubeconfig --name <cluster>` reveals the administrator kubeconfig a
[completed installation](container-clusters.md#installation) left in
the context's [custody](secrets.md#produced-material). It resolves the explicit
`--context`, or the current context, once; compiles that context's effective
state; and resolves `--name` in the selected graph's shared
ContainerCluster/StorageCluster name namespace. It never searches outside the
selected graph and never selects another cluster. Every ContainerCluster is
OpenShift or OKD, so every selected ContainerCluster is applicable. The checks
run in this order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| No selected ContainerCluster or StorageCluster has the name, including one only an excluded file declares | `access.target`, naming the context and the name | name a ContainerCluster the context selects; `bootwright render effective --context <context>` lists them |
| The name is a managed or external Ceph StorageCluster | `cluster.not-applicable`, naming `bootwright cluster kubeconfig`, the cluster, its kind and the applicable targets, OpenShift and OKD ContainerClusters | `bootwright render effective --context <context>` lists the clusters the context selects and their kinds |
| The context's custody holds no kubeconfig for the ContainerCluster | `access.unavailable`, naming the cluster | `bootwright apply --context <context>` |

The target and its applicability are settled from the compiled graph before
any custody read, so a name that selects nothing, or selects a StorageCluster,
reads no credential bytes. A store this build cannot open refuses with its own
[`secret.store.*` diagnostic](secrets.md#local-keyring-v4). The discovery
action is `render effective` because `cluster list` and `cluster info` are
unavailable. On success, standard output is exactly the custodied bytes with no
added LF, and standard error is empty; on any failure, standard output is empty
and standard error holds exactly one diagnostic. The entry exists from the
apply that proves the installation complete until the destroy that completes
the context's removal withdraws it, and `context delete --purge
--allow-orphans` removes it with the keyring.

### Machine SSH sessions

`machine rsh` and `machine exec` open one SSH session on the exact Machine as
the identity that Machine's desired state authorizes, using one pinned SSH
client. They register no lifecycle operation, freeze no plan and change no
ownership, continuation or desired state.

The login account and the credential that opens it are one resolved value;
no path resolves one without the other. A Bootwright-installed Machine
resolves the `bootwright` account with the fleet
[`remoteMachinesAccessKey`](api/environment.md#remote-machine-access-and-install-defaults);
every other Machine resolves `access.ssh.user` with its declared `auth` arm.
A `privateKeyRef` arm opens the named `Secret` for the session alone, a
`passwordRef` arm lets the client prompt on the terminal and names the
`secret show` that reveals the password, and an `operatorIdentity` arm offers
the default identities of the account the client runs as. Confidential
material is never written to a named path, an argument, the environment or any
durable state, and the operator is never asked to export it.

`--ssh-user` reaches any account on any Machine, including one Bootwright
installed. Naming the account the Machine already declares resolves to that
account's own credential. Naming any other account offers it none, because a
credential opens exactly the account it was authored for; such an invocation
must supply `--ssh-id-file`, and without one it fails `access.unavailable`
rather than putting one account's key on the wire under another's name.
`--ssh-id-file` is offered ahead of the declared credential, which remains the
fallback; a leading `~` resolves from the invoking account database, the file
is opened with the invoking account's credentials and must satisfy the
private-file rules in [security](security.md), and the key reaches the client
as a private copy. Neither flag alters desired state or the managed identity
Bootwright installs, and neither `--ssh-user-for-provisioned` nor
`--ssh-ask-sudo-password` is consumed.

The host key is proved before any credential is offered, from exactly one
source in this order: the Machine's `access.ssh.knownHostsRef` `Secret`; for a
Bootwright-installed Machine, the host key its
[installation evidence](substrates.md#identity-and-power-operations) proves,
which requires that this context currently owns that installation and that the
session dials the address that installation proved, so another address refuses
`trust.identity` naming the proved one; the
[context trust store](contexts.md#storage-locking-and-publication); and
finally, on an interactive terminal alone, one observation of the endpoint
confirmed against its displayed fingerprint and recorded as context trust. The
prompt names the context, and when a record of a Machine the context no longer
declares holds the endpoint, the confirmed write replaces that record, which
standard error names before the prompt; a record of a Machine still declared
that pins the endpoint to another key fails `trust.identity` before anything
is asked. Without a terminal an unproved key fails `trust.identity` and names
`machine trust`. Recording requires that explicit confirmation, so an
observation by itself trusts nothing. The session pins exactly the proved key
and its algorithm, so a Machine presenting another key fails to connect; a
changed key is never recorded by a session, and is superseded only by
`machine trust --replace`.

The session is the operator's. Standard input, output and error belong to the
client for its whole duration, its exit status is the result, and Bootwright
writes only diagnostics and the host-key confirmation, on standard error,
before the connection opens. `machine exec` runs the exact argument vector and
reports the remote command's exit status; `machine rsh` accepts no command
tail. The client reads no ambient SSH configuration, inherits no agent,
receives only terminal-identifying environment values, and is given the
session's material as open descriptors rather than named files.

Resolution follows explicit access: an unknown or excluded name fails
`access.target`, a Machine reached locally fails `access.unavailable` naming
direct execution on that host, and a Machine declaring no resolvable SSH
access fails `access.unavailable`. Each is exit `255` with empty standard
output, no connection attempt and no trust record. Every refusal Bootwright
reports before the session opens, `trust.identity` and its refusals at the
privilege boundary included, exits `255`, the SSH client's own failure status,
and so does a noninteractive invocation whose sudo exits `1` before the
child's start line, whatever sudo wrote, a sudoers denial included, which
reaches standard error as it came; a usage refusal keeps `2` and an interrupt
before the session keeps `130`. A status from `0` to `254` is therefore the
remote command's, except at an interactive terminal: sudo writes its own
refusal there and exits `1`, and the child writes no start line to a terminal,
so the supervisor cannot tell that refusal from a remote command's `1` and
keeps it. Once the session
opened, its status is the result even after an interrupt.

### Host-key trust

`machine trust` records in the
[context trust store](contexts.md#storage-locking-and-publication) the host key
each selected Machine that uses context trust presents: one reached over SSH
that declares no `access.ssh.knownHostsRef` and that Bootwright did not install.
It observes each such endpoint exactly once, offering no credential, and
reports one action for every selected Machine: `add` for a key the context
does not trust yet, `reuse` for the key it already trusts at that endpoint,
`replace` for a supersede, and `skip`, with its reason, for every other
Machine, which it never contacts. An `add` or `replace` whose endpoint a record
of a Machine the context no longer declares holds removes that record in the
same write, reported after the selected Machines as a `remove` row naming the
Machine, the endpoint, the key removed and which Machine now uses the address;
it counts as pending, but not as a Machine checked. One endpoint that cannot be
read fails the whole enrollment, which records nothing.

A changed key fails `trust.identity` naming both fingerprints. An unchanged key
observed at another endpoint fails `trust.identity` naming the endpoint the
context trusts it at, since the store trusts a key at one address. Either is
superseded only by `--replace` naming that Machine. A plan that would still pin
one endpoint to two keys, because a Machine the context still declares holds it
with another, fails `trust.identity` before it is shown, in a dry run too,
naming both Machines, the endpoint and the re-trust of the other Machine. When
that Machine no longer uses this context's trust, because it is reached
locally, installed, declares no SSH access or declares a `knownHostsRef`, a
re-trust of it refuses and nothing reads its record, so the refusal says why
and names the one change that drops the record: leave it out of the input for
one `context update`, repeat the command, then restore it. The remedy also
names what that change needs and costs: `context update` refuses while an
operation is incomplete, as the
[mutation guard](state-reconciliation.md#context-mutation-evidence) states; an
input that drops a Machine another object references, such as a cluster
member, does not compile; and each update publishes a new input revision, so
after a completed apply the next apply no longer settles but refuses the
changed input until a [destroy](state-reconciliation.md#lifecycle-unit). The
Machine reached locally is the
[controller Machine](api/environment.md#controller-machine), which leaves the
input only when the Environment's `spec.controller.machineRef` names another
local Machine. Before an apply binds the context, that input compiles and the
same steps drop the record. Once an apply has bound the context, the
[controller binding](contexts.md#controller-relationship-and-host-binding),
not compilation, refuses any input that changes the controller Machine, so
that refusal's remedy says no input edit drops the record and only a separate
context does.

With pending writes and no `--yes`, the plan is written to standard output
before the one [ordinary confirmation](#ordinary-confirmation): every Machine's
action, key and fingerprint, what a replacement supersedes, the earlier
fingerprint and, when it moved, the earlier endpoint, and every record a
`remove` row drops. The plan is the same observation the command records.
After recording, the result repeats only its headline; a declined,
non-interactive or canceled confirmation records nothing.
Under `--yes` or `--dry-run`, and in JSON, which
[requires one of them](cli/commands.md#flag-relationships-and-safeguards),
nothing precedes the result, which carries every Machine's row once.

### Machine power operations

`machine start`, `machine stop` and `machine restart` converge one exact
Machine to a power state. They are bounded operations, not desired state: they
register no lifecycle operation, freeze no plan, and change no ownership,
continuation, or frozen plan, so a machine an operator stopped is still a
machine this context owns and will reconcile.

Resolution follows explicit access: an unknown or excluded name fails
`access.target`, and a Machine this executable cannot reach a management
controller for fails `access.unavailable`, both with exit `1`, empty standard
output and no effect. A Machine whose controller is emulated by its provider
is reachable while the provider's machine block for it stands, because the
controller is one of that block's own effects: from the moment that block
completed under the current apply, whatever the Machine's installation reached,
until a removal proves the block gone. A Machine that authors its own
controller is reachable whenever that controller answers.

Stopping and restarting ask the
[ordinary confirmation](#ordinary-confirmation), whose prompt names the
Machine and its context; a declined or unanswerable one refuses
`machine.power` naming the command that repeats it with `--yes`. The
controller identity check against the pin follows the prompt and still
refuses before any power request.

Every power request crosses [the Machine's management controller](substrates.md#identity-and-power-operations)
over the one adapter boundary, under the context's shared lock, so a power
operation and a lifecycle mutation never run against the same host at once.
A request is not evidence: the result reports the state the controller proved
once the operation settled, together with the state before it and whether
anything changed. A state that never arrived within the bounded window is
`lifecycle.unknown` with exit `1`, never a success. What the adapter printed is
[retained and named for the run](cli/output.md#bounded-run-output). Stopping
asks the operating system to shut down and polls it to off; `--force` cuts the
power instead, and is a separate request rather than a fallback. Restarting
proves the stop before it starts, so an interrupted restart is never reported
as settled.

A physical Machine whose [pin](substrates.md#physical-machine-realization) the
context's current apply recorded is held to it: a controller reporting another
identity refuses `lifecycle.state` with exit `1` before any power request. The
adapter names that refusal to its runner before it fails, so the diagnostic's
object is the Machine, its message names the controller endpoint and that it
answers as another system, and its remedy is to correct the Machine's
controller address or destroy and apply the context so the machine is proved
again, pointing at the run's retained output for the identities. That output
names the Machine and both identities, and the diagnostic repeats neither: each
is what a controller reported, so it is printed with every character removed
that its [evidence](substrates.md#physical-machine-realization) refuses as not
printable, a format character such as a bidi override included, and then cut
to 128 characters, the bound that evidence holds it to. A
current-operation record or pin of that Machine that cannot be read refuses
`lifecycle.state` before any confirmation or run.

[`machine list --power-status`](#resource-inspection-and-explicit-access)
reads the same controllers over the same boundary and under the same lock, and
follows the same reachability rules, but drives nothing: it asks each
controller what state it is in and reports the answer. Because it observes
rather than converges, an unreachable controller leaves its own Machine
`unknown` instead of refusing, and a Machine no controller resolves for is
omitted from the reading instead of failing `access.unavailable`.

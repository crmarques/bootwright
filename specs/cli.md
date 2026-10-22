# Command-line Interface

`bootwright` exposes the [desired-state API](api.md) to operators and automation.
Equivalent logical input produces the same ordered result facts regardless of
TTY, locale, map iteration or discovery order. Human presentation may vary with
explicit flags and injected terminal capabilities; structured JSON and YAML
retain their documented DTOs. Prompts, explicit sensitive exports, and the
elapsed times, terminal redraw and interleaving of concurrent steps' [progress
rows](cli/output.md#long-running-progress) follow their named stream contracts.
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
- in human mode, writes no standard output, emits one LF-terminated error
  group on standard error with code `cli.not-implemented` and message
  `bootwright <command> is not implemented`, and exits `1`; and
- when JSON or YAML output is selected, emits the normal single
  selected-format envelope on standard output with `ok: false`, `exitCode: 1`,
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
`2`. It never establishes a structured output mode, and neither `--help` nor argument order can
bypass it. The diagnostic does not echo an argument or its contents. Rejection
invokes no command or unavailable handler and performs no standard-input,
ambient-configuration, filesystem, state, secret, random, prompt, privilege,
process, network, or remote I/O.

Command and flag names are case-sensitive and may not be abbreviated. A long
value flag accepts `--flag <value>` or `--flag=<value>`; `-f` and `-o` additionally
accept attached values such as `-f<value>` and `-ojson`. A Boolean flag accepts
its bare form as `true` or an
attached value such as `--yes=false` or `-h=false`; a separated token is a
positional operand, not its value. Boolean values use Go's case-sensitive
`strconv.ParseBool` spellings: `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`,
`F`, `FALSE`, `false`, and `False`. The only shorthands are `-h`, `-f` and inherited `-o`.

A command-local flag is recognized only after its complete owning command path
has been resolved and is not inherited by descendants. Before the complete
path is resolved, only inherited global flags and `-h`/`--help` are recognized.
At runnable `render`, a non-flag token that is not a declared subcommand starts
the operand list; later tokens do not extend the command path. These unexpected
operands fail semantic validation after explicit-help precedence, as on other
runnable commands. An explicit `help` path still requires exact subcommands.

Scalar flags follow command-line order and the last occurrence wins, including
the scalar comma-list flags `--clusters`, `--machines`, `--replace`, `--stage`,
the `--name` that names several Machines and the `--name` and `--node` that name
several clusters and nodes.
Collection flags aggregate in command-line order only where this contract says
they are repeatable. `validate -f` is repeatable. `--authorize` and
`destroy --unregistered` are repeatable and each occurrence may also contain
commas. Supplying an empty required value
is a usage error.

An explicitly supplied empty scalar value is also a usage error unless this
contract names one of these exceptions: `--context=` is omission and selects
the current context; an optional `--sha256=` is absence; and comma-list
flags use their command-specific empty-list rules below.

Empty-value rejection is occurrence-local: a later scalar occurrence does not
erase an earlier explicitly empty occurrence. The named empty exceptions remain
valid occurrences; enum and format validation otherwise applies to the final
resolved scalar value under the last-occurrence-wins rule.

`--clusters`, `--machines`, `--replace`, and `--stage` are comma-separated
lists, and so are the required `--name` of `machine start`, `machine stop`,
`machine restart` and `machine exec`, the required `--name` and the `--node` of
`cluster exec`, and the optional `--name` of `cluster info`, `machine info` and `secret info`.
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
  value is a usage error, and every member must be a declared stage name; and
- a supplied `--name` of `machine start`, `machine stop`, `machine restart`
  and `machine exec` must likewise resolve to at least one member, so a
  comma-only value is a usage error. `machine rsh` opens one interactive
  session and keeps a single `--name`: a value containing a comma is a usage
  error;
- a supplied `--name` of `cluster exec` and `cluster info`, and a supplied
  `--node` of `cluster exec`, must likewise resolve to at least one member, so
  a comma-only value is a usage error. `cluster rsh` opens one interactive
  session, so a comma in its `--name` or `--node` is a usage error; and
- `machine info` and `secret info` omit `--name` to select all metadata rows;
  supplied names match exactly after list normalization. A supplied `--name`
  must likewise resolve to at least one member, so a comma-only value is a
  usage error, and a member naming no Machine or Secret the context holds
  fails `access.target` with exit status 1, naming every such member and the
  list command that shows the resources the context selects, as `cluster info`
  does ([D165](milestones/backlog.md#decisions)). An explicitly empty scalar
  occurrence still fails under the general rule above; and
- `cluster info --secrets` requires `--name` to name exactly one cluster after
  normalization, so a list of several is a usage error.

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
`machine exec`, `cluster exec`, `cluster oc` and `cluster kubectl` accept a
non-empty command argument vector and share one rule, as `oc` and `kubectl`
do. They accept global and local flags until the first positional payload
token. Parsing then stops and every remaining token is payload verbatim,
flag-shaped or not, including a token that spells a Bootwright flag such as
`--name` or `--context`, so a remote `kubectl --context prod get pods` never
retargets the session. A flag-shaped first payload token therefore requires
`--`; for example, `--name c -- --help` treats `--help` as payload, while
`--name c --help` requests Bootwright help, and `--name c ls -la` runs `ls -la`.
A required Bootwright flag written after the payload is missing, and the usage
refusal adds that Bootwright flags go before the remote command. The `--`
separator is syntax only for these four payload-bearing commands and is a usage
error on every other command.

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
effects follow [Controller](controller.md#the-setup-flow).
Available commands requiring context state run as root; a
non-root invocation keeps an unprivileged supervisor and launches one exact
Bootwright child as UID 0 through the qualified absolute sudo executable. Pin
reexecution to the supervisor's verified executable through procfs so a pathname
replacement during authentication cannot substitute code. Sudo policies that
refuse that executable path fail closed. A sudoers rule must therefore match
`/proc/<pid>/exe`: sudo compares a command's base name before its path, so a
rule naming the Bootwright binary does not match the re-execution. Sudo owns
terminal password handling; Bootwright never captures the password. JSON, YAML or
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
and ends the holding. An interactive invocation passes them at once. Structured mode
forwards nothing that arrives before the start line, whatever it begins with,
such as a sudoers denial, so its standard error stays empty: it holds every
such line within the same bounds, drops any beyond them, and discards what it
held unless the `runtime.privilege` report below carries it. When sudo
cannot be run or waited for and no
result was written, the supervisor reports `runtime.interrupted` after an
interrupt and `runtime.privilege` otherwise. An interactive invocation whose
sudo ran ends with the child's status and no report of the supervisor's own,
since sudo's refusals and the child's own diagnostics, an interrupt's included,
already reached standard error. A structured child that exits nonzero without writing
its document reports `runtime.interrupted` after an interrupt,
`runtime.internal` naming its status once it started, and `runtime.privilege`
otherwise. In a human noninteractive invocation, when the child never started
and nothing but held lines reached standard error, the supervisor reports
`runtime.interrupted` after an interrupt, and `runtime.privilege` only when sudo
exited 1 having held a line. That `runtime.privilege` report, and a structured
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
jobs and from the kernel when that shell exits. The child escalates on its own
second `SIGINT` or `SIGTERM` only where it proves, at that signal, that it is
the foreground process group of a pseudo-terminal other than the supervisor's
terminal, as sudo's `use_pty`, its default since sudo 1.9.14, leaves an
interactive invocation; there each Ctrl-C reaches only the child, once, and
the second ends it at once with status `130`, which can leave its operation
for the next command to resolve. Without a pseudo-terminal, or with one in the
background, sudo can hand the child one interrupt twice, the kernel's signal
beside the relay or both forwarded, so there the child never escalates on its
own and the supervisor's second signal kills sudo.

The supervisor owns bounded `sudo -n -v` refresh subprocesses during that child.
Keep the same parent and terminal identity. An unambiguous positive effective
timeout refreshes at half its duration, including fractional minutes. Zero and
negative timeouts need no expiry refresh. Unknown policy uses a 30-second
best-effort interval; never assume a five-minute timeout. Refresh failure or
timeout warns once on standard error, except in structured mode, whose standard error
stays empty, and stops refresh without terminating the elevated command.
Completion/cancellation stops and reaps every refresh process. Never leave a
daemon or invalidate the user's wider sudo cache. The same `sudo -n -ll`
listing decides whether a standard-input `secret set` refuses before sudo
starts it because the policy logs input
([secret custody](secrets.md)).

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
account and root can read, such as one in `/usr/local/bin`. Since root runs
that file, the supervisor refuses `runtime.privilege` before sudo when another
account could replace it: when the resolved file or any directory above it is
owned by a UID other than 0 or the invoking account's, or is writable by group
or others, except a root-owned sticky directory such as `/tmp`, which lets no
account replace another's entry. The report says sudo would run that path as
root and names the file or directory and its owner or write bits, with the
remedy `sudo install -m 0755 <path> /usr/local/bin/bootwright`. The invoking
account's own build output, such as `bin/bootwright` at mode `0755` beneath its
home, stays allowed.

After raw bounds pass, a valid final inherited `--output json` or `--output
yaml` selects the corresponding single-document envelope for usage failures,
including command-resolution failures. It carries `ok: false`, `exitCode: 2`,
`result: null`, one source-free `cli.usage` diagnostic and empty `logs`, and
writes nothing on standard error. The selector may precede or follow the
command path. Invalid selectors remain human failures, and flag-shaped remote
payload never selects Bootwright's mode. Explicit help retains precedence over
semantic validation and uses the selected human or structured representation.

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
[Controller journeys](controller.md#the-setup-flow), including
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
render help and succeeds when neither `--input-dir` nor `--output-dir` is
supplied. It uses the selected human, JSON or YAML help representation and
performs no command work. The path flags
otherwise follow their [relationships](cli/commands.md#flag-relationships-and-safeguards).
What the artifact renderers write, and the Environment preflight families'
dry-run and host-key detail, are [deferred](deferred/cli-access-and-rendering.md).
`render effective` is the read-only exception: text mode emits canonical
effective YAML, and either structured mode places the complete canonical effective-state
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
  unless the evidence reads pristine, which over evidence the guard cannot
  read abandons what it cannot list;
- over an apply that has not completed, its continuation, which only
  [finalizes](state-reconciliation.md#lifecycle-unit) an apply whose blocks are
  all `done`, a `failed` one included, unless a lost block record refuses it
  or its re-proof refuses on what `status` reads,
  and `bootwright destroy --context <name>`, unless `status` names a
  contradiction of the apply's
  records, each of which refuses that removal
  ([continuation and removal](state-reconciliation.md#continuation-and-removal));
- over a destroy that has not completed, the `destroy` that continues,
  resolves, finalizes or replaces it, unless a lost block record refuses a
  continuation or its re-proof refuses on what `status` reads; a replacement
  reads no such record, so `status` names no lost record of a destroy it
  replaces as a contradiction;
- over either incomplete operation whose continuation or resolution still has
  a block to run, no continuation when the input revision or digest or the
  frozen execution closure differs from what the operation registered with,
  when another build registered it and this executable does not freeze the
  same blocks ([dependency safety](state-reconciliation.md#dependency-safety-during-recovery)),
  or when the controller state records no binding for the context; the
  destroy that supersedes an incomplete apply stays
  offered, and over a running, paused or unknown removal the deletion that
  refusal names is offered where it names one, and otherwise no step. `status`
  probes no host, so a binding recorded for another host is left to the
  continuation's own refusal;
- over either incomplete operation, `bootwright setup` in place of a
  continuation or resolution that still has a block to run while the host's
  controller setup has not completed or while its retained receipt holds no
  execution definition, because that continuation re-proves the setup and
  refuses either; the removal beside an incomplete apply and the replacement
  of a failed removal, which prove the setup before they resolve or register
  anything, are then replaced by it too, and a finalization is not held to it
  and stays offered;
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

### Desired and realized state comparison

`diff` sets the context's desired state beside what its apply realized, object
by object, by the [comparison rules](state-reconciliation.md#comparing-desired-and-real-state).
It accepts no positional operand, input path or mode, and it reads the
selected context's immutable input revision, which is exactly what `apply`
reads. It reads durable records under the shared lock, so it contacts no host,
registers no operation, allocates no identity, writes no record, binds no
Secret and creates no log. While an apply or destroy holds the context it
refuses `lifecycle.lease`, naming the safe retry; every other refusal is a
lifecycle read's own.

`--observe` also reads each block the context's apply realized through that
block's bounded read-only observation, on the host that block froze, by the
[live comparison rules](state-reconciliation.md#reading-the-real-side-live).
It still registers no operation, writes no record and binds no Secret: it
reopens the binding the apply froze, read-only, and adds only the refusals
those rules name. A long `--observe` holds the context's shared lock while it
observes, so an `apply` or `destroy` started meanwhile refuses
`lifecycle.lease`. What it cannot read, such as a collected input revision
or an object whose blocks failed, is shown as unknown beside the object and is
never a refusal. It exits `0` whenever it renders a comparison, whatever the
comparison found, and writes no [lifecycle receipt](#lifecycle-receipt).

The text result opens with these header lines, each value starting at column 11:

| Line | Value |
| --- | --- |
| `Context` | `<name> at input revision <selected revision>` |
| `Against` | `nothing; this context holds no operation, so nothing is realized`, or `apply <id> (<state>) of input revision <revision>, as its records prove; no host was contacted`, or `destroy <id> (<state>) of apply <id>, as its records prove; no host was contacted`, or, with `--observe`, `what apply <id> (<state>) realized, read live: <n> blocks observed, <u> unknown, <p> not projected` |
| `Input` | `not applied yet; bootwright apply --context <name> realizes it`; `unchanged since <apply>; bootwright apply settles over it and repairs nothing shown here` for a done apply; `unchanged since <apply>, which bootwright apply --context <name> continues` for an incomplete one or `..., which bootwright destroy --context <name> takes back` under a removal; or `changed since <apply>; bootwright apply refuses it until bootwright destroy --context <name> takes back what that apply owns` |
| `Plan` | `this executable plans the same blocks`; `this executable plans <c> blocks differently, <a> more and <r> fewer`; `this executable no longer plans the input: <message>`; or `no apply froze a plan to compare with` |
| `Build` | Only when another build than the one that registered the apply plans some of its blocks differently: `this executable plans <k> blocks of <apply> differently from the build that froze them`, followed with `--observe` by `; --observe does not read them` |

Each object whose difference is not `none`, or whose real side is `partial` or
`unknown`, follows after a blank line, in API kind order and then name:

```text
diff <source>/<Kind>/<name> desired/<Kind>/<name>
# plan: <same|changed|added|removed> (<block ids>); realized: <realization>
# not compared: <path>, <path>, ...
# <note>
# <reason>
# remedy: <remedy>
--- <source>/<Kind>/<name>
+++ desired/<Kind>/<name>
@@ -<line>,<count> +<line>,<count> @@
```

`<source>` is `applied`: the object as the input revision the apply froze
declares it, or `real`: the fields an `--observe` read projected, beside the
desired object reduced to those same fields. A `# not compared:` line names,
for a `real` side only, the declared paths no projection observed, at most 12
and then `and <n> more`; they are never a difference. The plan line reads
`# plan: none` for an object no block realizes, and `realized` reads `none`
for an object the operation does not name. A `#` line carries a note, then the reason and remedy of an absent or
unknown real side. The `---` side is `/dev/null` when the real side is absent,
and the `+++` side is `/dev/null` when the object is no longer declared. The
hunks are a unified line difference of the canonical YAML of a catalog holding
that one object, with three lines of context, adjacent hunks merged, and every
line shown with the display escaping of the [output contract](cli/output.md#json-output).
Counts are always written, even when `1`, and an empty side starts at line `0`.
A pair of sides so large that their line comparison would exceed four million
line pairs shows the changed region between their common first and last lines
as one replacement. An unknown object shows no hunk.

Whether an object differs is decided from the encoded bytes of its two sides:
unknown beats everything, an absent real side beside a declared object is
`added`, an undeclared object with a real side is `removed`, equal bytes are
`none`, and the rest are `changed`. A line the object's hunks do not hold is
never a difference.

The result closes after a blank line with
`<d> objects differ: <c> changed, <a> added, <r> removed, <u> unknown; <n> unchanged`,
or, when nothing differs, `No differences: <n> objects compared.` A count of
one is singular: `1 object differs: ...` and `No differences: 1 object
compared.` With
`--observe` the line adds `; <k> compared as applied`, or `, <k> as applied`
before the closing period, counting the present or partial objects whose
real side is the applied object. The
[JSON result](cli/output.md#diff-results) carries the same membership and the
same patch lines.

## Resource inspection and explicit access

List and info commands derive their result from validated desired state,
context-owned artifacts, and durable ownership evidence. A name can locate an
entry but never proves identity or ownership. `cluster list` and `cluster info`
report each selected cluster under the
[cluster discovery contract](cli/output.md#cluster-discovery). Access to a
cluster is one of four contracts: the
[administrator access export](#administrator-access-export) of a
ContainerCluster, the [cluster sessions](#cluster-sessions) of a managed Ceph
cluster, that cluster's [storage dashboard access](#storage-dashboard-access),
and the [client keyring export](#client-keyring-export) of one of its
StorageClients.
The `oc` and `kubectl` handoffs and the sessions on a ContainerCluster are
[deferred](deferred/cli-access-and-rendering.md) to the parked item
[B101](milestones/backlog.md#b101).

`secret show`, `cluster kubeconfig` and `cluster keyring` are raw
sensitive-byte exports.
They require an exact context and object, perform no implicit fallback, emit
only the requested material, and add no status prefix or suffix. A
`cluster info --secrets` result is instead structured and explicitly sensitive
over its exact resolved selection; in JSON, values occur only in fields whose
names end in `Value`. Neither form copies a sensitive value to a diagnostic,
log, history, cache, terminal title, or second stream. Callers are responsible
for a restrictive destination if they redirect an explicit reveal. Help and
completion never reveal.

`machine info [--name <list>]` reads the same local inventory service as
`machine list`, with power reads disabled. It filters exact names at the CLI
boundary after the service returns, retaining an independent copy of selected
rows, and refuses a name the listing lacks before showing any row. Human output groups available Placement, Access and Evidence facts;
full detail adds declared addresses and provider identity. No host, management
controller, trust probe or credential is contacted or read. Its structured
result has the same fields as the machine listing, with its own command name.

`secret info [--name <list>]` similarly filters the existing metadata listing
and refuses a name it lacks.
It groups Identity and Custody facts without checking availability, decrypting
material, invoking reveal, or inventing usage metadata. The
[Secrets contract](secrets.md#acquisition-and-commands) owns its metadata scope.

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
the context's [custody](secrets.md#produced-material), or the copy a removal
kept there unproved from an installation whose completion was never proved. It resolves the explicit
`--context`, or the current context, once; compiles that context's effective
state; and resolves `--name` in the selected graph's shared
ContainerCluster/StorageCluster name namespace. It never searches outside the
selected graph and never selects another cluster. Every ContainerCluster is
OpenShift or OKD, so every selected ContainerCluster is applicable. The checks
run in this order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| No selected ContainerCluster or StorageCluster has the name, including one only an excluded file declares | `access.target`, naming the context and the name | name a ContainerCluster the context selects; `bootwright cluster info --context <context>` lists them |
| The name is a managed or external Ceph StorageCluster | `cluster.not-applicable`, naming `bootwright cluster kubeconfig`, the cluster, its kind and the applicable targets, OpenShift and OKD ContainerClusters | `bootwright cluster info --context <context>` lists the clusters the context selects and their kinds |
| The context's custody holds no kubeconfig for the ContainerCluster | `access.unavailable`, naming the cluster | `bootwright apply --context <context>` |

The target and its applicability are settled from the compiled graph before
any custody read, so a name that selects nothing, or selects a StorageCluster,
reads no credential bytes. A store this build cannot open refuses with its own
[`secret.store.*` diagnostic](secrets.md#local-keyring-v4). The discovery
action is `cluster info`. In default text mode, successful standard output is
exactly the custodied bytes with no added LF, and standard error is empty, or, for a copy custody
holds unproved, holds exactly one `access.unproved` warning naming the cluster and saying its
access was not proved, as the copy was kept from an installation whose
completion was never proved; on any failure, standard output is empty
and standard error holds exactly one diagnostic. The entry exists from the
apply that proves the installation complete, or from the removal that kept the
unproved copy before deleting the installer's work area
([produced material custody](state-reconciliation.md#produced-material-custody)),
until the destroy that completes the context's removal withdraws it, and
`context delete --purge --allow-orphans` removes it with the keyring. Explicit
JSON or YAML output wraps the same sensitive bytes in the
[structured export envelope](cli/output.md#streams-and-exit-status), places any
unproved-access warning in envelope diagnostics, and leaves standard error
empty.

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

Not yet met: a Machine whose `auth` is a `passwordRef` arm never tries an
offered `--ssh-id-file` key ahead of its password; tracked as
[B544](milestones/backlog.md#b544).

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
declares, or of a declared Machine that no longer uses context trust (reached
locally, installed by Bootwright, declaring no SSH access or declaring a
`knownHostsRef`), holds the endpoint, the confirmed write replaces that record,
which standard error names, with why, before the prompt; a record of a Machine
that still uses context trust and pins the endpoint to another key fails
`trust.identity` before anything is asked; a stored file that already pins an
endpoint to two keys fails `trust.identity` naming the repair of
[host-key trust](#host-key-trust). Without a terminal an unproved key fails `trust.identity` and names
`machine trust`. Recording requires that explicit confirmation, so an
observation by itself trusts nothing. The session pins exactly the proved key
and its algorithm, so a Machine presenting another key fails to connect; a
changed key is never recorded by a session, and is superseded only by
`machine trust --replace`. The proved key is then admitted against this host's
SSH crypto policy: the session pins only the key's algorithms that policy
admits, and a key whose type it excludes fails `access.unavailable` naming the
Machine, the type and the remedy, before any credential is offered.

The session is the operator's. Standard input, output and error belong to the
client for its whole duration, its exit status is the result, and Bootwright
writes only diagnostics and the host-key confirmation, on standard error,
before the connection opens. `machine exec` runs the exact argument vector and
reports the remote command's exit status; `machine rsh` accepts no command
tail. The client reads no ambient SSH configuration, inherits no agent,
receives only terminal-identifying environment values, and reads anonymous
session material through descriptors the launcher holds until the client exits.

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

`machine exec` accepts a comma-separated `--name` under the
[list rules](#parsing-and-input-conventions); `machine rsh` names one Machine.
One member after normalization is the session above, unchanged. Two or more
members run the same argument vector on every Machine at once, each as its own
session under the rules above, except where the rules below replace them.
Canonical order is ascending bytewise Machine name, and it orders refusals,
warnings and the sessions of the result.

Every member is resolved in the context's effective graph, with its identity,
before anything is lent, proved or opened. Unknown or excluded members fail
`access.target` in one diagnostic naming each of them, and any other refusal is
reported for the first refusing Machine in canonical order. A Machine whose
resolved identity is the `password` arm fails `access.unavailable`: the client
prompts for a password on the terminal, which concurrent sessions cannot share;
the diagnostic names the `machine exec` of that Machine alone as the way, and
an offered `--ssh-id-file` does not lift the refusal, because a password
identity tells the client never to try a key; a borrowed `--ssh-user` account
resolves to the operator identity and is not refused. One credential lend
covers every session. Host keys are proved, and admitted against this host's
SSH crypto policy, one Machine at a time in canonical order before any session
opens, so an interactive first-use confirmation asks about one Machine
at a time, and each recorded write re-reads the trust store before it writes.
Any refusal exits `255`, opens no session and runs nothing. `--ssh-user` and
`--ssh-id-file` apply to every Machine alike.

Every session of several runs non-interactively: it has no standard input and
its client is told never to prompt, so none can ask for a password, a
passphrase or a host key. All sessions start together and run concurrently,
with no bound. Each client's standard output and standard error reach
Bootwright's own, line by line, each line prefixed `<machine>: `; a line is
written whole, so lines of different sessions are interleaved but never mixed
within a line. An unterminated final line is completed when its session ends,
and a line that reaches 64 KiB without an end is written as a line of its own.

After every session ended, each session that returned a nonzero status, in
canonical order, writes one `access.exit` warning on standard error naming the
Machine and the status; a client that could not start counts as status `255`
and its warning says so. The invocation's status is the highest status any
session returned, `0` only when all returned `0`. An interrupt after sessions
opened signals each client as it does a single session, and the statuses that
result stand and aggregate the same way.

### Cluster sessions

`cluster rsh` opens one session on a node of a managed Ceph cluster, and
`cluster exec` opens one or, as [below](#several-clusters-and-nodes), several.
They resolve the explicit `--context`, or the current context, once;
compile that context's effective state; and resolve `--name` in the selected
graph's shared ContainerCluster/StorageCluster name namespace, never searching
outside it. The node's `machineRef` then opens the session exactly as
[`machine rsh` and `machine exec`](#machine-ssh-sessions) do: the identity,
credential and host-key proof come from that Machine's desired state, the
argument vector reaches the client verbatim, and the client's exit status is the
result. `cluster rsh` accepts no command tail.

`--node` resolves only within the cluster's declared node roster,
`StorageCluster.spec.ceph.topology.nodes`. Canonical node order is ascending
bytewise order of node `name`, independent of declaration or map order.
Omission selects the first node in that order. An explicitly empty value is a
usage error under the scalar input rules. A supplied value selects the node
whose `name` it equals exactly, and failing that the node whose effective FQDN,
including an authored override, it equals exactly. There is no further tier: a
role is not a selector, because topology nodes carry no role. A node name wins
over a FQDN that spells it. Node selection changes no desired state, canonical
serialization or lifecycle scope.

The checks run in this order, each before the next, and every refusal exits
`255` with empty standard output, no connection attempt and no trust record:

| Condition | Diagnostic | Next action |
| --- | --- | --- |
| No selected ContainerCluster or StorageCluster has the name, including one only an excluded file declares | `access.target`, naming the context and the name | name a cluster the context selects; `bootwright cluster info --context <context>` lists them |
| The name is an external Ceph StorageCluster, which declares no node roster | `cluster.not-applicable`, naming the command, the cluster, its kind and the applicable target, a managed Ceph StorageCluster | `bootwright cluster info --context <context>` lists the clusters the context selects and their kinds |
| The name is an OpenShift or OKD ContainerCluster | `access.unavailable`, naming the cluster and saying sessions are not available for its kind | none; the cluster is reached through `cluster kubeconfig` |
| `--node` matches no node, or its FQDN match is shared by several nodes | `access.target`, naming the requested selector and the cluster | choose one declared node name; `bootwright cluster info --context <context> --name <cluster>` lists the nodes |

The Machine behind the selected node then resolves as a `machine rsh` or
`machine exec` target does, and refuses as that section states. Every refusal
before the session opens reads no credential bytes, and the payload tail of
`cluster exec` never selects the node or retargets the session. Help describes
a session, not a descriptor.

Sessions on OpenShift and OKD ContainerClusters are
[deferred](deferred/cli-access-and-rendering.md#containercluster-access-handoffs-and-sessions)
to the parked item [B101](milestones/backlog.md#b101).

#### Several clusters and nodes

`cluster exec` takes a comma-separated `--name` and a comma-separated `--node`
under the [list rules](#parsing-and-input-conventions); `cluster rsh` names one
cluster and at most one node. One cluster and one node is the session above,
unchanged. Otherwise each listed cluster runs the command on the Machine behind
its first node in canonical order, or, with `--node`, on the Machine behind
every `--node` member it declares, each member matched in each listed cluster by
exact node `name` and then by exact effective FQDN as above. A node name two
listed clusters both declare selects a node in each.

Every resolved node's `machineRef` joins one list, deduplicated, and that list
is run as [`machine exec`](#machine-ssh-sessions) runs it. A list of one
Machine, which two nodes or clusters sharing that Machine also yield, is the
single session above. Several run as `machine exec` runs several Machines: the
sessions are concurrent, take no standard input and never prompt, and the
prefix of each output line is the Machine name, not the cluster or the node.
Two nodes that share a Machine run it once. The invocation's status is the
highest status any session returned, and a refusal before a session opens exits
`255`.

Clusters resolve in canonical order, ascending bytewise name, and each check
below applies across every listed cluster before the next one runs:

| Condition | Diagnostic | Next action |
| --- | --- | --- |
| Listed names no selected ContainerCluster or StorageCluster has | one `access.target` naming the context and every such name | as above |
| A listed name is an external Ceph StorageCluster | `cluster.not-applicable` for the first such cluster | as above |
| A listed name is an OpenShift or OKD ContainerCluster | `access.unavailable` for the first such cluster | none |
| A `--node` member matches no node of any listed cluster | one `access.target` naming every such member; with one listed cluster, worded as above | choose declared node names; `bootwright cluster info --context <context>` lists the nodes, and with one listed cluster the remedy above |
| A listed cluster declares none of the `--node` members | one `access.target` naming every such cluster and the members | name only nodes each listed cluster declares, or run the command per cluster |

A FQDN shared by several nodes of one cluster refuses as above, for the first
cluster and member in canonical order, after the cluster checks and before the
two node rows. Without `--node`, a listed cluster that declares no node refuses
`access.target` as above. Each Machine then resolves, and refuses, as the
[`machine exec`](#machine-ssh-sessions) section states for one or for several,
so among several Machines one whose identity is a password refuses the whole
invocation.

### Storage dashboard access

A managed Ceph cluster's apply enables the Ceph dashboard and leaves its
administrator password in the context's
[custody](storage.md#secret-custody) as the produced output `dashboard` of the
cluster's block. `cluster info` reports the dashboard's access information in
the [managed Ceph entry](cli/output.md#cluster-discovery) without any secret:
its status, user, URLs and certificate SHA-256 fingerprint.

`cluster info --name` takes a comma-separated list under the
[list rules](#parsing-and-input-conventions) and selects exactly the listed
clusters, in canonical order; unknown members fail `access.target` in one
diagnostic naming each of them. `--secrets` reveals one cluster's values, so it
requires `--name` to name exactly one cluster after normalization: a list of
several is `cli.usage` with exit `2`, before any evidence is read.

`cluster info --secrets --name <cluster>` additionally reveals the password as
`passwordValue` in that entry's `dashboard`, on standard output only. Its checks
run in this order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| No selected ContainerCluster or StorageCluster has the name | `access.target`, naming the context and the name | `bootwright cluster info --context <context>` lists the clusters the context selects |
| The name is an external Ceph StorageCluster | `cluster.not-applicable`, naming the cluster, its kind and the applicable target, a managed Ceph StorageCluster | as above |
| The name is a ContainerCluster | `cluster.not-applicable`, naming `bootwright cluster kubeconfig` | as above |
| The context's custody holds no dashboard password for the cluster | `access.unavailable`, naming the cluster | `bootwright apply --context <context> --stage clusters` |

The target and its applicability are settled from the compiled graph before
any custody read, so a refusal of the first three kinds reads no credential
bytes. A copy that custody holds from an apply that never proved the dashboard
reachable is revealed with exactly one `access.unproved` warning naming the
cluster, and its `dashboard.status` is `unproved`. The value occurs only in
the `passwordValue` field, and is never copied to a diagnostic, log, evidence
record, plan or second stream.

The same explicit command also reveals the selected cluster's declared
StorageObjectUsers in `objectUsers`, sorted by name. Each entry contains `name`,
`gateway`, `accessKey` and `secretKey`; ordinary inspection omits this member
and reads no object-user custody. The reader checks the original produced
block/output, completed original cluster/gateway/realm/zone scope, exact
canonical credential shape and matching user ID before
export. A missing, corrupt or unproved copy refuses without any partial output,
and all borrowed material is cleared on failure and after rendering. Credentials
of a different cluster or gateway are never read. These fields occur only in
the explicitly sensitive standard output; no native credential retrieval runs.

For a private S3 client handoff, redirect the result into a protected local
file and transfer only the selected user's entry over an operator-verified
channel. Keep this file outside logs and acceptance captures:

```sh
umask 077
private_access=$(mktemp)
bootwright cluster info --name ceph-lab --secrets --output json > "$private_access"
```

The S3 client uses the declared gateway endpoint and its verified TLS trust.
Use a protected client credential file; never put key values in arguments.

### Client keyring export

`cluster keyring --name <cluster> --client <client>` reveals the Ceph keyring
of one StorageClient that an apply left in the context's
[custody](secrets.md#produced-material) as the produced output `client` of the
client's block. It resolves the explicit `--context`, or the current context,
once; compiles that context's effective state; and resolves `--name` in the
selected graph's shared ContainerCluster/StorageCluster name namespace and
`--client` among the selected StorageClients. It reveals one client of one
cluster, never searches outside the selected graph and never selects another
cluster or client. The checks run in this order, each before the next:

| Condition | Diagnostic, exit `1` | Next action |
| --- | --- | --- |
| `--name` is empty | `access.target`, saying the command requires one cluster name | `bootwright cluster info --context <context>` lists the clusters the context selects |
| `--client` is empty | `access.target`, saying the command requires one StorageClient name | `bootwright cluster info --context <context> --name <cluster>` lists the StorageClients of the cluster |
| No selected ContainerCluster or StorageCluster has the name | `access.target`, naming the context and the name | name a managed Ceph StorageCluster the context selects; `bootwright cluster info --context <context>` lists them |
| The name is a ContainerCluster | `cluster.not-applicable`, naming `bootwright cluster keyring`, the cluster, its kind and the applicable targets, managed Ceph StorageClusters | `bootwright cluster info --context <context>` lists the clusters the context selects and their kinds |
| The name is an external Ceph StorageCluster | `cluster.not-applicable`, as above | as above |
| No selected StorageClient has the client name | `access.target`, naming the context and the client | `bootwright cluster info --context <context> --name <cluster>` lists the StorageClients of the cluster |
| The StorageClient's `spec.clusterRef` names another cluster | `access.target`, naming the client, the cluster it belongs to and the named cluster | as above |
| The context's custody holds no keyring for the client | `access.unavailable`, naming the client and the cluster | `bootwright apply --context <context> --stage clusters` |
| The custodied entry is not exactly one credential of `client.<client>` | `secret.store.corrupt`, naming the client | read it natively with the administrator command `cluster info --context <context> --name <cluster>` prints, followed by `auth get client.<client>` |

The target, its applicability and the client's cluster are settled from the
compiled graph before any custody read, so a refusal of the first seven kinds
reads no credential bytes. A store this build cannot open refuses with its own
[`secret.store.*` diagnostic](secrets.md#local-keyring-v4). In default text
mode, successful standard output is exactly the native keyring form
`[client.<client>]`, LF, a tab, `key = <key>` and one final LF, as
`ceph auth get-or-create` prints it without its caps lines, and nothing else.
Standard error is empty, or, for a copy custody holds unproved, holds exactly
one `access.unproved` warning naming the client and the cluster and saying its
access was not proved; on any failure, standard output is empty and standard
error holds exactly one diagnostic. Explicit JSON or YAML output wraps the same
bytes in the [structured export envelope](cli/output.md#streams-and-exit-status)
with media type `text/plain`, places any unproved-access warning in envelope
diagnostics, and leaves standard error empty. The entry exists from the apply
that creates the client
([produced material custody](state-reconciliation.md#produced-material-custody))
until the destroy that completes the context's removal withdraws it, and
`context delete --purge --allow-orphans` removes it with the keyring.

### Host-key trust

`machine trust` records in the
[context trust store](contexts.md#storage-locking-and-publication) the host key
each selected Machine that uses context trust presents: one reached over SSH
that declares no `access.ssh.knownHostsRef` and that Bootwright did not install.
It observes each such endpoint exactly once, offering no credential, and
reports one action for every selected Machine: `add` for a key the context
does not trust yet, `reuse` for the key it already trusts at that endpoint,
`replace` for a supersede, and `skip`, with its reason, for every other
Machine, which it never contacts. An `add` or `replace` whose endpoint is held
by a record of a Machine the context no longer declares, or of a declared
Machine that no longer uses context trust (reached locally, installed by
Bootwright, declaring no SSH access or declaring a `knownHostsRef`), removes
that record in the same write, reported after the selected Machines as a
`remove` row naming the Machine, the endpoint, the key removed, why nothing
reads the record any more and which Machine now uses the address; it counts as
pending, but not as a Machine checked. The confirmed (or `--yes`) write records
it in every lifecycle state, with no input edit (D117). One endpoint that cannot be
read fails the whole enrollment, which records nothing.

A changed key fails `trust.identity` naming both fingerprints. An unchanged key
observed at another endpoint fails `trust.identity` naming the endpoint the
context trusts it at, since the store trusts a key at one address. Either is
superseded only by `--replace` naming that Machine. A plan that would still pin
one endpoint to two keys, because a Machine that still uses this context's
trust holds it with another, fails `trust.identity` before it is shown, in a
dry run too, naming both Machines, the endpoint and the re-trust of the other
Machine with `--replace`, after its new fingerprint is verified out of band.

A stored trust file that already pins one endpoint to two keys, which only a
hand edit produces, is refused by every reader, an SSH session included, with
`trust.identity` naming both Machines, the endpoint and the repair: `machine
trust` with `--machines` naming each of the two Machines that still uses
context trust, or plain `machine trust` when neither does. That repair never
names `--replace`: a named Machine whose observed key differs from its record
refuses it as a changed key, writing nothing, until its new fingerprint is
verified out of band and named in `--replace`. Only
`machine trust` reads such a file: at each endpoint pinned to two keys, its
plan removes the record of every Machine the context no longer declares or
that no longer uses context trust, as a `remove` row naming why and that the
store pinned the endpoint to divergent keys, and the plan must leave every
endpoint pinned to one key or it refuses as above and writes nothing.

With pending writes and no `--yes`, the plan is written to standard output
before the one [ordinary confirmation](#ordinary-confirmation): every Machine's
action, key and fingerprint, what a replacement supersedes, the earlier
fingerprint and, when it moved, the earlier endpoint, and every record a
`remove` row drops. The plan is the same observation the command records.
After recording, the result repeats only its headline; a declined,
non-interactive or canceled confirmation records nothing. A plan that cannot
be written to standard output asks nothing and records nothing, and fails
`trust.identity` naming the context and the repeat of `machine trust` with
`--context`.
Under `--yes` or `--dry-run`, and in either structured format, which
[requires one of them](cli/commands.md#flag-relationships-and-safeguards),
nothing precedes the result, which carries every Machine's row once.

### Machine power operations

`machine start`, `machine stop` and `machine restart` converge one exact
Machine to a power state. They are bounded operations, not desired state: they
register no lifecycle operation, freeze no plan, and change no ownership,
continuation, or frozen plan, so a machine an operator stopped is still a
machine this context owns and will reconcile.

`--name` is a comma-separated list under the
[list rules](#parsing-and-input-conventions). One member after normalization
is the single-Machine operation described below, unchanged. Two or more
members run that same operation for every Machine concurrently as one batch,
under the rules that follow; the sections below state the single-Machine
operation, and each of the batch's runs is exactly it. Canonical order is
ascending bytewise Machine name, and it orders refusals, progress positions,
the confirmation's object list, the result rows, the logs and the diagnostics.

Every member is resolved first, before anything is confirmed, lent or run.
Unknown or excluded members fail `access.target` in one diagnostic naming each
of them, and any other refusal of resolution is reported for the first
refusing Machine in canonical order; no Machine then runs. Stopping and
restarting ask the ordinary confirmation once for the whole batch, after every
resolution and pin read, naming every Machine in canonical order, and a
declined or unanswerable one refuses `machine.power` and runs nothing; the
command that repeats it with `--yes` names the whole list.

Each run keeps its own bounded runtime under the context's shared lock, its
own [retained output](cli/output.md#bounded-run-output), its own pin check and
its own request naming that one Machine. The runs execute concurrently, at most
as the lifecycle engine's [bound on running blocks](state-reconciliation.md#stages-and-the-pause-boundary)
allows, started in canonical order, inside one invocation scope so that the
runs of one batch are each other's live siblings
([security](security.md#process-boundary)); a run past the bound waits for a
slot. One Machine's failure never cancels another run, and an interrupt or
cancellation of the invocation cancels all of them. Each run is one progress
step at its canonical position of the batch total. The batch reports no single
log location: each Machine's result row names its own.

Every Machine whose run was admitted has a result row, settled or not. The
invocation exits `0` only when every Machine settled; otherwise it exits `1`
with the diagnostics of every Machine that failed, a run that ended on its own
deadline included, each naming its `Machine/<name>`, and still presents the
rows.

Not yet met: a batch of more than the 16 runs a context's runs area keeps can
retire the retained output of its own earliest finished runs before its result
names them; tracked as [B545](milestones/backlog.md#b545).

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
`lifecycle.unknown` with exit `1`, never a success. Its remedy names the exact
commands: a controller that proved no usable state is read again with
`bootwright machine list --context <context> --power-status` once it answers,
and the same request repeated if the Machine is not in the state it asked for;
a Machine that did not reach that state repeats the same request, and a
graceful stop or restart also names it with `--force` for a guest that does not
answer (`TestPowerEvidenceRemediesNameTheExactCommand`). What the adapter printed is
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

# Command-line Interface

`bootwright` exposes the [desired-state API](api.md) to operators and automation.
Equivalent logical input produces the same ordered output regardless of TTY,
locale, map iteration or discovery order. Prompts, explicit sensitive exports,
watch displays, and the elapsed times, terminal redraw and interleaving of
concurrent steps' [progress rows](cli/output.md#long-running-progress) are the
named exceptions. Progress is presentation: the plan, the result rows and the
receipt that follow it are ordered by the frozen plan whatever ran together.

Read this page with the [command and flag catalog](cli/commands.md) and
[output contract](cli/output.md). Together they define the CLI. Unlisted
commands, aliases, flags, shorthands, modes and operands are usage errors.

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
attached value such as `--watch=false` or `-v=false`; a separated token is a
positional operand, not its value. Boolean values use Go's case-sensitive
`strconv.ParseBool` spellings: `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`,
`F`, `FALSE`, `false`, and `False`. The only shorthands are `-h`, `-f`, and
`-v`.

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
  omission and a comma-only value as an empty result; and
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
every safeguard it enforces. Only `y` or `yes`, case-insensitive with
surrounding whitespace ignored, accepts. Read one answer of at most 64 bytes
including LF without read-ahead. Decline, non-interactive input, cancellation
or an I/O failure refuses, and the command performs none of the effects it
asked about. The host-key confirmation of an [SSH session](#machine-ssh-sessions)
reads its answer the same way. `--yes` suppresses only this prompt, under
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
refuse that executable path fail closed. Sudo owns
terminal password handling; Bootwright never captures the password. JSON or
noninteractive invocation uses noninteractive sudo and fails without cached or
passwordless authorization. Preserve argv, stdin payloads, outputs and status.
An interactive invocation hands sudo the invoking terminal itself on every
standard stream it occupies, so sudo's own terminal relay carries prompts and
answers to the child. A confirmation read whose terminal belongs to another
foreground process group requests the terminal through job control instead of
waiting for input that cannot arrive.

The supervisor owns bounded `sudo -n -v` refresh subprocesses during that child.
Keep the same parent and terminal identity. An unambiguous positive effective
timeout refreshes at half its duration, including fractional minutes. Zero and
negative timeouts need no expiry refresh. Unknown policy uses a 30-second
best-effort interval; never assume a five-minute timeout. Refresh failure or
timeout warns once and stops refresh without terminating the elevated command.
Completion/cancellation stops and reaps every refresh process. Never leave a
daemon or invalidate the user's wider sudo cache.

Capture the real invoking account before elevation. Validate sudo-provided
UID/GID/name against the local account database on manual sudo; direct root
uses root. Ignore spoofed sudo metadata on non-root launch. Resolve account
homes without `HOME`. Access selection files with the invoking user's actual
credentials; retain invoking-user home/ownership semantics for authored secret
file references. Root identity governs only root-owned runtime storage.

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
continuation point. With no operation it previews the fresh operation the
current state would start, listing every block in frozen order with the
description and impacts of the verb it previews. With an incomplete operation
it previews that exact continuation point instead, showing which blocks are
already done and which block resumes, and never a re-planned alternative. With
a completed apply it previews the destroy that the recorded ownership evidence
defines. It reads context state, allocates no identity, writes nothing and
creates no log.

An invocation whose work durable state already proves settles before any of
that: it presents no plan, asks for no confirmation, requires and refuses no
authorization, and reports `done` with the blocks its completed operation
finished, under the [settled-verb rule](state-reconciliation.md#lifecycle-unit).
Its result says in one line that it did nothing, so a table of completed blocks
is never read as work this invocation performed.

`apply` and `destroy` present the frozen plan, then any required
authorizations, then the ordinary confirmation. During execution they report
[progress](cli/output.md#long-running-progress) per block, with its
[presentation groups](cli/output.md#multi-machine-presentation) as sub-steps,
and they close with the ordered result, the safe log reference and the
[receipt](#lifecycle-receipt). A refusal before registration reports its
diagnostics and no receipt. An authorization token the frozen plan does not
require fails `lifecycle.authorization` before registration. A root lock or
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
`plan --stage` never fails `lifecycle.stage`: it previews which blocks the
selection would start and which it would defer.

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
receipt and `status --output json` derive from the same trustworthy state.

`next` names what the operation's own state calls for, so a `done` operation of
either verb reports `none`. A settled verb reports `done` and `none` too, over
the completed operation it proved, or over `none` when the context owns no
operation at all. A completed apply admits a later destroy, but
naming it as the next action instructs an operator to undo what just succeeded;
the verbs a context admits are what `plan` reports. `apply` and `destroy` appear
only as the previewed verb of a pure plan. Where `status` offers a next step it
offers a command this executable runs: a continuation and a resolution are both
offered as the operation's own verb repeated — `continue-apply` and `resolve`
as `bootwright apply` — and `none` is offered as no step at all. No next step
names a verb this executable does not expose.

`status` offers `bootwright destroy` beside the continuation of an apply that
has not completed, whichever state it stopped in, because that apply owns every
block it started and taking them back is as legitimate a way forward as
continuing. A completed apply is the one exception, for the reason above. This
is a next step only; the receipt's `next` still names what the operation's own
state calls for.

## Resource inspection and explicit access

List and info commands derive their result from validated desired state,
context-owned artifacts, and durable ownership evidence. A name can locate an
entry but never proves identity or ownership. The detailed contract of cluster
inspection, access handoff and node selection is
[deferred](deferred/cli-access-and-rendering.md) to a
[C6](milestones/backlog.md#candidates) slice.

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
reports both. The contact is the one address its SSH access resolves, which is
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
nothing for an operator to resume or read afterwards and no log location is
reported for it.

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
fallback; a leading `~` resolves from the invoking account database, and the
file must satisfy the private-file rules in [security](security.md). Neither
flag alters desired state or the managed identity Bootwright installs, and
neither `--ssh-user-for-provisioned` nor `--ssh-ask-sudo-password` is consumed.

The host key is proved before any credential is offered, from exactly one
source in this order: the Machine's `access.ssh.knownHostsRef` `Secret`; for a
Bootwright-installed Machine, the host key its
[installation evidence](substrates.md#identity-and-power-operations) proves,
which requires that this context currently owns that installation; the
[context trust store](contexts.md#storage-locking-and-publication); and
finally, on an interactive terminal alone, one observation of the endpoint
confirmed against its displayed fingerprint and recorded as context trust.
Without a terminal an unproved key fails `trust.identity` and names
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
access fails `access.unavailable`. Each is exit `1` with empty standard output,
no connection attempt and no trust record.

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
is reachable only while the context owns its realization, because the
controller is one of that realization's own effects; a Machine that authors its
own controller is reachable whenever that controller answers.

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

[`machine list --power-status`](#resource-inspection-and-explicit-access)
reads the same controllers over the same boundary and under the same lock, and
follows the same reachability rules, but drives nothing: it asks each
controller what state it is in and reports the answer. Because it observes
rather than converges, an unreachable controller leaves its own Machine
`unknown` instead of refusing, and a Machine no controller resolves for is
omitted from the reading instead of failing `access.unavailable`.

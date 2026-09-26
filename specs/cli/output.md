# CLI Output

Read with [CLI behavior](../cli.md) and the [command catalog](commands.md).

## Streams and exit status

Every invocation has one primary mode. Diagnostics never masquerade as primary
output.

| Condition | Standard output | Standard error | Exit status |
| --- | --- | --- | --- |
| Human structured success | result or help, LF-terminated | ordered warnings only | `0` |
| Human operational or validation failure | empty, except a complete negative secret check, controller readiness report, or already presented setup plan/progress or lifecycle state | ordered diagnostics, each LF-terminated | `1` |
| Human usage failure | empty | usage diagnostic and concise help, LF-terminated | `2` |
| JSON success or failure | exactly one JSON document followed by one LF | empty | the document's required `exitCode` |
| Effective-state text | exact canonical effective YAML with its required final LF | empty | `0` |
| Human effectful outcome | ordered plan, progress, status and result | ordered warnings and diagnostics | `0`, `1`, or `130` |
| Explicit sensitive result | exact requested bytes, with no added LF | diagnostics only on failure | `0` or `1` |
| Completion script | exact script with its required final LF | empty | `0` |
| Access handoff | one bounded, escaped descriptor followed by one LF | ordered diagnostics only | `0` or `1` |
| [SSH session](../cli.md#machine-ssh-sessions) | the remote process's own bytes | Bootwright diagnostics and the host-key confirmation before the connection, then the remote process's own bytes | the SSH client's exit status |
| Interrupt-driven cancellation | as required by the selected structured mode | as required by that mode | `130` |

Not yet met: `machine start`, `machine stop` and `machine restart` with `--output json` write the human `Logs` field before the document; tracked as [backlog F5](../milestones/backlog.md#audit-follow-ups-2026-09).

An operating-system interrupt reports `runtime.interrupted` and exits `130` for
a Bootwright-owned operation. Another canceled context or expired deadline
reports `runtime.canceled` or `runtime.deadline` and exits `1`.

A write or flush failure on standard output exits `1` with best-effort cleanup
and never recursively emits a second representation or fallback on standard
error. If any sensitive bytes were written, it emits no diagnostic that could
be mistaken for part of that value. Structured text, JSON, help, and completion
are UTF-8 with LF line endings; explicit sensitive results remain exact byte
streams. Terminal detection may select the in-place redraw of a running
[progress row](#long-running-progress) or an interactive prompt only where
defined; it never changes the bytes of a non-interactive structured
result. Color, cursor control, locale, and attacker-chosen styling are absent
from deterministic output.

## Human output

Human status lines use these exact tokens:

- `[OK]` for successful work;
- `[WARN]` for a non-fatal diagnostic;
- `[PENDING]` for a frozen lifecycle block that has not started;
- `[RUNNING]` for progress with no terminal outcome;
- `[SKIPPED]` when no work was required or permitted;
- `[FAIL]` for a definite failure;
- `[CANCELED]` when stopping is positively confirmed;
- `[UNKNOWN]` when an outcome or side effect cannot be proved; and
- `[DONE]` for a lifecycle block whose effect and required evidence are durable.

Summary, field, table, artifact, plan, and status groups use domain-owned order.
Warnings never change exit status. Lifecycle progress counts durable `DONE`
blocks against the frozen total and continues to display completed blocks so the
resume boundary is explicit. Human wording may evolve, but status meaning,
group membership, order, safe target identity, and next action remain stable.
An [SSH session](../cli.md#machine-ssh-sessions) presents no status token of its
own: what its streams carry is the remote process's.

### Shared human layout

Every command composes its result from the same blocks, so an unfamiliar
command is readable from a familiar one. One shared renderer owns this layout;
a command supplies content and never its own spacing, padding, or separators.

- An optional **headline** opens the result: either a status line
  (`[OK] Context initialized`) or a plain title (`Controller setup plan`). It is
  the only line at column zero besides section headings and table headers.
- A **section** is an optional capitalized heading followed by its body. One
  blank line separates a section from the block before it. A section with no
  heading groups trailing fields, such as the closing outcome and next action.
- **Fields** are `Label` and value pairs indented by two spaces, with labels
  padded so every value in one group starts at the same column. Labels are
  capitalized prose, not identifiers.
- **Rows** align every column to its widest cell, separated by two spaces, and
  are indented by two spaces. Status rows lead with a status token.
- A **table** adds an uppercase column header above unindented rows. The header
  participates in column width.
- **Steps** number an ordered list of planned changes from `1.`. A step may
  carry the effects it causes as unnumbered lines indented one unit past its
  own text, so an effect is always read against the change that performs it.

Trailing padding is never written, so no line ends in whitespace. Column width
is measured in Unicode code points after display escaping. Values cross the
display boundary inside the renderer, so alignment can never be widened by an
unescaped control sequence. Layout adds no color, cursor control, or box
drawing, and does not vary with terminal width; only the redrawn
[progress row](#long-running-progress) is bounded by it.

### Long-running progress

A command whose authorized work can take minutes reports progress as it runs,
because a silent process is indistinguishable from a stuck one. Progress is a
stream of status rows in the shared layout, written unbuffered to standard
output. The appended form below is identical through a pipe, a file and the
privilege supervisor's non-interactive relay; a terminal redraws the running
row in place as described at the end of this section; JSON mode never emits
progress.

Every row has this form. The status token is padded to the width of
`[RUNNING]` so the subject column aligns without knowing which tokens follow;
the step's position in its known total opens the subject, padded to the total's
width so every label starts at the same column; and a trailing `still running`
note or elapsed time is separated by the column gap:

```text
  [RUNNING]  [<position>/<total>] <step>[: <sub-step>[ - <completion>%]]
  [RUNNING]  [<position>/<total>] <step>[: <sub-step>[ - <completion>%]]  still running, <elapsed>
  [<STATUS>] [<position>/<total>] <step>[: <summary>][  <elapsed>]
```

Four rules govern the stream:

1. **Coverage.** Any unit of work that can exceed the heartbeat interval opens
   a `[RUNNING]` row before it starts, including work before confirmation.
   `setup` and `preflight controller` stream the headings
   [their results](../controller.md#results-and-qualification) define; every
   command reports confirmed effects under `Progress`. The first row opens its
   heading, and a section the stream already showed is not repeated by the
   result that follows.

   `Progress` holds effects alone. A proof a command performs before it may
   have any effect, such as
   [quiescence before a removal](../state-reconciliation.md#quiescence-before-removal),
   settles under `Checks` first, so the rows under `Progress` are one per step
   of the work that was authorized and every one of them follows the `Logs`
   field naming where to read it. A check reports under rules 2 to 4 like any
   other step: what it probes is its sub-step, so the whole of one check
   settles as one row however many targets it has to observe.
2. **Silence bound.** While a step runs and ten seconds pass without a new
   row of its own, the presenter repeats that step's row with `still running`
   and the time since the step started. The bound is per step, so a step that
   has gone quiet is repeated whatever the steps beside it are doing. The CLI
   presenter owns the timer; domain and adapter code emit events and never read
   a clock to present them.
3. **One line per step.** Steps run at the same time when the plan admits it,
   and each holds one line of its own from its first row until its outcome:
   no step ever overwrites another's row, and rows of different steps
   interleave in the order the steps report them. A step may report sub-steps
   as its detail: the source
   being acquired, the native transaction, the target tool, or the
   presentation group in flight. A sub-step never settles as a row of its own,
   because a screen of settled sub-steps buries the step that is actually
   running. Its outcome advances the step's completion instead, which the
   step's next row reports as `<completion>%` of the sub-steps the step knows
   it must take, counted in proved sub-steps rather than in elapsed time and
   omitted by a step whose remaining work has no such count. There is no third
   level.
4. **Outcome with duration.** Every step closes with the outcome it proved and,
   when it took at least one second, the elapsed time truncated to whole
   seconds in Go duration form, such as `1m48s`. The outcome row drops the
   sub-step it last reported, because a settled step is no longer anywhere, and
   keeps a summary the step proved about itself, such as a resolved version.

Progress is presentation only. A failure to report never changes an effect, an
outcome, or a recorded receipt, and progress rows never replace the ordered
result that follows them. Reporting stops at cancellation rather than claiming a
step whose outcome is no longer observable. Heartbeat notes and elapsed times
are the only timing-dependent bytes in human output; contract tests drive the
presenter through an injected clock.

When standard output is a terminal, the presenter rewrites the running steps in
place instead of appending. The steps now running occupy a block of consecutive
lines, one each, in the order they started. A carriage return and an erase-line
sequence precede each replacement; reaching the first line of a block of more
than one moves the cursor up by the lines already drawn, which is the only
cursor movement used. The elapsed time and completion refresh every second. A
step's outcome is written above the block and its line is given back, so the
block always holds exactly the steps that are still running and a settled step
scrolls away as ordinary text.

A heading owns its block. The steps under a heading are redrawn together, and a
new heading, a field such as the `Logs` reference, the result or a diagnostic
first terminates the block: its rows stay on screen as text, and nothing
redraws a line it no longer owns. A step still running when its block is
terminated keeps the last row it wrote, because that row is what it proved.

A redrawn row occupies one physical line: it is bounded by the width the
terminal reports when it is drawn, measured again for every row so a resize
takes effect, and the classic eighty columns stand in for a terminal that
reports no size. This is what makes a block's height exactly one line per step,
so the cursor moves up by as many lines as were drawn. A row that does not fit
keeps its status token and its
trailing note, because the elapsed time is what an operator watches, and marks
the text elided from the end of its subject with `...`. The appended form above
is never elided; it is what a pipe, a file and the privilege supervisor's
non-interactive relay receive. No color is used, and no cursor control beyond
the carriage return, the erase-line sequence and the upward move named above.

### Context identity

Context identity is presented by the commands that own it. `context init`,
`context update`, `context use`, `context current`, `context list`, and
`context delete` present the selected context's name, and its mode where
[their results](../contexts.md#command-results-and-confirmation) show one.
Every other human result omits the context block; the selected context is
already addressable through `context current`. JSON results keep their
documented `context` field unchanged, so machine consumers lose nothing.

Human diagnostics have one of these forms:

```text
[<STATUS>] <code> <source>:<line>:<column>: <message>[ <object>][ <field>][; next: <safe action>]
[<STATUS>] <code>: <message>[ <object>][ <field>][; next: <safe action>]
```

Optional source coordinates are included only when known. An object is rendered
as `[<kind>/<name>]` and a field as `(<field>)`. Text is single-line,
deterministically escaped, and contains neither a secret nor an untrusted
terminal sequence. When remediation is available, the labeled `next:` clause
states the shortest exact safe action. A refusal names the object or block,
observed or missing evidence, safety reason, and exact safe next action. It
never invents a force command.

## JSON output

Every command that lists `--output json` returns exactly one compact object with
these fields in this order:

```json
{"schemaVersion":"v1alpha1","command":"validate","ok":true,"exitCode":0,"result":{},"diagnostics":[],"logs":[]}
```

| Field | Contract |
| --- | --- |
| `schemaVersion` | Exact string `v1alpha1`; changing it is a public output-version change. |
| `command` | Canonical resolved command path without the executable. |
| `ok` | `true` if and only if `exitCode` is `0`. |
| `exitCode` | Exact Bootwright process exit status: `0`, `1`, `2`, or `130`. |
| `result` | Command-specific object, or JSON `null` when no trustworthy result exists. |
| `diagnostics` | Ordered diagnostic objects; empty when none. |
| `logs` | Ordered safe paths relative to the state root for private logs actually created; empty when none. |

Successful result objects have stable top-level fields:

| Command | Result fields |
| --- | --- |
| `add-ons list` | `addOns` |
| `secret check`, `secret list` | `context`, `secrets` |
| `secret encryption status` | `initialized`, `implementation`, `activeKey`, `keys`, `items` |
| `media list` | `media` |
| `validate` | `counts`, `excludedContainerClusters`, `excludedStorageClusters`, `excludedResourceFiles`, `advisories` |
| `preflight infra`, `preflight clusters`, `preflight container-cluster`, `preflight storage-cluster`, `preflight add-ons`, `preflight all` | `context`, `target`, `checks`, `summary` |
| `status` | `context`, `setupChecks`, `desired`, `clusters`, `storageClusters`, `shared`, `secrets`, `nextSteps`, `lifecycle` |
| `render` | `inputDir`, `outputDir`, `effectiveStatePath`, `lockPath`, `inventoryPath`, `varsPath`, `installer`, `storage` |
| `render effective` | `counts`, `effectiveState` |
| `render installer` | `clusters` |
| `render storage` | `clusters` |
| `machine list` | `context`, `machines`, `powerRead` |
| `machine trust` | `context`, `dryRun`, `hosts` |
| `machine start`, `machine stop`, `machine restart` | `context`, `machine`, `verb`, `power`, `previous`, `changed` |
| `cluster list` | `context`, `clusters` |
| `cluster info` | `context`, `clusters`, `storage` |

The `validate` result orders its fields as listed above and has this exact
shape:

```json
{"counts":{"filesSeen":2,"objectsDecoded":4},"excludedContainerClusters":[],"excludedStorageClusters":[],"excludedResourceFiles":[],"advisories":[]}
```

`counts` contains only `filesSeen` and `objectsDecoded`, in that order, with
[the API loader's counting semantics](../api.md#yaml-streams-and-decoding).
The cluster-exclusion arrays contain unique names in ascending bytewise order.
`excludedResourceFiles` contains unique, sorted paths of resource-excluded files
that declare Bootwright objects. These slash-separated display paths are
relative to the selected Environment file's directory; an informational `..`
segment grants no filesystem or resource-selection authority. A file outside
that directory receives remediation to place its declaration within the
directory before adding it to `resources`, never an invalid traversal selector.
Exclusion diagnostics carry the object identities and safe remediation.

`advisories` contains the ordered `api.deferred` warning subset using the
diagnostic shape below. Those warnings also remain in envelope `diagnostics`;
human output presents each diagnostic only once. All four arrays are always
present, including when empty. Every failed `validate` invocation returns
`result: null`; partial loader counts remain internal.

Each `secret check` entry carries both `version`, the durable identifier or
`null`, and `sequence`, its per-secret ordinal or `0` when no version applies.
Each `secret list` entry carries `currentVersion` and `currentSequence` on the
same terms. Human output shows only `v<sequence>`, and `-` when none applies;
the identifier is reserved for machine consumers, which must reference it rather
than the ordinal.

`render effective` returns admission `counts` and `effectiveState`, the canonical
ordered array of complete effective objects. Successful `diagnostics` and
`logs` are empty. Text mode emits the exact canonical YAML bytes, including
the final LF, with empty stderr. [Context command results](../contexts.md#command-results-and-confirmation)
remain text-only.

A successful human `validate` writes exactly one summary line to standard
output, substituting the decimal counts:

```text
[OK] Desired state is valid (files seen: <N>, objects decoded: <M>)
```

Ordered exclusion and advisory warnings use the ordinary diagnostic lines on
standard error. Warnings do not change success or add duplicate summary groups.

An unavailable optional collection is an empty array, not `null`; an unavailable
optional scalar or object is omitted unless its command contract requires a
stable nullable field. Counts are non-negative integers. Paths are safe,
normalized paths in the command's declared path domain. Objects and arrays use
the same canonical ordering as human output. No command switches a successful
field between array, object, scalar, and `null`.

JSON uses no insignificant whitespace, does not HTML-escape strings, and ends
with one LF. A JSON-mode failure never emits a partial success document followed
by an error document. Help is always human text and does not establish JSON
mode. The successful bare-`render` special case likewise emits human render
help even when `--output json` was validly selected.

A diagnostic object orders its fields as follows:

```json
{"severity":"error","code":"api.required","message":"metadata.name is required","source":{"path":"network.yaml","document":1,"line":4,"column":3},"object":{"apiVersion":"bootwright.io/v1alpha1","kind":"NetworkConfig","name":"management"},"field":"$.metadata.name","remediation":"set metadata.name to a unique DNS label"}
```

`severity`, `code`, and `message` are required. `source`, `object`, `field`, and
`remediation` are omitted when unavailable and are never `null`. A present
source orders `path`, `document`, `line`, and `column`; positive coordinates are
one-based and `0` means unavailable. Sensitive explicit-result bytes never
appear in this structure.

Safe display text preserves printable UTF-8 and escapes backslash, control
characters, ESC, newline, and invalid UTF-8 bytes with deterministic `\\`,
`\n`, `\r`, `\t`, `\uNNNN`, or `\xNN` sequences before JSON encoding.

## Cluster discovery

The `cluster list` and `cluster info` result shape, `accessCommands` included,
is [deferred](../deferred/cli-access-and-rendering.md#cluster-discovery) to a
[C6](../milestones/backlog.md#candidates) slice.

## Lifecycle and status results

The human result of `plan`, `apply` and `destroy` composes the
[shared layout](#shared-human-layout): an optional headline, a `Plan` section
whose steps are the frozen blocks in plan order, each naming its stage and
carrying its own impacts as indented lines, a `Checks` section for what the
operation proves before it registers, a `Progress` section while effects run, a
`Result` section of status rows, the `Logs` reference when an operation log
exists, and the receipt as the final four lines.

Because the plan is frozen
[wave by wave](../state-reconciliation.md#plan-and-execution), the numbered
steps are the order the work is started in. Each step that waits for another
names the steps it waits for by their place in that list, as `[after 2, 5]`,
and a step that waits for nothing carries no such marker, which is what marks
it as one of the first to start. A closing `Concurrency` field reports how many
waves the plan needs and how many of its steps share the fullest one, so a long
plan that is one chain reads differently from a long plan that is wide. With a
stage selection,
each pending step also says whether this invocation would start it, that it is
not selected, or which block it waits on, and a closing field reports how many
blocks would start and how many are deferred. A block row leads with its status
token and names the block description and its outcome; its
[presentation groups](#multi-machine-presentation) are never result rows. A
preview and a refusal have no progress, result rows or log reference.

`status` is the machine-readable view of the same durable state and performs no
probe. Its result orders these fields:

| Field | Contract |
| --- | --- |
| `context` | `name`, `mode` of the resolved context. |
| `setupChecks` | Ordered `{id, status}` rows derived from stored controller evidence alone, without host probes. `status` uses the check vocabulary of [controller readiness](../controller.md#results-and-qualification). |
| `desired` | `revision`, `environment` and admission `counts` of the selected immutable input, or nulls when no revision is imported. |
| `clusters`, `storageClusters` | Ordered `{name, kind, status}` rows for selected cluster roots. `status` is `unsupported` when this executable has no lifecycle capability for that cluster. |
| `shared` | Ordered `{kind, name, machine, status}` rows for selected shared services. `status` is `unsupported`, `pending`, `done` or `unknown`, derived from the frozen plan and its durable evidence. |
| `secrets` | `declared` and `bound` counts. |
| `nextSteps` | Ordered safe command strings, empty when no action is available. |
| `lifecycle` | `null` when no operation exists, else `operation`, `verb`, `state`, `next`, ordered `blocks` of `{id, description, stage, state, attempts}`, and `logs` of safe relative paths. |

Not yet met: nested status objects carry Go field names and another shape, and setup checks use their own vocabulary; tracked as [backlog F5](../milestones/backlog.md#audit-follow-ups-2026-09).

Rows sort by their documented key: checks and blocks in frozen order,
everything else in ascending bytewise name order. Human `status` presents the
same membership and order, omitting empty sections.

## Diagnostic taxonomy and order

Diagnostic codes are stable machine identifiers. Include an object identity
only when its kind and name form a valid API identity; malformed names are
reported through the source coordinates and `$.metadata.name` field without
repeating unbounded authored text in every diagnostic. This table is the
registry: it lists every code production code emits, from a function body or a
package-level variable initializer, and nothing else
(`TestDiagnosticCodesMatchOutputSpec`).

| Code | Meaning |
| --- | --- |
| `input.not-found` | A required input path does not exist. |
| `input.not-directory` | A required directory input is not a directory. |
| `input.symlink` | An input or managed path violates the link policy. |
| `input.read` | An input cannot be safely read. |
| `input.limit` | A documented resource limit was exceeded. |
| `yaml.syntax` | YAML is malformed or is not valid UTF-8. |
| `yaml.duplicate-key` | A mapping repeats a key. |
| `yaml.alias` | An anchor, alias, or merge key is present. |
| `yaml.tag` | A YAML node uses an unsupported or forbidden tag. |
| `yaml.shape` | A document or mapping key has the wrong YAML shape. |
| `api.version` | `apiVersion` is missing or unsupported. |
| `api.kind` | `kind` is missing or unsupported. |
| `api.field` | A field is unknown at its exact schema location. |
| `api.type` | A field has the wrong YAML scalar or collection type. |
| `api.required` | A required value other than `apiVersion` or `kind` is absent. |
| `api.value` | A scalar or collection value is invalid. |
| `api.duplicate` | An object or unique collection value is duplicated. |
| `api.reference` | A typed reference is unresolved or has the wrong target type. |
| `api.invariant` | A cross-field or cross-object rule is violated. |
| `api.selection` | Environment selection excludes an otherwise discovered authored object. |
| `api.deferred` | Valid authored state declares inactive operational semantics. |
| `cli.usage` | Arguments or flags do not match this contract. |
| `cli.not-implemented` | The command is defined but its application use case is not available. |
| `runtime.encode` | A trustworthy result could not be encoded. |
| `runtime.log` | A required private operation log could not be safely maintained. |
| `runtime.interrupted` | An operating-system interrupt canceled the command. |
| `runtime.canceled` | The supplied context was canceled without an interrupt. |
| `runtime.deadline` | The supplied context deadline expired. |
| `runtime.privilege` | Invoking identity, sudo authorization, or the elevated process boundary could not be verified. |
| `runtime.internal` | An unexpected failure escaped a typed boundary. |
| `context.configuration` | Context setup is invalid or attempts an unsupported backend change. |
| `context.input` | The context has no desired-state revision; import one with context update. |
| `context.state` | Context state does not permit the requested local transition. |
| `context.unsafe-delete` | Required ownership or recovery evidence prevents deletion. |
| `secret.store` | Confidential material cannot be safely read, generated, written, rotated, or deleted. |
| `secret.declaration` | A secret name or declaration is invalid, unresolved or duplicated. |
| `secret.source` | The declared source or its parameters do not permit the operation, or stored material no longer matches the declaration. |
| `secret.part` | A secret part is missing, not applicable, out of bounds or unusable by its consumer. |
| `secret.input` | Secret input or material is invalid, or a requested secret, version or binding has no usable material. |
| `secret.store.uninitialized` | The configured secret store is not initialized. |
| `secret.store.implementation` | The secret store implementation is unconfigured, unavailable or incompatible. |
| `secret.store.key-unavailable` | The secret store key or unlock session is unavailable. |
| `secret.store.conflict` | Secret storage changed during the operation or refuses the requested access. |
| `secret.store.corrupt` | Stored secret state is invalid, incomplete or not attributable. |
| `secret.store.crypto` | Secret encryption randomness or key material is unusable. |
| `secret.store.limit` | A secret material, input, metadata or recovery bound was exceeded. |
| `media.store` | Media identity, integrity, import, publication, or deletion failed. |
| `preflight.failed` | One or more required readiness checks definitely failed. |
| `controller.unsupported` | The setup host, dependency combination or acquisition route is not qualified. |
| `controller.identity` | Required controller binding or verified host evidence is missing or contradictory. |
| `controller.state` | Retained setup evidence is incomplete, contradictory or no longer valid. |
| `controller.conflict` | Another setup or context protects a shared prerequisite or holds its coordination boundary. |
| `controller.setup` | A local controller prerequisite action definitely failed. |
| `controller.unknown` | A local controller prerequisite action has an unresolved effect outcome. |
| `lifecycle.state` | Durable lifecycle state does not permit the requested transition or continuation. |
| `lifecycle.unsupported` | This executable cannot realize a selected object: no capability claims its kind, or its capability reports the shape unsupported. |
| `lifecycle.stage` | The stage selection admits no startable block, or excludes the block the operation must retry. |
| `lifecycle.authorization` | The frozen plan requires an authorization that was not validly supplied, or a supplied one it does not require. |
| `lifecycle.lease` | The context root lock or mutation lease cannot be safely acquired or recovered. |
| `lifecycle.unknown` | A frozen block has an unresolved unknown effect outcome. |
| `lifecycle.live` | A removal would take back state that is still in use, and refuses before registering. |
| `lifecycle.adapter-running` | An earlier lifecycle adapter still holds its job lock, so no adapter starts until it ends. |
| `trust.identity` | SSH identity is missing, changed, contradictory, or not authorized. |
| `access.unavailable` | An applicable access request lacks required local access metadata or an available credential artifact. |
| `access.target` | Explicit access cannot resolve one exact authorized target. |
| `access.handoff` | An explicit access descriptor cannot be safely resolved or encoded. |
| `machine.power` | A power transition was refused before it ran, or its confirmation could not be answered. |

A command context that needs a narrower code adds its row with the code's
first emission. A namespace alone is not a fallback code. New codes may be
added, but the meaning of an existing code cannot change.

Diagnostics are sorted by:

1. normalized source path;
2. document index;
3. line;
4. column;
5. code;
6. object `apiVersion`, `kind`, and name;
7. field path; and
8. message.

Within a sort key, unavailable values sort before available values. Diagnostics
without a source follow sourced diagnostics and use the same remaining order.
JSON and human modes use identical membership and order. Two diagnostics are
duplicates only when every present or absent member is equal.

## Private operation logs

Help, completion, `version`, every `validate`, `render effective`, `plan`, and
local list/current/status reads create no cache, temporary file, state record,
output path, or log. Other render commands create only their declared
artifacts. Preflight, local setup, sensitive export, and access handoff allocate
no lifecycle identity or log. Setup uses only its
[private recovery receipt](../controller.md#publication-and-interrupted-setup),
separate from the lifecycle receipt and operation logs. Only lifecycle effects
and unknown-effect resolution use the state-owned operation, block, and attempt
log tree below; a [bounded run](#bounded-run-output) retains what its own
adapter printed and nothing else.

Managed operations use this tree:

```text
<state-root>/contexts/<context-name>/state/operations/<operation-id>/
  logs/
    operation.jsonl
    blocks/<block-id>/
      attempt-000001.jsonl
      attempt-000001.output
      attempt-000002.jsonl
      attempt-000002-resolution-000001.jsonl
<state-root>/contexts/<context-name>/state/runs/<run-id>/
  run.output
```

[Workspace](../contexts.md#storage-locking-and-publication) owns `<state-root>` and `<context-name>`; state reconciliation
owns `<operation-id>`, `<run-id>`, `<block-id>`, and attempt numbers. Their safe
grammar, allocation, collision, and crash-gap rules are defined by
[state reconciliation](../state-reconciliation.md#durable-identities-and-private-paths).
Directories are `0700`; files are `0600`. Creation is exclusive beneath a held,
verified root handle and never follows a link or overwrites unrelated content.

`operation.jsonl` contains bounded normalized operation summaries. Each effect
or resolution attempt has one separately identified log. Raw callback bytes,
terminal escapes, environment dumps, command lines containing sensitive values,
secret values or digests, and content protected by an adapter's `no_log`
equivalent are forbidden. Truncation and dropped-event counts are explicit.

An `attempt-NNNNNN.output` beside an attempt log retains what that adapter
process printed on its own standard output and error, raw and unparsed. It
exists for every run that printed anything, not only for one that ended without
a complete result: the structured events say which groups settled, never what
the adapter did inside them, so a run that succeeded is as worth reading as one
that failed. Keeping material out of a retained run is the adapter's own
obligation, discharged where the material is used: every task that reads bound
material marks itself `no_log`, so the adapter's output never carries it. The
file is appended to while the run produces it, within a bounded delay, so a run
that wedges is readable before it ends rather than only once it stops. That
requires the adapter process to be launched so that it does not withhold its own
output, because a tool that buffers until it exits defeats the retention
whatever this side does. Publishing is coalesced rather than written a line at a
time, and the file is bounded and truncated at its limit without an inline
marker. It is troubleshooting material only: nothing reads it back, it is never
product output, ownership evidence or a continuation cursor, and its attempt log
records the file by name with the bytes it holds and whether the bound cut it
short. Neither retaining it nor failing to ever changes the outcome the
operation records.

The operation log and required attempt log exist before the corresponding
effect or resolution observation. A create, append, flush, or finalize failure
stops admission of new work, requests cancellation, and returns `runtime.log`.
Its effect-state and log-fault transitions depend on the available positive
evidence and follow
[state reconciliation](../state-reconciliation.md#plan-and-execution) exactly.
Logs are troubleshooting material, never ownership evidence or a continuation
cursor.

Human output names the operation's own log directory as a `Logs` field, giving
its path on this host rather than one relative to the state root, because the
point of naming it is that an operator can open it. It is named twice: once
before the first effect runs and so before every row under `Progress`, so the
work can be followed while it happens, and once after the terminal summary, so
it survives in the scrollback. A check that precedes registration precedes the
field as well, because the directory belongs to an operation that does not
exist until the check admits it. Both name the same directory, which is where a
run still in flight is writing; that directory and its files are private to the
identity owning the state tree, so reading them is a privileged action. JSON
`logs` keeps naming safe paths relative to the state root, and lists created
paths once: operation log first, then blocks in frozen plan order, effect
attempts in numeric order, each attempt's retained adapter output directly
after its own log, and their resolution attempts in numeric order.

### Bounded run output

A [bounded operation](../cli.md#machine-power-operations) registers no lifecycle
operation, so it has no attempt log for its adapter's output to sit beside. It
allocates a `<run-id>` of its own and keeps exactly one file, `run.output`,
holding what that adapter printed on its own standard output and error, raw and
unparsed. It writes no structured log, because it publishes no events, and it
publishes no evidence, ownership or continuation: a run is never an operation,
and nothing resumes, continues or is resolved through one.

The file is created exclusively, under the same grammar, modes and retention
bounds an attempt's retained output has, before the adapter runs, so the path
an operator is given exists whether or not the run reaches its result. Its
content is retained while the run produces it, under the same masking
obligation: every adapter task that reads bound material marks itself `no_log`,
so a retained run carries none. Neither retaining it nor failing to changes what
the run reports.

Human output names the run's own directory as the same `Logs` field, before the
adapter runs and again after the result, for the same reason an operation names
its log tree twice. Naming it first is what a refused run depends on, because a
run that fails reports a diagnostic instead of a result. JSON `logs` names the
retained file the same way an operation's logs are named.

## Multi-machine presentation

Under the [Go/Ansible boundary](../architecture.md#go-and-ansible-responsibility-boundary),
Bootwright consumes bounded structured events and does not forward or parse
Ansible prose, banners, recap, color, callback formatting, play names, or task
names as managed-operation output.

A multi-machine presentation group is a domain-owned step frozen into a plan
block; its identity, description, Machine target set, order and outcome meaning
follow the [stage contract](../state-reconciliation.md#stages-and-the-pause-boundary).
It is presented as a [sub-step](#long-running-progress) of its block: its frozen
description is the detail of the block's running row, and only a group the
frozen block declares advances the block's completion when it settles. A group
never settles as a row of its own, adds no result row and appears in no JSON
result, so event arrival, parallelism, retry and callback batching cannot alter
the frozen order of the result that follows. Per-Machine group results, with
their exception rows, aggregate precedence and JSON `groups`, are
[deferred](../deferred/cli-access-and-rendering.md#multi-machine-group-results).

Grouping is presentation only. Concurrency, retry, stop, replay, durable
evidence, and continuation belong to
[state reconciliation](../state-reconciliation.md); an adapter cannot infer or
alter them.

## Compatibility

Command paths, positional grammar, global and local flag names, shorthands,
accepted values, defaults, precedence, conflicts, prompt boundaries, stream
assignments, exit meanings, status tokens, diagnostic codes, JSON field names
and types, sensitive/raw boundaries, log references, and machine-grouping rules
are public contracts. A new optional field, enum value, warning, default, or
stricter validation can break an exhaustive consumer and requires compatibility
review.

Help and diagnostic wording may be clarified only when syntax, code, safe
location, object, field, meaning, and next action remain unchanged. Authored and
effective-state compatibility is governed by [the API contract](../api.md).

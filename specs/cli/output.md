# CLI Output

Read with [CLI behavior](../cli.md) and the [command catalog](commands.md).

## Streams and exit status

Every invocation has one primary mode. Diagnostics never masquerade as primary
output.

| Condition | Standard output | Standard error | Exit status |
| --- | --- | --- | --- |
| Human structured success | result or help, LF-terminated | ordered warnings, and the prompt of an interactive confirmation or input | `0` |
| Human operational or validation failure | empty, except a complete negative secret check, controller readiness report, or already presented setup plan/progress or lifecycle state | ordered diagnostics, each LF-terminated | `1` |
| Human usage failure | empty | usage diagnostic and concise help, LF-terminated | `2` |
| JSON success or failure | exactly one JSON document followed by one LF | empty | the document's required `exitCode` |
| Effective-state text | exact canonical effective YAML with its required final LF | empty | `0` |
| Human effectful outcome | ordered plan, progress, status and result | ordered warnings and diagnostics, and the prompt of an interactive confirmation or input | `0`, `1`, or `130` |
| Explicit sensitive result | exact requested bytes, with no added LF | diagnostics only on failure | `0` or `1` |
| Completion script | exact script with its required final LF | empty | `0` |
| Access handoff | one bounded, escaped descriptor followed by one LF | ordered diagnostics only | `0` or `1` |
| [SSH session](../cli.md#machine-ssh-sessions) | the remote process's own bytes | Bootwright diagnostics and the host-key confirmation before the connection, then the remote process's own bytes | the SSH client's exit status, even after an interrupt; `255` for a Bootwright refusal before the session opens |
| Interrupt-driven cancellation | as required by the selected structured mode | as required by that mode | `130` |

An operating-system interrupt reports `runtime.interrupted` and exits `130` for
a Bootwright-owned operation. Once an SSH session opened, the session owns its
streams and its status, so an interrupt adds nothing and the client's status
stands. An `apply` or `destroy` interrupted after it
registered its operation first writes that operation's
[result](#lifecycle-and-status-results) and receipt on standard output, and its
`runtime.interrupted` diagnostic names as `next:` the command that continues
it; the diagnostics the cancellation made its blocks report are not repeated.
Another canceled context or expired deadline reports `runtime.canceled` or
`runtime.deadline` and exits `1`.

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
- `[PENDING]` for a frozen lifecycle block that has not started, and for a
  setup check or an object `status` reports not yet bound or realized;
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
  the only status or title line at column zero besides section headings and
  table headers. The [lifecycle receipt](../cli.md#lifecycle-receipt) and a
  name-only result, such as `machine list --silent` or
  `context current --short`, are column-zero lines of their own, not headlines.
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
unescaped control sequence. A command passes each value raw and the renderer
escapes it exactly once, so a value reads in text as the
[safe display text](#json-output) JSON carries: a backslash prints as two, not
four. Layout adds no color, cursor control, or box
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

   `media add` reports its acquisition, or its re-verification of the image an
   interrupted add retained, and then its publication as two steps under
   `Progress`; `media list --checksums` reads each image as one check under
   `Checks`. The media store keeps no log, so no `Logs` field precedes them.
   An acquisition names the origin it reads and counts no sub-steps, so its
   row shows no completion and the heartbeat repeats it; it closes with the
   bytes it wrote. A check closes `[OK]` when the image matches its record and
   `[FAIL]` naming the digest it computed when it does not. A plain
   `media list` and a `media delete` report no progress.
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
`status` reports on one context, so it opens with that context as its headline,
`[OK] Context <name>`, followed by its `Mode`. Every other human result omits
the context block; the selected context is already addressable through
`context current`. JSON results keep their documented `context` field
unchanged, so machine consumers lose nothing.

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
never invents a force command. A `next:` clause or next step that names a
context-backed command carries `--context <name>` for the context the
invocation resolved, so a copied remedy never acts on another context's object
of the same name; the context-free commands `setup`, `media` and `version`
take none.

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
| `status` | `context`, `setupChecks`, `desired`, `clusters`, `storageClusters`, `shared`, `secrets`, `nextSteps`, `lifecycle`, `contradictions` |
| `render` | `inputDir`, `outputDir`, `effectiveStatePath`, `lockPath`, `inventoryPath`, `varsPath`, `installer`, `storage` |
| `render effective` | `counts`, `effectiveState` |
| `render installer` | `clusters` |
| `render storage` | `clusters` |
| `machine list` | `context`, `machines`, `powerRead` |
| `machine trust` | `context`, `dryRun`, `pending`, `recorded`, `hosts` |
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

`severity`, `code`, and `message` are required. `severity` is `error` or
`warning` (`TestDiagnosticSeveritiesAreErrorOrWarning`), which human output
presents as `[FAIL]` and `[WARN]`; a warning never changes the exit status.
`source`, `object`, `field`, and `remediation` are omitted when unavailable and
are never `null`. A present source orders `path`, `document`, `line`, and
`column`; positive coordinates are one-based and `0` means unavailable.
Sensitive explicit-result bytes never appear in this structure.

Safe display text preserves printable UTF-8 and escapes backslash, control
characters, ESC, newline, and invalid UTF-8 bytes with deterministic `\\`,
`\n`, `\r`, `\t`, `\uNNNN`, or `\xNN` sequences before JSON encoding.

### Machine results

Each `machines` entry of `machine list` orders its fields as follows:

```json
{"name":"rhel-01","contact":"rhel-01.lab.example.test","addresses":["198.51.100.11"],"os":"installed","provider":"lab-libvirt","clusters":[],"lifecycle":"applied","power":"on"}
```

`name`, `os` and `lifecycle` are strings and always present. `addresses` and
`clusters` are arrays, always present and empty when there are none:
`addresses` keeps the order the Machine declares, and `clusters` is sorted.
`contact`, `provider` and `power` are strings, each omitted when the Machine has
none. An absent `power` means no reading was taken, which `powerRead` reports,
or no management controller answers for the Machine, and is distinct from
`unknown`, a controller that was asked and gave no usable answer. What the
contact, the addresses, the lifecycle position and the power reading mean is
owned by [resource inspection](../cli.md#resource-inspection-and-explicit-access).
The human table shows the same columns, `NAME`, `CONTACT`, `ADDRESSES`, `OS`,
`PROVIDER`, `CLUSTERS`, `LIFECYCLE` and, only when a reading was taken,
`POWER`, with `-` for an absent value.

`machine start`, `machine stop` and `machine restart` report `power`, the `on`
or `off` state the management controller proved after the verb; `previous`, the
state it reported before the verb, omitted when it reported none; and
`changed`, whether the verb changed it.

### Media results

Each `media` entry of `media list` orders its fields as follows:

```json
{"name":"rhel-9.8-x86_64-boot.iso","size":1045430272,"sha256":"e8b0f3a61d9c2e47b5a803f6d1c94e27a0b6d3f81c5e9a24d7b0e3f6a19c5d82","source":"file:///srv/images/rhel-9.8-x86_64-boot.iso","added":"2026-09-20T08:15:00Z","frozen":true,"reservedBy":["lab-rhel"],"verified":"ok","computed":"e8b0f3a61d9c2e47b5a803f6d1c94e27a0b6d3f81c5e9a24d7b0e3f6a19c5d82"}
```

`name`, `size`, `sha256`, `source`, `added`, `frozen` and `reservedBy` are
always present. The first five are the image's
[record](../managed-os.md#media-store): its name, its size in bytes, its
hexadecimal SHA-256, its credential-free origin and its time of publication.
`reservedBy` is the array of contexts that reserve the image, sorted and empty
when none does, and `frozen` is `true` exactly when it is not empty. `verified`
is `mismatch` when the bytes the store holds no longer have the size the record
states and, with `--checksums`, also when their computed digest differs from
`sha256`; it is `ok` only with `--checksums`, when both match, and is otherwise
omitted, never empty. `computed` is the hexadecimal SHA-256 that `--checksums`
computed from the bytes, and is present only with `--checksums`. Whether a
context reserves an image never hides whether it verified.

The human table shows the columns `NAME`, `SIZE`, `DIGEST`, `COMPUTED` (only
with `--checksums`), `ADDED`, `RESERVED` and `STATE`, with the digests
prefixed by `sha256:`. `RESERVED` joins the reserving contexts with commas, or
shows `-`. `STATE` is `verified` for an `ok` image; `mismatch` for a
`mismatch` one, which `media add` replaces or `media delete` removes; and
`stored` for an image whose bytes were not read and whose size matches its
record.

Before a replacing `media add` or a `media delete` prompts, standard output
shows the stored image under a `Media replacement` or `Media deletion` title:
its `Name`, then the `Size`, `Digest`, `Added` and `Source` of its record, or a
`Record` field reading `unreadable` when the name holds an image whose record
cannot be read. A replacement adds `New source`, the origin the new record
would carry: when its pin adopts the stage an interrupted add retained for that
name, that stage's source, and otherwise the origin of the source it names
(`TestAReplacementShowsTheSourceItsRecordWillCarry`); a deletion that also
removes the stage an interrupted add retained
adds `Retained`, and one that removes only such a stage shows its name and
`Retained` alone. A presentation that cannot be written refuses the command
before it prompts. The result of `media delete` names the image and, when its
record could be read, the size and digest that record stated.

## Cluster discovery

The `cluster list` and `cluster info` result shape, `accessCommands` included,
is [deferred](../deferred/cli-access-and-rendering.md#cluster-discovery) to a
parked item, [B101](../milestones/backlog.md#b101).

## Lifecycle and status results

The human result of `plan`, `apply` and `destroy` composes the
[shared layout](#shared-human-layout): an optional headline, a `Plan` section
whose steps are the frozen blocks in plan order, each naming its stage and
carrying its own impacts as indented lines, a `Checks` section for what the
operation proves before it registers, a `Progress` section while effects run, a
`Result` section of status rows, the `Logs` reference when an operation log
exists, a `Next` field naming the exact command, with `--context`, that
continues a `paused`, `failed`, `unknown` or `running` operation, and the
receipt as the final four lines.

Because the plan is frozen
[wave by wave](../state-reconciliation.md#plan-and-execution), the numbered
steps are the order the work is started in. Each step that waits for another
names the steps it waits for by their place in that list, as `[after 2, 5]`,
and a step that waits for nothing carries no such marker, which is what marks
it as one of the first to start. A step whose consequence consumes an
authorization carries the token, as `[data-loss]`, after its stage and state
and before its selection and `[after …]` markers. A closing `Concurrency`
field reports the plan's own width, as `4 waves, widest 5 steps`, so a long
plan that is one chain reads differently from a long plan that is wide, and
when this build starts fewer blocks at a time than its widest wave holds it
adds that bound, as `; this build starts 1 block at a time`. A fresh
`destroy`'s plan, previewed or presented, adds a `Stop first` field naming in
plan order the Machines whose removal its quiescence gate observes on the host,
as `Stop first  rhel-01, rhel-02`, because that gate follows the prompt; a
continuation is not gated again and carries none. Every
presentation of a plan that consumes an authorization closes with a `Requires`
field naming each token the whole frozen plan requires and the steps that
consume it, as `--authorize data-loss (step 3, 5)`; a finalization's preview
carries none, because the `apply` or `destroy` that completes it runs no block
and requires no authorization. With a stage selection,
each pending step also says whether this invocation would start it, that it is
not selected, or which block it waits on, and a closing field reports how many
blocks would start and how many are deferred. While a block is unproved or
failed, the blocks the next `apply` works first are marked `resolve` or
`retry` and counted as started, and every other ready step reads deferred,
waiting on the first of them. A block row leads with its status
token and names the block description and its outcome; its
[presentation groups](#multi-machine-presentation) are never result rows. A
preview and a refusal have no progress, result rows or log reference.

`status` is the machine-readable view of the same durable state and performs no
probe. Its result orders these fields:

| Field | Contract |
| --- | --- |
| `context` | `name`, `mode` of the resolved context. `mode` is the context record's mode, which a lifecycle read admits only as `ready`. |
| `setupChecks` | `{id, status}` rows derived from stored controller evidence alone, without host probes: `controller-binding`, then `execution-bundle`, the identities [controller readiness](../controller.md#results-and-qualification) gives the same prerequisites. `execution-bundle` is `ready` when the controller record exists, is initialized and holds a complete setup receipt, which is what an apply needs before it claims the host, and `not-ready` otherwise. `controller-binding` is `ready` when that record binds this context, `pending` when it does not and the context holds no operation, because the first apply publishes it, and `not-ready` when it does not while an operation exists. Readiness itself never reports `pending`. |
| `desired` | `revision`, `environment` and admission `counts` of the selected immutable input. `counts` holds `filesSeen` and `objectsDecoded`, and an input that does not compile fails `status` with its diagnostics, so `desired` is always an object. |
| `clusters`, `storageClusters` | Ordered `{name, kind, status}` rows for selected cluster roots, `status` in the realization vocabulary below. |
| `shared` | Ordered `{kind, name, machine, status}` rows for selected managed shared services, `status` in the realization vocabulary below. |
| `secrets` | `declared`, the Secret objects the selected input declares, and `bindings`, the Secret bindings the current operation's record holds: one per operation however many Secrets it covers, and 0 beside no operation and once a completed removal finalized, which its pristine evidence proves without reading the keyring. |
| `nextSteps` | Ordered command strings whose decision would pass over the records `status` read, as the [lifecycle receipt](../cli.md#lifecycle-receipt) states; empty when none would. |
| `lifecycle` | `null` when no operation exists, else `operation`, `verb`, `state`, `next`, ordered `blocks` of `{id, description, stage, state, attempts}`, and `logs` of safe paths relative to the state root, in the order and form the envelope's [`logs`](#private-operation-logs) list them. A block that is `unknown` or `running` adds `unresolved`, `{reason, remedy}`: why its outcome is unproved and what the operator does before repeating a verb, as its [resolution](../state-reconciliation.md#attempts-and-unknown-outcomes) names them; every other block omits it. |
| `contradictions` | Ordered strings naming what the context's durable records contradict, each worded as the [refusal](../state-reconciliation.md#continuation-and-removal) over those records names it, and last an operation whose continuation or removal reopens a frozen Secret binding the context's keyring no longer lists, or whose keyring listing fails reporting the material corrupt or undecryptable; empty when nothing does. |

The cluster and shared rows share one realization vocabulary. Each object the
current operation's frozen plan names reads what all of that object's blocks
prove, whatever its declaration now says:

- `unsupported`: no capability of this executable claims the kind, its
  capability refuses the object, or it is a managed service retained
  `install-only`;
- `pending`: this context has not realized it: nothing names it, its blocks
  have not started, or a removal took it back or released it; the next apply
  realizes it;
- `done`: an apply proved every block of it;
- `failed`: a block of it failed, under either verb; and
- `unknown`: a block of it is running or `unknown`, or no record proves what
  a removal did to it.

Under an apply, an unproved block decides first, then a failed one, then one
not started. Under a removal, a removal block that is unproved or failed
decides the same way, and an object of a completed removal whose block record
does not read done, which only a lost record leaves, reads `unknown`, as the
contradiction status lists for that block says. Otherwise any removal block
proved gone makes the object `pending`, and so does a removal that names fewer
of its blocks than the apply it removes owned, because an earlier attempt took
the rest back. An object whose removal has not started reads what that apply
proved about it, except under a removal that names fewer blocks in all than
that apply owned: such a removal replaced a failed one, whose outcome for the
object no record keeps, so the object reads `unknown`. An object that apply
owned and the removal no longer names was released by an earlier attempt and
reads `pending`. An object no operation names keeps what its declaration gives
it: `unsupported` for the reasons above, else `pending`. Human `status`
presents them as `[SKIPPED]`, `[PENDING]`, `[OK]`, `[FAIL]` and `[UNKNOWN]`,
and any other value as `[UNKNOWN]`.

Rows sort by their documented key: checks in the order above, blocks in frozen
order, contradictions in the order their refusal names them, everything else
in ascending bytewise name order. Human `status` presents the same membership
and order, omitting empty sections. Its Setup rows name each check by the
label controller readiness gives it, and a `pending` controller binding adds
`bound by the first apply`.
Its Lifecycle section also names the build that registered the operation, as
`version` spells it, `<version> (<commit>)`, or `devel (<commit>)` for a build
stamped with no version, and the host directory of its logs, which JSON leaves
out, and an `Unresolved
<block>` section follows it for each block that carries `unresolved`, with its
`Reason` and `Remedy`.

## Diagnostic taxonomy and order

Diagnostic codes are stable machine identifiers. Include an object identity
only when its kind and name form a valid API identity; malformed names are
reported through the source coordinates and `$.metadata.name` field without
repeating unbounded authored text in every diagnostic. This table is the
registry: it lists every code production code emits, from a function body or a
package-level variable initializer, and nothing else
(`TestDiagnosticCodesMatchOutputSpec`). That check follows a code through
constants, variables, parameters and calls, a call through a variable bound to
a function included. A variable holds every value written to it anywhere, the
reading scope's own writes included, and a parameter also every argument it
receives. With no type information, a method call resolves by name to every
method it can select: an unexported one of its own package, or an exported one
of any package. A write it follows that gives no single value (a tuple or
compound assignment, a range clause, a taken address, a package variable
declared without a value) fails the check, and so does a function storing its
argument as a code that is used as a value other than by calling it or binding
it to a variable.

| Code | Meaning |
| --- | --- |
| `input.not-found` | A required input path does not exist. |
| `input.not-directory` | A required directory input is not a directory. |
| `input.symlink` | An input or managed path violates the link policy. |
| `input.read` | An input cannot be safely read. |
| `input.limit` | A documented resource limit was exceeded. |
| `yaml.syntax` | YAML is malformed or is not valid UTF-8. |
| `yaml.duplicate-key` | A mapping repeats a key. |
| `yaml.alias` | An anchor, alias, or merge key is present, on a value or a mapping key. |
| `yaml.tag` | A YAML node, a mapping key included, uses an unsupported or forbidden tag. |
| `yaml.shape` | A document is not a mapping, or a mapping key that carries no anchor, alias, merge, or forbidden tag is not a string. |
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
| `context.orphaned` | A completed deletion abandoned objects the context owned, which are no longer managed. |
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
| `lifecycle.stage` | The stage selection admits no startable block, excludes the block the operation must retry, or is given to a `plan` that previews a `destroy`. |
| `lifecycle.authorization` | The frozen plan requires an authorization that was not validly supplied, or a supplied one it does not require. |
| `lifecycle.lease` | The context root lock or mutation lease cannot be safely acquired or recovered. |
| `lifecycle.unknown` | A frozen block has an unresolved unknown effect outcome. |
| `lifecycle.live` | A removal would take back state that is still in use, and refuses before registering. |
| `lifecycle.adapter-running` | An adapter of the same context, or one whose record names no context, still holds its job lock, so no adapter of that context starts until it ends. |
| `trust.identity` | SSH identity is missing, changed, contradictory, or not authorized. |
| `access.unavailable` | An applicable access request lacks required local access metadata or an available credential artifact. |
| `access.target` | Explicit access cannot resolve one exact authorized target. |
| `access.handoff` | An explicit access descriptor cannot be safely resolved or encoded. |
| `access.credential` | A Machine's SSH access names a password credential the operator reveals and types; written before the connection. |
| `cluster.not-applicable` | A cluster command does not apply to the selected cluster's kind. |
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
no lifecycle identity or log. Setup uses its
[private recovery receipt](../controller.md#publication-and-interrupted-setup),
separate from the lifecycle receipt and operation logs, and keeps what its
controller Ansible printed in a [setup run](#setup-run-output) and nothing
else. Only lifecycle effects and unknown-effect resolution use the state-owned
operation, block, and attempt log tree below; a
[bounded run](#bounded-run-output) retains what its own adapter printed and
nothing else.

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
material marks itself `no_log`, and the adapter's output callback prints
nothing a `no_log` result raised
([security](../security.md#logs-output-and-diagnostics)), so the adapter's
output never carries it. The file is appended to while the run produces it,
within a bounded delay, so a run that wedges is readable before it ends rather
than only once it stops. That
requires the adapter process to be launched so that it does not withhold its own
output, because a tool that buffers until it exits defeats the retention
whatever this side does. Publishing is coalesced rather than written a line at a
time, and the file is bounded and truncated at its limit without an inline
marker, or sooner where it would take the operation area into the bytes
[admission keeps free](../contexts.md#storage-locking-and-publication) for
records and logs, which leaves no file once nothing fits beneath them. It is
troubleshooting material only: nothing reads it back, it is never product
output, ownership evidence or a continuation cursor, and its attempt log
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
bounds an attempt's retained output has, once the controller admits the run's
private Python runtime and before the adapter runs, so the path an operator is
given exists whether or not the run reaches its result. Its content is retained
while the run produces it, under the same masking obligation: every adapter
task that reads bound material marks itself `no_log`, so a retained run carries
none. Neither retaining it nor failing to changes what the run reports. An
adapter failure that its output explains points its remediation at this file,
as an attempt's points at the output beside that attempt's log. A
[reading](../cli.md#resource-inspection-and-explicit-access) names no retained
output, so it keeps none: it allocates no `<run-id>`, creates nothing in the
context's runs area, discards what its adapter printed and points no
remediation at output.

Human output names the run's own directory as the same `Logs` field, before the
adapter runs and again after the result, for the same reason an operation names
its log tree twice. Naming it first is what a refused run depends on, because a
run that fails reports a diagnostic instead of a result. JSON `logs` names the
retained file the same way an operation's logs are named. A JSON invocation
reports no progress, so a run that fails once the controller admits its private
Python runtime, which is where human output first names the directory, names
the file in its failure envelope: `result` is `null` and `logs` lists the file
beside the diagnostic, whether the adapter refused or the run was interrupted,
canceled or past its deadline. A run that fails before that admission lists
none, as its human output names no directory, and keeps none: the file is
created only once the runtime is admitted, so a refused admission, such as a
native package transaction holding its lock, or an interrupt, cancellation or
deadline that lands first leaves no unnamed file behind. A run whose directory
or file cannot be created, including one an interrupt stops between the two
and one whose file landed before an interrupt, or a failed read-back or sync of
its publication, stopped it, fails before its adapter runs, names neither and
removes what it made, even once interrupted: the file while it still holds
nothing, then the directory.

### Setup run output

Local `setup` registers no lifecycle operation and selects no context, so the
Ansible that installs or recovers its container runtime has neither an attempt
log nor a context's run area to sit beside. A confirmed setup that starts that
Ansible, for a preparation or a recovery, first creates a run of its own
beneath the controller directory and keeps exactly one file there:

```text
/var/lib/bootwright/controller/runs/<setup-run-id>/
  run.output
```

`<setup-run-id>` is `setup-` and six digits, numbered upward from
`setup-000001`. The
[controller record](../contexts/controller-record.md#setup-runs) owns its
grammar, modes, byte bound, admission and retention: only the newest runs are
kept, and the oldest is removed before a new one is created. The run and its
file are created exclusively beneath the held controller directory handle,
never through a link and never over an existing name. The file exists before
the Ansible starts and keeps its standard output and error, raw and unparsed,
while it runs, under the masking obligation a bounded run's output has: every
task that reads bound material marks itself `no_log`. It is truncated at its
bound without a marker. Like every retained output it is troubleshooting
material only: the receipt never names it and nothing reads it back.

A dry run, a no-op setup, a setup whose approved actions start no Ansible and
the dependency helpers that resolve before the plan create no run. Neither
creating, writing nor removing a run ever changes a setup outcome or its
receipt: a run that cannot be created leaves that Ansible's output discarded,
exactly as a setup kept none.

Human setup output names the run's directory on this host as the same `Logs`
field, once before the Ansible starts and once with the result, for the reason
an operation names its log tree twice. Setup has no JSON output, so no `logs`
member names it.

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

# CLI Output

Read with [CLI behavior](../cli.md) and the [command catalog](commands.md).

## Streams and exit status

Every invocation has one primary mode. Diagnostics never masquerade as primary
output.

| Condition | Standard output | Standard error | Exit status |
| --- | --- | --- | --- |
| Human success | result or help, LF-terminated | ordered warnings, and the prompt of an interactive confirmation or input | `0` |
| Human operational or validation failure | empty, except a complete negative secret check, controller readiness report, or already presented setup plan/progress or lifecycle state | ordered diagnostics, each LF-terminated | `1` |
| Human usage failure | empty | usage diagnostic and concise help, LF-terminated | `2` |
| JSON or YAML success or failure | exactly one document in the selected format followed by one LF | empty | the document's required `exitCode` |
| Effective-state text | exact canonical effective YAML with its required final LF | empty | `0` |
| Human effectful outcome | ordered plan, progress, status and result | ordered warnings and diagnostics, and the prompt of an interactive confirmation or input | `0`, `1`, or `130` |
| Explicit sensitive result in default text mode | exact requested bytes, with no added LF | diagnostics only on failure | `0` or `1` |
| Completion script in default text mode | exact script with its required final LF | empty | `0` |
| Access handoff | one bounded, escaped descriptor followed by one LF | ordered diagnostics only | `0` or `1` |
| [SSH session](../cli.md#machine-ssh-sessions) | the remote process's own bytes | Bootwright diagnostics and the host-key confirmation before the connection, then the remote process's own bytes | the SSH client's exit status, even after an interrupt; `255` for a Bootwright refusal before the session opens |
| [Several SSH sessions](../cli.md#machine-ssh-sessions) | each session's standard output as whole lines prefixed `<machine>: ` | Bootwright diagnostics and the host-key confirmations before any connection, then each session's standard error as whole lines prefixed the same way, then one `access.exit` warning for each nonzero status | the highest status any session returned, even after an interrupt; `255` for a Bootwright refusal before any session opens |
| Interrupt-driven cancellation | as required by the selected structured mode | as required by that mode | `130` |

An operating-system interrupt reports `runtime.interrupted` and exits `130` for
a Bootwright-owned operation. Once an SSH session opened, the session owns its
streams and its status, so an interrupt adds nothing and the client's status
stands. An `apply` or `destroy` interrupted after it
registered its operation first writes that operation's
[result](#lifecycle-and-status-results) and receipt on standard output, and its
`runtime.interrupted` diagnostic names as `Next:` the command that continues
it; the diagnostics the cancellation made its blocks report are not repeated.
Another canceled context or expired deadline reports `runtime.canceled` or
`runtime.deadline` and exits `1`.

A write or flush failure on standard output exits `1` with best-effort cleanup
and never recursively emits a second representation or fallback on standard
error. If any sensitive bytes were written, it emits no diagnostic that could
be mistaken for part of that value. Human text, JSON, YAML, help, and completion
are UTF-8 with LF line endings;
default explicit sensitive results remain exact byte streams. Presentation may
adapt human output to explicitly selected flags and injected terminal
capabilities. JSON and YAML never contain streamed progress, color or cursor control.
Live SSH sessions are a stream exception: selecting either structured format
refuses with `cli.usage` before service dispatch, privilege or session effects.
Structured secret input from a terminal also refuses with `cli.usage` before
privilege or any hidden read; provide it through a pipe or a file.
Default raw exports remain byte-exact. Explicit structured credential and
completion exports use `result.export` with `mediaType`, `encoding` (`utf-8`
or `base64`) and `content`, preserving every original byte when decoded.
Exported content is base64-encoded, in JSON and YAML alike, when it is not
valid UTF-8 or contains a C0 control other than tab, line feed or carriage
return, DEL, or a C1 control (U+0080-U+009F); every YAML string value is
double-quoted, and so is every mapping key that is not a letter followed by
letters, digits, `_`, `.`, `-` or `/`, or that a YAML 1.1 reader would read as
a boolean or null. A YAML string keeps every character the JSON string holds.

## Human output

The shared renderer presents status meaning, resource identity, grouped facts,
and the next safe action. Successful headlines use `✓`, pending or unchanged
headlines use `○`, and failed or unknown headlines use `!`; `--ascii` selects
ASCII equivalents. Diagnostics use explicit `error` or `warning` severity with the safe subject
and its detail group. Progress retains explicit state words such as `[RUNNING]`, `[DONE]`,
`[SKIPPED]`, `[FAIL]`, `[CANCELED]` and `[UNKNOWN]`.

Identity is bright white, nested details grey, active work and stage headings
blue, verified completion green, attention or pending evidence amber, and
failure red. Meaning remains explicit in text when color is absent. Warnings
never change exit status. A resource's name remains identity text even when its
spelling is a status word. Commands provide explicit identity and state-column
metadata; the renderer never infers semantics from a header or a value.

### Shared human layout

Every command composes the same semantic blocks. Commands supply raw facts;
one renderer owns escaping, styling, spacing, wrapping and column widths.
Headlines, section headings and tables begin at two spaces; ordinary fields
begin at four spaces. Resource info uses a resource heading followed by grey
group headings at four spaces and grey details at six spaces. One blank line
separates top-level groups. Fields align values within their group. Tables use
three spaces between columns and switch to stacked resource records when the
available width cannot hold them; every identity, state and detail is retained.
Plans group numbered changes by stage in frozen order and indent the effects
beneath the change that causes them.

Widths measure terminal cells after display escaping, accounting for combining
characters, wide glyphs and renderer-generated ANSI sequences. Untrusted values
are escaped exactly once before layout. Tables and long fields wrap without
losing facts. Paths, identifiers and commands are never split and may exceed the
width. An explicit width is capped by an injected terminal width; the
renderer leaves the final terminal cell unused to avoid an accidental wrap.
With no width or terminal capability, the default is 96 columns. No line ends
in padding. The composition root supplies terminal, no-color and dumb-terminal
facts; `internal/cli` performs no environment or descriptor discovery.
Standard output and standard error use their own injected terminal capabilities.
SSH streams retain their original descriptors without presentation wrappers.

Resource lists emphasize each white identity, grey descriptive columns and
semantic states. Resource info groups only facts the existing service result
provides: machine Placement, Access and Evidence; secret Identity and Custody;
cluster Topology, Access, Evidence and Custody where meaningful. No view invents
roles, counts, usage, fingerprints, timestamps or readiness. Compact detail
keeps essential identity, state, access and remedies; full detail adds available
verbose evidence and access templates. Structured modes retain the complete DTO
regardless of detail, color, width or ASCII selection.

The lifecycle receipt remains exactly four unstyled, unwrapped lines. Name-only
text and default raw exports retain their defined bytes. Styling and wrapping
apply only to product presentation, never to exported content.

### Long-running progress

Progress reports events the application actually observed. The presentation
model copies the frozen plan and observes events; it never admits work,
schedules a block or changes domain state. Static previews show pending work
without an unknown percentage, fake pace, ETA or elapsed-time extrapolation.
During execution, the lifecycle meter has ten squares and counts durable done
blocks against **all** frozen blocks, including pending and deferred blocks.
A continued operation seeds its durable states from the frozen preview. ASCII
mode uses `#` and `-` for filled and empty squares. Full detail shows available
dependency waits; active rows show the actual stage, target and sub-step.

An authorized step that can exceed the heartbeat interval opens a running row
before it starts. Effects appear under `Progress`, after the private `Logs`
location when one exists. Read-only proofs appear under `Checks`; a machine
power reading and a media checksum check retain no log. Plain local machine
and media listings report no progress. Setup and controller readiness retain
the headings their results define. `media add` reports acquisition (or the
re-verification an interrupted add needs) and publication as two steps;
`media list --checksums` reports one check per image and closes it with a
verified match, the computed mismatching digest, or the read failure.
An acquisition names its actual source and reports the bytes written when it
settles. `machine list --power-status` reports one read check per host through
which its selected controllers are reached, with that role's groups as its
sub-steps; it settles with the proved number of machines read or a failure.

One row belongs to each running step; sub-steps update that row and never settle
as independent work. A known sub-step count may show the percentage actually
proved, and an unknown count shows none. Every step settles with its observed
outcome and a duration only when at least one second elapsed. A running step
silent for ten seconds gets a `still running` heartbeat and its elapsed time.
Concurrent steps retain separate rows. Checks and work never masquerade as one
another, and only durable done events advance the lifecycle meter.

`--progress auto` redraws on an injected capable terminal and appends through a
pipe. `always` explicitly requests redraw; `never` suppresses progress while
retaining results and log locations. A dumb terminal uses the static form.
JSON and YAML emit no progress. A redraw refreshes each second, remeasures
available cells, keeps each active row on one physical line and may elide its
subject to fit. Cursor movement is limited to carriage return, erase-line and
moving up across rows the presenter owns. A heading change, result or log field
closes the owned block before further output. A settled row scrolls above it.

Progress failures never change an effect, outcome or receipt. Cancellation
stops reporting rather than claiming an unobserved outcome. Timers and terminal
capabilities are injected, so tests exercise heartbeats, resize and cancellation
without ambient terminal or clock dependencies.

### Context identity

Context identity is presented by the commands that own it. `context init`,
`context update`, `context use`, `context current`, `context list`, and
`context delete` present the selected context's name, and its mode where
[their results](../contexts.md#command-results-and-confirmation) show one.
`status` reports on one context, so it opens with that context as its headline,
`✓ Context <name>`, followed by its `Mode`. Every other human result omits
the context block; the selected context is already addressable through
`context current`. JSON results keep their documented `context` field
unchanged, so machine consumers lose nothing.

### Human diagnostics

Human diagnostics open with severity and a safe subject, followed by indented
context and the message. An object-bearing refusal uses its `Kind/name` as the
white subject and puts the diagnostic code beneath it; otherwise the code is
the subject. Source coordinates appear only when known. Optional field and
remedy lines are labelled `Field:` and `Next:`. For example:

```text
  error  Machine/node
    access.unavailable
    inputs.yaml:4:3
    The declared access is unavailable
    Field: $.spec.access
    Next: bootwright machine info --context lab --name node
```

Each raw value is deterministically escaped once before wrapping, so no
attacker-controlled terminal sequence or line break can create presentation
structure. A refusal names the object or block, observed or missing evidence,
safety reason and exact safe next action; it never invents a force command.
A product string names no decision, question, milestone or slice ID and no
vendor release or support status; a refusal of a feature Bootwright does not
realize says so and may name its backlog item.
`TestOperatorStringsNameNoDecisionOrQuestionID` and
`TestRefusalsNameNoVendorRelease` check the Go sources and the collection's
plugins.
A context-backed next action carries `--context <name>` for the context the
invocation resolved. The context-free commands `setup`, `media` and `version`
take none. No diagnostic carries confidential material.

## JSON output

Every command accepts inherited `--output json` or `-o json`. It returns one
compact object with the following envelope fields in this order. Live SSH
sessions refuse this representation before dispatch. Existing JSON result
schemas, ordering and escaping remain stable.

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
| `help` and explicit help | `command`, `usage`, `description`, `commands`, `flags` |
| `version` | `version`, `commit`, `source`, `go`, `target`, `dependencyBundle` |
| `completion <shell>`, `secret show`, `cluster kubeconfig` | `export` |
| `context init`, `context update` | `context`, `counts`, `filesCopied`, `inputChanged` |
| `context use`, `context current` | `context` |
| `context list` | `contexts` |
| `context delete` | `name`, `outcome`, `currentCleared`, `orphansAbandoned`, `releasedReservations` |
| `secret set`, `secret generate`, `secret delete` | `context`, optional `name`, `changed`, `unchanged`, `changedNames`, `unchangedNames`, `parts` |
| `secret encryption init`, `secret encryption rotate` | `context`, `implementation`, `activeKey`, `changed`, `retiredKeys`, `reencryptedVersions`, `reencryptedParts` |
| `media add`, `media delete` | `name`, `outcome`, `size`, optional `sha256` |
| `plan` | `context`, `verb`, `steps`, `stages`, `waves`, `widest`, `bound`, `startable`, `deferred`, `continuation`, `finalizes`, optional `anotherBuild`, `stops`, `stopsAfterOnline`, `receipt` |
| `apply`, `destroy` | `context`, `verb`, `steps`, `blocks`, `settled`, `recovered`, `next`, `receipt` |
| `add-ons list` | `addOns` |
| `secret check`, `secret list`, `secret info` | `context`, `secrets` |
| `secret encryption status` | `initialized`, `implementation`, `activeKey`, `keys`, `items` |
| `media list` | `media` |
| `validate` | `counts`, `excludedContainerClusters`, `excludedStorageClusters`, `excludedResourceFiles`, `advisories` |
| `preflight infra`, `preflight clusters`, `preflight container-cluster`, `preflight storage-cluster`, `preflight add-ons`, `preflight all` | `context`, `target`, `checks`, `summary` |
| `status` | `context`, `setupChecks`, `desired`, `clusters`, `storageClusters`, `shared`, `secrets`, `nextSteps`, `lifecycle`, `contradictions` |
| `diff` | `context`, `operation`, `input`, `plan`, `observed`, `blocks`, `objects`, `summary` |
| `render` | `inputDir`, `outputDir`, `effectiveStatePath`, `lockPath`, `inventoryPath`, `varsPath`, `installer`, `storage` |
| `render effective` | `counts`, `effectiveState` |
| `render installer` | `clusters` |
| `render storage` | `clusters` |
| `setup` (with `--download-only`) | `directory`, `outcome`, `files`, `next` |
| `setup` (mutation), `preflight controller` | optional `context`, optional `machine`, optional `route`, `dryRun`, `outcome`, `platform`, `checks`, `actions`, `requirements`, `files`, optional `preview`, `progress`, optional `receipt`, `next` |
| `setup` (with `--dry-run`) | `dryRun`, `outcome`, `platform`, `checks`, `actions`, `requirements`, `files` |
| `machine list`, `machine info` | `context`, `machines`, `powerRead` |
| `machine trust` | `context`, `dryRun`, `pending`, `recorded`, `hosts` |
| `machine start`, `machine stop`, `machine restart` | `context`, `verb`, `machines` |
| `cluster list` | `context`, `clusters` |
| `cluster info` | `context`, `clusters`, `storage` |

YAML mode encodes the same tagged CLI-owned envelope and result DTO, using
JSON field names and preserving integer values, array order, explicit nulls and
omissions. It writes exactly one document and one final LF, with no progress or
diagnostics on standard error. Color, width, detail and ASCII never alter
structured values. Empty diagnostics and logs remain arrays. Default raw
exports are unchanged; an explicitly selected structured export is sensitive
under the same disclosure authority as its raw command.

Storage preflight reports every check as `id`, `target`, `status`, `reason` and
`next`. Status is `passed`, `failed`, `unknown` or `pending`; only `passed`
proves readiness. Optional `capacity` carries `resource`, `unit`, `required`,
`available`, `reserve`, `deficit` and `observed`. The summary counts each status.
JSON retains the complete report when checks fail. Uninstalled guests remain
pending without remote probes; dry-run performs no live capacity or guest
probe. Installed guest probes use established verified Machine access and a
bounded read-only command set. Anonymous key observation under
`--trust-on-first-use` neither enrolls nor accepts a key.

A `setup --dry-run --output json` that the host or the state root refuses
writes the same fields with `ok` `false` and the refusal in `diagnostics`,
including the `state-root` check, and nothing else on standard output: the
human plan is not written beside it.

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
Each `secret list` and `secret info` entry carries `currentVersion` and `currentSequence` on the
same terms. Human lists show `v<sequence>`, and `-` when none applies. Full
secret info also shows the available durable version identifier; machine
consumers must reference that identifier rather than the ordinal.

`render effective` returns admission `counts` and `effectiveState`, the canonical
ordered array of complete effective objects. Successful `diagnostics` and
`logs` are empty. Text mode emits the exact canonical YAML bytes, including
the final LF, with empty stderr. Context commands also support their tagged
structured result presentations.

A successful human `validate` writes exactly one summary line to standard
output, substituting the decimal counts:

```text
  ✓ Desired state is valid (files seen: <N>, objects decoded: <M>)
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
by an error document. Help, including the successful bare-`render` special
case, uses the selected human or structured representation and performs no
application dispatch.

A diagnostic object orders its fields as follows:

```json
{"severity":"error","code":"api.required","message":"metadata.name is required","source":{"path":"network.yaml","document":1,"line":4,"column":3},"object":{"apiVersion":"bootwright.io/v1alpha1","kind":"NetworkConfig","name":"management"},"field":"$.metadata.name","remediation":"set metadata.name to a unique DNS label"}
```

`severity`, `code`, and `message` are required. `severity` is `error` or
`warning` (`TestDiagnosticSeveritiesAreErrorOrWarning`), which human output
presents as explicit severity and subject groups; a warning never changes the
exit status.
`source`, `object`, `field`, and `remediation` are omitted when unavailable and
are never `null`. A present source orders `path`, `document`, `line`, and
`column`; positive coordinates are one-based and `0` means unavailable.
Sensitive explicit-result bytes never appear in this structure.

Safe display text preserves printable UTF-8 and escapes backslash, control
characters, ESC, newline, and invalid UTF-8 bytes with deterministic `\\`,
`\n`, `\r`, `\t`, `\uNNNN`, or `\xNN` sequences before JSON encoding. Explicit `export.content` is a byte-preserving exception:
the serializer escapes its representation, while decoding the declared UTF-8
or base64 content recovers exactly the requested bytes.

### Machine results

`machine info` uses the same inventory row shape as `machine list`, after its
optional exact-name selection (an unknown name refuses, as in the CLI spec), and always requests no power read. No info
field claims a live observation that the list service did not make.

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
When the width holds them, the human table shows the same columns, `NAME`,
`CONTACT`, `ADDRESSES`, `OS`, `PROVIDER`, `CLUSTERS`, `LIFECYCLE` and, only when
a reading was taken, `POWER`, with `-` for an absent value; otherwise it shows
stacked records with the same facts.

`machine start`, `machine stop` and `machine restart` report `context`, `verb`
and `machines`, whatever the number of Machines `--name` lists. `machines` holds
one row for each Machine whose run was admitted, in ascending bytewise Machine
name order, each with `machine`, its name; `power`, the `on` or `off` state the
management controller proved after the verb, omitted for a Machine whose run
did not settle; `previous`, the state it reported before the verb, omitted when
it reported none; `changed`, whether the verb changed it, `false` for a row
that did not settle; and `logs`, an array of the safe paths, relative to the
state root, of that run's retained output, in the form the envelope's
[`logs`](#private-operation-logs) uses. Envelope `logs` lists every row's files
in row order. When any Machine did not settle and the request named several,
the envelope has `ok` `false` and `exitCode` `1` and still carries the rows of
every admitted Machine, with the diagnostics of each failed Machine in row
order, each with the object `Machine/<name>`. A refusal before any run is
admitted, and the failure of a request that named one Machine, carry
diagnostics alone, with `result` `null`. The human result of several Machines is
a headline, `✓` when any changed and `○` when none changed, and a
table with the columns `MACHINE`, `POWER`, `PREVIOUS` and `LOGS`, where `LOGS`
is the run's directory and `-` stands for an absent value. When any Machine did
not settle the headline is `!`, the table is still written to standard
output and the diagnostics go to standard error. One Machine's human result is
unchanged.

### Media results

Each `media` entry of `media list` orders its fields as follows:

```json
{"name":"rhel-9.8-x86_64-boot.iso","size":1045430272,"sha256":"e8b0f3a61d9c2e47b5a803f6d1c94e27a0b6d3f81c5e9a24d7b0e3f6a19c5d82","source":"file:///srv/images/rhel-9.8-x86_64-boot.iso","added":"2026-09-20T08:15:00Z","frozen":true,"reservedBy":["lab-rhel"],"verified":"ok","computed":"e8b0f3a61d9c2e47b5a803f6d1c94e27a0b6d3f81c5e9a24d7b0e3f6a19c5d82"}
```

`name`, `frozen` and `reservedBy` are always present. `size`, `sha256`,
`source` and `added` are present whenever the image's
[record](../managed-os.md#media-store) could be read, and are omitted
otherwise; with `name` they are that record: its name, its size in bytes, its
hexadecimal SHA-256, its credential-free origin and its time of publication.
`reservedBy` is the array of contexts that reserve the image, sorted and empty
when none does, and `frozen` is `true` exactly when it is not empty. `verified`
is `mismatch` when the bytes the store holds no longer have the size the record
states and, with `--checksums`, also when their computed digest differs from
`sha256`; `failed` when the store could not read the image's record, its file
or its bytes, or the bytes changed, or were deleted or replaced, while
`--checksums` read them; it is `ok` only with `--checksums`, when both match,
and is otherwise omitted, never empty. `computed` is the hexadecimal SHA-256
that `--checksums` computed from the bytes, and is present only when it
computed one. `reason` names why an image failed, and is present exactly when
`verified` is `failed`. Whether a context reserves an image never hides
whether it verified.

The human table shows the columns `NAME`, `SIZE`, `DIGEST`, `COMPUTED` (only
with `--checksums`), `ADDED`, `RESERVED` and `STATE`, with the digests
prefixed by `sha256:`. `SIZE`, `DIGEST` and `ADDED` show `-` for an image
whose record could not be read. `RESERVED` joins the reserving contexts with
commas, or shows `-`. `STATE` is `verified` for an `ok` image; `mismatch` for a
`mismatch` one, which `media add` replaces or `media delete` removes; `failed`
for a `failed` one; and `stored` for an image whose bytes were not read and
whose size matches its record. When any image failed, a `Failed` group follows
the table, naming each failed image and its reason, and then one remedy line:
remove the image with `media delete`, replace it with `media add`, or repeat
`media list --checksums` for one that changed while it was read.

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

`cluster list` and `cluster info` read only validated desired state, durable
block evidence, the current operation's frozen plan and custody presence. They
contact no host and read no credential bytes unless `--secrets` asks.

`cluster list` returns one `clusters` array containing both cluster kinds in
ascending bytewise order of cluster name. Every entry identifies `name` and
`kind`, with `kind` exactly `ContainerCluster` or `StorageCluster`. Human output
shows the same names and kinds in the same order.

`cluster info` retains its `context`, `clusters`, and `storage` result fields.
`clusters` is the ContainerCluster array and `storage` is the StorageCluster
array; each sorts by cluster name and is empty when its kind is not selected.
Each cluster entry includes `name`, `kind`, and `accessCommands` alongside its
endpoints and artifact metadata. Human output shows container clusters then
storage clusters with the same membership and ordering as JSON. An explicitly
selected unknown or excluded cluster fails `access.target` with exit `1` under
the selected human, JSON or YAML failure mode; it never yields a successful empty
selection. Without `--name`, the catalog's default-all selection applies.

A managed Ceph StorageCluster entry adds these fields, in this order, after its
`name`, `kind` and `accessCommands`:

| Field | Contract |
| --- | --- |
| `management` | `managed`. |
| `release` | The declared Ceph release. |
| `fsid` | The cluster FSID its bootstrap evidence proves. |
| `image` | `{reference, digest, platform}` of the [frozen image](../storage.md#frozen-release-and-trust), from the cluster's image evidence. |
| `monitors` | `[{host, fqdn, address}]`, one per monitor in its cluster-block evidence, mapped to the declared node it runs on. |
| `adminNode` | The declared name of the bootstrap node, which holds the administrator keyring. |
| `cephCommand` | The exact prefix that runs the Ceph CLI as the administrator through `cluster exec`: `bootwright cluster exec --context <context> --name <cluster> --node <adminNode> -- sudo -n ceph --conf /var/lib/bootwright/ceph/<fsid>/ceph.conf --keyring /var/lib/bootwright/ceph/<fsid>/ceph.client.admin.keyring`. The operator appends the Ceph arguments. |
| `status` | `ready`, or `unavailable` with a `reason` and the remedy `bootwright apply --context <context> --stage clusters` when the evidence above is missing; an unavailable entry omits the fields that evidence would carry. |
| `dashboard` | `{status, user, urls, certificateSHA256}`: `status` is `ready`, or `unproved` with a `reason` when the apply could not reach the dashboard; `user` is the administrator account; `urls` and `certificateSHA256` are those the apply proved. |
| `resources` | `[{kind, name, status, scope}]`: the rows `status` reports under this cluster, in the same order with the same `status`, on a ready and an unavailable entry alike. `scope` is a one-line redacted statement of the object in the spellings below, built from the request the current operation froze for it; it is omitted when no request of this build froze the object. For StorageObjectGateway it is the gateway request, not the zone pools that object also freezes. StorageExport, which no capability realizes, takes it from its declaration. No `scope` carries a key, password, Secret reference or other Secret material. |

| Kind | `scope` |
| --- | --- |
| StoragePlacementPolicy | `rule <rule>, failure domain <failureDomain>`, then `, device class <deviceClass>` when one is set |
| StoragePool, replicated | `replicated size <size>/min <minSize>, application <application>` |
| StoragePool, erasure | `erasure k=<dataChunks> m=<codingChunks>, application <application>`, then `, overwrites` when overwrites are enabled |
| StorageRBDImage | `image <pool>/<name>, <sizeBytes> bytes`, then `, data pool <dataPool>` when one is set |
| StorageClient | `client.<name> <access> on pool <pool>` |
| StorageFilesystem | `filesystem <name>, metadata <metadataPool>, data <dataPools separated by spaces>` |
| StorageSubvolume | `subvolume <filesystem>/<group>/<name>`, with `_nogroup`, cephadm's default group, when no group is set |
| StorageObjectGateway | `zone <realm>/<zoneGroup>/<zone>` |
| StorageNFSExport | `export <native.pseudo> on nfs.<serviceID>`, cephadm's service name |
| StorageExport | `export of pool <rbdPoolRef>, filesystem <filesystemRef>`, then `, object gateway <objectGatewayRef>` when set; `export` without `dataFoundation` |

`--secrets --name <cluster>` adds `passwordValue` to `dashboard`, the only
field that carries a secret. Its contract, refusals and warning are the
[storage dashboard access](../cli.md#storage-dashboard-access).

`accessCommands` contains exactly one entry for each canonical command, ordered
`cluster exec`, `cluster kubeconfig`, `cluster kubectl`, `cluster oc`, then
`cluster rsh`. Each entry orders these fields:

| Field | Contract |
| --- | --- |
| `command` | Canonical command path without the executable. |
| `status` | Exactly `not-applicable`, `not-implemented`, `unavailable`, or `ready`, selected by the precedence below. |
| `node` | Default node's declared name for applicable `cluster rsh` and `cluster exec`; omitted for other entries. |
| `reason` | Required non-empty safe explanation when status is not `ready`; omitted when ready. |

Within an available `cluster info` result, evaluate each entry as follows:

1. `not-applicable`: the cluster fails the catalog's
   [applicability rule](commands.md#cluster-command-applicability).
2. `not-implemented`: the command applies but this executable does not implement
   the requested access use case, as for `cluster oc`, `cluster kubectl` and the
   sessions of an OpenShift or OKD ContainerCluster.
3. `unavailable`: the command applies and is implemented, but local access
   metadata, credential-artifact availability, identity, ownership, or context
   state does not establish the prerequisites. The reason identifies the
   missing or blocking evidence and the safe next action.
4. `ready`: the prerequisites are established by local context-owned metadata
   and evidence. This means ready to open the session or request the explicit
   export; it does not assert live endpoint readiness or success of later
   execution.

SSH entries describe the default node selected under
[cluster sessions](../cli.md#cluster-sessions); readiness for that node makes no
claim about other nodes. Inspection opens no credential bytes by default and
performs no live probes to populate these fields. Secret declarations alone do
not prove that access material is available. Sensitive `cluster info --secrets`
retains its separate explicit disclosure boundary.

Human `cluster info` groups available Topology, Access, Evidence, Resources
and Custody facts. Resources lists each row as `<Kind>/<name>` with its
status; full detail appends its scope. Full detail includes verbose frozen
evidence and access templates; compact detail retains statuses, actionable
reasons, ready access and the dashboard certificate fingerprint, the evidence
for the browser's self-signed warning.
It identifies the default
SSH node, and gives context- and cluster-qualified invocation templates for
applicable commands, including `--node` for SSH and `-- <command>...` for a
required payload. A managed Ceph entry also shows its release, FSID, image
reference and digest, monitors, administrator command and dashboard URLs, user
and status. Unavailable and unimplemented templates are visibly labeled;
inapplicable entries explain the restriction and offer no invocation template.
A template is guidance, never an executable handoff or a promise of readiness.

These inspection statuses do not alter invocation precedence: invoking an
unavailable command still follows the no-context-read
[unavailable-command gate](../cli.md#recognized-but-unavailable-commands).

The `oc` and `kubectl` handoffs are
[deferred](../deferred/cli-access-and-rendering.md) to the parked item
[B101](../milestones/backlog.md#b101).

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

Known frozen kinds and implementations use a concise action followed by a separately
styled object identity in plan and effect progress rows. When metadata cannot
distinguish the action, the complete frozen description appears without an
appended object. Check progress retains the check description.

Because the plan is frozen
[wave by wave](../state-reconciliation.md#plan-and-execution), the numbered
steps are the order the work is started in. Full detail names each dependency
wait by the predecessor's place in that
list; compact detail keeps the state and invocation selection. A step whose
consequence consumes an authorization always carries the token and its impacts,
including in compact detail. Stage headings may recur across waves so the
presentation never reorders the frozen plan merely to merge stage groups. A closing `Concurrency`
field reports the plan's own width, as `4 waves, widest 5 steps`, so a long
plan that is one chain reads differently from a long plan that is wide, and
when this build starts fewer blocks at a time than its widest wave holds it
adds that bound, as `; this build starts 1 block at a time`. A continuation
that another build than the one that registered the operation proved it freezes
the same blocks adds a `Build` field after it, naming that build as `version`
spells it and the steps this build runs, as `1.4.0 (9f2c1ab) registered this
operation; steps 2, 3 run under this build`, `step 2 runs under this build` for
one, and `another build registered this operation` first when no executable is
recorded. Steps that are done or not selected are left out, and a fresh plan, a
finalization, a continuation under the registering build and a continuation
with no step left to run under this build carry no such field. A fresh
`destroy`'s plan, previewed or presented, adds a `Stop first` field naming in
plan order the Machines whose initial removal quiescence gate observes on the host,
as `Stop first  rhel-01, rhel-02`, because that gate follows the prompt; a
continuation is not gated again and carries none. Every
presentation of a plan that consumes an authorization closes with a `Requires`
field naming each token the whole frozen plan requires and the steps that
consume it, as `--authorize data-loss (step 3, 5)`; a finalization's preview
carries none, because the `apply` or `destroy` that completes it runs no block
and requires no authorization. With a stage selection,
each pending step also says whether this invocation would start it, that it is
not selected, or which block it waits on, and a closing field reports how many
blocks would start and how many are deferred. While a block is unproved, the
blocks the next `apply` resolves are marked `resolve` and counted as started,
and every other ready step reads deferred, waiting on the first of them; that
`apply` then admits the rest by what the resolutions prove, retrying a block
one leaves failed. While
a block is failed, every failed block the next `apply` retries is marked
`retry` and every ready step that starts beside them `start`, all counted as
started, and a step that depends on a failed block reads deferred, waiting on
it. A block row leads with its status
token and names the block description and its outcome; its
[presentation groups](#multi-machine-presentation) are never result rows. A
preview and a refusal have no progress, result rows or log reference.

A removal with online cleanup also names `Stop after online cleanup`: those
Machines stay reachable for online storage cleanup or for an installation's
unregistration from its subscription service, then destroy pauses with their
stop commands before deleting any of their disks. Repeating destroy after the
operator stops them resumes the registered removal. The field is absent when
no such boundary exists.

`status` is the machine-readable view of the same durable state and performs no
probe. Its result orders these fields:

| Field | Contract |
| --- | --- |
| `context` | `name`, `mode` of the resolved context. `mode` is the context record's mode, which a lifecycle read admits only as `ready`. |
| `setupChecks` | `{id, status}` rows derived from stored controller evidence alone, without host probes: `controller-binding`, then `execution-bundle` (shown to humans as "Controller setup"), the identities [controller readiness](../controller.md#results-and-qualification) gives the same prerequisites. `execution-bundle` is `ready` when the setup subtree holds a receipt and no interrupted setup, which is what an apply needs before it claims the host, and `not-ready` otherwise. `controller-binding` is `ready` when that record binds this context, `pending` when it does not and the context holds no operation, because the first apply publishes it, and `not-ready` when it does not while an operation exists. Readiness itself never reports `pending`. |
| `desired` | `revision`, `environment` and admission `counts` of the selected immutable input. `counts` holds `filesSeen` and `objectsDecoded`, and an input that does not compile fails `status` with its diagnostics, so `desired` is always an object. |
| `clusters` | Ordered `{name, kind, status}` rows for selected ContainerCluster roots, `status` in the realization vocabulary below. |
| `storageClusters` | Ordered `{name, kind, status, resources}` rows for selected StorageClusters. `resources` holds one `{kind, name, status}` row for each selected storage child object (a kind whose `clusterRef` names a StorageCluster) whose `clusterRef` names this cluster, ordered by API kind order then name, `status` in the realization vocabulary below; it is `[]` for an external StorageCluster and for one with no such object. |
| `shared` | Ordered `{kind, name, machine, status}` rows for selected managed shared services, `status` in the realization vocabulary below. |
| `secrets` | `declared`, the Secret objects the selected input declares, and `bindings`, the Secret bindings the current operation's record holds: one per operation however many Secrets it covers, and 0 beside no operation and once a completed removal finalized, which its pristine evidence proves without reading the keyring. |
| `nextSteps` | Ordered command strings whose decision would pass over the records `status` read, as the [lifecycle receipt](../cli.md#lifecycle-receipt) states; empty when none would. |
| `lifecycle` | `null` when no operation exists, else `operation`, `verb`, `state`, `next`, `anotherBuild` (boolean: the build in hand is not the one that registered the operation, by the rule of [another build](../state-reconciliation.md#dependency-safety-during-recovery)), ordered `blocks` of `{id, description, stage, state, attempts}`, and `logs` of safe paths relative to the state root, in the order and form the envelope's [`logs`](#private-operation-logs) list them. A block that is `unknown` or `running` adds `unresolved`, `{reason, remedy}`: why its outcome is unproved and what the operator does, which for a block no resolution observed is the exact command, with `--context` and the frozen plan's `--authorize` tokens, that observes it, as its [resolution](../state-reconciliation.md#attempts-and-unknown-outcomes) names them; every other block omits it. |
| `contradictions` | Ordered strings naming what the context's durable records contradict, each worded as the [refusal](../state-reconciliation.md#continuation-and-removal) over those records names it, and last an operation whose continuation or removal reopens a frozen Secret binding the context's keyring no longer lists, or whose keyring listing fails reporting the material corrupt or undecryptable; empty when nothing does. |

The cluster, resource and shared rows share one realization vocabulary. Each
object the current operation's frozen plan names reads what all of that
object's blocks prove, whatever its declaration now says:

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
the rest back. An object whose removal has not started reads what the nearest
removal it replaces did to it: `unknown` when that removal left a block of it
unproved, `failed` when one failed, `pending` when one was proved gone; and
otherwise what the apply proved. A replacement an earlier build recorded, which
names no removal it replaces, reads `unknown` for such an object when it names
fewer blocks than the apply owned, as before. An object that apply
owned and the removal no longer names was released by an earlier attempt and
reads `pending`. An object no operation names keeps what its declaration gives
it: `unsupported` for the reasons above, else `pending`. Human `status`
presents them as `[SKIPPED]`, `[PENDING]`, `[OK]`, `[FAIL]` and `[UNKNOWN]`,
and any other value as `[UNKNOWN]`.

Rows sort by their documented key: checks in the order above, blocks in frozen
order, contradictions in the order their refusal names them, resources in API
kind order then name, everything else in ascending bytewise name order. Human
`status` presents the same membership and order, omitting empty sections. Its
Setup rows name each check by the label controller readiness gives it, and a
`pending` controller binding adds `bound by the first apply`. A `Storage
resources` section follows `Storage clusters`, with `State`, `Resource`
(`<Kind>/<name>`) and `Cluster` columns in JSON order, and is omitted when no
resource row exists.
Its Lifecycle section also names the build that registered the operation, as
`version` spells it, `<version> (<commit>)`, or `devel (<commit>)` for a build
stamped with no version, followed by `, another build than this one` when
`anotherBuild` is true (alone, as `another build than this one`, when the
record names no executable), and the host directory of its logs, which JSON
leaves out, and an `Unresolved <block>` section follows it for each block that
carries `unresolved`, with its
`Reason` and `Remedy`.

### Diff results

`diff` returns the comparison as one object whose fields are in this order, each
object of `objects` beside the unified `patch` lines the text result prints:

```json
{"context":{"name":"lab","revision":"rev-8c41d07e"},"operation":{"id":"op-3f9a0c12","verb":"apply","state":"done","apply":"op-3f9a0c12","revision":"rev-1b7e55a0","applied":"readable"},"input":"changed","plan":{"state":"differs","reason":"","changed":1,"added":0,"removed":0,"rebuilt":[]},"observed":false,"blocks":{"observed":0,"unknown":0,"unprojected":0},"objects":[{"kind":"StoragePool","name":"rbd-data","difference":"changed","plan":"changed","blocks":["ceph-pool-rbd-data"],"realization":"done","real":{"source":"applied","state":"present","document":{},"notCompared":[],"notes":[],"reason":"","remedy":""},"desired":{"document":{}},"patch":["--- applied/StoragePool/rbd-data","+++ desired/StoragePool/rbd-data"]}],"summary":{"none":0,"changed":1,"added":0,"removed":0,"unknown":0,"applied":0}}
```

| Field | Contract |
| --- | --- |
| `context` | `name` of the resolved context and `revision` of its selected input. |
| `operation` | `id`, `verb` and `state` of the current operation, `apply` and `revision` of the apply the comparison reads (the current apply, or the apply a removal takes back), and `applied`: `readable`, or the reason that apply's input revision cannot be read. Every value is `""` when the context holds no operation. |
| `input` | `none`, `unchanged` or `changed`, by the [comparison rules](../state-reconciliation.md#comparing-desired-and-real-state). |
| `plan` | `state` (`none`, `same`, `differs` or `unplanned`), the planner's `reason` when `unplanned`, the counts of `changed`, `added` and `removed` blocks, and `rebuilt`, the ids of the blocks another build plans differently. |
| `observed`, `blocks` | Whether the comparison read a host, and the counts of blocks `observed`, left `unknown` and `unprojected`. Without a host read they are `false` and zeros. |
| `objects` | Every declared, applied or planned object in API kind order and then name, except one that is both undeclared and absent. |
| `objects[].difference` | `none`, `changed`, `added`, `removed` or `unknown`. |
| `objects[].plan`, `blocks`, `realization` | The object's plan change, its block ids in plan order and its [realization](#lifecycle-and-status-results), each `""` or empty when no block names the object. |
| `objects[].real` | `source` (`applied`, or `observed` for a side `--observe` read), `state` (`present`, `partial`, `absent` or `unknown`), `document`, `notCompared` (the declared paths a live read did not compare), `notes`, `reason` and `remedy`. |
| `objects[].desired` | `document` of the declared object. |
| `document` | The effective-state encoder's canonical JSON of the one object, set only when `difference` is `changed`, `added` or `removed` and that side exists; otherwise `null`. |
| `patch` | The unified patch lines of the text result, empty unless `difference` is `changed`, `added` or `removed`. |
| `summary` | The counts `none`, `changed`, `added`, `removed` and `unknown` of objects by difference, and `applied`, the objects compared with the applied side because no host read projected them, which is `0` without one. |

Every string passes through the display escaping above. The command adds no
diagnostic code.

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
| `preflight.unknown` | Readiness includes unknown or pending checks and no definite failure. |
| `controller.unsupported` | The setup host, dependency combination or acquisition route is not qualified. |
| `controller.identity` | Required controller binding or verified host evidence is missing or contradictory. |
| `controller.state` | Retained setup evidence is incomplete, contradictory or no longer valid. |
| `controller.conflict` | Another setup or context protects a shared prerequisite or holds its coordination boundary. |
| `controller.setup` | A local controller prerequisite action definitely failed. |
| `controller.untested-release` | Informational: the host's release is outside the releases this build was tested on; setup checks its floors and continues. Never a refusal. |
| `controller.loader-preload` | Informational: `/etc/ld.so.preload` is not empty, so it loads libraries into every process, root dnf runs included; setup continues. Never a refusal. |
| `controller.runtime-retention` | Informational: after a setup, a controller runtime a running command holds was kept ("kept: 3 runtimes (1 in use)"), or older runtimes and wheels could not all be removed; the next setup retries. Never a refusal and never changes the setup's outcome. |
| `controller.unknown` | A local controller prerequisite action has an unresolved effect outcome. |
| `lifecycle.state` | Durable lifecycle state does not permit the requested transition or continuation. |
| `lifecycle.unsupported` | This executable cannot realize a selected object: no capability claims its kind, or its capability reports the shape unsupported. |
| `lifecycle.stage` | The stage selection admits no startable block, excludes every block the operation must retry, or is given to a `plan` that previews a `destroy`. |
| `lifecycle.authorization` | The frozen plan requires an authorization that was not validly supplied, or a supplied one it does not require. |
| `lifecycle.lease` | The context root lock or mutation lease cannot be safely acquired or recovered. |
| `lifecycle.unknown` | A frozen block has an unresolved unknown effect outcome. |
| `lifecycle.live` | A removal would take back state that is still in use, and refuses before registering. |
| `lifecycle.adapter-running` | An adapter of the same context, or one whose record names no context, still holds its job lock, so no adapter of that context starts until it ends. |
| `trust.identity` | SSH identity is missing, changed, contradictory, or not authorized. |
| `access.unavailable` | An applicable access request lacks required local access metadata or an available credential artifact. |
| `access.unproved` | A revealed credential was kept from an effect never proved complete, so the access it grants was not proved; a warning. |
| `access.target` | Explicit access cannot resolve one exact authorized target. |
| `access.handoff` | An explicit access descriptor cannot be safely resolved or encoded. |
| `access.credential` | A Machine's SSH access names a password credential the operator reveals and types; written before the connection. |
| `access.exit` | One of several concurrent SSH sessions returned a nonzero status, or its client could not start (status `255`); a warning written after every session ended, naming the Machine and the status. |
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
dnf printed in a [setup run](#setup-run-output) and nothing
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
canceled or past its deadline. A batch of several Machines whose runs failed
with diagnostics keeps the [result](#machine-results) of the runs it admitted
beside them, but one interrupted, canceled or past its deadline has a `null`
`result` and lists every admitted run's file. A run that fails before that
admission lists none, as its human output names no directory, and keeps none: the file is
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
dnf it runs has neither an attempt log nor a context's run area to sit beside.
A confirmed setup first creates a run of its own beneath its setup subtree and
keeps exactly one file there:

```text
/var/lib/bootwright/setup/runs/<setup-run-id>/
  run.output
```

`<setup-run-id>` is `setup-` and six digits, numbered upward from
`setup-000001`. The
[setup subtree](../contexts/controller-record.md#the-setup-subtree) owns its
grammar, modes, byte bound, admission and retention: only the newest runs are
kept, and the oldest is removed before a new one is created. The run and its
file are created exclusively beneath the held setup directory handle, never
through a link and never over an existing name. The file exists before dnf
starts and keeps dnf's standard output and error, raw and unparsed, while it
runs. It is truncated at its bound without a marker. Like every retained
output it is troubleshooting material only: the receipt never names it and
nothing reads it back.

A dry run and a no-op setup create no run. Neither creating, writing nor
removing a run ever changes a setup outcome or its receipt: a run that cannot
be created leaves dnf's output discarded, exactly as a setup kept none.

Human setup output names the run's directory on this host as the same `Logs`
field, once before dnf starts and once with the result, for the reason an
operation names its log tree twice. A dry run's JSON output starts nothing, so
no `logs` member names it.

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
assignments, exit meanings, status meaning, diagnostic codes, structured field names
and types, sensitive/raw boundaries, log references, and machine-grouping rules
are public contracts. A new optional field, enum value, warning, default, or
stricter validation can break an exhaustive consumer and requires compatibility
review.

Help and diagnostic wording may be clarified only when syntax, code, safe
location, object, field, meaning, and next action remain unchanged. Authored and
effective-state compatibility is governed by [the API contract](../api.md).

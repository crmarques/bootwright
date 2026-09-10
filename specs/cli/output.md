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
| Human effectful outcome | ordered plan, status, and presentation groups | ordered warnings and diagnostics | `0`, `1`, or `130` |
| Explicit sensitive result | exact requested bytes, with no added LF | diagnostics only on failure | `0` or `1` |
| Completion script | exact script with its required final LF | empty | `0` |
| Access handoff | one bounded, escaped descriptor followed by one LF | ordered diagnostics only | `0` or `1` |
| Interrupt-driven cancellation | as required by the selected structured mode | as required by that mode | `130` |

An operating-system interrupt reports `runtime.interrupted` and exits `130` for
a Bootwright-owned operation. Another canceled context or expired deadline
reports `runtime.canceled` or `runtime.deadline` and exits `1`. An interrupted
watch reports `runtime.interrupted`, preserves no partial structured result,
and exits `130`.

A write or flush failure on standard output exits `1` with best-effort cleanup
and never recursively emits a second representation or fallback on standard
error. If any sensitive bytes were written, it emits no diagnostic that could
be mistaken for part of that value. Structured text, JSON, help, and completion
are UTF-8 with LF line endings; explicit sensitive results remain exact byte
streams. Terminal detection may select a watch redraw or an interactive prompt
only where defined; it never changes the bytes of a non-interactive structured
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
| `machine list` | `context`, `machines` |
| `machine trust` | `context`, `dryRun`, `hosts` |
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
by an error document. Help is always human text; requesting help performs no
command work and does not establish JSON mode. The successful bare-`render`
special case likewise emits human render help even when `--output json` was
validly selected.

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
the selected human or JSON failure mode; it never yields a successful empty
selection. Without `--name`, the catalog's default-all selection applies.

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
   the requested access use case.
3. `unavailable`: the command applies and is implemented, but local access
   metadata, credential-artifact availability, identity, ownership, or context
   state does not establish the prerequisites. The reason identifies the
   missing or blocking evidence and the safe next action.
4. `ready`: the prerequisites are established by local context-owned metadata
   and evidence. This means ready to request the descriptor or explicit export;
   it does not assert live endpoint readiness or success of later execution.

SSH entries describe the default node selected under
[cluster node selection](../cli.md#cluster-node-selection); readiness for that
node makes no claim about other nodes. Inspection opens no credential bytes by
default and performs no live probes to populate these fields. Secret declarations
alone do not prove that access material is available. Sensitive `cluster info
--secrets` retains its separate explicit disclosure boundary.

Human `cluster info` includes these statuses and reasons, identifies the default
SSH node, and gives context- and cluster-qualified invocation templates for
applicable commands, including `--node` for SSH and `-- <command>...` for a
required payload. Unavailable and unimplemented templates are visibly labeled;
inapplicable entries explain the restriction and offer no invocation template.
A template is guidance, never an executable handoff or a promise of readiness.

These inspection statuses do not alter invocation precedence: invoking an
unavailable command still follows the no-context-read
[unavailable-command gate](../cli.md#recognized-but-unavailable-commands).

## Diagnostic taxonomy and order

Diagnostic codes are stable machine identifiers. Include an object identity
only when its kind and name form a valid API identity; malformed names are
reported through the source coordinates and `$.metadata.name` field without
repeating unbounded authored text in every diagnostic. The shared codes are:

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
| `runtime.output` | The selected output writer failed. |
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
| `addon.catalog` | An add-on name, version, registration, or catalog identity is invalid. |
| `secret.store` | Confidential material cannot be safely read, generated, written, rotated, or deleted. |
| `media.store` | Media identity, integrity, import, publication, or deletion failed. |
| `preflight.failed` | One or more required readiness checks definitely failed. |
| `preflight.unknown` | Required readiness could not be positively determined. |
| `controller.unsupported` | The setup host, dependency combination or acquisition route is not qualified. |
| `controller.identity` | Required controller binding or verified host evidence is missing or contradictory. |
| `controller.conflict` | Another setup or context protects a shared prerequisite or holds its coordination boundary. |
| `controller.setup` | A local setup action definitely failed. |
| `controller.unknown` | A setup action has an unresolved effect outcome. |
| `render.publish` | A requested artifact could not be safely rendered or published. |
| `lifecycle.state` | Durable lifecycle state does not permit the requested transition or continuation. |
| `lifecycle.destroy-unavailable` | Apply is operational but this executable cannot invoke the required future destroy. |
| `lifecycle.authorization` | The frozen plan requires an authorization that was not validly supplied. |
| `lifecycle.lease` | The context mutation lease cannot be safely acquired or recovered. |
| `lifecycle.unknown` | A frozen block has an unresolved unknown effect outcome. |
| `trust.identity` | SSH identity is missing, changed, contradictory, or not authorized. |
| `cluster.not-applicable` | The resolved cluster kind or variant does not support the requested cluster command. |
| `access.unavailable` | An applicable access request lacks required local access metadata or an available credential artifact. |
| `access.target` | Explicit access cannot resolve one exact authorized target. |
| `access.handoff` | An explicit access descriptor cannot be safely resolved or encoded. |

Command contexts may add narrower codes beneath these namespaces before the
corresponding failure is exposed. A namespace alone is not a fallback code.
New codes may be added, but the meaning of an existing code cannot change.

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
log tree below.

Managed operations use this tree:

```text
<state-root>/contexts/<context-name>/state/operations/<operation-id>/
  logs/
    operation.jsonl
    blocks/<block-id>/
      attempt-000001.jsonl
      attempt-000002.jsonl
      attempt-000002-resolution-000001.jsonl
```

Workspace owns `/var/lib/bootwright` and `<context-name>`; state reconciliation
owns `<operation-id>`, `<block-id>`, and attempt numbers. Their safe grammar,
allocation, collision, and crash-gap rules are defined by
[state reconciliation](../state-reconciliation.md#durable-identities-and-private-paths).
Directories are `0700`; files are `0600`. Creation is exclusive beneath a held,
verified root handle and never follows a link or overwrites unrelated content.

`operation.jsonl` contains bounded normalized operation summaries. Each effect
or resolution attempt has one separately identified log. Raw callback bytes,
terminal escapes, environment dumps, command lines containing sensitive values,
secret values or digests, and content protected by an adapter's `no_log`
equivalent are forbidden. Truncation and dropped-event counts are explicit.

The operation log and required attempt log exist before the corresponding
effect or resolution observation. A create, append, flush, or finalize failure
stops admission of new work, requests cancellation, and returns `runtime.log`.
Its effect-state and log-fault transitions depend on the available positive
evidence and follow
[state reconciliation](../state-reconciliation.md#plan-and-execution) exactly.
Logs are troubleshooting material, never ownership evidence or a continuation
cursor.

Human output names a safe relative log path once as `details: <path>` after the
relevant terminal summary. JSON `logs` lists created paths once: operation log
first, then blocks in frozen plan order, effect attempts in numeric order, and
their resolution attempts in numeric order.

## Multi-machine presentation

Ansible is an adapter, not the product interface. Bootwright consumes bounded
structured events and does not forward or parse Ansible prose, banners, recap,
color, callback formatting, play names, or task names as managed-operation
output.

A multi-machine presentation group is a domain-owned step frozen into a plan
block. Its stable ID, safe description, non-empty Machine target set, order, and
outcome meanings come from the capability contract. Results are grouped as
follows:

1. blocks follow frozen plan order and groups follow their order within a block;
2. Machines sort by canonical `Machine/<metadata.name>` identity;
3. every targeted Machine receives exactly one terminal outcome; and
4. event arrival, parallelism, retry, and callback batching cannot alter order.

A human group begins with:

```text
[<STATUS>] <description>: <total> machines (<counts>)
```

Non-zero counts use this order: `changed`, `unchanged`, `skipped`, `failed`,
`unreachable`, `canceled`, `unknown`. Failed, unreachable, canceled, and unknown
Machine identities follow in canonical order:

```text
  [FAIL] Machine/<name>: failed
  [FAIL] Machine/<name>: unreachable
  [CANCELED] Machine/<name>: canceled
  [UNKNOWN] Machine/<name>: unknown
```

Successful per-Machine detail is omitted. Aggregate precedence is `unknown`,
then failed or unreachable, then canceled, then all-skipped, then success. A
missing terminal event, lost connection, indeterminate callback, or unproved
effect is unknown, never success or cancellation. Bootwright completes the
group with one terminal outcome per target before presenting it.

JSON-mode commands that report groups include a `groups` result array. Each
group orders `blockId`, `groupId`, `description`, `status`, `machines`,
`counts`, `exceptions`, and `log`. `status` is `ok`, `skipped`, `failed`,
`canceled`, or `unknown`; all seven count members are present and sum to
`machines`. Exceptions include exactly failed, unreachable, canceled, and
unknown Machines in canonical order. `log` is the safe relative attempt path on
the final group in its block and `null` otherwise.

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

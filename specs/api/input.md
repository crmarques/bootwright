# Desired-state input

This page owns how the compiler acquires and bounds its input: discovery, the
Environment selection order, the fixed input ceilings and the parser boundary.
The [API contract](../api.md) owns grammar, graph validation and effective
output.

## Discovery

The [CLI contract](../cli.md#parsing-and-input-conventions) owns public input
acquisition and flag cardinality. The compiler receives one ordered source
universe: the selected context input, the union of file and directory sources
resolved for context-free validation, or the one file or directory supplied to
context-free whole rendering. An input file must be a readable, regular
lowercase `.yaml` or `.yml` file with exactly one link and must not be a
symlink. An input directory must be readable and must not be a symlink. Kind
selectors and object selectors are not supported.

For multiple sources, discovery cleans each operand, removes repeated cleaned
path strings, recursively discovers every directory operand, combines the
result, removes repeated cleaned file paths, and orders the files lexically.
Flag order gives no precedence and distinct source files never override one
another. The combined universe undergoes one Environment selection, and the
selected documents enter one graph. A duplicate authored object identity in
that graph is an API error under the
[graph rules](../api.md#graph-validation). A resource path declared by
the one `Environment` resolves relative to that Environment's actual source
file, independent of which input operand discovered it.

For each directory source, Bootwright recursively discovers descendant regular
files ending in lowercase `.yaml` or `.yml`. Before reading a file, discovery
does not descend into a non-root dot-prefixed directory or a non-root directory
whose basename is exactly `vendor`, `node_modules`, `playbooks`, `roles`,
`collections`, `manifests`, or `secrets`. The last five names are fixed payload
roots and are never desired-state inputs.
Directory symlinks are not followed, and a discovered YAML symlink is an input
error. Every discovered YAML candidate and add-on marker is a regular file with
exactly one link; a hard-linked one refuses with `input.symlink`, as a symbolic
link does. Paths are cleaned to absolute paths and ordered lexically. A missing
source refuses with `input.not-found`; an unreadable, wrong-type, or symlink
source is an input error. An empty source universe reaches graph validation and
fails because it has no `Environment`.

## Environment selection

Discovery establishes the input universe. The selecting `Environment` then
sets the complete desired-state scope:

1. Scan discovered YAML streams for exactly one structurally decoded
   `Environment`; always select its declaring file. Validate its authored kind
   defaults and apply `defaults.Environment` once before checking its required
   spec fields and selections. Envelope identity fields cannot be defaulted.
2. Apply its resulting `resources` list, or select all discovered files when omitted.
   Every resource must be covered by an acquired file/directory source or
   selected context input; authored YAML never widens filesystem authority.
   [Environment selection](environment.md#resource-and-cluster-selection)
   owns path rules, the marker-proven add-on snapshot exception, and cluster
   root closure.
3. Strictly decode selected files in lexical path/document order and follow
   [the compilation phases](../api.md#yaml-streams-and-decoding). Resolve references only
   inside the retained graph after cluster selection and normalization.

An authored empty resource or cluster-selection list is invalid. It is
reported once, at its field, and selection otherwise proceeds as if it were
omitted. A refused resource or cluster-selection entry is reported at its own
index. Operations all consume the same complete selected state; selection is
not a partial operation flag. `validate` warns deterministically for excluded files declaring
Bootwright objects and for excluded cluster roots. `render effective` has no
success-warning channel.

Non-YAML files are not desired-state documents. The five payload basenames
above are exact, case-sensitive path-segment rules. A corresponding co-located
Secret, playbook or add-on field must use its schema's reserved segment, and
`resources` cannot select that root or a descendant. A lowercase YAML file
elsewhere under an input root remains a desired-state candidate even if an
object also names it as payload. The [compiler boundary](../api.md#compiler-boundary)
forbids following payload references.

## Fixed input ceilings

Input ceilings are inclusive. Diagnostic storage reserves one of its 1,000
slots for the `input.limit` sentinel.

| Resource | Ceiling |
| --- | --- |
| Descendant path depth | 32 segments below the input root |
| Filesystem entries enumerated | 65,536 across all visited directories |
| YAML-suffix candidate paths | 4,096 |
| Native add-on marker candidate paths | 4,096 |
| One native add-on marker | 64 bytes |
| All native add-on markers | 262,144 bytes (256 KiB) |
| One YAML file | 2,097,152 bytes (2 MiB) |
| All discovered YAML files | 33,554,432 bytes (32 MiB) |
| YAML stream documents | 256 per file and 8,192 in aggregate |
| YAML representation depth | 64 parent-to-child edges below the document node |
| YAML representation nodes | 100,000 per document and 1,000,000 in aggregate |
| Returned diagnostics | 1,000 total: at most 999 ordinary diagnostics plus one reserved limit diagnostic |

The root has depth zero; each descendant segment adds one. Count each visited
directory entry before type, name, suffix, link or skip checks; skipped
subtree descendants are not enumerated or counted. Read each directory up to
the remaining aggregate budget plus one and sort admitted names before descent.
Process directories and paths lexically.

Count lowercase YAML-suffix non-directory candidates before link/type checks,
and admitted regular YAML files before Environment selection. Count exact bytes
from verified opened handles, checking per-file size before proportional
buffer/parse allocation. Each admitted file counts once across Environment
scanning and selected-state decoding, even if later excluded.

Count marker candidates at the Environment schema's exact sibling
position/basename before type, link, stability, size or grammar checks. Process
candidates lexically; check per-marker bytes before grammar and aggregate bytes,
in table order. Read at most the remaining applicable budget plus one byte;
never allocate from an untrusted declared size.

Count every composed stream document, including empty ones, before schema
decoding. Its document node has depth zero; every child edge adds one. Count
document, mapping, sequence, scalar and alias nodes once during a bounded
traversal, even if grammar later rejects them. Traverse mapping keys and
values; key use, tags and anchors add no count. Never traverse or expand alias
targets. Per-file/document counters reset only at their named boundary;
aggregate counters never reset. Scanning and decoding share counts and reuse
the admitted representation instead of parsing it again.

Crossing a filesystem or byte ceiling immediately stops the current read and
all further admission. A document, depth or node violation is detected at the
per-document boundary below and stops further admission, including other
streams. Return no state and one `input.limit` error naming the resource and
ceiling; prior diagnostics may remain. No schema decoding of the rejected
document, normalization, publication or effects follow. The first failure in
processing order wins; simultaneous checks use table order.

Deduplicate diagnostics before counting. Retain at most the first 999 distinct
ordinary diagnostics in processing order. At the 1,000th, stop validation and
fill the reserved slot with one source-free `input.limit`, then sort all 1,000
by CLI diagnostic order. Every limit failure makes the command-specific result
`null`; partial file/object counts are not completion evidence.

## Parser boundary

Read and verify the source bytes within the per-file and aggregate byte
ceilings before passing them to the parser. The selected stable YAML v3
decoder composes one complete document at a time. Check the returned document
count, depth and node counts before envelope or schema decoding, Environment
selection, normalization or expansion. Structural ceilings bound the admitted
representation, not allocations performed inside that one parser call.

At a document-count ceiling, one additional document may be composed to
distinguish end-of-stream from an over-limit stream. Discard that document
without schema decoding and return `input.limit`; do not add it to the
retained trees. A parser syntax failure before it returns a document
takes precedence over structural limits that could only be checked on the
unavailable representation. Prior verified byte-limit failures always precede
parsing. A stream carries no version directive or a `%YAML 1.1`
directive; a `%YAML 1.2` directive is rejected. Bootwright applies the
[strict scalar rules](../api.md#yaml-streams-and-decoding) in both
directive-free and accepted-directive streams.

A syntax failure names the parser's reason and line, and is reported
separately from invalid UTF-8, which names the line and column of the first
invalid byte. An `input.limit` message names its resource and inclusive
ceiling. Syntax failure metadata is bounded to one record per acquired file.
Apply the returned-diagnostic ceiling after resource selection; syntax errors
from excluded files do not consume that allowance. Structural limits remain
global.
For open native maps, diagnostics name the containing typed field and retain
source coordinates without repeating arbitrary native keys as field paths.

Parse each source stream once. Retain only admitted trees within the aggregate
node ceiling and reuse them for Environment scanning and selected decoding.
Cancellation is cooperative: check the supplied context around each decode,
in the context-aware reader, during node traversal and between later phases.
A decoder already processing buffered bytes is not forcibly interrupted.
Do not abandon parser goroutines or claim an in-process hard time or memory
limit. The isolated resource-qualification gate is recorded in
[milestones](../milestones/delivered.md#m1b--durable-contexts-and-desired-state-admission).

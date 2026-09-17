# A listed operation entry that will not resolve

Observed across three real applies on the lab host between 2026-09-15 and
2026-09-17. [Contexts](../../specs/contexts.md) owns the required behavior; this
page records why the operation store confirms an entry before refusing it.

## Symptom

A fresh `apply` fails its first block and reports:

```
[FAIL] context.state: lifecycle operation entry is not this store's own:
  .../blocks/controller-prerequisites/attempt-000001.json
```

The named entry is afterwards a perfectly ordinary file — regular, `root:root`,
`0600`, one link, one filesystem — and repeating the verb passes the same block
untouched. The block's own failure reason is never printed, because the refusal
replaces the read that would have reported it.

## Why the walk races a writer

`operationArea.capacity` measures the whole operations subtree before every
lifecycle write and log append, while adapters stream output into that same
subtree and `Replace` publishes records into it. `Replace` stages
`pending-<candidate>.json` beside its target and renames it over the target, so
`scan` lists a directory and then opens each name separately against a tree a
legitimate writer is moving names within. A name it just listed is routinely
gone, or mid-rename, by the time it opens it.

## What made it three separate diagnoses

`unsafeEntry` named the entry but never the answer that refused it, so each
occurrence was diagnosed by inference from git history rather than from the
message, and each fix addressed one guessed answer:

- `5abee867` — an entry that VANISHES mid-measure answers `ENOENT`; skip it.
- `eb4ce1d7` — assumed `EAGAIN` from `openat2` under `RESOLVE_BENEATH` and added
  `retryResolution` (16 immediate attempts) in `files_linux_amd64.go`. A probe
  that renamed onto a target continuously while four goroutines resolved it
  produced **2,013,036 successful resolutions and zero EAGAIN**, so this site
  does not appear to answer `EAGAIN` at all; the retry is harmless but was not
  the fix.
- The third occurrence is the one this page answers, and its answer was lost
  with the process.

## The answer the named cause gave

Once `unsafeEntry` carried its cause, the next occurrence reported one:

```
[FAIL] context.state: lifecycle operation entry is not this store's own:
  .../blocks/controller-prerequisites/attempt-000001.json: not a directory
```

`not a directory` is not a race. `Store.RecordPreparation` published the
before-state with `Replace` and then called `Area.Sync` on the same record path.
`Sync` makes a *directory* durable and resolves every component of its path as
one, so the record's own name was opened as a directory and answered `ENOTDIR`,
which reproduces on every confirmation and refuses the whole attempt. `Replace`
had already succeeded, so the record on disk was correct and ordinary, which is
why the entry always looked innocent afterwards.

This explains the shape the symptom always had. The controller stage is the one
stage that publishes a before-state, so only `controller-prerequisites` reached
the call, and only on an attempt whose `preparation` was still empty — a fresh
context's `attempt-000001.json`, every time. Nothing here was timing-dependent.

The fix removes the call: `Replace` writes and syncs the staged file, then syncs
the directory it renames the record into, so a record is already durable when
`Replace` returns. `Area.Sync` now documents that it names a directory and never
a record, the in-memory area used by the store's own tests refuses a record the
way `contextfs` does, and
`TestPublishingABeforeStateNeverSyncsTheRecord` and
`TestSyncingARecordPathIsRefusedAsANonDirectory` hold both halves.

Entry confirmation stays: it answers a genuine mid-rename race, and it is what
made this diagnosis possible by forcing the refusal to carry `not a directory`.
It was not itself the cure for this symptom. Whether the three earlier
occurrences were also this call cannot be proved from the evidence kept — their
refusals named no cause — but they shared its block, its attempt number and its
"fails once, looks ordinary afterwards" shape.

## The rule

A refusal is evidence about an entry only once it reproduces. `resolveEntry` and
`confirmDirectory` (`operations_linux_amd64.go`) re-read an entry that will not
resolve, `entryConfirmations` times with a doubling millisecond pause, and call
it foreign only when the answer survives. `ENOENT` still skips at once, and a
genuinely foreign entry — a directory whose mode, owner or device is not this
store's, a name this area does not create, an entry that is neither file nor
directory — still refuses every later write, which
`TestAForeignOperationEntryIsRefusedByName` holds.

`unsafeEntry` now appends the cause. A refusal this area raised is a
`diagnostics.Failure`, whose `Error` is the fixed string `failed with
diagnostics`, so `causeText` renders the diagnostic's message instead; a kernel
answer renders itself.

**How to apply:** never report the first answer from a walk over a tree a
concurrent writer publishes into, and never raise a refusal that discards what
refused it. When a store refusal names a path, it must also name the reason, or
the next occurrence is diagnosed by inference again — three fixes here were
built on an inferred race, and the first message that carried its cause settled
the question in one run. A test double for a filesystem capability enforces the
shape of the path it is given, or it hides exactly this defect: the store's own
tests exercised the bad call for weeks and passed. Relates to
[controller-setup-resolution-cost.md](controller-setup-resolution-cost.md) for
what else measures this subtree.

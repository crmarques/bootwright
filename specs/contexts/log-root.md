# Log root

Troubleshooting output lives under the log root, `/var/log/bootwright`, and
never in the store: each operation's private log, each attempt's and
resolution's log and retained adapter output, each bounded run's output and
each setup's output. Nothing there is evidence, ownership or a continuation
cursor, nothing reads it back for a decision, and losing it changes no outcome.
Isolated test storage injects its own log root at composition, beside its test
state root. The [store](../contexts.md#storage-locking-and-publication) owns
everything else Bootwright keeps for a context.

## Layout

```text
/var/log/bootwright/
  controller/install-NNNNNN.output
  contexts/<name>/
    current -> operations/<operation-id>
    operations/<operation-id>/operation.jsonl
    operations/<operation-id>/blocks/<block-id>/attempt-NNNNNN.jsonl
    operations/<operation-id>/blocks/<block-id>/attempt-NNNNNN.output
    operations/<operation-id>/blocks/<block-id>/attempt-NNNNNN-resolution-NNNNNN.jsonl
    operations/<operation-id>/blocks/<block-id>/attempt-NNNNNN-resolution-NNNNNN.output
    runs/run-NNNNNN.output
```

## Modes and creation

Every directory is a `root:root` `0700` directory and every file a `root:root`
`0600` file. The store reaches the root only through directories, never
following a symbolic link, and every object beneath it by a descent from a held
handle that follows none, on the root's own device; the root itself may be a
filesystem of its own. A missing directory is created private when a write
first needs it, and a directory a concurrent writer created first is opened as
if it had existed. An existing root or directory beneath it that is a link, of
another owner or mode, or of another type refuses `context.state` naming its
path and saying that nothing repairs it, and nothing is chmodded or chowned
(`TestTheLogRootIsCreatedPrivateAndRefusesALinkOrForeignMode`). The log root
needs no persistent filesystem: on tmpfs a reboot loses only logs.

## Writers, readers and bounds

A context's `operations/` is written only inside its lifecycle transaction,
under the context's exclusive lease; its `runs/` by its bounded runs under the
shared lease, each holding its own file under an advisory lock; and `controller/`
under the store's `controller/install/lock`. A reader of a context's logs holds only the shared root
lock, released before it reads a byte of any file it opened, and takes no
lease; a reader of `controller/` takes no root lock; a directory not yet created
lists nothing. Each poll of a follow re-reads the record and re-lists the
directory only of a block that was not `done` at its previous poll, so its hold
shrinks as blocks settle, and a mutator's registry transaction waits that hold
out ([lock rules](../contexts.md#storage-locking-and-publication)). A reader
opens each file through the same no-follow descent, refusing a symbolic link
anywhere on it (`TestLogOpenRefusesASymlinkedDirectoryInItsDescent`), and
refuses a file that is not a regular, singly linked file of the directory's
owner that no group or other may read (`context.state`, naming the file,
`TestLogOpenRefusesALinkASecondLinkAGroupReadableFileOrANonRegularOne`).
`operations/` is the context's operation log area, an `operationstore.Area` that the shared area
contract holds, as it holds `runs/` (`TestTheLogAreasHonourTheAreaContract`).
Each JSONL log keeps its own bound, the attempt log's in the [bounds
table](../contexts.md#storage-locking-and-publication) and 8 MiB for an
operation log, and an attempt's retained adapter output is cut short once the
context's operation log area holds 256 MiB, measured once per transaction and
then counted as outputs write. That bound never cuts a JSONL log, so capacity
alone never faults one (`TestAttemptOutputIsCutAtTheContextLogBound`); a log
write the filesystem refuses, a full one included, is the [log
fault](../state-reconciliation.md#operation-records) any log write is, and a
refused output write only cuts the output short.

## Retention and the current link

Each registration, once the index names the new operation, removes the oldest
operation log directories, by modification time and then name, other than those
of the operation the index named before and of the one registering, until
fewer than 64 remain and they hold at most 128 MiB. It removes only private entries
it listed and verified, unlinks a link as a link and never follows one, and
leaves a directory that holds anything else
(`TestOperationLogRetentionKeepsCurrentWithinCountAndBytes`). The registration
then renames a staged link over `current`, naming the newest registered
operation's directory for a human to follow; an entry there that is no link is
left. Retention and the link are best effort: neither changes a registration's
outcome or faults a log, and the store never resolves a path through `current`
(`TestTheCurrentLinkNamesTheNewestRegistrationAndIsNeverFollowed`).

## Bounded runs

A bounded run's output is `runs/run-NNNNNN.output`, numbered one past the
highest the directory lists, so concurrent runs take distinct, sequential
numbers (`TestConcurrentBoundedRunsTakeDistinctSequentialNumbers`). Before it
creates its file, a run retires the lowest-numbered runs no live run holds
until fewer than the [bounds
table](../contexts.md#storage-locking-and-publication)'s runs remain, each only
while it is a private, singly linked regular file, and an entry not named as a
run is neither counted nor removed (`TestABoundedRunKeepsTheNewestRuns`,
`TestARetiredRunIsRemovedOnlyAsTheStoreLeftIt`,
`TestBoundedRunsKeepWorkingHoweverManyRan`). It creates its file exclusively
without following a link, trying the next number at most 16 times when another
run took one, holds it shared until its callback returns, so no retention
removes a run in progress (`TestARunInProgressIsNeverRetired`), proves the name
still names that file, and makes it durable before a result names it. A run
that cannot hold the file it created removes it
(`TestARunThatCannotOpenLeavesNoFile`). A directory whose next number would be
past `999999` refuses `lifecycle.state`, naming the `runs/` directory to remove
(`TestBoundedRunNumbersRefuseOnceExhausted`).

## Setup output

A setup's output is `controller/install-NNNNNN.output`, its number the setup's ID for a
setup that starts afresh; a resumed setup writes its output under a new number
and keeps its original ID.
The next is numbered past both the newest output and every `install-NNNNNN` the
setup record's receipt or pending intent names, so retention never lends a
later setup an ID a receipt attributes
(`TestSetupOutputNumbersPastTheReceiptsIDs`), and the oldest outputs beyond the
[bounds table](../contexts.md#storage-locking-and-publication)'s setup outputs
are removed before it is created. A name a setup output takes that is no
private regular file within its bound refuses setup; every other name is
ignored.

## Lifetime and naming

On a host that holds no context, `controller uninstall` removes the log root
whole, under [the uninstall flow](../controller.md#the-uninstall-flow); no
other command removes the root itself.

`context delete --purge` removes the context's directory under the log root as
the last filesystem step before it drops the registry entry, and a repeated
deletion finds it gone. A directory holding a mount, an entry of another owner
or anything else this store never creates refuses `context.state` naming that
entry, and links are removed as links
(`TestDeletionRemovesTheContextLogDirectory`). `context init` removes a
directory an earlier context of the same name left there, under the exclusive
root lock, before the context is ready
(`TestInitRemovesALeftoverLogDirectory`). A structured result names a log
relative to the log root, such as
`contexts/<name>/operations/<operation-id>/operation.jsonl` or
`contexts/<name>/runs/run-000003.output`, as the output contract's [private
operation logs](../cli/output.md#private-operation-logs), [bounded run
output](../cli/output.md#bounded-run-output) and [setup run
output](../cli/output.md#setup-run-output) present them.

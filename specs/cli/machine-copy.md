# Machine file copies

`machine cp --name <list> local:<path> remote:<path>` uploads one file or
recursive directory to every selected Machine; reversing the operands downloads
it. Exactly one endpoint is local. Names follow the list rules and explicit
access resolves the same account, credential and host-key proof as
[SSH sessions](../cli.md#machine-ssh-sessions). Copy changes no desired state, ownership,
lifecycle operation or continuation. An upload may change files managed by a
later apply; it grants no lifecycle ownership of those changes.

## Operands and paths

Local paths resolve from the invoking directory and are read and written under
the verified invoking account, including when context access elevates. Remote
relative paths resolve from the SSH login directory. Paths are literal UTF-8,
at most 4096 bytes, with no shell, wildcard, environment or tilde expansion.
Bootwright flags precede the two operands. One source object is copied; a
trailing source slash never changes to contents-only semantics. Source root
`/`, links, special files and source filesystem crossings refuse. A tree has
at most 8192 entries, depth 64 and an 8 MiB metadata frame; payloads stream
through bounded buffers.

## Destination rules

An existing destination directory receives the source basename. An absent
destination names the new object. A trailing destination slash requires a
container directory, created if its parent exists. Several-Machine downloads
require a local directory (created if absent) and always save the source as
`<local>/<machine>/<source-basename>`; the wrapper appears only for several
normalized names. Missing unrelated parents and file/directory type conflicts
refuse. Existing directories merge; existing regular files require
`--overwrite`. Destination-only entries remain. Several upload selections
sharing one SSH host-key identity and login account refuse to avoid
concurrent writes to one destination.

## Worker and access

The fixed file worker runs as `/usr/bin/python3 -I -B -c` over the pinned SSH
client, and locally through a qualified interpreter under the invoking account.
Both endpoints require Linux with Python 3.9 or newer and procfs; no remote sudo occurs.
`--ssh-user` and `--ssh-id-file` follow the session credential rules. Password
authentication is available only for one Machine at an interactive terminal in
text mode; several or structured transfers never prompt for credentials.
An explicitly offered key is the sole authentication method for a
password-configured target in a batch.

Before payload writes, every Machine's identity and host key is proved, then
sources and destinations are preflighted. Preflight workers exit; execution
reopens and revalidates its source manifest and destination interpretation.
At most the [running-block bound](../state-reconciliation.md#stages-and-the-pause-boundary)
transfers run together. Source changes detected before or during a file read
refuse publication; this is no snapshot of a changing tree. Files copy their
ordinary permission bits, stripping special bits; ownership belongs to the
receiving account. ACLs, extended attributes, labels, timestamps, hard-link
relationships and sparse allocation are not preserved. Directory permissions
are applied only to directories this invocation created.

## Safety and failure

[Copy filesystem safety](../security.md#explicit-file-copy-safety) owns staging,
publication and cleanup. Each file publishes independently after a complete,
stable source read. No cross-file or cross-Machine rollback is promised.
Failures on one Machine leave the others running; cancellation prevents queued
transfers from starting. Completed files and directories remain after failure
or interruption. A disconnect during publication reports an unknown result;
inspect that destination before retrying. Cleanup removes only owned staging;
an uncatchable termination may leave a private staging directory.

## Refusals

| Condition | Diagnostic, exit | Row status | Phase |
| --- | --- | --- | --- |
| `--ssh-ask-password`, no name, or operands that are not one `local:` and one `remote:` path | `cli.usage`, `2` | no row | before any resolution |
| A Machine that needs interactive password authentication in a batch with no offered key | `access.unavailable`, `1` | no row | after identity proof, before any worker |
| Several uploads sharing one SSH host-key identity and login account | `access.target`, `1` | no row | before any worker |
| A source root, link, special file, filesystem crossing or tree bound, an invalid path, a missing parent, a type conflict, or an existing file without `--overwrite` | `access.copy`, `1`, naming the worker's reason, such as `source-root` or `destination-exists` | no row | preflight, before any payload write |
| A source change, or any other failure of a transfer | `access.copy`, `1` | `failed` | execution |
| A disconnect during publication | `access.copy`, `1` | `unknown` | execution |
| An interrupt | `130` | `canceled` for a started transfer, `skipped` for a queued one | execution |

`TestCopyPreflightsEveryMachineBeforeAnyPayloadWrite`,
`TestCopyConcurrentFailuresKeepEveryMachineOutcome`,
`TestCopyBatchPasswordRefusalNamesTheCopyCommand` and
`TestCopyCancellationStopsQueuedTransfersAndRetainsRows` hold the table.

## Output

Copy owns its output and status, unlike a live session. Human text reports
Machine, destination, status, completed file count and bytes. JSON and YAML
return one result document, with no payload bytes or raw worker output.
Rows are in canonical Machine order and report `copied`, `failed`, `unknown`,
`canceled` or `skipped`. Counts include only confirmed file publications.
Exit status is 0 for complete success, 1 for operational failure, 2 for usage
and 130 for an interrupt; an executed partial result survives interruption.

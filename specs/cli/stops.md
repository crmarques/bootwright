# Stop catalog

Read with [CLI output](output.md#diagnostic-taxonomy-and-order) and
[attempt outcomes](../state-reconciliation.md#attempt-outcomes).

A stop is a declared way an `apply` or `destroy` ends without completing its
verb: a refusal, a failed effect, an unproved outcome, a pause or an
interruption. Each is declared once, beside the code that raises it, as a
`diagnostics.Stop`, and is one row of this catalog. Its code is the
diagnostic's `code`; this catalog registers it, and the
[diagnostic registry](output.md#diagnostic-taxonomy-and-order) does not list it
again. A firing's message is composed where it is raised; its remedy defaults
to the row's when the raising code has none. Its certainty decides the
attempt's outcome: `unproved` leaves it `unknown`, every other certainty
`failed`. Codes not yet moved onto a declared stop remain in the registry, and
`TestEveryLifecycleDiagnosticIsBuiltFromADeclaredStop` counts them in a ledger
that only shrinks.

## Stop vocabulary

| Field | Values |
| --- | --- |
| Level | `context`, `operation`, `removal-phase`, `block`, `attempt`, `resolution`, `adapter-run` |
| Certainty | `none`: nothing began; `possible`: an effect may be partial and a repeat converges it; `unproved`: the effect must be observed before anything else starts; `owed`: the effect is complete and its bookkeeping is not |
| Cause | `input`, `environment`, `target`, `concurrency`, `build`, `records`, `bound`, `interrupt`, `deadline`, `defect`, `unclassified` |
| Fault | for an `environment` cause: `dns`, `unreachable`, `timeout`, `certificate`, `credential`, `proxy`, `storage`, `host-key` |
| Recovery | `repeat`, `fix-then-repeat`, `change-input`, `other-verb`, `wait`, `authorize`, `registering-build`, `abandon-context`, `none` |

`unclassified` is the cause of a stop the code that raises it cannot yet
attribute, such as a role task that failed without naming a declared reason.

## Catalog

### Adapter run stops

The lifecycle runner's stops of one adapter run
(`internal/reconciliation/ansiblerunner`): the refusals that let no adapter
start, and the endings of one that ran.

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `runner.result-channel` | adapter-run | none | environment | - | repeat | the result channel the adapter reports on could not be opened, so no adapter ran | repeat the operation |
| `runner.authorization-channel` | adapter-run | none | environment | - | repeat | the channel the runner authorizes effects on could not be opened, so no adapter ran | repeat the operation |
| `runner.not-started` | adapter-run | none | build | - | fix-then-repeat | the qualified adapter process could not start | run bootwright controller install |
| `runner.channel-retained` | adapter-run | unproved | defect | - | repeat | processes the adapter left behind still held its result channel after it completed | repeat the operation, which observes the effect before anything else starts |
| `runner.output-retained` | adapter-run | unproved | defect | - | repeat | processes the adapter left behind still held its output after it exited | repeat the operation, which observes the effect before anything else starts |
| `runner.exited-before-load` | adapter-run | none | unclassified | - | fix-then-repeat | the adapter exited before it loaded its request, so it performed no effect | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.task-failed` | adapter-run | possible | unclassified | - | fix-then-repeat | a task of the adapter's role failed and no reason the run declares named the failure | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.host-unreachable` | adapter-run | possible | environment | `unreachable` | fix-then-repeat | a host the adapter's role works on did not answer | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.exited` | adapter-run | possible | unclassified | - | fix-then-repeat | the adapter exited with a failure after it loaded its request and named no task | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.result-incomplete` | adapter-run | unproved | defect | - | repeat | a record the adapter wrote broke the result protocol's shape | repeat the operation, which observes the effect before anything else starts |
| `runner.protocol-invalid` | adapter-run | unproved | defect | - | repeat | the adapter wrote a record where the result protocol does not admit it | repeat the operation, which observes the effect before anything else starts |
| `runner.authorization-uncertain` | adapter-run | unproved | environment | - | repeat | the runner could not tell whether the adapter received its authorization | repeat the operation, which observes the effect before anything else starts |
| `runner.deadline` | adapter-run | unproved | deadline | - | repeat | the adapter run reached its deadline before it ended | repeat the operation, which observes the effect before anything else starts |
| `runner.no-result` | adapter-run | unproved | defect | - | repeat | the adapter ended without a complete result | repeat the operation, which observes the effect before anything else starts |
| `runner.request-invalid` | adapter-run | none | defect | - | none | the frozen adapter request names something the runner refuses to run, so no adapter ran | report the message; plan the operation again with this build |
| `runner.host-key-unbound` | adapter-run | none | input | - | change-input | an SSH placement names no host key its own access binds, so no adapter ran | bind the host's key through its access.ssh.knownHostsRef, then plan the operation again |
| `runner.boundary-unavailable` | adapter-run | none | build | - | fix-then-repeat | the authorized adapter execution boundary is not installed | run bootwright controller install |
| `runner.unsupported-platform` | adapter-run | none | build | - | none | lifecycle adapters run only on Linux on amd64 | run Bootwright on Linux on amd64 |
| `runner.storage` | adapter-run | none | environment | `storage` | fix-then-repeat | the runner's private storage for an adapter run could not be prepared or proved, so no adapter ran | restore the private directories under /var/lib/bootwright and the run scratch parent, then repeat the operation |
| `runner.scratch-unusable` | adapter-run | none | environment | `storage` | fix-then-repeat | the run scratch directory is not one an adapter may use, so no adapter ran | give the run scratch parent a filesystem that allows execution and is owned by the invoking user alone, then repeat the operation |
| `runner.stale-run` | adapter-run | none | environment | `storage` | fix-then-repeat | an earlier adapter run left a directory the runner could not remove safely | remove the stale bootwright-run directories no process holds, then repeat the operation |
| `runner.adapter-running` | adapter-run | none | concurrency | - | wait | another lifecycle adapter, or processes it left, still holds the job lock this run needs | wait for the adapter the message names to end and repeat the command |
| `runner.material-unprepared` | adapter-run | none | records | - | fix-then-repeat | the bound Secret material the run lends could not be prepared, so no adapter ran | restore the context's keyring, then repeat the operation |
| `runner.invocation-ended` | adapter-run | none | defect | - | repeat | a run was asked of an invocation that had already ended, so no adapter ran | repeat the operation |
| `runner.output-invalid` | adapter-run | possible | defect | - | repeat | a file the adapter was to produce is not the private regular file the run expects | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.preparation-unrecorded` | adapter-run | unproved | records | - | repeat | the before-state the adapter reported could not be recorded before its effect | repeat the operation, which observes the effect before anything else starts |

### Engine stops

The lifecycle engine's stops (`internal/reconciliation/lifecycle`): a block
that stopped on its host or on a defect, an attempt that recorded no start, a
removal that would take back state in use, and the end of an operation that
did not complete, which reads as the consequence of the causes before it.

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `engine.records-full` | block | possible | environment | `storage` | fix-then-repeat | a block stopped because a filesystem holding the context's records or logs had no room | free space, or raise the quota, on the filesystems that hold /var/lib/bootwright and /var/log/bootwright, then repeat the operation |
| `engine.records-read-only` | block | possible | environment | `storage` | fix-then-repeat | a block stopped because a filesystem holding the context's records or logs is read-only | make the filesystems that hold /var/lib/bootwright and /var/log/bootwright writable, then repeat the operation |
| `engine.records-denied` | block | possible | environment | `storage` | fix-then-repeat | a block stopped because a record or log of the context could not be accessed | run the command as the invoking user through its elevation, and check the ownership of the filesystems that hold /var/lib/bootwright and /var/log/bootwright, then repeat the operation |
| `engine.records-device` | block | possible | environment | `storage` | fix-then-repeat | a block stopped because a record or log write met a device error | inspect the filesystems that hold /var/lib/bootwright and /var/log/bootwright (the kernel log names the device), then repeat the operation |
| `engine.escaped` | block | possible | defect | - | repeat | a block stopped on a failure that escaped a typed boundary; its own words are never shown, since they may name a private path | repeat the operation; report the failure if it stops the same way |
| `engine.start-unrecorded` | attempt | none | records | - | repeat | an attempt could not record its start, so it performed no effect | repeat the operation |
| `apply.unresolved` | operation | unproved | unclassified | - | repeat | the apply ended with an effect whose outcome is unproved | repeat the apply, which observes that effect before anything else starts |
| `destroy.unresolved` | operation | unproved | unclassified | - | repeat | the destroy ended with an effect whose outcome is unproved | repeat the destroy, which observes that effect before anything else starts |
| `apply.incomplete` | operation | possible | unclassified | - | repeat | the apply ended with blocks not done; the diagnostics before it say why | continue the apply once each cause is fixed |
| `destroy.incomplete` | operation | possible | unclassified | - | repeat | the destroy ended with blocks not proved gone; the diagnostics before it say why | repeat the destroy once each cause is fixed |
| `destroy.in-use` | removal-phase | none | target | - | fix-then-repeat | a removal would take back state that is still in use, so it takes back nothing at that boundary | stop what the message names, then repeat the destroy |
| `destroy.consumer-survives` | removal-phase | none | target | - | fix-then-repeat | a removal would retire what a consumer that outlives it still uses | keep this context while the consumer runs; a destroy takes back what a consumer uses only when the consumer leaves with owned virtual node Machines the same destroy deletes |

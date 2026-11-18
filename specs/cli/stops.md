# Stop catalog

Read with [CLI output](output.md#diagnostic-taxonomy-and-order) and
[attempt outcomes](../state-reconciliation.md#attempt-outcomes).

A stop is a declared way an `apply` or `destroy` ends without completing its
verb: a refusal, a failed effect, an unproved outcome, a pause or an
interruption. Each is declared once, beside the code that raises it, as a
`diagnostics.Stop`, and is one row of this catalog. Its code is the
diagnostic's `code`; this catalog registers it, and the
[diagnostic registry](output.md#diagnostic-taxonomy-and-order) does not list it
again. [`bootwright explain`](commands.md#command-and-flag-catalog) prints
the catalog, or one row with `--code`. A firing's message is composed where it is raised; its remedy defaults
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
removal that would take back state in use, the end of an operation that did
not complete, which reads as the consequence of the causes before it, and the
refusals of a decision and its plan before any effect.

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
| `engine.finalization-due` | operation | owed | defect | - | repeat | an operation is due its finalization, which no decision may skip | repeat the command, which finalizes that operation before it decides |
| `engine.records-contradict` | operation | none | records | - | fix-then-repeat | an incomplete operation holds records that contradict what it started | read the records with bootwright status, then restore them from a backup or delete the context |
| `engine.other-verb-incomplete` | operation | none | input | - | other-verb | an incomplete operation of the other verb must be continued before another operation | continue or replace the incomplete operation the message names |
| `apply.records-missing` | operation | none | records | - | abandon-context | a completed apply records no completion for blocks it froze, so nothing proves its input applied | restore the context's records, or take back what it owns and apply again |
| `destroy.records-missing` | operation | none | records | - | abandon-context | a completed destroy records no completion for blocks it froze, so nothing proves their effects removed | restore the context's records, or delete the context with what it may still own |
| `apply.input-changed` | operation | none | input | - | change-input | the desired state changed after the apply completed, and it is no addition to it | take back what the context owns with destroy, then apply the changed input |
| `apply.not-an-addition` | operation | none | input | - | change-input | a changed input over a completed apply adds something an additive apply cannot take | take back what the context owns with destroy, then apply the changed input |
| `apply.prerequisite-lost` | operation | none | records | - | other-verb | a block an addition needs, held by an earlier apply of its lineage, is gone or no longer done | take back what the lineage owns with destroy |
| `apply.nothing-declared` | operation | none | input | - | change-input | the selected Environment declares nothing this executable would create | declare a managed infrastructure service |
| `apply.stage-empty` | operation | none | input | - | fix-then-repeat | the selected stages or the --until instance have nothing to start | repeat the apply with a wider selection |
| `apply.stage-excludes-retry` | operation | none | input | - | fix-then-repeat | the block the operation must retry first is outside the selected stages | repeat the apply with a selection that holds that block |
| `apply.until-unknown` | operation | none | input | - | fix-then-repeat | --until names no add-on instance of the plan | name an add-on instance the plan holds, or repeat without --until |
| `plan.input-uncompilable` | context | none | input | - | change-input | the context's frozen desired state could not be compiled | repair the input with validate and import it again |
| `plan.capability-missing` | context | none | build | - | none | no lifecycle capability of this build plans the selected state | use a build that carries the capability; report it if this build should |
| `plan.capability-defect` | context | none | defect | - | none | a capability planned a block it may not plan | report the message as a defect |
| `plan.host-key-unbound` | context | none | input | - | change-input | an SSH placement binds no host key, or reaches the controller where its pinned key does not apply | author access.ssh.knownHostsRef on the Machine the message names |
| `plan.environment-invalid` | context | none | input | - | change-input | the selected state holds no single Environment naming its controller Machine | repair the input with validate |

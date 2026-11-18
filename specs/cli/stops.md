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

#### Execution and removal

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
| `apply.produced-input-missing` | block | none | records | - | fix-then-repeat | a block borrows an output its producer has not proved, or no longer offers | complete the producer block, then repeat the operation |
| `engine.recapture-mismatch` | block | possible | target | - | fix-then-repeat | an output a block recaptured presents another identity than the one custody holds | find what replaced the effect the block proved; the context's records name the original |
| `engine.resolved-unperformed` | resolution | none | target | - | repeat | an observation proved a frozen effect never performed, or left part done, so the block is failed and retried | repeat the operation, which retries the block |
| `destroy.postcondition-unknown` | block | none | target | - | fix-then-repeat | a retried removal read its target again and still could not tell its postcondition, so it performed no effect | restore the target the removal reads, then repeat the destroy |
| `engine.finalization-incomplete` | operation | owed | records | - | repeat | an operation's effects are complete and its finalization is not | repeat the command, which finishes the finalization |
| `destroy.release-incomplete` | operation | owed | records | - | repeat | the removal completed, and releasing what it owned did not | repeat the destroy to finish it |
| `destroy.unproved-effects` | operation | unproved | target | - | fix-then-repeat | a removal cannot prove what some effects left behind, so it registered nothing | do what each block's diagnostic reports, then repeat the destroy |
| `engine.outcome-unknown` | resolution | unproved | target | - | fix-then-repeat | a block's outcome is still unknown after an observation, for the reason the message names | do what the reason names, then repeat the command to observe the block again |

#### Decisions

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
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
| `engine.unindexed-records` | context | none | records | - | abandon-context | the context holds operation records or evidence its index names no operation for | restore the context's records, or delete the context with what it may still own |
| `engine.request-defect` | operation | none | defect | - | none | a capability declared or requested something the engine refuses to compose | report the message as a defect |
| `engine.other-build` | operation | none | build | - | registering-build | this executable cannot prove it would continue or remove the operation as the build that registered it | repeat the command with the build that registered the operation |
| `engine.plan-moved` | operation | none | concurrency | - | repeat | the context changed between the plan and the transition, so nothing was registered | repeat the command to plan from what the context holds now |
| `engine.input-moved` | operation | none | input | - | fix-then-repeat | the context's input is no longer the revision the operation froze | restore the exact input revision the operation froze |
| `engine.binding-lost` | operation | none | records | - | abandon-context | a Secret binding the operation froze is lost, so its material cannot be lent again | restore the context's keyring from a complete backup, or delete the context and remove what it named by hand |
| `destroy.stage-selected` | operation | none | input | - | fix-then-repeat | the next operation is a destroy, which accepts no stage selection | repeat the command without --stage and --until |
| `apply.until-before-preparation` | operation | none | input | - | other-verb | the add-on boundary --until names cannot be planned before preparation completes | run apply to complete preparation, then select --until |
| `engine.records-damaged` | operation | none | records | - | abandon-context | the context's operation records do not form the lineage or removal the operation needs | restore the context store from a backup, or delete the context with what it may still own |
| `engine.borrowed-ssh` | operation | none | input | - | fix-then-repeat | a lifecycle operation was given borrowed SSH credentials, which it never uses | remove the --ssh flags and author each Machine's own access |
| `engine.confirmation-required` | operation | none | input | - | authorize | the operation needs a confirmation the invocation cannot ask for | repeat the command on a terminal, or with --yes once the plan is reviewed |
| `engine.input-invalid` | operation | none | input | - | fix-then-repeat | an invocation option holds a value the engine refuses | repeat the command with the value the message names |
| `engine.credential-moved` | operation | none | concurrency | - | repeat | a cluster administrator credential changed while its update was decided or pending | repeat the credential update against the current context |

#### Plans, preparations and authorizations

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `plan.input-uncompilable` | context | none | input | - | change-input | the context's frozen desired state could not be compiled | repair the input with validate and import it again |
| `plan.capability-missing` | context | none | build | - | none | no lifecycle capability of this build plans the selected state | use a build that carries the capability; report it if this build should |
| `plan.capability-defect` | context | none | defect | - | none | a capability planned a block it may not plan | report the message as a defect |
| `plan.host-key-unbound` | context | none | input | - | change-input | an SSH placement binds no host key, or reaches the controller where its pinned key does not apply | author access.ssh.knownHostsRef on the Machine the message names |
| `plan.environment-invalid` | context | none | input | - | change-input | the selected state holds no single Environment naming its controller Machine | repair the input with validate |
| `authorize.unneeded` | operation | none | input | - | fix-then-repeat | --authorize names a token the plan does not require | repeat the command without that --authorize |
| `authorize.missing` | operation | none | input | - | authorize | the plan has consequences no --authorize grants | review the plan, then repeat the command with the authorization it names |
| `authorize.stale` | operation | none | input | - | authorize | an --authorize digest names steps the plan no longer holds, or an unattended run binds none | review the plan, then repeat the command with the digest it prints |
| `authorize.rebind-invalid` | operation | none | input | - | fix-then-repeat | an --authorize rebind names a Secret the operation cannot rebind | repeat the command naming each rotated access credential once as --authorize rebind=<secret> |
| `prepare.refused` | operation | none | input | - | fix-then-repeat | a preparation cannot start or advance over the context's current operation or input | finish or take back the current operation, or restore the input the preparation recorded |
| `prepare.unsupported` | operation | none | build | - | none | this executable registers no preparation for the requested owner | use a build that registers the preparation |
| `authorize.relaxation-changed` | operation | none | input | - | fix-then-repeat | an operation continues only under the erase relaxations its plan froze, and the invocation names others | continue it naming the relaxations its plan froze, or replace it with a fresh destroy |
| `diff.context-moved` | context | none | concurrency | - | repeat | the context changed while diff read it | repeat the diff |

### Host firewall stops

The managed OS host firewall capability's stops (`internal/managedos/hostfirewall`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `firewall.request-invalid` | block | none | defect | - | none | the host firewall request a plan composed breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `firewall.unconfigured` | block | none | defect | - | none | the host firewall capability runs without its adapter | report the message as a defect |
| `firewall.records-missing` | block | none | records | - | abandon-context | the host firewall block names no apply its evidence came from | restore the context store from a backup |
| `firewall.evidence-mismatch` | block | possible | records | - | repeat | the host firewall evidence names another request or version than the block froze | repeat the operation, which observes the firewall again |
| `firewall.unproved` | block | unproved | target | - | repeat | the host firewall adapter ended without proving its postcondition | repeat the operation, which observes the firewall before anything else starts |

### Machine power stops

The Machine power and media eject capability's stops (`internal/machine/power`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `power.request-invalid` | adapter-run | none | input | - | fix-then-repeat | the power request names a verb or context the command does not take, so nothing was sent | repeat the command with a supported verb and context |
| `power.identity-changed` | adapter-run | none | target | - | fix-then-repeat | the management controller answers as another system than the one the context's apply proved, so no power request was sent | correct the Machine's BMC or provider, or take back and apply the context again |
| `power.evidence-mismatch` | adapter-run | possible | defect | - | repeat | the power or eject evidence names another request, Machine or survey, or a state the command cannot name | repeat the command; report the message if it stops the same way |
| `power.unproved` | adapter-run | unproved | target | - | repeat | the management controller did not prove the power or media state the command asked for | read the power state again with bootwright machine status, then repeat the command |

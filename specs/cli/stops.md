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

## Host firewall stops

The managed OS host firewall capability's stops (`internal/managedos/hostfirewall`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `firewall.request-invalid` | block | none | defect | - | none | the host firewall request a plan composed breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `firewall.unconfigured` | block | none | defect | - | none | the host firewall capability runs without its adapter | report the message as a defect |
| `firewall.records-missing` | block | none | records | - | abandon-context | the host firewall block names no apply its evidence came from | restore the context store from a backup |
| `firewall.evidence-mismatch` | block | possible | records | - | repeat | the host firewall evidence names another request or version than the block froze | repeat the operation, which observes the firewall again |
| `firewall.unproved` | block | unproved | target | - | repeat | the host firewall adapter ended without proving its postcondition | repeat the operation, which observes the firewall before anything else starts |

## Machine power stops

The Machine power and media eject capability's stops (`internal/machine/power`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `power.request-invalid` | adapter-run | none | input | - | fix-then-repeat | the power request names a verb or context the command does not take, so nothing was sent | repeat the command with a supported verb and context |
| `power.identity-changed` | adapter-run | none | target | - | fix-then-repeat | the management controller answers as another system than the one the context's apply proved, so no power request was sent | correct the Machine's BMC or provider, or take back and apply the context again |
| `power.evidence-mismatch` | adapter-run | possible | defect | - | repeat | the power or eject evidence names another request, Machine or survey, or a state the command cannot name | repeat the command; report the message if it stops the same way |
| `power.unproved` | adapter-run | unproved | target | - | repeat | the management controller did not prove the power or media state the command asked for | read the power state again with bootwright machine status, then repeat the command |

## Disk discovery stops

The managed OS disk discovery capability's stops (`internal/managedos/diskdiscovery`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `discovery.unconfigured` | block | none | defect | - | none | disk discovery runs without compiled state or one of the readers and sources it depends on | report the message as a defect |
| `discovery.request-invalid` | block | none | defect | - | none | the frozen disk discovery request names something its adapter cannot run, so no adapter ran | report the message as a defect |
| `discovery.unsupported` | context | none | input | - | change-input | the Machine or the artifact server it names cannot host a disk discovery boot | use a controller-local HTTPS artifact server and full or certificate BMC verification |
| `discovery.machine-installed` | context | none | target | - | change-input | the Machine already has installation ownership, so booting it for discovery would disturb that installation | retain the existing installation or remove it through its recorded lifecycle first |
| `discovery.prerequisites-missing` | context | none | input | - | other-verb | the controller and infrastructure setup a discovery boot needs is not applied | repeat bootwright machine discover with --prepare-prerequisites to review and confirm controller and infrastructure setup |
| `discovery.refinement-refused` | context | none | input | - | change-input | a change after preparation reaches beyond the disk-selection fields that may follow it | keep prepared ownership unchanged and update only the disk-selection fields |
| `discovery.records-missing` | block | none | records | - | abandon-context | the records a disk discovery builds on hold no inventory, proof or identity the block names | restore the context store from a backup |
| `discovery.records-mismatch` | block | none | records | - | abandon-context | a recorded disk inventory or attempt names another Machine, request or execution than the block froze | retain the original inventory and resolve its ownership before preparing another |
| `discovery.records-conflict` | block | none | records | - | abandon-context | more than one owned disk inventory competes for one Machine | retain one inventory and resolve the ownership of the others |
| `discovery.inventory-missing` | context | none | input | - | other-verb | the Machine has no completed disk discovery for its current policy and media | run bootwright machine discover with --prepare-prerequisites |
| `discovery.root-unprovable` | context | none | input | - | change-input | the recorded inventory does not select exactly one root disk under the Machine's discovery policy | refine spec.hardware.rootDisk.discover.match and update the context before planning again |
| `discovery.evidence-mismatch` | block | possible | records | - | repeat | the disk discovery evidence names another contract or request than the block froze | repeat the operation, which observes the discovery again |
| `discovery.incomplete` | block | possible | target | - | repeat | the disk discovery adapter ended without proving its inventory and the cleanup of its boot | repeat the operation, which converges the discovery boot |

## Physical erase stops

The physical erase capability's stops (`internal/managedos/physicalerase`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `erase.unconfigured` | block | none | defect | - | none | the physical erase capability runs without compiled state or a collaborator it needs | report the message as a defect |
| `erase.custody-unavailable` | block | none | defect | - | repeat | a physical erase was asked to run without durable removal receipt custody, so nothing began | continue the original destroy through the lifecycle engine |
| `erase.records-missing` | block | none | records | - | abandon-context | the original apply or removal records the physical erase rests on are missing or do not prove it | restore the context store from a backup |
| `erase.evidence-mismatch` | block | possible | records | - | abandon-context | an erase receipt or waiver names another machine, MAC set or preparation than the records froze | restore the original apply and removal records |
| `erase.receipt-incomplete` | block | possible | target | - | repeat | the erase evidence does not prove every byte its method erases or the withdrawal of its custody | continue the same removal |
| `erase.machine-running` | adapter-run | none | target | - | fix-then-repeat | the machine is running, so the offline erase inserted no media into it and did not boot it | stop the machine, then repeat the destroy |
| `erase.target-mismatch` | adapter-run | none | target | - | fix-then-repeat | the controller answers as another system, or the machine reports other hardware, than the apply proved, so the erase stopped before it acted | correct the controller or the hardware, then repeat the destroy |
| `erase.controller-unanswered` | adapter-run | none | environment | `unreachable` | fix-then-repeat | the management controller did not answer the system read, so the erase stopped before it acted | restore the controller's access, then repeat the destroy |
| `erase.controller-refused` | adapter-run | possible | target | - | fix-then-repeat | the management controller refused a virtual-media, boot or power request of the erase | correct its cause through the BMC, then repeat the destroy |
| `erase.controller-deadline` | adapter-run | possible | deadline | - | fix-then-repeat | the management controller did not complete a phase of the erase within its time bound | restore the controller's responsiveness, then repeat the destroy |
| `erase.poweroff-unproved` | adapter-run | possible | target | - | fix-then-repeat | the machine did not power off after its erase receipt was acknowledged, so its erase media was not ejected | stop the machine, then repeat the destroy |
| `erase.worker-failed` | adapter-run | possible | target | - | fix-then-repeat | the RAM erase worker stopped without a complete receipt, so no target is proved erased | stop the machine, then repeat the destroy |
| `erase.worker-unreachable` | adapter-run | possible | environment | `unreachable` | fix-then-repeat | the controller did not reach the RAM erase worker within its wait, so no target is proved erased | check that the controller reaches the worker's address, then repeat the destroy |
| `erase.foreign-worker` | adapter-run | possible | target | - | fix-then-repeat | the host answering at the worker's address is not this removal's RAM erase worker, so its receipt was not accepted | look at the BMC console and boot order, then repeat the destroy |
| `erase.receipt-timeout` | adapter-run | possible | deadline | - | fix-then-repeat | the RAM erase worker did not report a complete receipt within the erase deadline | stop the machine, then repeat the destroy |
| `erase.custody-mismatch` | adapter-run | none | records | - | fix-then-repeat | the private erase custody on the execution host belongs to another request or differs from what the removal recorded, so it was not used | inspect the custody on the execution host, then repeat the destroy |
| `erase.boot-media-mismatch` | adapter-run | none | environment | - | fix-then-repeat | the installer image the apply pinned is missing from the media store or no longer matches, so no erase image was built | restore the image with bootwright media add, then repeat the destroy |
| `erase.image-build-failed` | adapter-run | none | build | - | fix-then-repeat | the erase image could not be built from its pinned installer image within its time bound, so the machine was not booted | check the execution host's free space and tools, then repeat the destroy |
| `erase.install-network` | adapter-run | none | input | - | authorize | the install network names a bond option or VLAN that the erase image's boot arguments cannot carry, so the machine was not booted | release the machine without erasing its disks |

## Mirror fill stops

The mirror fill capability's stops (`internal/infrastructureservices/mirrorfill`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `mirror.planning-input-missing` | operation | none | defect | - | none | the mirror capability was built without an input its plan reads, so it planned nothing | report the message as a defect |
| `mirror.request-invalid` | block | none | build | - | registering-build | the frozen mirror request is not one this build can run, so no adapter ran | destroy it with the build that applied it |
| `mirror.unconfigured` | block | none | defect | - | none | the mirror capability runs without an adapter or helper it needs, so no adapter ran | report the message as a defect |
| `mirror.adapter-unproved` | block | unproved | target | - | repeat | the mirror adapter ended without proving its postcondition | repeat the operation, which observes the mirror again |
| `mirror.evidence-mismatch` | block | possible | records | - | repeat | the mirror evidence names another request or version than the block froze, or lies outside its bounds | repeat the operation, which observes the mirror again |
| `mirror.evidence-unencodable` | block | none | defect | - | none | the mirror's removal evidence could not be encoded | report the message as a defect |
| `mirror.drift` | resolution | possible | target | - | fix-then-repeat | the mirror holds content its frozen request does not name in that place | restore the mirror to what its frozen request names, then repeat the operation |
| `mirror.client-unselected` | operation | none | input | - | change-input | the controller stage selects no oc client for the release the mirror host copies | set the cluster's release version to one this build installs |
| `mirror.skopeo-missing` | adapter-run | none | target | - | fix-then-repeat | the mirror host has no working skopeo, so its images were not copied | install the skopeo package on the mirror host, then repeat the apply |
| `mirror.digest-mismatch` | adapter-run | possible | target | - | fix-then-repeat | a digest the mirror host read is not the one this build pins, so the fill stopped | verify the origin and the route to it, then repeat the apply |
| `mirror.release-incomplete` | adapter-run | possible | target | - | fix-then-repeat | the image mirror lacks the release or one of its components after the copy | verify the free space of the mirror's storage, then repeat the apply |
| `mirror.origin-unreachable` | adapter-run | unproved | environment | `unreachable` | fix-then-repeat | the mirror host could not read content from its origin | give the mirror host a route to the origins, then repeat the apply |
| `mirror.unreadable` | adapter-run | unproved | target | - | fix-then-repeat | the mirror host could not read the image mirror with its credential and trust anchor | verify the Registry is ready and answers its credential, then repeat the apply |

## KubeVirt substrate stops

The KubeVirt substrate's stops (`internal/substrate/kubevirt`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `kubevirt.context-invalid` | operation | none | input | - | change-input | the lifecycle context's name is not a valid identity, so no KubeVirt request was derived | name the context with a valid lowercase identifier |
| `kubevirt.name-unsafe` | operation | none | input | - | change-input | a Machine's name cannot be used as a host identifier, so no KubeVirt request was derived | rename the Machine |
| `kubevirt.nic-mac-collision` | operation | none | input | - | change-input | two NICs derive the same MAC address, so no KubeVirt request was derived | rename one of the NICs or Machines |
| `kubevirt.nic-network-kind` | operation | none | input | - | change-input | a NIC names a segment that is not a Localnet or bridge network, so no KubeVirt request was derived | select a ClusterUserDefinedNetwork or NetworkAttachmentDefinition segment for the NIC |
| `kubevirt.plan-without-state` | operation | none | defect | - | none | the KubeVirt capability was asked to plan without compiled desired state | report the message as a defect |
| `kubevirt.unconfigured` | block | none | defect | - | none | the KubeVirt capability runs without its adapter, so nothing ran | report the message as a defect |
| `kubevirt.access-uncustodied` | block | none | defect | - | none | the access of a host cluster was asked for outside an operation that holds its custody, so none was lent | report the message as a defect |
| `kubevirt.ownership-unavailable` | block | none | defect | - | none | the operation that owns a KubeVirt machine's VirtualMachine is not known to its run, so no adapter ran | report the message as a defect |
| `kubevirt.evidence-unencodable` | block | none | defect | - | none | evidence or a pin the KubeVirt capability composed cannot be encoded | report the message as a defect |
| `kubevirt.proof-unrecorded` | block | none | records | - | fix-then-repeat | the proof of the host cluster a Machine is realized on is not recorded for the operation, so no adapter ran | run the apply again so the provider's proof precedes its Machines |
| `kubevirt.host-changed` | block | none | target | - | fix-then-repeat | the host cluster a provider now reaches is not the one its VirtualMachine was realized on, so no adapter ran | restore the host cluster the Machine was realized on, or destroy and apply again |
| `kubevirt.outcome-unusable` | adapter-run | possible | defect | - | repeat | the KubeVirt adapter reported no outcome the capability can use | repeat the operation, which observes the effect again |
| `kubevirt.evidence-mismatch` | block | possible | records | - | repeat | the KubeVirt evidence names another request, VirtualMachine or cluster than the block froze | repeat the operation, which observes the effect again |
| `kubevirt.lend-unproved` | block | possible | target | - | fix-then-repeat | the host access a run was lent did not prove its postcondition on the host cluster | repeat the operation, which lends the access again |
| `kubevirt.host-unproved` | block | possible | target | - | fix-then-repeat | the KubeVirt provider's proof does not hold the host as OpenShift Virtualization ready for virtual machines | correct the host or spec.kubevirt of the provider, then run the apply again |
| `kubevirt.host-silent` | block | possible | environment | `unreachable` | fix-then-repeat | the host cluster did not answer for the VirtualMachine, so the evidence proves nothing | restore access to the host cluster, then repeat the operation |
| `kubevirt.machine-unproved` | block | possible | target | - | repeat | the KubeVirt machine evidence does not prove the VirtualMachine in the state the block asked for | repeat the operation, which observes the VirtualMachine again |
| `kubevirt.machine-foreign` | block | possible | target | - | fix-then-repeat | a VirtualMachine of the Machine's name exists on the host that this context does not own | remove or rename the VirtualMachine the message names, then repeat the operation |
| `kubevirt.removal-unproved` | block | possible | target | - | repeat | the KubeVirt removal evidence does not prove the VirtualMachine and what it owned gone, or still whole | repeat the operation, which observes the VirtualMachine again |

## OpenShift agent install stops

The OpenShift agent-based installation capability's stops (`internal/containercluster/agentinstall`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `openshift.plan-state-missing` | context | none | defect | - | none | an OpenShift cluster capability was asked to plan without the compiled desired state | report the message as a defect |
| `openshift.input-unprojectable` | context | none | input | - | change-input | the declared cluster cannot be projected into an installer configuration, so no installation was planned | correct the cluster the message names and plan again |
| `openshift.selection-unsupported` | context | none | input | - | change-input | the declared cluster selects something this executable does not install, so no installation was planned | remove or change the selection the message names |
| `openshift.adapter-unconfigured` | block | none | defect | - | none | the OpenShift cluster capability runs without an adapter or reader it needs, so nothing began | report the message as a defect |
| `openshift.outcome-unusable` | block | possible | defect | - | repeat | an OpenShift cluster adapter ended and reported no outcome the capability can use | repeat the operation, which observes the cluster again |
| `openshift.request-invalid` | block | none | defect | - | none | the frozen request of an OpenShift cluster block breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `openshift.machine-proof-missing` | block | none | records | - | fix-then-repeat | a Machine the cluster installs on has no recorded proof the installation needs, so nothing began | apply the Machine stage, then repeat the operation |
| `openshift.evidence-invalid` | block | possible | records | - | repeat | the evidence of an OpenShift cluster adapter is incomplete or names another request | repeat the operation, which observes the cluster again |
| `openshift.evidence-unsettled` | block | possible | target | - | repeat | the evidence of an OpenShift cluster adapter does not prove the state its block expects | repeat the operation, which observes the cluster again |
| `openshift.evidence-mismatch` | block | possible | target | - | fix-then-repeat | the evidence of an OpenShift cluster adapter names another cluster, release, image or node than its block froze | find what replaced the cluster the block installed, then repeat the operation |
| `openshift.node-refused` | adapter-run | possible | target | - | fix-then-repeat | the installation adapter stopped on a node whose management controller, boot image or network refused it | read the message, repair what it names, then repeat the operation |
| `openshift.kubeconfig-unnamed` | block | possible | target | - | fix-then-repeat | an administrator kubeconfig the cluster produced names no single cluster authority and principal | repeat the operation, which reads the cluster's kubeconfig again |

## Managed service stops

The managed infrastructure service capability's stops (`internal/infrastructureservices/managedservice`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `service.request-invalid` | operation | none | defect | - | none | the plan input a managed service is derived from is not usable, so nothing was planned | report the message as a defect |
| `service.name-unsafe` | operation | none | input | - | change-input | a managed service's name cannot name its unit, container or directory on the host | rename the service the message names |
| `service.egress-unsupported` | operation | none | input | - | change-input | the placement host reaches the network through a proxy this build cannot pull a service image through | select direct access or an external proxy without authentication or private trust on the placement host |
| `service.unconfigured` | block | none | defect | - | none | the managed service capability runs without an adapter or a certificate check it needs | report the message as a defect |
| `service.password-file-failed` | block | none | defect | - | none | the password file a service authenticates its clients with could not be derived from its credential | report the message as a defect |
| `service.outcome-unusable` | block | possible | defect | - | repeat | the managed service adapter ended without an outcome the block can settle on | repeat the operation, which observes the service again |
| `service.evidence-mismatch` | block | possible | records | - | repeat | the evidence a managed service adapter returned names another request or is not a form it may take | repeat the operation, which observes the service again |
| `service.postcondition-unmet` | block | possible | target | - | repeat | the observed managed service is not in the state the operation leaves it in | repeat the operation, which converges the service |
| `service.certificate-mismatch` | block | possible | target | - | fix-then-repeat | a listener presented a different certificate than the operation bound | restore the certificate the operation bound, then repeat the operation |
| `service.foreign-listener` | adapter-run | none | target | - | fix-then-repeat | something other than the service already holds a socket the service binds, so its unit was not started | stop what listens there, or choose another bind address |
| `service.container-runtime-missing` | adapter-run | none | target | - | fix-then-repeat | the placement host has no working container runtime, so nothing was pulled, published or started | install podman on the placement host, then repeat the operation |
| `service.bind-address-unassigned` | adapter-run | none | target | - | fix-then-repeat | the placement host holds no local address the service binds, so nothing was pulled, published or started | restore the declared address on the placement host, then repeat the operation |
| `service.listener-failed` | adapter-run | possible | target | - | fix-then-repeat | the listener refused or returned an invalid readiness answer | restore the declared service listener and retry the operation |
| `service.readiness-timeout` | adapter-run | unproved | deadline | - | repeat | the listener did not answer within its bounded readiness window | repeat the operation, which observes the listener before anything else starts |
| `service.readiness-unproved` | adapter-run | unproved | target | - | repeat | the listener supplied no usable readiness evidence | repeat the operation, which observes the listener before anything else starts |

## Bare-metal substrate stops

The bare-metal substrate's stops (`internal/substrate/baremetal`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `baremetal.request-invalid` | operation | none | defect | - | none | a plan reached the physical Machine capability without compiled state, or a request it composed breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `baremetal.name-unsafe` | operation | none | input | - | change-input | a physical Machine's name cannot name a host, so no adapter ran | rename the Machine |
| `baremetal.unconfigured` | block | none | defect | - | none | the physical Machine capability runs without its adapter | report the message as a defect |
| `baremetal.no-usable-outcome` | block | possible | target | - | repeat | the machine adapter ended without an outcome the block can use | repeat the operation, which observes the machine again |
| `baremetal.postcondition-unproved` | block | possible | target | - | repeat | the machine adapter ended without proving the postcondition of its verb | repeat the operation, which observes the machine again |
| `baremetal.controller-report-incomplete` | block | possible | target | - | fix-then-repeat | the management controller reported a fault, or reported no identity or no power state | check the management controller of the Machine, then repeat the operation |
| `baremetal.inventory-mismatch` | block | possible | target | - | fix-then-repeat | the machine reports an inventory other than the one its Machine declares | check that the machine behind the management controller is the one the Machine declares, then repeat the operation |
| `baremetal.addresses-undeclared` | block | possible | input | - | change-input | a physical Machine declares no hardware address to prove it by | declare the hardware addresses of the Machine in spec.hardware.nics |
| `baremetal.claim-unnameable` | block | none | input | - | change-input | what the machine reports and its Machine declares cannot name one host claim, so no adapter ran | correct the hardware the Machine declares |
| `baremetal.evidence-mismatch` | block | possible | records | - | repeat | the machine evidence names another request or another Machine block than the block froze | repeat the operation, which observes the machine again |
| `baremetal.identity-unproved` | block | possible | records | - | repeat | the recorded machine evidence proves no identity the context may pin | repeat the operation, which observes the machine again |

## Libvirt substrate stops

The libvirt substrate's stops (`internal/substrate/libvirt`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `libvirt.declaration-invalid` | block | none | input | - | change-input | a libvirt provider, segment, Network or Machine declares a name or arm the host cannot realize, so no adapter ran | correct the declaration the message names |
| `libvirt.request-invalid` | block | none | defect | - | none | a plan reached the libvirt capability without compiled state, or a request it composed breaks its shape or its bounds, so no adapter ran | report the message as a defect |
| `libvirt.frozen-request-invalid` | block | none | records | - | registering-build | a frozen libvirt request holds a media presentation this build cannot run, so no adapter ran | destroy it with the build that applied it |
| `libvirt.unconfigured` | block | none | defect | - | none | a libvirt capability runs without its adapter | report the message as a defect |
| `libvirt.records-missing` | block | none | records | - | abandon-context | the applied revision places no libvirt profile under a machine whose evidence it holds | restore the context store from a backup |
| `libvirt.outcome-unusable` | block | possible | target | - | repeat | a libvirt adapter ended without a usable outcome | repeat the operation, which observes the host before anything else starts |
| `libvirt.postcondition-unproved` | block | possible | target | - | repeat | a libvirt adapter ended without proving the postcondition of its apply or its removal | repeat the operation, which observes the host before anything else starts |
| `libvirt.evidence-mismatch` | block | possible | records | - | repeat | libvirt evidence names another request than the block froze, or reports a shape that request does not allow | repeat the operation, which observes the host again |
| `libvirt.host-drifted` | block | possible | target | - | repeat | the provider host or machine does not hold what the frozen libvirt request declares | repeat the apply, which restores what the request declares |
| `libvirt.host-silent` | block | possible | target | - | fix-then-repeat | the libvirt connection, a driver or NetworkManager did not answer for what the evidence asks about, so nothing is proved | restore the service the message names on the provider host, then repeat the operation |
| `libvirt.removal-incomplete` | block | possible | target | - | repeat | removal evidence still reports something the removal owns, or does not prove it gone | repeat the destroy, which takes back what remains |
| `libvirt.ownership-conflict` | block | possible | target | - | fix-then-repeat | an object of the name the provider uses exists without this context's ownership, so no repeat converges it | remove or rename the object the message names, then repeat the operation |

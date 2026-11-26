# Stop catalog

Read with [CLI output](output.md#diagnostic-taxonomy-and-order) and
[attempt outcomes](../state-reconciliation.md#attempt-outcomes).

A stop is a declared diagnostic of a Bootwright command, an error or a warning.
For an `apply` or `destroy` it is a way the verb ends without completing: a
refusal, a failed effect, an unproved outcome, a pause or an interruption. Each
is declared once, beside the code that raises it, as a
`diagnostics.Stop`, and is one row of this catalog. Its code is the
diagnostic's `code`; this catalog registers it, and the
[diagnostic registry](output.md#diagnostic-taxonomy-and-order) does not list it
again. [`bootwright explain`](commands.md#command-and-flag-catalog) prints
the catalog, or one row with `--code`. A firing's message is composed where it is raised; its remedy defaults
to the row's when the raising code has none. Its certainty decides the
attempt's outcome, when it fires inside an apply or destroy attempt:
`unproved` leaves it `unknown`, every other certainty `failed`. A stop's remedy
follows the [remediation rule](output.md#json-output). Codes not yet moved onto a declared stop remain in the registry, and
`TestEveryLifecycleDiagnosticIsBuiltFromADeclaredStop` counts them in a ledger
that only shrinks.

A stop may also explicitly attest that a failed removal is confined to its
target (`diagnostics.Stop.LocalRemovalFailure`). Only a stop of certainty
`none` and cause `target`, whose firing changed nothing outside its target and
leaves no shared state in doubt, attests it.
[Removal admission](../state-reconciliation.md#keep-going-removal)
uses that attestation after the failure is recorded. Other stop metadata
cannot establish that independent work may continue. These stops, and no
other, attest it:

- `erase.machine-running`
- `firewall.foreign-service`
- `firewall.inactive`

`TestEveryCatalogedStopIsComposedAndExplained` compares this list with the
declared stops.

## Stop vocabulary

| Field | Values |
| --- | --- |
| Level | where the stop ends the verb: `context`: the context as a whole, its records or its selection; `operation`: the whole operation, before any block runs or as it ends; `removal-phase`: one phase of a removal; `block`: one block; `attempt`: one attempt of a block; `resolution`: the observation that resolves an unknown attempt; `adapter-run`: one run of an adapter |
| Certainty | `none`: nothing began; `possible`: an effect may have begun and be partial, and the Recovery names what converges it; `unproved`: the effect must be observed before anything else starts; `owed`: the effect is complete and its bookkeeping is not; an adapter that ended without proving its postcondition is `unproved`, unless its capability's retry converges without observing first, which its remedy says |
| Cause | where the stop's reason lies, while the Recovery says what the operator changes: `input`: the authored input or the invocation; `environment`: the host, network or service Bootwright depends on, which the Fault names; `target`: the Machine, cluster or service acted on; `concurrency`: another command or process holds what this one needs; `build`: this executable, or the build that froze the records; `records`: the context's records are missing, unreadable or contradict one another; `bound`: a documented bound was exceeded; `interrupt`: an interrupt ended it; `deadline`: a deadline ended it; `defect`: a defect of Bootwright; `unclassified`: below |
| Fault | for an `environment` cause, what failed: `dns`: a name did not resolve; `unreachable`: nothing answered; `timeout`: no answer came within its bound; `certificate`: a TLS identity did not verify; `credential`: a credential was refused; `proxy`: a proxy refused or failed; `storage`: a store lacked space or refused a write; `host-key`: an SSH host key was unproved or changed |
| Recovery | the operator's next action, and whether the context's records survive it: `repeat`: run the same command again unchanged, and the records survive; `fix-then-repeat`: correct what the remedy names on a target, host or environment without changing the input, then run the command again, adjusted as the remedy names, and the records survive; `change-input`: change the input the remedy names, then run the command again, and the records survive; `other-verb`: first run the other command the remedy names, such as `destroy`, a continuation of another operation or a discovery stage, and the records survive for that command to read; `wait`: run the command again once the other command or process the message names ends, and the records survive; `authorize`: run the command again with the confirmation or authorization the remedy names, and the records survive; `registering-build`: run the command the remedy names with the build that registered or froze the records, and the records survive for that build alone; `abandon-context`: the records cannot continue as they are, so only restoring them from a backup or deleting the context remains; `none`: no Bootwright command recovers the stop, so the operator reports the defect or acts outside Bootwright as the remedy names, and the records survive |

`unclassified` is the cause of a stop the code that raises it cannot yet
attribute, such as a role task that failed without naming a declared reason.
A row's Recovery names the first step its Remedy asks. Every value of a catalog
column is defined here.

An adapter-run stop a declared reason raises is selected by that reason, and an
optional detail, a closed token, only fills its message; neither is the stop's
Cause. For managed-OS installation and the host firewall,
`installation.DetailRefusal` renders the detail.

## Catalog

Each section below holds the stops one Go package declares.

## Adapter run stops

The lifecycle runner's stops of one adapter run
(`internal/reconciliation/ansiblerunner`): the refusals that let no adapter
start, and the endings of one that ran.

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `runner.result-channel` | adapter-run | none | environment | - | repeat | the result channel the adapter reports on could not be opened, so no adapter ran | repeat the operation |
| `runner.authorization-channel` | adapter-run | none | environment | - | repeat | the channel the runner authorizes effects on could not be opened, so no adapter ran | repeat the operation |
| `runner.not-started` | adapter-run | none | build | - | fix-then-repeat | the qualified adapter process could not start | run bootwright install |
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
| `runner.privilege-unproved` | adapter-run | none | environment | - | fix-then-repeat | a provided lifecycle host did not prove passwordless sudo before the adapter ran | check the SSH connection and the bound account's sudo privileges, then repeat |
| `runner.boundary-unavailable` | adapter-run | none | build | - | fix-then-repeat | the authorized adapter execution boundary is not installed | run bootwright install |
| `runner.unsupported-platform` | adapter-run | none | build | - | none | lifecycle adapters run only on Linux on amd64 | run Bootwright on Linux on amd64 |
| `runner.storage` | adapter-run | none | environment | `storage` | fix-then-repeat | the runner's private storage for an adapter run could not be prepared or proved, so no adapter ran | restore the private directories under /var/lib/bootwright and the run scratch parent, then repeat the operation |
| `runner.scratch-unusable` | adapter-run | none | environment | `storage` | fix-then-repeat | the run scratch directory is not one an adapter may use, so no adapter ran | give the run scratch parent a filesystem that allows execution and is owned by the invoking user alone, then repeat the operation |
| `runner.stale-run` | adapter-run | none | environment | `storage` | fix-then-repeat | an earlier adapter run left a directory the runner could not remove safely | remove each bootwright-run-<n> directory under /run whose lock file no process holds an advisory lock on, such as one where flock -n <dir>/lock true succeeds, with its bootwright-scratch-<n>-* directories under /var/lib/bootwright-staging/runs, then repeat the operation |
| `runner.adapter-running` | adapter-run | none | concurrency | - | wait | another lifecycle adapter, or processes it left, still holds the job lock this run needs | wait for the adapter the message names to end and repeat the command |
| `runner.material-unprepared` | adapter-run | none | records | - | fix-then-repeat | the bound Secret material the run lends could not be prepared, so no adapter ran | restore the context's keyring, then repeat the operation |
| `runner.invocation-ended` | adapter-run | none | defect | - | repeat | a run was asked of an invocation that had already ended, so no adapter ran | repeat the operation |
| `runner.output-invalid` | adapter-run | possible | defect | - | repeat | a file the adapter was to produce is not the private regular file the run expects | read the block's retained output with bootwright logs --raw, then repeat the operation |
| `runner.preparation-unrecorded` | adapter-run | unproved | records | - | repeat | the before-state the adapter reported could not be recorded before its effect | repeat the operation, which observes the effect before anything else starts |
| `runner.binding-unavailable` | adapter-run | none | records | - | repeat | a bound Secret the run lends is not available to the attempt or lacks the part it needs, so no adapter ran | repeat the operation, which reopens its Secret bindings |
| `runner.host-key-invalid` | adapter-run | none | input | - | change-input | the SSH host key Secret of a placement is not one known-hosts entry for its host, so no adapter ran | author access.ssh.knownHostsRef on the Machine as one known-hosts entry for its SSH host |
| `runner.automation-mismatch` | adapter-run | none | build | - | repeat | the staged automation does not match this executable's automation, so no adapter ran | rerun the command |

## Engine stops

The lifecycle engine's stops (`internal/reconciliation/lifecycle`): a block
that stopped on its host or on a defect, an attempt that recorded no start, a
removal that would take back state in use, an operation that did not
complete, the consequence of the causes before it, and the refusals of a
decision and its plan before any effect.

### Execution and removal

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
| `lifecycle.start-power` | operation | none | target | - | fix-then-repeat | a new apply or destroy could not prove every managed machine powered off or absent | stop the named machines or restore access to their power state and repeat the command |
| `destroy.in-use` | removal-phase | none | target | - | fix-then-repeat | a removal would take back state that is still in use, so it takes back nothing at that boundary | stop what the message names, then repeat the destroy |
| `destroy.consumer-survives` | removal-phase | none | target | - | fix-then-repeat | a removal would retire what a consumer that outlives it still uses | keep this context while the consumer runs; a destroy takes back what a consumer uses only when the consumer leaves with owned virtual node Machines the same destroy deletes |
| `apply.produced-input-missing` | block | none | records | - | fix-then-repeat | a block borrows an output its producer has not proved, or no longer offers | complete the producer block, then repeat the operation |
| `engine.recapture-mismatch` | block | possible | target | - | fix-then-repeat | an output a block recaptured presents another identity than the one custody holds | find what replaced the effect the block proved; the context's records name the original |
| `engine.resolved-unperformed` | resolution | none | target | - | repeat | an observation proved a frozen effect never performed, so the block is failed and retried | repeat the operation, which retries the block |
| `engine.resolved-partial` | resolution | possible | target | - | repeat | an observation left a frozen effect partly realized or a removal's postcondition unreadable, so the block is failed and retried | repeat the operation, which retries the block |
| `destroy.postcondition-unknown` | block | none | target | - | fix-then-repeat | a retried removal read its target again and still could not tell its postcondition, so it performed no effect | restore the target the removal reads, then repeat the destroy |
| `engine.finalization-incomplete` | operation | owed | records | - | repeat | an operation's effects are complete and its finalization is not | repeat the command, which finishes the finalization |
| `destroy.release-incomplete` | operation | owed | records | - | repeat | the removal completed, and releasing what it owned did not | repeat the destroy to finish it |
| `destroy.unproved-effects` | operation | unproved | target | - | fix-then-repeat | a removal cannot prove what some effects left behind, so it registered nothing | do what each block's diagnostic reports, then repeat the destroy |
| `engine.outcome-unknown` | resolution | unproved | target | - | fix-then-repeat | a block's outcome is still unknown after an observation, for the reason the message names | do what the reason names, then repeat the command to observe the block again |
| `engine.log-unmaintained` | block | possible | environment | `storage` | fix-then-repeat | a required private operation log could not be maintained | restore the context's private log tree under /var/log/bootwright before repeating the operation |
| `engine.produced-name-invalid` | block | possible | defect | - | none | a block offered produced material under an invalid or repeated name, so custody kept none of it | report the message as a defect |

### Decisions

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `engine.finalization-due` | operation | owed | defect | - | repeat | an operation is due its finalization, which no decision may skip | repeat the command, which finalizes that operation before it decides |
| `engine.records-contradict` | operation | none | records | - | abandon-context | an incomplete operation holds records that contradict what it started | read the records with bootwright status --context <context>, then restore them from a backup or delete the context |
| `engine.other-verb-incomplete` | operation | none | input | - | other-verb | an incomplete operation of the other verb must be continued before another operation | continue or replace the incomplete operation the message names |
| `apply.records-missing` | operation | none | records | - | abandon-context | a completed apply records no completion for blocks it froze, so nothing proves its input applied | take back its frozen plan with destroy, or delete the context with context delete --purge |
| `destroy.records-missing` | operation | none | records | - | abandon-context | a completed destroy records no completion for blocks it froze, so nothing proves their effects removed | delete the context with context delete --purge, adding --allow-orphans unless its evidence is pristine |
| `apply.input-changed` | operation | none | input | - | other-verb | the desired state changed after the apply completed, and it is no addition to it | take back what the context owns with destroy, then apply the changed input |
| `apply.not-an-addition` | operation | none | input | - | other-verb | a changed input over a completed apply adds something an additive apply cannot take | take back what the context owns with destroy, then apply the changed input |
| `apply.prerequisite-lost` | operation | none | records | - | other-verb | a block an addition needs, held by an earlier apply of its lineage, is gone or no longer done | take back what the lineage owns with destroy |
| `apply.nothing-declared` | operation | none | input | - | change-input | the selected Environment declares nothing this executable would create | declare a managed infrastructure service |
| `apply.stage-empty` | operation | none | input | - | fix-then-repeat | the selected stages or the --until instance have nothing to start | repeat the apply with a wider selection |
| `apply.stage-excludes-retry` | operation | none | input | - | fix-then-repeat | the block the operation must retry first is outside the selected stages | repeat the apply with a selection that holds that block |
| `apply.until-unknown` | operation | none | input | - | fix-then-repeat | --until names no add-on instance of the plan | name an add-on instance the plan holds, or repeat without --until |
| `engine.unindexed-records` | context | none | records | - | abandon-context | the context holds operation records or evidence its index names no operation for | delete the context with context delete --purge, adding --allow-orphans unless its evidence is pristine |
| `engine.request-defect` | operation | none | defect | - | none | a capability declared or requested something the engine refuses to compose | report the message as a defect |
| `engine.other-build` | operation | none | build | - | registering-build | this executable cannot prove it would continue or remove the operation as the build that registered it | repeat the command with the build that registered the operation |
| `destroy.inverse-moved` | operation | none | build | - | registering-build | this executable computes another answer than the removal plan froze for which blocks a step's effect leaves with, or for whether the original apply's records leave the step nothing to take back, so it performed no effect | continue or replace the removal with the build that registered it |
| `destroy.inverse-unstated` | operation | none | defect | - | none | a removal step states no effect, nothing it keeps, no step it leaves with and no reason it has nothing to take back, or nothing to take back beside steps it leaves with, so nothing was registered | report the message as a defect |
| `engine.plan-moved` | operation | none | concurrency | - | repeat | the context changed between the plan and the transition, so nothing was registered | repeat the command to plan from what the context holds now |
| `engine.input-moved` | operation | none | input | - | fix-then-repeat | the context's input is no longer the revision the operation froze | restore the exact input revision the operation froze |
| `engine.binding-lost` | operation | none | records | - | abandon-context | a Secret binding the operation froze is lost, so its material cannot be lent again | restore the context's keyring from a complete backup, or delete the context and remove what it named by hand |
| `destroy.stage-selected` | operation | none | input | - | fix-then-repeat | the next operation is a destroy, which accepts no stage selection | repeat the command without --stage and --until |
| `destroy.stage-below-floor` | operation | none | input | - | fix-then-repeat | a step below the floor depends on what a staged destroy from it takes back, so nothing was registered | repeat with the lower stage the message names, or destroy without --stage |
| `destroy.stage-keeps-unfinished` | operation | none | records | - | other-verb | a step a staged destroy would keep is not done, so nothing was registered | finish it with apply, take it back with a lower stage, or destroy without --stage |
| `destroy.stage-keeps-host` | operation | none | input | - | fix-then-repeat | a step a staged destroy would take back runs on a Machine it keeps, which nothing yet takes such a step back from, so nothing was registered | repeat with the stage or the item the message names, or destroy without --stage |
| `destroy.kept-host-stopped` | removal-phase | none | target | - | fix-then-repeat | a staged destroy takes back steps on a kept Machine while it runs, and that Machine was not proved running, so it took back nothing | start the Machine the message names, then repeat the destroy |
| `destroy.stage-changed` | operation | none | input | - | fix-then-repeat | a later floor than an unfinished staged destroy froze, or any floor over an unfinished whole destroy, would narrow it, so nothing was registered | finish it with the command the message names, or review and take back the whole context with the commands it names |
| `apply.target-before-preparation` | operation | none | input | - | other-verb | a scoped apply cannot freeze its installation plan before inventory outside its targets exists | complete the named machine inventories and review the scoped apply again |
| `apply.until-before-preparation` | operation | none | input | - | other-verb | the add-on boundary --until names cannot be planned before preparation completes | run apply without --until to complete preparation, then select --until |
| `engine.records-damaged` | operation | none | records | - | abandon-context | the context's operation records do not form the lineage or removal the operation needs | restore the context store from a backup, or delete the context with what it may still own |
| `engine.borrowed-ssh` | operation | none | input | - | fix-then-repeat | a lifecycle operation was given borrowed SSH credentials, which it never uses | remove the --ssh flags and author each Machine's own access |
| `engine.confirmation-required` | operation | none | input | - | authorize | the operation needs a confirmation the invocation cannot ask for | repeat the command on a terminal, or with --yes once the plan is reviewed |
| `engine.input-invalid` | operation | none | input | - | fix-then-repeat | an invocation option holds a value the engine refuses | repeat the command with the value the message names |
| `engine.credential-moved` | operation | none | concurrency | - | repeat | a cluster administrator credential changed while its update was decided or pending | repeat the credential update against the current context |

### Plans, preparations and authorizations

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `apply.kept-block-changed` | operation | none | input | - | other-verb | the selected input or this build would plan a kept step of a trimmed context otherwise | run the destroy the message names, then apply |
| `apply.restore-empty` | operation | none | input | - | other-verb | the selected input declares nothing a staged destroy took back, so a restoring apply has nothing to restore | take back the whole context with destroy, or declare again what the staged destroy took back and apply |
| `apply.restore-adds-new` | operation | none | input | - | change-input | the selected input declares a step no staged destroy of the context took back, which a restoring apply never creates | restore without it, then add an add-on instance with the additive apply, or take back the whole context with destroy, then apply |
| `plan.input-uncompilable` | context | none | input | - | change-input | the context's frozen desired state could not be compiled | repair the input with validate and import it again |
| `plan.capability-missing` | context | none | build | - | none | no lifecycle capability of this build plans the selected state | use a build that carries the capability; report it if this build should |
| `plan.capability-defect` | context | none | defect | - | none | a capability planned a block it may not plan | report the message as a defect |
| `plan.host-key-unbound` | context | none | input | - | change-input | an SSH placement binds no host key, or reaches the controller where its pinned key does not apply | author access.ssh.knownHostsRef on the Machine the message names |
| `plan.environment-invalid` | context | none | input | - | change-input | the selected state holds no single Environment naming its controller Machine | repair the input with validate |
| `authorize.unneeded` | operation | none | input | - | fix-then-repeat | --authorize names a token the plan does not require or a relaxation that changes nothing, or an --unregistered that acknowledges nothing | repeat the command without that --authorize |
| `authorize.missing` | operation | none | input | - | authorize | the plan has consequences no --authorize grants | review the plan, then repeat the command with the authorization it names |
| `authorize.stale` | operation | none | input | - | authorize | an --authorize digest names steps the plan no longer holds, or an unattended run binds none | review the plan, then repeat the command with the digest it prints |
| `authorize.rebind-invalid` | operation | none | input | - | fix-then-repeat | an --authorize rebind names a Secret the operation cannot rebind | repeat the command naming each rotated access credential once as --authorize rebind=<secret> |
| `prepare.refused` | operation | none | input | - | fix-then-repeat | a preparation cannot start or advance over the context's current operation or input | finish or take back the current operation, or restore the input the preparation recorded |
| `prepare.unsupported` | operation | none | build | - | none | this executable registers no preparation for the requested owner | use a build that registers the preparation |
| `authorize.relaxation-changed` | operation | none | input | - | fix-then-repeat | an operation continues only under the relaxations its plan froze, and the invocation names others | continue it naming the relaxations its plan froze, or replace it with a fresh destroy |
| `authorize.abandon-unattended` | operation | none | input | - | authorize | an unattended destroy names --authorize abandon=<machine>, which only a confirmation an operator answers accepts, so nothing was registered | run the destroy in a terminal without --yes and confirm the plan |
| `diff.context-moved` | context | none | concurrency | - | repeat | the context changed while diff read it | repeat the diff |
| `plan.controller-order` | context | none | input | - | change-input | a block the controller stage waits for runs only after that stage | place the object the message names where the controller stage need not wait for it |
| `plan.reservation-key-too-long` | context | none | input | - | change-input | a host reservation key would exceed its bound once keyed by the SSH host it is placed on | reach the Machine by a shorter address, or give the reserving service a shorter name |
| `plan.host-key-unreadable` | context | none | input | - | change-input | the SSH host key Secret of a Machine binds no readable host key, so the reservations placed there cannot name the host it pins | author access.ssh.knownHostsRef on the Machine as one known-hosts entry for its SSH host |
| `plan.socket-conflict` | context | none | input | - | change-input | two services of the context would listen on one socket of a host | give one of them another bind address or port |
| `plan.template-value` | context | none | input | - | change-input | a planned request holds a template delimiter or a key ansible-core reserves, which no adapter may take as data | remove it from the desired-state values, import them, then plan again |
| `plan.object-unsupported` | context | none | build | - | change-input | this executable cannot realize a selected object, for the reason the message names, so nothing was registered | remove the object this executable cannot realize, or use a build that supports it |

## Engine credential, controller and warning stops

The lifecycle engine's other stops (`internal/reconciliation/lifecycle`): the
refusals of a cluster credential or sign-on update and of the controller host,
and the warnings the engine reports.

### Credentials and the controller host

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `engine.credential-unapplied` | operation | none | input | - | other-verb | an administrator credential update names a cluster no completed apply installed and proved | complete the cluster's apply before updating its credentials |
| `engine.credential-cluster-unknown` | operation | none | input | - | change-input | an administrator credential update names a cluster the context does not select | bootwright cluster info --context <context> lists the selected clusters |
| `engine.credential-login-inapplicable` | operation | none | input | - | change-input | a credential update names a login the cluster does not have | bootwright cluster info --context <context> --name <cluster> shows its logins |
| `engine.credential-external-cluster` | operation | none | input | - | change-input | an administrator credential update names an external cluster, whose credentials its own administrator keeps | update the credentials through the external cluster's administrator |
| `engine.credential-custody-unproved` | operation | none | records | - | fix-then-repeat | the custody of a cluster administrator credential is missing, unproved or invalid, so its update changed nothing | restore the original credential custody |
| `engine.credential-update-pending` | operation | none | records | - | other-verb | an administrator credential update was left pending, so custody holds no credential proved current | finish the retained update with bootwright cluster update-admin-creds --context <context> |
| `engine.credential-preparation-invalid` | operation | none | target | - | fix-then-repeat | the cluster's read-only preparation of an administrator credential update was not proved or returned no valid update, so nothing changed | check the cluster's original administrator account |
| `engine.credential-update-unproved` | operation | possible | target | - | repeat | the adapter that updates a cluster administrator credential ended without proving the update | repeat bootwright cluster update-admin-creds --context <context> to finish the retained update |
| `engine.sign-on-unapplied` | operation | none | input | - | other-verb | a sign-on suspend or resume names a cluster whose sign-on no completed current apply realized | complete the cluster's apply before suspending or resuming its sign-on |
| `engine.sign-on-cluster-unknown` | operation | none | input | - | change-input | a sign-on suspend or resume names a cluster the context does not select | bootwright cluster info --context <context> lists the selected clusters |
| `engine.sign-on-external-cluster` | operation | none | input | - | change-input | a sign-on suspend or resume names an external cluster, whose sign-on its own administrator keeps | change the sign-on through the external cluster's administrator |
| `engine.sign-on-absent` | operation | none | input | - | change-input | a sign-on suspend or resume names a cluster no apply of the context gave a single sign-on | bootwright cluster info --context <context> --name <cluster> shows the cluster's sign-on |
| `engine.sign-on-suspension-damaged` | operation | none | records | - | fix-then-repeat | the context's sign-on suspension record is unreadable or names another block or request, so nothing ran | restore the context's records from a matching backup |
| `engine.sign-on-moved` | operation | none | concurrency | - | repeat | the context's operation, input or sign-on suspension changed while a suspend or resume was decided, so nothing ran | repeat the command against the current context |
| `engine.controller-unprepared` | operation | none | environment | - | fix-then-repeat | this host has no completed controller setup, so nothing ran | run bootwright install |
| `engine.controller-unbound` | operation | none | records | - | fix-then-repeat | the context is not bound to the controller host it runs on, so nothing ran | run bootwright install to record the controller binding and host evidence |
| `engine.controller-other-host` | operation | none | target | - | fix-then-repeat | this host is not the controller host the context is bound to, or the recorded binding does not match it, so nothing ran | run the command on the host the context is bound to |
| `engine.runtime-unprepared` | operation | none | environment | - | fix-then-repeat | the controller runtime for this build is not prepared on this host and could not be rebuilt, so nothing ran | run bootwright install |
| `engine.client-not-installed` | block | none | target | - | fix-then-repeat | a controller client the work needs is not installed by the context's controller stage, so nothing ran | apply the context's controller stage, then repeat the command |
| `engine.wiring-missing` | context | none | defect | - | repeat | this build was wired without a part the command needs, so nothing ran | repeat the command; a repeated failure is a Bootwright defect to report |

### Warnings

Warnings the engine reports, or relays from an adapter's completed result.
They carry the row's code and the raiser's remedy, end no verb and emit no
default remedy, `cause` or `recovery`; their Level, Certainty, Cause and
Recovery cells are shown by `explain` alone.

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `plan.consumer-outlives-destroy` | operation | none | target | - | change-input | a warning that a consumer runs where a destroy of this context will not delete it, so that destroy will refuse before registration | run the consumer on Machines this context deletes, or keep this context while the consumer runs |
| `storage.health` | block | none | target | - | none | a warning: a read-only Ceph observation reported a health check naming a pool or filesystem | resolve the health check through the Ceph cluster |
| `storage.not-ready` | block | none | target | - | none | a warning: a read-only Ceph observation completed with checks that kept the block from ready | resolve the checks the message names through the Ceph cluster |
| `substrate.shadowed-route` | block | none | target | - | none | a warning: a libvirt host apply admitted a main-table route broader than a managed network's | narrow the route the message names on the libvirt host |
| `controller.outdated-client` | operation | none | build | - | other-verb | a warning: an apply's controller stage proved a client below this build's minimum, which a settling apply and a removal keep running | take the apply back with destroy and apply again to install this build's default client, or keep the build that applied it |

## Machine power stops

The Machine power and media eject capability's stops (`internal/machine/power`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `power.request-invalid` | adapter-run | none | input | - | fix-then-repeat | the power request names a verb or context the command does not take, so nothing was sent | repeat the command with a supported verb and context |
| `power.identity-changed` | adapter-run | none | target | - | fix-then-repeat | the management controller answers as another system than the one the context's apply proved, so no power request was sent | correct the Machine's BMC or provider, or take back and apply the context again |
| `power.evidence-mismatch` | adapter-run | possible | defect | - | repeat | the power or eject evidence names another request, Machine or survey, or a state the command cannot name | repeat the command; report the message if it stops the same way |
| `power.unproved` | adapter-run | unproved | target | - | repeat | the management controller did not prove the power or media state the command asked for | read the power state again with bootwright machine list --context <context> --power-status, then repeat the command |

## Disk discovery stops

The managed OS disk discovery capability's stops (`internal/managedos/diskdiscovery`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `discovery.unconfigured` | block | none | defect | - | none | disk discovery runs without compiled state or one of the readers and sources it depends on | report the message as a defect |
| `discovery.request-invalid` | block | none | defect | - | none | the frozen disk discovery request names something its adapter cannot run, so no adapter ran | report the message as a defect |
| `discovery.unsupported` | context | none | input | - | change-input | the Machine or the artifact server it names cannot host a disk discovery boot | use a controller-local HTTPS artifact server and full or certificate BMC verification |
| `discovery.machine-installed` | context | none | target | - | change-input | the Machine already has installation ownership, so booting it for discovery would disturb that installation | retain the existing installation or remove it through its recorded lifecycle first |
| `discovery.prerequisites-missing` | context | none | input | - | other-verb | the controller and infrastructure setup a discovery boot needs is not applied | bootwright machine discover --context <context> --name <list> --stage controller --dry-run |
| `discovery.refinement-refused` | context | none | input | - | change-input | a change after preparation reaches beyond the disk-selection fields that may follow it | keep prepared ownership unchanged and update only the disk-selection fields |
| `discovery.records-missing` | block | none | records | - | abandon-context | the records a disk discovery builds on hold no inventory, proof or identity the block names | restore the context store from a backup |
| `discovery.records-mismatch` | block | none | records | - | none | a recorded disk inventory or attempt names another Machine, request or execution than the block froze | retain the original inventory and resolve its ownership before preparing another |
| `discovery.records-conflict` | block | none | records | - | none | more than one owned disk inventory competes for one Machine | retain one inventory and resolve the ownership of the others |
| `discovery.inventory-missing` | context | none | input | - | other-verb | the Machine has no completed disk discovery for its current policy and media | run bootwright machine discover --context <context> --name <machine> with --stage controller |
| `discovery.inventory-stale` | context | none | input | - | change-input | an owned inventory belongs to a different discovery request | restore the recorded discovery settings or review removal of the current lifecycle ownership before discovering again |
| `discovery.root-unprovable` | context | none | input | - | change-input | the recorded inventory cannot prove the protected disks and exactly one eligible root | inspect the recorded inventory and correct the protected storage claim or root match named by the refusal, then update the context |
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
| `erase.boot-media-mismatch` | adapter-run | none | environment | - | fix-then-repeat | the installer image the apply pinned is missing from the media store or no longer matches, so no erase image was built | restore the image with bootwright catalog media add, then repeat the destroy |
| `erase.image-build-failed` | adapter-run | none | build | - | fix-then-repeat | the erase image could not be built from its pinned installer image within its time bound, so the machine was not booted | check the execution host's free space and tools, then repeat the destroy |
| `erase.install-network` | adapter-run | none | input | - | authorize | the install network names a bond option or VLAN that the erase image's boot arguments cannot carry, so the machine was not booted | release the machine without erasing its disks |
| `erase.relaxation-inapplicable` | operation | none | input | - | fix-then-repeat | an erase relaxation names a block that is no removal erase, a method other than metadataClear or retain, or one that changes nothing, so nothing was registered | repeat the destroy without that --authorize |

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
| `kubevirt.outcome-unusable` | adapter-run | unproved | defect | - | repeat | the KubeVirt adapter reported no outcome the capability can use | repeat the operation, which observes the effect again |
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
| `openshift.outcome-unusable` | block | unproved | defect | - | repeat | an OpenShift cluster adapter ended and reported no outcome the capability can use | repeat the operation, which observes the cluster again |
| `openshift.request-invalid` | block | none | defect | - | none | the frozen request of an OpenShift cluster block breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `openshift.machine-proof-missing` | block | none | records | - | fix-then-repeat | a Machine the cluster installs on has no recorded proof the installation needs, so nothing began | apply the Machine stage, then repeat the operation |
| `openshift.evidence-invalid` | block | possible | records | - | repeat | the evidence of an OpenShift cluster adapter is incomplete or names another request | repeat the operation, which observes the cluster again |
| `openshift.evidence-unsettled` | block | possible | target | - | repeat | the evidence of an OpenShift cluster adapter does not prove the state its block expects | repeat the operation, which observes the cluster again |
| `openshift.evidence-mismatch` | block | possible | target | - | fix-then-repeat | the evidence of an OpenShift cluster adapter names another cluster, release, image or node than its block froze | find what replaced the cluster the block installed, then repeat the operation |
| `openshift.node-refused` | adapter-run | possible | target | - | fix-then-repeat | the installation adapter stopped on a node whose management controller, boot image or network refused it | read the message, repair what it names, then repeat the operation |
| `openshift.kubeconfig-unnamed` | block | possible | target | - | fix-then-repeat | an administrator kubeconfig the cluster produced names no single cluster authority and principal | repeat the operation, which reads the cluster's kubeconfig again |
| `openshift.kept-node-handed` | block | possible | target | - | other-verb | a staged destroy that keeps a cluster's nodes found its work area proving a node was handed the image, so it stopped before that work area is discarded | take back the nodes with the destroy the message names |

## Managed service stops

The managed infrastructure service capability's stops (`internal/infrastructureservices/managedservice`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `service.request-invalid` | operation | none | defect | - | none | the plan input a managed service is derived from is not usable, so nothing was planned | report the message as a defect |
| `service.name-unsafe` | operation | none | input | - | change-input | a managed service's name cannot name its unit, container or directory on the host | rename the service the message names |
| `service.egress-unsupported` | operation | none | input | - | change-input | the placement host reaches the network through a proxy this build cannot pull a service image through | select direct access or an external proxy without authentication or private trust on the placement host |
| `service.unconfigured` | block | none | defect | - | none | the managed service capability runs without an adapter or a certificate check it needs | report the message as a defect |
| `service.password-file-failed` | block | none | defect | - | none | the password file a service authenticates its clients with could not be derived from its credential | report the message as a defect |
| `service.outcome-unusable` | block | unproved | defect | - | repeat | the managed service adapter ended without an outcome the block can settle on | repeat the operation, which observes the service again |
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
| `baremetal.no-usable-outcome` | block | unproved | target | - | repeat | the machine adapter ended without an outcome the block can use | repeat the operation, which observes the machine again |
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
| `libvirt.outcome-unusable` | block | unproved | target | - | repeat | a libvirt adapter ended without a usable outcome | repeat the operation, which observes the host before anything else starts |
| `libvirt.postcondition-unproved` | block | possible | target | - | repeat | a libvirt adapter ended without proving the postcondition of its apply or its removal | repeat the operation, which observes the host again |
| `libvirt.evidence-mismatch` | block | possible | records | - | repeat | libvirt evidence names another request than the block froze, or reports a shape that request does not allow | repeat the operation, which observes the host again |
| `libvirt.host-drifted` | block | possible | target | - | repeat | the provider host or machine does not hold what the frozen libvirt request declares | repeat the apply, which restores what the request declares |
| `libvirt.host-silent` | block | possible | target | - | fix-then-repeat | the libvirt connection, a driver or NetworkManager did not answer for what the evidence asks about, so nothing is proved | restore the service the message names on the provider host, then repeat the operation |
| `libvirt.removal-incomplete` | block | possible | target | - | repeat | removal evidence still reports something the removal owns, or does not prove it gone | repeat the destroy, which takes back what remains |
| `libvirt.ownership-conflict` | block | possible | target | - | fix-then-repeat | an object of the name the provider uses exists without this context's ownership, so no repeat converges it | remove or rename the object the message names, then repeat the operation |

## Host firewall stops

The host firewall capability's stops (`internal/managedos/hostfirewall`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `firewall.request-invalid` | block | none | defect | - | none | the host firewall request a plan composed breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `firewall.unconfigured` | block | none | defect | - | none | the host firewall capability runs without its adapter | report the message as a defect |
| `firewall.records-missing` | block | none | records | - | abandon-context | the host firewall block names no apply its evidence came from | restore the context store from a backup |
| `firewall.evidence-mismatch` | block | possible | records | - | repeat | the host firewall evidence names another request or version than the block froze | repeat the operation, which observes the firewall again |
| `firewall.unproved` | block | unproved | target | - | repeat | the host firewall adapter ended without proving its postcondition | repeat the operation, which observes the firewall before anything else starts |
| `firewall.foreign-service` | adapter-run | none | target | - | fix-then-repeat | the Machine holds a firewalld service of the same name this context did not write, so its firewall was left unchanged | remove the foreign firewalld service, then run the command again |
| `firewall.no-interface` | adapter-run | none | input | - | change-input | the Machine has no interface on a network a firewalld service admits traffic on, so its firewall was left unchanged | give the Machine the declared listener address or required network, then run the command again |
| `firewall.inactive` | adapter-run | none | target | - | fix-then-repeat | firewalld is not running on a Machine whose install profile enables it, so its firewall was left unchanged | start firewalld on the Machine, then run the command again |
| `firewall.change-refused` | adapter-run | possible | target | - | fix-then-repeat | firewalld refused a change on the Machine partway through its apply or removal | read the firewalld journal on the Machine, or for SSH admission its SSH ingress unit journal, correct it, then run the command again |

## Provided OS update stops

Stops for [provided OS updates](../managed-os.md#provided-os-updates).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `osupdate.request-invalid` | block | none | defect | - | none | the provided OS update request is invalid, so no adapter ran | report the message as a defect |
| `osupdate.refused` | adapter-run | possible | target | - | fix-then-repeat | the Machine or its update source did not satisfy the provided OS update policy | check the Machine's OS release, update repositories and lifecycle access, then repeat the operation |
| `osupdate.unproved` | block | unproved | target | - | repeat | the provided OS update adapter ended without proving the update | repeat the operation to observe the Machine before resuming the update |

## Provided OS hostname stops

Stops for the [provided hostname](../managed-os.md#provided-hostname).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `oshostname.request-invalid` | block | none | defect | - | none | the provided OS hostname request is invalid, so no adapter ran | report the message as a defect |
| `oshostname.refused` | adapter-run | possible | target | - | fix-then-repeat | the Machine did not accept the declared hostname | check what manages the Machine's hostname and its lifecycle access, then repeat the operation |
| `oshostname.unproved` | block | unproved | target | - | repeat | the provided OS hostname adapter ended without proving the hostname | repeat the operation to observe the Machine before resuming the hostname |

## Provided registration stops

Stops for [provided registration](../managed-os.md#provided-registration).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `osregistration.request-invalid` | block | none | defect | - | none | the provided registration request is invalid, so no adapter ran | report the message as a defect |
| `osregistration.refused` | adapter-run | possible | target | - | fix-then-repeat | the Machine's registration or its Satellite did not satisfy the provided registration | check the Machine's registration, its route to the Satellite and the Entitlement's CA bundle, then repeat the operation |
| `osregistration.unproved` | block | unproved | target | - | repeat | the provided registration adapter ended without proving the registration | repeat the operation to observe the Machine before resuming the registration |

## Provided OS time stops

Stops for [provided time synchronization](../managed-os.md#provided-time-synchronization).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ostime.request-invalid` | block | none | defect | - | none | the provided time synchronization request is invalid, so no adapter ran | report the message as a defect |
| `ostime.refused` | adapter-run | possible | target | - | fix-then-repeat | the Machine's time service or configuration did not satisfy the provided time synchronization | check the Machine's time service, its chrony configuration and its route to the declared servers, then repeat the operation |
| `ostime.unproved` | block | unproved | target | - | repeat | the provided time synchronization adapter ended without proving the clock synchronized | repeat the operation to observe the Machine before resuming the time synchronization |

## Managed OS installation stops

The managed OS installation capability's stops (`internal/managedos/installation`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `install.unconfigured` | block | none | defect | - | none | the installation capability runs without a compiled state, reader or adapter it needs | report the message as a defect |
| `install.request-invalid` | block | none | records | - | fix-then-repeat | a frozen installation request holds values its own derivation or this build's Kickstart cannot carry, so nothing began | plan the installation again with this release |
| `install.input-invalid` | operation | none | input | - | change-input | the declared Machine, profile or package source cannot compose an installation | correct the declaration the message names |
| `install.media-unusable` | operation | none | input | - | fix-then-repeat | the host media store holds no usable image of the installation media a profile names | import the image again with bootwright catalog media add |
| `install.unsupported` | operation | none | input | - | change-input | the declared installation names a shape this build cannot install | change the declaration the message names |
| `install.records-missing` | block | none | records | - | abandon-context | the apply records no longer prove the installation, publication or discovery a later step depends on | restore the original apply records |
| `install.evidence-invalid` | block | possible | records | - | repeat | the installation evidence is malformed or names another request, address, marker or state than the block froze | repeat the operation, which observes the installation again |
| `install.postcondition-unmet` | block | possible | target | - | repeat | the installation evidence does not show the state the check requires | repeat the operation, which observes the installation again |
| `install.foreign-state` | block | possible | target | - | other-verb | the installed machine holds an installation, registration or update record this installation did not create | take the context back with bootwright destroy --context <context> and apply again |
| `install.no-outcome` | block | unproved | defect | - | repeat | the installation adapter reported no usable outcome after it ran | repeat the operation, which observes the installation again |
| `install.host-key-mismatch` | block | none | records | - | other-verb | the host key type a request froze is not the one the bound Secret or this build delivers | take the context back with bootwright destroy --context <context>, then plan and apply it again |
| `install.foreign-installation` | adapter-run | none | target | - | change-input | the machine answered with another installation's marker, so no installer was booted | verify the declared Machine and its management controller |
| `install.source-changed` | adapter-run | none | target | - | other-verb | an image or package tree no longer has the size and digest the operation froze, so nothing was published | take the context back with bootwright destroy --context <context> and import the image again |
| `install.artifact-server-not-applied` | adapter-run | none | target | - | fix-then-repeat | the artifact server that serves the package tree is not applied on this host, so nothing was published | apply the artifact server, then repeat this apply |
| `install.guest-policy-unmet` | adapter-run | possible | target | - | other-verb | the installed machine lacks the root encryption or FIPS mode its installation set only at install time | take the context back with bootwright destroy --context <context>, then apply again |
| `install.registration-rejected` | adapter-run | possible | input | - | fix-then-repeat | the subscription service or Satellite did not accept the registration the installation declares | correct the registration declaration, then repeat the operation |
| `install.service-unavailable` | adapter-run | possible | environment | `unreachable` | fix-then-repeat | the subscription service, an update repository or the machine's own tooling did not answer | restore the route from the machine to the service, then repeat the operation |
| `install.foreign-record` | adapter-run | possible | target | - | other-verb | the machine holds a subscription registration or update record of another installation | take the context back with bootwright destroy --context <context> and apply again |
| `install.update-refused` | adapter-run | possible | input | - | fix-then-repeat | the update sources cannot hold the machine within the release its installation declares | correct the update sources of the install profile, then apply again |
| `install.update-failed` | adapter-run | possible | target | - | fix-then-repeat | the package transaction of the machine's update failed | repeat the operation once the update sources offer a consistent transaction |

## Ceph stops

The Ceph storage capability's stops (`internal/storage/ceph`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ceph.request-invalid` | block | none | defect | - | none | a Ceph request, its frozen identity or its planning state breaks its bounds or its shape, so no adapter ran | report the message as a defect |
| `ceph.unconfigured` | block | none | defect | - | none | a Ceph capability runs without an adapter, a proof reader or a bound Secret it needs | report the message as a defect |
| `ceph.records-missing` | block | none | records | - | abandon-context | a Ceph block holds no original apply, identity or proof that its operation needs, or holds an ambiguous one | restore the context store from a backup |
| `ceph.evidence-mismatch` | block | possible | records | - | repeat | the evidence a Ceph block holds names another request or does not prove what the block froze | repeat the operation, which observes the Ceph state again |
| `ceph.proof-mismatch` | block | none | records | - | fix-then-repeat | the proof of an earlier Ceph block names another request, operation or host set than the block that reads it | restore the original apply evidence |
| `ceph.other-build` | block | none | build | - | registering-build | a Ceph block froze values this build does not derive or read, so no adapter ran | destroy it with the build that applied it, then apply with this build |
| `ceph.host-key-invalid` | block | none | records | - | fix-then-repeat | the SSH host key a Ceph host delivered or its Secret pins is not one usable entry | bind the host's key through its access.ssh.knownHostsRef, then repeat the operation |
| `ceph.disks-shared` | block | none | target | - | change-input | physical Ceph hosts report one disk identity, so an OSD service would claim a disk shared by hosts | select independently owned local disks |
| `ceph.fips-unproved` | adapter-run | possible | target | - | fix-then-repeat | a Ceph host no longer proves the installed FIPS kernel and crypto policy the operation froze | restore the installed FIPS policy |
| `ceph.unsupported-release` | block | none | input | - | change-input | the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe, so no adapter ran | declare bounded version strings, an image repository and verifiedTunnel transport |
| `ceph.license-unaccepted` | block | none | input | - | change-input | the IBM license of the Ceph entitlement has not been accepted, so no adapter ran | review the provider license and declare Entitlement.spec.license.accept |
| `ceph.image-invalid` | block | none | input | - | change-input | a Ceph image or its registry does not match the entitlement or the digest the input authored | select an image beneath the Entitlement registry by a tag or a sha256 digest |
| `ceph.unsupported-profile` | block | none | input | - | change-input | the requested distribution, trust profile or kind is one the core Ceph adapter does not realize | select the IBM Storage Ceph distribution and the core profile |
| `ceph.unsupported-topology` | block | none | input | - | change-input | the hosts, SSH keys, addresses or provided Machines a Ceph cluster selects are not a topology the core adapter realizes | declare a topology the core Ceph profile supports |
| `ceph.unsupported-service` | block | none | input | - | change-input | a native Ceph service selects a type, option, placement or binding the core adapter does not realize | declare only the services and options the core Ceph profile supports |
| `ceph.unsupported-disk` | block | none | input | - | change-input | an OSD service or disk selection is not one the core adapter allocates | select explicit owned data disks the core Ceph profile supports |
| `ceph.unsupported-pool` | block | none | input | - | change-input | a Ceph pool, placement policy or consumer names a setting or pool kind the core adapter does not realize | select a replicated pool and the settings the core Ceph profile supports |
| `ceph.package-source-unsupported` | block | none | input | - | change-input | the package source a Ceph host selects is not one the core adapter can acquire packages from | select the hosted DVD or subscription profile used to install each Ceph Machine |
| `ceph.budget-exceeded` | block | none | bound | - | change-input | a Ceph operation needs a run deadline past the bound Ceph holds its derived deadlines to, so no adapter ran | declare fewer hosts, namespaces or initiator hosts |
| `ceph.retirement-refused` | removal-phase | none | input | - | abandon-context | a physical Ceph host holds no disk-erasure authority that covers every selected disk, so its retirement took nothing back | automatic retirement needs every selected data, DB and WAL disk in a hardware.dataDisks entry of each Machine, as originally applied, with a non-retain onRemoval; without it only bootwright context delete --name <context> --purge --allow-orphans remains, which abandons what the context owns |
| `ceph.retirement-unavailable` | operation | none | input | - | change-input | a warning: the planned Ceph cluster would refuse automatic retirement, because it has a physical Ceph consumer or a selected physical disk that the frozen erase policy cannot retire | review the original erase policy and cluster consumers before apply |
| `ceph.hosts-survive` | removal-phase | none | target | - | fix-then-repeat | a Ceph cluster would be removed while hosts that outlive the destroy still run it: Bootwright-owned virtual hosts, physical hosts whose original erase policy and verified erase receipt are missing, or a physical CephConsumer | remove the cluster in the same destroy as every Bootwright-owned virtual host Machine it runs on; for physical hosts, supply the original erase policy of every host and its terminal verified erase receipt; retain the context while a physical CephConsumer uses the cluster |
| `ceph.secret-invalid` | block | none | input | - | change-input | a Secret a CephSMBResource binds, or a CephObjectGateway's authentication names directly or through its LDAPServer, is not a value the adapter can lend | bind a Secret holding what the refusal names: one line of 1 to 1024 bytes of UTF-8 text for an SMB password, one line of 1 to 1023 bytes without leading or trailing whitespace for an LDAP bind password, parsable PEM certificates for an LDAP trust bundle, or exactly 16 letters and digits for an STS key |
| `ceph.stretch-intent-changed` | adapter-run | none | input | - | change-input | the stretch topology the input declares differs from the intent the cluster was created with | restore the stretch topology the cluster was created with |
| `ceph.ssh-policy-excluded` | adapter-run | none | input | - | change-input | an SSH crypto policy admits none of the algorithms of a Ceph host's pinned host key, so no session opened | pin a host key the SSH crypto policy admits |
| `ceph.ssh-policy-unreadable` | adapter-run | none | environment | - | fix-then-repeat | the controller's SSH crypto policy could not be read or evaluated, so no Ceph session opened | restore the controller's SSH crypto policy files, then repeat the operation |
| `ceph.device-refused` | adapter-run | possible | target | - | fix-then-repeat | a disk a Ceph host selects for an OSD is not one the operation may allocate, so the adapter stopped | select empty disks that the host presents by their declared identity |
| `ceph.unqualified` | adapter-run | possible | build | - | fix-then-repeat | the selected Ceph build or host lacks a capability the operation needs, so the adapter stopped | select a Ceph build whose release qualification covers the operation |
| `ceph.identity-foreign` | adapter-run | unproved | target | - | fix-then-repeat | the cluster holds a resource of a declared name that this operation did not create | rename the declared resource, or remove the foreign one outside Bootwright, then apply again |
| `ceph.identity-drifted` | adapter-run | unproved | target | - | fix-then-repeat | a resource this operation created is gone or no longer matches what the operation froze | restore the resource to its declared state outside Bootwright, then apply again |
| `ceph.prerequisite` | adapter-run | unproved | target | - | fix-then-repeat | the cluster or its hosts lack a state the operation needs before it may continue | restore the state the message names, then apply again |
| `ceph.not-ready` | adapter-run | unproved | target | - | fix-then-repeat | a native Ceph command refused the operation or its daemons did not reach readiness within the bound | correct the cause the native error names outside Bootwright, then apply again |
| `ceph.osd-unproved` | adapter-run | unproved | target | - | fix-then-repeat | an OSD disk this operation prepared or cleared did not prove its state | inspect the disk with wipefs --no-act and lvmdevices, then apply again |
| `ceph.package-unproved` | adapter-run | unproved | target | - | fix-then-repeat | the Ceph package preparation of a host refused its request, source or result, so the host's packages are unproved | inspect the host's package journal and the declared package sources, then apply again |
| `ceph.boundary-unproved` | adapter-run | unproved | target | - | repeat | an adapter could not prove one of its boundaries, so what it did is unproved | inspect the retained operation evidence, then repeat the operation |
| `ceph.enrollment-budget` | adapter-run | unproved | bound | - | repeat | the enrollment worker had too little of its budget left to admit a host | apply again to continue the prepared cluster enrollment |
| `ceph.consumer-incomplete` | adapter-run | unproved | target | - | repeat | a Ceph consumer created nothing yet or began and did not complete | repeat the operation, which observes the consumer before anything else starts |
| `ceph.unproved` | block | unproved | target | - | repeat | a Ceph adapter ended without proving its postcondition | repeat the operation, which observes the Ceph state before anything else starts |
| `ceph.result-invalid` | block | unproved | defect | - | repeat | a Ceph adapter returned an outcome or a value it never produces | repeat the operation, which observes the Ceph state before anything else starts |
| `ceph.evidence-unproved` | block | unproved | target | - | repeat | the evidence of a Ceph block does not prove the ownership and resource scope the block froze | repeat the operation, which observes the Ceph state before anything else starts |
| `ceph.tiebreaker-unproved` | block | unproved | target | - | repeat | a provided stretch tiebreaker Machine did not prove its Ceph footprint, its transport boundary or its removal | repeat the operation, which observes the tiebreaker before anything else starts |
| `ceph.sign-on-unaddable` | operation | none | input | - | change-input | a CephSignOn added to a completed apply names a cluster that apply did not install with a management gateway and an oauth2-proxy image, or one whose lineage already holds a sign-on, so nothing was added | declare it on a cluster that apply installed with a mgmt-gateway service and spec.images.oauth2_proxy, or take the apply back and apply again |

## Media store stops

The media store's stops (`internal/managedos/media`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `media.store-busy` | operation | possible | concurrency | - | wait | another Bootwright command holds the media store, so an image was verified and retained but not published | repeat the command once the other command finishes, which publishes the retained image |

## Artifact server stops

The managed artifact server's stops (`internal/infrastructureservices/artifactserver`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `artifact.context-invalid` | operation | none | defect | - | none | the context identity the managed artifact servers are derived under is not usable, so nothing was planned | report the message as a defect |
| `artifact.name-unsafe` | operation | none | input | - | change-input | an artifact server's name cannot name its unit, container or directory on the host | rename the artifact server the message names |
| `artifact.server-unmanaged` | operation | none | input | - | change-input | a consumer selected an artifact server that is not managed, and an external server is nobody's to publish into | select a managed artifact server |
| `artifact.private-unverified` | operation | none | input | - | change-input | material only one machine may read is published at an endpoint that is not served over a verified connection | select an https endpoint of the artifact server |
| `artifact.listeners-mismatch` | block | possible | records | - | repeat | the evidence an artifact server returned does not cover or match the listeners its request froze | repeat the operation, which observes the artifact server again |
| `artifact.listener-unanswered` | block | possible | target | - | repeat | an artifact server's listener did not answer with an HTTP status line | repeat the operation, which observes the artifact server again |
| `artifact.certificate-evidence-invalid` | block | possible | records | - | repeat | an artifact server's listener reported a certificate it should not have, or none it should have | repeat the operation, which observes the artifact server again |
| `artifact.certificate-mismatch` | block | possible | target | - | fix-then-repeat | an artifact server's listener presented a different certificate than the operation bound | restore the certificate the operation bound, then repeat the operation |

## Machine placement stops

The Machine placement and access checks' stops (`internal/machine`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `machine.enrollment` | block | possible | input | - | fix-then-repeat | a provided Machine's enrollment could not verify a credential, account, or grant | inspect the named Machine's first-access declaration and retry with its authorized administrator |
| `machine.enrollment-sudo-tty` | block | none | target | - | fix-then-repeat | remote sudo requires a terminal and did not start the enrollment command | have the host administrator authorize sudo without a terminal for this account, then repeat the operation |
| `machine.placement-no-access` | operation | none | input | - | change-input | a Machine that hosts lifecycle work is neither the controller nor declares SSH access, so no work was placed on it | place the work on the controller Machine, or author access.ssh on the Machine |
| `machine.placement-auth-unsupported` | operation | none | input | - | change-input | a Machine that hosts lifecycle work authenticates with an operator identity or a password, which a placement does not support | author access.ssh.auth.privateKeyRef on the Machine |
| `machine.placement-key-missing` | operation | none | input | - | change-input | a Machine that hosts lifecycle work declares no private key reference or, for access.lifecycle, no host pin | author the Machine's private key reference as its message names |
| `machine.placement-host-key-missing` | operation | none | input | - | change-input | a Machine that hosts lifecycle work declares no bound SSH host key | author access.ssh.knownHostsRef on the Machine |
| `machine.placement-escalates` | operation | none | input | - | change-input | a Machine that hosts lifecycle work declares a login whose account and privilege a placement does not admit: an access.ssh account other than root or an escalation Secret, or an access.lifecycle login other than root with direct privilege or a non-root account with sudo | author the Machine's login as its message names |
| `machine.installed-access-mismatch` | operation | none | input | - | change-input | a Machine Bootwright installs declares SSH access other than its installation's own, so no work was placed on it | remove spec.access.ssh from the Machine so its access is the installation's own |

## Machine hardware stops

The Machine hardware capability's stops (`internal/machine/hardware`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `hardware.desired-state-missing` | operation | none | defect | - | none | the machine hardware capability was asked to plan without compiled desired state | report the message as a defect |
| `hardware.request-invalid` | block | none | records | - | registering-build | the frozen machine hardware request names no Machine reached over SSH, so no adapter ran | destroy it with the build that applied it |
| `hardware.request-mismatch` | block | none | defect | - | none | the frozen machine hardware request names another Machine than its block, so no adapter ran | report the message as a defect |
| `hardware.unconfigured` | block | none | defect | - | none | the machine hardware capability runs without its adapter | report the message as a defect |
| `hardware.evidence-malformed` | block | possible | records | - | repeat | the machine hardware evidence breaks its shape or its bounds | repeat the operation, which reads the Machine's hardware again |
| `hardware.evidence-incomplete` | block | possible | target | - | repeat | the machine hardware evidence proves no complete read of its host's permanent hardware addresses | repeat the operation, which reads the Machine's hardware again |
| `hardware.unproved` | block | unproved | target | - | repeat | the machine hardware adapter ended without proving its postcondition | repeat the operation, which observes the effect before anything else starts |
| `hardware.removal-evidence-unencoded` | block | none | defect | - | none | the evidence of the machine hardware removal could not be encoded | report the message as a defect |
| `hardware.addresses-too-many` | block | possible | input | - | change-input | a Machine's host reports more permanent hardware addresses than one claim holds | place the work on a Machine whose host reports fewer physical network interfaces |
| `hardware.addresses-none` | block | possible | input | - | change-input | a Machine's host reports no permanent hardware address, so no claim serializes it against a context that drives it by its BMC | place the work on a Machine whose host has a device-backed Ethernet interface not on USB |

## Controller client stops

The controller's client tools capability's stops (`internal/controller/clients`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `clients.egress-unproved` | block | none | records | - | fix-then-repeat | the completed controller stage does not hold the records that prove the one route its downloads took, so nothing ran | complete the controller stage and retry apply without changing its route |
| `clients.evidence-unencodable` | block | possible | defect | - | none | the controller prerequisites evidence cannot be written within its bounds after the stage ran | use a compatible executable |
| `clients.plan-defect` | operation | none | defect | - | none | the controller prerequisites capability is planned without compiled desired state or without the Machine its block names, so nothing ran | use a compatible executable |
| `clients.publication-missing` | block | none | defect | - | none | the controller prerequisites block runs without the stage capability that publishes its clients, so nothing ran | use a compatible executable |
| `clients.request-unreadable` | block | none | records | - | registering-build | a frozen controller prerequisites request names packages, a part or a mirror trust anchor this build does not select, so no adapter ran | destroy it with the build that applied it |

## Pre-boot stops

The substrate pre-boot checks' stops (`internal/substrate`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `preboot.unrealized-substrate` | operation | none | input | - | change-input | a Machine is placed on a provider whose substrate this executable does not realize, so nothing was planned | place the Machine on a libvirt, bare-metal or KubeVirt provider |
| `preboot.target-unanswered` | adapter-run | unproved | environment | `unreachable` | fix-then-repeat | the management controller did not answer the system read that proves a physical machine, so what it is stays unknown | restore access to the controller, then repeat the apply |
| `preboot.hardware-mismatch` | adapter-run | none | target | - | fix-then-repeat | a physical machine does not report exactly the hardware its Machine declares, its complete Storage inventory does not show exactly one drive matching its root proof, or its inventory could not be read in full, so no media was inserted | make the machine report the hardware its Machine declares, then continue the apply |
| `preboot.identity-mismatch` | adapter-run | none | target | - | change-input | the system answering for a Machine is not the one the operation proved for it, so no media was inserted | correct the declared address of the Machine, or restore the system it was realized on, then continue the apply |
| `preboot.machine-running` | adapter-run | none | target | - | fix-then-repeat | a machine is running while its installation would write its disk, so no media was inserted | stop the machine, then continue the apply |
| `preboot.tpm-absent` | adapter-run | none | target | - | fix-then-repeat | an encrypted installation has no positive proof of a TPM 2.0, so no encrypted installer was booted | provide a TPM 2.0 for the machine, then continue the apply |
| `preboot.firmware-mismatch` | adapter-run | none | target | - | fix-then-repeat | a machine does not report the firmware settings its Machine asks to prove, so no media was inserted | set the declared firmware values through the controller, then continue the apply |
| `preboot.firmware-not-applied` | adapter-run | possible | target | - | fix-then-repeat | the controller refused or did not apply the firmware settings its Machine asks to enforce, and a setup boot or settings it staged may remain | correct the cause through the controller, then continue the apply |

## Cluster object stops

The cluster objects add-on driver's stops (`internal/addons/clusterobjects/driver`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `clusterobjects.runtime-unavailable` | block | none | target | - | fix-then-repeat | the cluster object runtime or the custody of the cluster's produced input is not available, so no adapter ran | apply the controller and cluster stages first |
| `clusterobjects.identity-unavailable` | block | none | defect | - | none | the apply that owns the cluster objects named no ownership identity, so no adapter ran | report the message as a defect |
| `clusterobjects.operation-unsupported` | block | none | defect | - | none | a cluster object run was asked for an operation other than apply or observe, so no adapter ran | report the message as a defect |
| `clusterobjects.placement-unbound` | block | none | input | - | fix-then-repeat | the Secret a placement copies into the cluster is not bound to the operation, so no adapter ran | restore its Secret binding or apply the export producer first |
| `clusterobjects.placement-unmapped` | block | none | input | - | change-input | a Secret placement does not map exactly the parts of the Secret it copies, so no adapter ran | correct the package part mapping |
| `clusterobjects.evidence-mismatch` | block | possible | records | - | repeat | the evidence a cluster object adapter returned names another request or version | repeat the operation, which observes the objects again |
| `clusterobjects.evidence-unsafe` | block | possible | defect | - | repeat | the evidence a cluster object adapter returned carries native values that may not be kept | repeat the operation, which observes the objects again |

## Add-on stops

The add-on instance capability's stops (`internal/addons/instance`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `addon.binding-invalid` | operation | none | input | - | change-input | an add-on binding, or the add-on it selects, cannot be realized or rendered as declared, so nothing was planned | correct the add-on binding |
| `addon.controller-differs` | operation | none | input | - | change-input | the controller an add-on selects is not the controller of the operation, so nothing was planned | correct the controller selection |
| `addon.facts-unavailable` | operation | none | defect | - | none | the add-on capability was built without the cluster facts its plan reads, so nothing was planned | report the message as a defect |
| `addon.placement-invalid` | operation | none | input | - | change-input | a Secret placement of an add-on names input that is undeclared or does not fit it, so nothing was planned | correct the Secret the add-on's placement names |
| `addon.frozen-other-build` | block | none | build | - | registering-build | the frozen add-on request does not match what this build holds for its package, so no adapter ran | destroy the operation with the build that froze it, or plan again with this build |
| `addon.completion-unproved` | block | unproved | target | - | repeat | the add-on adapter ended without evidence that its objects reached the declared state | repeat apply to observe its objects |
| `addon.outcome-unknown` | block | unproved | defect | - | repeat | the add-on adapter ended without an outcome the block can settle on | repeat apply to observe its objects |
| `addon.addition-undecodable` | operation | none | defect | - | none | an add-on instance added to a completed apply froze a request this build cannot read, so nothing was added | report the message as a defect |
| `addon.cluster-not-installed` | operation | none | input | - | change-input | an add-on added to a completed apply names a cluster that apply did not install, so nothing was added | declare it on a cluster that apply installed, or take the apply back and apply again |

## Command stops

The command line's stops (`internal/cli`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `command.unconfirmed` | operation | none | input | - | authorize | the confirmation an apply or destroy asks for was declined, canceled or could not be asked, so nothing began | after a decline or a non-interactive refusal, review the plan, then repeat the command with --yes; after a cancellation, repeat the command to be asked again |

## Context input stops

The context input checks' stops (`internal/workspace/contexts`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `input.added-since-apply` | context | none | input | - | repeat | a warning: imported input declares more than the completed apply froze, only add-on instances, and the next apply adds it without running its blocks | run bootwright apply --context <context> to add it to that apply |
| `input.changed-since-apply` | context | none | input | - | other-verb | a warning: the context holds a completed apply and imported input changed since it, which apply refuses until a destroy takes back what that apply owns | take the apply back with bootwright destroy --context <context>, then apply again |
| `input.changes-kept` | context | none | input | - | other-verb | a warning: the context rests trimmed and imported input would change a step its staged destroy kept, declares nothing it took back, or declares a step none took back, which the restoring apply refuses | follow the remedy the message names, then apply |
| `input.restore-unchecked` | context | none | input | - | repeat | a warning: the context rests trimmed and the restoring apply's comparison of the imported input could not run | review the restore with bootwright apply --dry-run --context <context>, which names the cause |

## Context store stops

The context store's stops (`internal/workspace/contextfs`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `contextfs.runs-exhausted` | operation | none | bound | - | fix-then-repeat | the numbers a context's run logs are named by are used up, so no run was started | remove the run log directory the message names, then repeat the command |

## Record stops

The reconciliation records' and plans' stops (`internal/reconciliation`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `records.request-unwritable` | operation | none | defect | - | none | a request a plan composed cannot be frozen into its canonical bytes, so nothing ran | report the message as a defect |
| `records.request-unreadable` | block | none | records | - | registering-build | a frozen request is not one this build reads back as it was written, so no adapter ran | destroy it with the build that applied it |
| `records.version-unsupported` | block | none | build | - | registering-build | a frozen request names a version this build does not read, so no adapter ran | destroy it with the build that applied it |
| `records.evidence-unreadable` | block | possible | defect | - | none | the evidence an adapter returned is empty, unbounded or not the one shape its capability declares | report the message as a defect |
| `records.state-unrecognized` | block | possible | defect | - | none | a lifecycle record, number or identity holds a value this build does not recognize | report the message as a defect |
| `records.plan-invalid` | operation | none | defect | - | none | a lifecycle plan, lineage or block breaks the shape or the bounds a plan holds, so nothing ran | report the message as a defect |
| `records.lineage-reshaped` | operation | none | input | - | other-verb | an additive apply would freeze a block of the applied lineage differently, so nothing ran | take the apply back with bootwright destroy --context <context>, then apply the changed input |
| `records.plan-too-large` | operation | none | bound | - | change-input | a lifecycle plan exceeds its block count or its aggregate request size, so nothing ran | declare fewer objects in the context, or split them across contexts |
| `records.block-identity-clash` | operation | none | input | - | change-input | two objects of different kinds name one block, so the plan cannot tell them apart and nothing ran | rename one of the two objects so their block identities differ |
| `records.dependency-cycle` | operation | none | input | - | change-input | the blocks of a plan wait for one another in a cycle, so no order exists and nothing ran | remove one of the references that order these blocks after one another |

## Operation record stops

The operation store's stops (`internal/reconciliation/operationstore`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `operations.record-invalid` | operation | possible | records | - | abandon-context | an operation record of the context, or the record this build was to write, is malformed, over its bound or not valid | restore the context's records from a matching backup, or delete the context with what it may still own |
| `operations.defect` | block | none | defect | - | none | the operation store was driven against a precondition its contract states, so the call recorded nothing | report the message as a defect |
| `operations.bound` | operation | none | bound | - | change-input | a plan or record this build was to write exceeds its encoded size bound, so the call recorded nothing | declare fewer objects in the context, or split them across contexts, then repeat the command |
| `operations.record-missing` | operation | none | records | - | abandon-context | an operation the context names has no durable record to read | restore the context's records from a matching backup, or delete the context with what it may still own |
| `operations.plan-mismatch` | operation | none | records | - | abandon-context | the frozen plan the context holds is not the plan its operation recorded | restore the context's records from a matching backup, or delete the context with what it may still own |
| `operations.record-other-build` | operation | none | build | - | registering-build | a record of the context names another build as its writer, and this build cannot read it | continue or remove the operation with the build that wrote the record |
| `operations.record-unreadable` | operation | none | records | - | registering-build | the record of an operation fails closed decoding or is not canonical, so status cannot read it | inspect and remove it with the build that registered it, then delete its context; when this build registered it, restore the whole context store from a matching backup |
| `operations.plan-other-build` | operation | none | build | - | registering-build | the frozen plan of an operation is not one this build could have produced | destroy the context with the build that registered the operation |
| `operations.retention-full` | operation | none | bound | - | other-verb | the context has retained the most operations it keeps, or its operation area cannot hold a new one, so none was registered | take back what the context owns with destroy, or delete the context and create it again |
| `operations.area-reserved` | operation | none | bound | - | other-verb | the operation area keeps its remaining room for the removal of the apply, so it cannot hold another attempt | take back what the context owns with destroy, or delete the context and create it again |

## Context lock stops

The context root lock's and lease's stops (`internal/workspace/contextfs/kernel`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `context.busy` | context | possible | concurrency | - | wait | another Bootwright command holds the context's root lock or lease, so this command stopped where it needed it | repeat the command once the other command on the context has finished |

## Log stops

The `bootwright logs` reader's stops and warnings (`internal/reconciliation/logview`).

| Code | Level | Certainty | Cause | Fault | Recovery | Meaning | Remedy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `logs.not-found` | context | none | records | - | change-input | the logs a reading names, or every operation of the context, no longer exist in the records or retained logs | run bootwright status --context <context> to see the operation records that remain |
| `logs.detached` | context | none | concurrency | - | other-verb | a warning that ends a followed log: its operation still reads running, but no apply or destroy holds its context | continue the operation with the apply or destroy the message names |
| `logs.truncated` | context | none | bound | - | none | a warning: an attempt's retained output was cut short at its bound, so it holds less than the adapter printed | read the block's events with bootwright logs, which no bound cuts short |
| `logs.wiring-missing` | context | none | defect | - | repeat | the log reader was wired without a part the reading needs, so nothing was read | repeat the command; a repeated failure is a Bootwright defect to report |
| `logs.no-context-selected` | context | none | input | - | change-input | the reading names no context and none is selected | select one with bootwright context use --name <context>, or name it with --context <context> |

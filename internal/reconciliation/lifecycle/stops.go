package lifecycle

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const storeFilesystems = "the filesystems that hold /var/lib/bootwright and /var/log/bootwright"

var (
	engineRecordsFull = diagnostics.Stop{Code: "engine.records-full", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block stopped because a filesystem holding the context's records or logs had no room",
		Remedy:  "free space, or raise the quota, on " + storeFilesystems + ", then repeat the operation"}
	engineRecordsReadOnly = diagnostics.Stop{Code: "engine.records-read-only", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block stopped because a filesystem holding the context's records or logs is read-only",
		Remedy:  "make " + storeFilesystems + " writable, then repeat the operation"}
	engineRecordsDenied = diagnostics.Stop{Code: "engine.records-denied", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block stopped because a record or log of the context could not be accessed",
		Remedy:  "run the command as the invoking user through its elevation, and check the ownership of " + storeFilesystems + ", then repeat the operation"}
	engineRecordsDevice = diagnostics.Stop{Code: "engine.records-device", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block stopped because a record or log write met a device error",
		Remedy:  "inspect " + storeFilesystems + " (the kernel log names the device), then repeat the operation"}
	engineEscaped = diagnostics.Stop{Code: "engine.escaped", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a block stopped on a failure that escaped a typed boundary; its own words are never shown, since they may name a private path",
		Remedy:  "repeat the operation; report the failure if it stops the same way"}
	engineStartUnrecorded = diagnostics.Stop{Code: "engine.start-unrecorded", Level: diagnostics.LevelAttempt, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an attempt could not record its start, so it performed no effect", Remedy: "repeat the operation"}
	applyUnresolved = diagnostics.Stop{Code: "apply.unresolved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the apply ended with an effect whose outcome is unproved", Remedy: "repeat the apply, which observes that effect before anything else starts"}
	destroyUnresolved = diagnostics.Stop{Code: "destroy.unresolved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the destroy ended with an effect whose outcome is unproved", Remedy: "repeat the destroy, which observes that effect before anything else starts"}
	applyIncomplete = diagnostics.Stop{Code: "apply.incomplete", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the apply ended with blocks not done; the diagnostics before it say why", Remedy: "continue the apply once each cause is fixed"}
	destroyIncomplete = diagnostics.Stop{Code: "destroy.incomplete", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseUnclassified, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the destroy ended with blocks not proved gone; the diagnostics before it say why", Remedy: "repeat the destroy once each cause is fixed"}
	startPowerRefused = diagnostics.Stop{Code: "lifecycle.start-power", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a new apply or destroy could not prove every managed machine powered off or absent", Remedy: "stop the named machines or restore access to their power state and repeat the command"}
	destroyInUse = diagnostics.Stop{Code: "destroy.in-use", Level: diagnostics.LevelRemoval, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a removal would take back state that is still in use, so it takes back nothing at that boundary",
		Remedy:  "stop what the message names, then repeat the destroy"}
	destroyConsumerSurvives = diagnostics.Stop{Code: "destroy.consumer-survives", Level: diagnostics.LevelRemoval, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a removal would retire what a consumer that outlives it still uses",
		Remedy:  survivingConsumerRemedy}
	engineFinalizationDue = diagnostics.Stop{Code: "engine.finalization-due", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyOwed,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an operation is due its finalization, which no decision may skip", Remedy: "repeat the command, which finalizes that operation before it decides"}
	engineRecordsContradict = diagnostics.Stop{Code: "engine.records-contradict", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an incomplete operation holds records that contradict what it started", Remedy: "read the records with bootwright status, then restore them from a backup or delete the context"}
	engineOtherVerbIncomplete = diagnostics.Stop{Code: "engine.other-verb-incomplete", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "an incomplete operation of the other verb must be continued before another operation", Remedy: "continue or replace the incomplete operation the message names"}
	applyRecordsMissing = diagnostics.Stop{Code: "apply.records-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "a completed apply records no completion for blocks it froze, so nothing proves its input applied", Remedy: "restore the context's records, or take back what it owns and apply again"}
	destroyRecordsMissing = diagnostics.Stop{Code: "destroy.records-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "a completed destroy records no completion for blocks it froze, so nothing proves their effects removed", Remedy: "restore the context's records, or delete the context with what it may still own"}
	applyInputChanged = diagnostics.Stop{Code: "apply.input-changed", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the desired state changed after the apply completed, and it is no addition to it", Remedy: "take back what the context owns with destroy, then apply the changed input"}
	applyNotAnAddition = diagnostics.Stop{Code: "apply.not-an-addition", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a changed input over a completed apply adds something an additive apply cannot take", Remedy: "take back what the context owns with destroy, then apply the changed input"}
	applyPrerequisiteLost = diagnostics.Stop{Code: "apply.prerequisite-lost", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a block an addition needs, held by an earlier apply of its lineage, is gone or no longer done", Remedy: "take back what the lineage owns with destroy"}
	applyNothingDeclared = diagnostics.Stop{Code: "apply.nothing-declared", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the selected Environment declares nothing this executable would create", Remedy: "declare a managed infrastructure service"}
	applyStageEmpty = diagnostics.Stop{Code: "apply.stage-empty", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the selected stages or the --until instance have nothing to start", Remedy: "repeat the apply with a wider selection"}
	applyStageExcludesRetry = diagnostics.Stop{Code: "apply.stage-excludes-retry", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the block the operation must retry first is outside the selected stages", Remedy: "repeat the apply with a selection that holds that block"}
	applyUntilUnknown = diagnostics.Stop{Code: "apply.until-unknown", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "--until names no add-on instance of the plan", Remedy: "name an add-on instance the plan holds, or repeat without --until"}
	planInputUncompilable = diagnostics.Stop{Code: "plan.input-uncompilable", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the context's frozen desired state could not be compiled", Remedy: "repair the input with validate and import it again"}
	planCapabilityMissing = diagnostics.Stop{Code: "plan.capability-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryNone,
		Meaning: "no lifecycle capability of this build plans the selected state", Remedy: "use a build that carries the capability; report it if this build should"}
	planCapabilityDefect = diagnostics.Stop{Code: "plan.capability-defect", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a capability planned a block it may not plan", Remedy: "report the message as a defect"}
	planHostKeyUnbound = diagnostics.Stop{Code: "plan.host-key-unbound", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an SSH placement binds no host key, or reaches the controller where its pinned key does not apply", Remedy: "author access.ssh.knownHostsRef on the Machine the message names"}
	planEnvironmentInvalid = diagnostics.Stop{Code: "plan.environment-invalid", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the selected state holds no single Environment naming its controller Machine", Remedy: "repair the input with validate"}
	engineUnindexedRecords = diagnostics.Stop{Code: "engine.unindexed-records", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the context holds operation records or evidence its index names no operation for", Remedy: "restore the context's records, or delete the context with what it may still own"}
	engineRequestDefect = diagnostics.Stop{Code: "engine.request-defect", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a capability declared or requested something the engine refuses to compose", Remedy: "report the message as a defect"}
	engineOtherBuild = diagnostics.Stop{Code: "engine.other-build", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "this executable cannot prove it would continue or remove the operation as the build that registered it", Remedy: "repeat the command with the build that registered the operation"}
	enginePlanMoved = diagnostics.Stop{Code: "engine.plan-moved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the context changed between the plan and the transition, so nothing was registered", Remedy: "repeat the command to plan from what the context holds now"}
	engineInputMoved = diagnostics.Stop{Code: "engine.input-moved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the context's input is no longer the revision the operation froze", Remedy: "restore the exact input revision the operation froze"}
	engineBindingLost = diagnostics.Stop{Code: "engine.binding-lost", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "a Secret binding the operation froze is lost, so its material cannot be lent again", Remedy: "restore the context's keyring from a complete backup, or delete the context and remove what it named by hand"}
	authorizeUnneeded = diagnostics.Stop{Code: "authorize.unneeded", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "--authorize names a token the plan does not require", Remedy: "repeat the command without that --authorize"}
	authorizeMissing = diagnostics.Stop{Code: "authorize.missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryAuthorize,
		Meaning: "the plan has consequences no --authorize grants", Remedy: "review the plan, then repeat the command with the authorization it names"}
	authorizeStale = diagnostics.Stop{Code: "authorize.stale", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryAuthorize,
		Meaning: "an --authorize digest names steps the plan no longer holds, or an unattended run binds none", Remedy: "review the plan, then repeat the command with the digest it prints"}
	authorizeRebindInvalid = diagnostics.Stop{Code: "authorize.rebind-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an --authorize rebind names a Secret the operation cannot rebind", Remedy: "repeat the command naming each rotated access credential once as --authorize rebind=<secret>"}
	destroyStageSelected = diagnostics.Stop{Code: "destroy.stage-selected", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the next operation is a destroy, which accepts no stage selection", Remedy: "repeat the command without --stage and --until"}
	applyTargetBeforePreparation = diagnostics.Stop{Code: "apply.target-before-preparation", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a scoped apply cannot freeze its installation plan before inventory outside its targets exists", Remedy: "complete the named machine inventories and review the scoped apply again"}
	applyUntilBeforePreparation = diagnostics.Stop{Code: "apply.until-before-preparation", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "the add-on boundary --until names cannot be planned before preparation completes", Remedy: "run apply to complete preparation, then select --until"}
	engineRecordsDamaged = diagnostics.Stop{Code: "engine.records-damaged", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the context's operation records do not form the lineage or removal the operation needs", Remedy: "restore the context store from a backup, or delete the context with what it may still own"}
	applyProducedInputMissing = diagnostics.Stop{Code: "apply.produced-input-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block borrows an output its producer has not proved, or no longer offers", Remedy: "complete the producer block, then repeat the operation"}
	engineRecaptureMismatch = diagnostics.Stop{Code: "engine.recapture-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an output a block recaptured presents another identity than the one custody holds", Remedy: "find what replaced the effect the block proved; the context's records name the original"}
	engineResolvedUnperformed = diagnostics.Stop{Code: "engine.resolved-unperformed", Level: diagnostics.LevelResolution, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an observation proved a frozen effect never performed, or left part done, so the block is failed and retried", Remedy: "repeat the operation, which retries the block"}
	destroyPostconditionUnknown = diagnostics.Stop{Code: "destroy.postcondition-unknown", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a retried removal read its target again and still could not tell its postcondition, so it performed no effect", Remedy: "restore the target the removal reads, then repeat the destroy"}
	prepareRefused = diagnostics.Stop{Code: "prepare.refused", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a preparation cannot start or advance over the context's current operation or input", Remedy: "finish or take back the current operation, or restore the input the preparation recorded"}
	prepareUnsupported = diagnostics.Stop{Code: "prepare.unsupported", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryNone,
		Meaning: "this executable registers no preparation for the requested owner", Remedy: "use a build that registers the preparation"}
	engineBorrowedSSH = diagnostics.Stop{Code: "engine.borrowed-ssh", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a lifecycle operation was given borrowed SSH credentials, which it never uses", Remedy: "remove the --ssh flags and author each Machine's own access"}
	engineConfirmationRequired = diagnostics.Stop{Code: "engine.confirmation-required", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryAuthorize,
		Meaning: "the operation needs a confirmation the invocation cannot ask for", Remedy: "repeat the command on a terminal, or with --yes once the plan is reviewed"}
	engineInputInvalid = diagnostics.Stop{Code: "engine.input-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an invocation option holds a value the engine refuses", Remedy: "repeat the command with the value the message names"}
	engineCredentialMoved = diagnostics.Stop{Code: "engine.credential-moved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a cluster administrator credential changed while its update was decided or pending", Remedy: "repeat the credential update against the current context"}
	engineFinalizationIncomplete = diagnostics.Stop{Code: "engine.finalization-incomplete", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyOwed,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an operation's effects are complete and its finalization is not", Remedy: "repeat the command, which finishes the finalization"}
	destroyReleaseIncomplete = diagnostics.Stop{Code: "destroy.release-incomplete", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyOwed,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the removal completed, and releasing what it owned did not", Remedy: "repeat the destroy to finish it"}
	destroyUnprovedEffects = diagnostics.Stop{Code: "destroy.unproved-effects", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a removal cannot prove what some effects left behind, so it registered nothing", Remedy: "do what each block's diagnostic reports, then repeat the destroy"}
	engineOutcomeUnknown = diagnostics.Stop{Code: "engine.outcome-unknown", Level: diagnostics.LevelResolution, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a block's outcome is still unknown after an observation, for the reason the message names", Remedy: "do what the reason names, then repeat the command to observe the block again"}
	authorizeRelaxationChanged = diagnostics.Stop{Code: "authorize.relaxation-changed", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an operation continues only under the erase relaxations its plan froze, and the invocation names others", Remedy: "continue it naming the relaxations its plan froze, or replace it with a fresh destroy"}
	diffContextMoved = diagnostics.Stop{Code: "diff.context-moved", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the context changed while diff read it", Remedy: "repeat the diff"}
	engineCredentialUnapplied = diagnostics.Stop{Code: "engine.credential-unapplied", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "an administrator credential update names a cluster no completed apply installed and proved", Remedy: "complete the cluster's apply before updating its credentials"}
	engineCredentialClusterUnknown = diagnostics.Stop{Code: "engine.credential-cluster-unknown", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an administrator credential update names a cluster the context does not select", Remedy: "bootwright cluster info lists the selected clusters"}
	engineCredentialLoginInapplicable = diagnostics.Stop{Code: "engine.credential-login-inapplicable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a credential update names a login the cluster does not have", Remedy: "bootwright cluster info --name <cluster> shows its logins"}
	engineCredentialExternal = diagnostics.Stop{Code: "engine.credential-external-cluster", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an administrator credential update names an external cluster, whose credentials its own administrator keeps", Remedy: "update the credentials through the external cluster's administrator"}
	engineCredentialCustody = diagnostics.Stop{Code: "engine.credential-custody-unproved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the custody of a cluster administrator credential is missing, unproved or invalid, so its update changed nothing", Remedy: "restore the original credential custody"}
	engineCredentialPending = diagnostics.Stop{Code: "engine.credential-update-pending", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "an administrator credential update was left pending, so custody holds no credential proved current", Remedy: "finish the retained update with bootwright cluster update-admin-creds"}
	engineCredentialPreparationInvalid = diagnostics.Stop{Code: "engine.credential-preparation-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the cluster's read-only preparation of an administrator credential update was not proved or returned no valid update, so nothing changed", Remedy: "check the cluster's original administrator account"}
	engineCredentialUpdateUnproved = diagnostics.Stop{Code: "engine.credential-update-unproved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the adapter that updates a cluster administrator credential ended without proving the update", Remedy: RepeatAdminCredentialUpdate}
	engineControllerUnprepared = diagnostics.Stop{Code: "engine.controller-unprepared", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "this host has no completed controller setup, so nothing ran", Remedy: "run bootwright install"}
	engineControllerUnbound = diagnostics.Stop{Code: "engine.controller-unbound", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the context is not bound to the controller host it runs on, so nothing ran", Remedy: "run bootwright install to record the controller binding and host evidence"}
	engineControllerOtherHost = diagnostics.Stop{Code: "engine.controller-other-host", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "this host is not the controller host the context is bound to, or the recorded binding does not match it, so nothing ran", Remedy: "run the command on the host the context is bound to"}
	engineRuntimeUnprepared = diagnostics.Stop{Code: "engine.runtime-unprepared", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the controller runtime for this build is not prepared on this host and could not be rebuilt, so nothing ran", Remedy: "run bootwright install"}
	engineClientNotInstalled = diagnostics.Stop{Code: "engine.client-not-installed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a controller client the work needs is not installed by the context's controller stage, so nothing ran", Remedy: "apply the context's controller stage, then repeat the command"}
	engineWiringMissing = diagnostics.Stop{Code: "engine.wiring-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "this build was wired without a part the command needs, so nothing ran", Remedy: "repeat the command; a repeated failure is a Bootwright defect to report"}
	engineLogUnmaintained = diagnostics.Stop{Code: "engine.log-unmaintained", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultStorage, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a required private operation log could not be maintained", Remedy: "restore the context's private log tree under /var/log/bootwright before repeating the operation"}
	engineProducedNameInvalid = diagnostics.Stop{Code: "engine.produced-name-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a block offered produced material under an invalid or repeated name, so custody kept none of it", Remedy: "report the message as a defect"}
	planControllerOrder = diagnostics.Stop{Code: "plan.controller-order", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a block the controller stage waits for runs only after that stage", Remedy: "place the object the message names where the controller stage need not wait for it"}
	planReservationKeyTooLong = diagnostics.Stop{Code: "plan.reservation-key-too-long", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a host reservation key would exceed its bound once keyed by the SSH host it is placed on", Remedy: "reach the Machine by a shorter address, or give the reserving service a shorter name"}
	planHostKeyUnreadable = diagnostics.Stop{Code: "plan.host-key-unreadable", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the SSH host key Secret of a Machine binds no readable host key, so the reservations placed there cannot name the host it pins", Remedy: "author access.ssh.knownHostsRef on the Machine as one known-hosts entry for its SSH host"}
	planSocketConflict = diagnostics.Stop{Code: "plan.socket-conflict", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "two services of the context would listen on one socket of a host", Remedy: "give one of them another bind address or port"}
	planTemplateValue = diagnostics.Stop{Code: "plan.template-value", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a planned request holds a template delimiter or a key ansible-core reserves, which no adapter may take as data", Remedy: "remove it from the desired-state values, import them, then plan again"}
	planObjectUnsupported = diagnostics.Stop{Code: "plan.object-unsupported", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "this executable cannot realize a selected object, for the reason the message names, so nothing was registered", Remedy: "remove the object this executable cannot realize, or use a build that supports it"}
	planConsumerOutlivesDestroy = diagnostics.Stop{Code: "plan.consumer-outlives-destroy", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a warning that a consumer runs where a destroy of this context will not delete it, so that destroy will refuse before registration", Remedy: "run the consumer on Machines this context deletes, or keep this context while the consumer runs"}
	storageHealth = diagnostics.Stop{Code: "storage.health", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryNone,
		Meaning: "a warning: a read-only Ceph observation reported a health check naming a pool or filesystem", Remedy: "resolve the health check through the Ceph cluster"}
	storageNotReady = diagnostics.Stop{Code: "storage.not-ready", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryNone,
		Meaning: "a warning: a read-only Ceph observation completed with checks that kept the block from ready", Remedy: "resolve the checks the message names through the Ceph cluster"}
	substrateShadowedRoute = diagnostics.Stop{Code: "substrate.shadowed-route", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryNone,
		Meaning: "a warning: a libvirt host apply admitted a main-table route broader than a managed network's", Remedy: "narrow the route the message names on the libvirt host"}
	controllerOutdatedClient = diagnostics.Stop{Code: "controller.outdated-client", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a warning: an apply's controller stage proved a client below this build's minimum, which a settling apply and a removal keep running", Remedy: "take the apply back with destroy and apply again to install this build's default client, or keep the build that applied it"}
)

var stops = []diagnostics.Stop{engineRecordsFull, engineRecordsReadOnly, engineRecordsDenied, engineRecordsDevice, engineEscaped, engineStartUnrecorded,
	applyUnresolved, destroyUnresolved, applyIncomplete, destroyIncomplete, startPowerRefused, destroyInUse, destroyConsumerSurvives,
	engineFinalizationDue, engineRecordsContradict, engineOtherVerbIncomplete, applyRecordsMissing, destroyRecordsMissing, applyInputChanged,
	applyNotAnAddition, applyPrerequisiteLost, applyNothingDeclared, applyStageEmpty, applyStageExcludesRetry, applyUntilUnknown,
	planInputUncompilable, planCapabilityMissing, planCapabilityDefect, planHostKeyUnbound, planEnvironmentInvalid,
	engineUnindexedRecords, engineRequestDefect, engineOtherBuild, enginePlanMoved, engineInputMoved, engineBindingLost, authorizeUnneeded,
	authorizeMissing, authorizeStale, authorizeRebindInvalid, destroyStageSelected, applyUntilBeforePreparation, applyTargetBeforePreparation,
	engineRecordsDamaged, applyProducedInputMissing, engineRecaptureMismatch, engineResolvedUnperformed, destroyPostconditionUnknown,
	prepareRefused, prepareUnsupported,
	engineBorrowedSSH, engineConfirmationRequired, engineInputInvalid, engineCredentialMoved, engineFinalizationIncomplete,
	destroyReleaseIncomplete, destroyUnprovedEffects, engineOutcomeUnknown, authorizeRelaxationChanged, diffContextMoved,
	engineCredentialUnapplied, engineCredentialClusterUnknown, engineCredentialLoginInapplicable, engineCredentialExternal, engineCredentialCustody, engineCredentialPending,
	engineCredentialPreparationInvalid, engineCredentialUpdateUnproved, engineControllerUnprepared, engineControllerUnbound, engineControllerOtherHost,
	engineRuntimeUnprepared, engineClientNotInstalled, engineWiringMissing, engineLogUnmaintained, engineProducedNameInvalid,
	planControllerOrder, planReservationKeyTooLong, planHostKeyUnreadable, planSocketConflict, planTemplateValue, planObjectUnsupported,
	planConsumerOutlivesDestroy, storageHealth, storageNotReady, substrateShadowedRoute, controllerOutdatedClient}

var relayedWarnings = []string{storageHealth.Code, storageNotReady.Code, substrateShadowedRoute.Code, "substrate.lend-not-revoked", "substrate.unclaimed-server"}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

func raised(stop diagnostics.Stop, message, remediation string) diagnostics.Diagnostic {
	return diagnostics.Of(stop.Fail(message, remediation))[0]
}

func warned(stop diagnostics.Stop, message, remediation string) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{Severity: "warning", Code: stop.Code, Message: message, Remediation: remediation}
}

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
)

var stops = []diagnostics.Stop{engineRecordsFull, engineRecordsReadOnly, engineRecordsDenied, engineRecordsDevice, engineEscaped, engineStartUnrecorded,
	applyUnresolved, destroyUnresolved, applyIncomplete, destroyIncomplete, destroyInUse, destroyConsumerSurvives,
	engineFinalizationDue, engineRecordsContradict, engineOtherVerbIncomplete, applyRecordsMissing, destroyRecordsMissing, applyInputChanged,
	applyNotAnAddition, applyPrerequisiteLost, applyNothingDeclared, applyStageEmpty, applyStageExcludesRetry, applyUntilUnknown,
	planInputUncompilable, planCapabilityMissing, planCapabilityDefect, planHostKeyUnbound, planEnvironmentInvalid}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

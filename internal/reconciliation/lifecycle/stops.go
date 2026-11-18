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
)

var stops = []diagnostics.Stop{engineRecordsFull, engineRecordsReadOnly, engineRecordsDenied, engineRecordsDevice, engineEscaped, engineStartUnrecorded,
	applyUnresolved, destroyUnresolved, applyIncomplete, destroyIncomplete, destroyInUse, destroyConsumerSurvives}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

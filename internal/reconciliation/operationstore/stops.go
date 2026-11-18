package operationstore

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	operationsRecordInvalid = diagnostics.Stop{Code: "operations.record-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "an operation record of the context, or the record this build was to write, is malformed, over its bound or not valid", Remedy: "restore the context's records from a matching backup, or delete the context with what it may still own"}
	operationsDefect = diagnostics.Stop{Code: "operations.defect", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the operation store was driven against a precondition its contract states, so the call recorded nothing", Remedy: "report the message as a defect"}
	operationsBound = diagnostics.Stop{Code: "operations.bound", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a plan or record this build was to write exceeds its encoded size bound, so the call recorded nothing", Remedy: "declare fewer objects in the context, or split them across contexts, then repeat the command"}
	operationsRecordMissing = diagnostics.Stop{Code: "operations.record-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "an operation the context names has no durable record to read", Remedy: "restore the context's records from a matching backup, or delete the context with what it may still own"}
	operationsPlanMismatch = diagnostics.Stop{Code: "operations.plan-mismatch", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the frozen plan the context holds is not the plan its operation recorded", Remedy: "restore the context's records from a matching backup, or delete the context with what it may still own"}
	operationsRecordOtherBuild = diagnostics.Stop{Code: "operations.record-other-build", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a record of the context names another build as its writer, and this build cannot read it", Remedy: "continue or remove the operation with the build that wrote the record"}
	operationsRecordUnreadable = diagnostics.Stop{Code: "operations.record-unreadable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "the record of an operation is malformed, unsupported or not canonical, so status cannot read it", Remedy: "use the build that registered the operation to inspect and remove it, or restore the whole context store from a matching backup"}
	operationsPlanOtherBuild = diagnostics.Stop{Code: "operations.plan-other-build", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "the frozen plan of an operation is not one this build could have produced", Remedy: "destroy the context with the build that registered the operation"}
	operationsRetentionFull = diagnostics.Stop{Code: "operations.retention-full", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "the context has retained the most operations it keeps, or its operation area cannot hold a new one, so none was registered", Remedy: "take back what the context owns with destroy, or delete the context and create it again"}
	operationsAreaReserved = diagnostics.Stop{Code: "operations.area-reserved", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "the operation area keeps its remaining room for the removal of the apply, so it cannot hold another attempt", Remedy: "take back what the context owns with destroy, or delete the context and create it again"}
)

var stops = []diagnostics.Stop{operationsRecordInvalid, operationsDefect, operationsBound, operationsRecordMissing, operationsPlanMismatch, operationsRecordOtherBuild, operationsRecordUnreadable, operationsPlanOtherBuild,
	operationsRetentionFull, operationsAreaReserved}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

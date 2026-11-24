package reconciliation

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	recordsRequestUnwritable = diagnostics.Stop{Code: "records.request-unwritable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a request a plan composed cannot be frozen into its canonical bytes, so nothing ran", Remedy: "report the message as a defect"}
	recordsRequestUnreadable = diagnostics.Stop{Code: "records.request-unreadable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a frozen request is not one this build reads back as it was written, so no adapter ran", Remedy: "destroy it with the build that applied it"}
	recordsVersionUnsupported = diagnostics.Stop{Code: "records.version-unsupported", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a frozen request names a version this build does not read, so no adapter ran", Remedy: "destroy it with the build that applied it"}
	recordsEvidenceUnreadable = diagnostics.Stop{Code: "records.evidence-unreadable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the evidence an adapter returned is empty, unbounded or not the one shape its capability declares", Remedy: "report the message as a defect"}
	recordsStateUnrecognized = diagnostics.Stop{Code: "records.state-unrecognized", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a lifecycle record, number or identity holds a value this build does not recognize", Remedy: "report the message as a defect"}
	recordsPlanInvalid = diagnostics.Stop{Code: "records.plan-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a lifecycle plan, lineage or block breaks the shape or the bounds a plan holds, so nothing ran", Remedy: "report the message as a defect"}
	recordsLineageReshaped = diagnostics.Stop{Code: "records.lineage-reshaped", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "an additive apply would freeze a block of the applied lineage differently, so nothing ran", Remedy: "take the apply back with bootwright destroy --context <context>, then apply the changed input"}
	recordsPlanTooLarge = diagnostics.Stop{Code: "records.plan-too-large", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a lifecycle plan exceeds its block count or its aggregate request size, so nothing ran", Remedy: "declare fewer objects in the context, or split them across contexts"}
	recordsBlockIdentityClash = diagnostics.Stop{Code: "records.block-identity-clash", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "two objects of different kinds name one block, so the plan cannot tell them apart and nothing ran", Remedy: "rename one of the two objects so their block identities differ"}
	recordsDependencyCycle = diagnostics.Stop{Code: "records.dependency-cycle", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the blocks of a plan wait for one another in a cycle, so no order exists and nothing ran", Remedy: "remove one of the references that order these blocks after one another"}
)

var stops = []diagnostics.Stop{recordsRequestUnwritable, recordsRequestUnreadable, recordsVersionUnsupported, recordsEvidenceUnreadable,
	recordsStateUnrecognized, recordsPlanInvalid, recordsLineageReshaped, recordsPlanTooLarge, recordsBlockIdentityClash, recordsDependencyCycle}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

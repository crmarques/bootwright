package hardware

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	hardwareDesiredStateMissing = diagnostics.Stop{Code: "hardware.desired-state-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the machine hardware capability was asked to plan without compiled desired state", Remedy: "report the message as a defect"}
	hardwareRequestInvalid = diagnostics.Stop{Code: "hardware.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "the frozen machine hardware request names no Machine reached over SSH, so no adapter ran", Remedy: "destroy it with the build that applied it"}
	hardwareRequestMismatch = diagnostics.Stop{Code: "hardware.request-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the frozen machine hardware request names another Machine than its block, so no adapter ran", Remedy: "report the message as a defect"}
	hardwareUnconfigured = diagnostics.Stop{Code: "hardware.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the machine hardware capability runs without its adapter", Remedy: "report the message as a defect"}
	hardwareEvidenceMalformed = diagnostics.Stop{Code: "hardware.evidence-malformed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine hardware evidence breaks its shape or its bounds", Remedy: "repeat the operation, which reads the Machine's hardware again"}
	hardwareEvidenceIncomplete = diagnostics.Stop{Code: "hardware.evidence-incomplete", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine hardware evidence proves no complete read of its host's permanent hardware addresses", Remedy: "repeat the operation, which reads the Machine's hardware again"}
	hardwareUnproved = diagnostics.Stop{Code: "hardware.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine hardware adapter ended without proving its postcondition", Remedy: "repeat the operation, which observes the effect before anything else starts"}
	hardwareRemovalEvidenceUnencoded = diagnostics.Stop{Code: "hardware.removal-evidence-unencoded", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the evidence of the machine hardware removal could not be encoded", Remedy: "report the message as a defect"}
	hardwareAddressesTooMany = diagnostics.Stop{Code: "hardware.addresses-too-many", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine's host reports more permanent hardware addresses than one claim holds", Remedy: "place the work on a Machine whose host reports fewer physical network interfaces"}
)

var stops = []diagnostics.Stop{hardwareDesiredStateMissing, hardwareRequestInvalid, hardwareRequestMismatch, hardwareUnconfigured, hardwareEvidenceMalformed,
	hardwareEvidenceIncomplete, hardwareUnproved, hardwareRemovalEvidenceUnencoded, hardwareAddressesTooMany}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

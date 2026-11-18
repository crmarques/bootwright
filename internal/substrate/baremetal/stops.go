package baremetal

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	baremetalRequestInvalid = diagnostics.Stop{Code: "baremetal.request-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a plan reached the physical Machine capability without compiled state, or a request it composed breaks its bounds or its shape, so no adapter ran", Remedy: "report the message as a defect"}
	baremetalNameUnsafe = diagnostics.Stop{Code: "baremetal.name-unsafe", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a physical Machine's name cannot name a host, so no adapter ran", Remedy: "rename the Machine"}
	baremetalUnconfigured = diagnostics.Stop{Code: "baremetal.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the physical Machine capability runs without its adapter", Remedy: "report the message as a defect"}
	baremetalNoUsableOutcome = diagnostics.Stop{Code: "baremetal.no-usable-outcome", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine adapter ended without an outcome the block can use", Remedy: "repeat the operation, which observes the machine again"}
	baremetalPostconditionUnproved = diagnostics.Stop{Code: "baremetal.postcondition-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine adapter ended without proving the postcondition of its verb", Remedy: "repeat the operation, which observes the machine again"}
	baremetalControllerReport = diagnostics.Stop{Code: "baremetal.controller-report-incomplete", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the management controller reported a fault, or reported no identity or no power state", Remedy: "check the management controller of the Machine, then repeat the operation"}
	baremetalInventoryMismatch = diagnostics.Stop{Code: "baremetal.inventory-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the machine reports an inventory other than the one its Machine declares", Remedy: "check that the machine behind the management controller is the one the Machine declares, then repeat the operation"}
	baremetalAddressesUndeclared = diagnostics.Stop{Code: "baremetal.addresses-undeclared", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a physical Machine declares no hardware address to prove it by", Remedy: "declare the hardware addresses of the Machine in spec.hardware.nics"}
	baremetalClaimUnnameable = diagnostics.Stop{Code: "baremetal.claim-unnameable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "what the machine reports and its Machine declares cannot name one host claim, so no adapter ran", Remedy: "correct the hardware the Machine declares"}
	baremetalEvidenceMismatch = diagnostics.Stop{Code: "baremetal.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the machine evidence names another request or another Machine block than the block froze", Remedy: "repeat the operation, which observes the machine again"}
	baremetalIdentityUnproved = diagnostics.Stop{Code: "baremetal.identity-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the recorded machine evidence proves no identity the context may pin", Remedy: "repeat the operation, which observes the machine again"}
)

var stops = []diagnostics.Stop{baremetalRequestInvalid, baremetalNameUnsafe, baremetalUnconfigured, baremetalNoUsableOutcome, baremetalPostconditionUnproved, baremetalControllerReport, baremetalInventoryMismatch, baremetalAddressesUndeclared, baremetalClaimUnnameable, baremetalEvidenceMismatch, baremetalIdentityUnproved}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

package providedregistration

import "github.com/crmarques/bootwright/internal/diagnostics"

var (
	requestInvalid = diagnostics.Stop{Code: "osregistration.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the provided registration request is invalid, so no adapter ran", Remedy: "report the message as a defect"}
	registrationRefused = diagnostics.Stop{Code: "osregistration.refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Machine's registration or its Satellite did not satisfy the provided registration", Remedy: "check the Machine's registration, its route to the Satellite and the Entitlement's CA bundle, then repeat the operation"}
	registrationUnproved = diagnostics.Stop{Code: "osregistration.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the provided registration adapter ended without proving the registration", Remedy: "repeat the operation to observe the Machine before resuming the registration"}
)

func Stops() []diagnostics.Stop {
	return []diagnostics.Stop{requestInvalid, registrationRefused, registrationUnproved}
}

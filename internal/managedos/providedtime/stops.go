package providedtime

import "github.com/crmarques/bootwright/internal/diagnostics"

var (
	requestInvalid = diagnostics.Stop{Code: "ostime.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the provided time synchronization request is invalid, so no adapter ran", Remedy: "report the message as a defect"}
	timeRefused = diagnostics.Stop{Code: "ostime.refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Machine's time service or configuration did not satisfy the provided time synchronization", Remedy: "check the Machine's time service, its chrony configuration and its route to the declared servers, then repeat the operation"}
	timeUnproved = diagnostics.Stop{Code: "ostime.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the provided time synchronization adapter ended without proving the clock synchronized", Remedy: "repeat the operation to observe the Machine before resuming the time synchronization"}
)

func Stops() []diagnostics.Stop {
	return []diagnostics.Stop{requestInvalid, timeRefused, timeUnproved}
}

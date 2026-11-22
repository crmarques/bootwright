package providedupdate

import "github.com/crmarques/bootwright/internal/diagnostics"

var (
	requestInvalid = diagnostics.Stop{Code: "osupdate.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the provided OS update request is invalid, so no adapter ran", Remedy: "report the message as a defect"}
	updateRefused = diagnostics.Stop{Code: "osupdate.refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Machine or its update source did not satisfy the provided OS update policy", Remedy: "check the Machine's OS release, update repositories and lifecycle access, then repeat the operation"}
	updateUnproved = diagnostics.Stop{Code: "osupdate.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the provided OS update adapter ended without proving the update", Remedy: "repeat the operation to observe the Machine before resuming the update"}
)

func Stops() []diagnostics.Stop {
	return []diagnostics.Stop{requestInvalid, updateRefused, updateUnproved}
}

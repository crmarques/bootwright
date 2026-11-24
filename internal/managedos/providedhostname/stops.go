package providedhostname

import "github.com/crmarques/bootwright/internal/diagnostics"

var (
	requestInvalid = diagnostics.Stop{Code: "oshostname.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the provided OS hostname request is invalid, so no adapter ran", Remedy: "report the message as a defect"}
	hostnameRefused = diagnostics.Stop{Code: "oshostname.refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Machine did not accept the declared hostname", Remedy: "check what manages the Machine's hostname and its lifecycle access, then repeat the operation"}
	hostnameUnproved = diagnostics.Stop{Code: "oshostname.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the provided OS hostname adapter ended without proving the hostname", Remedy: "repeat the operation to observe the Machine before resuming the hostname"}
)

func Stops() []diagnostics.Stop {
	return []diagnostics.Stop{requestInvalid, hostnameRefused, hostnameUnproved}
}

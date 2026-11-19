package power

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	powerRequestInvalid = diagnostics.Stop{Code: "power.request-invalid", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the power request names a verb or context the command does not take, so nothing was sent", Remedy: "repeat the command with a supported verb and context"}
	powerIdentityChanged = diagnostics.Stop{Code: "power.identity-changed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the management controller answers as another system than the one the context's apply proved, so no power request was sent", Remedy: "correct the Machine's BMC or provider, or take back and apply the context again"}
	powerEvidenceMismatch = diagnostics.Stop{Code: "power.evidence-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the power or eject evidence names another request, Machine or survey, or a state the command cannot name", Remedy: "repeat the command; report the message if it stops the same way"}
	powerUnproved = diagnostics.Stop{Code: "power.unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the management controller did not prove the power or media state the command asked for", Remedy: "read the power state again with bootwright machine status, then repeat the command"}
)

var stops = []diagnostics.Stop{powerRequestInvalid, powerIdentityChanged, powerEvidenceMismatch, powerUnproved}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

// UnprovedRegisteredMachine refuses a lifecycle power request without the original machine and access proof.
func UnprovedRegisteredMachine() error {
	return powerIdentityChanged.Fail("the owning apply did not prove the machine and access needed for this power transition", "restore its original machine proof and supported management access")
}

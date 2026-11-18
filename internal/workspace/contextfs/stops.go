package contextfs

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var contextfsRunsExhausted = diagnostics.Stop{Code: "contextfs.runs-exhausted", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
	Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryFixThenRepeat,
	Meaning: "the numbers a context's run logs are named by are used up, so no run was started", Remedy: "remove the run log directory the message names, then repeat the command"}

var stops = []diagnostics.Stop{contextfsRunsExhausted}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

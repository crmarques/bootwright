package kernel

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var ContextBusy = diagnostics.Stop{Code: "context.busy", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyPossible,
	Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryWait,
	Meaning: "another Bootwright command holds the context's root lock or lease, so this command stopped where it needed it", Remedy: "repeat the command once the other command on the context has finished"}

var stops = []diagnostics.Stop{ContextBusy}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

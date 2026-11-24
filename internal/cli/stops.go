package cli

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var commandUnconfirmed = diagnostics.Stop{Code: "command.unconfirmed", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
	Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryAuthorize,
	Meaning: "the confirmation an apply or destroy asks for was declined, canceled or could not be asked, so nothing began", Remedy: "after a decline or a non-interactive refusal, review the plan, then repeat the command with --yes; after a cancellation, repeat the command to be asked again"}

var stops = []diagnostics.Stop{commandUnconfirmed}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

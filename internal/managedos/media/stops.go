package media

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var mediaStoreBusy = diagnostics.Stop{Code: "media.store-busy", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyPossible,
	Cause: diagnostics.CauseConcurrency, Recovery: diagnostics.RecoveryWait,
	Meaning: "another Bootwright command holds the media store, so an image was verified and retained but not published", Remedy: "repeat the command once the other command finishes, which publishes the retained image"}

var stops = []diagnostics.Stop{mediaStoreBusy}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

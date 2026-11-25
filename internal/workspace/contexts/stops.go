package contexts

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	contextInputAdded = diagnostics.Stop{Code: "input.added-since-apply", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a warning: imported input declares more than the completed apply froze, only add-on instances, and the next apply adds it without running its blocks", Remedy: "run bootwright apply --context <context> to add it to that apply"}
	contextInputChanged = diagnostics.Stop{Code: "input.changed-since-apply", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a warning: the context holds a completed apply and imported input changed since it, which apply refuses until a destroy takes back what that apply owns", Remedy: "take the apply back with bootwright destroy --context <context>, then apply again"}
	contextInputChangesKept = diagnostics.Stop{Code: "input.changes-kept", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a warning: the context rests trimmed and imported input would change a step its staged destroy kept, declares nothing it took back, or declares a step none took back, which the restoring apply refuses", Remedy: "follow the remedy the message names, then apply"}
	contextRestoreUnchecked = diagnostics.Stop{Code: "input.restore-unchecked", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a warning: the context rests trimmed and the restoring apply's comparison of the imported input could not run", Remedy: "review the restore with bootwright apply --dry-run --context <context>, which names the cause"}
)

var stops = []diagnostics.Stop{contextInputAdded, contextInputChanged, contextInputChangesKept, contextRestoreUnchecked}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

func warning(stop diagnostics.Stop, message, remediation string) diagnostics.Diagnostic {
	reported := diagnostics.Of(stop.Fail(message, remediation))[0]
	reported.Severity = "warning"
	return reported
}

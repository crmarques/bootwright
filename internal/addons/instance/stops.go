package instance

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	addonBindingInvalid = diagnostics.Stop{Code: "addon.binding-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an add-on binding, or the add-on it selects, cannot be realized or rendered as declared, so nothing was planned", Remedy: "correct the add-on binding"}
	addonControllerDiffers = diagnostics.Stop{Code: "addon.controller-differs", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the controller an add-on selects is not the controller of the operation, so nothing was planned", Remedy: "correct the controller selection"}
	addonFactsUnavailable = diagnostics.Stop{Code: "addon.facts-unavailable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the add-on capability was built without the cluster facts its plan reads, so nothing was planned", Remedy: "report the message as a defect"}
	addonPlacementInvalid = diagnostics.Stop{Code: "addon.placement-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Secret placement of an add-on names input that is undeclared or does not fit it, so nothing was planned", Remedy: "correct the Secret the add-on's placement names"}
	addonFrozenOtherBuild = diagnostics.Stop{Code: "addon.frozen-other-build", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "the frozen add-on request does not match what this build holds for its package, so no adapter ran", Remedy: "destroy the operation with the build that froze it, or plan again with this build"}
	addonCompletionUnproved = diagnostics.Stop{Code: "addon.completion-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the add-on adapter ended without evidence that its objects reached the declared state", Remedy: "repeat apply to observe its objects"}
	addonOutcomeUnknown = diagnostics.Stop{Code: "addon.outcome-unknown", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the add-on adapter ended without an outcome the block can settle on", Remedy: "repeat apply to observe its objects"}
	addonAdditionUndecodable = diagnostics.Stop{Code: "addon.addition-undecodable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "an add-on instance added to a completed apply froze a request this build cannot read, so nothing was added", Remedy: "report the message as a defect"}
	addonClusterNotInstalled = diagnostics.Stop{Code: "addon.cluster-not-installed", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an add-on added to a completed apply names a cluster that apply did not install, so nothing was added", Remedy: "declare it on a cluster that apply installed, or take the apply back and apply again"}
)

var stops = []diagnostics.Stop{addonBindingInvalid, addonControllerDiffers, addonFactsUnavailable, addonPlacementInvalid, addonFrozenOtherBuild,
	addonCompletionUnproved, addonOutcomeUnknown, addonAdditionUndecodable, addonClusterNotInstalled}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

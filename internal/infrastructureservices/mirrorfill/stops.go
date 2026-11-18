package mirrorfill

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	mirrorPlanningInputMissing = diagnostics.Stop{Code: "mirror.planning-input-missing", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the mirror capability was built without an input its plan reads, so it planned nothing", Remedy: "report the message as a defect"}
	mirrorRequestInvalid = diagnostics.Stop{Code: "mirror.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "the frozen mirror request is not one this build can run, so no adapter ran", Remedy: "destroy it with the build that applied it"}
	mirrorUnconfigured = diagnostics.Stop{Code: "mirror.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the mirror capability runs without an adapter or helper it needs, so no adapter ran", Remedy: "report the message as a defect"}
	mirrorAdapterUnproved = diagnostics.Stop{Code: "mirror.adapter-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the mirror adapter ended without proving its postcondition", Remedy: "repeat the operation, which observes the mirror again"}
	mirrorEvidenceMismatch = diagnostics.Stop{Code: "mirror.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the mirror evidence names another request or version than the block froze, or lies outside its bounds", Remedy: "repeat the operation, which observes the mirror again"}
	mirrorEvidenceUnencodable = diagnostics.Stop{Code: "mirror.evidence-unencodable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the mirror's removal evidence could not be encoded", Remedy: "report the message as a defect"}
	mirrorDrift = diagnostics.Stop{Code: "mirror.drift", Level: diagnostics.LevelResolution, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the mirror holds content its frozen request does not name in that place", Remedy: "restore the mirror to what its frozen request names, then repeat the operation"}
	mirrorClientUnselected = diagnostics.Stop{Code: "mirror.client-unselected", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the controller stage selects no oc client for the release the mirror host copies", Remedy: "set the cluster's release version to one this build installs"}
	mirrorSkopeoMissing = diagnostics.Stop{Code: "mirror.skopeo-missing", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the mirror host has no working skopeo, so its images were not copied", Remedy: "install the skopeo package on the mirror host, then repeat the apply"}
	mirrorDigestMismatch = diagnostics.Stop{Code: "mirror.digest-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a digest the mirror host read is not the one this build pins, so the fill stopped", Remedy: "verify the origin and the route to it, then repeat the apply"}
	mirrorReleaseIncomplete = diagnostics.Stop{Code: "mirror.release-incomplete", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the image mirror lacks the release or one of its components after the copy", Remedy: "verify the free space of the mirror's storage, then repeat the apply"}
	mirrorOriginUnreachable = diagnostics.Stop{Code: "mirror.origin-unreachable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the mirror host could not read content from its origin", Remedy: "give the mirror host a route to the origins, then repeat the apply"}
	mirrorUnreadable = diagnostics.Stop{Code: "mirror.unreadable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the mirror host could not read the image mirror with its credential and trust anchor", Remedy: "verify the Registry is ready and answers its credential, then repeat the apply"}
)

var stops = []diagnostics.Stop{mirrorPlanningInputMissing, mirrorRequestInvalid, mirrorUnconfigured, mirrorAdapterUnproved, mirrorEvidenceMismatch, mirrorEvidenceUnencodable,
	mirrorDrift, mirrorClientUnselected, mirrorSkopeoMissing, mirrorDigestMismatch, mirrorReleaseIncomplete, mirrorOriginUnreachable, mirrorUnreadable}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

package driver

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	clusterobjectsRuntimeUnavailable = diagnostics.Stop{Code: "clusterobjects.runtime-unavailable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the cluster object runtime or the custody of the cluster's produced input is not available, so no adapter ran", Remedy: "apply the controller and cluster stages first"}
	clusterobjectsIdentityUnavailable = diagnostics.Stop{Code: "clusterobjects.identity-unavailable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the apply that owns the cluster objects named no ownership identity, so no adapter ran", Remedy: "report the message as a defect"}
	clusterobjectsOperationUnsupported = diagnostics.Stop{Code: "clusterobjects.operation-unsupported", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a cluster object run was asked for an operation other than apply or observe, so no adapter ran", Remedy: "report the message as a defect"}
	clusterobjectsPlacementUnbound = diagnostics.Stop{Code: "clusterobjects.placement-unbound", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Secret a placement copies into the cluster is not bound to the operation, so no adapter ran", Remedy: "restore its Secret binding or apply the export producer first"}
	clusterobjectsPlacementUnmapped = diagnostics.Stop{Code: "clusterobjects.placement-unmapped", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Secret placement does not map exactly the parts of the Secret it copies, so no adapter ran", Remedy: "correct the package part mapping"}
	clusterobjectsEvidenceMismatch = diagnostics.Stop{Code: "clusterobjects.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence a cluster object adapter returned names another request or version", Remedy: "repeat the operation, which observes the objects again"}
	clusterobjectsEvidenceUnsafe = diagnostics.Stop{Code: "clusterobjects.evidence-unsafe", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence a cluster object adapter returned carries native values that may not be kept", Remedy: "repeat the operation, which observes the objects again"}
)

var stops = []diagnostics.Stop{clusterobjectsRuntimeUnavailable, clusterobjectsIdentityUnavailable, clusterobjectsOperationUnsupported,
	clusterobjectsPlacementUnbound, clusterobjectsPlacementUnmapped, clusterobjectsEvidenceMismatch, clusterobjectsEvidenceUnsafe}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

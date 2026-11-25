package agentinstall

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	openshiftPlanStateMissing = diagnostics.Stop{Code: "openshift.plan-state-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "an OpenShift cluster capability was asked to plan without the compiled desired state", Remedy: "report the message as a defect"}
	openshiftInputUnprojectable = diagnostics.Stop{Code: "openshift.input-unprojectable", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the declared cluster cannot be projected into an installer configuration, so no installation was planned", Remedy: "correct the cluster the message names and plan again"}
	openshiftSelectionUnsupported = diagnostics.Stop{Code: "openshift.selection-unsupported", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the declared cluster selects something this executable does not install, so no installation was planned", Remedy: "remove or change the selection the message names"}
	openshiftAdapterUnconfigured = diagnostics.Stop{Code: "openshift.adapter-unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the OpenShift cluster capability runs without an adapter or reader it needs, so nothing began", Remedy: "report the message as a defect"}
	openshiftOutcomeUnusable = diagnostics.Stop{Code: "openshift.outcome-unusable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an OpenShift cluster adapter ended and reported no outcome the capability can use", Remedy: "repeat the operation, which observes the cluster again"}
	openshiftRequestInvalid = diagnostics.Stop{Code: "openshift.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the frozen request of an OpenShift cluster block breaks its bounds or its shape, so no adapter ran", Remedy: "report the message as a defect"}
	openshiftMachineProofMissing = diagnostics.Stop{Code: "openshift.machine-proof-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a Machine the cluster installs on has no recorded proof the installation needs, so nothing began", Remedy: "apply the Machine stage, then repeat the operation"}
	openshiftEvidenceInvalid = diagnostics.Stop{Code: "openshift.evidence-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence of an OpenShift cluster adapter is incomplete or names another request", Remedy: "repeat the operation, which observes the cluster again"}
	openshiftEvidenceUnsettled = diagnostics.Stop{Code: "openshift.evidence-unsettled", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence of an OpenShift cluster adapter does not prove the state its block expects", Remedy: "repeat the operation, which observes the cluster again"}
	openshiftEvidenceMismatch = diagnostics.Stop{Code: "openshift.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the evidence of an OpenShift cluster adapter names another cluster, release, image or node than its block froze", Remedy: "find what replaced the cluster the block installed, then repeat the operation"}
	openshiftNodeRefused = diagnostics.Stop{Code: "openshift.node-refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the installation adapter stopped on a node whose management controller, boot image or network refused it", Remedy: "read the message, repair what it names, then repeat the operation"}
	openshiftKubeconfigUnnamed = diagnostics.Stop{Code: "openshift.kubeconfig-unnamed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an administrator kubeconfig the cluster produced names no single cluster authority and principal", Remedy: "repeat the operation, which reads the cluster's kubeconfig again"}
	openshiftKeptNodeHanded = diagnostics.Stop{Code: "openshift.kept-node-handed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "a staged destroy that keeps a cluster's nodes found its work area proving a node was handed the image, so it stopped before that work area is discarded", Remedy: "take back the nodes with the destroy the message names"}
)

var stops = []diagnostics.Stop{openshiftPlanStateMissing, openshiftInputUnprojectable, openshiftSelectionUnsupported, openshiftAdapterUnconfigured,
	openshiftOutcomeUnusable, openshiftRequestInvalid, openshiftMachineProofMissing, openshiftEvidenceInvalid, openshiftEvidenceUnsettled,
	openshiftEvidenceMismatch, openshiftNodeRefused, openshiftKubeconfigUnnamed, openshiftKeptNodeHanded}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

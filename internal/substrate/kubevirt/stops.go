package kubevirt

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	kubevirtContextInvalid = diagnostics.Stop{Code: "kubevirt.context-invalid", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the lifecycle context's name is not a valid identity, so no KubeVirt request was derived", Remedy: "name the context with a valid lowercase identifier"}
	kubevirtNameUnsafe = diagnostics.Stop{Code: "kubevirt.name-unsafe", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine's name cannot be used as a host identifier, so no KubeVirt request was derived", Remedy: "rename the Machine"}
	kubevirtNICMACCollision = diagnostics.Stop{Code: "kubevirt.nic-mac-collision", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "two NICs derive the same MAC address, so no KubeVirt request was derived", Remedy: "rename one of the NICs or Machines"}
	kubevirtNICNetworkKind = diagnostics.Stop{Code: "kubevirt.nic-network-kind", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a NIC names a segment that is not a Localnet or bridge network, so no KubeVirt request was derived", Remedy: "select a ClusterUserDefinedNetwork or NetworkAttachmentDefinition segment for the NIC"}
	kubevirtPlanWithoutState = diagnostics.Stop{Code: "kubevirt.plan-without-state", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the KubeVirt capability was asked to plan without compiled desired state", Remedy: "report the message as a defect"}
	kubevirtUnconfigured = diagnostics.Stop{Code: "kubevirt.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the KubeVirt capability runs without its adapter, so nothing ran", Remedy: "report the message as a defect"}
	kubevirtAccessUncustodied = diagnostics.Stop{Code: "kubevirt.access-uncustodied", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the access of a host cluster was asked for outside an operation that holds its custody, so none was lent", Remedy: "report the message as a defect"}
	kubevirtOwnershipUnavailable = diagnostics.Stop{Code: "kubevirt.ownership-unavailable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the operation that owns a KubeVirt machine's VirtualMachine is not known to its run, so no adapter ran", Remedy: "report the message as a defect"}
	kubevirtEvidenceUnencodable = diagnostics.Stop{Code: "kubevirt.evidence-unencodable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "evidence or a pin the KubeVirt capability composed cannot be encoded", Remedy: "report the message as a defect"}
	kubevirtProofUnrecorded = diagnostics.Stop{Code: "kubevirt.proof-unrecorded", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the proof of the host cluster a Machine is realized on is not recorded for the operation, so no adapter ran", Remedy: "run the apply again so the provider's proof precedes its Machines"}
	kubevirtHostChanged = diagnostics.Stop{Code: "kubevirt.host-changed", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the host cluster a provider now reaches is not the one its VirtualMachine was realized on, so no adapter ran", Remedy: "restore the host cluster the Machine was realized on, or destroy and apply again"}
	kubevirtOutcomeUnusable = diagnostics.Stop{Code: "kubevirt.outcome-unusable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the KubeVirt adapter reported no outcome the capability can use", Remedy: "repeat the operation, which observes the effect again"}
	kubevirtEvidenceMismatch = diagnostics.Stop{Code: "kubevirt.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the KubeVirt evidence names another request, VirtualMachine or cluster than the block froze", Remedy: "repeat the operation, which observes the effect again"}
	kubevirtLendUnproved = diagnostics.Stop{Code: "kubevirt.lend-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the host access a run was lent did not prove its postcondition on the host cluster", Remedy: "repeat the operation, which lends the access again"}
	kubevirtHostUnproved = diagnostics.Stop{Code: "kubevirt.host-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the KubeVirt provider's proof does not hold the host as OpenShift Virtualization ready for virtual machines", Remedy: "correct the host or spec.kubevirt of the provider, then run the apply again"}
	kubevirtHostSilent = diagnostics.Stop{Code: "kubevirt.host-silent", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the host cluster did not answer for the VirtualMachine, so the evidence proves nothing", Remedy: "restore access to the host cluster, then repeat the operation"}
	kubevirtMachineUnproved = diagnostics.Stop{Code: "kubevirt.machine-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the KubeVirt machine evidence does not prove the VirtualMachine in the state the block asked for", Remedy: "repeat the operation, which observes the VirtualMachine again"}
	kubevirtMachineForeign = diagnostics.Stop{Code: "kubevirt.machine-foreign", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a VirtualMachine of the Machine's name exists on the host that this context does not own", Remedy: "remove or rename the VirtualMachine the message names, then repeat the operation"}
	kubevirtRemovalUnproved = diagnostics.Stop{Code: "kubevirt.removal-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the KubeVirt removal evidence does not prove the VirtualMachine and what it owned gone, or still whole", Remedy: "repeat the operation, which observes the VirtualMachine again"}
)

var stops = []diagnostics.Stop{
	kubevirtContextInvalid, kubevirtNameUnsafe, kubevirtNICMACCollision, kubevirtNICNetworkKind, kubevirtPlanWithoutState, kubevirtUnconfigured,
	kubevirtAccessUncustodied, kubevirtOwnershipUnavailable, kubevirtEvidenceUnencodable, kubevirtProofUnrecorded, kubevirtHostChanged,
	kubevirtOutcomeUnusable, kubevirtEvidenceMismatch, kubevirtLendUnproved, kubevirtHostUnproved, kubevirtHostSilent, kubevirtMachineUnproved,
	kubevirtMachineForeign, kubevirtRemovalUnproved,
}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

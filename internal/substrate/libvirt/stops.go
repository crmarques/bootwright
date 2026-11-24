package libvirt

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	libvirtDeclarationInvalid = diagnostics.Stop{Code: "libvirt.declaration-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a libvirt provider, segment, Network or Machine declares a name or arm the host cannot realize, so no adapter ran", Remedy: "correct the declaration the message names"}
	libvirtRequestInvalid = diagnostics.Stop{Code: "libvirt.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a plan reached the libvirt capability without compiled state, or a request it composed breaks its shape or its bounds, so no adapter ran", Remedy: "report the message as a defect"}
	libvirtFrozenInvalid = diagnostics.Stop{Code: "libvirt.frozen-request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a frozen libvirt request holds a media presentation this build cannot run, so no adapter ran", Remedy: "destroy it with the build that applied it"}
	libvirtUnconfigured = diagnostics.Stop{Code: "libvirt.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a libvirt capability runs without its adapter", Remedy: "report the message as a defect"}
	libvirtRecordsMissing = diagnostics.Stop{Code: "libvirt.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the applied revision places no libvirt profile under a machine whose evidence it holds", Remedy: "restore the context store from a backup"}
	libvirtOutcomeUnusable = diagnostics.Stop{Code: "libvirt.outcome-unusable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a libvirt adapter ended without a usable outcome", Remedy: "repeat the operation, which observes the host before anything else starts"}
	libvirtPostconditionUnproved = diagnostics.Stop{Code: "libvirt.postcondition-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a libvirt adapter ended without proving the postcondition of its apply or its removal", Remedy: "repeat the operation, which observes the host before anything else starts"}
	libvirtEvidenceMismatch = diagnostics.Stop{Code: "libvirt.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "libvirt evidence names another request than the block froze, or reports a shape that request does not allow", Remedy: "repeat the operation, which observes the host again"}
	libvirtHostDrifted = diagnostics.Stop{Code: "libvirt.host-drifted", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the provider host or machine does not hold what the frozen libvirt request declares", Remedy: "repeat the apply, which restores what the request declares"}
	libvirtHostSilent = diagnostics.Stop{Code: "libvirt.host-silent", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the libvirt connection, a driver or NetworkManager did not answer for what the evidence asks about, so nothing is proved", Remedy: "restore the service the message names on the provider host, then repeat the operation"}
	libvirtRemovalIncomplete = diagnostics.Stop{Code: "libvirt.removal-incomplete", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "removal evidence still reports something the removal owns, or does not prove it gone", Remedy: "repeat the destroy, which takes back what remains"}
	libvirtOwnershipConflict = diagnostics.Stop{Code: "libvirt.ownership-conflict", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an object of the name the provider uses exists without this context's ownership, so no repeat converges it", Remedy: "remove or rename the object the message names, then repeat the operation"}
)

var stops = []diagnostics.Stop{
	libvirtDeclarationInvalid, libvirtRequestInvalid, libvirtFrozenInvalid, libvirtUnconfigured, libvirtRecordsMissing, libvirtOutcomeUnusable, libvirtPostconditionUnproved,
	libvirtEvidenceMismatch, libvirtHostDrifted, libvirtHostSilent, libvirtRemovalIncomplete, libvirtOwnershipConflict,
}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

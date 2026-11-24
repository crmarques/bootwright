package diskdiscovery

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	discoveryUnconfigured = diagnostics.Stop{Code: "discovery.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "disk discovery runs without compiled state or one of the readers and sources it depends on", Remedy: "report the message as a defect"}
	discoveryRequestInvalid = diagnostics.Stop{Code: "discovery.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the frozen disk discovery request names something its adapter cannot run, so no adapter ran", Remedy: "report the message as a defect"}
	discoveryUnsupported = diagnostics.Stop{Code: "discovery.unsupported", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the Machine or the artifact server it names cannot host a disk discovery boot", Remedy: "use a controller-local HTTPS artifact server and full or certificate BMC verification"}
	discoveryMachineInstalled = diagnostics.Stop{Code: "discovery.machine-installed", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the Machine already has installation ownership, so booting it for discovery would disturb that installation", Remedy: "retain the existing installation or remove it through its recorded lifecycle first"}
	discoveryPrerequisitesMissing = diagnostics.Stop{Code: "discovery.prerequisites-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "the controller and infrastructure setup a discovery boot needs is not applied", Remedy: "repeat bootwright machine discover with --stage controller to review and confirm controller and infrastructure setup"}
	discoveryRefinementRefused = diagnostics.Stop{Code: "discovery.refinement-refused", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a change after preparation reaches beyond the disk-selection fields that may follow it", Remedy: "keep prepared ownership unchanged and update only the disk-selection fields"}
	discoveryRecordsMissing = diagnostics.Stop{Code: "discovery.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the records a disk discovery builds on hold no inventory, proof or identity the block names", Remedy: "restore the context store from a backup"}
	discoveryRecordsMismatch = diagnostics.Stop{Code: "discovery.records-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryNone,
		Meaning: "a recorded disk inventory or attempt names another Machine, request or execution than the block froze", Remedy: "retain the original inventory and resolve its ownership before preparing another"}
	discoveryRecordsConflict = diagnostics.Stop{Code: "discovery.records-conflict", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryNone,
		Meaning: "more than one owned disk inventory competes for one Machine", Remedy: "retain one inventory and resolve the ownership of the others"}
	discoveryInventoryMissing = diagnostics.Stop{Code: "discovery.inventory-missing", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryOtherVerb,
		Meaning: "the Machine has no completed disk discovery for its current policy and media", Remedy: "run bootwright machine discover with --stage controller"}
	discoveryInventoryStale = diagnostics.Stop{Code: "discovery.inventory-stale", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an owned inventory belongs to a different discovery request", Remedy: "restore the recorded discovery settings or review removal of the current lifecycle ownership before discovering again"}
	discoveryRootUnprovable = diagnostics.Stop{Code: "discovery.root-unprovable", Level: diagnostics.LevelContext, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the recorded inventory cannot prove the protected disks and exactly one eligible root", Remedy: "inspect the recorded inventory and correct the protected storage claim or root match named by the refusal, then update the context"}
	discoveryEvidenceMismatch = diagnostics.Stop{Code: "discovery.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the disk discovery evidence names another contract or request than the block froze", Remedy: "repeat the operation, which observes the discovery again"}
	discoveryIncomplete = diagnostics.Stop{Code: "discovery.incomplete", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the disk discovery adapter ended without proving its inventory and the cleanup of its boot", Remedy: "repeat the operation, which converges the discovery boot"}
)

var stops = []diagnostics.Stop{discoveryUnconfigured, discoveryRequestInvalid, discoveryUnsupported, discoveryMachineInstalled, discoveryPrerequisitesMissing, discoveryRefinementRefused, discoveryRecordsMissing, discoveryRecordsMismatch, discoveryRecordsConflict, discoveryInventoryMissing, discoveryInventoryStale, discoveryRootUnprovable, discoveryEvidenceMismatch, discoveryIncomplete}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

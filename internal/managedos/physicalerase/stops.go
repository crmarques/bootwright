package physicalerase

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	stopUnconfigured = diagnostics.Stop{Code: "erase.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "the physical erase capability runs without compiled state or a collaborator it needs", Remedy: "report the message as a defect"}
	stopCustodyUnavailable = diagnostics.Stop{Code: "erase.custody-unavailable", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a physical erase was asked to run without durable removal receipt custody, so nothing began", Remedy: "continue the original destroy through the lifecycle engine"}
	stopRecordsMissing = diagnostics.Stop{Code: "erase.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "the original apply or removal records the physical erase rests on are missing or do not prove it", Remedy: "restore the context store from a backup"}
	stopEvidenceMismatch = diagnostics.Stop{Code: "erase.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "an erase receipt or waiver names another machine, MAC set or preparation than the records froze", Remedy: "restore the original apply and removal records"}
	stopReceiptIncomplete = diagnostics.Stop{Code: "erase.receipt-incomplete", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the erase evidence does not prove every byte its method erases or the withdrawal of its custody", Remedy: "continue the same removal"}
	stopMachineRunning = diagnostics.Stop{Code: "erase.machine-running", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the machine is running, so the offline erase inserted no media into it and did not boot it", Remedy: "stop the machine, then repeat the destroy"}
	stopTargetMismatch = diagnostics.Stop{Code: "erase.target-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the controller answers as another system, or the machine reports other hardware, than the apply proved, so the erase stopped before it acted", Remedy: "correct the controller or the hardware, then repeat the destroy"}
	stopControllerUnanswered = diagnostics.Stop{Code: "erase.controller-unanswered", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the management controller did not answer the system read, so the erase stopped before it acted", Remedy: "restore the controller's access, then repeat the destroy"}
	stopControllerRefused = diagnostics.Stop{Code: "erase.controller-refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the management controller refused a virtual-media, boot or power request of the erase", Remedy: "correct its cause through the BMC, then repeat the destroy"}
	stopControllerDeadline = diagnostics.Stop{Code: "erase.controller-deadline", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDeadline, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the management controller did not complete a phase of the erase within its time bound", Remedy: "restore the controller's responsiveness, then repeat the destroy"}
	stopPowerOffUnproved = diagnostics.Stop{Code: "erase.poweroff-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the machine did not power off after its erase receipt was acknowledged, so its erase media was not ejected", Remedy: "stop the machine, then repeat the destroy"}
	stopWorkerFailed = diagnostics.Stop{Code: "erase.worker-failed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the RAM erase worker stopped without a complete receipt, so no target is proved erased", Remedy: "stop the machine, then repeat the destroy"}
	stopWorkerUnreachable = diagnostics.Stop{Code: "erase.worker-unreachable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the controller did not reach the RAM erase worker within its wait, so no target is proved erased", Remedy: "check that the controller reaches the worker's address, then repeat the destroy"}
	stopForeignWorker = diagnostics.Stop{Code: "erase.foreign-worker", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the host answering at the worker's address is not this removal's RAM erase worker, so its receipt was not accepted", Remedy: "look at the BMC console and boot order, then repeat the destroy"}
	stopReceiptTimeout = diagnostics.Stop{Code: "erase.receipt-timeout", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseDeadline, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the RAM erase worker did not report a complete receipt within the erase deadline", Remedy: "stop the machine, then repeat the destroy"}
	stopCustodyMismatch = diagnostics.Stop{Code: "erase.custody-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the private erase custody on the execution host belongs to another request or differs from what the removal recorded, so it was not used", Remedy: "inspect the custody on the execution host, then repeat the destroy"}
	stopBootMediaMismatch = diagnostics.Stop{Code: "erase.boot-media-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the installer image the apply pinned is missing from the media store or no longer matches, so no erase image was built", Remedy: "restore the image with bootwright catalog media add, then repeat the destroy"}
	stopImageBuildFailed = diagnostics.Stop{Code: "erase.image-build-failed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the erase image could not be built from its pinned installer image within its time bound, so the machine was not booted", Remedy: "check the execution host's free space and tools, then repeat the destroy"}
	stopInstallNetwork = diagnostics.Stop{Code: "erase.install-network", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryAuthorize,
		Meaning: "the install network names a bond option or VLAN that the erase image's boot arguments cannot carry, so the machine was not booted", Remedy: "release the machine without erasing its disks"}
	stopRelaxationInapplicable = diagnostics.Stop{Code: "erase.relaxation-inapplicable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an erase relaxation names a block that is no removal erase, a method other than metadataClear or retain, or one that changes nothing, so nothing was registered", Remedy: "repeat the destroy without that --authorize"}
)

var stops = []diagnostics.Stop{stopUnconfigured, stopCustodyUnavailable, stopRecordsMissing, stopEvidenceMismatch, stopReceiptIncomplete, stopMachineRunning, stopTargetMismatch,
	stopControllerUnanswered, stopControllerRefused, stopControllerDeadline, stopPowerOffUnproved, stopWorkerFailed, stopWorkerUnreachable, stopForeignWorker, stopReceiptTimeout,
	stopCustodyMismatch, stopBootMediaMismatch, stopImageBuildFailed, stopInstallNetwork, stopRelaxationInapplicable}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

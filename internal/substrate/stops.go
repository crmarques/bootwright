package substrate

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	prebootUnrealizedSubstrate = diagnostics.Stop{Code: "preboot.unrealized-substrate", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Machine is placed on a provider whose substrate this executable does not realize, so nothing was planned", Remedy: "place the Machine on a libvirt, bare-metal or KubeVirt provider"}
	prebootTargetUnanswered = diagnostics.Stop{Code: "preboot.target-unanswered", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseEnvironment, Fault: diagnostics.FaultUnreachable, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the management controller did not answer the system read that proves a physical machine, so what it is stays unknown", Remedy: "restore access to the controller, then repeat the apply"}
	prebootHardwareMismatch = diagnostics.Stop{Code: "preboot.hardware-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a physical machine does not report exactly the hardware its Machine declares, or its inventory could not be read in full, so no media was inserted", Remedy: "make the machine report the hardware its Machine declares, then continue the apply"}
	prebootIdentityMismatch = diagnostics.Stop{Code: "preboot.identity-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the system answering for a Machine is not the one the operation proved for it, so no media was inserted", Remedy: "correct the declared address of the Machine, or restore the system it was realized on, then continue the apply"}
	prebootMachineRunning = diagnostics.Stop{Code: "preboot.machine-running", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a machine is running while its installation would write its disk, so no media was inserted", Remedy: "stop the machine, then continue the apply"}
	prebootTPMAbsent = diagnostics.Stop{Code: "preboot.tpm-absent", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an encrypted installation has no positive proof of a TPM 2.0, so no encrypted installer was booted", Remedy: "provide a TPM 2.0 for the machine, then continue the apply"}
	prebootFirmwareMismatch = diagnostics.Stop{Code: "preboot.firmware-mismatch", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a machine does not report the firmware settings its Machine asks to prove, so no media was inserted", Remedy: "set the declared firmware values through the controller, then continue the apply"}
	prebootFirmwareNotApplied = diagnostics.Stop{Code: "preboot.firmware-not-applied", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the controller refused or did not apply the firmware settings its Machine asks to enforce, and a setup boot or settings it staged may remain", Remedy: "correct the cause through the controller, then continue the apply"}
)

var stops = []diagnostics.Stop{prebootUnrealizedSubstrate, prebootTargetUnanswered, prebootHardwareMismatch, prebootIdentityMismatch, prebootMachineRunning,
	prebootTPMAbsent, prebootFirmwareMismatch, prebootFirmwareNotApplied}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }

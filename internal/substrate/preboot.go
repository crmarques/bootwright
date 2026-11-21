package substrate

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The refusals a machine role's pre_boot entry point names when its proof of
// the target fails, each left for the installation that composed it to name to
// its own runner, which reports the installation's diagnostic for it instead
// of the adapter's failure. An unanswered system read keeps its cause and stays unknown.
const (
	// RefusalTargetUnanswered preserves an unknown physical target proof.
	RefusalTargetUnanswered = "target-unanswered"
	// RefusalHardwareMismatch is a physical machine whose complete hardware
	// inventory is not the declared set, or that could not be read in full.
	RefusalHardwareMismatch = "hardware-mismatch"
	// RefusalIdentityMismatch is a physical machine that answers as another
	// system than the one its Machine's block pinned in this operation.
	RefusalIdentityMismatch = "identity-mismatch"
	// RefusalMachineRunning is a machine found running, whose disk an
	// installation would write while it runs.
	RefusalMachineRunning = "machine-running"
	// RefusalTPMAbsent means an encrypted install lacks a positive TPM 2.0 proof.
	RefusalTPMAbsent = "tpm-absent"
	// RefusalFirmwareMismatch is a prove firmware block the machine does not match.
	RefusalFirmwareMismatch = "firmware-setting-mismatch"
	// RefusalFirmwareNotApplied is an enforce firmware block the BMC refused or did not apply.
	RefusalFirmwareNotApplied = "firmware-setting-not-applied"
)

// PreBootRefusals is what the pre-boot proof of a Machine on one arm may
// refuse, each with the diagnostic an installation reports for it: the Machine
// as the refused object, what was refused and what to change, with every
// command it names bound to the context the installation runs in. The
// continuation is the exact command that continues the refusing apply,
// bootwright apply --context <context> with every --authorize token its frozen
// plan consumes, as the lifecycle's execution carries it; when it is empty the
// remedy names the plain apply of the context. An arm with no pre-boot entry
// point refuses nothing here, because its installation fails before any proof.
func PreBootRefusals(arm Arm, contextName, continuation, machine, endpoint string) map[string]error {
	identity := string(api.Machine) + "/" + machine
	refused := func(stop diagnostics.Stop, message, remediation string) error {
		failure := stop.Fail(message, remediation).(*diagnostics.Failure)
		failure.Diagnostics[0].Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: machine}
		return failure
	}
	apply := "bootwright apply --context " + contextName
	next := continuation
	if next == "" {
		next = apply
	}
	running := refused(prebootMachineRunning, identity+" is running, and its installation erases its disk, so no media was inserted into it and it was not booted",
		"stop it with bootwright machine stop --context "+contextName+" --name "+machine+", then run "+next)
	// The refusing apply stays incomplete, so it continues its frozen plan
	// and the context's input cannot change under it: a corrected declaration
	// takes effect only after a destroy takes the context back.
	destroy := "take this context back with bootwright destroy --context " + contextName
	update := " and import that input with bootwright update --name " + contextName + " --input-dir <directory>"
	switch arm {
	case ArmLibvirt:
		return map[string]error{RefusalMachineRunning: running}
	case ArmKubeVirt:
		return map[string]error{
			RefusalMachineRunning: running,
			RefusalTPMAbsent: refused(prebootTPMAbsent, identity+" presents no persistent vTPM, so no encrypted installer was booted",
				destroy+", declare tpm without persistent: false on the profile "+identity+" selects"+update+", then run "+apply),
			RefusalIdentityMismatch: refused(prebootIdentityMismatch, "the host cluster or VirtualMachine answering for "+identity+" is not the one its machine block recorded, so no media was inserted into it and it was not booted",
				"restore the Secret the provider's spec.kubevirt.kubeconfigRef names so it reaches the cluster "+identity+" was realized on, then run "+next+
					" with --authorize rebind=<that Secret>; to realize it again instead, "+destroy+", then run "+apply),
		}
	case ArmBaremetal:
		return map[string]error{
			RefusalTargetUnanswered: UnansweredTarget(machine, endpoint),
			RefusalHardwareMismatch: refused(prebootHardwareMismatch, "the machine answering at "+endpoint+" does not report exactly the hardware addresses "+identity+
				" declares, or its inventory could not be read in full, so no media was inserted into it and it was not booted",
				"once the machine reports the hardware "+identity+" declares in full, run "+next+"; to correct that declaration instead, "+destroy+
					", correct spec.hardware.bmc.address or spec.hardware.nics on "+identity+update+", then run "+apply),
			RefusalIdentityMismatch: refused(prebootIdentityMismatch, "the management controller at "+endpoint+" answers as another system than the one this operation proved for "+
				identity+", so no media was inserted into it and it was not booted",
				destroy+", correct spec.hardware.bmc.address on "+identity+" if it names another BMC"+update+", then run "+apply+
					", so the machine is proved again"),
			RefusalMachineRunning: running,
			RefusalTPMAbsent:      refused(prebootTPMAbsent, identity+" does not positively report an enabled TPM 2.0, so no encrypted installer was booted", "enable a supported TPM 2.0 on "+identity+", then run "+next),
			RefusalFirmwareMismatch: refused(prebootFirmwareMismatch, "the machine answering at "+endpoint+" does not report the firmware settings "+identity+" declares under "+FirmwarePath+".mode prove, so no media was inserted into it and it was not booted",
				"set the declared values on "+identity+" through its BMC, then run "+next+"; to have Bootwright set them instead, "+destroy+
					", set "+FirmwarePath+".mode to enforce on "+identity+update+", then run "+apply),
			RefusalFirmwareNotApplied: refused(prebootFirmwareNotApplied, "the management controller at "+endpoint+" refused or did not apply the firmware settings "+identity+" declares, so no media was inserted into it and no installer was booted",
				"when the reasons say the machine was not proved off, first stop it with bootwright machine stop --context "+contextName+" --name "+machine+" --force; "+
					"read the BMC's message in the attempt's retained output and correct the cause through the BMC, then run "+next+
					"; any one-time setup boot and settings this step staged stay on the machine and apply at its next boot; to correct the declaration instead, "+destroy+
					", correct "+FirmwarePath+" on "+identity+update+", then run "+apply),
		}
	}
	return nil
}

// UnansweredTarget never interprets an unavailable system read as different hardware.
func UnansweredTarget(machine, endpoint string) error {
	failure := prebootTargetUnanswered.Fail("the management controller at "+endpoint+" did not answer the system read for Machine/"+machine,
		"read the controller's cause in the attempt's retained output, restore access and repeat apply").(*diagnostics.Failure)
	failure.Diagnostics[0].Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: machine}
	return failure
}

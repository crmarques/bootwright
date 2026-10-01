package substrate

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The refusals a machine role's pre_boot entry point names when its proof of
// the target fails, each left for the installation that composed it to name to
// its own runner, which reports the installation's diagnostic for it instead
// of the adapter's failure. A read the management controller did not answer
// names none: the controller's own message, in the retained output, is its
// reason.
const (
	// RefusalHardwareMismatch is a physical machine whose complete hardware
	// inventory lacks a declared address, or that could not be read in full.
	RefusalHardwareMismatch = "hardware-mismatch"
	// RefusalIdentityMismatch is a physical machine that answers as another
	// system than the one its Machine's block pinned in this operation.
	RefusalIdentityMismatch = "identity-mismatch"
	// RefusalMachineRunning is a machine found running, whose disk an
	// installation would write while it runs.
	RefusalMachineRunning = "machine-running"
)

// PreBootRefusals is what the pre-boot proof of a Machine on one arm may
// refuse, each with the diagnostic an installation reports for it: the Machine
// as the refused object, what was refused and what to change. An arm with no
// pre-boot entry point refuses nothing here, because its installation fails
// before any proof.
func PreBootRefusals(arm, machine, endpoint string) map[string]error {
	identity := string(api.Machine) + "/" + machine
	refused := func(message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: machine},
		}}}
	}
	running := refused(identity+" is running, and its installation erases its disk, so no media was inserted into it and it was not booted",
		"stop it with bootwright machine stop --name "+machine+", then repeat the apply")
	switch arm {
	case ArmLibvirt:
		return map[string]error{RefusalMachineRunning: running}
	case ArmBaremetal:
		return map[string]error{
			RefusalHardwareMismatch: refused("the machine answering at "+endpoint+" does not report every hardware address "+identity+
				" declares, or its inventory could not be read in full, so no media was inserted into it and it was not booted",
				"correct spec.hardware.management.bmc.address or spec.hardware.nics on "+identity+", then repeat the apply"),
			RefusalIdentityMismatch: refused("the management controller at "+endpoint+" answers as another system than the one this operation proved for "+
				identity+", so no media was inserted into it and it was not booted",
				"correct spec.hardware.management.bmc.address on "+identity+", or destroy and apply this context so the machine is proved again"),
			RefusalMachineRunning: running,
		}
	}
	return nil
}

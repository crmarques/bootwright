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
func PreBootRefusals(arm, contextName, continuation, machine, endpoint string) map[string]error {
	identity := string(api.Machine) + "/" + machine
	refused := func(message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: machine},
		}}}
	}
	apply := "bootwright apply --context " + contextName
	next := continuation
	if next == "" {
		next = apply
	}
	running := refused(identity+" is running, and its installation erases its disk, so no media was inserted into it and it was not booted",
		"stop it with bootwright machine stop --context "+contextName+" --name "+machine+", then run "+next)
	// The refusing apply stays incomplete, so it continues its frozen plan
	// and the context's input cannot change under it: a corrected declaration
	// takes effect only after a destroy takes the context back.
	destroy := "take this context back with bootwright destroy --context " + contextName
	update := " and import that input with bootwright context update --name " + contextName + " --input-dir <directory>"
	switch arm {
	case ArmLibvirt:
		return map[string]error{RefusalMachineRunning: running}
	case ArmBaremetal:
		return map[string]error{
			RefusalHardwareMismatch: refused("the machine answering at "+endpoint+" does not report exactly the hardware addresses "+identity+
				" declares, or its inventory could not be read in full, so no media was inserted into it and it was not booted",
				"once the machine reports the hardware "+identity+" declares in full, run "+next+"; to correct that declaration instead, "+destroy+
					", correct spec.substrate.baremetal.bmc.address or spec.substrate.baremetal.nics on "+identity+update+", then run "+apply),
			RefusalIdentityMismatch: refused("the management controller at "+endpoint+" answers as another system than the one this operation proved for "+
				identity+", so no media was inserted into it and it was not booted",
				destroy+", correct spec.substrate.baremetal.bmc.address on "+identity+" if it names another controller"+update+", then run "+apply+
					", so the machine is proved again"),
			RefusalMachineRunning: running,
			RefusalTPMAbsent:      refused(identity+" does not positively report an enabled TPM 2.0, so no encrypted installer was booted", "enable a supported TPM 2.0 on "+identity+", then run "+next),
		}
	}
	return nil
}

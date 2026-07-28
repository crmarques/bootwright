package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateAuthoredMachineAccess(state v1alpha1.State) []string {
	var errs []string
	for _, machine := range state.Machines {
		if !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		prefix := fmt.Sprintf("Machine/%s spec.access", machine.Metadata.Name)
		if machine.Spec.Access.SSH != nil || machine.Spec.Access.Local {
			errs = append(errs, fmt.Sprintf("%s must not be authored on a Machine Bootwright installs (os.provided=false with os.installProfileRef); the install creates the %q service account and authorizes Environment spec.machineAccess.keyRef for it. Remove the block", prefix, v1alpha1.BootwrightSSHUser))
			continue
		}
		if machine.Spec.Access.RootLogin != "" {
			errs = append(errs, fmt.Sprintf("%s.rootLogin must not be authored on a Machine Bootwright installs; the install always writes PermitRootLogin no, so there is no root login to keep or revoke", prefix))
		}
	}
	return errs
}

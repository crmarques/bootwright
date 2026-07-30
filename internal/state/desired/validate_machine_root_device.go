package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateAnacondaRootDeviceHintsAreUsable(state v1alpha1.State) []string {
	var errs []string
	for _, machine := range state.Machines {
		if !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		hints := machine.Spec.OS.Install.RootDeviceHints
		if hints == nil {
			continue
		}
		if _, ok := v1alpha1.AnacondaRootDiskSelector(hints); ok {
			continue
		}
		unusable := v1alpha1.AnacondaUnsupportedRootDeviceHints(hints)
		if len(unusable) == 0 {
			continue
		}
		errs = append(errs, fmt.Sprintf("Machine/%s declares spec.os.install.rootDeviceHints %s, but its managed-OS install cannot select a disk from those fields: a kickstart names a disk by kernel name or /dev/disk/by-id path, not by a predicate. The hint would be silently ignored and the install would fall back to clearpart --all, WIPING every disk on the machine. Set deviceName (for example /dev/sda, or /dev/disk/by-id/... for a stable name) or wwn. %s honoured on the OpenShift agent-installer path, which resolves hints on the host, but a managed-OS install has no such resolver", machine.Metadata.Name, strings.Join(unusable, ", "), unusableHintsAre(unusable)))
	}
	return errs
}

func unusableHintsAre(fields []string) string {
	if len(fields) == 1 {
		return fields[0] + " is"
	}
	return strings.Join(fields, ", ") + " are"
}

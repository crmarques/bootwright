package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeMachineAccess(state *v1alpha1.State) {
	fleetKey := machineAccessKeyRef(*state)
	for i := range state.Machines {
		machine := &state.Machines[i]
		if machine.Spec.Access.Local {
			continue
		}
		if machine.Spec.Access.SSH == nil {
			deriveMachineAccess(machine, fleetKey)
		}
		ssh := machine.Spec.Access.SSH
		if ssh == nil {
			continue
		}
		if ssh.AddressRef.Name == "" {
			ssh.AddressRef.Name = defaultMachineAddressName(*machine)
		}
		if ssh.Auth.IsZero() {
			ssh.Auth.OperatorIdentity = &v1alpha1.MachineSSHOperatorIdentity{}
		}
		if machine.Spec.Access.RootLogin == "" {
			machine.Spec.Access.RootLogin = v1alpha1.MachineRootLoginKeep
		}
	}
}

func deriveMachineAccess(machine *v1alpha1.Machine, fleetKey v1alpha1.SecretRef) {
	switch {
	case v1alpha1.MachineInstallsOS(*machine):
		machine.Spec.Access.SSH = &v1alpha1.MachineSSHSpec{
			User: v1alpha1.BootwrightSSHUser,
			Auth: v1alpha1.MachineSSHAuth{
				Provision: &v1alpha1.MachineSSHProvision{KeyRef: fleetKey},
			},
		}
		machine.DefaultedRefs.Access = true
	case v1alpha1.MachineOSProvided(*machine):
		machine.Spec.Access.SSH = &v1alpha1.MachineSSHSpec{
			Auth: v1alpha1.MachineSSHAuth{OperatorIdentity: &v1alpha1.MachineSSHOperatorIdentity{}},
		}
		machine.DefaultedRefs.Access = true
	}
}

func machineAccessKeyRef(state v1alpha1.State) v1alpha1.SecretRef {
	if len(state.Environments) == 0 {
		return v1alpha1.SecretRef{}
	}
	return state.Environments[0].Spec.MachineAccess.KeyRef
}

func defaultMachineAddressName(machine v1alpha1.Machine) string {
	for _, address := range machine.Spec.Addresses {
		if address.Name == "ssh" {
			return "ssh"
		}
	}
	if _, ok := v1alpha1.MachineAddressByName(machine, v1alpha1.MachineAddressFQDN); ok {
		return v1alpha1.MachineAddressFQDN
	}
	return ""
}

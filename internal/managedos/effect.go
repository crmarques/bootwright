package managedos

import api "github.com/crmarques/bootwright/api/v1alpha1"

func EffectBearing(object api.Object) (string, bool) {
	if object.Kind() != api.Machine {
		return "", false
	}
	if !api.MachineInstalled(object) && !api.MachineClusterInstalled(object) {
		return "", false
	}
	return "no capability of this executable realizes a Machine whose operating system is not provided", true
}
